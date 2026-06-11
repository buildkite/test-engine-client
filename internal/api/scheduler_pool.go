package api

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

type SchedulerPoolResponse struct {
	Run SchedulerRun `json:"run"`
}

func (c Client) FetchSchedulerPool(ctx context.Context, pipelineUUID, buildUUID, queueName string) (SchedulerPoolResponse, error) {
	params := url.Values{}
	params.Set("pipeline_uuid", pipelineUUID)
	params.Set("build_uuid", buildUUID)
	params.Set("queue_name", queueName)

	requestURL := fmt.Sprintf("%s/v2/analytics/organizations/%s/scheduler/pools?%s", c.ServerBaseURL, c.OrganizationSlug, params.Encode())

	var response SchedulerPoolResponse
	_, err := c.DoWithRetry(ctx, httpRequest{
		Method: http.MethodGet,
		URL:    requestURL,
	}, &response)
	if err != nil {
		return SchedulerPoolResponse{}, err
	}

	return response, nil
}

type SchedulerLeaseParams struct {
	JobUUID         string `json:"job_uuid"`
	CostLimit       int    `json:"cost_limit,omitempty"`
	LeaseTTLSeconds int    `json:"lease_ttl_seconds,omitempty"`
}

type SchedulerLeasesResponse struct {
	Leases []SchedulerLease `json:"leases"`
}

type SchedulerLease struct {
	LeaseUUID      string         `json:"lease_uuid"`
	LeaseExpiresAt time.Time      `json:"lease_expires_at"`
	Group          SchedulerGroup `json:"group"`
}

type SchedulerGroup struct {
	UUID              string              `json:"uuid"`
	Selectors         []SchedulerSelector `json:"selectors"`
	CustomCost        *float64            `json:"custom_cost"`
	Priority          int                 `json:"priority"`
	RetryContinuation any                 `json:"retry_continuation"`
	MetaData          map[string]any      `json:"meta_data"`
}

type SchedulerSelector struct {
	Type  string `json:"type,omitempty"`
	Value any    `json:"value"`
}

func (c Client) LeaseSchedulerGroups(ctx context.Context, poolUUID string, params SchedulerLeaseParams) (SchedulerLeasesResponse, error) {
	requestURL := fmt.Sprintf("%s/v2/analytics/organizations/%s/scheduler/pools/%s/leases", c.ServerBaseURL, c.OrganizationSlug, poolUUID)

	var response SchedulerLeasesResponse
	_, err := c.DoWithRetry(ctx, httpRequest{
		Method: http.MethodPost,
		URL:    requestURL,
		Body:   params,
	}, &response)
	if err != nil {
		return SchedulerLeasesResponse{}, err
	}

	return response, nil
}

type CompleteSchedulerLeasesParams struct {
	LeaseUUIDs []string `json:"lease_uuids"`
}

func (c Client) CompleteSchedulerLeases(ctx context.Context, poolUUID string, params CompleteSchedulerLeasesParams) error {
	requestURL := fmt.Sprintf("%s/v2/analytics/organizations/%s/scheduler/pools/%s/leases/complete", c.ServerBaseURL, c.OrganizationSlug, poolUUID)

	_, err := c.DoWithRetry(ctx, httpRequest{
		Method: http.MethodPost,
		URL:    requestURL,
		Body:   params,
	}, nil)
	return err
}
