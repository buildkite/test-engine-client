package runner

type TestStatus string

const (
	TestStatusPassed TestStatus = "passed"
	TestStatusFailed TestStatus = "failed"
	TestStatusError  TestStatus = "error"
)

type TestResult struct {
	Status      TestStatus
	FailedTests []string
}
