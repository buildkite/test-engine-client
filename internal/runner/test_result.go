package runner

import "github.com/buildkite/test-engine-client/internal/plan"

type TestStatus string

const (
	TestStatusPassed  TestStatus = "passed"
	TestStatusFailed  TestStatus = "failed"
	TestStatusSkipped TestStatus = "skipped"
)

// TestResult is a struct to keep track the result of an individual test case.
type TestResult struct {
	plan.TestCase
	Status         TestStatus
	ExecutionCount int
	Muted          bool
}

// testIdentifier returns a unique identifier for a test case based on its scope, name and path.
// Different tests can have the same name and scope, therefore the path is included in the identifier
// to make it unique.
func testIdentifier(testCase plan.TestCase) string {
	return testCase.Scope + "/" + testCase.Name + "/" + testCase.Path
}

// mutedTestIdentifier returns a unique identifier for a muted test case based on its scope and name.
// Test Engine server identify a unique tests by its scope and name only, therefore we need follow the same logic
// to match a local test with the list of muted tests received from the server.
func mutedTestIdentifier(testCase plan.TestCase) string {
	return testCase.Scope + "/" + testCase.Name
}
