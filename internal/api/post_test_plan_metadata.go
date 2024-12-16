package api

import (
	"context"
	"fmt"
	"net/http"

	"github.com/buildkite/test-engine-client/internal/runner"
)

type Timeline struct {
	Timestamp string `json:"timestamp"`
	Event     string `json:"event"`
}

type TestPlanMetadataParams struct {
	Version    string               `json:"version"`
	Env        map[string]string    `json:"env"`
	Timeline   []Timeline           `json:"timeline"`
	Statistics runner.RunStatistics `json:"statistics"`
}

func (c Client) PostTestPlanMetadata(ctx context.Context, suiteSlug string, identifier string, params TestPlanMetadataParams) error {
	url := fmt.Sprintf("%s/v2/analytics/organizations/%s/suites/%s/test_plan_metadata", c.ServerBaseUrl, c.OrganizationSlug, suiteSlug)

	_, err := c.DoWithRetry(ctx, httpRequest{
		Method: http.MethodPost,
		URL:    url,
		Body:   params,
	}, nil)

	return err
}
