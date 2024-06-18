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

// FetchTestPlan fetchs a test plan from the server.
// If the plan is found, it returns the plan, otherwise it returns nil.
// Error is returned if there is a client side failure or invalid request (400, 401, 403).
// Other server errors are ignored and treated as "not found".
func (c Client) FetchTestPlan(suiteSlug string, identifier string) (*plan.TestPlan, error) {
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
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden:
		// treat following errors as client side errors because they are not recoverable.
		// 400: Bad request (invalid request)
		// 401: Unauthorized (invalid access token)
		// 403: Forbidden (access token does not have required permissions)
		responseBody, _ := io.ReadAll(resp.Body)
		var errorResp errorResponse
		json.Unmarshal(responseBody, &errorResp)
		return nil, fmt.Errorf(errorResp.Message)
	default:
		// ignore other errors and treat them as "not found", so the client can proceed with creating a new plan.
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
