package api

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

// Suite identifies a Test Engine suite and its organization.
type Suite struct {
	ID             string `json:"id"`
	OrganizationID string `json:"organization_id"`
}

// GetSuite retrieves a suite by slug.
//
// Endpoint: GET /v2/analytics/organizations/:org/suites/:suite
func (c Client) GetSuite(ctx context.Context, suiteSlug string) (Suite, error) {
	reqURL := fmt.Sprintf(
		"%s/v2/analytics/organizations/%s/suites/%s",
		c.ServerBaseURL, url.PathEscape(c.OrganizationSlug), url.PathEscape(suiteSlug))

	var suite Suite
	_, err := c.doJSONWithRetry(ctx, httpRequest{Method: http.MethodGet, URL: reqURL}, &suite)
	if err != nil {
		return Suite{}, fmt.Errorf("getting suite: %w", err)
	}
	return suite, nil
}
