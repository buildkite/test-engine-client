//go:build !windows && integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/buildkite/test-engine-client/v3/internal/api"
	"github.com/buildkite/test-engine-client/v3/internal/command"
	"github.com/buildkite/test-engine-client/v3/internal/config"
	"github.com/buildkite/test-engine-client/v3/internal/debug"
	"github.com/buildkite/test-engine-client/v3/internal/plan"
	"github.com/buildkite/test-engine-client/v3/internal/runnerexec"
)

func TestPoolRSpec(t *testing.T) {
	root := os.Getenv("BKTEC_RSPEC_RUNNER_ROOT")
	if root == "" {
		t.Fatal("set BKTEC_RSPEC_RUNNER_ROOT to the bktest/test-collector-ruby directory")
	}
	// The outer Go test is uploaded by bktec. The nested fixture examples must
	// remain local or Test Engine would record unrelated RSpec executions.
	uploadToken, hadUploadToken := os.LookupEnv("BUILDKITE_ANALYTICS_TOKEN")
	if err := os.Unsetenv("BUILDKITE_ANALYTICS_TOKEN"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if hadUploadToken {
			if err := os.Setenv("BUILDKITE_ANALYTICS_TOKEN", uploadToken); err != nil {
				t.Error(err)
			}
		} else if err := os.Unsetenv("BUILDKITE_ANALYTICS_TOKEN"); err != nil {
			t.Error(err)
		}
	})
	previousDebug := debug.Enabled
	debug.SetDebug(true)
	t.Cleanup(func() { debug.SetDebug(previousDebug) })
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(filepath.Join(previous, "..", "command", "testdata", "rspec")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })

	type leaseFixture struct {
		id       string
		attempts []api.LeaseAttempt
		want     []api.AttemptResult
	}
	leases := []leaseFixture{
		{
			id: "mixed",
			attempts: []api.LeaseAttempt{
				{ID: "passing-selector", SelectorType: "test_plan_test_case_v1", Selector: plan.TestCase{Format: plan.TestCaseFormatSelector, Value: "spec/fruits/apple_spec.rb"}},
				{ID: "passing-example", SelectorType: "test_plan_test_case_v1", Selector: plan.TestCase{Format: plan.TestCaseFormatExample, Identifier: "./spec/fruits/banana_spec.rb[1:1]", Path: "./spec/fruits/banana_spec.rb[1:1]"}},
			},
			want: []api.AttemptResult{{AttemptID: "passing-selector", Result: "passed"}, {AttemptID: "passing-example", Result: "passed"}},
		},
		{
			id: "failed-selector",
			attempts: []api.LeaseAttempt{
				{ID: "failed-selector", SelectorType: "test_plan_test_case_v1", Selector: plan.TestCase{Format: plan.TestCaseFormatSelector, Value: "spec/fruits/tomato_spec.rb"}},
			},
			want: []api.AttemptResult{{AttemptID: "failed-selector", Result: "failed"}},
		},
		{
			id: "failed-example",
			attempts: []api.LeaseAttempt{
				{ID: "failed-example", SelectorType: "test_plan_test_case_v1", Selector: plan.TestCase{Format: plan.TestCaseFormatExample, Identifier: "./spec/fruits/tomato_spec.rb[1:2]", Path: "./spec/fruits/tomato_spec.rb[1:2]"}},
			},
			want: []api.AttemptResult{{AttemptID: "failed-example", Result: "failed"}},
		},
		{
			id: "error-file",
			attempts: []api.LeaseAttempt{
				{ID: "error-file", SelectorType: "test_plan_test_case_v1", Selector: plan.TestCase{Format: plan.TestCaseFormatFile, Path: "spec/bad_syntax_spec.rb"}},
			},
			want: []api.AttemptResult{{AttemptID: "error-file", Result: "errored"}},
		},
	}

	// Costs far below the minimum lease request allowance make each lease
	// prefetch the next one as soon as its batch is dispatched.
	for _, fixture := range leases {
		for i := range fixture.attempts {
			fixture.attempts[i].Costs.DurationP90MS = 1
		}
	}
	var mu sync.Mutex
	acquisitions, completions, prefetches := 0, 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		base := "/v2/organizations/org/test-scheduler/pools/pool"
		switch {
		case r.Method == http.MethodGet && r.URL.Path == base:
			_ = json.NewEncoder(w).Encode(api.Pool{ID: "pool", State: "consuming", MutedTests: []plan.TestCase{}})
		case r.Method == http.MethodPost && r.URL.Path == base+"/leases":
			if acquisitions > completions {
				prefetches++
			}
			acquisitions++
			if acquisitions > len(leases) {
				fmt.Fprint(w, `{"lease":null,"pool":{"id":"pool","state":"consumed"}}`)
				return
			}
			fixture := leases[acquisitions-1]
			lease := api.Lease{ID: fixture.id, ExpiresAt: time.Now().Add(10 * time.Minute), Attempts: fixture.attempts}
			_ = json.NewEncoder(w).Encode(map[string]any{"lease": lease, "pool": map[string]string{"id": "pool", "state": "consuming"}})
		case r.Method == http.MethodPost && r.URL.Path == base+"/leases/complete":
			var request struct {
				Leases []struct {
					ID       string `json:"lease_id"`
					Attempts []struct {
						ID     string `json:"attempt_id"`
						Result string `json:"result"`
					} `json:"attempts"`
				} `json:"leases"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil || completions >= len(leases) || len(request.Leases) != 1 {
				t.Errorf("unexpected completion: %+v, %v", request, err)
				http.Error(w, "invalid completion", http.StatusBadRequest)
				return
			}
			fixture := leases[completions]
			got := make([]api.AttemptResult, len(request.Leases[0].Attempts))
			for i, attempt := range request.Leases[0].Attempts {
				got[i] = api.AttemptResult{AttemptID: attempt.ID, Result: attempt.Result}
			}
			if request.Leases[0].ID != fixture.id || !slices.Equal(got, fixture.want) {
				t.Errorf("completion for %s = %+v, want %+v", request.Leases[0].ID, got, fixture.want)
				http.Error(w, "invalid completion", http.StatusBadRequest)
				return
			}
			completions++
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected Scheduler call: %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected request", http.StatusBadRequest)
		}
	}))
	defer server.Close()
	cfg := config.New()
	cfg.PoolID, cfg.SuiteSlug, cfg.OrganizationSlug = "pool", "suite", "org"
	cfg.ServerBaseURL, cfg.AccessToken, cfg.OIDC = server.URL, "header.payload.signature", false
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	err = command.PoolExec(ctx, &cfg, "", []string{"ruby", "-I", filepath.Join(root, "lib"), filepath.Join(root, "exe/buildkite-rspec"), "--format", "documentation"}, 0, runnerexec.Options{StartupTimeout: 5 * time.Second, BatchTimeout: 5 * time.Second, ShutdownTimeout: 5 * time.Second})
	if err == nil || err.Error() != "pool execution: passed attempts: 2; failed attempts: 2; errored attempts: 1" {
		t.Fatalf("unexpected pool outcome: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	// The last lease's prefetch, if any, finds the pool consumed before the
	// ordinary request after accounting confirms it.
	if acquisitions < len(leases)+1 || acquisitions > len(leases)+2 || completions != len(leases) || prefetches == 0 {
		t.Fatalf("acquisitions=%d completions=%d prefetches=%d", acquisitions, completions, prefetches)
	}
}
