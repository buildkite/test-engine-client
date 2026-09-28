// Package runnerexec hosts the runner protocol independently of work sourcing,
// test outcomes, retries, muting, and upload/accounting policy.
package runnerexec

import (
	"encoding/json"
	"time"

	"github.com/buildkite/test-engine-client/v3/internal/plan"
)

const SocketEnv = "BUILDKITE_TEST_ENGINE_RUNNER_SOCKET"

type SessionRequest struct {
	InstanceID string `json:"instance_id"`
	Runner     struct {
		Name             string `json:"name"`
		Version          string `json:"version"`
		Framework        string `json:"framework"`
		FrameworkVersion string `json:"framework_version"`
		Language         string `json:"language"`
		LanguageVersion  string `json:"language_version"`
		PID              int    `json:"pid"`
	} `json:"runner"`
	Capabilities struct {
		SelectorFormats []plan.TestCaseFormat `json:"selector_formats"`
	} `json:"capabilities"`
}

type SessionResponse struct {
	SessionID string `json:"session_id"`
	Poll      struct {
		MaxWaitMS int `json:"max_wait_ms"`
	} `json:"poll"`
}

type Batch struct {
	ID        string          `json:"id"`
	Tests     []plan.TestCase `json:"tests"`
	TimeoutMS int64           `json:"timeout_ms"`
}

type PullResponse struct {
	Type         string `json:"type"`
	Batch        *Batch `json:"batch,omitempty"`
	RetryAfterMS int    `json:"retry_after_ms,omitempty"`
	Reason       string `json:"reason,omitempty"`
}

type Result struct {
	Status       string          `json:"status"`
	ReportFormat string          `json:"report_format"`
	Report       json.RawMessage `json:"report,omitempty"`
	Error        *RunnerError    `json:"error,omitempty"`
}

type RunnerError struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

// Source callbacks are serialized. They must return promptly and must not call
// back into the host. Next returns a batch, a terminal reason, or neither (wait).
// Batch IDs must be unique for the entire process lifetime. Accepted delivers an
// envelope once; validating native reports and choosing retries belong here, not
// in the transport. Unresolved is called only for dispatched, unaccepted work.
type Source interface {
	Next() (*Batch, string, error)
	Accepted(Batch, Result)
	Unresolved(Batch, error)
}

// DispatchSource optionally observes the dispatch boundary after host validation
// but before a batch can reach the runner. Returning an error fences dispatch.
type DispatchSource interface {
	Dispatched(Batch) error
}

// Options durations are Go durations; wire durations are integer milliseconds.
// Zero selects provisional defaults; negative durations are invalid.
type Options struct {
	StartupTimeout  time.Duration
	BatchTimeout    time.Duration
	ShutdownTimeout time.Duration
}

func validReason(reason string) bool {
	switch reason {
	case "plan_completed", "pool_consumed", "pool_errored", "terminating", "error":
		return true
	}
	return false
}
