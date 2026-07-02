package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func newTestSchedulerClient(svrURL string) *Client {
	return NewClient(ClientConfig{
		AccessToken:      "oidc-token",
		OrganizationSlug: "buildkite",
		ServerBaseURL:    svrURL,
	})
}

func TestCreateSchedulerPlan(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("request method = %q, want %q", r.Method, http.MethodPost)
		}
		if r.URL.Path != "/v2/organizations/buildkite/test-scheduler/plan" {
			t.Errorf("request path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer oidc-token" {
			t.Errorf("Authorization header = %q", got)
		}

		assertJSONBody(t, r.Body, `{
			"organization_id": "org-uuid",
			"suite_id": "suite-uuid",
			"pipeline_id": "pipeline-uuid",
			"build_id": "build-uuid",
			"key": "default",
			"test_plan_identifier": "build-uuid/step-uuid"
		}`)

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{
			"pool": {
				"id": "pool-uuid",
				"organization_id": "org-uuid",
				"suite_id": "suite-uuid",
				"pipeline_id": "pipeline-uuid",
				"build_id": "build-uuid",
				"key": "default",
				"state": "consuming",
				"expires_at": "2026-06-13T00:00:00Z",
				"created_at": "2026-06-12T00:00:00Z"
			},
			"uploaded_entries_count": 42
		}`)
	}))
	defer svr.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	got, err := newTestSchedulerClient(svr.URL).CreateSchedulerPlan(ctx, CreateSchedulerPlanParams{
		OrganizationID:     "org-uuid",
		SuiteID:            "suite-uuid",
		PipelineID:         "pipeline-uuid",
		BuildID:            "build-uuid",
		Key:                "default",
		TestPlanIdentifier: "build-uuid/step-uuid",
	})
	if err != nil {
		t.Fatalf("CreateSchedulerPlan() error = %v", err)
	}

	want := CreateSchedulerPlanResponse{
		Pool: SchedulerPool{
			ID:             "pool-uuid",
			OrganizationID: "org-uuid",
			SuiteID:        "suite-uuid",
			PipelineID:     "pipeline-uuid",
			BuildID:        "build-uuid",
			Key:            "default",
			State:          "consuming",
			ExpiresAt:      "2026-06-13T00:00:00Z",
			CreatedAt:      "2026-06-12T00:00:00Z",
		},
		UploadedEntriesCount: 42,
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("CreateSchedulerPlan() diff (-got +want):\n%s", diff)
	}
}

func TestCreateSchedulerPlan_Conflict(t *testing.T) {
	requestCount := 0
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		http.Error(w, `{"message": "Pool already exists"}`, http.StatusConflict)
	}))
	defer svr.Close()

	_, err := newTestSchedulerClient(svr.URL).CreateSchedulerPlan(context.Background(), CreateSchedulerPlanParams{})

	// A 409 from the scheduler plan endpoint must not be retried; it surfaces
	// immediately as a ConflictError so the caller can resolve the existing pool.
	if requestCount != 1 {
		t.Errorf("http request count = %d, want 1", requestCount)
	}

	if conflictError := new(ConflictError); !errors.As(err, &conflictError) {
		t.Fatalf("CreateSchedulerPlan() error type = %T, want %T", err, ConflictError{})
	}

	if err.Error() != "Pool already exists" {
		t.Errorf("CreateSchedulerPlan() error = %q, want %q", err.Error(), "Pool already exists")
	}
}

func TestFetchSchedulerPools(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("request method = %q, want %q", r.Method, http.MethodGet)
		}
		if r.URL.Path != "/v2/organizations/buildkite/test-scheduler/pools" {
			t.Errorf("request path = %q", r.URL.Path)
		}

		query := r.URL.Query()
		if got := query.Get("pipeline_id"); got != "pipeline-uuid" {
			t.Errorf("pipeline_id query = %q", got)
		}
		if got := query.Get("build_id"); got != "build-uuid" {
			t.Errorf("build_id query = %q", got)
		}
		if got := query.Get("key"); got != "default" {
			t.Errorf("key query = %q", got)
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = io.WriteString(w, `{
			"pools": [{
				"id": "pool-uuid",
				"organization_id": "org-uuid",
				"suite_id": "suite-uuid",
				"pipeline_id": "pipeline-uuid",
				"build_id": "build-uuid",
				"key": "default",
				"state": "consuming",
				"expires_at": "2026-06-13T00:00:00Z",
				"created_at": "2026-06-12T00:00:00Z"
			}]
		}`)
	}))
	defer svr.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	got, err := newTestSchedulerClient(svr.URL).FetchSchedulerPools(ctx, FetchSchedulerPoolsParams{
		PipelineID: "pipeline-uuid",
		BuildID:    "build-uuid",
		Key:        "default",
	})
	if err != nil {
		t.Fatalf("FetchSchedulerPools() error = %v", err)
	}

	want := []SchedulerPool{{
		ID:             "pool-uuid",
		OrganizationID: "org-uuid",
		SuiteID:        "suite-uuid",
		PipelineID:     "pipeline-uuid",
		BuildID:        "build-uuid",
		Key:            "default",
		State:          "consuming",
		ExpiresAt:      "2026-06-13T00:00:00Z",
		CreatedAt:      "2026-06-12T00:00:00Z",
	}}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("FetchSchedulerPools() diff (-got +want):\n%s", diff)
	}
}

func TestCreateSchedulerLease(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("request method = %q, want %q", r.Method, http.MethodPost)
		}
		if r.URL.Path != "/v2/organizations/buildkite/test-scheduler/pools/pool-uuid/leases" {
			t.Errorf("request path = %q", r.URL.Path)
		}

		assertJSONBody(t, r.Body, `{"target_cost_limit": {"custom": 30}, "lease_ttl_seconds": 300}`)

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = io.WriteString(w, `{
			"lease": {
				"id": "lease-uuid",
				"expires_at": "2026-06-12T00:05:00Z",
				"attempts": [
					{
						"id": "attempt-1",
						"pool_id": "pool-uuid",
						"entry_id": "entry-1",
						"attempt_index": 0,
						"selector_type": "file",
						"selector": {"path": "spec/foo_spec.rb"},
						"costs": {"custom": 1.5},
						"priority": 2,
						"state": "leased",
						"lease_id": "lease-uuid",
						"lease_expires_at": "2026-06-12T00:05:00Z",
						"meta_data": {"source": "plan"}
					},
					{
						"id": "attempt-2",
						"pool_id": "pool-uuid",
						"entry_id": "entry-2",
						"attempt_index": 0,
						"selector_type": "example",
						"selector": {"path": "spec/bar_spec.rb[1:2]"},
						"costs": {"custom": 1},
						"priority": 1,
						"state": "leased",
						"lease_id": "lease-uuid",
						"lease_expires_at": "2026-06-12T00:05:00Z",
						"meta_data": null
					}
				]
			}
		}`)
	}))
	defer svr.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	got, err := newTestSchedulerClient(svr.URL).CreateSchedulerLease(ctx, "pool-uuid", CreateSchedulerLeaseParams{
		TargetCostLimit: 30,
		LeaseTTLSeconds: 300,
	})
	if err != nil {
		t.Fatalf("CreateSchedulerLease() error = %v", err)
	}

	want := CreateSchedulerLeaseResponse{
		Lease: &SchedulerLease{
			ID:        "lease-uuid",
			ExpiresAt: "2026-06-12T00:05:00Z",
			Attempts: []SchedulerLeaseAttempt{
				{
					ID:             "attempt-1",
					PoolID:         "pool-uuid",
					EntryID:        "entry-1",
					AttemptIndex:   0,
					SelectorType:   "file",
					Selector:       json.RawMessage(`{"path": "spec/foo_spec.rb"}`),
					Costs:          SchedulerCosts{Custom: 1.5},
					Priority:       2,
					State:          "leased",
					LeaseID:        "lease-uuid",
					LeaseExpiresAt: "2026-06-12T00:05:00Z",
					MetaData:       json.RawMessage(`{"source": "plan"}`),
				},
				{
					ID:             "attempt-2",
					PoolID:         "pool-uuid",
					EntryID:        "entry-2",
					AttemptIndex:   0,
					SelectorType:   "example",
					Selector:       json.RawMessage(`{"path": "spec/bar_spec.rb[1:2]"}`),
					Costs:          SchedulerCosts{Custom: 1},
					Priority:       1,
					State:          "leased",
					LeaseID:        "lease-uuid",
					LeaseExpiresAt: "2026-06-12T00:05:00Z",
					MetaData:       json.RawMessage(`null`),
				},
			},
		},
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("CreateSchedulerLease() diff (-got +want):\n%s", diff)
	}
}

func TestCreateSchedulerLease_NoWork(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = io.WriteString(w, `{"lease": null}`)
	}))
	defer svr.Close()

	got, err := newTestSchedulerClient(svr.URL).CreateSchedulerLease(context.Background(), "pool-uuid", CreateSchedulerLeaseParams{})
	if err != nil {
		t.Fatalf("CreateSchedulerLease() error = %v", err)
	}

	if got.Lease != nil {
		t.Errorf("CreateSchedulerLease() lease = %v, want nil", got.Lease)
	}
}

func TestCompleteSchedulerLeases(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/organizations/buildkite/test-scheduler/pools/pool-uuid/leases/complete" {
			t.Errorf("request path = %q", r.URL.Path)
		}

		assertJSONBody(t, r.Body, `{"leases": [{"id": "lease-uuid"}]}`)

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = io.WriteString(w, `{"completed_entry_ids": ["entry-1", "entry-2"]}`)
	}))
	defer svr.Close()

	got, err := newTestSchedulerClient(svr.URL).CompleteSchedulerLeases(context.Background(), "pool-uuid", CompleteSchedulerLeasesParams{
		Leases: []SchedulerLeaseRef{{ID: "lease-uuid"}},
	})
	if err != nil {
		t.Fatalf("CompleteSchedulerLeases() error = %v", err)
	}

	want := CompleteSchedulerLeasesResponse{CompletedEntryIDs: []string{"entry-1", "entry-2"}}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("CompleteSchedulerLeases() diff (-got +want):\n%s", diff)
	}
}

func TestCompleteSchedulerLeases_Conflict(t *testing.T) {
	requestCount := 0
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		http.Error(w, `{"message": "Lease expired"}`, http.StatusConflict)
	}))
	defer svr.Close()

	_, err := newTestSchedulerClient(svr.URL).CompleteSchedulerLeases(context.Background(), "pool-uuid", CompleteSchedulerLeasesParams{
		Leases: []SchedulerLeaseRef{{ID: "lease-uuid"}},
	})

	if requestCount != 1 {
		t.Errorf("http request count = %d, want 1", requestCount)
	}

	if conflictError := new(ConflictError); !errors.As(err, &conflictError) {
		t.Fatalf("CompleteSchedulerLeases() error type = %T, want %T", err, ConflictError{})
	}

	if err.Error() != "Lease expired" {
		t.Errorf("CompleteSchedulerLeases() error = %q, want %q", err.Error(), "Lease expired")
	}
}

func TestHeartbeatSchedulerLeases(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/organizations/buildkite/test-scheduler/pools/pool-uuid/leases/heartbeat" {
			t.Errorf("request path = %q", r.URL.Path)
		}

		assertJSONBody(t, r.Body, `{"lease_ids": ["lease-uuid"], "lease_ttl_seconds": 300}`)

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = io.WriteString(w, `{"leases": [{"id": "lease-uuid", "expires_at": "2026-06-12T00:10:00Z"}]}`)
	}))
	defer svr.Close()

	got, err := newTestSchedulerClient(svr.URL).HeartbeatSchedulerLeases(context.Background(), "pool-uuid", HeartbeatSchedulerLeasesParams{
		LeaseIDs:        []string{"lease-uuid"},
		LeaseTTLSeconds: 300,
	})
	if err != nil {
		t.Fatalf("HeartbeatSchedulerLeases() error = %v", err)
	}

	want := HeartbeatSchedulerLeasesResponse{
		Leases: []SchedulerLeaseExpiry{{ID: "lease-uuid", ExpiresAt: "2026-06-12T00:10:00Z"}},
	}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("HeartbeatSchedulerLeases() diff (-got +want):\n%s", diff)
	}
}

func TestHeartbeatSchedulerLeases_Conflict(t *testing.T) {
	requestCount := 0
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		http.Error(w, `{"message": "Lease expired"}`, http.StatusConflict)
	}))
	defer svr.Close()

	_, err := newTestSchedulerClient(svr.URL).HeartbeatSchedulerLeases(context.Background(), "pool-uuid", HeartbeatSchedulerLeasesParams{
		LeaseIDs: []string{"lease-uuid"},
	})

	if requestCount != 1 {
		t.Errorf("http request count = %d, want 1", requestCount)
	}

	if conflictError := new(ConflictError); !errors.As(err, &conflictError) {
		t.Fatalf("HeartbeatSchedulerLeases() error type = %T, want %T", err, ConflictError{})
	}
}

func TestFetchSchedulerPoolMetrics(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("request method = %q, want %q", r.Method, http.MethodGet)
		}
		if r.URL.Path != "/v2/organizations/buildkite/test-scheduler/pools/pool-uuid/metrics" {
			t.Errorf("request path = %q", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = io.WriteString(w, `{
			"metrics": {
				"waiting_entries_count": 0,
				"leased_entries_count": 0,
				"completed_entries_count": 12,
				"total_entries_count": 12,
				"oldest_waiting_entry_created_at": null,
				"waiting_custom_cost_sum": 0,
				"drained": true
			}
		}`)
	}))
	defer svr.Close()

	got, err := newTestSchedulerClient(svr.URL).FetchSchedulerPoolMetrics(context.Background(), "pool-uuid")
	if err != nil {
		t.Fatalf("FetchSchedulerPoolMetrics() error = %v", err)
	}

	want := SchedulerPoolMetrics{
		CompletedEntriesCount: 12,
		TotalEntriesCount:     12,
		Drained:               true,
	}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("FetchSchedulerPoolMetrics() diff (-got +want):\n%s", diff)
	}
}

func TestFetchSchedulerPools_Empty(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = io.WriteString(w, `{"pools": []}`)
	}))
	defer svr.Close()

	got, err := newTestSchedulerClient(svr.URL).FetchSchedulerPools(context.Background(), FetchSchedulerPoolsParams{
		PipelineID: "pipeline-uuid",
		BuildID:    "build-uuid",
		Key:        "default",
	})
	if err != nil {
		t.Fatalf("FetchSchedulerPools() error = %v", err)
	}

	if len(got) != 0 {
		t.Errorf("FetchSchedulerPools() = %v, want empty", got)
	}
}
