package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/buildkite/test-engine-client/v2/internal/plan"
)

func TestTestSchedulerAPIRequests(t *testing.T) {
	tests := []struct {
		name       string
		call       func(context.Context, *Client) error
		wantMethod string
		wantPath   string
		wantBody   map[string]any
		status     int
		response   string
	}{
		{
			name: "create pool",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.CreateTestSchedulerPool(ctx, CreateTestSchedulerPoolParams{Suite: "suite", Pipeline: "pipe", BuildID: "build", Key: "key", TTLSeconds: 3600})
				return err
			},
			wantMethod: http.MethodPost,
			wantPath:   "/v2/organizations/buildkite/test-scheduler/pools",
			wantBody:   map[string]any{"suite": "suite", "pipeline": "pipe", "build_id": "build", "key": "key", "ttl_seconds": float64(3600)},
			status:     http.StatusCreated,
			response:   `{"pool":{"id":"pool","build_id":"build","key":"key","state":"populating"}}`,
		},
		{
			name: "create entries",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.CreateTestSchedulerEntries(ctx, "pool", []TestSchedulerEntryParams{{
					SelectorType: "custom",
					Selector:     TestSchedulerEntrySelector{Format: plan.TestCaseFormatFile, Path: "spec/a_spec.rb"},
					Costs:        map[string]float64{"custom": 1},
				}})
				return err
			},
			wantMethod: http.MethodPost,
			wantPath:   "/v2/organizations/buildkite/test-scheduler/pools/pool/entries",
			wantBody: map[string]any{"entries": []any{map[string]any{
				"selector_type": "custom",
				"selector":      map[string]any{"format": "file", "path": "spec/a_spec.rb"},
				"costs":         map[string]any{"custom": float64(1)},
			}}},
			status:   http.StatusCreated,
			response: `{"entries":[]}`,
		},
		{
			name: "lease",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.CreateTestSchedulerLease(ctx, "pool", CreateTestSchedulerLeaseParams{TargetCostLimit: map[string]float64{"custom": 10}, LeaseTTLSeconds: 600})
				return err
			},
			wantMethod: http.MethodPost,
			wantPath:   "/v2/organizations/buildkite/test-scheduler/pools/pool/leases",
			wantBody:   map[string]any{"target_cost_limit": map[string]any{"custom": float64(10)}, "lease_ttl_seconds": float64(600)},
			status:     http.StatusOK,
			response:   `{"lease":null}`,
		},
		{
			name: "complete",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.CompleteTestSchedulerLeases(ctx, "pool", CompleteTestSchedulerLeasesParams{Leases: []CompleteTestSchedulerLeaseParams{{LeaseID: "lease", Attempts: []CompleteTestSchedulerAttemptParams{{AttemptID: "attempt", Result: "passed"}}}}})
				return err
			},
			wantMethod: http.MethodPost,
			wantPath:   "/v2/organizations/buildkite/test-scheduler/pools/pool/leases/complete",
			wantBody:   map[string]any{"leases": []any{map[string]any{"lease_id": "lease", "attempts": []any{map[string]any{"attempt_id": "attempt", "result": "passed"}}}}},
			status:     http.StatusOK,
			response:   `{"leases":[],"pool":{"id":"pool","state":"populating"}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != tt.wantMethod {
					t.Errorf("method = %s, want %s", r.Method, tt.wantMethod)
				}
				if r.URL.Path != tt.wantPath {
					t.Errorf("path = %s, want %s", r.URL.Path, tt.wantPath)
				}
				if got := r.Header.Get("Authorization"); got != "Bearer token" {
					t.Errorf("Authorization = %q, want Bearer token", got)
				}

				var got map[string]any
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
					t.Fatalf("decode body: %v", err)
				}
				if diff := cmpMap(got, tt.wantBody); diff != "" {
					t.Errorf("body diff: %s", diff)
				}

				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.response))
			}))
			defer server.Close()

			client := NewClient(ClientConfig{ServerBaseURL: server.URL, AccessToken: "token", OrganizationSlug: "buildkite"})
			if err := tt.call(context.Background(), client); err != nil {
				t.Fatalf("call error = %v", err)
			}
		})
	}
}

func TestTestSchedulerConflictIsNotRetried(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"message":"A test pool already exists for this build and key"}`))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{ServerBaseURL: server.URL, AccessToken: "token", OrganizationSlug: "buildkite"})
	_, err := client.CreateTestSchedulerPool(context.Background(), CreateTestSchedulerPoolParams{Suite: "suite", Pipeline: "pipe", BuildID: "build", Key: "key"})
	if err == nil {
		t.Fatal("CreateTestSchedulerPool() error = nil, want conflict")
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
}

func cmpMap(got, want map[string]any) string {
	gotJSON, _ := json.Marshal(got)
	wantJSON, _ := json.Marshal(want)
	if string(gotJSON) != string(wantJSON) {
		return string(gotJSON) + " != " + string(wantJSON)
	}
	return ""
}
