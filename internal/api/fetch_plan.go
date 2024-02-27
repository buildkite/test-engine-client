package api

import (
	"bytes"
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

// TestPlanParams represents the config params sent when fetching a test plan.
type TestPlanParams struct {
	SuiteToken  string     `json:"suite_token"`
	Mode        string     `json:"mode"`
	Identifier  string     `json:"identifier"`
	Parallelism int        `json:"parallelism"`
	Tests       plan.Tests `json:"tests"`
}

// FetchTestPlan fetches a test plan from the service, including retries.
func FetchTestPlan(splitterPath string, params TestPlanParams) (plan.TestPlan, error) {
	var testPlan plan.TestPlan

	r := roko.NewRetrier(
		roko.WithMaxAttempts(5),
		roko.WithStrategy(roko.Constant(5*time.Second)),
	)
	err := r.Do(func(r *roko.Retrier) error {
		var err error
		testPlan, err = tryFetchTestPlan(splitterPath, params)
		// Don't retry if the request was invalid
		if errors.Is(err, errInvalidRequest) {
			r.Break()
		}
		return err
	})

	if err != nil && r.AttemptCount() == 5 {
		return testPlan, fmt.Errorf("%w: %w", ErrRetryLimitExceeded, err)
	}
	return testPlan, err
}

// tryFetchTestPlan fetches a test plan from the service.
func tryFetchTestPlan(splitterPath string, params TestPlanParams) (plan.TestPlan, error) {
	// convert params to json string
	requestBody, err := json.Marshal(params)
	if err != nil {
		return plan.TestPlan{}, fmt.Errorf("converting params to JSON: %w", err)
	}

	// create request
	postUrl := splitterPath + "/test-splitting/plan"
	r, err := http.NewRequest("POST", postUrl, bytes.NewBuffer(requestBody))
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

	// TODO: check the response status code

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
