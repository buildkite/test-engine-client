package api

import (
	"context"
	"fmt"
	"net/http"

	"github.com/buildkite/test-engine-client/v2/internal/config"
	"github.com/buildkite/test-engine-client/v2/internal/plan"
)

type FilterTestsParams struct {
	Files []plan.TestCase `json:"files"`
	Env   *config.Config  `json:"env"`
}

// FilterReasonSkippedTests is the reason value the server attaches to a
// filtered file when the file contains one or more skipped tests. It must match
// TestSplitting::TestPlan::TestFilesFilter::Reason::SKIP on the server.
const FilterReasonSkippedTests = "file contains 1 or more skipped tests"

type FilteredTest struct {
	Path string `json:"path"`
	// Reason explains why the server returned this file, e.g. "slow file" or
	// "file contains 1 or more skipped tests".
	Reason string `json:"reason"`
}

type FilteredTestResponse struct {
	Tests []FilteredTest `json:"tests"`
}

// FilterTests fetches test files from the server. It returns a list of test files that
// need to be split by example.
//
// Currently, it only fetches tests file that are slow and test files that have tests
// marked for skipping.
//
// The splitByExample flag is passed through to the server, which is false will only
// return test files that contain skipped tests, while true will also return slow test
// files.
func (c Client) FilterTests(ctx context.Context, suiteSlug string, params FilterTestsParams) ([]FilteredTest, error) {
	url := fmt.Sprintf("%s/v2/analytics/organizations/%s/suites/%s/test_plan/filter_tests", c.ServerBaseURL, c.OrganizationSlug, suiteSlug)

	var response FilteredTestResponse
	_, err := c.DoWithRetry(ctx, httpRequest{
		Method: http.MethodPost,
		URL:    url,
		Body:   params,
	}, &response)
	if err != nil {
		return []FilteredTest{}, err
	}

	return response.Tests, nil
}
