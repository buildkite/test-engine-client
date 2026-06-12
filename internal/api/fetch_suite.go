package api

import (
	"context"
	"fmt"
	"net/http"
)

// Suite represents a Test Engine suite as returned by the suite show endpoint.
type Suite struct {
	ID string `json:"id"`
}

// FetchSuite fetches a suite from the server, primarily to resolve the suite
// slug into its UUID.
// ErrRetryTimeout is returned if the client failed to communicate with the server after exceeding the retry limit.
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
