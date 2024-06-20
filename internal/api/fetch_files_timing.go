package api

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

type fetchFilesTimingParams struct {
	Paths []string `json:"paths"`
}

// FetchFilesTiming fetches the timing of the requested files from the server.
// The server only returns timings for the files that has been run before.
// ErrRetryTimeout is returned if the client failed to communicate with the server after exceeding the retry limit.
func (c Client) FetchFilesTiming(ctx context.Context, suiteSlug string, files []string) (map[string]time.Duration, error) {
	url := fmt.Sprintf("%s/v2/analytics/organizations/%s/suites/%s/test_files", c.ServerBaseUrl, c.OrganizationSlug, suiteSlug)

	var filesTiming map[string]int
	_, err := c.DoWithRetry(ctx, httpRequest{
		Method: http.MethodPost,
		URL:    url,
		Body: fetchFilesTimingParams{
			Paths: files,
		},
	}, &filesTiming)

	if err != nil {
		return nil, err
	}

	result := map[string]time.Duration{}
	for path, duration := range filesTiming {
		result[path] = time.Duration(duration * int(time.Millisecond))
	}

	return result, nil
}
