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
	ErrRetryLimitExceeded = errors.New("retry limit exceeded")

	errInvalidRequest = errors.New("request was invalid")
)

var (
	retryDelay = 5 * time.Second
)

// TestPlanParams represents the config params sent when fetching a test plan.
type TestPlanParams struct {
	SuiteToken  string     `json:"suite_token"`
	Mode        string     `json:"mode"`
	Identifier  string     `json:"identifier"`
	Parallelism int        `json:"parallelism"`
	Tests       plan.Tests `json:"tests"`
}

// FetchTestPlan fetches a test plan from the service, including retries.
func FetchTestPlan(ctx context.Context, splitterPath string, params TestPlanParams) (plan.TestPlan, error) {
	const retryMaxAttempts = 5
	const baseDelay = 2
	const delayAdjustment = 0

	r := roko.NewRetrier(
		roko.WithMaxAttempts(retryMaxAttempts),
		roko.WithStrategy(roko.Exponential(baseDelay, delayAdjustment)), // Using exponential backoff, the calculation is (2s ** attempts) + 0s
	)
	testPlan, err := roko.DoFunc(ctx, r, func(r *roko.Retrier) (plan.TestPlan, error) {
		tp, err := tryFetchTestPlan(ctx, splitterPath, params)
		// Don't retry if the request was invalid
		if errors.Is(err, errInvalidRequest) {
			r.Break()
		}
		return tp, err
	})

	if err != nil && r.AttemptCount() == retryMaxAttempts {
		return testPlan, fmt.Errorf("%w: %w", ErrRetryLimitExceeded, err)
	}
	return testPlan, err
}

// tryFetchTestPlan fetches a test plan from the service.
func tryFetchTestPlan(ctx context.Context, splitterPath string, params TestPlanParams) (plan.TestPlan, error) {
	// convert params to json string
	requestBody, err := json.Marshal(params)
	if err != nil {
		return plan.TestPlan{}, fmt.Errorf("converting params to JSON: %w", err)
	}

	reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	// create request
	postUrl := splitterPath + "/test-splitting/plan"
	r, err := http.NewRequestWithContext(reqCtx, "POST", postUrl, bytes.NewBuffer(requestBody))
	if err != nil {
		return plan.TestPlan{}, fmt.Errorf("creating request: %w", err)
	}
	r.Header.Add("Content-Type", "application/json")

	// send request
	client := &http.Client{}
	resp, err := client.Do(r)
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
