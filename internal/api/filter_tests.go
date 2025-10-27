package api

import (
	"context"
	"fmt"
	"net/http"

	"github.com/buildkite/test-engine-client/internal/plan"
)

type FilterTestsParams struct {
	Files []plan.TestCase   `json:"files"`
	Env   map[string]string `json:"env"`
}

type FilteredTest struct {
	Path string `json:"path"`
}

type FilteredTestResponse struct {
	Tests []FilteredTest `json:"tests"`
}

// FilterTests filters tests from the server. It returns a list of tests that need to be split by example.
// Currently, it only filters tests that are slow.
func (c Client) FilterTests(ctx context.Context, suiteSlug string, params FilterTestsParams) ([]FilteredTest, error) {
	url := fmt.Sprintf("%s/v2/analytics/organizations/%s/suites/%s/test_plan/filter_tests", c.ServerBaseUrl, c.OrganizationSlug, suiteSlug)

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
