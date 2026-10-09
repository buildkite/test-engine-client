package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/buildkite/roko"
	"github.com/buildkite/test-engine-client/v3/internal/debug"
	"github.com/buildkite/test-engine-client/v3/internal/plan"
)

// Pool is the full Scheduler representation, not a lease response. Nil
// Parallelism means sizing has not completed or was not requested; a pointer to
// zero is a completed empty plan. A nil MutedTests means the immutable snapshot
// is absent; an empty snapshot is [].
type Pool struct {
	ID          string          `json:"id"`
	State       string          `json:"state"`
	Parallelism *int            `json:"parallelism,omitempty"`
	MutedTests  []plan.TestCase `json:"muted_tests"`
	Error       *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
	Location string `json:"-"`
}

type PoolPlanParams struct {
	Suite    string         `json:"suite"`
	Pipeline string         `json:"pipeline"`
	BuildID  string         `json:"build_id"`
	Key      string         `json:"key"`
	Plan     PoolPlan       `json:"plan"`
	Lease    *PoolPlanLease `json:"lease,omitempty"`
}

type PoolPlanLease struct {
	Costs       *PoolPlanLeaseCosts `json:"costs,omitempty"`
	MaxAttempts int                 `json:"max_attempts,omitempty"`
}

type PoolPlanLeaseCosts struct {
	DurationP90MS int `json:"duration_p90_ms"`
}

// PoolPlan excludes Test Plan identifiers and static lane tasks. Dynamic sizing
// produces a worker-count recommendation on the completed pool representation.
type PoolPlan struct {
	Runner         string             `json:"runner"`
	Branch         string             `json:"branch"`
	Tests          TestPlanParamsTest `json:"tests"`
	MaxParallelism int                `json:"max_parallelism,omitempty"`
	TargetTime     float64            `json:"target_time,omitempty"`
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

// PoolAttemptResults counts a pool's completed attempts that did not pass.
type PoolAttemptResults struct {
	Failed  int `json:"failed"`
	Errored int `json:"errored"`
}

// poolResultsTimeout bounds how long WaitForPoolResults waits for the metrics
// replica to catch up with a consumed pool.
const poolResultsTimeout = 30 * time.Second

// WaitForPoolResults returns a consumed pool's failed and errored attempt
// counts. Metrics are read from a replica that can lag behind the pool state,
// so zero counts are only trusted once the metrics snapshot itself shows the
// pool consumed and drained. Completed results never change, so any failures
// are returned straight away.
func (c *Client) WaitForPoolResults(ctx context.Context, id string) (PoolAttemptResults, error) {
	ctx, cancel := context.WithTimeout(ctx, poolResultsTimeout)
	defer cancel()
	delay := time.Second
	for {
		var metrics struct {
			Pool struct {
				State   string `json:"state"`
				Drained bool   `json:"drained"`
			} `json:"pool"`
			Attempts struct {
				Results PoolAttemptResults `json:"results"`
			} `json:"attempts"`
		}
		if _, err := c.doJSONWithRetry(ctx, httpRequest{Method: http.MethodGet, URL: c.poolURL(id) + "/metrics"}, &metrics); err != nil {
			return PoolAttemptResults{}, fmt.Errorf("getting test pool %s metrics: %w", id, err)
		}
		results := metrics.Attempts.Results
		if results.Failed > 0 || results.Errored > 0 || (metrics.Pool.State == "consumed" && metrics.Pool.Drained) {
			return results, nil
		}
		select {
		case <-ctx.Done():
			return PoolAttemptResults{}, fmt.Errorf("waiting for test pool %s results: %w", id, ctx.Err())
		case <-time.After(delay):
		}
		delay = min(delay*2, 5*time.Second)
	}
}

// requestPool is only for idempotent pool reads and fetch-or-create planning.
// Do not use it for lease creation: ambiguous failures are safe to retry here,
// but could create a second lease. Unlike doWithRetry, not every 409 is retried.
// After the retry budget runs out, it returns a RetryTimeoutError holding the
// last retryable failure, so callers can tell an unavailable Scheduler apart
// from a rejected request or the caller's own cancellation.
func (c *Client) requestPool(parent context.Context, method, endpoint string, body []byte, success int) (Pool, error) {
	ctx, cancel := context.WithTimeout(parent, retryTimeout)
	defer cancel()
	r := roko.NewRetrier(roko.TryForever(), roko.WithStrategy(roko.ExponentialSubsecond(initialDelay)), roko.WithJitter())
	var lastErr error
	// Like doWithRetry, report failed attempts and retries. Planning contention
	// is expected while another worker creates the pool, so it isn't reported.
	failed := false
	pool, err := roko.DoFunc(ctx, r, func(r *roko.Retrier) (_ Pool, err error) {
		if failed {
			fmt.Fprintf(os.Stderr, "bktec: Retrying API request (attempt %d)\n", r.AttemptCount()+1)
		}
		failed = false
		defer func() {
			// Errors caused by the retry deadline itself say nothing new.
			if err != nil && ctx.Err() == nil {
				lastErr = err
			}
		}()
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
			failed = ctx.Err() == nil
			if failed {
				printRetryError(err)
			}
			return Pool{}, err
		}
		defer resp.Body.Close()
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			failed = ctx.Err() == nil
			if failed {
				printRetryError(err)
			}
			return Pool{}, err
		}
		var message responseError
		_ = json.Unmarshal(data, &message)
		if method == http.MethodPost && resp.StatusCode == http.StatusConflict && message.Message == "Still creating test pool, please retry." {
			debug.Printf("Test pool is still being created; retrying")
			return Pool{}, &PoolError{StatusCode: resp.StatusCode, Message: message.Message}
		}
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			err := &PoolError{StatusCode: resp.StatusCode, Message: message.Message}
			failed = true
			printRetryError(err)
			return Pool{}, err
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
	if err != nil && parent.Err() == nil && ctx.Err() != nil {
		if lastErr == nil {
			lastErr = err
		}
		return Pool{}, &RetryTimeoutError{LastError: lastErr}
	}
	return pool, err
}

// PoolErroredError reports a pool whose server-side planning failed.
type PoolErroredError struct {
	ID      string
	Message string
}

func (e *PoolErroredError) Error() string {
	return fmt.Sprintf("test pool %s is errored: %s", e.ID, e.Message)
}

func poolFailure(pool Pool) error {
	message := "Test pool failed."
	if pool.Error != nil && pool.Error.Message != "" {
		message = pool.Error.Message
	}
	return &PoolErroredError{ID: pool.ID, Message: message}
}

// WaitForPool polls while planning and returns the full representation once
// leasing may begin. Incrementally populated pools can lease before population
// finishes; server-planned pools publish entries and muted tests atomically.
// A consumed pool is terminal and needs no lease, including an empty plan.
func (c *Client) WaitForPool(ctx context.Context, id string) (Pool, error) {
	delay := time.Second
	for {
		pool, err := c.GetPool(ctx, id)
		if err != nil {
			return Pool{}, err
		}
		switch pool.State {
		case "planning":
		case "populating", "consuming", "consumed":
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
