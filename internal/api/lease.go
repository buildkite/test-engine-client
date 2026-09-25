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

	"github.com/buildkite/test-engine-client/v3/internal/plan"
)

type LeaseAttempt struct {
	ID           string        `json:"id"`
	SelectorType string        `json:"selector_type"`
	Selector     plan.TestCase `json:"selector"`
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

// LeaseHTTPError retains status for ownership-loss decisions without leaking credentials.
type LeaseHTTPError struct{ Status int }

func (e *LeaseHTTPError) Error() string { return fmt.Sprintf("Scheduler returned HTTP %d", e.Status) }

func (c *Client) AcquireLease(ctx context.Context, poolID string) (LeaseResponse, error) {
	var result LeaseResponse
	err := c.leaseRequest(ctx, poolID, "", map[string]int{"lease_ttl_seconds": 600}, &result, false)
	return result, err
}

func (c *Client) HeartbeatLease(ctx context.Context, poolID, leaseID string) (time.Time, error) {
	var result struct {
		Leases []struct {
			ID        string    `json:"lease_id"`
			ExpiresAt time.Time `json:"expires_at"`
		} `json:"leases"`
	}
	err := c.leaseRequest(ctx, poolID, "/heartbeat", map[string]any{"lease_ids": []string{leaseID}, "lease_ttl_seconds": 600}, &result, true)
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
	var response struct {
		Leases []struct {
			ID       string `json:"lease_id"`
			Attempts []struct {
				ID     string `json:"id"`
				Result string `json:"result"`
				Status string `json:"completion_status"`
			} `json:"attempts"`
		} `json:"leases"`
	}
	if err := c.leaseRequest(ctx, poolID, "/complete", body, &response, true); err != nil {
		return err
	}
	if len(response.Leases) != 1 || response.Leases[0].ID != leaseID || len(response.Leases[0].Attempts) != len(results) {
		return errors.New("invalid completion response")
	}
	for i, got := range response.Leases[0].Attempts {
		// #nosec G602 -- the response and request lengths were checked above.
		if got.ID != results[i].AttemptID || got.Result != results[i].Result || (got.Status != "completed" && got.Status != "already_completed") {
			return errors.New("invalid completion acknowledgement")
		}
	}
	return nil
}

// Release is used only for an entirely undispatched lease. Do not replay an
// ambiguous release: unlike completion the server does not promise idempotency.
func (c *Client) ReleaseLease(ctx context.Context, poolID, leaseID string) error {
	return c.leaseRequest(ctx, poolID, "/release", map[string]any{"leases": []any{map[string]string{"id": leaseID, "reason": "worker stopped before dispatch"}}}, nil, false)
}

// The shared doWithRetry retries ambiguous network failures and every 409.
// Acquiring another lease after a lost response could strand the first one,
// so only explicit rate limits and connection-open timeouts can retry it.
// Heartbeat and same-result completion can replay safely after uncertainty.
func (c *Client) leaseRequest(ctx context.Context, poolID, operation string, body, out any, replaySafe bool) error {
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
			if resp.StatusCode == http.StatusOK && readErr == nil {
				if out == nil {
					return nil
				}
				readErr = json.Unmarshal(raw, out)
				if readErr == nil {
					return nil
				}
			}
			err = &LeaseHTTPError{Status: resp.StatusCode}
			if readErr != nil {
				err = readErr
			}
			retry = resp.StatusCode == 429 || (replaySafe && (readErr != nil || resp.StatusCode >= 500))
			if resp.StatusCode == 429 {
				seconds, _ := strconv.Atoi(resp.Header.Get("RateLimit-Reset"))
				if seconds <= 0 {
					// The Scheduler's job quota sends reset in the 429 body, not
					// Retry-After or RateLimit-Reset.
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
