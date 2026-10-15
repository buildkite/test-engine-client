package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"uuid"

	"github.com/buildkite/test-engine-client/v3/internal/plan"
)

type LeaseAttempt struct {
	ID           string        `json:"id"`
	SelectorType string        `json:"selector_type"`
	Selector     plan.TestCase `json:"selector"`
	Costs        AttemptCosts  `json:"costs"`
}

// AttemptCosts holds only the pool's cost dimension, so at most one duration
// is set. Both are absent (zero) for pools scheduled by a custom cost. If
// attempts ever carry several costs, take the dimension from the pool's
// lease.costs instead.
type AttemptCosts struct {
	DurationP50MS int64 `json:"duration_p50_ms"`
	DurationP90MS int64 `json:"duration_p90_ms"`
}

type Lease struct {
	ID        string         `json:"id"`
	ExpiresAt time.Time      `json:"expires_at"`
	Attempts  []LeaseAttempt `json:"attempts"`
}

type LeaseResponse struct {
	Lease *Lease `json:"lease"`
	Pool  struct {
		ID    string `json:"id"`
		State string `json:"state"`
	} `json:"pool"`
}

type AttemptResult struct {
	AttemptID string `json:"attempt_id"`
	Result    string `json:"result"`
}

const LeaseErrorCodeMaximumLifetime = "MAXIMUM_LIFETIME"

// The key was used for a different request or its lease is no longer active.
// Nothing was leased, so a new key can acquire again.
const LeaseErrorCodeNotReplayable = "LEASE_NOT_REPLAYABLE"

// LeaseHTTPError retains structured response details for ownership-loss decisions.
type LeaseHTTPError struct {
	Status int
	Code   string
}

func (e *LeaseHTTPError) Error() string { return fmt.Sprintf("Scheduler returned HTTP %d", e.Status) }

// Retries reuse one idempotency key, so the Scheduler replays a lease it
// committed before the response was lost instead of leasing more attempts.
// An empty response commits nothing, so every call starts with a new key.
func (c *Client) AcquireLease(ctx context.Context, poolID string) (LeaseResponse, error) {
	var result LeaseResponse
	key := uuid.NewV7().String()
	err := c.leaseRequest(ctx, poolID, "", &key, map[string]int{"lease_ttl_seconds": 600}, &result, true)
	if err == nil && result.Lease != nil && result.Lease.ID != key {
		// Without the key a retry may have leased attempts that nothing will run.
		return result, fmt.Errorf("scheduler returned lease %s for idempotency key %s", result.Lease.ID, key)
	}
	return result, err
}

func (c *Client) HeartbeatLease(ctx context.Context, poolID, leaseID string) (time.Time, error) {
	var result struct {
		Leases []struct {
			ID        string    `json:"lease_id"`
			ExpiresAt time.Time `json:"expires_at"`
		} `json:"leases"`
	}
	err := c.leaseRequest(ctx, poolID, "/heartbeat", nil, map[string]any{"lease_ids": []string{leaseID}, "lease_ttl_seconds": 600}, &result, true)
	if err != nil {
		return time.Time{}, err
	}
	if len(result.Leases) != 1 || result.Leases[0].ID != leaseID || !result.Leases[0].ExpiresAt.After(time.Now()) {
		return time.Time{}, errors.New("invalid heartbeat response")
	}
	return result.Leases[0].ExpiresAt, nil
}

func (c *Client) CompleteLease(ctx context.Context, poolID, leaseID string, results []AttemptResult) error {
	body := map[string]any{"leases": []any{map[string]any{"lease_id": leaseID, "attempts": results}}}
	return c.leaseRequest(ctx, poolID, "/complete", nil, body, nil, true)
}

// Do not replay an ambiguous release: unlike completion the server does not
// promise idempotency.
func (c *Client) ReleaseLease(ctx context.Context, poolID, leaseID string) error {
	return c.leaseRequest(ctx, poolID, "/release", nil, map[string]any{"leases": []any{map[string]string{"id": leaseID, "reason": "Worker shutdown"}}}, nil, false)
}

// The shared doWithRetry retries ambiguous network failures and every 409.
// Lease requests retry ambiguous failures (transport errors, unreadable
// bodies, 5xx) only when replaySafe: heartbeat, same-result completion and
// keyed acquisition. Others retry only rate limits and connection-open timeouts.
// A keyed request whose key can no longer be replayed retries with a new key.
// Retries stop at the 90-second deadline.
func (c *Client) leaseRequest(ctx context.Context, poolID, operation string, idempotencyKey *string, body, out any, replaySafe bool) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	endpoint := strings.TrimRight(c.ServerBaseURL, "/") + "/v2/organizations/" + url.PathEscape(c.OrganizationSlug) + "/test-scheduler/pools/" + url.PathEscape(poolID) + "/leases" + operation
	delay := 250 * time.Millisecond
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
		if err != nil {
			return err
		}
		// Do not let net/http infer that lease creation can be automatically replayed.
		req.GetBody = nil
		req.Header.Set("Content-Type", "application/json")
		if idempotencyKey != nil {
			req.Header.Set("Idempotency-Key", *idempotencyKey)
		}
		hc := *c.httpClient
		hc.Timeout = 15 * time.Second
		hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		resp, err := hc.Do(req)
		retry := false
		wait := delay
		if err != nil {
			var op *net.OpError
			retry = replaySafe || (errors.As(err, &op) && op.Op == "dial" && op.Timeout())
		} else {
			raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
			resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 && out == nil {
				return nil
			}
			if resp.StatusCode == http.StatusOK && readErr == nil {
				readErr = json.Unmarshal(raw, out)
				if readErr == nil {
					return nil
				}
			}
			var responseError struct {
				Code string `json:"code"`
			}
			code := ""
			if unmarshalErr := json.Unmarshal(raw, &responseError); unmarshalErr == nil {
				code = responseError.Code
			}
			err = &LeaseHTTPError{Status: resp.StatusCode, Code: code}
			if readErr != nil {
				err = readErr
			}
			retry = resp.StatusCode == 429 || (replaySafe && (readErr != nil || resp.StatusCode >= 500))
			if idempotencyKey != nil && resp.StatusCode == http.StatusConflict && code == LeaseErrorCodeNotReplayable {
				*idempotencyKey = uuid.NewV7().String()
				retry = true
			}
			if resp.StatusCode == 429 {
				seconds := 0
				for _, prefix := range []string{"RateLimit-Job", "RateLimit", "RateLimit-Pool"} {
					reset, resetErr := strconv.Atoi(resp.Header.Get(prefix + "-Reset"))
					remaining, remainingErr := strconv.Atoi(resp.Header.Get(prefix + "-Remaining"))
					if resetErr == nil && reset > 0 && (remainingErr != nil || remaining <= 0) && reset > seconds {
						seconds = reset
					}
				}
				if seconds <= 0 {
					// Older Scheduler responses may only include reset in the body.
					var limit struct {
						Reset int `json:"reset"`
					}
					if json.Unmarshal(raw, &limit) == nil {
						seconds = limit.Reset
					}
				}
				if seconds > 0 {
					wait = time.Duration(seconds) * time.Second
				}
			}
		}
		if !retry {
			return err
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("scheduler retry: %w", ctx.Err())
		case <-timer.C:
		}
		if delay < 4*time.Second {
			delay *= 2
		}
	}
}
