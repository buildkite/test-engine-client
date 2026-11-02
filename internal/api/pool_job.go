package api

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// WaitForPoolJob returns whether this job contributed to a terminal entry
// failure. A false result is final only once the pool is consumed.
func (c *Client) WaitForPoolJob(ctx context.Context, poolID, jobID string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	delay := time.Second
	for {
		var result struct {
			Job struct {
				ID     string `json:"id"`
				Failed *bool  `json:"participated_in_terminal_failure"`
			} `json:"job"`
			Pool struct {
				ID    string `json:"id"`
				State string `json:"state"`
			} `json:"pool"`
		}
		endpoint := c.poolURL(poolID) + "/jobs/" + url.PathEscape(jobID)
		response, err := c.doJSONWithRetry(ctx, httpRequest{Method: http.MethodGet, URL: endpoint}, &result)
		if response != nil && response.StatusCode == http.StatusNotFound {
			// Older servers and disabled feature gates may return non-JSON 404s.
			return false, &NotFoundError{Message: "test pool job API unavailable"}
		}
		if err != nil {
			return false, fmt.Errorf("getting test pool %s job %s: %w", poolID, jobID, err)
		}
		if result.Pool.ID != poolID || !strings.EqualFold(result.Job.ID, jobID) || result.Job.Failed == nil {
			return false, fmt.Errorf("invalid test pool job response")
		}
		switch result.Pool.State {
		case "consumed":
			return *result.Job.Failed, nil
		case "planning", "populating", "consuming":
			if *result.Job.Failed {
				return true, nil
			}
		default:
			return false, fmt.Errorf("test pool %s has unsupported state %q", poolID, result.Pool.State)
		}
		select {
		case <-ctx.Done():
			return false, fmt.Errorf("waiting for test pool %s job %s: %w", poolID, jobID, ctx.Err())
		case <-time.After(delay):
		}
		delay = min(delay*2, 5*time.Second)
	}
}
