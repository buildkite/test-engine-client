package command

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/buildkite/test-engine-client/v2/internal/config"
	"github.com/buildkite/test-engine-client/v2/internal/plan"
	"github.com/buildkite/test-engine-client/v2/internal/runner"
)

func testCase(path string) plan.TestCase {
	// Set Name as well as Path so muted-test matching (which keys on Scope+Name)
	// can target individual cases precisely.
	return plan.TestCase{Name: path, Path: path}
}

// resultWith builds a RunResult by recording the given statuses. Any test case
// passed in muted is registered as muted, so a failure on it is a muted failure.
func resultWith(muted []plan.TestCase, record func(r *runner.RunResult)) runner.RunResult {
	r := runner.NewRunResult(muted)
	record(r)
	return *r
}

func TestPromiseFailureIfNeeded(t *testing.T) {
	hardFailures := resultWith(nil, func(r *runner.RunResult) {
		r.RecordTestResult(testCase("a_spec.rb"), runner.TestStatusPassed)
		r.RecordTestResult(testCase("b_spec.rb"), runner.TestStatusFailed)
		r.RecordTestResult(testCase("c_spec.rb"), runner.TestStatusFailed)
	})

	allPassed := resultWith(nil, func(r *runner.RunResult) {
		r.RecordTestResult(testCase("a_spec.rb"), runner.TestStatusPassed)
	})

	mutedOnlyFailures := resultWith([]plan.TestCase{testCase("b_spec.rb")}, func(r *runner.RunResult) {
		r.RecordTestResult(testCase("a_spec.rb"), runner.TestStatusPassed)
		r.RecordTestResult(testCase("b_spec.rb"), runner.TestStatusFailed) // muted -> not a hard failure
	})

	tests := []struct {
		name            string
		promiseEnabled  bool
		result          runner.RunResult
		serverStatus    int
		wantRequest     bool
		wantExitStatus  float64
		wantReasonStart string
	}{
		{
			name:            "hard failures with flag on promises",
			promiseEnabled:  true,
			result:          hardFailures,
			wantRequest:     true,
			wantExitStatus:  1,
			wantReasonStart: "test_failure",
		},
		{
			name:            "agent error is swallowed (best-effort)",
			promiseEnabled:  true,
			result:          hardFailures,
			serverStatus:    http.StatusInternalServerError,
			wantRequest:     true,
			wantExitStatus:  1,
			wantReasonStart: "test_failure",
		},
		{
			name:           "hard failures with flag off does nothing",
			promiseEnabled: false,
			result:         hardFailures,
			wantRequest:    false,
		},
		{
			name:           "no failures does nothing",
			promiseEnabled: true,
			result:         allPassed,
			wantRequest:    false,
		},
		{
			name:           "muted-only failures does nothing",
			promiseEnabled: true,
			result:         mutedOnlyFailures,
			wantRequest:    false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var hits int32
			var gotBody map[string]any

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				atomic.AddInt32(&hits, 1)
				raw, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(raw, &gotBody)
				status := tc.serverStatus
				if status == 0 {
					status = http.StatusOK
				}
				w.WriteHeader(status)
			}))
			defer server.Close()

			cfg := &config.Config{
				PromiseFailure:   tc.promiseEnabled,
				AgentEndpoint:    server.URL,
				AgentAccessToken: "test-token",
				JobID:            "job-uuid",
			}

			promiseFailureIfNeeded(t.Context(), cfg, tc.result)

			gotRequest := atomic.LoadInt32(&hits) > 0
			if gotRequest != tc.wantRequest {
				t.Fatalf("request made = %v, want %v", gotRequest, tc.wantRequest)
			}

			if !tc.wantRequest {
				return
			}

			if got := gotBody["exit_status"]; got != tc.wantExitStatus {
				t.Errorf("exit_status = %v, want %v", got, tc.wantExitStatus)
			}
			reason, _ := gotBody["reason"].(string)
			if !strings.HasPrefix(reason, tc.wantReasonStart) {
				t.Errorf("reason = %q, want prefix %q", reason, tc.wantReasonStart)
			}
		})
	}
}
