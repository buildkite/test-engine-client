package command

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/buildkite/test-engine-client/v2/internal/api"
	"github.com/buildkite/test-engine-client/v2/internal/config"
	"github.com/buildkite/test-engine-client/v2/internal/debug"
	"github.com/buildkite/test-engine-client/v2/internal/plan"
	"github.com/google/go-cmp/cmp"
)

func TestPlanJSON(t *testing.T) {
	svr := getHttptestServer()
	defer svr.Close()

	cfg := getConfig()
	cfg.ServerBaseURL = svr.URL

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

func TestPlanPlanOut(t *testing.T) {
	// The server's exact response body, including a field the client struct
	// does not model (server_only), to prove --plan-out passes the response
	// through unmodified rather than re-marshalling a struct.
	serverBody := `{"identifier":"facecafe","parallelism":42,"experiment":"","tasks":{"0":{"node_number":0,"tests":[{"path":"testdata/rspec/spec/fruits/apple_spec.rb"}]}},"server_only":"kept"}`

	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		switch r.URL.Path {
		case "/v2/analytics/organizations/buildkite/suites/rspec/test_plan/filter_tests":
			json.NewEncoder(w).Encode(api.FilteredTestResponse{})
		case "/v2/analytics/organizations/buildkite/suites/rspec/test_plan":
			w.Write([]byte(serverBody))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer svr.Close()

	cfg := getConfig()
	cfg.ServerBaseURL = svr.URL
	cfg.PlanOut = "-"

	if err := cfg.ValidateForPlan(); err != nil {
		t.Errorf("Invalid config: %v", err)
	}

	ctx := context.Background()

	var buf bytes.Buffer
	setPlanWriter(t, &buf)

	// This is the method under test
	if err := Plan(ctx, cfg, "", PlanOutputPlanOut, ""); err != nil {
		t.Errorf("command.Plan(...) error = %v", err)
	}

	// stdout is the server's exact response, only re-indented. Comparing the
	// compacted output against the compacted server body proves no field was
	// added, dropped, or renamed.
	var gotCompact bytes.Buffer
	if err := json.Compact(&gotCompact, buf.Bytes()); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, buf.String())
	}
	if gotCompact.String() != serverBody {
		t.Errorf("--plan-out output was modified.\n got: %s\nwant: %s", gotCompact.String(), serverBody)
	}

	// Output should be indented, not compact.
	if !strings.Contains(buf.String(), "\n  \"identifier\"") {
		t.Errorf("expected indented JSON output, got:\n%s", buf.String())
	}
}

// --plan-out with a file path writes the plan to that file rather than stdout.
func TestPlanPlanOut_WritesToFile(t *testing.T) {
	serverBody := `{"identifier":"facecafe","parallelism":42,"tasks":{"0":{"node_number":0,"tests":[{"path":"a_spec.rb"}]}}}`

	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		switch r.URL.Path {
		case "/v2/analytics/organizations/buildkite/suites/rspec/test_plan/filter_tests":
			json.NewEncoder(w).Encode(api.FilteredTestResponse{})
		case "/v2/analytics/organizations/buildkite/suites/rspec/test_plan":
			w.Write([]byte(serverBody))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer svr.Close()

	outPath := filepath.Join(t.TempDir(), "plan.json")

	cfg := getConfig()
	cfg.ServerBaseURL = svr.URL
	cfg.PlanOut = outPath

	if err := cfg.ValidateForPlan(); err != nil {
		t.Errorf("Invalid config: %v", err)
	}

	// planWriter (stdout) must stay empty; the plan goes to the file.
	var stdout bytes.Buffer
	setPlanWriter(t, &stdout)

	if err := Plan(context.Background(), cfg, "", PlanOutputPlanOut, ""); err != nil {
		t.Errorf("command.Plan(...) error = %v", err)
	}

	if stdout.Len() != 0 {
		t.Errorf("expected no stdout output when writing to a file, got: %s", stdout.String())
	}

	contents, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("reading --plan-out file: %v", err)
	}

	var gotCompact bytes.Buffer
	if err := json.Compact(&gotCompact, contents); err != nil {
		t.Fatalf("file is not valid JSON: %v\ncontents: %s", err, contents)
	}
	if gotCompact.String() != serverBody {
		t.Errorf("--plan-out file was modified.\n got: %s\nwant: %s", gotCompact.String(), serverBody)
	}
}

func TestPlanPlanOut_Parallelism0(t *testing.T) {
	svr := getZeroParallelismServer()
	defer svr.Close()

	cfg := getConfig()
	cfg.ServerBaseURL = svr.URL
	cfg.PlanOut = "-"

	if err := cfg.ValidateForPlan(); err != nil {
		t.Errorf("Invalid config: %v", err)
	}

	ctx := context.Background()

	var buf bytes.Buffer
	setPlanWriter(t, &buf)

	getStderr := captureStderr(t)

	// This is the method under test
	planErr := Plan(ctx, cfg, "", PlanOutputPlanOut, "")

	stderrOutput := getStderr()

	if planErr != nil {
		t.Errorf("command.Plan(...) error = %v", planErr)
	}

	// The server plan is emitted verbatim, even at parallelism 0: the
	// identifier, parallelism, and the (empty) task breakdown are passed
	// through. --plan-out does not substitute a fallback for a well-formed
	// server response.
	var got plan.TestPlan
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, buf.String())
	}

	want := plan.TestPlan{
		Identifier:  "facecafe",
		Parallelism: 0,
		Tasks:       map[string]*plan.Task{},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("emitted plan diff (-want +got):\n%s", diff)
	}

	// The parallelism warning is written to stderr.
	if !strings.Contains(stderrOutput, "Parallelism is 0") {
		t.Errorf("expected stderr to contain parallelism warning, got: %s", stderrOutput)
	}
}

func TestPlanPipelineUpload(t *testing.T) {
	svr := getHttptestServer()
	defer svr.Close()

	cfg := getConfig()
	cfg.ServerBaseURL = svr.URL

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

// An off-agent `bktec plan` supplies --plan-identifier instead of
// BUILDKITE_BUILD_ID/BUILDKITE_STEP_ID. The identifier must flow through to the
// test_plan create request as the `identifier` param (the server cache key).
func TestPlan_PlanIdentifierFlowsToRequestParam(t *testing.T) {
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
				Identifier:  "01919f1e-0000-7000-8000-000000000000",
				Parallelism: 42,
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer svr.Close()

	cfg := getConfig()
	cfg.ServerBaseURL = svr.URL
	// An off-agent run has no build/step; identity comes from the identifier.
	cfg.BuildID = ""
	cfg.StepID = ""
	cfg.Identifier = "01919f1e-0000-7000-8000-000000000000"

	if err := cfg.ValidateForPlan(); err != nil {
		t.Fatalf("ValidateForPlan() error = %v", err)
	}

	ctx := context.Background()

	var buf bytes.Buffer
	setPlanWriter(t, &buf)

	if err := Plan(ctx, cfg, "", PlanOutputJSON, ""); err != nil {
		t.Fatalf("command.Plan(...) error = %v", err)
	}

	var params map[string]any
	if err := json.Unmarshal(requestBody, &params); err != nil {
		t.Fatalf("failed to unmarshal request body %q: %v", requestBody, err)
	}

	if got, want := params["identifier"], "01919f1e-0000-7000-8000-000000000000"; got != want {
		t.Errorf("request identifier = %v, want %v", got, want)
	}
}

// Supplying an identifier lets `ValidateForPlan` skip the BUILDKITE_BUILD_ID /
// BUILDKITE_STEP_ID blank guards, so an off-agent run needn't fake build env vars.
func TestValidateForPlan_PlanIdentifierSkipsBuildStepGuards(t *testing.T) {
	cfg := getConfig()
	cfg.BuildID = ""
	cfg.StepID = ""
	cfg.Identifier = "01919f1e-0000-7000-8000-000000000000"

	if err := cfg.ValidateForPlan(); err != nil {
		t.Fatalf("ValidateForPlan() with --plan-identifier and no BUILD_ID/STEP_ID error = %v", err)
	}

	if cfg.Identifier != "01919f1e-0000-7000-8000-000000000000" {
		t.Errorf("cfg.Identifier = %q, want it preserved (not overwritten with BUILD_ID/STEP_ID)", cfg.Identifier)
	}
}

// A supplied identifier takes precedence over BUILDKITE_BUILD_ID/BUILDKITE_STEP_ID:
// when both are set, the identifier is kept rather than overwritten with the
// "<build>/<step>" composite.
func TestValidateForPlan_PlanIdentifierTakesPrecedenceOverBuildStep(t *testing.T) {
	cfg := getConfig()
	cfg.BuildID = "123"
	cfg.StepID = "789"
	cfg.Identifier = "01919f1e-0000-7000-8000-000000000000"

	if err := cfg.ValidateForPlan(); err != nil {
		t.Fatalf("ValidateForPlan() error = %v", err)
	}

	if cfg.Identifier != "01919f1e-0000-7000-8000-000000000000" {
		t.Errorf("cfg.Identifier = %q, want the supplied identifier to win over %q", cfg.Identifier, "123/789")
	}
}

// Without an identifier but with BUILD_ID/STEP_ID, the identifier defaults to
// the "<build>/<step>" composite.
func TestValidateForPlan_IdentifierDefaultsToBuildStepWhenUnset(t *testing.T) {
	cfg := getConfig()
	cfg.BuildID = "123"
	cfg.StepID = "789"
	cfg.Identifier = ""

	if err := cfg.ValidateForPlan(); err != nil {
		t.Fatalf("ValidateForPlan() error = %v", err)
	}

	if cfg.Identifier != "123/789" {
		t.Errorf("cfg.Identifier = %q, want %q", cfg.Identifier, "123/789")
	}
}

// Without an identifier and without BUILD_ID/STEP_ID, validation must still fail.
func TestValidateForPlan_RequiresBuildStepWhenNoPlanIdentifier(t *testing.T) {
	cfg := getConfig()
	cfg.BuildID = ""
	cfg.StepID = ""
	cfg.Identifier = ""

	err := cfg.ValidateForPlan()
	if err == nil {
		t.Fatal("ValidateForPlan() error = nil, want errors for blank BUILDKITE_BUILD_ID/BUILDKITE_STEP_ID")
	}
	if !strings.Contains(err.Error(), "BUILDKITE_BUILD_ID") || !strings.Contains(err.Error(), "BUILDKITE_STEP_ID") {
		t.Errorf("ValidateForPlan() error = %v, want it to mention BUILDKITE_BUILD_ID and BUILDKITE_STEP_ID", err)
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
	cfg.ServerBaseURL = svr.URL

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
	cfg.ServerBaseURL = svr.URL

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

func TestPlanJSON_ErrorPlanFallback(t *testing.T) {
	svr := getErrorPlanServer()
	defer svr.Close()

	cfg := getConfig()
	cfg.ServerBaseURL = svr.URL
	// Identifier and MaxParallelism seed the locally-computed fallback plan.
	cfg.Identifier = "local-id"
	cfg.MaxParallelism = 7

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

	// The server's error plan (empty tasks) is replaced by the local fallback,
	// so the emitted identifier and parallelism come from cfg, not the server.
	want := `{"BUILDKITE_TEST_ENGINE_PLAN_IDENTIFIER":"local-id","BUILDKITE_TEST_ENGINE_PARALLELISM":"7"}
`
	got := buf.String()

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("command.Plan(...) JSON output diff = %s", diff)
	}

	// Verify the error-plan warning was logged to stderr.
	if !strings.Contains(stderrOutput, "Falling back to non-intelligent splitting") {
		t.Errorf("expected stderr to contain error-plan warning, got: %s", stderrOutput)
	}
}

func TestPlanPipelineUpload_ErrorPlanFallback(t *testing.T) {
	svr := getErrorPlanServer()
	defer svr.Close()

	cfg := getConfig()
	cfg.ServerBaseURL = svr.URL
	cfg.Identifier = "local-id"
	cfg.MaxParallelism = 7

	if err := cfg.ValidateForPlan(); err != nil {
		t.Errorf("Invalid config: %v", err)
	}

	ctx := context.Background()

	var buf bytes.Buffer
	setPlanWriter(t, &buf)

	setPipelineUploadCommand(t, "echo", "called with")

	getStderr := captureStderr(t)

	// This is the method under test
	planErr := Plan(ctx, cfg, "", PlanOutputPipelineUpload, "testtemplate.yml")

	stderrOutput := getStderr()

	if planErr != nil {
		t.Errorf("command.Plan(...) error = %v", planErr)
	}

	// The fallback plan has non-zero parallelism, so pipeline upload runs with
	// the fallback identifier and parallelism.
	want := `Executing buildkite-agent pipeline upload with BUILDKITE_TEST_ENGINE_PLAN_IDENTIFIER=local-id BUILDKITE_TEST_ENGINE_PARALLELISM=7
called with testtemplate.yml
`
	got := buf.String()
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("command.Plan(...) diff = %s", diff)
	}

	// Verify the error-plan warning was logged to stderr.
	if !strings.Contains(stderrOutput, "Falling back to non-intelligent splitting") {
		t.Errorf("expected stderr to contain error-plan warning, got: %s", stderrOutput)
	}
}

// When the resolved plan has parallelism 0, --pipeline-upload must not run
// buildkite-agent. This is reachable via a fallback plan whose parallelism
// comes from an unset --max-parallelism (cfg.MaxParallelism == 0), triggered
// here by a server error plan.
func TestPlanPipelineUpload_Parallelism0(t *testing.T) {
	svr := getErrorPlanServer()
	defer svr.Close()

	cfg := getConfig()
	cfg.ServerBaseURL = svr.URL
	cfg.Identifier = "local-id"
	// MaxParallelism left at 0, so the fallback plan has parallelism 0.

	if err := cfg.ValidateForPlan(); err != nil {
		t.Errorf("Invalid config: %v", err)
	}

	ctx := context.Background()

	var buf bytes.Buffer
	setPlanWriter(t, &buf)

	// Fail loudly if pipeline upload is executed despite parallelism 0.
	setPipelineUploadCommand(t, "echo", "SHOULD_NOT_RUN")

	getStderr := captureStderr(t)

	// This is the method under test
	planErr := Plan(ctx, cfg, "", PlanOutputPipelineUpload, "testtemplate.yml")

	stderrOutput := getStderr()

	if planErr != nil {
		t.Errorf("command.Plan(...) error = %v", planErr)
	}

	// Nothing is written to stdout: pipeline upload does not run.
	if got := buf.String(); got != "" {
		t.Errorf("expected no pipeline upload output, got: %s", got)
	}

	// Verify the parallelism-0 warning was logged to stderr.
	if !strings.Contains(stderrOutput, "Parallelism is 0") {
		t.Errorf("expected stderr to contain parallelism-0 warning, got: %s", stderrOutput)
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
	cfg.ServerBaseURL = svr.URL

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
			// A realistic zero-parallelism plan: no nodes means no tasks. This
			// is a well-formed server response, distinct from an error plan.
			testPlan := plan.TestPlan{
				Identifier:  "facecafe",
				Parallelism: 0,
				Tasks:       map[string]*plan.Task{},
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

// getErrorPlanServer returns an "error plan": a 200 response whose task list is
// empty (`{"tasks": {}}`). The --json and --pipeline-upload paths treat this as
// a signal to substitute a locally-computed fallback plan.
func getErrorPlanServer() *httptest.Server {
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
				Parallelism: 4,
				Tasks:       map[string]*plan.Task{},
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
	cfg.BuildID = "123"
	cfg.StepID = "789"
	cfg.OrganizationSlug = "buildkite"
	cfg.NodeIndex = 0
	cfg.Parallelism = 3
	cfg.AccessToken = "asdf1234"
	cfg.SuiteSlug = "rspec"
	cfg.TestRunner = "rspec"
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
	cfg.ServerBaseURL = svr.URL
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
	if !strings.Contains(stderrOutput, "Not a git repository") &&
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
	cfg.ServerBaseURL = svr.URL
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
	if strings.Contains(stderrOutput, "Not a git repository") ||
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
	cfg.ServerBaseURL = svr.URL

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
	cfg.ServerBaseURL = svr.URL

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

// When the server cannot be reached (here, a retry timeout), --plan-out has
// nothing to pass through, so it emits a locally-computed fallback plan on
// stdout and warns on stderr that it is not a server plan.
func TestPlanPlanOut_FallbackWarnsOnStderr(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}))
	defer svr.Close()

	cfg := getConfig()
	cfg.Identifier = "hello"
	cfg.MaxParallelism = 10
	cfg.ServerBaseURL = svr.URL
	cfg.PlanOut = "-"

	if err := cfg.ValidateForPlan(); err != nil {
		t.Errorf("Invalid config: %v", err)
	}

	// Short timeout to trigger fallback quickly
	fetchCtx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	var buf bytes.Buffer
	setPlanWriter(t, &buf)

	getStderr := captureStderr(t)

	// This is the method under test
	planErr := Plan(fetchCtx, cfg, "", PlanOutputPlanOut, "")

	stderrOutput := getStderr()

	if planErr != nil {
		t.Errorf("command.Plan(...) error = %v", planErr)
	}

	// The fallback plan is still emitted as valid JSON on stdout, carrying the
	// identifier and parallelism, without the client-internal Fallback field
	// leaking.
	var got plan.TestPlan
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, buf.String())
	}
	if strings.Contains(strings.ToLower(buf.String()), "fallback") {
		t.Errorf("--plan-out output leaked the Fallback field:\n%s", buf.String())
	}
	if got.Identifier != "hello" {
		t.Errorf("fallback plan Identifier = %q, want %q", got.Identifier, "hello")
	}
	if got.Parallelism != 10 {
		t.Errorf("fallback plan Parallelism = %d, want 10 (from --max-parallelism)", got.Parallelism)
	}

	// The fallback caveat is written to stderr.
	if !strings.Contains(stderrOutput, "locally-computed fallback plan") {
		t.Errorf("expected stderr to contain the fallback caveat, got: %s", stderrOutput)
	}
}

// When the server returns an error plan (`{"tasks": {}}`), --plan-out passes
// it through verbatim rather than substituting a local fallback, so the output
// reflects the server's actual response. It is not marked as a local fallback.
func TestPlanPlanOut_ServerErrorPlanPassedThrough(t *testing.T) {
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
			// Error plan: empty task map.
			enc.Encode(plan.TestPlan{Identifier: "facecafe", Parallelism: 0, Tasks: map[string]*plan.Task{}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer svr.Close()

	cfg := getConfig()
	cfg.ServerBaseURL = svr.URL
	cfg.PlanOut = "-"

	if err := cfg.ValidateForPlan(); err != nil {
		t.Errorf("Invalid config: %v", err)
	}

	var buf bytes.Buffer
	setPlanWriter(t, &buf)

	getStderr := captureStderr(t)

	planErr := Plan(context.Background(), cfg, "", PlanOutputPlanOut, "")

	stderrOutput := getStderr()

	if planErr != nil {
		t.Errorf("command.Plan(...) error = %v", planErr)
	}

	var got plan.TestPlan
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, buf.String())
	}

	// The server's error plan is passed through: empty tasks, server identifier,
	// no locally-distributed task map.
	if len(got.Tasks) != 0 {
		t.Errorf("expected empty tasks passed through from server, got: %s", buf.String())
	}
	if got.Identifier != "facecafe" {
		t.Errorf("expected server identifier %q, got %q", "facecafe", got.Identifier)
	}

	// The empty-plan warning is written to stderr...
	if !strings.Contains(stderrOutput, "returned an empty plan") {
		t.Errorf("expected stderr to contain the empty-plan warning, got: %s", stderrOutput)
	}
	// ...but it is NOT presented as a locally-computed fallback, nor does it
	// claim a fallback to non-intelligent splitting, since the server's plan is
	// what gets emitted.
	if strings.Contains(stderrOutput, "locally-computed fallback plan") {
		t.Errorf("server error plan should not be reported as a local fallback, got: %s", stderrOutput)
	}
	if strings.Contains(stderrOutput, "Falling back to non-intelligent splitting") {
		t.Errorf("server error plan should not claim a fallback to non-intelligent splitting, got: %s", stderrOutput)
	}
}

// A fatal API error (here, 401 Unauthorized) is returned rather than swallowed:
// --plan-out exits with an error and emits nothing on stdout, instead of
// falling back to a local plan.
func TestPlanPlanOut_FatalErrorReturnsNoOutput(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"message": "Invalid access token"}`))
	}))
	defer svr.Close()

	cfg := getConfig()
	cfg.ServerBaseURL = svr.URL
	cfg.PlanOut = "-"

	if err := cfg.ValidateForPlan(); err != nil {
		t.Errorf("Invalid config: %v", err)
	}

	var buf bytes.Buffer
	setPlanWriter(t, &buf)

	planErr := Plan(context.Background(), cfg, "", PlanOutputPlanOut, "")

	// A fatal error is propagated (non-nil), so main exits non-zero.
	if planErr == nil {
		t.Error("expected a fatal error for a 401 response, got nil")
	}

	// Nothing is written to stdout: no plan, no fallback.
	if buf.Len() != 0 {
		t.Errorf("expected no stdout output on fatal error, got: %s", buf.String())
	}
}
