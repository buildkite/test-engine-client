package api

import (
	"context"
	"fmt"
	"net/http"

	"github.com/buildkite/test-engine-client/internal/plan"
)

// FetchTestPlan fetchs a test plan from the server.
// ErrRetryTimeout is returned if the client failed to communicate with the server after exceeding the retry limit.
func (c Client) FetchTestPlan(ctx context.Context, suiteSlug string, identifier string, jobRetryCount int) (*plan.TestPlan, error) {
	url := fmt.Sprintf("%s/v2/analytics/organizations/%s/suites/%s/test_plan?identifier=%s&job_retry_count=%d", c.ServerBaseUrl, c.OrganizationSlug, suiteSlug, identifier, jobRetryCount)

	var testPlan plan.TestPlan

	resp, err := c.DoWithRetry(ctx, httpRequest{
		Method: http.MethodGet,
		URL:    url,
	}, &testPlan)

	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			return nil, nil
		}
		return nil, err
	}

	return &testPlan, nil
}
