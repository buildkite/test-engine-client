package runner

type RunStatus string

const (
	RunStatusPassed RunStatus = "passed"
	RunStatusFailed RunStatus = "failed"
	RunStatusError  RunStatus = "error"
)

type RunResult struct {
	Status      RunStatus
	FailedTests []string
}
