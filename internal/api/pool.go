package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/buildkite/roko"
	"github.com/buildkite/test-engine-client/v3/internal/plan"
)

// Pool is the full Scheduler representation, not a lease response. Pool state,
// rather than the presence of MutedTests, indicates planning readiness.
type Pool struct {
	ID         string          `json:"id"`
	State      string          `json:"state"`
	MutedTests []plan.TestCase `json:"muted_tests"`
	Error      *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
	Location string `json:"-"`
}

type PoolPlanParams struct {
	Suite    string   `json:"suite"`
	Pipeline string   `json:"pipeline"`
	BuildID  string   `json:"build_id"`
	Key      string   `json:"key"`
	Plan     PoolPlan `json:"plan"`
}

// PoolPlan deliberately excludes Test Plan identifiers and lane-sizing fields.
type PoolPlan struct {
	Runner         string             `json:"runner"`
	Branch         string             `json:"branch"`
	Tests          TestPlanParamsTest `json:"tests"`
	Selection      *SelectionParams   `json:"selection,omitempty"`
	LocationPrefix string             `json:"location_prefix,omitempty"`
	Metadata       map[string]string  `json:"metadata,omitempty"`
}

type PoolError struct {
	StatusCode int
	Message    string
}

func (e *PoolError) Error() string {
	return fmt.Sprintf("test pool API (%d): %s", e.StatusCode, e.Message)
}

func (c *Client) poolURL(id string) string {
	return fmt.Sprintf("%s/v2/organizations/%s/test-scheduler/pools/%s", c.ServerBaseURL, url.PathEscape(c.OrganizationSlug), url.PathEscape(id))
}

func (c *Client) PlanPool(ctx context.Context, params PoolPlanParams) (Pool, error) {
	body, err := json.Marshal(params)
	if err != nil {
		return Pool{}, err
	}
	return c.requestPool(ctx, http.MethodPost, c.poolURL("plan"), body, http.StatusAccepted)
}

func (c *Client) GetPool(ctx context.Context, id string) (Pool, error) {
	return c.requestPool(ctx, http.MethodGet, c.poolURL(id), nil, http.StatusOK)
}

// requestPool is only for idempotent pool reads and fetch-or-create planning.
// Do not use it for lease creation: ambiguous failures are safe to retry here,
// but could create a second lease. Unlike doWithRetry, not every 409 is retried.
func (c *Client) requestPool(ctx context.Context, method, endpoint string, body []byte, success int) (Pool, error) {
	ctx, cancel := context.WithTimeout(ctx, retryTimeout)
	defer cancel()
	r := roko.NewRetrier(roko.TryForever(), roko.WithStrategy(roko.ExponentialSubsecond(initialDelay)), roko.WithJitter())
	return roko.DoFunc(ctx, r, func(r *roko.Retrier) (Pool, error) {
		req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
		if err != nil {
			r.Break()
			return Pool{}, err
		}
		req.Header.Set("Content-Type", "application/json")
		client := *c.httpClient
		client.Timeout = 15 * time.Second
		resp, err := client.Do(req)
		if err != nil {
			return Pool{}, err
		}
		defer resp.Body.Close()
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			return Pool{}, err
		}
		var message responseError
		_ = json.Unmarshal(data, &message)
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 ||
			(method == http.MethodPost && resp.StatusCode == http.StatusConflict && message.Message == "Still creating test pool, please retry.") {
			return Pool{}, &PoolError{StatusCode: resp.StatusCode, Message: message.Message}
		}
		r.Break()
		var pool Pool
		if resp.StatusCode == success || resp.StatusCode == http.StatusConflict {
			if err := json.Unmarshal(data, &pool); err != nil {
				return Pool{}, fmt.Errorf("decoding pool response: %w", err)
			}
			if pool.State == "errored" {
				return Pool{}, poolFailure(pool)
			}
		}
		if resp.StatusCode != success {
			if resp.StatusCode == http.StatusGone {
				message.Message = "test pool has expired; use a new key or pool ID"
			}
			return Pool{}, &PoolError{StatusCode: resp.StatusCode, Message: message.Message}
		}
		if pool.ID == "" || pool.State == "" {
			return Pool{}, fmt.Errorf("pool response is missing id or state")
		}
		pool.Location = resp.Header.Get("Location")
		return pool, nil
	})
}

func poolFailure(pool Pool) error {
	message := "Test pool failed."
	if pool.Error != nil && pool.Error.Message != "" {
		message = pool.Error.Message
	}
	return fmt.Errorf("test pool %s is errored: %s", pool.ID, message)
}

// WaitForPool always fetches the full representation, even for a pre-created
// pool. Callers must retain its muted tests for dispatch; leases don't contain them.
// A consumed pool is terminal and needs no lease, including an empty plan.
func (c *Client) WaitForPool(ctx context.Context, id string) (Pool, error) {
	delay := time.Second
	for {
		pool, err := c.GetPool(ctx, id)
		if err != nil {
			return Pool{}, err
		}
		switch pool.State {
		case "planning", "populating":
		case "consuming", "consumed":
			return pool, nil
		default:
			return Pool{}, fmt.Errorf("test pool %s has unsupported state %q", id, pool.State)
		}
		select {
		case <-ctx.Done():
			return Pool{}, ctx.Err()
		case <-time.After(delay):
		}
		delay = min(delay*2, 5*time.Second)
	}
}
