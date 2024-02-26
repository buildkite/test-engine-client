package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
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
	if resp.StatusCode != http.StatusOK {
		fmt.Println("Non-OK HTTP status:", resp.StatusCode)
		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			// terminate program
			log.Fatalf("Cannot process the request")
		} else {
			// fallback to naive split
			fmt.Printf("5xx error")
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
