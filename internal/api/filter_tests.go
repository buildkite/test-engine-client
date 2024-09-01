package api

import (
	"context"
	"fmt"
	"net/http"

	"github.com/buildkite/test-splitter/internal/plan"
)

type FilterTestsParams struct {
	Files          []plan.TestCase `json:"files"`
	Parallelism    int             `json:"parallelism"`
	SplitByExample bool            `json:"split_by_example"`
}

type FilteredTest struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

type filteredTestResponse struct {
	Tests []FilteredTest `json:"tests"`
}

// FilterTests filters tests from the server.
func (c Client) FilterTests(ctx context.Context, suiteSlug string, params FilterTestsParams) ([]FilteredTest, error) {
	url := fmt.Sprintf("%s/v2/analytics/organizations/%s/suites/%s/test_plan/filter_tests", c.ServerBaseUrl, c.OrganizationSlug, suiteSlug)

	var response filteredTestResponse
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
