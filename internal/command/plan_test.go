package command_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/buildkite/test-engine-client/internal/api"
	"github.com/buildkite/test-engine-client/internal/command"
	"github.com/buildkite/test-engine-client/internal/config"
	"github.com/buildkite/test-engine-client/internal/plan"
	"github.com/google/go-cmp/cmp"
)

func TestPlan(t *testing.T) {
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
	defer svr.Close()

	cfg := config.NewEmpty()

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
	cfg.ServerBaseUrl = svr.URL

	if err := cfg.Validate(); err != nil {
		t.Errorf("Invalid config: %v", err)
	}

	ctx := context.Background()

	// By default command.Run writes to os.Stdout.
	// Replace with a string buffer here so we can test the command output.
	var buf bytes.Buffer
	command.SetPlanWriter(&buf)

	// This is the method under test
	err := command.Plan(ctx, cfg, "")

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
