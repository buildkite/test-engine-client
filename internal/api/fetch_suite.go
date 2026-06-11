package api

import (
	"context"
	"fmt"
	"net/http"
)

type Suite struct {
	ID   string `json:"id"`
	Slug string `json:"slug"`
}

func (c Client) FetchSuite(ctx context.Context, suiteSlug string) (Suite, error) {
	url := fmt.Sprintf("%s/v2/analytics/organizations/%s/suites/%s", c.ServerBaseURL, c.OrganizationSlug, suiteSlug)

	var suite Suite
	_, err := c.DoWithRetry(ctx, httpRequest{
		Method: http.MethodGet,
		URL:    url,
	}, &suite)
	if err != nil {
		return Suite{}, err
	}

	return suite, nil
}
