package api

import (
	"context"
	"fmt"
	"net/http"

	"github.com/buildkite/test-engine-client/internal/plan"
)

type TestPlanParamsTest struct {
	Files    []plan.TestCase `json:"files"`
	Examples []plan.TestCase `json:"examples,omitempty"`
}

// TestPlanParams represents the config params sent when fetching a test plan.
type TestPlanParams struct {
	Runner         string             `json:"runner"`
	Identifier     string             `json:"identifier"`
	Parallelism    int                `json:"parallelism"`
	MaxParallelism int                `json:"max_parallelism,omitempty"`
	Branch         string             `json:"branch"`
	Tests          TestPlanParamsTest `json:"tests"`
}

// CreateTestPlan creates a test plan from the server.
// ErrRetryTimeout is returned if the client failed to communicate with the server after exceeding the retry limit.
func (c Client) CreateTestPlan(ctx context.Context, suiteSlug string, params TestPlanParams) (plan.TestPlan, error) {
	postUrl := fmt.Sprintf("%s/v2/analytics/organizations/%s/suites/%s/test_plan", c.ServerBaseUrl, c.OrganizationSlug, suiteSlug)

	var testPlan plan.TestPlan
	_, err := c.DoWithRetry(ctx, httpRequest{
		Method: http.MethodPost,
		URL:    postUrl,
		Body:   params,
	}, &testPlan)

	if err != nil {
		return plan.TestPlan{}, err
	}

	return testPlan, nil
}
