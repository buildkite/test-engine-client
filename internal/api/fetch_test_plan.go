package api

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/buildkite/test-splitter/internal/plan"
)

// FetchTestPlan fetchs a test plan from the server's cache.
func (c client) FetchTestPlan(suiteSlug string, identifier string) (*plan.TestPlan, error) {
	url := fmt.Sprintf("%s/v2/analytics/organizations/%s/suites/%s/test_plan?identifier=%s", c.ServerBaseUrl, c.OrganizationSlug, suiteSlug, identifier)

	// send request
	resp, err := c.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	// read response
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}

	// parse response
	var testPlan plan.TestPlan
	err = json.Unmarshal(responseBody, &testPlan)
	if err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	return &testPlan, nil
}
