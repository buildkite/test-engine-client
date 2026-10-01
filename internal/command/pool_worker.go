package command

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/buildkite/test-engine-client/v3/internal/api"
	"github.com/buildkite/test-engine-client/v3/internal/debug"
	"github.com/buildkite/test-engine-client/v3/internal/plan"
	"github.com/buildkite/test-engine-client/v3/internal/runner"
	"github.com/buildkite/test-engine-client/v3/internal/runnerexec"
)

type poolScheduler interface {
	AcquireLease(context.Context, string) (api.LeaseResponse, error)
	HeartbeatLease(context.Context, string, string) (time.Time, error)
	CompleteLease(context.Context, string, string, []api.AttemptResult) error
	ReleaseLease(context.Context, string, string) error
}

// All protocol callbacks are nonblocking; network and report work runs outside
// the host lock. One worker holds one lease, including its local retries.
type poolSource struct {
	mu              sync.Mutex
	pending         *runnerexec.Batch
	dispatched      bool
	dispatchable    bool
	terminating     bool
	undispatchable  bool
	current         *poolLease
	reason          string
	err             error
	passedAttempts  int
	failedAttempts  int
	erroredAttempts int
	reportedTests   map[string]poolReportedTest
	erroredWork     map[string]api.LeaseAttempt
	sequence        int
	retryRound      int
	results         chan runnerexec.Result
	starts          chan batchStart
	cancel          context.CancelFunc
	// Worker goroutine only; inputs to leasePrefetch.schedule.
	pace                float64       // actual runtime / p90 total; 0 until observed
	lastAcquireDuration time.Duration // how long the last lease request took
}

// poolLease is one Scheduler lease and its heartbeat. Ownership fields are
// guarded by poolSource.mu.
type poolLease struct {
	api.Lease
	state   string
	owned   bool
	expires time.Time
	stop    func()
}

type batchStart struct {
	id string
	at time.Time
}

func newPoolSource(cancel context.CancelFunc) *poolSource {
	return &poolSource{
		results:       make(chan runnerexec.Result, 1),
		starts:        make(chan batchStart, 1),
		cancel:        cancel,
		reportedTests: make(map[string]poolReportedTest),
		erroredWork:   make(map[string]api.LeaseAttempt),
	}
}
func (s *poolSource) Next() (*runnerexec.Batch, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return nil, "", s.err
	}
	batch := s.pending
	s.pending = nil
	return batch, s.reason, nil
}
func (s *poolSource) Dispatched(batch runnerexec.Batch) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	lease := s.current
	if !s.dispatchable || s.terminating || lease == nil || !lease.owned || !time.Now().Before(lease.expires) {
		return errors.New("lease no longer owned at dispatch")
	}
	s.dispatchable = false
	s.dispatched = true
	if s.retryRound > 0 {
		debug.Printf("Dispatched local retry batch %s to persistent runner (round=%d)", batch.ID, s.retryRound)
	} else {
		// Replace any stale start; this is the only sender and holds s.mu.
		select {
		case <-s.starts:
		default:
		}
		s.starts <- batchStart{id: batch.ID, at: time.Now()}
		debug.Printf("Dispatched pool batch %s to persistent runner", batch.ID)
	}
	return nil
}
func (s *poolSource) Undispatchable(_ runnerexec.Batch, err error) {
	s.mu.Lock()
	s.undispatchable = true
	s.mu.Unlock()
	s.stop(err)
}
func (s *poolSource) Accepted(_ runnerexec.Batch, result runnerexec.Result) { s.results <- result }
func (s *poolSource) Unresolved(_ runnerexec.Batch, err error)              { s.stop(err) }
func (s *poolSource) Terminating() {
	s.mu.Lock()
	if s.err != nil {
		s.mu.Unlock()
		return
	}
	s.terminating = true
	s.dispatchable = false
	s.pending = nil
	s.mu.Unlock()
	s.cancel()
}
func (s *poolSource) stop(err error) {
	s.mu.Lock()
	s.err = errors.Join(s.err, err)
	s.pending = nil
	s.mu.Unlock()
	s.cancel()
}
func (s *poolSource) outcome() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failedAttempts != 0 || s.erroredAttempts != 0 {
		return errors.Join(s.err, fmt.Errorf("pool execution: passed attempts: %d; failed attempts: %d; errored attempts: %d", s.passedAttempts, s.failedAttempts, s.erroredAttempts))
	}
	return s.err
}

func reportedTestKey(test runner.ReportedTest) string {
	if test.TestCase.Identifier != "" {
		return test.TestCase.Identifier
	}
	if test.TestCase.Path != "" {
		return test.TestCase.Path
	}
	if test.TestCase.Scope != "" || test.TestCase.Name != "" {
		return strings.Join([]string{test.Selector, test.TestCase.Scope, test.TestCase.Name}, "\x00")
	}
	return test.Location
}

func (s *poolSource) recordSettled(results *poolResults) {
	tests, errored := results.settled()
	for _, test := range tests {
		s.reportedTests[reportedTestKey(test.test)] = test
	}
	for _, attempt := range errored {
		s.erroredWork[attempt.ID] = attempt
	}
}

func (s *poolSource) summary() ([]poolReportedTest, []api.LeaseAttempt) {
	s.mu.Lock()
	defer s.mu.Unlock()
	failed := make([]poolReportedTest, 0, len(s.reportedTests))
	for _, test := range s.reportedTests {
		if test.test.Status == runner.TestStatusFailed {
			failed = append(failed, test)
		}
	}
	errored := make([]api.LeaseAttempt, 0, len(s.erroredWork))
	for _, attempt := range s.erroredWork {
		errored = append(errored, attempt)
	}
	slices.SortFunc(failed, func(a, b poolReportedTest) int {
		if aExample, bExample := a.entry.Format == plan.TestCaseFormatExample, b.entry.Format == plan.TestCaseFormatExample; aExample != bExample {
			if aExample {
				return 1
			}
			return -1
		}
		if order := strings.Compare(attemptSelector(a.entry), attemptSelector(b.entry)); order != 0 {
			return order
		}
		return strings.Compare(reportedTestKey(a.test), reportedTestKey(b.test))
	})
	slices.SortFunc(errored, func(a, b api.LeaseAttempt) int { return strings.Compare(a.ID, b.ID) })
	return failed, errored
}

func (s *poolSource) work(ctx context.Context, client poolScheduler, pool api.Pool, retries int) {
	if pool.State == "consumed" {
		s.mu.Lock()
		s.reason = "pool_consumed"
		s.mu.Unlock()
		return
	}
	var next *poolLease
	defer func() {
		if next != nil {
			s.releaseUnused(client, pool.ID, next, "worker stopped")
		}
	}()
	for ctx.Err() == nil {
		lease := next
		next = nil
		if lease != nil {
			s.mu.Lock()
			lost := !lease.owned || !time.Now().Before(lease.expires)
			s.mu.Unlock()
			if lost {
				lease.stop()
				debug.Printf("Prefetched lease %s was lost before dispatch; requesting another", lease.ID)
				lease = nil
			} else {
				debug.Printf("Using prefetched lease %s (%d scheduler attempts, state=%s)", lease.ID, len(lease.Attempts), lease.state)
			}
		}
		if lease == nil {
			// Spread simultaneous workers' lease starts (and thus similarly sized
			// batches' finishes) without holding a completed lease for a random delay.
			// #nosec G404 -- lease jitter is not used for security.
			timer := time.NewTimer(time.Duration(rand.IntN(250)) * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			debug.Printf("Requesting lease")
			start := time.Now()
			response, err := client.AcquireLease(ctx, pool.ID)
			if err != nil {
				s.stop(err)
				return
			}
			s.lastAcquireDuration = time.Since(start)
			if response.Lease == nil {
				debug.Printf("Returned no lease (state=%s)", response.Pool.State)
				switch response.Pool.State {
				case "consumed":
					s.mu.Lock()
					s.reason = "pool_consumed"
					s.mu.Unlock()
					return
				case "planning", "populating", "consuming":
					// Keep one idle worker below a 10-empty-leases/minute job quota.
					// Other workers may share that job's quota; 429s use the server reset.
					timer := time.NewTimer(7 * time.Second)
					select {
					case <-ctx.Done():
						timer.Stop()
						return
					case <-timer.C:
					}
					continue
				default:
					s.stop(fmt.Errorf("pool cannot dispatch in state %q", response.Pool.State))
					return
				}
			}
			lease = s.track(client, pool.ID, *response.Lease, response.Pool.State)
			debug.Printf("Acquired lease %s (%d scheduler attempts, state=%s)", lease.ID, len(lease.Attempts), lease.state)
		}
		var err error
		if next, err = s.executeLease(ctx, client, pool, lease, retries); err != nil {
			s.stop(err)
			return
		}
	}
}

// track heartbeats a lease from acquisition until its stop function returns.
// Runner cancellation stops dispatch, not ownership: keep heartbeating until
// final accounting returns so a timed-out or exited runner still has time to
// complete dispatched work or release undispatched work before lease expiry.
// Heartbeat requests themselves are bounded by the current expiry.
func (s *poolSource) track(client poolScheduler, poolID string, lease api.Lease, state string) *poolLease {
	l := &poolLease{Lease: lease, state: state, owned: true, expires: lease.ExpiresAt}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); s.heartbeat(ctx, client, poolID, l) }()
	l.stop = func() { cancel(); <-done }
	return l
}

// releaseUnused returns an undispatched prefetched lease to the pool. Failure
// only delays its attempts until the lease expires, so it does not fail the job.
func (s *poolSource) releaseUnused(client poolScheduler, poolID string, lease *poolLease, why string) {
	lease.stop()
	s.mu.Lock()
	owned, expires := lease.owned && time.Now().Before(lease.expires), lease.expires
	s.mu.Unlock()
	if !owned {
		return
	}
	deadline := time.Now().Add(90 * time.Second)
	if expires.Before(deadline) {
		deadline = expires
	}
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	err := client.ReleaseLease(ctx, poolID, lease.ID)
	debug.Printf("Released prefetched lease %s (%s; error=%v)", lease.ID, why, err)
}

const (
	// prefetchMinLead is the minimum requestLead in leasePrefetch.schedule.
	prefetchMinLead = 2 * time.Second
	// prefetchMaxIdle bounds how long a prefetched lease can wait behind an
	// overrunning current lease, so idle workers can take its attempts and the
	// wait does not consume much of its maximum lifetime.
	prefetchMaxIdle = 30 * time.Second
)

// leaseEstimate sums attempt p90 costs; zero means no duration estimate.
func leaseEstimate(attempts []api.LeaseAttempt) time.Duration {
	var total int64
	for _, a := range attempts {
		total += a.Costs.DurationP90MS
	}
	return time.Duration(total) * time.Millisecond
}

// leasePrefetch acquires at most one next lease while the current lease runs.
// Its methods run on the worker goroutine; only the acquisition is concurrent.
type leasePrefetch struct {
	due       *time.Timer
	acquiring chan prefetchResult
	next      *poolLease
	idle      *time.Timer
}

type prefetchResult struct {
	lease   *poolLease
	state   string
	latency time.Duration
	err     error
}

func timerC(t *time.Timer) <-chan time.Time {
	if t == nil {
		return nil
	}
	return t.C
}

// schedule starts the prefetch timer when round 0 of the current lease is
// dispatched. The timer fires after:
//
//	delay            = predictedRuntime - requestLead
//	predictedRuntime = p90Total × pace
//	requestLead      = max(prefetchMinLead, 2 × last lease request duration)
//
// where:
//   - p90Total sums the attempts' duration_p90_ms. A sum of p90s overestimates
//     a typical lease.
//   - pace corrects that: the ratio of actual runtime to p90Total on this
//     worker's earlier leases, or 1 before any are observed.
//   - requestLead is a padded estimate of how long the lease request takes, so
//     the next lease arrives just before the current lease finishes.
//
// A delay of zero or less prefetches immediately. Without costs there is no
// prefetch; the next lease is requested after accounting.
func (p *leasePrefetch) schedule(s *poolSource, lease *poolLease, started time.Time) {
	p90Total := leaseEstimate(lease.Attempts)
	if p90Total <= 0 {
		return
	}
	pace := s.pace
	if pace == 0 {
		pace = 1
	}
	predictedRuntime := time.Duration(float64(p90Total) * pace)
	requestLead := max(prefetchMinLead, 2*s.lastAcquireDuration)
	// started is the dispatch time; this runs slightly after it.
	delay := max(predictedRuntime-requestLead-time.Since(started), 0)
	p.due = time.NewTimer(delay)
	debug.Printf("Scheduled next lease prefetch in %s (p90 total=%s; pace=%.2f; predicted runtime=%s; request lead=%s)",
		delay.Round(time.Millisecond), p90Total, pace, predictedRuntime.Round(time.Millisecond), requestLead.Round(time.Millisecond))
}

func (p *leasePrefetch) start(ctx context.Context, s *poolSource, client poolScheduler, poolID string) {
	p.due = nil
	p.acquiring = make(chan prefetchResult, 1)
	go func(out chan<- prefetchResult) {
		debug.Printf("Prefetching next lease")
		start := time.Now()
		response, err := client.AcquireLease(ctx, poolID)
		r := prefetchResult{latency: time.Since(start), err: err, state: response.Pool.State}
		if err == nil && response.Lease != nil {
			r.lease = s.track(client, poolID, *response.Lease, response.Pool.State)
		}
		out <- r
	}(p.acquiring)
}

func (p *leasePrefetch) receive(s *poolSource, r prefetchResult) {
	p.acquiring = nil
	switch {
	case r.err != nil:
		// The ordinary request after accounting reports persistent failures.
		debug.Printf("Lease prefetch failed: %v", r.err)
	case r.lease == nil:
		s.lastAcquireDuration = r.latency
		debug.Printf("Lease prefetch returned no lease (state=%s)", r.state)
	default:
		s.lastAcquireDuration = r.latency
		p.next = r.lease
		p.idle = time.NewTimer(prefetchMaxIdle)
		debug.Printf("Prefetched lease %s (%d scheduler attempts, state=%s)", r.lease.ID, len(r.lease.Attempts), r.state)
	}
}

// finish never abandons an in-flight acquisition, which could strand a lease
// until its TTL. It hands on the next lease only after a clean finish.
func (p *leasePrefetch) finish(s *poolSource, client poolScheduler, poolID string, clean bool) *poolLease {
	if p.due != nil {
		p.due.Stop()
	}
	if p.acquiring != nil {
		p.receive(s, <-p.acquiring)
	}
	if p.idle != nil {
		p.idle.Stop()
	}
	if p.next != nil && !clean {
		s.releaseUnused(client, poolID, p.next, "current lease stopped")
		p.next = nil
	}
	return p.next
}

func validateLease(lease api.Lease) error {
	if lease.ID == "" || !lease.ExpiresAt.After(time.Now()) || len(lease.Attempts) == 0 {
		return errors.New("invalid lease identity, expiry or attempts")
	}
	ids := map[string]bool{}
	selectors := map[string]bool{}
	for _, a := range lease.Attempts {
		c := a.Selector
		target := c.Path
		switch c.Format {
		case plan.TestCaseFormatFile:
		case plan.TestCaseFormatExample:
			if c.Identifier != "" {
				target = c.Identifier
			}
		case plan.TestCaseFormatSelector:
			target = c.Value
		default:
			return errors.New("unsupported selector format")
		}
		if a.ID == "" || ids[a.ID] || a.SelectorType != "test_plan_test_case_v1" || target == "" || selectors[target] {
			return errors.New("invalid or duplicate lease selector/attempt")
		}
		ids[a.ID] = true
		selectors[target] = true
	}
	return nil
}

// executeLease runs and accounts one lease. It may prefetch the next lease,
// which it returns, still heartbeating and undispatched, after a clean finish.
func (s *poolSource) executeLease(ctx context.Context, client poolScheduler, pool api.Pool, lease *poolLease, retries int) (next *poolLease, err error) {
	s.mu.Lock()
	s.current = lease
	s.dispatched = false
	s.undispatchable = false
	s.mu.Unlock()
	prefetch := &leasePrefetch{}
	// Deferred first so it runs after the current lease is accounted.
	defer func() { next = prefetch.finish(s, client, pool.ID, err == nil) }()
	defer lease.stop()
	results := newPoolResults(lease.Attempts, pool.MutedTests)
	defer func() {
		s.mu.Lock()
		owned := lease.owned && time.Now().Before(lease.expires)
		expires := lease.expires
		dispatched := s.dispatched
		terminating := s.terminating
		undispatchable := s.undispatchable
		s.dispatchable = false
		s.pending = nil
		s.mu.Unlock()
		if !owned {
			err = errors.Join(err, errors.New("lease ownership lost before accounting"))
			return
		}
		// Do not inherit runner cancellation: Scheduler still needs a final
		// accounting decision while we own the lease. Never account after expiry.
		accountingDeadline := time.Now().Add(90 * time.Second)
		if expires.Before(accountingDeadline) {
			accountingDeadline = expires
		}
		accounting, cancel := context.WithDeadline(context.Background(), accountingDeadline)
		defer cancel()
		if terminating || (!dispatched && !undispatchable) {
			err = errors.Join(err, client.ReleaseLease(accounting, pool.ID, lease.ID))
			debug.Printf("Released lease %s (dispatched=%t; terminating=%t; error=%v)", lease.ID, dispatched, terminating, err)
			return
		}
		final := results.final()
		if e := client.CompleteLease(accounting, pool.ID, lease.ID, final); e != nil {
			err = errors.Join(err, e)
			return
		}
		passed, failed, errored := 0, 0, 0
		for _, r := range final {
			switch r.Result {
			case "passed":
				passed++
			case "failed":
				failed++
			case "errored":
				errored++
			}
		}
		s.mu.Lock()
		s.passedAttempts += passed
		s.failedAttempts += failed
		s.erroredAttempts += errored
		s.recordSettled(results)
		s.mu.Unlock()
		debug.Printf("Completed lease %s (scheduler attempts=%d; final: passed=%d failed=%d errored=%d)", lease.ID, len(final), passed, failed, errored)
	}()
	if err = validateLease(lease.Lease); err != nil {
		for i := range results.broken {
			results.broken[i] = true
		}
		s.mu.Lock()
		s.undispatchable = true
		s.mu.Unlock()
		return nil, err
	}
	if lease.state != "consuming" && lease.state != "populating" {
		return nil, fmt.Errorf("pool cannot dispatch in state %q", lease.state)
	}
	tests := make([]plan.TestCase, len(lease.Attempts))
	owners := make([]int, len(tests))
	for i, a := range lease.Attempts {
		tests[i] = a.Selector
		owners[i] = i
	}
	var started time.Time
	for round := 0; len(tests) > 0; round++ {
		s.mu.Lock()
		s.sequence++
		s.retryRound = round
		s.dispatchable = true
		s.pending = &runnerexec.Batch{ID: fmt.Sprintf("b_%d", s.sequence), Tests: tests}
		batchID := s.pending.ID
		s.mu.Unlock()
		if round > 0 {
			debug.Printf("Offered local retry batch %s from lease %s (round=%d; %d tests)", batchID, lease.ID, round, len(tests))
		} else {
			debug.Printf("Offered batch %s from lease %s (%d tests)", batchID, lease.ID, len(tests))
		}
		var result runnerexec.Result
	wait:
		for {
			select {
			case result = <-s.results:
				break wait
			case <-ctx.Done():
				for _, i := range owners {
					results.broken[i] = true
				}
				return nil, ctx.Err()
			case start := <-s.starts:
				if start.id == batchID {
					started = start.at
					prefetch.schedule(s, lease, started)
				}
			case <-timerC(prefetch.due):
				prefetch.start(ctx, s, client, pool.ID)
			case r := <-prefetch.acquiring:
				prefetch.receive(s, r)
			case <-timerC(prefetch.idle):
				prefetch.idle = nil
				s.releaseUnused(client, pool.ID, prefetch.next, fmt.Sprintf("current lease still running after %s", prefetchMaxIdle))
				prefetch.next = nil
			}
		}
		if round == 0 {
			if started.IsZero() {
				select {
				case start := <-s.starts:
					if start.id == batchID {
						started = start.at
					}
				default:
				}
			}
			// pace = average of the previous pace and this lease's actual
			// runtime / p90 total: recent leases weigh most, and one unusual
			// lease moves it only halfway.
			if p90Total := leaseEstimate(lease.Attempts); p90Total > 0 && !started.IsZero() && result.Status == "completed" {
				observed := float64(time.Since(started)) / float64(p90Total)
				if s.pace == 0 {
					s.pace = observed
				} else {
					s.pace = (s.pace + observed) / 2
				}
			}
		}
		results.absorb(tests, owners, result)
		if debug.Enabled {
			current := results.final()
			seen := make(map[int]bool, len(owners))
			passed, failed, errored := 0, 0, 0
			for _, i := range owners {
				if seen[i] {
					continue
				}
				seen[i] = true
				switch current[i].Result {
				case "passed":
					passed++
				case "failed":
					failed++
				case "errored":
					errored++
				}
			}
			debug.Printf("Received batch %s result (status=%s; provisional attempt results: passed=%d failed=%d errored=%d)", batchID, result.Status, passed, failed, errored)
		}
		if round >= retries {
			break
		}
		tests, owners = results.retries()
	}
	return nil, nil
}

func (s *poolSource) heartbeat(ctx context.Context, client poolScheduler, poolID string, lease *poolLease) {
	for {
		s.mu.Lock()
		expires := lease.expires
		s.mu.Unlock()
		wait := time.Until(expires) / 3
		if wait > time.Minute {
			wait = time.Minute
		}
		if wait <= 0 {
			s.lose(lease, errors.New("lease expired"))
			return
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		request, cancel := context.WithDeadline(ctx, expires)
		next, err := client.HeartbeatLease(request, poolID, lease.ID)
		cancel()
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			var leaseError *api.LeaseHTTPError
			// Only a Scheduler 4xx confirms ownership loss. After the client's
			// retry budget, keep the lease until its last known expiry.
			if !errors.As(err, &leaseError) || leaseError.Status >= 500 || leaseError.Status == 429 {
				debug.Printf("Lease %s heartbeat failed; retaining ownership until %s: %v", lease.ID, expires.Format(time.RFC3339), err)
				continue
			}
			if leaseError.Status == 422 && leaseError.Code == api.LeaseErrorCodeMaximumLifetime {
				debug.Printf("Lease %s reached its maximum lifetime; retaining ownership until %s", lease.ID, expires.Format(time.RFC3339))
				timer := time.NewTimer(time.Until(expires))
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
				s.lose(lease, errors.New("lease expired"))
				return
			}
			s.lose(lease, fmt.Errorf("lease heartbeat: %w", err))
			return
		}
		s.mu.Lock()
		lease.expires = next
		s.mu.Unlock()
		debug.Printf("Renewed lease %s (expires=%s)", lease.ID, next.Format(time.RFC3339))
	}
}

// lose records confirmed ownership loss. Losing the lease being executed stops
// the worker; an undispatched prefetched lease is only dropped.
func (s *poolSource) lose(lease *poolLease, err error) {
	s.mu.Lock()
	lease.owned = false
	active := s.current == lease
	s.mu.Unlock()
	if active {
		s.stop(err)
		return
	}
	debug.Printf("Prefetched lease %s lost before dispatch: %v", lease.ID, err)
}
