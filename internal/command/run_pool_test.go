package command

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/buildkite/test-engine-client/v2/internal/api"
	"github.com/buildkite/test-engine-client/v2/internal/config"
	"github.com/buildkite/test-engine-client/v2/internal/plan"
	"github.com/google/go-cmp/cmp"
)

func setPoolTimings(t *testing.T, resolveTimeout, resolveInterval, idleInterval time.Duration) {
	t.Helper()
	origResolveTimeout := poolResolveTimeout
	origResolveInterval := poolResolveInterval
	origIdleInterval := poolIdleInterval

	poolResolveTimeout = resolveTimeout
	poolResolveInterval = resolveInterval
	poolIdleInterval = idleInterval

	t.Cleanup(func() {
		poolResolveTimeout = origResolveTimeout
		poolResolveInterval = origResolveInterval
		poolIdleInterval = origIdleInterval
	})
}

func TestTestCasesFromLeaseEntries(t *testing.T) {
	entries := []api.SchedulerLeaseEntry{
		{ID: "entry-1", Type: "file", Selector: json.RawMessage(`{"path": "spec/foo_spec.rb"}`)},
		{ID: "entry-2", Type: "example", Selector: json.RawMessage(`{"path": "spec/bar_spec.rb[1:2]"}`)},
	}

	got, err := testCasesFromLeaseEntries(entries, "")
	if err != nil {
		t.Fatalf("testCasesFromLeaseEntries() error = %v", err)
	}

	want := []plan.TestCase{
		{Path: "spec/foo_spec.rb"},
		{Path: "spec/bar_spec.rb[1:2]"},
	}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("testCasesFromLeaseEntries() diff (-got +want):\n%s", diff)
	}
}

func TestTestCasesFromLeaseEntries_LocationPrefix(t *testing.T) {
	entries := []api.SchedulerLeaseEntry{
		{ID: "entry-1", Type: "file", Selector: json.RawMessage(`{"path": "package/spec/foo_spec.rb"}`)},
	}

	got, err := testCasesFromLeaseEntries(entries, "package/")
	if err != nil {
		t.Fatalf("testCasesFromLeaseEntries() error = %v", err)
	}

	want := []plan.TestCase{{Path: "spec/foo_spec.rb"}}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("testCasesFromLeaseEntries() diff (-got +want):\n%s", diff)
	}
}

func TestTestCasesFromLeaseEntries_UnknownType(t *testing.T) {
	entries := []api.SchedulerLeaseEntry{
		{ID: "entry-1", Type: "mystery", Selector: json.RawMessage(`{"path": "spec/foo_spec.rb"}`)},
	}

	_, err := testCasesFromLeaseEntries(entries, "")
	if err == nil {
		t.Fatal("testCasesFromLeaseEntries() error = nil, want error")
	}

	if want := `unsupported test pool entry type "mystery"`; !strings.Contains(err.Error(), want) {
		t.Errorf("testCasesFromLeaseEntries() error = %q, want substring %q", err.Error(), want)
	}
}

func TestTestCasesFromLeaseEntries_MissingPath(t *testing.T) {
	entries := []api.SchedulerLeaseEntry{
		{ID: "entry-1", Type: "file", Selector: json.RawMessage(`{}`)},
	}

	_, err := testCasesFromLeaseEntries(entries, "")
	if err == nil {
		t.Fatal("testCasesFromLeaseEntries() error = nil, want error")
	}

	if want := "has no path in its selector"; !strings.Contains(err.Error(), want) {
		t.Errorf("testCasesFromLeaseEntries() error = %q, want substring %q", err.Error(), want)
	}
}

func TestResolveSchedulerPool_PollsUntilFound(t *testing.T) {
	setPoolTimings(t, 5*time.Second, 1*time.Millisecond, 1*time.Millisecond)

	var requestCount atomic.Int32
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if requestCount.Add(1) < 3 {
			_, _ = io.WriteString(w, `{"pools": []}`)
			return
		}
		_, _ = io.WriteString(w, `{"pools": [{"id": "pool-uuid", "key": "default", "state": "consuming"}]}`)
	}))
	defer svr.Close()

	client := api.NewClient(api.ClientConfig{
		AccessToken:      "oidc-token",
		OrganizationSlug: "buildkite",
		ServerBaseURL:    svr.URL,
	})
	claims := api.OIDCClaims{PipelineID: "pipeline-uuid", BuildID: "build-uuid"}

	pool, err := resolveSchedulerPool(context.Background(), client, claims, "default")
	if err != nil {
		t.Fatalf("resolveSchedulerPool() error = %v", err)
	}

	if pool.ID != "pool-uuid" {
		t.Errorf("resolveSchedulerPool() pool.ID = %q, want %q", pool.ID, "pool-uuid")
	}
	if got := requestCount.Load(); got != 3 {
		t.Errorf("http request count = %d, want 3", got)
	}
}

func TestResolveSchedulerPool_Timeout(t *testing.T) {
	setPoolTimings(t, 5*time.Millisecond, 1*time.Millisecond, 1*time.Millisecond)

	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = io.WriteString(w, `{"pools": []}`)
	}))
	defer svr.Close()

	client := api.NewClient(api.ClientConfig{
		AccessToken:      "oidc-token",
		OrganizationSlug: "buildkite",
		ServerBaseURL:    svr.URL,
	})
	claims := api.OIDCClaims{PipelineID: "pipeline-uuid", BuildID: "build-uuid"}

	_, err := resolveSchedulerPool(context.Background(), client, claims, "default")
	if err == nil {
		t.Fatal("resolveSchedulerPool() error = nil, want timeout error")
	}

	if want := `timed out`; !strings.Contains(err.Error(), want) {
		t.Errorf("resolveSchedulerPool() error = %q, want substring %q", err.Error(), want)
	}
}

// newPoolServer returns an httptest server simulating a Test Scheduler pool
// that hands out one lease containing the given test file, then reports
// drained. The returned counters record lease and complete request counts.
func newPoolServer(t *testing.T, testPath string) (svr *httptest.Server, leaseCount *atomic.Int32, completeCount *atomic.Int32) {
	t.Helper()
	leaseCount = new(atomic.Int32)
	completeCount = new(atomic.Int32)

	svr = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		switch r.URL.Path {
		case "/v2/organizations/buildkite/test-scheduler/pools":
			_, _ = io.WriteString(w, `{"pools": [{"id": "pool-uuid", "key": "default", "state": "consuming"}]}`)
		case "/v2/organizations/buildkite/test-scheduler/pools/pool-uuid/leases":
			if leaseCount.Add(1) == 1 {
				fmt.Fprintf(w, `{
					"lease": {
						"id": "lease-uuid",
						"expires_at": "2026-06-12T00:05:00Z",
						"entries": [{"id": "entry-1", "type": "file", "selector": {"path": %q}, "custom_cost": 1, "priority": 1, "meta_data": null}]
					}
				}`, testPath)
				return
			}
			_, _ = io.WriteString(w, `{"lease": null}`)
		case "/v2/organizations/buildkite/test-scheduler/pools/pool-uuid/leases/complete":
			completeCount.Add(1)
			_, _ = io.WriteString(w, `{"completed_entry_ids": ["entry-1"]}`)
		case "/v2/organizations/buildkite/test-scheduler/pools/pool-uuid/metrics":
			_, _ = io.WriteString(w, `{"metrics": {"waiting_entries_count": 0, "leased_entries_count": 0, "completed_entries_count": 1, "total_entries_count": 1, "oldest_waiting_entry_created_at": null, "waiting_custom_cost_sum": 0, "drained": true}}`)
		default:
			t.Errorf("unexpected request path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	return svr, leaseCount, completeCount
}

func getPoolRunConfig(t *testing.T, serverURL string) *config.Config {
	t.Helper()
	cfg := config.New()
	cfg.OrganizationSlug = "buildkite"
	cfg.SuiteSlug = "rspec"
	cfg.AccessToken = "asdf1234"
	cfg.ServerBaseURL = serverURL
	cfg.PoolName = "default"
	cfg.TargetCostLimit = 30
	cfg.TestRunner = "rspec"
	cfg.TestCommand = "rspec --format json --out {{resultPath}}"
	cfg.ResultPath = "tmp/rspec-pool.json"
	cfg.OIDC = true
	cfg.BuildkiteAgentCommand = fakeBuildkiteAgent(t, makeOIDCToken(t, schedulerClaims(t)))
	t.Cleanup(func() {
		os.Remove("tmp/rspec-pool.json")
	})
	return &cfg
}

func TestRunTestSchedulerPool(t *testing.T) {
	setPoolTimings(t, 5*time.Second, 1*time.Millisecond, 1*time.Millisecond)

	svr, leaseCount, completeCount := newPoolServer(t, "./testdata/rspec/spec/fruits/apple_spec.rb")
	defer svr.Close()

	cfg := getPoolRunConfig(t, svr.URL)

	err := RunTestSchedulerPool(context.Background(), cfg)
	if err != nil {
		t.Errorf("RunTestSchedulerPool() error = %v", err)
	}

	if got := leaseCount.Load(); got != 2 {
		t.Errorf("lease request count = %d, want 2", got)
	}
	if got := completeCount.Load(); got != 1 {
		t.Errorf("complete request count = %d, want 1", got)
	}
}

func TestRunTestSchedulerPool_FailedTests(t *testing.T) {
	setPoolTimings(t, 5*time.Second, 1*time.Millisecond, 1*time.Millisecond)

	svr, _, completeCount := newPoolServer(t, "./testdata/rspec/spec/fruits/tomato_spec.rb")
	defer svr.Close()

	cfg := getPoolRunConfig(t, svr.URL)

	err := RunTestSchedulerPool(context.Background(), cfg)
	if err == nil {
		t.Fatal("RunTestSchedulerPool() error = nil, want test failure error")
	}

	// The error wraps the runner's exec.ExitError so the process exits with
	// the same code as a static-split run would.
	exitError := new(exec.ExitError)
	if !errors.As(err, &exitError) {
		t.Fatalf("RunTestSchedulerPool() error = %v, want wrapped exec.ExitError", err)
	}
	if exitError.ExitCode() != 1 {
		t.Errorf("RunTestSchedulerPool() exit code = %d, want 1", exitError.ExitCode())
	}

	// Failed batches still complete their lease; failure is reported via the
	// exit code, not by leaving entries leased.
	if got := completeCount.Load(); got != 1 {
		t.Errorf("complete request count = %d, want 1", got)
	}
}
