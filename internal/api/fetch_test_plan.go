package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/buildkite/test-splitter/internal/plan"
)

// FetchTestPlan fetchs a test plan from the server's cache.
// If the test plan is found in the cache, it is returned, otherwise nil is returned.
// Error is returned if there is a client side failure or the server returns a 401.
// Other server errors are ignored and treated as cache miss.
//
// Note: we could ignore all server errors and treat them as a cache miss,
// but there is no reason to continue the process if the client is unauthorized,
// so we treat 401 as an error.
func (c client) FetchTestPlan(suiteSlug string, identifier string) (*plan.TestPlan, error) {
	url := fmt.Sprintf("%s/v2/analytics/organizations/%s/suites/%s/test_plan?identifier=%s", c.ServerBaseUrl, c.OrganizationSlug, suiteSlug, identifier)

	// send request
	resp, err := c.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		// happy path
	case http.StatusUnauthorized:
		// treat as error
		return nil, fmt.Errorf("unauthorized: %w", err)
	default:
		// ignore other errors and treat them as cache miss
		return nil, nil
	}

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
