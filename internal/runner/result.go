package runner

import "github.com/buildkite/test-engine-client/internal/plan"

type RunStatus string

const (
	RunStatusPassed RunStatus = "passed"
	RunStatusFailed RunStatus = "failed"
	RunStatusError  RunStatus = "error"
)

type RunResult struct {
	Status      RunStatus
	FailedTests []plan.TestCase
}
