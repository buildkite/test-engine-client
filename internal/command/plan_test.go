package command

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

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

	want := `called with testtemplate.yml
`
	got := buf.String()

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("command.Plan(...) diff = %s", diff)
	}
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
