// Package agent talks to the Buildkite Agent API (agent.buildkite.com).
//
// This is deliberately separate from internal/api, which talks to the Test
// Engine API with a different base URL and a different access token. The Agent
// API is the service that owns a running job, so it is the only place that can
// accept a "promised failure" for that job.
package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"

	"github.com/buildkite/test-engine-client/v2/internal/version"
)

// promiseFailureRequest is the JSON body sent to the promise_failure endpoint.
// The field names match the Buildkite Agent API contract exercised by the
// promised-failure-cascade-tests harness (PB-1665):
//
//	{"exit_status": 1, "reason": "test_failure"}
type promiseFailureRequest struct {
	ExitStatus int    `json:"exit_status"`
	Reason     string `json:"reason"`
}

// PromiseFailure tells the Buildkite Agent API that the current job is going to
// finish with a non-zero exit status, before the job actually exits. This lets
// the build "cascade" to failing early.
//
// It mirrors the curl the cascade-test pipelines use:
//
//	PUT  {endpoint}/jobs/{jobID}/promise_failure
//	Authorization: Token {accessToken}
//	Content-Type: application/json
//	{"exit_status": 1, "reason": "..."}
//
// endpoint and accessToken come from the job environment (BUILDKITE_AGENT_ENDPOINT
// and BUILDKITE_AGENT_ACCESS_TOKEN), which the agent injects into every job.
//
// This call is best-effort by contract: callers should log a failure and carry
// on, never changing the test run's real exit status because of a promise error.
func PromiseFailure(ctx context.Context, httpClient *http.Client, endpoint string, accessToken string, jobID string, exitStatus int, reason string) error {
	if endpoint == "" {
		return fmt.Errorf("agent endpoint is blank (is BUILDKITE_AGENT_ENDPOINT set?)")
	}
	if accessToken == "" {
		return fmt.Errorf("agent access token is blank (is BUILDKITE_AGENT_ACCESS_TOKEN set?)")
	}
	if jobID == "" {
		return fmt.Errorf("job ID is blank (is BUILDKITE_JOB_ID set?)")
	}

	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	url := fmt.Sprintf("%s/jobs/%s/promise_failure", endpoint, jobID)

	body, err := json.Marshal(promiseFailureRequest{ExitStatus: exitStatus, Reason: reason})
	if err != nil {
		return fmt.Errorf("encoding promise_failure body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("building promise_failure request: %w", err)
	}
	req.Header.Set("Authorization", "Token "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", fmt.Sprintf(
		"Buildkite Test Engine Client/%s (%s/%s)",
		version.Version, runtime.GOOS, runtime.GOARCH,
	))

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("sending promise_failure request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("promise_failure returned HTTP %d", resp.StatusCode)
	}

	return nil
}
