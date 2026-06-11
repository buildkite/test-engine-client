package api

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

type SchedulerPlanParams struct {
	OrganizationUUID   string `json:"organization_uuid"`
	SuiteUUID          string `json:"suite_uuid"`
	PipelineUUID       string `json:"pipeline_uuid"`
	BuildUUID          string `json:"build_uuid"`
	QueueName          string `json:"queue_name"`
	Ecosystem          string `json:"ecosystem"`
	Framework          string `json:"framework,omitempty"`
	TestPlanIdentifier string `json:"test_plan_identifier"`
}

type SchedulerPlanResponse struct {
	Run                 SchedulerRun `json:"run"`
	UploadedGroupsCount int          `json:"uploaded_groups_count"`
}

type SchedulerRun struct {
	UUID             string    `json:"uuid"`
	OrganizationUUID string    `json:"organization_uuid"`
	SuiteUUID        string    `json:"suite_uuid"`
	PipelineUUID     string    `json:"pipeline_uuid"`
	BuildUUID        string    `json:"build_uuid"`
	QueueName        string    `json:"queue_name"`
	Ecosystem        string    `json:"ecosystem"`
	Framework        string    `json:"framework"`
	ExpiresAt        time.Time `json:"expires_at"`
	CreatedAt        time.Time `json:"created_at"`
}

func (c Client) CreateSchedulerPlan(ctx context.Context, params SchedulerPlanParams) (SchedulerPlanResponse, error) {
	url := fmt.Sprintf("%s/v2/analytics/organizations/%s/scheduler/plan", c.ServerBaseURL, c.OrganizationSlug)

	var response SchedulerPlanResponse
	_, err := c.DoWithRetry(ctx, httpRequest{
		Method: http.MethodPost,
		URL:    url,
		Body:   params,
	}, &response)
	if err != nil {
		return SchedulerPlanResponse{}, err
	}

	return response, nil
}
