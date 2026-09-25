package command

import (
	"bytes"
	"context"
	"errors"
	"strings"
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
	heartbeat func(context.Context) (time.Time, error)
	complete  func(context.Context, []api.AttemptResult) error
	release   func() error
}

func (f *fakePoolScheduler) AcquireLease(ctx context.Context, _ string) (api.LeaseResponse, error) {
	return f.acquire(ctx)
}
func (f *fakePoolScheduler) HeartbeatLease(ctx context.Context, _, _ string) (time.Time, error) {
	if f.heartbeat != nil {
		return f.heartbeat(ctx)
	}
	return time.Now().Add(time.Minute), nil
}
func (f *fakePoolScheduler) CompleteLease(ctx context.Context, _, _ string, r []api.AttemptResult) error {
	return f.complete(ctx, r)
}
func (f *fakePoolScheduler) ReleaseLease(context.Context, string, string) error { return f.release() }

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
	calls := 0
	completed := false
	heartbeat := make(chan struct{}, 1)
	f := &fakePoolScheduler{
		acquire: func(context.Context) (api.LeaseResponse, error) {
			calls++
			r := api.LeaseResponse{}
			r.Pool.State = "consuming"
			if calls == 1 {
				lease.ExpiresAt = time.Now().Add(120 * time.Millisecond)
				r.Lease = &lease
			} else {
				if !completed {
					t.Error("acquired next lease before accounting")
				}
				r.Pool.State = "consumed"
			}
			return r, nil
		},
		heartbeat: func(context.Context) (time.Time, error) {
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
			completed = true
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
	if !completed || calls != 2 {
		t.Fatalf("completed=%v calls=%d", completed, calls)
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
	go func() { done <- s.executeLease(ctx, f, api.Pool{ID: "pool"}, lease, 0, "consuming") }()
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

func TestPoolWorkerReleaseVersusConservativeCompletion(t *testing.T) {
	for _, mode := range []string{"undispatched", "dispatched", "malformed", "pool errored"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			s := newPoolSource(cancel)
			lease := testLease()
			if mode == "malformed" {
				lease.Attempts[0].SelectorType = "custom"
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
			go func() { done <- s.executeLease(ctx, f, api.Pool{ID: "pool"}, lease, 0, state) }()
			if mode == "undispatched" || mode == "dispatched" {
				batch := waitPoolBatch(t, s)
				if mode == "dispatched" {
					if err := s.Dispatched(*batch); err != nil {
						t.Fatal(err)
					}
				}
				cancel()
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
			if mode == "dispatched" {
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
		s.expires = time.Now().Add(10 * time.Minute)
		var calls atomic.Int32
		f := &fakePoolScheduler{heartbeat: func(context.Context) (time.Time, error) {
			calls.Add(1)
			return time.Now().Add(10 * time.Minute), nil
		}}
		done := make(chan struct{})
		go func() { defer close(done); s.heartbeat(ctx, f, "pool", "lease") }()
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

func TestPoolHeartbeatLossStopsDispatchAndAccounting(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := newPoolSource(cancel)
	lease := testLease()
	lease.ExpiresAt = time.Now().Add(60 * time.Millisecond)
	var accounted atomic.Int32
	f := &fakePoolScheduler{heartbeat: func(context.Context) (time.Time, error) { return time.Time{}, errors.New("ownership lost") }, complete: func(context.Context, []api.AttemptResult) error { accounted.Add(1); return nil }, release: func() error { accounted.Add(1); return nil }}
	done := make(chan error, 1)
	go func() { done <- s.executeLease(ctx, f, api.Pool{ID: "pool"}, lease, 0, "consuming") }()
	batch := waitPoolBatch(t, s)
	if err := s.Dispatched(*batch); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("missing ownership error")
		}
	case <-time.After(time.Second):
		t.Fatal("stuck")
	}
	if accounted.Load() != 0 || s.outcome() == nil {
		t.Fatal("lost lease was accounted as owned")
	}
	if err := s.Dispatched(*batch); err == nil {
		t.Fatal("dispatch after lease loss")
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
