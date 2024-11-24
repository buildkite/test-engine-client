package runner

import "github.com/buildkite/test-engine-client/internal/plan"

type TestStatus string

const (
	TestStatusPassed  TestStatus = "passed"
	TestStatusFailed  TestStatus = "failed"
	TestStatusPending TestStatus = "pending"
)

// TestResult is a struct to keep track the result of an individual test case.
type TestResult struct {
	plan.TestCase
	Status         TestStatus
	ExecutionCount int
	Muted          bool
}

func testIdentifier(testCase plan.TestCase) string {
	return testCase.Scope + "/" + testCase.Name
}
