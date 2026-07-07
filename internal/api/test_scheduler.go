package api

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/buildkite/test-engine-client/v2/internal/plan"
)

type TestSchedulerPool struct {
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

type TestSchedulerPoolResponse struct {
	Pool TestSchedulerPool `json:"pool"`
}

type CreateTestSchedulerPoolParams struct {
	Suite      string `json:"suite"`
	Pipeline   string `json:"pipeline"`
	BuildID    string `json:"build_id"`
	Key        string `json:"key"`
	TTLSeconds int    `json:"ttl_seconds,omitempty"`
}

type TestSchedulerEntrySelector struct {
	Format            plan.TestCaseFormat `json:"format"`
	Path              string              `json:"path,omitempty"`
	Value             string              `json:"value,omitempty"`
	Identifier        string              `json:"identifier,omitempty"`
	Name              string              `json:"name,omitempty"`
	Scope             string              `json:"scope,omitempty"`
	EstimatedDuration int                 `json:"estimated_duration,omitempty"`
	TimingSampleSize  int                 `json:"timing_sample_size,omitempty"`
}

type TestSchedulerEntryParams struct {
	SelectorType  string                     `json:"selector_type"`
	Selector      TestSchedulerEntrySelector `json:"selector"`
	Costs         map[string]float64         `json:"costs,omitempty"`
	Priority      int                        `json:"priority,omitempty"`
	AffinityGroup *string                    `json:"affinity_group,omitempty"`
	MetaData      map[string]any             `json:"meta_data,omitempty"`
}

type CreateTestSchedulerEntriesParams struct {
	Entries []TestSchedulerEntryParams `json:"entries"`
}

type TestSchedulerEntry struct {
	ID            string                     `json:"id"`
	SelectorType  string                     `json:"selector_type"`
	Selector      TestSchedulerEntrySelector `json:"selector"`
	Costs         map[string]float64         `json:"costs"`
	Priority      int                        `json:"priority"`
	AffinityGroup *string                    `json:"affinity_group"`
	MetaData      map[string]any             `json:"meta_data"`
}

type CreateTestSchedulerEntriesResponse struct {
	Entries []TestSchedulerEntry `json:"entries"`
}

type CreateTestSchedulerLeaseParams struct {
	TargetCostLimit map[string]float64 `json:"target_cost_limit,omitempty"`
	LeaseTTLSeconds int                `json:"lease_ttl_seconds,omitempty"`
}

type TestSchedulerAttempt struct {
	ID             string                     `json:"id"`
	PoolID         string                     `json:"pool_id"`
	EntryID        string                     `json:"entry_id"`
	AttemptIndex   int                        `json:"attempt_index"`
	SelectorType   string                     `json:"selector_type"`
	Selector       TestSchedulerEntrySelector `json:"selector"`
	Costs          map[string]float64         `json:"costs"`
	Priority       int                        `json:"priority"`
	AffinityGroup  *string                    `json:"affinity_group"`
	State          string                     `json:"state"`
	Result         *string                    `json:"result"`
	LeaseID        string                     `json:"lease_id"`
	LeaseExpiresAt string                     `json:"lease_expires_at"`
	CompletedAt    *string                    `json:"completed_at"`
	MetaData       map[string]any             `json:"meta_data"`
	CreatedAt      string                     `json:"created_at"`
}

type TestSchedulerLease struct {
	ID        string                 `json:"id"`
	ExpiresAt string                 `json:"expires_at"`
	Attempts  []TestSchedulerAttempt `json:"attempts"`
}

type CreateTestSchedulerLeaseResponse struct {
	Lease *TestSchedulerLease `json:"lease"`
}

type CompleteTestSchedulerLeasesParams struct {
	Leases []CompleteTestSchedulerLeaseParams `json:"leases"`
}

type CompleteTestSchedulerLeaseParams struct {
	LeaseID  string                               `json:"lease_id"`
	Attempts []CompleteTestSchedulerAttemptParams `json:"attempts"`
}

type CompleteTestSchedulerAttemptParams struct {
	AttemptID string `json:"attempt_id"`
	Result    string `json:"result"`
}

type CompleteTestSchedulerLeasesResponse struct {
	Leases []struct {
		LeaseID  string `json:"lease_id"`
		Attempts []struct {
			ID               string `json:"id"`
			State            string `json:"state"`
			Result           string `json:"result"`
			LeaseID          string `json:"lease_id"`
			CompletionStatus string `json:"completion_status"`
		} `json:"attempts"`
	} `json:"leases"`
	Pool TestSchedulerPool `json:"pool"`
}

func (c Client) CreateTestSchedulerPool(ctx context.Context, params CreateTestSchedulerPoolParams) (TestSchedulerPool, error) {
	postURL := fmt.Sprintf("%s/v2/organizations/%s/test-scheduler/pools", c.ServerBaseURL, url.PathEscape(c.OrganizationSlug))
	var response TestSchedulerPoolResponse
	_, err := c.doJSONWithRetryExpected(ctx, httpRequest{
		Method: http.MethodPost,
		URL:    postURL,
		Body:   params,
	}, &response, false, http.StatusCreated)
	return response.Pool, err
}

func (c Client) GetTestSchedulerPool(ctx context.Context, poolID string) (TestSchedulerPool, error) {
	getURL := fmt.Sprintf("%s/v2/organizations/%s/test-scheduler/pools/%s", c.ServerBaseURL, url.PathEscape(c.OrganizationSlug), url.PathEscape(poolID))
	var response TestSchedulerPoolResponse
	_, err := c.doJSONWithRetryExpected(ctx, httpRequest{
		Method: http.MethodGet,
		URL:    getURL,
	}, &response, false, http.StatusOK)
	return response.Pool, err
}

func (c Client) CreateTestSchedulerEntries(ctx context.Context, poolID string, entries []TestSchedulerEntryParams) ([]TestSchedulerEntry, error) {
	postURL := fmt.Sprintf("%s/v2/organizations/%s/test-scheduler/pools/%s/entries", c.ServerBaseURL, url.PathEscape(c.OrganizationSlug), url.PathEscape(poolID))
	var response CreateTestSchedulerEntriesResponse
	_, err := c.doJSONWithRetryExpected(ctx, httpRequest{
		Method: http.MethodPost,
		URL:    postURL,
		Body: CreateTestSchedulerEntriesParams{
			Entries: entries,
		},
	}, &response, false, http.StatusCreated)
	return response.Entries, err
}

func (c Client) CreateTestSchedulerLease(ctx context.Context, poolID string, params CreateTestSchedulerLeaseParams) (*TestSchedulerLease, error) {
	postURL := fmt.Sprintf("%s/v2/organizations/%s/test-scheduler/pools/%s/leases", c.ServerBaseURL, url.PathEscape(c.OrganizationSlug), url.PathEscape(poolID))
	var response CreateTestSchedulerLeaseResponse
	_, err := c.doJSONWithRetryExpected(ctx, httpRequest{
		Method: http.MethodPost,
		URL:    postURL,
		Body:   params,
	}, &response, false, http.StatusOK)
	return response.Lease, err
}

func (c Client) CompleteTestSchedulerLeases(ctx context.Context, poolID string, params CompleteTestSchedulerLeasesParams) (CompleteTestSchedulerLeasesResponse, error) {
	postURL := fmt.Sprintf("%s/v2/organizations/%s/test-scheduler/pools/%s/leases/complete", c.ServerBaseURL, url.PathEscape(c.OrganizationSlug), url.PathEscape(poolID))
	var response CompleteTestSchedulerLeasesResponse
	_, err := c.doJSONWithRetryExpected(ctx, httpRequest{
		Method: http.MethodPost,
		URL:    postURL,
		Body:   params,
	}, &response, false, http.StatusOK)
	return response, err
}
