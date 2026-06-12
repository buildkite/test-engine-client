package api

import (
	"context"
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
