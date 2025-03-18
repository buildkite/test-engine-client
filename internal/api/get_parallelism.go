package api

import (
	"context"
	"fmt"
	"net/http"
)

type parallelismResponse struct {
	Parallelism int
}

func (c Client) GetParallelism(ctx context.Context, suiteSlug string) (int, error) {
	url := fmt.Sprintf("%s/v2/analytics/organizations/%s/suites/%s/test_plan/parallelism", c.ServerBaseUrl, c.OrganizationSlug, suiteSlug)

	var response parallelismResponse
	_, err := c.DoWithRetry(ctx, httpRequest{
		Method: http.MethodPost,
		URL:    url,
	}, &response)

	if err != nil {
		return 0, err
	}

	return response.Parallelism, nil
}
