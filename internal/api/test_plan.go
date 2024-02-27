package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const retryLimit = 5

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

// Task represents the task for the given node.
type Task struct {
	NodeNumber int   `json:"node_number"`
	Tests      Tests `json:"tests"`
}

// TestPlan represents the entire test plan.
type TestPlan struct {
	Tasks map[string]Task `json:"tasks"`
}

// TestPlanParams represents the config params sent when fetching a test plan.
type TestPlanParams struct {
	SuiteToken  string `json:"suite_token"`
	Mode        string `json:"mode"`
	Identifier  string `json:"identifier"`
	Parallelism int    `json:"parallelism"`
	Tests       Tests  `json:"tests"`
}

func MakeAPICall(r *http.Request, client *http.Client) (*http.Response, error) {
	resp, err := client.Do(r)
	if err != nil {
		return resp, fmt.Errorf("sending request: %w", err)
	}

	defer resp.Body.Close()
	return resp, nil
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
	i := 0
retryLoop:
	for i < retryLimit {
		// resp, err := client.Do(r)
		// if err != nil {
		// 	return TestPlan{}, fmt.Errorf("sending request: %w", err)
		// }
		// defer resp.Body.Close()

		resp, err := MakeAPICall(r, client)
		if err != nil {
			return TestPlan{}, fmt.Errorf("sending request: %w", err)
		}
		// TODO: check the response status code
		switch {
		case resp.StatusCode == http.StatusOK:
			// This is our happy path
			break retryLoop
		case resp.StatusCode >= 400 && resp.StatusCode < 500:
			return TestPlan{}, fmt.Errorf("server response: %d", resp.StatusCode)
		case resp.StatusCode >= 500 && resp.StatusCode < 600:
			// retr
			// time.Sleep // consider fuzzed exponential backoff
		}
	}

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
