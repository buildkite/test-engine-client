package command

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/buildkite/test-engine-client/v3/internal/api"
	"github.com/buildkite/test-engine-client/v3/internal/plan"
	"github.com/buildkite/test-engine-client/v3/internal/runnerexec"
)

type fakePoolScheduler struct {
	acquire   func(context.Context) (api.LeaseResponse, error)
	heartbeat func(context.Context, string) (time.Time, error)
	complete  func(context.Context, []api.AttemptResult) error
	release   func() error
	mu        sync.Mutex
	calls     []string
}

func (f *fakePoolScheduler) record(call string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
}

func (f *fakePoolScheduler) recorded() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func (f *fakePoolScheduler) AcquireLease(ctx context.Context, _ string) (api.LeaseResponse, error) {
	var r api.LeaseResponse
	var err error
	if f.acquire != nil {
		r, err = f.acquire(ctx)
	} else {
		// executeLease prefetches after the final result; report nothing left.
		r.Pool.State = "consumed"
	}
	if r.Lease != nil {
		f.record("acquire " + r.Lease.ID)
	} else {
		f.record("acquire none")
	}
	return r, err
}
func (f *fakePoolScheduler) HeartbeatLease(ctx context.Context, _, leaseID string) (time.Time, error) {
	f.record("heartbeat " + leaseID)
	if f.heartbeat != nil {
		return f.heartbeat(ctx, leaseID)
	}
	return time.Now().Add(time.Minute), nil
}
func (f *fakePoolScheduler) CompleteLease(ctx context.Context, _, leaseID string, r []api.AttemptResult) error {
	f.record("complete " + leaseID)
	return f.complete(ctx, r)
}
func (f *fakePoolScheduler) ReleaseLease(_ context.Context, _, leaseID string) error {
	f.record("release " + leaseID)
	return f.release()
}

func testLease() api.Lease {
	return api.Lease{ID: "lease", ExpiresAt: time.Now().Add(time.Minute), Attempts: []api.LeaseAttempt{{ID: "original", SelectorType: "test_plan_test_case_v1", Selector: plan.TestCase{Format: "file", Path: "a"}}}}
}
func waitPoolBatch(t *testing.T, s *poolSource) *runnerexec.Batch {
	t.Helper()
	timeout := time.After(3 * time.Second)
	for {
		batch, _, err := s.Next()
		if err != nil {
			t.Fatal(err)
		}
		if batch != nil {
			return batch
		}
		select {
		case <-timeout:
			t.Fatal("no batch")
		case <-time.After(time.Millisecond):
		}
	}
}

func TestPoolWorkerRetriesBeforeNextLeaseAndKeepsHeartbeatUntilComplete(t *testing.T) {
	var logs bytes.Buffer
	setDebugEnabled(t, &logs)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := newPoolSource(cancel)
	lease := testLease()
	var calls atomic.Int32
	var completed, final atomic.Bool
	heartbeat := make(chan struct{}, 1)
	f := &fakePoolScheduler{
		acquire: func(context.Context) (api.LeaseResponse, error) {
			n := calls.Add(1)
			r := api.LeaseResponse{}
			r.Pool.State = "consuming"
			if n == 1 {
				lease.ExpiresAt = time.Now().Add(120 * time.Millisecond)
				r.Lease = &lease
			} else {
				// The next lease may be requested alongside completion, but not
				// before the local retry round has finished.
				if !final.Load() {
					t.Error("requested next lease before the final retry result")
				}
				r.Pool.State = "consumed"
			}
			return r, nil
		},
		heartbeat: func(context.Context, string) (time.Time, error) {
			select {
			case heartbeat <- struct{}{}:
			default:
			}
			return time.Now().Add(time.Minute), nil
		},
		complete: func(ctx context.Context, r []api.AttemptResult) error {
			if len(r) != 1 || r[0].AttemptID != "original" || r[0].Result != "passed" {
				t.Errorf("results=%v", r)
			}
			select {
			case <-heartbeat:
			case <-ctx.Done():
				return ctx.Err()
			}
			completed.Store(true)
			return nil
		},
		release: func() error { t.Error("released dispatched work"); return nil },
	}
	done := make(chan struct{})
	go func() { defer close(done); s.work(ctx, f, api.Pool{ID: "pool"}, 1) }()
	for i := 0; i < 2; i++ {
		batch := waitPoolBatch(t, s)
		if i == 1 && (batch.Tests[0].Format != "example" || batch.Tests[0].Identifier != "a[1]") {
			t.Fatal(batch)
		}
		if err := s.Dispatched(*batch); err != nil {
			t.Fatal(err)
		}
		status := "failed"
		if i == 1 {
			status = "passed"
			final.Store(true)
		}
		s.Accepted(*batch, poolReport(`[{"id":"a[1]","file_path":"a","status":"`+status+`"}]`, 1, 0))
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("worker stuck")
	}
	if err := s.outcome(); err != nil {
		t.Fatal(err)
	}
	// The lease, the prefetch alongside completion, then the ordinary request
	// that confirms the pool is consumed.
	if !completed.Load() || calls.Load() != 3 {
		t.Fatalf("completed=%v calls=%d", completed.Load(), calls.Load())
	}
	if !strings.Contains(logs.String(), "Received batch b_1 result (status=completed; provisional attempt results: passed=0 failed=1 errored=0)") ||
		!strings.Contains(logs.String(), "Received batch b_2 result (status=completed; provisional attempt results: passed=1 failed=0 errored=0)") {
		t.Fatalf("retry attempt summaries missing: %s", logs.String())
	}
	if !strings.Contains(logs.String(), "Offered local retry batch b_2 from lease lease (round=1; 1 tests)") ||
		!strings.Contains(logs.String(), "Dispatched local retry batch b_2 to persistent runner (round=1)") ||
		!strings.Contains(logs.String(), "Completed lease lease (scheduler attempts=1; final: passed=1 failed=0 errored=0)") {
		t.Fatalf("retry and final lease result unclear: %s", logs.String())
	}
}

func TestPoolWorkerOutcomeDistinguishesFailedAndErroredAttempts(t *testing.T) {
	var logs bytes.Buffer
	setDebugEnabled(t, &logs)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := newPoolSource(cancel)
	lease := testLease()
	lease.Attempts = append(lease.Attempts, api.LeaseAttempt{
		ID: "missing", SelectorType: "test_plan_test_case_v1",
		Selector: plan.TestCase{Format: "example", Identifier: "b[1]", Path: "b"},
	}, api.LeaseAttempt{
		ID: "passing", SelectorType: "test_plan_test_case_v1",
		Selector: plan.TestCase{Format: "file", Path: "c"},
	})
	completed := false
	f := &fakePoolScheduler{
		complete: func(_ context.Context, results []api.AttemptResult) error {
			if len(results) != 3 || results[0].Result != "failed" || results[1].Result != "errored" || results[2].Result != "passed" {
				t.Errorf("results=%v", results)
			}
			completed = true
			return nil
		},
		release: func() error { t.Error("dispatched lease released"); return nil },
	}
	done := make(chan error, 1)
	go func() {
		_, err := s.executeLease(ctx, f, api.Pool{ID: "pool"}, s.track(f, "pool", lease, "consuming"), 0)
		done <- err
	}()
	batch := waitPoolBatch(t, s)
	if err := s.Dispatched(*batch); err != nil {
		t.Fatal(err)
	}
	s.Accepted(*batch, poolReport(`[{"id":"a[1]","file_path":"a","status":"failed"},{"id":"c[1]","file_path":"c","status":"passed"}]`, 2, 0))
	if err := <-done; err != nil || !completed {
		t.Fatalf("accounting error=%v completed=%v", err, completed)
	}
	if got := s.outcome(); got == nil || got.Error() != "pool execution: passed attempts: 1; failed attempts: 1; errored attempts: 1" {
		t.Fatalf("outcome=%v", got)
	}
	if !strings.Contains(logs.String(), "Received batch b_1 result (status=completed; provisional attempt results: passed=1 failed=1 errored=1)") {
		t.Fatalf("missing batch attempt summary: %s", logs.String())
	}
	if !strings.Contains(logs.String(), "Completed lease lease (scheduler attempts=3; final: passed=1 failed=1 errored=1)") {
		t.Fatalf("missing final lease result: %s", logs.String())
	}
}

func TestPoolWorkerRejectsOverlappingFileAndExampleAttempts(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := newPoolSource(cancel)
	lease := api.Lease{
		ID:        "lease",
		ExpiresAt: time.Now().Add(time.Minute),
		Attempts: []api.LeaseAttempt{
			{
				ID: "file", SelectorType: "test_plan_test_case_v1",
				Selector: plan.TestCase{Format: "file", Path: "spec/file_spec.rb"},
			},
			{
				ID: "example", SelectorType: "test_plan_test_case_v1",
				Selector: plan.TestCase{Format: "example", Identifier: "./spec/file_spec.rb[1:1]", Path: "./spec/file_spec.rb[1:1]", Scope: "File", Name: "passes"},
			},
		},
	}
	completed := false
	f := &fakePoolScheduler{
		complete: func(_ context.Context, results []api.AttemptResult) error {
			want := []api.AttemptResult{{AttemptID: "file", Result: "errored"}, {AttemptID: "example", Result: "errored"}}
			if len(results) != len(want) || results[0] != want[0] || results[1] != want[1] {
				t.Errorf("results=%v, want %v", results, want)
			}
			completed = true
			return nil
		},
		release: func() error { t.Error("dispatched lease released"); return nil },
	}
	done := make(chan error, 1)
	go func() {
		_, err := s.executeLease(ctx, f, api.Pool{ID: "pool"}, s.track(f, "pool", lease, "consuming"), 0)
		done <- err
	}()
	batch := waitPoolBatch(t, s)
	if len(batch.Tests) != 2 || batch.Tests[0].Format != "file" || batch.Tests[1].Format != "example" {
		t.Fatalf("batch tests=%v", batch.Tests)
	}
	if err := s.Dispatched(*batch); err != nil {
		t.Fatal(err)
	}
	s.Accepted(*batch, poolReport(`[
{"id":"./spec/file_spec.rb[1:1]","file_path":"./spec/file_spec.rb","status":"passed","description":"passes","full_description":"File passes"},
{"id":"./spec/file_spec.rb[1:2]","file_path":"./spec/file_spec.rb","status":"failed","description":"fails","full_description":"File fails"}
]`, 2, 0))
	if err := <-done; err != nil || !completed {
		t.Fatalf("accounting error=%v completed=%v", err, completed)
	}
}

func TestPoolWorkerReleaseVersusConservativeCompletion(t *testing.T) {
	for _, mode := range []string{"undispatched", "dispatched", "terminating", "failure before termination", "invalid attempt", "unsupported selector", "pool errored"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			s := newPoolSource(cancel)
			lease := testLease()
			if mode == "invalid attempt" {
				lease.Attempts[0].SelectorType = "custom"
			}
			if mode == "unsupported selector" {
				lease.Attempts[0].Selector.Format = "unknown"
			}
			releases, completes := 0, 0
			f := &fakePoolScheduler{release: func() error { releases++; return nil }, complete: func(_ context.Context, r []api.AttemptResult) error {
				completes++
				if r[0].Result != "errored" {
					t.Error(r)
				}
				return nil
			}}
			state := "consuming"
			if mode == "pool errored" {
				state = "errored"
			}
			done := make(chan error, 1)
			go func() {
				_, err := s.executeLease(ctx, f, api.Pool{ID: "pool"}, s.track(f, "pool", lease, state), 0)
				done <- err
			}()
			if mode == "undispatched" || mode == "dispatched" || mode == "terminating" || mode == "failure before termination" {
				batch := waitPoolBatch(t, s)
				if mode == "dispatched" || mode == "terminating" || mode == "failure before termination" {
					if err := s.Dispatched(*batch); err != nil {
						t.Fatal(err)
					}
				}
				switch mode {
				case "terminating":
					s.Terminating()
				case "failure before termination":
					s.mu.Lock()
					s.err = errors.New("batch timeout")
					s.mu.Unlock()
					s.Terminating()
					cancel()
				default:
					cancel()
				}
			}
			select {
			case err := <-done:
				if err == nil {
					t.Error("missing failure")
				}
			case <-time.After(time.Second):
				t.Fatal("stuck")
			}
			if mode == "undispatched" {
				if err := s.Dispatched(runnerexec.Batch{}); err == nil {
					t.Fatal("host dispatched an offered batch after release")
				}
			}
			if mode == "dispatched" || mode == "failure before termination" || mode == "invalid attempt" || mode == "unsupported selector" {
				if completes != 1 || releases != 0 {
					t.Fatal(completes, releases)
				}
			} else if releases != 1 || completes != 0 {
				t.Fatal(completes, releases)
			}
		})
	}
}

func TestPoolHeartbeatInterval(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		s := newPoolSource(cancel)
		lease := &poolLease{owned: true, expires: time.Now().Add(10 * time.Minute)}
		s.current = lease
		var calls atomic.Int32
		f := &fakePoolScheduler{heartbeat: func(context.Context, string) (time.Time, error) {
			calls.Add(1)
			return time.Now().Add(10 * time.Minute), nil
		}}
		done := make(chan struct{})
		go func() { defer close(done); s.heartbeat(ctx, f, "pool", lease) }()
		time.Sleep(59 * time.Second)
		synctest.Wait()
		if calls.Load() != 0 {
			t.Fatalf("heartbeats before 60 seconds=%d", calls.Load())
		}
		time.Sleep(time.Second)
		synctest.Wait()
		if calls.Load() != 1 {
			t.Fatalf("heartbeats at 60 seconds=%d, want 1", calls.Load())
		}
		cancel()
		<-done
	})
}

func TestPoolMaximumLifetimeHeartbeatAllowsTerminalAccountingBeforeExpiry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		s := newPoolSource(cancel)
		lease := testLease()
		lease.ExpiresAt = time.Now().Add(2 * time.Minute)
		var heartbeats, completes atomic.Int32
		f := &fakePoolScheduler{
			heartbeat: func(context.Context, string) (time.Time, error) {
				heartbeats.Add(1)
				return time.Time{}, &api.LeaseHTTPError{Status: 422, Code: api.LeaseErrorCodeMaximumLifetime}
			},
			complete: func(ctx context.Context, results []api.AttemptResult) error {
				completes.Add(1)
				deadline, ok := ctx.Deadline()
				if !ok || !deadline.Equal(lease.ExpiresAt) {
					t.Errorf("accounting deadline=%s, want lease expiry %s", deadline, lease.ExpiresAt)
				}
				if len(results) != 1 || results[0] != (api.AttemptResult{AttemptID: "original", Result: "passed"}) {
					t.Errorf("results=%v", results)
				}
				return nil
			},
			release: func() error { t.Error("dispatched lease released"); return nil },
		}
		done := make(chan error, 1)
		go func() {
			_, err := s.executeLease(ctx, f, api.Pool{ID: "pool"}, s.track(f, "pool", lease, "consuming"), 0)
			done <- err
		}()
		synctest.Wait()
		batch, _, err := s.Next()
		if err != nil || batch == nil {
			t.Fatalf("batch=%v err=%v", batch, err)
		}
		if err := s.Dispatched(*batch); err != nil {
			t.Fatal(err)
		}
		time.Sleep(40 * time.Second)
		synctest.Wait()
		if heartbeats.Load() != 1 {
			t.Fatalf("heartbeats=%d, want 1", heartbeats.Load())
		}
		s.Accepted(*batch, poolReport(`[{"id":"a[1]","file_path":"a","status":"passed"}]`, 1, 0))
		synctest.Wait()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if completes.Load() != 1 || heartbeats.Load() != 1 {
			t.Fatalf("completes=%d heartbeats=%d", completes.Load(), heartbeats.Load())
		}
	})
}

func TestPoolMaximumLifetimeHeartbeatCancelsRunningWorkAtExpiry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		s := newPoolSource(cancel)
		lease := testLease()
		lease.ExpiresAt = time.Now().Add(2 * time.Minute)
		var heartbeats, accounted atomic.Int32
		f := &fakePoolScheduler{
			heartbeat: func(context.Context, string) (time.Time, error) {
				heartbeats.Add(1)
				return time.Time{}, &api.LeaseHTTPError{Status: 422, Code: api.LeaseErrorCodeMaximumLifetime}
			},
			complete: func(context.Context, []api.AttemptResult) error { accounted.Add(1); return nil },
			release:  func() error { accounted.Add(1); return nil },
		}
		done := make(chan error, 1)
		go func() {
			_, err := s.executeLease(ctx, f, api.Pool{ID: "pool"}, s.track(f, "pool", lease, "consuming"), 0)
			done <- err
		}()
		synctest.Wait()
		batch, _, err := s.Next()
		if err != nil || batch == nil {
			t.Fatalf("batch=%v err=%v", batch, err)
		}
		if err := s.Dispatched(*batch); err != nil {
			t.Fatal(err)
		}
		time.Sleep(40 * time.Second)
		synctest.Wait()
		if heartbeats.Load() != 1 {
			t.Fatalf("heartbeats=%d, want 1", heartbeats.Load())
		}
		time.Sleep(80 * time.Second)
		synctest.Wait()
		err = <-done
		if err == nil || !strings.Contains(err.Error(), "context canceled") || !strings.Contains(err.Error(), "lease ownership lost before accounting") {
			t.Fatalf("error=%v", err)
		}
		if outcome := s.outcome(); outcome == nil || !strings.Contains(outcome.Error(), "lease expired") {
			t.Fatalf("outcome=%v", outcome)
		}
		if accounted.Load() != 0 || heartbeats.Load() != 1 {
			t.Fatalf("accounted=%d heartbeats=%d", accounted.Load(), heartbeats.Load())
		}
	})
}

func TestPoolHeartbeatLossStopsDispatchAndAccounting(t *testing.T) {
	for _, loss := range []*api.LeaseHTTPError{{Status: 404}, {Status: 409}, {Status: 422, Code: "INVALID_LEASE_TTL"}} {
		t.Run(loss.Error(), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				s := newPoolSource(cancel)
				lease := testLease()
				lease.ExpiresAt = time.Now().Add(10 * time.Minute)
				var accounted atomic.Int32
				f := &fakePoolScheduler{heartbeat: func(context.Context, string) (time.Time, error) {
					return time.Time{}, loss
				}, complete: func(context.Context, []api.AttemptResult) error { accounted.Add(1); return nil }, release: func() error { accounted.Add(1); return nil }}
				done := make(chan error, 1)
				go func() {
					_, err := s.executeLease(ctx, f, api.Pool{ID: "pool"}, s.track(f, "pool", lease, "consuming"), 0)
					done <- err
				}()
				synctest.Wait()
				batch, _, err := s.Next()
				if err != nil || batch == nil {
					t.Fatalf("batch=%v err=%v", batch, err)
				}
				if err := s.Dispatched(*batch); err != nil {
					t.Fatal(err)
				}
				// The first heartbeat is due after one minute; loss is immediate,
				// long before the lease's ten-minute expiry.
				time.Sleep(time.Minute)
				synctest.Wait()
				select {
				case err := <-done:
					if err == nil {
						t.Fatal("missing ownership error")
					}
				default:
					t.Fatal("confirmed ownership loss did not stop the lease")
				}
				if accounted.Load() != 0 || s.outcome() == nil {
					t.Fatal("lost lease was accounted as owned")
				}
				if err := s.Dispatched(*batch); err == nil {
					t.Fatal("dispatch after lease loss")
				}
			})
		})
	}
}

func TestPoolTransientHeartbeatFailuresRetainOwnership(t *testing.T) {
	transient := []error{errors.New("connection reset"), &api.LeaseHTTPError{Status: 503}, &api.LeaseHTTPError{Status: 429}}
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		s := newPoolSource(cancel)
		lease := testLease()
		lease.ExpiresAt = time.Now().Add(2 * time.Minute)
		var heartbeats, completes atomic.Int32
		f := &fakePoolScheduler{
			heartbeat: func(context.Context, string) (time.Time, error) {
				n := int(heartbeats.Add(1))
				if n <= len(transient) {
					return time.Time{}, transient[n-1]
				}
				return time.Now().Add(10 * time.Minute), nil
			},
			complete: func(_ context.Context, results []api.AttemptResult) error {
				completes.Add(1)
				if len(results) != 1 || results[0] != (api.AttemptResult{AttemptID: "original", Result: "passed"}) {
					t.Errorf("results=%v", results)
				}
				return nil
			},
			release: func() error { t.Error("dispatched lease released"); return nil },
		}
		done := make(chan error, 1)
		go func() {
			_, err := s.executeLease(ctx, f, api.Pool{ID: "pool"}, s.track(f, "pool", lease, "consuming"), 0)
			done <- err
		}()
		synctest.Wait()
		batch, _, err := s.Next()
		if err != nil || batch == nil {
			t.Fatalf("batch=%v err=%v", batch, err)
		}
		if err := s.Dispatched(*batch); err != nil {
			t.Fatal(err)
		}
		// Outlive the original expiry: only the recovered heartbeat keeps it owned.
		time.Sleep(3 * time.Minute)
		synctest.Wait()
		if n := heartbeats.Load(); n <= int32(len(transient)) {
			t.Fatalf("heartbeats=%d, want recovery after %d failures", n, len(transient))
		}
		select {
		case err := <-done:
			t.Fatalf("lease stopped during transient heartbeat failures: %v", err)
		default:
		}
		s.Accepted(*batch, poolReport(`[{"id":"a[1]","file_path":"a","status":"passed"}]`, 1, 0))
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if completes.Load() != 1 || s.outcome() != nil {
			t.Fatalf("completes=%d outcome=%v", completes.Load(), s.outcome())
		}
	})
}

func TestPoolTransientHeartbeatOutageAllowsAccountingUntilExpiry(t *testing.T) {
	for _, mode := range []string{"accounts before expiry", "expires"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				s := newPoolSource(cancel)
				lease := testLease()
				lease.ExpiresAt = time.Now().Add(2 * time.Minute)
				var heartbeats, completes, releases atomic.Int32
				f := &fakePoolScheduler{
					heartbeat: func(ctx context.Context, _ string) (time.Time, error) {
						heartbeats.Add(1)
						// Like the client, keep retrying until the request deadline.
						<-ctx.Done()
						return time.Time{}, ctx.Err()
					},
					complete: func(ctx context.Context, _ []api.AttemptResult) error {
						completes.Add(1)
						if deadline, ok := ctx.Deadline(); !ok || !deadline.Equal(lease.ExpiresAt) {
							t.Errorf("accounting deadline=%s, want lease expiry %s", deadline, lease.ExpiresAt)
						}
						return nil
					},
					release: func() error { releases.Add(1); return nil },
				}
				done := make(chan error, 1)
				go func() {
					_, err := s.executeLease(ctx, f, api.Pool{ID: "pool"}, s.track(f, "pool", lease, "consuming"), 0)
					done <- err
				}()
				synctest.Wait()
				batch, _, err := s.Next()
				if err != nil || batch == nil {
					t.Fatalf("batch=%v err=%v", batch, err)
				}
				if err := s.Dispatched(*batch); err != nil {
					t.Fatal(err)
				}
				time.Sleep(90 * time.Second)
				synctest.Wait()
				if heartbeats.Load() == 0 {
					t.Fatal("no heartbeat attempted")
				}
				if mode == "accounts before expiry" {
					s.Accepted(*batch, poolReport(`[{"id":"a[1]","file_path":"a","status":"passed"}]`, 1, 0))
					if err := <-done; err != nil {
						t.Fatal(err)
					}
					if completes.Load() != 1 || releases.Load() != 0 {
						t.Fatalf("completes=%d releases=%d", completes.Load(), releases.Load())
					}
					return
				}
				time.Sleep(time.Minute)
				synctest.Wait()
				err = <-done
				if err == nil || !strings.Contains(err.Error(), "lease ownership lost before accounting") {
					t.Fatalf("error=%v", err)
				}
				if outcome := s.outcome(); outcome == nil || !strings.Contains(outcome.Error(), "lease expired") {
					t.Fatalf("outcome=%v", outcome)
				}
				if completes.Load() != 0 || releases.Load() != 0 {
					t.Fatalf("completes=%d releases=%d", completes.Load(), releases.Load())
				}
			})
		})
	}
}

func TestEmptyPoolStatesDoNotMeanDone(t *testing.T) {
	for _, state := range []string{"planning", "populating", "consuming", "consumed", "errored"} {
		t.Run(state, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				s := newPoolSource(cancel)
				calls := 0
				f := &fakePoolScheduler{acquire: func(context.Context) (api.LeaseResponse, error) {
					calls++
					r := api.LeaseResponse{}
					r.Pool.State = state
					if calls > 1 {
						r.Pool.State = "consumed"
					}
					return r, nil
				}}
				s.work(ctx, f, api.Pool{ID: "pool"}, 0)
				_, reason, err := s.Next()
				if state == "errored" {
					if err == nil {
						t.Fatal("errored pool passed")
					}
					return
				}
				if reason != "pool_consumed" || err != nil {
					t.Fatal(reason, err)
				}
				if state != "consumed" && calls != 2 {
					t.Fatal("empty pool treated as drained")
				}
			})
		})
	}
}

func TestEmptyLeasePollingStaysBelowTenPerMinute(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		s := newPoolSource(cancel)
		var requests []time.Time
		f := &fakePoolScheduler{acquire: func(context.Context) (api.LeaseResponse, error) {
			requests = append(requests, time.Now())
			r := api.LeaseResponse{}
			r.Pool.State = "consuming"
			return r, nil
		}}
		s.work(ctx, f, api.Pool{ID: "pool"}, 0)
		if len(requests) != 9 {
			t.Fatalf("requests in 60 seconds=%d, want 9", len(requests))
		}
		for i := 1; i < len(requests); i++ {
			if gap := requests[i].Sub(requests[i-1]); gap < 7*time.Second {
				t.Fatalf("empty lease requests %d and %d only %s apart", i-1, i, gap)
			}
		}
	})
}

func costLease(id string, cost time.Duration) *api.Lease {
	return &api.Lease{ID: id, ExpiresAt: time.Now().Add(10 * time.Minute), Attempts: []api.LeaseAttempt{{
		ID: id + "-attempt", SelectorType: "test_plan_test_case_v1",
		Selector: plan.TestCase{Format: "file", Path: id}, Costs: api.AttemptCosts{DurationP90MS: cost.Milliseconds()},
	}}}
}

// leaseSequence serves leases in order, then reports the pool consumed.
func leaseSequence(leases ...*api.Lease) func(context.Context) (api.LeaseResponse, error) {
	var mu sync.Mutex
	return func(context.Context) (api.LeaseResponse, error) {
		mu.Lock()
		defer mu.Unlock()
		r := api.LeaseResponse{}
		r.Pool.State = "consumed"
		if len(leases) > 0 {
			r.Pool.State = "consuming"
			r.Lease, leases = leases[0], leases[1:]
		}
		return r, nil
	}
}

// dispatchOffered dispatches the batch the worker is offering now, without
// advancing time.
func dispatchOffered(t *testing.T, s *poolSource, path string) runnerexec.Batch {
	t.Helper()
	synctest.Wait()
	batch, _, err := s.Next()
	if err != nil || batch == nil || (path != "" && batch.Tests[0].Path != path) {
		t.Fatalf("batch=%v err=%v, want %s", batch, err, path)
	}
	if err := s.Dispatched(*batch); err != nil {
		t.Fatal(err)
	}
	return *batch
}

func passBatch(s *poolSource, batch runnerexec.Batch) {
	path := batch.Tests[0].Path
	s.Accepted(batch, poolReport(`[{"id":"`+path+`[1]","file_path":"`+path+`","status":"passed"}]`, 1, 0))
}

func acquires(f *fakePoolScheduler) []string {
	var calls []string
	for _, call := range f.recorded() {
		if strings.HasPrefix(call, "acquire") {
			calls = append(calls, call)
		}
	}
	return calls
}

func TestPoolWorkerPrefetchesNextLeaseAtLearnedPace(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		s := newPoolSource(cancel)
		f := &fakePoolScheduler{
			acquire:  leaseSequence(costLease("one", time.Minute), costLease("two", time.Minute), costLease("three", time.Minute)),
			complete: func(context.Context, []api.AttemptResult) error { return nil },
			release:  func() error { t.Error("released a lease"); return nil },
		}
		done := make(chan struct{})
		go func() { defer close(done); s.work(ctx, f, api.Pool{ID: "pool"}, 0) }()

		// Without history, prefetch near the p90 estimate; finishing at half of it
		// is before the timer, so lease two is requested alongside completion.
		time.Sleep(time.Second) // past the lease-start jitter
		one := dispatchOffered(t, s, "one")
		time.Sleep(30 * time.Second)
		passBatch(s, one)
		time.Sleep(time.Second)
		two := dispatchOffered(t, s, "two")
		if got := acquires(f); !slices.Equal(got, []string{"acquire one", "acquire two"}) {
			t.Fatalf("acquires=%v", got)
		}

		// Lease one ran at half its estimate, so lease two's predicted runtime is
		// 30s: prefetch after 30s - 6s headroom - 2s lease request allowance.
		time.Sleep(22*time.Second - time.Millisecond)
		synctest.Wait()
		if got := len(acquires(f)); got != 2 {
			t.Fatalf("prefetched too early: acquires=%v", acquires(f))
		}
		time.Sleep(time.Millisecond)
		synctest.Wait()
		if got := acquires(f); !slices.Equal(got, []string{"acquire one", "acquire two", "acquire three"}) {
			t.Fatalf("acquires=%v", got)
		}

		// Lease three is offered as soon as lease two is accounted, with no jitter
		// or lease request in between.
		time.Sleep(2 * time.Second)
		passBatch(s, two)
		three := dispatchOffered(t, s, "three")
		if got := f.recorded(); !slices.Contains(got, "complete two") || len(acquires(f)) != 3 {
			t.Fatalf("calls=%v", got)
		}
		passBatch(s, three)
		time.Sleep(time.Second)
		<-done
		if err := s.outcome(); err != nil || s.passedAttempts != 3 {
			t.Fatalf("outcome=%v passed=%d", err, s.passedAttempts)
		}
	})
}

func TestPoolWorkerReleasesUnusedPrefetchedLease(t *testing.T) {
	for _, mode := range []string{"idle", "terminating"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				s := newPoolSource(cancel)
				f := &fakePoolScheduler{
					acquire:  leaseSequence(costLease("one", time.Minute), costLease("two", time.Minute)),
					complete: func(context.Context, []api.AttemptResult) error { return nil },
					release:  func() error { return nil },
				}
				done := make(chan struct{})
				go func() { defer close(done); s.work(ctx, f, api.Pool{ID: "pool"}, 0) }()
				time.Sleep(time.Second)
				one := dispatchOffered(t, s, "one")
				time.Sleep(46 * time.Second) // 60s - 12s headroom - 2s allowance
				synctest.Wait()
				if got := acquires(f); !slices.Equal(got, []string{"acquire one", "acquire two"}) {
					t.Fatalf("acquires=%v", got)
				}
				if mode == "terminating" {
					s.Terminating()
					<-done
					got := f.recorded()
					if !slices.Contains(got, "release one") || !slices.Contains(got, "release two") || slices.Contains(got, "complete one") {
						t.Fatalf("calls=%v", got)
					}
					return
				}
				// The current lease overruns: hand its prefetched lease back.
				time.Sleep(prefetchMaxIdle)
				synctest.Wait()
				if got := f.recorded(); !slices.Contains(got, "release two") {
					t.Fatalf("calls=%v", got)
				}
				passBatch(s, one)
				time.Sleep(time.Second)
				<-done
				got := f.recorded()
				if !slices.Contains(got, "complete one") || slices.Contains(got, "complete two") || slices.Contains(got, "release one") {
					t.Fatalf("calls=%v", got)
				}
				if a := acquires(f); !slices.Equal(a, []string{"acquire one", "acquire two", "acquire none"}) {
					t.Fatalf("acquires=%v", a)
				}
				if err := s.outcome(); err != nil {
					t.Fatal(err)
				}
			})
		})
	}
}

func TestPoolWorkerDropsPrefetchedLeaseLostBeforeDispatch(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		s := newPoolSource(cancel)
		two := costLease("two", time.Minute)
		// Prefetched at 47 seconds with 30 seconds left: its first heartbeat is
		// due at 57 seconds, before the idle limit at 77 seconds.
		two.ExpiresAt = time.Now().Add(77 * time.Second)
		f := &fakePoolScheduler{
			acquire: leaseSequence(costLease("one", time.Minute), two, costLease("three", time.Minute)),
			heartbeat: func(_ context.Context, id string) (time.Time, error) {
				if id == "two" {
					return time.Time{}, &api.LeaseHTTPError{Status: 404}
				}
				return time.Now().Add(10 * time.Minute), nil
			},
			complete: func(context.Context, []api.AttemptResult) error { return nil },
			release:  func() error { t.Error("released a lease"); return nil },
		}
		done := make(chan struct{})
		go func() { defer close(done); s.work(ctx, f, api.Pool{ID: "pool"}, 0) }()
		time.Sleep(time.Second)
		one := dispatchOffered(t, s, "one")
		time.Sleep(70 * time.Second)
		synctest.Wait()
		if got := f.recorded(); !slices.Contains(got, "acquire two") || !slices.Contains(got, "heartbeat two") {
			t.Fatalf("calls=%v", got)
		}
		if err := s.outcome(); err != nil {
			t.Fatalf("losing an undispatched lease stopped the worker: %v", err)
		}
		passBatch(s, one)
		time.Sleep(time.Second)
		three := dispatchOffered(t, s, "three")
		passBatch(s, three)
		time.Sleep(time.Second)
		<-done
		if got := f.recorded(); slices.Contains(got, "complete two") || !slices.Contains(got, "complete one") || !slices.Contains(got, "complete three") {
			t.Fatalf("calls=%v", got)
		}
		if err := s.outcome(); err != nil {
			t.Fatal(err)
		}
	})
}

func TestPoolPrefetchHeadroomIsCapped(t *testing.T) {
	for _, tc := range []struct {
		p90Total, want time.Duration
	}{
		{time.Minute, 46 * time.Second},                     // 60s - 12s headroom - 2s allowance
		{10 * time.Minute, 10*time.Minute - 17*time.Second}, // headroom capped at 15s
		{time.Second, 0}, // prefetch immediately
	} {
		t.Run(tc.p90Total.String(), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				p := &leasePrefetch{}
				p.schedule(newPoolSource(func() {}), &poolLease{Lease: *costLease("lease", tc.p90Total)}, time.Now())
				start := time.Now()
				<-p.due.C
				if got := time.Since(start); got != tc.want {
					t.Fatalf("prefetch after %s, want %s", got, tc.want)
				}
			})
		})
	}
}

func reportBatch(s *poolSource, batch runnerexec.Batch, path, status string) {
	s.Accepted(batch, poolReport(`[{"id":"`+path+`[1]","file_path":"`+path+`","status":"`+status+`"}]`, 1, 0))
}

func TestPoolWorkerPrefetchesAlongsideCompletionWhenLeaseFinishesFirst(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		s := newPoolSource(cancel)
		completing := make(chan struct{})
		f := &fakePoolScheduler{
			acquire: leaseSequence(costLease("one", time.Minute), costLease("two", time.Minute)),
			complete: func(context.Context, []api.AttemptResult) error {
				<-completing
				return nil
			},
			release: func() error { t.Error("released a lease"); return nil },
		}
		done := make(chan struct{})
		go func() { defer close(done); s.work(ctx, f, api.Pool{ID: "pool"}, 1) }()
		time.Sleep(time.Second)
		// The timer is due at 46 seconds; round 0 and its retry finish at 40.
		one := dispatchOffered(t, s, "one")
		time.Sleep(30 * time.Second)
		reportBatch(s, one, "one", "failed")
		retry := dispatchOffered(t, s, "")
		time.Sleep(10 * time.Second)
		reportBatch(s, retry, "one", "passed")
		synctest.Wait()
		if got := f.recorded(); !slices.Equal(acquires(f), []string{"acquire one", "acquire two"}) || !slices.Contains(got, "complete one") {
			t.Fatalf("lease two was not requested while completing lease one: %v", got)
		}
		close(completing)
		two := dispatchOffered(t, s, "two")
		passBatch(s, two)
		time.Sleep(time.Second)
		<-done
		if err := s.outcome(); err != nil || s.passedAttempts != 2 {
			t.Fatalf("outcome=%v passed=%d", err, s.passedAttempts)
		}
	})
}

func TestPoolWorkerReleasesPrefetchedLeaseWaitingBehindRetries(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		s := newPoolSource(cancel)
		f := &fakePoolScheduler{
			acquire:  leaseSequence(costLease("one", time.Minute), costLease("two", time.Minute), costLease("three", time.Minute)),
			complete: func(context.Context, []api.AttemptResult) error { return nil },
			release:  func() error { return nil },
		}
		done := make(chan struct{})
		go func() { defer close(done); s.work(ctx, f, api.Pool{ID: "pool"}, 1) }()
		time.Sleep(time.Second)
		one := dispatchOffered(t, s, "one")
		// Lease two is prefetched at 46 seconds, during round 0.
		time.Sleep(55 * time.Second)
		reportBatch(s, one, "one", "failed")
		retry := dispatchOffered(t, s, "")
		if got := acquires(f); !slices.Equal(got, []string{"acquire one", "acquire two"}) {
			t.Fatalf("acquires=%v", got)
		}
		// The retry round outlasts lease two's 30-second idle limit.
		time.Sleep(25 * time.Second)
		synctest.Wait()
		if got := f.recorded(); !slices.Contains(got, "release two") || slices.Contains(got, "complete one") {
			t.Fatalf("calls=%v", got)
		}
		time.Sleep(10 * time.Second)
		reportBatch(s, retry, "one", "passed")
		time.Sleep(time.Second)
		three := dispatchOffered(t, s, "three")
		passBatch(s, three)
		time.Sleep(time.Second)
		<-done
		got := f.recorded()
		if slices.Contains(got, "complete two") || !slices.Contains(got, "complete one") || !slices.Contains(got, "complete three") {
			t.Fatalf("calls=%v", got)
		}
		if err := s.outcome(); err != nil || s.passedAttempts != 2 {
			t.Fatalf("outcome=%v passed=%d", err, s.passedAttempts)
		}
	})
}

func TestPoolWorkerSlowIdleReleaseDoesNotBlockCurrentLease(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		s := newPoolSource(cancel)
		// Lease one cannot be renewed and expires at 100 seconds.
		one := costLease("one", time.Minute)
		one.ExpiresAt = time.Now().Add(100 * time.Second)
		f := &fakePoolScheduler{
			acquire: leaseSequence(one, costLease("two", time.Minute)),
			heartbeat: func(_ context.Context, id string) (time.Time, error) {
				if id == "one" {
					return time.Time{}, &api.LeaseHTTPError{Status: 422, Code: api.LeaseErrorCodeMaximumLifetime}
				}
				return time.Now().Add(10 * time.Minute), nil
			},
			complete: func(context.Context, []api.AttemptResult) error { return nil },
			// Like a release retrying a rate limit until its deadline.
			release: func() error { time.Sleep(90 * time.Second); return nil },
		}
		done := make(chan struct{})
		go func() { defer close(done); s.work(ctx, f, api.Pool{ID: "pool"}, 0) }()
		time.Sleep(time.Second)
		batch := dispatchOffered(t, s, "one")
		// Lease two is prefetched at 47 seconds and its idle release starts at 77.
		time.Sleep(89 * time.Second)
		synctest.Wait()
		if got := f.recorded(); !slices.Contains(got, "release two") {
			t.Fatalf("calls=%v", got)
		}
		passBatch(s, batch)
		synctest.Wait()
		if got := f.recorded(); !slices.Contains(got, "complete one") {
			t.Fatalf("lease one was not completed while lease two's release was pending: %v", got)
		}
		<-done
		if err := s.outcome(); err != nil {
			t.Fatal(err)
		}
	})
}
