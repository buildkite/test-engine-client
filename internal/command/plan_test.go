package command_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/buildkite/test-engine-client/internal/api"
	"github.com/buildkite/test-engine-client/internal/command"
	"github.com/buildkite/test-engine-client/internal/plan"
	"github.com/google/go-cmp/cmp"
	"github.com/urfave/cli/v3"
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

	var environment = map[string]string{
		"BUILDKITE_BRANCH":                        "tet-123-add-branch-name",
		"BUILDKITE_BUILD_ID":                      "123",
		"BUILDKITE_STEP_ID":                       "789",
		"BUILDKITE_ORGANIZATION_SLUG":             "buildkite",
		"BUILDKITE_PARALLEL_JOB":                  "0",
		"BUILDKITE_PARALLEL_JOB_COUNT":            "3",
		"BUILDKITE_TEST_ENGINE_API_ACCESS_TOKEN":  "asdf1234",
		"BUILDKITE_TEST_ENGINE_SUITE_SLUG":        "rspec",
		"BUILDKITE_TEST_ENGINE_TEST_RUNNER":       "rspec",
		"BUILDKITE_TEST_ENGINE_RESULT_PATH":       "out.json",
		"BUILDKITE_TEST_ENGINE_DEBUG_ENABLED":     "true",
		"BUILDKITE_TEST_ENGINE_TEST_FILE_PATTERN": "testdata/rspec/spec/**/*_spec.rb",
		"BUILDKITE_TEST_ENGINE_BASE_URL":          svr.URL,
	}

	for k, v := range environment {
		os.Setenv(k, v)
	}

	ctx := context.Background()
	cmd := &cli.Command{}

	// By default command.Run writes to os.Stdout.
	// Replace with a string buffer here so we can test the command output.
	var buf bytes.Buffer
	command.SetPlanWriter(&buf)

	// This is the method under test
	err := command.Plan(ctx, cmd)

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
