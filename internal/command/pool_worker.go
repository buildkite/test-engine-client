package command

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/buildkite/test-engine-client/v3/internal/api"
	"github.com/buildkite/test-engine-client/v3/internal/debug"
	"github.com/buildkite/test-engine-client/v3/internal/plan"
	"github.com/buildkite/test-engine-client/v3/internal/runnerexec"
)

type poolScheduler interface {
	AcquireLease(context.Context, string) (api.LeaseResponse, error)
	HeartbeatLease(context.Context, string, string) (time.Time, error)
	CompleteLease(context.Context, string, string, []api.AttemptResult) error
	ReleaseLease(context.Context, string, string) error
	GetPool(context.Context, string) (api.Pool, error)
}

// All protocol callbacks are nonblocking; network and report work runs outside
// the host lock. One worker holds one lease, including its local retries.
type poolSource struct {
	mu              sync.Mutex
	pending         *runnerexec.Batch
	dispatched      bool
	dispatchable    bool
	expires         time.Time
	owned           bool
	reason          string
	err             error
	passedAttempts  int
	failedAttempts  int
	erroredAttempts int
	sequence        int
	retryRound      int
	results         chan runnerexec.Result
	cancel          context.CancelFunc
}

func newPoolSource(cancel context.CancelFunc) *poolSource {
	return &poolSource{results: make(chan runnerexec.Result, 1), cancel: cancel}
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
	if !s.dispatchable || !s.owned || !time.Now().Before(s.expires) {
		return errors.New("lease no longer owned at dispatch")
	}
	s.dispatchable = false
	s.dispatched = true
	if s.retryRound > 0 {
		debug.Printf("Dispatched local retry batch %s to persistent runner (round=%d)", batch.ID, s.retryRound)
	} else {
		debug.Printf("Dispatched pool batch %s to persistent runner", batch.ID)
	}
	return nil
}
func (s *poolSource) Accepted(_ runnerexec.Batch, result runnerexec.Result) { s.results <- result }
func (s *poolSource) Unresolved(_ runnerexec.Batch, err error)              { s.stop(err) }
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

func (s *poolSource) work(ctx context.Context, client poolScheduler, pool api.Pool, retries int) {
	if pool.State == "consumed" {
		s.mu.Lock()
		s.reason = "pool_consumed"
		s.mu.Unlock()
		return
	}
	for ctx.Err() == nil {
		// Spread simultaneous workers' lease starts (and thus similarly sized
		// batches' finishes) without holding a completed lease for a random delay.
		timer := time.NewTimer(time.Duration(rand.IntN(250)) * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		debug.Printf("Requesting lease")
		response, err := client.AcquireLease(ctx, pool.ID)
		if err != nil {
			s.stop(err)
			return
		}
		if response.Lease != nil {
			debug.Printf("Acquired lease %s (%d scheduler attempts, state=%s)", response.Lease.ID, len(response.Lease.Attempts), response.Pool.State)
			if err := s.executeLease(ctx, client, pool, *response.Lease, retries, response.Pool.State); err != nil {
				s.stop(err)
				return
			}
			continue
		}
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
		default:
			s.stop(fmt.Errorf("pool cannot dispatch in state %q", response.Pool.State))
			return
		}
	}
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

func (s *poolSource) executeLease(ctx context.Context, client poolScheduler, pool api.Pool, lease api.Lease, retries int, state string) (err error) {
	s.mu.Lock()
	s.owned = true
	s.expires = lease.ExpiresAt
	s.dispatched = false
	s.mu.Unlock()
	// Runner cancellation stops dispatch, not ownership. Keep heartbeating until
	// final accounting returns so a timed-out or exited runner still has time to
	// complete dispatched work or release undispatched work before lease expiry.
	// Heartbeat requests themselves are bounded by the current expiry.
	hbCtx, hbCancel := context.WithCancel(context.Background())
	hbDone := make(chan struct{})
	go func() { defer close(hbDone); s.heartbeat(hbCtx, client, pool.ID, lease.ID) }()
	defer func() { hbCancel(); <-hbDone }()
	results := newPoolResults(lease.Attempts, pool.MutedTests)
	defer func() {
		s.mu.Lock()
		owned := s.owned && time.Now().Before(s.expires)
		dispatched := s.dispatched
		s.dispatchable = false
		s.pending = nil
		s.mu.Unlock()
		if !owned {
			err = errors.Join(err, errors.New("lease ownership lost before accounting"))
			return
		}
		// Do not inherit runner cancellation: Scheduler still needs a final
		// accounting decision while we own the lease. This deadline bounds it.
		accounting, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		if !dispatched {
			err = errors.Join(err, client.ReleaseLease(accounting, pool.ID, lease.ID))
			debug.Printf("Released undispatched lease %s (error=%v)", lease.ID, err)
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
		s.mu.Unlock()
		debug.Printf("Completed lease %s (scheduler attempts=%d; final: passed=%d failed=%d errored=%d)", lease.ID, len(final), passed, failed, errored)
	}()
	if err = validateLease(lease); err != nil {
		return err
	}
	if state != "consuming" && state != "populating" {
		return fmt.Errorf("pool cannot dispatch in state %q", state)
	}
	tests := make([]plan.TestCase, len(lease.Attempts))
	owners := make([]int, len(tests))
	for i, a := range lease.Attempts {
		tests[i] = a.Selector
		owners[i] = i
	}
	for round := 0; len(tests) > 0; round++ {
		// Catch a pool error transition before every initial/retry dispatch.
		current, e := client.GetPool(ctx, pool.ID)
		if e != nil || current.State == "errored" || current.State == "consumed" {
			if e == nil {
				e = fmt.Errorf("pool became %s with active work", current.State)
			}
			for _, i := range owners {
				results.broken[i] = true
			}
			return e
		}
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
		select {
		case result := <-s.results:
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
		case <-ctx.Done():
			for _, i := range owners {
				results.broken[i] = true
			}
			return ctx.Err()
		}
		if round >= retries {
			break
		}
		tests, owners = results.retries()
	}
	return nil
}

func (s *poolSource) heartbeat(ctx context.Context, client poolScheduler, poolID, leaseID string) {
	for {
		s.mu.Lock()
		expires := s.expires
		s.mu.Unlock()
		wait := time.Until(expires) / 3
		if wait > time.Minute {
			wait = time.Minute
		}
		if wait <= 0 {
			s.mu.Lock()
			s.owned = false
			s.mu.Unlock()
			s.stop(errors.New("lease expired"))
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
		next, err := client.HeartbeatLease(request, poolID, leaseID)
		cancel()
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			s.mu.Lock()
			s.owned = false
			s.mu.Unlock()
			s.stop(fmt.Errorf("lease heartbeat: %w", err))
			return
		}
		s.mu.Lock()
		s.expires = next
		s.mu.Unlock()
		debug.Printf("Renewed lease %s (expires=%s)", leaseID, next.Format(time.RFC3339))
	}
}
