package runner

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/buildkite/test-engine-client/internal/plan"
	"github.com/google/go-cmp/cmp"
)

func TestRecordTestResult(t *testing.T) {
	r := NewRunResult([]plan.TestCase{})
	testCase := plan.TestCase{Scope: "apple", Name: "is red"}
	identifier := testIdentifier(testCase)
	r.RecordTestResult(testCase, TestStatusPassed)

	if r.Status() != RunStatusPassed {
		t.Errorf("Status() is %s, want %s", r.Status(), RunStatusPassed)
	}

	if r.tests[identifier].Status != TestStatusPassed {
		t.Errorf("%q status is %s, want %s", "apple/is red", r.tests[identifier].Status, TestStatusPassed)
	}
}

func TestRecordTestResult_MultipleExecution(t *testing.T) {
	r := NewRunResult([]plan.TestCase{})

	testCase := plan.TestCase{Scope: "apple", Name: "is red"}
	identifier := testIdentifier(testCase)
	r.RecordTestResult(testCase, TestStatusFailed)
	r.RecordTestResult(testCase, TestStatusPassed)

	if r.Status() != RunStatusPassed {
		t.Errorf("Status() is %s, want %s", r.Status(), RunStatusPassed)
	}

	// It increments the execution count
	if r.tests[identifier].ExecutionCount != 2 {
		t.Errorf("%q execution count is %d, want %d", "apple/is red", r.tests[identifier].ExecutionCount, 2)
	}

	// It set the last execution status
	if r.tests[identifier].Status != TestStatusPassed {
		t.Errorf("%q status is %s, want %s", "apple/is red", r.tests[identifier].Status, TestStatusPassed)
	}
}

func TestFailedTests(t *testing.T) {
	r := NewRunResult([]plan.TestCase{})

	apple1 := plan.TestCase{Scope: "apple", Name: "is red"}
	apple2 := plan.TestCase{Scope: "apple", Name: "is green"}
	banana := plan.TestCase{Scope: "banana", Name: "is yellow"}
	r.RecordTestResult(apple1, TestStatusFailed)
	r.RecordTestResult(apple2, TestStatusPassed)
	r.RecordTestResult(banana, TestStatusFailed)

	failedTests := r.FailedTests()

	if r.Status() != RunStatusFailed {
		t.Errorf("Status() is %s, want %s", r.Status(), RunStatusFailed)
	}

	if len(failedTests) != 2 {
		t.Errorf("failed tests count is %d, want %d", len(failedTests), 2)
	}

	wantFailedTests := []plan.TestCase{apple1, banana}

	// Sort the failed tests by scope and name when comparing
	sorter := cmp.Transformer("Sort", func(in []plan.TestCase) []plan.TestCase {
		out := append([]plan.TestCase(nil), in...) // Copy input to avoid mutating it
		slices.SortFunc(out, func(a, b plan.TestCase) int {
			return strings.Compare(a.Scope+"/"+a.Name, b.Scope+"/"+b.Name)
		})
		return out
	})

	if diff := cmp.Diff(failedTests, wantFailedTests, sorter); diff != "" {
		t.Errorf("FailedTests() diff (-got +want):\n%s", diff)
	}
}

func TestFailedTests_TestPassAfterRetry(t *testing.T) {
	r := NewRunResult([]plan.TestCase{})

	apple1 := plan.TestCase{Scope: "apple", Name: "is red"}
	apple2 := plan.TestCase{Scope: "apple", Name: "is green"}
	banana := plan.TestCase{Scope: "banana", Name: "is yellow"}
	r.RecordTestResult(apple1, TestStatusFailed)
	r.RecordTestResult(apple2, TestStatusPassed)
	r.RecordTestResult(banana, TestStatusFailed)
	r.RecordTestResult(banana, TestStatusPassed)

	failedTests := r.FailedTests()
	if r.Status() != RunStatusFailed {
		t.Errorf("Status() is %s, want %s", r.Status(), RunStatusFailed)
	}

	if len(failedTests) != 1 {
		t.Errorf("failed tests count is %d, want %d", len(failedTests), 1)
	}

	wantFailedTests := []plan.TestCase{
		{Scope: "apple", Name: "is red"},
	}

	if diff := cmp.Diff(failedTests, wantFailedTests); diff != "" {
		t.Errorf("FailedTests() diff (-got +want):\n%s", diff)
	}
}

func TestFailedTests_ExcludeMutedTests(t *testing.T) {
	apple1 := plan.TestCase{Scope: "apple", Name: "is red"}
	apple2 := plan.TestCase{Scope: "apple", Name: "is green"}
	banana := plan.TestCase{Scope: "banana", Name: "is yellow"}

	r := NewRunResult([]plan.TestCase{
		apple1,
	})
	r.RecordTestResult(apple1, TestStatusFailed) // Muted so no impact
	r.RecordTestResult(apple1, TestStatusFailed) // Retry fails, but muted so no impact
	r.RecordTestResult(apple2, TestStatusPassed)

	// At this point the run status is "passsed"
	if r.Status() != RunStatusPassed {
		t.Errorf("Status() is %s, want %s", r.Status(), RunStatusPassed)
	}

	// Now it enters "failed" status
	r.RecordTestResult(banana, TestStatusFailed)

	if r.Status() != RunStatusFailed {
		t.Errorf("Status() is %s, want %s", r.Status(), RunStatusFailed)
	}

	// Asserting the failed tests don't include muted test
	failedTests := r.FailedTests()

	if len(failedTests) != 1 {
		t.Errorf("failed tests count is %d, want %d", len(failedTests), 1)
	}

	wantFailedTests := []plan.TestCase{banana}

	if diff := cmp.Diff(failedTests, wantFailedTests); diff != "" {
		t.Errorf("FailedTests() diff (-got +want):\n%s", diff)
	}
}

func TestMutedTests(t *testing.T) {

	apple := plan.TestCase{Scope: "apple", Name: "is red"}
	banana := plan.TestCase{Scope: "banana", Name: "is yellow"}

	r := NewRunResult([]plan.TestCase{
		apple,
	})

	r.RecordTestResult(apple, TestStatusFailed)
	r.RecordTestResult(banana, TestStatusPassed)

	mutedTests := r.MutedTests()

	if len(mutedTests) != 1 {
		t.Errorf("failed tests count is %d, want %d", len(mutedTests), 1)
	}

	wantMutedTest := []TestResult{
		{TestCase: apple, Status: TestStatusFailed, ExecutionCount: 1, Muted: true},
	}

	if diff := cmp.Diff(mutedTests, wantMutedTest); diff != "" {
		t.Errorf("FailedTests() diff (-got +want):\n%s", diff)
	}
}

func TestRunStatistics(t *testing.T) {
	r := NewRunResult([]plan.TestCase{
		// muted tests
		{Scope: "mango", Name: "is sweet", Path: "mango.rb:1"},
		{Scope: "mango", Name: "is sour"},
		{Scope: "cat", Name: "is not a fruit"}, // unrecorded (not related to this run) test case should be ignored
	})

	// passed on first run: 3
	r.RecordTestResult(plan.TestCase{Scope: "apple", Name: "is red"}, TestStatusPassed)
	r.RecordTestResult(plan.TestCase{Scope: "mango", Name: "is red", Path: "mango.rb:3"}, TestStatusPassed)
	r.RecordTestResult(plan.TestCase{Scope: "mango", Name: "is red", Path: "mango.rb:7"}, TestStatusPassed) // Different tests with the same name and scope should be counted separately

	//passed on retry: 1
	r.RecordTestResult(plan.TestCase{Scope: "apple", Name: "is green"}, TestStatusFailed)
	r.RecordTestResult(plan.TestCase{Scope: "apple", Name: "is green"}, TestStatusPassed)

	// muted: 1 failed, 1 passed
	r.RecordTestResult(plan.TestCase{Scope: "mango", Name: "is sweet", Path: "mango.rb:5"}, TestStatusPassed) // This test matched with a test in the muted tests lists even though the path is different because we only compare the scope and name for muted tests
	r.RecordTestResult(plan.TestCase{Scope: "mango", Name: "is sour"}, TestStatusFailed)

	// failed: 1
	r.RecordTestResult(plan.TestCase{Scope: "banana", Name: "is yellow"}, TestStatusFailed)
	r.RecordTestResult(plan.TestCase{Scope: "banana", Name: "is yellow"}, TestStatusFailed) // test failed twice

	// skipped: 1
	r.RecordTestResult(plan.TestCase{Scope: "orange", Name: "is orange"}, TestStatusSkipped)

	stats := r.Statistics()

	if diff := cmp.Diff(stats, RunStatistics{
		Total:            8,
		PassedOnFirstRun: 3,
		PassedOnRetry:    1,
		MutedPassed:      1,
		MutedFailed:      1,
		Failed:           1,
		Skipped:          1,
	}); diff != "" {
		t.Errorf("Statistics() diff (-got +want):\n%s", diff)
	}
}

func TestRunStatus_Passed(t *testing.T) {
	r := NewRunResult([]plan.TestCase{
		{Scope: "watermelon", Name: "is juicy"},
	})
	r.RecordTestResult(plan.TestCase{Scope: "mango", Name: "is sour"}, TestStatusPassed)
	// skipped test should not affect the run status
	r.RecordTestResult(plan.TestCase{Scope: "apple", Name: "is red"}, TestStatusSkipped)
	// muted test should not affect the run status even if it's failed
	r.RecordTestResult(plan.TestCase{Scope: "watermelon", Name: "is juicy"}, TestStatusFailed)

	if r.Status() != RunStatusPassed {
		t.Errorf("Status() is %s, want %s", r.Status(), RunStatusPassed)
	}
}

func TestRunStatus_Failed(t *testing.T) {
	r := NewRunResult([]plan.TestCase{})
	r.RecordTestResult(plan.TestCase{Scope: "mango", Name: "is sour"}, TestStatusFailed)
	r.RecordTestResult(plan.TestCase{Scope: "apple", Name: "is red"}, TestStatusPassed)
	r.RecordTestResult(plan.TestCase{Scope: "watermelon", Name: "is juicy"}, TestStatusUnknown)
	r.RecordTestResult(plan.TestCase{Scope: "banana", Name: "is yellow"}, TestStatusSkipped)

	// doesn't matter if there are tests with other status, if there's at least one failed test the status should be "failed"
	if r.Status() != RunStatusFailed {
		t.Errorf("Status() is %s, want %s", r.Status(), RunStatusFailed)
	}
}

func TestRunStatus_Error(t *testing.T) {
	r := NewRunResult([]plan.TestCase{})
	r.error = fmt.Errorf("error")

	r.RecordTestResult(plan.TestCase{Scope: "mango", Name: "is sour"}, TestStatusFailed)
	r.RecordTestResult(plan.TestCase{Scope: "apple", Name: "is red"}, TestStatusPassed)
	r.RecordTestResult(plan.TestCase{Scope: "watermelon", Name: "is juicy"}, TestStatusUnknown)

	// doesn't matter what individual test results are, if there's an error the status should be "error"
	if r.Status() != RunStatusError {
		t.Errorf("Status() is %s, want %s", r.Status(), RunStatusError)
	}
}

func TestRunStatus_Unknown(t *testing.T) {
	r := NewRunResult([]plan.TestCase{})
	// when there are no tests, the status should be "unknown"
	if r.Status() != RunStatusUnknown {
		t.Errorf("Status() is %s, want %s", r.Status(), RunStatusUnknown)
	}

	r.RecordTestResult(plan.TestCase{Scope: "mango", Name: "is sour"}, TestStatusUnknown)
	r.RecordTestResult(plan.TestCase{Scope: "apple", Name: "is red"}, TestStatusPassed)
	// even if the rest of the tests are passed, if there's at least one test with unknown status, the overall status should be "unknown"
	if r.Status() != RunStatusUnknown {
		t.Errorf("Status() is %s, want %s", r.Status(), RunStatusUnknown)
	}
}

func TestOnlyMutedFailures(t *testing.T) {
	r := NewRunResult([]plan.TestCase{
		{Scope: "mango", Name: "is sour"},
	})

	// when there are no failed tests, it should return false
	r.RecordTestResult(plan.TestCase{Scope: "banana", Name: "is yellow"}, TestStatusPassed)
	if r.OnlyMutedFailures() != false {
		t.Errorf("OnlyMutedFailures() is %v, want %v", r.OnlyMutedFailures(), false)
	}

	// return true when all failed tests are muted
	r.RecordTestResult(plan.TestCase{Scope: "mango", Name: "is sour"}, TestStatusFailed)
	if r.OnlyMutedFailures() != true {
		t.Errorf("OnlyMutedFailures() is %v, want %v", r.OnlyMutedFailures(), true)
	}

	// even if all failed tests are muted, if there's an error it should return false
	r.error = fmt.Errorf("some error")
	if r.OnlyMutedFailures() != false {
		t.Errorf("OnlyMutedFailures() is %v, want %v", r.OnlyMutedFailures(), false)
	}

	// reset error and record a non-muted failed test, it should return false
	r.error = nil
	r.RecordTestResult(plan.TestCase{Scope: "apple", Name: "is red"}, TestStatusFailed)
	if r.OnlyMutedFailures() != false {
		t.Errorf("OnlyMutedFailures() is %v, want %v", r.OnlyMutedFailures(), false)
	}
}

func TestCollectionErrors_ExcludedFromFailedTests(t *testing.T) {
	r := NewRunResult([]plan.TestCase{})

	realFailure := plan.TestCase{Scope: "tests/math.py", Name: "test_add"}
	collectionErr := plan.TestCase{Scope: "tests/broken.py", Name: "tests/broken.py"}

	r.RecordTestResult(realFailure, TestStatusFailed)
	r.RecordCollectionError(collectionErr)

	if r.Status() != RunStatusFailed {
		t.Errorf("Status() = %s, want %s", r.Status(), RunStatusFailed)
	}

	failedTests := r.FailedTests()
	if len(failedTests) != 1 {
		t.Fatalf("len(FailedTests()) = %d, want 1", len(failedTests))
	}
	if failedTests[0].Name != "test_add" {
		t.Errorf("FailedTests()[0].Name = %q, want %q", failedTests[0].Name, "test_add")
	}
}

func TestCollectionErrors_ExcludedFromFailedMutedTests(t *testing.T) {
	collectionErr := plan.TestCase{Scope: "tests/broken.py", Name: "tests/broken.py"}

	r := NewRunResult([]plan.TestCase{collectionErr})
	r.RecordCollectionError(collectionErr)

	failedMuted := r.FailedMutedTests()
	if len(failedMuted) != 0 {
		t.Errorf("len(FailedMutedTests()) = %d, want 0", len(failedMuted))
	}
}

func TestCollectionErrors_OnlyCollectionErrorsStillFailed(t *testing.T) {
	r := NewRunResult([]plan.TestCase{})

	collectionErr := plan.TestCase{Scope: "tests/broken.py", Name: "tests/broken.py"}
	r.RecordCollectionError(collectionErr)

	if r.Status() != RunStatusFailed {
		t.Errorf("Status() = %s, want %s", r.Status(), RunStatusFailed)
	}

	// No retryable failures
	if len(r.FailedTests()) != 0 {
		t.Errorf("len(FailedTests()) = %d, want 0", len(r.FailedTests()))
	}
}

func TestParseTestEngineTestResult(t *testing.T) {
	results, err := parseTestEngineTestResult("testdata/test-engine-result.json")
	if err != nil {
		t.Errorf("parseTestEngineTestResult() error = %v", err)
	}
	if len(results) != 2 {
		t.Errorf("len(results) = %d, want 2", len(results))
	}
}

func TestParseTestEngineTestResult_WithTags(t *testing.T) {
	results, err := parseTestEngineTestResult("testdata/test-engine-result-with-tags.json")
	if err != nil {
		t.Fatalf("parseTestEngineTestResult() error = %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}

	if results[0].Tags != nil {
		t.Errorf("results[0].Tags = %v, want nil", results[0].Tags)
	}

	wantTag := "true"
	if got := results[1].Tags["test.pytest_collection_error"]; got != wantTag {
		t.Errorf("results[1].Tags[\"test.pytest_collection_error\"] = %q, want %q", got, wantTag)
	}
}
