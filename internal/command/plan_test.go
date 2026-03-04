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

	// Capture stderr
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	origStderr := os.Stderr
	os.Stderr = stderrW
	t.Cleanup(func() {
		os.Stderr = origStderr
	})

	// This is the method under test
	planErr := Plan(ctx, cfg, "", PlanOutputJSON, "")

	// Close the write end so we can read
	stderrW.Close()
	var stderrBuf bytes.Buffer
	io.Copy(&stderrBuf, stderrR)
	stderrR.Close()

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
	stderrOutput := stderrBuf.String()
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

	// Capture stderr
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	origStderr := os.Stderr
	os.Stderr = stderrW
	t.Cleanup(func() {
		os.Stderr = origStderr
	})

	// This is the method under test
	planErr := Plan(ctx, cfg, "", PlanOutputPipelineUpload, "testtemplate.yml")

	// Close the write end so we can read
	stderrW.Close()
	var stderrBuf bytes.Buffer
	io.Copy(&stderrBuf, stderrR)
	stderrR.Close()

	if planErr != nil {
		t.Errorf("command.Plan(...) error = %v", planErr)
	}

	// Verify pipeline upload was NOT executed (stdout buffer should have no "SHOULD_NOT_RUN")
	got := buf.String()
	if got != "" {
		t.Errorf("expected no pipeline upload output, got: %s", got)
	}

	// Verify warning was logged to stderr
	stderrOutput := stderrBuf.String()
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
			w.WriteHeader(http.StatusNotFound)
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
				Tasks:       map[string]*plan.Task{},
			}
			enc.Encode(testPlan)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	return svr
}

func getHttptestServer() *httptest.Server {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusNotFound)
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
