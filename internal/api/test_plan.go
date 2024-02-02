package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// TestCase represents a single test case.
type TestCase struct {
	Path              string `json:"path"`
	EstimatedDuration *int   `json:"estimated_duration"`
}

// Tests represents a set of tests.
type Tests struct {
	Cases  []TestCase `json:"cases"`
	Format string     `json:"format"`
}

type Task struct {
	NodeNumber int   `json:"node_number"`
	Tests      Tests `json:"tests"`
}

type TestPlan struct {
	Tasks map[string]Task `json:"tasks"`
}

type TestPlanParams struct {
	SuiteToken  string `json:"suite_token"`
	Mode        string `json:"mode"`
	Identifier  string `json:"identifier"`
	Parallelism int    `json:"parallelism"`
	Tests       Tests  `json:"tests"`
}

// FetchTestPlan fetches a test plan from the service.
func FetchTestPlan(splitterPath string, params TestPlanParams) (TestPlan, error) {
	// convert params to json string
	requestBody, err := json.Marshal(params)
	if err != nil {
		return TestPlan{}, fmt.Errorf("converting params to JSON: %w", err)
	}

	// create request
	postUrl := splitterPath + "/test-splitting/plan"
	r, err := http.NewRequest("POST", postUrl, bytes.NewBuffer(requestBody))
	if err != nil {
		return TestPlan{}, fmt.Errorf("creating request: %w", err)
	}
	r.Header.Add("Content-Type", "application/json")

	// send request
	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		return TestPlan{}, fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	// TODO: check the response status code

	// read response
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return TestPlan{}, fmt.Errorf("reading response body: %w", err)
	}

	// parse response
	var testPlan TestPlan
	err = json.Unmarshal(responseBody, &testPlan)
	if err != nil {
		return TestPlan{}, fmt.Errorf("parsing response: %w", err)
	}

	return testPlan, nil
}