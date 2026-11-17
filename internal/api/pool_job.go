package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/buildkite/test-engine-client/v3/internal/plan"
)

// PoolEntry is a Scheduler entry, not an attempt created by a job retry.
type PoolEntry struct {
	ID           string        `json:"id"`
	SelectorType string        `json:"selector_type"`
	Selector     plan.TestCase `json:"selector"`
}

// GetPoolJobFailedEntries reads the fixed failure set for a job and its retry
// ancestors. Fetch every page before running anything; a partial list must not
// turn a retry into a successful run that silently skipped failures.
func (c *Client) GetPoolJobFailedEntries(ctx context.Context, poolID, jobID string) ([]PoolEntry, error) {
	endpoint := c.poolURL(poolID) + "/entries"
	query := url.Values{"result": {"failed"}, "job_id": {jobID}, "per_page": {"100"}}
	var entries []PoolEntry
	for {
		var result struct {
			Items []PoolEntry `json:"items"`
			Links *struct {
				Next string `json:"next"`
			} `json:"links"`
		}
		if _, err := c.doJSONWithRetry(ctx, httpRequest{Method: http.MethodGet, URL: endpoint + "?" + query.Encode()}, &result); err != nil {
			return nil, fmt.Errorf("getting test pool %s job %s failed entries: %w", poolID, jobID, err)
		}
		if result.Items == nil {
			return nil, errors.New("test pool entries response is missing items")
		}
		if result.Links == nil {
			return nil, errors.New("test pool entries response is missing links")
		}
		entries = append(entries, result.Items...)
		if result.Links.Next == "" {
			return entries, nil
		}
		next, err := url.Parse(result.Links.Next)
		if err != nil {
			return nil, fmt.Errorf("invalid failed entries pagination link: %w", err)
		}
		// Pagination changes the query only, never the pool or job filter.
		query = next.Query()
		query.Set("result", "failed")
		query.Set("job_id", jobID)
		query.Set("per_page", "100")
	}
}

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
