// Package testqueue implements the experimental Test Engine queue API client.
package testqueue

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/buildkite/test-engine-client/v2/internal/plan"
)

// Client is a JSON HTTP client for the queue service.
type Client struct {
	AccessToken   string
	ServerBaseURL string
	httpClient    *http.Client
}

// NewClient creates a queue API client.
func NewClient(serverBaseURL, accessToken string) *Client {
	return &Client{
		AccessToken:   accessToken,
		ServerBaseURL: strings.TrimRight(serverBaseURL, "/"),
		httpClient:    &http.Client{Timeout: 30 * time.Second},
	}
}

// QueueRef identifies a logical queue.
type QueueRef struct {
	QueueUUID        string `json:"queue_uuid,omitempty"`
	OrganizationUUID string `json:"organization_uuid"`
	OrganizationSlug string `json:"organization_slug,omitempty"`
	SuiteUUID        string `json:"suite_uuid"`
	SuiteSlug        string `json:"suite_slug,omitempty"`
	RunUUID          string `json:"run_uuid,omitempty"`
	BuildUUID        string `json:"build_uuid"`
	PipelineSlug     string `json:"pipeline_slug"`
	StepKey          string `json:"step_key,omitempty"`
	QueueName        string `json:"queue_name"`
}

// QueueEntry is a test case to enqueue.
type QueueEntry struct {
	UUID          string         `json:"uuid"`
	Test          plan.TestCase  `json:"test"`
	Metadata      map[string]any `json:"metadata,omitempty"`
	IsRetry       bool           `json:"is_retry,omitempty"`
	QueuePriority int            `json:"queue_priority,omitempty"`
}

// LeasedEntry is a test case leased from the queue.
type LeasedEntry struct {
	UUID           string         `json:"uuid"`
	Test           plan.TestCase  `json:"test"`
	Metadata       map[string]any `json:"metadata"`
	IsRetry        bool           `json:"is_retry"`
	QueuePriority  int            `json:"queue_priority"`
	Attempt        int            `json:"attempt"`
	LeaseID        string         `json:"lease_id"`
	LeaseExpiresAt time.Time      `json:"lease_expires_at"`
}

// QueueMetrics is the current operational state of a queue.
type QueueMetrics struct {
	QueueUUID            string     `json:"queue_uuid"`
	State                string     `json:"state"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
	ExpiresAt            time.Time  `json:"expires_at"`
	QueuedEntries        int64      `json:"queued_entries"`
	LeasedEntries        int64      `json:"leased_entries"`
	ExpiredLeasedEntries int64      `json:"expired_leased_entries"`
	TotalEntries         int64      `json:"total_entries"`
	OldestQueuedAt       *time.Time `json:"oldest_queued_at,omitempty"`
	OldestLeasedAt       *time.Time `json:"oldest_leased_at,omitempty"`
	OldestLeaseExpiresAt *time.Time `json:"oldest_lease_expires_at,omitempty"`
}

// PushBatch creates or finds a queue and inserts entries.
func (c *Client) PushBatch(ctx context.Context, queue QueueRef, entries []QueueEntry) (string, int, error) {
	var response struct {
		QueueUUID string `json:"queue_uuid"`
		Inserted  int    `json:"inserted"`
	}
	err := c.do(ctx, http.MethodPost, "/v1/queues/push", map[string]any{
		"queue":   queue,
		"entries": entries,
	}, &response)
	return response.QueueUUID, response.Inserted, err
}

// PopBatch leases entries from a queue.
func (c *Client) PopBatch(ctx context.Context, queueUUID string, limit int, leaseDurationSeconds int, leaseOwner string) (string, []LeasedEntry, bool, error) {
	var response struct {
		LeaseID string        `json:"lease_id"`
		Entries []LeasedEntry `json:"entries"`
		Drained bool          `json:"drained"`
	}
	request := map[string]any{
		"queue_uuid":             queueUUID,
		"limit":                  limit,
		"lease_duration_seconds": leaseDurationSeconds,
	}
	if leaseOwner != "" {
		request["lease_owner"] = leaseOwner
	}

	err := c.do(ctx, http.MethodPost, "/v1/queues/pop", request, &response)
	return response.LeaseID, response.Entries, response.Drained, err
}

// CompleteLease deletes completed leased entries.
func (c *Client) CompleteLease(ctx context.Context, queueUUID string, leaseID string, entryUUIDs []string) (int, error) {
	var response struct {
		Deleted int `json:"deleted"`
	}
	err := c.do(ctx, http.MethodPost, "/v1/queues/complete", map[string]any{
		"queue_uuid":  queueUUID,
		"lease_id":    leaseID,
		"entry_uuids": entryUUIDs,
	}, &response)
	return response.Deleted, err
}

// CompleteLeaseAndPush atomically completes leased entries and enqueues follow-up entries.
func (c *Client) CompleteLeaseAndPush(ctx context.Context, queueUUID string, leaseID string, entryUUIDs []string, entries []QueueEntry) (int, int, error) {
	var response struct {
		Deleted  int `json:"deleted"`
		Inserted int `json:"inserted"`
	}
	err := c.do(ctx, http.MethodPost, "/v1/queues/complete_and_push", map[string]any{
		"queue_uuid":  queueUUID,
		"lease_id":    leaseID,
		"entry_uuids": entryUUIDs,
		"entries":     entries,
	}, &response)
	return response.Deleted, response.Inserted, err
}

// RequeueLease returns leased entries to the queue.
func (c *Client) RequeueLease(ctx context.Context, queueUUID string, leaseID string) (int, error) {
	var response struct {
		Requeued int `json:"requeued"`
	}
	err := c.do(ctx, http.MethodPost, "/v1/queues/requeue", map[string]any{
		"queue_uuid": queueUUID,
		"lease_id":   leaseID,
	}, &response)
	return response.Requeued, err
}

// HeartbeatLease extends an active lease.
func (c *Client) HeartbeatLease(ctx context.Context, queueUUID string, leaseID string, extendSeconds int) (time.Time, error) {
	var response struct {
		LeaseExpiresAt time.Time `json:"lease_expires_at"`
	}
	err := c.do(ctx, http.MethodPost, "/v1/queues/heartbeat", map[string]any{
		"queue_uuid":     queueUUID,
		"lease_id":       leaseID,
		"extend_seconds": extendSeconds,
	}, &response)
	return response.LeaseExpiresAt, err
}

// GetMetrics returns the current operational state of a queue.
func (c *Client) GetMetrics(ctx context.Context, queueUUID string) (QueueMetrics, error) {
	var response QueueMetrics
	err := c.do(ctx, http.MethodPost, "/v1/queues/metrics", map[string]any{
		"queue_uuid": queueUUID,
	}, &response)
	return response, err
}

// CloseQueue marks a queue as closed after producers have finished pushing entries.
func (c *Client) CloseQueue(ctx context.Context, queueUUID string) error {
	return c.do(ctx, http.MethodPost, "/v1/queues/close", map[string]any{
		"queue_uuid": queueUUID,
	}, nil)
}

func (c *Client) do(ctx context.Context, method string, path string, body any, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encoding queue request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.ServerBaseURL+path, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("creating queue request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.AccessToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.AccessToken)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("sending queue request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading queue response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		var responseError struct {
			Error   string `json:"error"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal(responseBody, &responseError); err == nil {
			if responseError.Error != "" {
				return fmt.Errorf("queue request failed: %s", responseError.Error)
			}
			if responseError.Message != "" {
				return fmt.Errorf("queue request failed: %s", responseError.Message)
			}
		}
		return fmt.Errorf("queue request failed with status %d", resp.StatusCode)
	}

	if out != nil && len(responseBody) > 0 {
		if err := json.Unmarshal(responseBody, out); err != nil {
			return fmt.Errorf("decoding queue response: %w", err)
		}
	}

	return nil
}
