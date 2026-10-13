//go:build !windows

package command

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/buildkite/test-engine-client/v3/internal/api"
	"github.com/buildkite/test-engine-client/v3/internal/config"
	"github.com/buildkite/test-engine-client/v3/internal/plan"
	"github.com/buildkite/test-engine-client/v3/internal/runner"
	"github.com/buildkite/test-engine-client/v3/internal/runnerexec"
	"github.com/urfave/cli/v3"
)

// TestPoolConsumeJest runs real Jest commands against a fake Scheduler: a new
// command per lease and per local retry round, with muting and accounting.
func TestPoolConsumeJest(t *testing.T) {
	jest, err := filepath.Abs(filepath.Join("..", "runner", "testdata", "node_modules", ".bin", "jest"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, source := range map[string]string{
		"jest.config.js": `module.exports = {}`,
		// Fails on its first run only, so the local retry passes it.
		"flaky.test.js": `const fs = require('fs')
describe('flaky', () => {
  test('passes on retry', () => {
    const marker = __dirname + '/flaky.ran'
    if (!fs.existsSync(marker)) {
      fs.writeFileSync(marker, '')
      throw new Error('first run')
    }
  })
})`,
		"muted.test.js": `describe('muted', () => { test('fails', () => { throw new Error('muted') }) })`,
		// Top-level tests with the same name in two files: retries must match
		// each by file and run it again rather than report it skipped.
		"a.test.js": `test('renders', () => { throw new Error('a') })
test('renders twice', () => {})`,
		"b.test.js":      `test('renders', () => { throw new Error('b') })`,
		"broken.test.js": `boom()`,
		"ok.test.js":     `test('works', () => {})`,
		// A muted failure must not hide an error outside the file's tests.
		"hook.test.js": `describe('muted', () => {
  afterAll(() => { throw new Error('afterAll') })
  test('fails', () => { throw new Error('muted') })
})`,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(dir)

	attempt := func(id, path string) api.LeaseAttempt {
		return api.LeaseAttempt{ID: id, SelectorType: "test_plan_test_case_v1", Selector: plan.TestCase{Format: plan.TestCaseFormatSelector, Value: path}}
	}
	leases := [][]api.LeaseAttempt{
		{attempt("flaky", "flaky.test.js"), attempt("muted", "muted.test.js")},
		{attempt("a", "a.test.js"), attempt("b", "b.test.js")},
		// A file that doesn't load errors only its own attempt.
		{attempt("broken", "broken.test.js"), attempt("ok", "ok.test.js")},
		{attempt("hook", "hook.test.js")},
	}
	var mu sync.Mutex
	acquired, uploads := 0, 0
	completed := map[string][]api.AttemptResult{}
	names := map[string]string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		base := "/v2/organizations/org/test-scheduler/pools/existing"
		switch {
		case r.Method == http.MethodGet && r.URL.Path == base:
			fmt.Fprint(w, `{"id":"existing","state":"consuming","muted_tests":[{"scope":"muted","name":"fails"}]}`)
		case r.Method == http.MethodPost && r.URL.Path == base+"/leases":
			if acquired == len(leases) {
				fmt.Fprint(w, `{"lease":null,"pool":{"state":"consumed"}}`)
				return
			}
			// The Scheduler uses the idempotency key as the lease ID.
			key := r.Header.Get("Idempotency-Key")
			names[key] = fmt.Sprintf("lease-%d", acquired+1)
			lease := api.Lease{ID: key, ExpiresAt: time.Now().Add(time.Minute), Attempts: leases[acquired]}
			acquired++
			_ = json.NewEncoder(w).Encode(map[string]any{"lease": lease, "pool": map[string]string{"state": "consuming"}})
		case r.Method == http.MethodPost && r.URL.Path == base+"/leases/complete":
			var body struct {
				Leases []struct {
					ID       string              `json:"lease_id"`
					Attempts []api.AttemptResult `json:"attempts"`
				} `json:"leases"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Leases) != 1 {
				t.Errorf("completion=%+v err=%v", body, err)
			}
			completed[names[body.Leases[0].ID]] = body.Leases[0].Attempts
			fmt.Fprint(w, `{}`)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/uploads":
			if r.Header.Get("Authorization") != "Token token=upload-token" {
				t.Errorf("upload authorization=%q", r.Header.Get("Authorization"))
			}
			uploads++
			fmt.Fprint(w, `{}`)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected request", http.StatusNotFound)
		}
	}))
	defer server.Close()

	cfg := config.New()
	cfg.PoolID, cfg.OrganizationSlug, cfg.SuiteSlug, cfg.TestRunner = "existing", "org", "suite", "jest"
	cfg.ServerBaseURL, cfg.UploadBaseURL, cfg.AccessToken, cfg.OIDC = server.URL, server.URL, "header.payload.signature", false
	cfg.ResultPath = filepath.Join(dir, "jest.json")
	cfg.UploadResults, cfg.UploadToken = true, "upload-token"
	cfg.TestCommand = jest + " --runTestsByPath {{testExamples}} --json --testLocationInResults --outputFile {{resultPath}}"
	cfg.RetryCommand = jest + " --runTestsByPath {{testExamples}} --testNamePattern '{{testNamePattern}}' --json --testLocationInResults --outputFile {{resultPath}}"
	err = PoolConsume(context.Background(), &cfg, "", 1)

	want := map[string][]api.AttemptResult{
		"lease-1": {{AttemptID: "flaky", Result: "passed"}, {AttemptID: "muted", Result: "passed"}},
		"lease-2": {{AttemptID: "a", Result: "failed"}, {AttemptID: "b", Result: "failed"}},
		"lease-3": {{AttemptID: "broken", Result: "errored"}, {AttemptID: "ok", Result: "passed"}},
		"lease-4": {{AttemptID: "hook", Result: "errored"}},
	}
	if !reflect.DeepEqual(completed, want) {
		t.Errorf("completed=%v, want %v", completed, want)
	}
	// Each lease runs once and lease 1 and 2 retry their failed tests once;
	// bktec uploads the report of every command.
	if uploads != 6 {
		t.Errorf("uploads=%d, want 6", uploads)
	}
	var coded cli.ExitCoder
	if err == nil || errors.As(err, &coded) || !strings.Contains(err.Error(), "passed attempts: 3; failed attempts: 2; errored attempts: 2") {
		t.Fatalf("err=%v, want an internal error counting every attempt", err)
	}
}

// TestPoolConsumePlaywright runs real Playwright commands with two projects
// and a setup project against a fake Scheduler. Each leased file runs in both
// projects, and its attempt accounts for both.
func TestPoolConsumePlaywright(t *testing.T) {
	modules, err := filepath.Abs(filepath.Join("..", "runner", "testdata", "node_modules"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Symlink(modules, filepath.Join(dir, "node_modules")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "tests"), 0700); err != nil {
		t.Fatal(err)
	}
	for name, source := range map[string]string{
		// Spec files are reported relative to testDir, not the working directory.
		"playwright.config.js": `module.exports = {
  testDir: './tests',
  reporter: [['json', { outputFile: './results.json' }]],
  projects: [
    { name: 'setup', testMatch: /.*\.setup\.js/ },
    { name: 'alpha', dependencies: ['setup'] },
    { name: 'beta', dependencies: ['setup'] },
  ],
}`,
		// Setup runs with every command, whichever files are leased.
		"tests/auth.setup.js": `const { test } = require('@playwright/test')
test('authenticate', () => {
  if (require('fs').existsSync(__dirname + '/../fail-setup')) throw new Error('setup')
})`,
		// Fails in beta on its first run only, so the local retry passes it.
		"tests/flaky.spec.js": `const { test } = require('@playwright/test')
test('passes on retry', ({}, info) => {
  const marker = __dirname + '/../flaky.' + info.project.name
  if (info.project.name === 'beta' && !require('fs').existsSync(marker)) {
    require('fs').writeFileSync(marker, '')
    throw new Error('first run')
  }
})`,
		// Muted only in beta.
		"tests/muted.spec.js": `const { test } = require('@playwright/test')
test('fails', ({}, info) => { if (info.project.name === 'beta') throw new Error('muted') })`,
		"tests/failed.spec.js": `const { test } = require('@playwright/test')
test.describe('group', () => {
  test('fails', ({}, info) => { if (info.project.name === 'alpha') throw new Error('alpha') })
})`,
		// The first test fails on its first run only, which skips the second.
		// The second test never runs until the first passes, so a passing retry
		// of the first must not pass the file.
		"tests/serial.spec.js": `const { test } = require('@playwright/test')
test.describe.serial('serial', () => {
  test('flaky', ({}, info) => {
    const marker = __dirname + '/../serial.' + info.project.name
    if (!require('fs').existsSync(marker)) {
      require('fs').writeFileSync(marker, '')
      throw new Error('first run')
    }
  })
  test('fails', () => { throw new Error('fails') })
})`,
		"tests/broken.spec.js": `boom()`,
		// A test skipped on purpose doesn't affect the file's attempt.
		"tests/ok.spec.js": `const { test } = require('@playwright/test')
test('works', () => {})
test.skip('not yet', () => {})`,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(dir)

	attempt := func(id, path string) api.LeaseAttempt {
		return api.LeaseAttempt{ID: id, SelectorType: "test_plan_test_case_v1", Selector: plan.TestCase{Format: plan.TestCaseFormatSelector, Value: path}}
	}
	leases := [][]api.LeaseAttempt{
		{attempt("flaky", "tests/flaky.spec.js"), attempt("muted", "tests/muted.spec.js"), attempt("serial", "tests/serial.spec.js")},
		// A failure in one project fails the test's attempt.
		{attempt("failed", "tests/failed.spec.js"), attempt("ok", "tests/ok.spec.js")},
		// Playwright runs no tests when a file doesn't load.
		{attempt("broken", "tests/broken.spec.js"), attempt("ok-after-broken", "tests/ok.spec.js")},
		// A failed setup skips the tests that depend on it.
		{attempt("ok-after-setup", "tests/ok.spec.js")},
	}
	var mu sync.Mutex
	acquired := 0
	completed := map[string][]api.AttemptResult{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		base := "/v2/organizations/org/test-scheduler/pools/existing"
		switch {
		case r.Method == http.MethodGet && r.URL.Path == base:
			fmt.Fprint(w, `{"id":"existing","state":"consuming","muted_tests":[{"scope":" beta muted.spec.js fails","name":"fails"}]}`)
		case r.Method == http.MethodPost && r.URL.Path == base+"/leases":
			if acquired == len(leases) {
				fmt.Fprint(w, `{"lease":null,"pool":{"state":"consumed"}}`)
				return
			}
			if acquired == 3 {
				if err := os.WriteFile(filepath.Join(dir, "fail-setup"), nil, 0600); err != nil {
					t.Error(err)
				}
			}
			lease := api.Lease{ID: fmt.Sprintf("lease-%d", acquired+1), ExpiresAt: time.Now().Add(time.Minute), Attempts: leases[acquired]}
			acquired++
			_ = json.NewEncoder(w).Encode(map[string]any{"lease": lease, "pool": map[string]string{"state": "consuming"}})
		case r.Method == http.MethodPost && r.URL.Path == base+"/leases/complete":
			var body struct {
				Leases []struct {
					ID       string              `json:"lease_id"`
					Attempts []api.AttemptResult `json:"attempts"`
				} `json:"leases"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Leases) != 1 {
				t.Errorf("completion=%+v err=%v", body, err)
			}
			completed[body.Leases[0].ID] = body.Leases[0].Attempts
			fmt.Fprint(w, `{}`)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected request", http.StatusNotFound)
		}
	}))
	defer server.Close()

	cfg := config.New()
	cfg.PoolID, cfg.OrganizationSlug, cfg.SuiteSlug, cfg.TestRunner = "existing", "org", "suite", "playwright"
	cfg.ServerBaseURL, cfg.AccessToken, cfg.OIDC = server.URL, "header.payload.signature", false
	cfg.ResultPath = filepath.Join(dir, "results.json")
	cfg.TestCommand = filepath.Join(modules, ".bin", "playwright") + " test"
	err = PoolConsume(context.Background(), &cfg, "", 1)

	want := map[string][]api.AttemptResult{
		"lease-1": {{AttemptID: "flaky", Result: "passed"}, {AttemptID: "muted", Result: "passed"}, {AttemptID: "serial", Result: "errored"}},
		"lease-2": {{AttemptID: "failed", Result: "failed"}, {AttemptID: "ok", Result: "passed"}},
		"lease-3": {{AttemptID: "broken", Result: "errored"}, {AttemptID: "ok-after-broken", Result: "errored"}},
		"lease-4": {{AttemptID: "ok-after-setup", Result: "errored"}},
	}
	if !reflect.DeepEqual(completed, want) {
		t.Errorf("completed=%v, want %v", completed, want)
	}
	if err == nil || !strings.Contains(err.Error(), "passed attempts: 3; failed attempts: 1; errored attempts: 4") {
		t.Fatalf("err=%v, want every attempt counted", err)
	}
}

func TestRunPoolCommandRequiresUsableReport(t *testing.T) {
	for _, tc := range []struct {
		name, script string
		wantStatus   string
	}{
		{"report with failures", `printf '%s' '{"testResults":[{"name":"a.test.js","status":"failed","assertionResults":[{"status":"failed","title":"t","ancestorTitles":[]}]}]}' > "$2"; exit 1`, "completed"},
		{"no report", `exit 0`, "errored"},
		// Like bktec run, Jest failing outside its tests, such as on a coverage
		// threshold, fails even when every test passed.
		{"non-zero exit without failures", `printf '%s' '{"testResults":[{"name":"a.test.js","status":"passed","assertionResults":[{"status":"passed","title":"t","ancestorTitles":[]}]}]}' > "$2"; exit 1`, "errored"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resultPath := filepath.Join(t.TempDir(), "jest.json")
			// A previous lease's report must not be reused.
			if err := os.WriteFile(resultPath, []byte(`{"testResults":[]}`), 0600); err != nil {
				t.Fatal(err)
			}
			testRunner := runner.NewJest(runner.RunnerConfig{
				TestCommand: fmt.Sprintf("sh -c %q sh {{testExamples}} {{resultPath}}", tc.script),
				ResultPath:  resultPath,
			})
			batch := runnerexec.Batch{ID: "b_1", Tests: []plan.TestCase{{Format: plan.TestCaseFormatSelector, Value: "a.test.js"}}}
			if got := runPoolCommand(context.Background(), testRunner, batch); got.Status != tc.wantStatus {
				t.Fatalf("status=%q, want %q", got.Status, tc.wantStatus)
			}
		})
	}
}

// Playwright only stops its webServer on SIGINT, so commands are interrupted
// rather than terminated.
func TestRunPoolProcessInterruptsCommand(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "interrupted")
	cmd := exec.Command("sh", "-c", `trap 'touch "$1"; exit 1' INT; while :; do sleep 0.1; done`, "sh", marker)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_ = runPoolProcess(ctx, cmd)
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("command was not interrupted with SIGINT")
	}
}

// A wrapper such as npx exits on SIGTERM while the test runner it started
// keeps running, so stopping a command must stop its descendants too.
func TestRunPoolProcessStopsDescendants(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "still-running")
	cmd := exec.Command("sh", "-c", `(sleep 1; touch "$1") & wait`, "sh", marker)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := runPoolProcess(ctx, cmd); err == nil {
		t.Fatal("stopped command exited cleanly")
	}
	time.Sleep(1500 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("descendant kept running after the command was stopped")
	}
}
