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

func TestFailedTests_TetsPassAfterRetry(t *testing.T) {
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
	r.RecordTestResult(apple1, TestStatusFailed)
	r.RecordTestResult(apple2, TestStatusPassed)
	r.RecordTestResult(banana, TestStatusFailed)

	failedTests := r.FailedTests()

	if r.Status() != RunStatusFailed {
		t.Errorf("Status() is %s, want %s", r.Status(), RunStatusFailed)
	}

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
		{Scope: "mango", Name: "is sweet"},
		{Scope: "mango", Name: "is sour"},
		{Scope: "cat", Name: "is not a fruit"}, // unrecorded (not related to this run) test case should be ignored
	})

	// passed on first run: 2
	r.RecordTestResult(plan.TestCase{Scope: "apple", Name: "is red"}, TestStatusPassed)
	r.RecordTestResult(plan.TestCase{Scope: "mango", Name: "is red"}, TestStatusPassed)

	//passed on retry: 1
	r.RecordTestResult(plan.TestCase{Scope: "apple", Name: "is green"}, TestStatusFailed)
	r.RecordTestResult(plan.TestCase{Scope: "apple", Name: "is green"}, TestStatusPassed)

	// muted: 1 failed, 1 passed
	r.RecordTestResult(plan.TestCase{Scope: "mango", Name: "is sweet"}, TestStatusPassed)
	r.RecordTestResult(plan.TestCase{Scope: "mango", Name: "is sour"}, TestStatusFailed)

	// failed: 1
	r.RecordTestResult(plan.TestCase{Scope: "banana", Name: "is yellow"}, TestStatusFailed)
	r.RecordTestResult(plan.TestCase{Scope: "banana", Name: "is yellow"}, TestStatusFailed) // test failed twice

	// skipped: 1
	r.RecordTestResult(plan.TestCase{Scope: "orange", Name: "is orange"}, TestStatusSkipped)

	stats := r.Statistics()

	if diff := cmp.Diff(stats, RunStatistics{
		Total:            7,
		PassedOnFirstRun: 2,
		PassedOnRetry:    1,
		MutedPassed:      1,
		MutedFailed:      1,
		Failed:           1,
		Skipped:          1,
	}); diff != "" {
		t.Errorf("Statistics() diff (-got +want):\n%s", diff)
	}
}

func TestRunStatus(t *testing.T) {
	r := NewRunResult([]plan.TestCase{})
	r.RecordTestResult(plan.TestCase{Scope: "mango", Name: "is sour"}, TestStatusPassed)
	r.RecordTestResult(plan.TestCase{Scope: "apple", Name: "is red"}, TestStatusSkipped)
	if r.Status() != RunStatusPassed {
		t.Errorf("Status() is %s, want %s", r.Status(), RunStatusPassed)
	}
}

func TestRunStatus_Failed(t *testing.T) {
	r := NewRunResult([]plan.TestCase{})
	r.RecordTestResult(plan.TestCase{Scope: "mango", Name: "is sour"}, TestStatusFailed)
	if r.Status() != RunStatusFailed {
		t.Errorf("Status() is %s, want %s", r.Status(), RunStatusFailed)
	}
}

func TestRunStatus_Error(t *testing.T) {
	r := NewRunResult([]plan.TestCase{})
	r.error = fmt.Errorf("error")
	if r.Status() != RunStatusError {
		t.Errorf("Status() is %s, want %s", r.Status(), RunStatusError)
	}
}
