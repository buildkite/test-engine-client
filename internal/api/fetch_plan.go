package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/buildkite/test-splitter/internal/plan"
)

// FetchTestPlan fetches a test plan from the service.
func FetchTestPlan(splitterPath string, params plan.TestPlanParams) (plan.TestPlan, error) {
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
