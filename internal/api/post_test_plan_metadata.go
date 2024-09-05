package api

import (
	"context"
	"fmt"
	"net/http"
)

type Timeline struct {
	Timestamp string `json:"timestamp"`
	Event     string `json:"event"`
}

type TestPlanMetadataParams struct {
	Version  string            `json:"version"`
	BktecEnv map[string]string `json:"bktec_env"`
	Timeline []Timeline        `json:"timeline"`
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
