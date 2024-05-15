package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/buildkite/roko"
	"github.com/buildkite/test-splitter/internal/plan"
)

var (
	errInvalidRequest = errors.New("request was invalid")
)

// TestPlanParams represents the config params sent when fetching a test plan.
type TestPlanParams struct {
	Mode        string     `json:"mode"`
	Identifier  string     `json:"identifier"`
	Parallelism int        `json:"parallelism"`
	Tests       plan.Tests `json:"tests"`
}

// CreateTestPlan creates a test plan from the service, including retries.
func (c client) CreateTestPlan(ctx context.Context, suiteSlug string, params TestPlanParams) (plan.TestPlan, error) {
	// Retry using exponential backoff offerred by roko
	// https://pkg.go.dev/github.com/buildkite/roko#ExponentialSubsecond
	//
	// The formula defined by roko is: delay = initialDelay ** (retries/16 + 1)
	// Example of 3s initial delay growing over 6 attempts:
	//  3s    → 5s    → 8s   → 13s   → 22s
	// for a total retry delay of 51 seconds between attempts.
	// Each request times out after 12 seconds, chosen to provide some
	// headroom on top of the goal p99 time to fetch of 10s.
	// So in the worst case only 3 full attempts and 1 partial attempt
	// may be made to fetch a test plan before the overall 70 second
	// timeout.

	// Initial retry delay for fetching test plans.
	// This is a variable so it can be overridden in tests.
	// See comment in FetchTestPlan.
	const initialDelay = 3000 * time.Millisecond

	r := roko.NewRetrier(
		roko.TryForever(),
		roko.WithStrategy(roko.ExponentialSubsecond(initialDelay)),
		roko.WithJitter(),
	)
	testPlan, err := roko.DoFunc(ctx, r, func(r *roko.Retrier) (plan.TestPlan, error) {
		tp, err := c.tryCreateTestPlan(ctx, suiteSlug, params)
		// Don't retry if the request was invalid
		if errors.Is(err, errInvalidRequest) {
			r.Break()
		}
		return tp, err
	})

	return testPlan, err
}

// tryCreateTestPlan creates a test plan from the service.
func (c client) tryCreateTestPlan(ctx context.Context, suiteSlug string, params TestPlanParams) (plan.TestPlan, error) {
	// convert params to json string
	requestBody, err := json.Marshal(params)
	if err != nil {
		return plan.TestPlan{}, fmt.Errorf("converting params to JSON: %w", err)
	}

	// See above for explanation of 15 seconds.
	reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	// create request
	postUrl := fmt.Sprintf("%s/v2/analytics/organizations/%s/suites/%s/test_plan", c.ServerBaseUrl, c.OrganizationSlug, suiteSlug)
	r, err := http.NewRequestWithContext(reqCtx, "POST", postUrl, bytes.NewBuffer(requestBody))
	if err != nil {
		return plan.TestPlan{}, fmt.Errorf("creating request: %w", err)
	}
	r.Header.Add("Content-Type", "application/json")

	// send request
	resp, err := c.httpClient.Do(r)
	if err != nil {
		return plan.TestPlan{}, fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusOK:
		// This is our happy path

	case resp.StatusCode >= 400 && resp.StatusCode < 500:
		return plan.TestPlan{}, fmt.Errorf("%w: server response: %d", errInvalidRequest, resp.StatusCode)

	case resp.StatusCode >= 500 && resp.StatusCode < 600:
		return plan.TestPlan{}, fmt.Errorf("server response: %d", resp.StatusCode)
	}

	// read response
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return plan.TestPlan{}, fmt.Errorf("reading response body: %w", err)
	}

	// parse response
	var testPlan plan.TestPlan
	err = json.Unmarshal(responseBody, &testPlan)
	if err != nil {
		return plan.TestPlan{}, fmt.Errorf("parsing response: %w", err)
	}

	return testPlan, nil
}
