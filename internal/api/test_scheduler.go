package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// SchedulerPool represents a Test Scheduler pool.
type SchedulerPool struct {
	ID             string `json:"id"`
	OrganizationID string `json:"organization_id"`
	SuiteID        string `json:"suite_id"`
	PipelineID     string `json:"pipeline_id"`
	BuildID        string `json:"build_id"`
	Key            string `json:"key"`
	State          string `json:"state"`
	ExpiresAt      string `json:"expires_at"`
	CreatedAt      string `json:"created_at"`
}

// CreateSchedulerPlanParams represents the request body for creating a Test
// Scheduler pool from a previously created test plan.
type CreateSchedulerPlanParams struct {
	OrganizationID     string `json:"organization_id"`
	SuiteID            string `json:"suite_id"`
	PipelineID         string `json:"pipeline_id"`
	BuildID            string `json:"build_id"`
	Key                string `json:"key"`
	TestPlanIdentifier string `json:"test_plan_identifier"`
	TTLSeconds         int    `json:"ttl_seconds,omitempty"`
}

// CreateSchedulerPlanResponse is the response from the Test Scheduler plan endpoint.
type CreateSchedulerPlanResponse struct {
	Pool                 SchedulerPool `json:"pool"`
	UploadedEntriesCount int           `json:"uploaded_entries_count"`
}

// CreateSchedulerPlan creates a Test Scheduler pool populated with the entries
// of the test plan identified by params.TestPlanIdentifier.
// A ConflictError is returned when the pool (pipeline_id, build_id, key)
// already exists; callers should recover via FetchSchedulerPools.
// ErrRetryTimeout is returned if the client failed to communicate with the server after exceeding the retry limit.
func (c Client) CreateSchedulerPlan(ctx context.Context, params CreateSchedulerPlanParams) (CreateSchedulerPlanResponse, error) {
	postURL := fmt.Sprintf("%s/v2/organizations/%s/test-scheduler/plan", c.ServerBaseURL, c.OrganizationSlug)

	var response CreateSchedulerPlanResponse
	_, err := c.DoWithRetry(ctx, httpRequest{
		Method:          http.MethodPost,
		URL:             postURL,
		Body:            params,
		noRetryConflict: true,
	}, &response)
	if err != nil {
		return CreateSchedulerPlanResponse{}, err
	}

	return response, nil
}

// FetchSchedulerPoolsParams are the query parameters for listing Test
// Scheduler pools.
type FetchSchedulerPoolsParams struct {
	PipelineID string
	BuildID    string
	Key        string
}

type fetchSchedulerPoolsResponse struct {
	Pools []SchedulerPool `json:"pools"`
}

// FetchSchedulerPools lists Test Scheduler pools matching the given pipeline,
// build and key. An empty slice is returned when no pool matches.
// ErrRetryTimeout is returned if the client failed to communicate with the server after exceeding the retry limit.
func (c Client) FetchSchedulerPools(ctx context.Context, params FetchSchedulerPoolsParams) ([]SchedulerPool, error) {
	query := url.Values{}
	query.Set("pipeline_id", params.PipelineID)
	query.Set("build_id", params.BuildID)
	query.Set("key", params.Key)
	getURL := fmt.Sprintf("%s/v2/organizations/%s/test-scheduler/pools?%s", c.ServerBaseURL, c.OrganizationSlug, query.Encode())

	var response fetchSchedulerPoolsResponse
	_, err := c.DoWithRetry(ctx, httpRequest{
		Method: http.MethodGet,
		URL:    getURL,
	}, &response)
	if err != nil {
		return nil, err
	}

	return response.Pools, nil
}

// SchedulerLeaseEntry is a single unit of work leased from a Test Scheduler
// pool. Selector and MetaData are arbitrary JSON; for plan-generated entries
// (type "file" or "example") the selector is `{"path": "spec/foo_spec.rb"}`.
type SchedulerLeaseEntry struct {
	ID         string          `json:"id"`
	Type       string          `json:"type"`
	Selector   json.RawMessage `json:"selector"`
	CustomCost float64         `json:"custom_cost"`
	Priority   int             `json:"priority"`
	MetaData   json.RawMessage `json:"meta_data"`
}

// SchedulerLease is a batch of entries leased to this worker.
type SchedulerLease struct {
	ID        string                `json:"id"`
	ExpiresAt string                `json:"expires_at"`
	Entries   []SchedulerLeaseEntry `json:"entries"`
}

// CreateSchedulerLeaseParams represents the request body for acquiring a lease.
type CreateSchedulerLeaseParams struct {
	TargetCostLimit float64 `json:"target_cost_limit,omitempty"`
	LeaseTTLSeconds int     `json:"lease_ttl_seconds,omitempty"`
}

// CreateSchedulerLeaseResponse is the response from the lease endpoint.
// Lease is nil when the pool has no waiting work.
type CreateSchedulerLeaseResponse struct {
	Lease *SchedulerLease `json:"lease"`
}

// CreateSchedulerLease acquires a lease of waiting entries from the pool.
// ErrRetryTimeout is returned if the client failed to communicate with the server after exceeding the retry limit.
func (c Client) CreateSchedulerLease(ctx context.Context, poolID string, params CreateSchedulerLeaseParams) (CreateSchedulerLeaseResponse, error) {
	postURL := fmt.Sprintf("%s/v2/organizations/%s/test-scheduler/pools/%s/leases", c.ServerBaseURL, c.OrganizationSlug, poolID)

	var response CreateSchedulerLeaseResponse
	_, err := c.DoWithRetry(ctx, httpRequest{
		Method:          http.MethodPost,
		URL:             postURL,
		Body:            params,
		noRetryConflict: true,
	}, &response)
	if err != nil {
		return CreateSchedulerLeaseResponse{}, err
	}

	return response, nil
}

// SchedulerLeaseRef identifies a lease in a complete request.
type SchedulerLeaseRef struct {
	ID string `json:"id"`
}

// CompleteSchedulerLeasesParams represents the request body for completing
// leases. Omitting entry IDs completes all still-leased entries under each
// lease, which is idempotent-safe: retrying after a lost response succeeds
// with an empty list.
type CompleteSchedulerLeasesParams struct {
	Leases []SchedulerLeaseRef `json:"leases"`
}

// CompleteSchedulerLeasesResponse is the response from the complete endpoint.
type CompleteSchedulerLeasesResponse struct {
	CompletedEntryIDs []string `json:"completed_entry_ids"`
}

// CompleteSchedulerLeases marks all still-leased entries under the given
// leases as completed. A ConflictError is returned when a lease is expired or
// not owned by this worker.
// ErrRetryTimeout is returned if the client failed to communicate with the server after exceeding the retry limit.
func (c Client) CompleteSchedulerLeases(ctx context.Context, poolID string, params CompleteSchedulerLeasesParams) (CompleteSchedulerLeasesResponse, error) {
	postURL := fmt.Sprintf("%s/v2/organizations/%s/test-scheduler/pools/%s/leases/complete", c.ServerBaseURL, c.OrganizationSlug, poolID)

	var response CompleteSchedulerLeasesResponse
	_, err := c.DoWithRetry(ctx, httpRequest{
		Method:          http.MethodPost,
		URL:             postURL,
		Body:            params,
		noRetryConflict: true,
	}, &response)
	if err != nil {
		return CompleteSchedulerLeasesResponse{}, err
	}

	return response, nil
}

// HeartbeatSchedulerLeasesParams represents the request body for extending leases.
type HeartbeatSchedulerLeasesParams struct {
	LeaseIDs        []string `json:"lease_ids"`
	LeaseTTLSeconds int      `json:"lease_ttl_seconds,omitempty"`
}

// SchedulerLeaseExpiry is a lease with its renewed expiry.
type SchedulerLeaseExpiry struct {
	ID        string `json:"id"`
	ExpiresAt string `json:"expires_at"`
}

// HeartbeatSchedulerLeasesResponse is the response from the heartbeat endpoint.
type HeartbeatSchedulerLeasesResponse struct {
	Leases []SchedulerLeaseExpiry `json:"leases"`
}

// HeartbeatSchedulerLeases extends the expiry of the given leases. A
// ConflictError is returned when a lease has expired.
// ErrRetryTimeout is returned if the client failed to communicate with the server after exceeding the retry limit.
func (c Client) HeartbeatSchedulerLeases(ctx context.Context, poolID string, params HeartbeatSchedulerLeasesParams) (HeartbeatSchedulerLeasesResponse, error) {
	postURL := fmt.Sprintf("%s/v2/organizations/%s/test-scheduler/pools/%s/leases/heartbeat", c.ServerBaseURL, c.OrganizationSlug, poolID)

	var response HeartbeatSchedulerLeasesResponse
	_, err := c.DoWithRetry(ctx, httpRequest{
		Method:          http.MethodPost,
		URL:             postURL,
		Body:            params,
		noRetryConflict: true,
	}, &response)
	if err != nil {
		return HeartbeatSchedulerLeasesResponse{}, err
	}

	return response, nil
}

// SchedulerPoolMetrics describes the state of a pool. Drained signals that all
// entries have been completed and workers can exit.
type SchedulerPoolMetrics struct {
	WaitingEntriesCount         int     `json:"waiting_entries_count"`
	LeasedEntriesCount          int     `json:"leased_entries_count"`
	CompletedEntriesCount       int     `json:"completed_entries_count"`
	TotalEntriesCount           int     `json:"total_entries_count"`
	OldestWaitingEntryCreatedAt *string `json:"oldest_waiting_entry_created_at"`
	WaitingCustomCostSum        float64 `json:"waiting_custom_cost_sum"`
	Drained                     bool    `json:"drained"`
}

type fetchSchedulerPoolMetricsResponse struct {
	Metrics SchedulerPoolMetrics `json:"metrics"`
}

// FetchSchedulerPoolMetrics fetches the metrics of a pool.
// ErrRetryTimeout is returned if the client failed to communicate with the server after exceeding the retry limit.
func (c Client) FetchSchedulerPoolMetrics(ctx context.Context, poolID string) (SchedulerPoolMetrics, error) {
	getURL := fmt.Sprintf("%s/v2/organizations/%s/test-scheduler/pools/%s/metrics", c.ServerBaseURL, c.OrganizationSlug, poolID)

	var response fetchSchedulerPoolMetricsResponse
	_, err := c.DoWithRetry(ctx, httpRequest{
		Method: http.MethodGet,
		URL:    getURL,
	}, &response)
	if err != nil {
		return SchedulerPoolMetrics{}, err
	}

	return response.Metrics, nil
}
