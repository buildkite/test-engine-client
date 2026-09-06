package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/buildkite/test-engine-client/v3/internal/plan"
)

// FetchTestPlan fetchs a test plan from the server.
// ErrRetryTimeout is returned if the client failed to communicate with the server after exceeding the retry limit.
func (c Client) FetchTestPlan(ctx context.Context, suiteSlug string, identifier string, jobRetryCount int) (*plan.TestPlan, error) {
	testPlan, _, err := c.FetchTestPlanRaw(ctx, suiteSlug, identifier, jobRetryCount)
	return testPlan, err
}

// FetchTestPlanRaw is like FetchTestPlan but also returns the unmodified JSON
// response, preserving fields not modeled by the execution structs.
func (c Client) FetchTestPlanRaw(ctx context.Context, suiteSlug string, identifier string, jobRetryCount int) (*plan.TestPlan, json.RawMessage, error) {
	url := fmt.Sprintf("%s/v2/analytics/organizations/%s/suites/%s/test_plan?identifier=%s&job_retry_count=%d", c.ServerBaseURL, c.OrganizationSlug, suiteSlug, identifier, jobRetryCount)

	var raw json.RawMessage

	_, err := c.doJSONWithRetry(ctx, httpRequest{
		Method: http.MethodGet,
		URL:    url,
	}, &raw)

	if err != nil {
		var notFoundErr *NotFoundError
		if errors.As(err, &notFoundErr) {
			return nil, nil, nil
		}
		return nil, nil, err
	}

	var testPlan plan.TestPlan
	// Match doJSONWithRetry: an empty successful response leaves a zero plan,
	// allowing run to use its existing empty-plan fallback.
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &testPlan); err != nil {
			return nil, nil, fmt.Errorf("parsing test plan: %w", err)
		}
	}
	return &testPlan, raw, nil
}
