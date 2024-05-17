package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/buildkite/test-splitter/internal/plan"
)

type errorResponse struct {
	Message string `json:"message"`
}

// FetchTestPlan fetchs a test plan from the server's cache.
// If the test plan is found in the cache, it is returned, otherwise nil is returned.
// Error is returned if there is a client side failure or request is invalid (400 - 403).
// Other server errors are ignored and treated as cache miss.
func (c client) FetchTestPlan(suiteSlug string, identifier string) (*plan.TestPlan, error) {
	url := fmt.Sprintf("%s/v2/analytics/organizations/%s/suites/%s/test_plan?identifier=%s", c.ServerBaseUrl, c.OrganizationSlug, suiteSlug, identifier)

	// send request
	resp, err := c.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusOK:
		// happy path
	case resp.StatusCode >= 400 && resp.StatusCode <= 403:
		// treat 400-403 as an error because the request is invalid
		responseBody, _ := io.ReadAll(resp.Body)
		var errorResp errorResponse
		json.Unmarshal(responseBody, &errorResp)
		return nil, fmt.Errorf(errorResp.Message)
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
