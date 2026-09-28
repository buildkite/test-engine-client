//go:build !windows && integration

package command

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/buildkite/test-engine-client/v3/internal/config"
	"github.com/buildkite/test-engine-client/v3/internal/debug"
	"github.com/buildkite/test-engine-client/v3/internal/runnerexec"
)

func TestPoolRSpecIntegration(t *testing.T) {
	root := os.Getenv("BKTEC_RSPEC_RUNNER_ROOT")
	if root == "" {
		t.Fatal("set BKTEC_RSPEC_RUNNER_ROOT to the bktest/test-collector-ruby directory")
	}
	previousDebug := debug.Enabled
	debug.SetDebug(true)
	t.Cleanup(func() { debug.SetDebug(previousDebug) })
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "spec"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "spec/rails_helper.rb"), []byte("require 'buildkite/test_collector'\nBuildkite::TestCollector.configure(hook: :rspec, token: nil)\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "spec/sample_spec.rb"), []byte("RSpec.describe('pool') { it('executes') { expect(true).to eq(true) } }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "spec/shared_examples.rb"), []byte("RSpec.shared_examples('shared') { it('executes') { expect(true).to eq(true) } }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "spec/shared_spec.rb"), []byte("require_relative 'shared_examples'\nRSpec.describe('including') { it_behaves_like 'shared' }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })
	var mu sync.Mutex
	acquisitions, completions := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		base := "/v2/organizations/org/test-scheduler/pools/pool"
		switch {
		case r.Method == http.MethodGet && r.URL.Path == base:
			state := "consuming"
			if completions > 0 {
				state = "consumed"
			}
			fmt.Fprintf(w, `{"id":"pool","state":%q,"muted_tests":[]}`, state)
		case r.Method == http.MethodPost && r.URL.Path == base+"/leases":
			acquisitions++
			if acquisitions == 1 {
				fmt.Fprint(w, `{"lease":null,"pool":{"id":"pool","state":"consuming"}}`)
			} else if acquisitions == 2 {
				fmt.Fprintf(w, `{"lease":{"id":"lease","expires_at":%q,"attempts":[{"id":"attempt","selector_type":"test_plan_test_case_v1","selector":{"format":"file","path":"spec/sample_spec.rb"}},{"id":"shared","selector_type":"test_plan_test_case_v1","selector":{"format":"file","path":"spec/shared_spec.rb"}}]},"pool":{"id":"pool","state":"consuming"}}`, time.Now().Add(10*time.Minute).UTC().Format(time.RFC3339Nano))
			} else {
				fmt.Fprint(w, `{"lease":null,"pool":{"id":"pool","state":"consumed"}}`)
			}
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
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil || len(request.Leases) != 1 || request.Leases[0].ID != "lease" || len(request.Leases[0].Attempts) != 2 || request.Leases[0].Attempts[0].ID != "attempt" || request.Leases[0].Attempts[0].Result != "passed" || request.Leases[0].Attempts[1].ID != "shared" || request.Leases[0].Attempts[1].Result != "passed" {
				t.Errorf("unexpected completion: %+v, %v", request, err)
				http.Error(w, "invalid completion", http.StatusBadRequest)
				return
			}
			completions++
			fmt.Fprint(w, `{"leases":[{"lease_id":"lease","attempts":[{"id":"attempt","result":"passed","completion_status":"completed"},{"id":"shared","result":"passed","completion_status":"completed"}]}]}`)
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
	err = PoolExec(ctx, &cfg, "", []string{"ruby", "-I", filepath.Join(root, "lib"), filepath.Join(root, "exe/buildkite-rspec"), "--format", "documentation"}, 0, runnerexec.Options{StartupTimeout: 5 * time.Second, BatchTimeout: 5 * time.Second, ShutdownTimeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if acquisitions != 3 || completions != 1 {
		t.Fatalf("acquisitions=%d completions=%d", acquisitions, completions)
	}
}
