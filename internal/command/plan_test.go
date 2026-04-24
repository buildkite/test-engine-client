package command

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/buildkite/test-engine-client/internal/api"
	"github.com/buildkite/test-engine-client/internal/config"
	"github.com/buildkite/test-engine-client/internal/debug"
	"github.com/buildkite/test-engine-client/internal/plan"
	"github.com/google/go-cmp/cmp"
)

func TestPlanJSON(t *testing.T) {
	svr := getHttptestServer()
	defer svr.Close()

	cfg := getConfig()
	cfg.ServerBaseUrl = svr.URL

	if err := cfg.ValidateForPlan(); err != nil {
		t.Errorf("Invalid config: %v", err)
	}

	ctx := context.Background()

	// By default command.Run writes to os.Stdout.
	// Replace with a string buffer here so we can test the command output.
	var buf bytes.Buffer
	setPlanWriter(t, &buf)

	// This is the method under test
	err := Plan(ctx, cfg, "", PlanOutputJSON, "")
	if err != nil {
		t.Errorf("command.Plan(...) error = %v", err)
	}

	want := `{"BUILDKITE_TEST_ENGINE_PLAN_IDENTIFIER":"facecafe","BUILDKITE_TEST_ENGINE_PARALLELISM":"42"}
`
	got := buf.String()

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("command.Plan(...) diff = %s", diff)
	}
}

func TestPlanPipelineUpload(t *testing.T) {
	svr := getHttptestServer()
	defer svr.Close()

	cfg := getConfig()
	cfg.ServerBaseUrl = svr.URL

	if err := cfg.ValidateForPlan(); err != nil {
		t.Errorf("Invalid config: %v", err)
	}

	ctx := context.Background()

	// By default command.Run writes to os.Stdout.
	// Replace with a string buffer here so we can test the command output.
	var buf bytes.Buffer
	setPlanWriter(t, &buf)

	// Set a dummy command and args to run instead of `buildkite-agent pipeline upload`
	setPipelineUploadCommand(t, "echo", "called", "with")

	// This is the method under test
	err := Plan(ctx, cfg, "", PlanOutputPipelineUpload, "testtemplate.yml")
	if err != nil {
		t.Errorf("command.Plan(...) error = %v", err)
	}

	want := `Executing buildkite-agent pipeline upload with BUILDKITE_TEST_ENGINE_PLAN_IDENTIFIER=facecafe BUILDKITE_TEST_ENGINE_PARALLELISM=42
called with testtemplate.yml
`
	got := buf.String()

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("command.Plan(...) diff = %s", diff)
	}
}

func TestPlanJSON_BillingError(t *testing.T) {
	// mock server to return 403 with a billing error
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message": "Billing Error: please update your plan"}`, http.StatusForbidden)
	}))
	defer svr.Close()

	cfg := getConfig()
	cfg.Identifier = "hello"
	cfg.MaxParallelism = 123
	cfg.ServerBaseUrl = svr.URL

	if err := cfg.ValidateForPlan(); err != nil {
		t.Errorf("Invalid config: %v", err)
	}

	ctx := context.Background()

	// By default command.Run writes to os.Stdout.
	// Replace with a string buffer here so we can test the command output.
	var buf bytes.Buffer
	setPlanWriter(t, &buf)

	// This is the method under test
	err := Plan(ctx, cfg, "", PlanOutputJSON, "")
	if err != nil {
		t.Errorf("command.Plan(...) error = %v", err)
	}

	want := `{"BUILDKITE_TEST_ENGINE_PLAN_IDENTIFIER":"hello","BUILDKITE_TEST_ENGINE_PARALLELISM":"123"}
`
	got := buf.String()

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("command.Plan(...) diff = %s", diff)
	}
}

func TestPlanJSON_InternalServerError(t *testing.T) {
	// mock server to return 500 Internal Server Error
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}))
	defer svr.Close()

	cfg := getConfig()
	cfg.Identifier = "hello"
	cfg.MaxParallelism = 123
	cfg.ServerBaseUrl = svr.URL

	if err := cfg.ValidateForPlan(); err != nil {
		t.Errorf("Invalid config: %v", err)
	}

	// set the fetch timeout to 1 second so we don't wait too long
	ctx := context.Background()
	fetchCtx, cancel := context.WithTimeout(ctx, 1*time.Millisecond)
	defer cancel()

	// By default command.Run writes to os.Stdout.
	// Replace with a string buffer here so we can test the command output.
	var buf bytes.Buffer
	setPlanWriter(t, &buf)

	// This is the method under test
	// Expecting it to return an error due to server TestPlan_InternalServerError(
	err := Plan(fetchCtx, cfg, "", PlanOutputJSON, "")
	if err != nil {
		t.Errorf("command.Plan(...) error = %v", err)
	}

	want := `{"BUILDKITE_TEST_ENGINE_PLAN_IDENTIFIER":"hello","BUILDKITE_TEST_ENGINE_PARALLELISM":"123"}
`
	got := buf.String()

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("command.Plan(...) diff = %s", diff)
	}
}

func TestPlanJSON_Parallelism0(t *testing.T) {
	svr := getZeroParallelismServer()
	defer svr.Close()

	cfg := getConfig()
	cfg.ServerBaseUrl = svr.URL

	if err := cfg.ValidateForPlan(); err != nil {
		t.Errorf("Invalid config: %v", err)
	}

	ctx := context.Background()

	var buf bytes.Buffer
	setPlanWriter(t, &buf)

	getStderr := captureStderr(t)

	// This is the method under test
	planErr := Plan(ctx, cfg, "", PlanOutputJSON, "")

	stderrOutput := getStderr()

	// Verify command exits successfully
	if planErr != nil {
		t.Errorf("command.Plan(...) error = %v", planErr)
	}

	// Verify JSON output on stdout still contains the expected keys
	want := `{"BUILDKITE_TEST_ENGINE_PLAN_IDENTIFIER":"facecafe","BUILDKITE_TEST_ENGINE_PARALLELISM":"0"}
`
	got := buf.String()

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("command.Plan(...) JSON output diff = %s", diff)
	}

	// Verify warning was logged to stderr
	if !strings.Contains(stderrOutput, "Parallelism is 0") {
		t.Errorf("expected stderr to contain parallelism warning, got: %s", stderrOutput)
	}
}

func TestPlanPipelineUpload_Parallelism0(t *testing.T) {
	svr := getZeroParallelismServer()
	defer svr.Close()

	cfg := getConfig()
	cfg.ServerBaseUrl = svr.URL

	if err := cfg.ValidateForPlan(); err != nil {
		t.Errorf("Invalid config: %v", err)
	}

	ctx := context.Background()

	var buf bytes.Buffer
	setPlanWriter(t, &buf)

	// Set a dummy command that records whether it was called.
	// If pipeline upload runs, we'll see its output in buf.
	setPipelineUploadCommand(t, "echo", "SHOULD_NOT_RUN")

	getStderr := captureStderr(t)

	// This is the method under test
	planErr := Plan(ctx, cfg, "", PlanOutputPipelineUpload, "testtemplate.yml")

	stderrOutput := getStderr()

	if planErr != nil {
		t.Errorf("command.Plan(...) error = %v", planErr)
	}

	// Verify pipeline upload was NOT executed (stdout buffer should have no "SHOULD_NOT_RUN")
	got := buf.String()
	if got != "" {
		t.Errorf("expected no pipeline upload output, got: %s", got)
	}

	// Verify warning was logged to stderr
	if !strings.Contains(stderrOutput, "Parallelism is 0") {
		t.Errorf("expected stderr to contain parallelism warning, got: %s", stderrOutput)
	}
}

func TestPlanPipelineUpload_InternalServerError(t *testing.T) {
	// mock server to return 500 Internal Server Error
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}))
	defer svr.Close()

	cfg := getConfig()
	cfg.Identifier = "hello"
	cfg.MaxParallelism = 123
	cfg.ServerBaseUrl = svr.URL

	if err := cfg.ValidateForPlan(); err != nil {
		t.Errorf("Invalid config: %v", err)
	}

	// set the fetch timeout to 1 second so we don't wait too long
	ctx := context.Background()
	fetchCtx, cancel := context.WithTimeout(ctx, 1*time.Millisecond)
	defer cancel()

	// By default command.Run writes to os.Stdout.
	// Replace with a string buffer here so we can test the command output.
	var buf bytes.Buffer
	setPlanWriter(t, &buf)

	// Set a dummy command and args to run instead of `buildkite-agent pipeline upload`
	setPipelineUploadCommand(t, "echo", "called", "with")

	// This is the method under test
	err := Plan(fetchCtx, cfg, "", PlanOutputPipelineUpload, "testtemplate.yml")
	if err != nil {
		t.Errorf("command.Plan(...) error = %v", err)
	}

	want := `Executing buildkite-agent pipeline upload with BUILDKITE_TEST_ENGINE_PLAN_IDENTIFIER=hello BUILDKITE_TEST_ENGINE_PARALLELISM=123
called with testtemplate.yml
`
	got := buf.String()

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("command.Plan(...) diff = %s", diff)
	}
}

func getZeroParallelismServer() *httptest.Server {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"message": "Not found"}`))
			return
		}

		enc := json.NewEncoder(w)

		switch r.URL.Path {
		case "/v2/analytics/organizations/buildkite/suites/rspec/test_plan/filter_tests":
			filteredTests := api.FilteredTestResponse{}
			enc.Encode(filteredTests)
		case "/v2/analytics/organizations/buildkite/suites/rspec/test_plan":
			testPlan := plan.TestPlan{
				Identifier:  "facecafe",
				Parallelism: 0,
				Tasks: map[string]*plan.Task{
					"0": {NodeNumber: 0, Tests: []plan.TestCase{{Path: "testdata/rspec/spec/fruits/apple_spec.rb"}}},
				},
			}
			enc.Encode(testPlan)
		default:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"message": "Not found"}`))
		}
	}))
	return svr
}

func getHttptestServer() *httptest.Server {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"message": "Not found"}`))
			return
		}

		enc := json.NewEncoder(w)

		switch r.URL.Path {
		case "/v2/analytics/organizations/buildkite/suites/rspec/test_plan/filter_tests":
			filteredTests := api.FilteredTestResponse{}
			enc.Encode(filteredTests)
		case "/v2/analytics/organizations/buildkite/suites/rspec/test_plan":
			testPlan := plan.TestPlan{
				Identifier:  "facecafe",
				Parallelism: 42,
				Tasks: map[string]*plan.Task{
					"0": {NodeNumber: 0, Tests: []plan.TestCase{{Path: "testdata/rspec/spec/fruits/apple_spec.rb"}}},
				},
			}
			enc.Encode(testPlan)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	return svr
}

func getConfig() *config.Config {
	cfg := config.New()

	cfg.Branch = "tet-123-add-branch-name"
	cfg.BuildId = "123"
	cfg.StepId = "789"
	cfg.OrganizationSlug = "buildkite"
	cfg.NodeIndex = 0
	cfg.Parallelism = 3
	cfg.AccessToken = "asdf1234"
	cfg.SuiteSlug = "rspec"
	cfg.TestRunner = "rspec"
	cfg.ResultPath = "out.json"
	cfg.DebugEnabled = true
	cfg.TestFilePattern = "testdata/rspec/spec/**/*_spec.rb"

	return &cfg
}

func setPlanWriter(t *testing.T, w io.Writer) {
	t.Helper()
	origWriter := planWriter
	planWriter = w

	t.Cleanup(func() {
		planWriter = origWriter
	})
}

func setPipelineUploadCommand(t *testing.T, cmd string, args ...string) {
	t.Helper()
	origCommand := pipelineUploadCommand
	origArgs := pipelineUploadArgs

	pipelineUploadCommand = cmd
	pipelineUploadArgs = args

	t.Cleanup(func() {
		pipelineUploadCommand = origCommand
		pipelineUploadArgs = origArgs
	})
}

// captureStderr redirects os.Stderr to a pipe and returns a function that,
// when called, closes the write end and returns everything written to stderr.
func captureStderr(t *testing.T) func() string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	orig := os.Stderr
	os.Stderr = w
	t.Cleanup(func() {
		os.Stderr = orig
	})
	return func() string {
		w.Close()
		var buf bytes.Buffer
		io.Copy(&buf, r)
		r.Close()
		return buf.String()
	}
}

// setDebugEnabled enables debug mode and directs debug output to the given writer.
// It restores the original state on test cleanup.
func setDebugEnabled(t *testing.T, w io.Writer) {
	t.Helper()
	origEnabled := debug.Enabled
	debug.SetDebug(true)
	debug.SetOutput(w)

	t.Cleanup(func() {
		debug.SetDebug(origEnabled)
		debug.SetOutput(os.Stdout) // default output
	})
}

func TestPlan_CollectGitMetadataWithoutSelection(t *testing.T) {
	// Capture the request body to verify metadata is sent
	var requestBody []byte
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		enc := json.NewEncoder(w)

		switch r.URL.Path {
		case "/v2/analytics/organizations/buildkite/suites/rspec/test_plan/filter_tests":
			enc.Encode(api.FilteredTestResponse{})
		case "/v2/analytics/organizations/buildkite/suites/rspec/test_plan":
			requestBody, _ = io.ReadAll(r.Body)
			enc.Encode(plan.TestPlan{
				Identifier:  "facecafe",
				Parallelism: 42,
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer svr.Close()

	cfg := getConfig()
	cfg.ServerBaseUrl = svr.URL
	cfg.CollectGitMetadata = true
	cfg.SelectionStrategy = "" // no selection

	ctx := context.Background()

	var buf bytes.Buffer
	setPlanWriter(t, &buf)

	getStderr := captureStderr(t)

	err := Plan(ctx, cfg, "", PlanOutputJSON, "")

	stderrOutput := getStderr()

	if err != nil {
		t.Fatalf("command.Plan(...) error = %v", err)
	}

	// The auto-collection should have been triggered. In a test environment
	// without a git repo, it will warn and skip, but the important thing is
	// that the code path was entered (the warning proves the gate was passed).
	if !strings.Contains(stderrOutput, "not a git repository") &&
		!strings.Contains(stderrOutput, "auto-detected base branch") {
		// If we're in a git repo (test runs inside a git checkout), we'll
		// see metadata in the request body instead.
		if len(requestBody) > 0 {
			var params map[string]interface{}
			if err := json.Unmarshal(requestBody, &params); err == nil {
				if metadata, ok := params["metadata"]; ok && metadata != nil {
					// Auto-collection ran and populated metadata -- gate worked
					return
				}
			}
		}
		t.Errorf("expected auto-collection to run (either git warning or metadata in request), stderr: %s", stderrOutput)
	}
}

func TestPlan_NoCollectGitMetadataByDefault(t *testing.T) {
	// Capture the request body to verify no metadata is sent
	var requestBody []byte
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		enc := json.NewEncoder(w)

		switch r.URL.Path {
		case "/v2/analytics/organizations/buildkite/suites/rspec/test_plan/filter_tests":
			enc.Encode(api.FilteredTestResponse{})
		case "/v2/analytics/organizations/buildkite/suites/rspec/test_plan":
			requestBody, _ = io.ReadAll(r.Body)
			enc.Encode(plan.TestPlan{
				Identifier:  "facecafe",
				Parallelism: 42,
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer svr.Close()

	cfg := getConfig()
	cfg.ServerBaseUrl = svr.URL
	cfg.CollectGitMetadata = false
	cfg.SelectionStrategy = ""

	ctx := context.Background()

	var buf bytes.Buffer
	setPlanWriter(t, &buf)

	getStderr := captureStderr(t)

	err := Plan(ctx, cfg, "", PlanOutputJSON, "")

	stderrOutput := getStderr()

	if err != nil {
		t.Fatalf("command.Plan(...) error = %v", err)
	}

	// Auto-collection should NOT have run -- no git warnings expected
	if strings.Contains(stderrOutput, "not a git repository") ||
		strings.Contains(stderrOutput, "auto-detected base branch") ||
		strings.Contains(stderrOutput, "skipping metadata auto-collection") {
		t.Errorf("auto-collection should not run when both SelectionStrategy and CollectGitMetadata are unset, stderr: %s", stderrOutput)
	}

	// Verify no metadata in request body
	if len(requestBody) > 0 {
		var params map[string]interface{}
		if err := json.Unmarshal(requestBody, &params); err == nil {
			if metadata, ok := params["metadata"]; ok && metadata != nil {
				t.Errorf("expected no metadata in request, got: %v", metadata)
			}
		}
	}
}

func TestPlanJSON_DebugLogging(t *testing.T) {
	svr := getHttptestServer()
	defer svr.Close()

	cfg := getConfig()
	cfg.ServerBaseUrl = svr.URL

	if err := cfg.ValidateForPlan(); err != nil {
		t.Errorf("Invalid config: %v", err)
	}

	ctx := context.Background()

	// Capture stdout (plan data)
	var stdoutBuf bytes.Buffer
	setPlanWriter(t, &stdoutBuf)

	getStderr := captureStderr(t)

	// Enable debug and direct debug output to stderr (same as main.go does)
	setDebugEnabled(t, os.Stderr)

	// This is the method under test
	planErr := Plan(ctx, cfg, "", PlanOutputJSON, "")

	stderrOutput := getStderr()

	if planErr != nil {
		t.Errorf("command.Plan(...) error = %v", planErr)
	}

	stdoutOutput := stdoutBuf.String()

	// Verify debug output includes message before API call
	if !strings.Contains(stderrOutput, "Creating test plan via API") {
		t.Errorf("expected stderr to contain 'Creating test plan via API', got: %s", stderrOutput)
	}

	// Verify debug output includes the returned plan identifier and parallelism
	if !strings.Contains(stderrOutput, `"facecafe"`) {
		t.Errorf("expected stderr to contain plan identifier 'facecafe', got: %s", stderrOutput)
	}
	if !strings.Contains(stderrOutput, "Parallelism: 42") {
		t.Errorf("expected stderr to contain 'Parallelism: 42', got: %s", stderrOutput)
	}

	// Verify debug output indicates this is NOT a fallback plan
	if !strings.Contains(stderrOutput, "Test plan created.") {
		t.Errorf("expected stderr to contain 'Test plan created.', got: %s", stderrOutput)
	}

	// Verify debug output is NOT in stdout
	if strings.Contains(stdoutOutput, "DEBUG") {
		t.Errorf("debug output should not appear in stdout, got: %s", stdoutOutput)
	}
	if strings.Contains(stdoutOutput, "Creating test plan via API") {
		t.Errorf("debug output should not appear in stdout, got: %s", stdoutOutput)
	}
}

func TestPlanJSON_DebugLogging_Fallback(t *testing.T) {
	// Mock server to return 500 Internal Server Error (triggers retry timeout and fallback)
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}))
	defer svr.Close()

	cfg := getConfig()
	cfg.Identifier = "hello"
	cfg.MaxParallelism = 10
	cfg.ServerBaseUrl = svr.URL

	if err := cfg.ValidateForPlan(); err != nil {
		t.Errorf("Invalid config: %v", err)
	}

	// Short timeout to trigger fallback quickly
	ctx := context.Background()
	fetchCtx, cancel := context.WithTimeout(ctx, 1*time.Millisecond)
	defer cancel()

	// Capture stdout (plan data)
	var stdoutBuf bytes.Buffer
	setPlanWriter(t, &stdoutBuf)

	getStderr := captureStderr(t)

	// Enable debug and direct debug output to stderr
	setDebugEnabled(t, os.Stderr)

	// This is the method under test
	planErr := Plan(fetchCtx, cfg, "", PlanOutputJSON, "")

	stderrOutput := getStderr()

	if planErr != nil {
		t.Errorf("command.Plan(...) error = %v", planErr)
	}

	stdoutOutput := stdoutBuf.String()

	// Verify debug output includes message before API call
	if !strings.Contains(stderrOutput, "Creating test plan via API") {
		t.Errorf("expected stderr to contain 'Creating test plan via API', got: %s", stderrOutput)
	}

	// Verify debug output indicates fallback plan was used
	if !strings.Contains(stderrOutput, "Using fallback plan.") {
		t.Errorf("expected stderr to contain 'Using fallback plan.', got: %s", stderrOutput)
	}

	// Verify debug output includes the fallback plan identifier and parallelism
	if !strings.Contains(stderrOutput, `"hello"`) {
		t.Errorf("expected stderr to contain fallback plan identifier 'hello', got: %s", stderrOutput)
	}
	if !strings.Contains(stderrOutput, "Parallelism: 10") {
		t.Errorf("expected stderr to contain 'Parallelism: 10', got: %s", stderrOutput)
	}

	// Verify debug output is NOT in stdout
	if strings.Contains(stdoutOutput, "DEBUG") {
		t.Errorf("debug output should not appear in stdout, got: %s", stdoutOutput)
	}
}
