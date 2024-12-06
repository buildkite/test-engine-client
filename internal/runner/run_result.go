package runner

import (
	"github.com/buildkite/test-engine-client/internal/plan"
)

type RunStatus string

const (
	// RunStatusPassed indicates that the run was completed and all tests passed.
	RunStatusPassed RunStatus = "passed"
	// RunStatusFailed indicates that the run was completed, but one or more tests failed.
	RunStatusFailed RunStatus = "failed"
	// RunStatusError indicates that the run was completed, but there was an error outside of the tests.
	RunStatusError RunStatus = "error"
	// RunStatusUnknown indicates that the run status is unknown.
	// It could be that no tests were run, run was interrupted, or the report is not available.
	RunStatusUnknown RunStatus = "unknown"
)

// RunResult is a struct to keep track the results of a test run.
// It contains the logics to record test results, calculate the status of the run.
type RunResult struct {
	// tests is a map containing individual test results.
	tests map[string]*TestResult
	// mutedTestLookup is a map containing the test identifiers of muted tests.
	// This list might contain tests that are not part of the current run (i.e. belong to a different node).
	mutedTestLookup map[string]bool
	error           error
}

func NewRunResult(mutedTests []plan.TestCase) *RunResult {
	r := &RunResult{
		tests:           make(map[string]*TestResult),
		mutedTestLookup: make(map[string]bool),
	}

	for _, testCase := range mutedTests {
		identifier := testIdentifier(testCase)
		r.mutedTestLookup[identifier] = true
	}
	return r
}

// getTest finds or creates a TestResult struct for a given test case
// in the tests map, and returns a pointer to it.
func (r *RunResult) getTest(testCase plan.TestCase) *TestResult {
	if r.tests == nil {
		r.tests = make(map[string]*TestResult)
	}

	testIdentifier := testIdentifier(testCase)

	test, exists := r.tests[testIdentifier]
	if !exists {
		test = &TestResult{
			TestCase: testCase,
		}
		r.tests[testIdentifier] = test
	}
	return test
}

// RecordTestResult records the result of a test case.
// If the test case found in the mutedTestLookup, it will be marked as muted.
func (r *RunResult) RecordTestResult(testCase plan.TestCase, status TestStatus) {
	test := r.getTest(testCase)
	test.Status = status
	test.ExecutionCount++
	if r.mutedTestLookup[testIdentifier(testCase)] {
		test.Muted = true
	}
}

func (r *RunResult) RecordSkipTest(testCase plan.TestCase, method SkipMethod) {
	r.RecordTestResult(testCase, TestStatusSkipped)
	test := r.getTest(testCase)
	test.SkipMethod = method
}

// FailedTests returns a list of test cases that failed.
func (r *RunResult) FailedTests() []plan.TestCase {
	var failedTests []plan.TestCase

	for _, test := range r.tests {
		if test.Status == TestStatusFailed && !test.Muted {
			failedTests = append(failedTests, test.TestCase)
		}
	}

	return failedTests
}

func (r *RunResult) MutedTests() []TestResult {
	var mutedTests []TestResult
	for _, test := range r.tests {
		if test.Muted {
			mutedTests = append(mutedTests, *test)
		}
	}

	return mutedTests
}

type SkippedTests struct {
	TestRunner []plan.TestCase
	TestEngine []plan.TestCase
}

func (r *RunResult) SkippedTests() SkippedTests {
	var testRunners []plan.TestCase
	var testEngines []plan.TestCase

	for _, test := range r.tests {
		if test.Status == TestStatusSkipped {
			if test.SkipMethod == SkipMethodRunner {
				testRunners = append(testRunners, test.TestCase)
			} else if test.SkipMethod == SkipMethodTestEngine {
				testEngines = append(testEngines, test.TestCase)
			}
		}
	}
	return SkippedTests{TestRunner: testRunners, TestEngine: testEngines}
}

// Status returns the overall status of the test run.
// If there is an error, it returns RunStatusError.
// If there are failed tests, it returns RunStatusFailed.
// Otherwise, it returns RunStatusPassed.
func (r *RunResult) Status() RunStatus {
	if r.error != nil {
		return RunStatusError
	}

	if len(r.tests) == 0 {
		return RunStatusUnknown
	}

	if len(r.FailedTests()) > 0 {
		return RunStatusFailed
	}

	return RunStatusPassed
}

func (r *RunResult) Error() error {
	return r.error
}

type RunStatistics struct {
	Total            int
	PassedOnFirstRun int
	PassedOnRetry    int
	MutedPassed      int
	MutedFailed      int
	Failed           int
	SkippedByTestRunner int
	SkippedByTestEngine int
}

func (r *RunResult) Statistics() RunStatistics {
	stat := &RunStatistics{}

	for _, testResult := range r.tests {
		switch {
		case testResult.Muted:
			switch testResult.Status {
			case TestStatusPassed:
				stat.MutedPassed++
			case TestStatusFailed:
				stat.MutedFailed++
			}

		case testResult.Status == TestStatusPassed:
			if testResult.ExecutionCount > 1 {
				stat.PassedOnRetry++
			} else {
				stat.PassedOnFirstRun++
			}

		case testResult.Status == TestStatusFailed:
			stat.Failed++
		case testResult.Status == TestStatusSkipped:
			switch testResult.SkipMethod {
			case SkipMethodRunner:
				stat.SkippedByTestRunner++
			case SkipMethodTestEngine:
				stat.SkippedByTestEngine++
			}
		}
	}

	stat.Total = len(r.tests)

	return *stat
}
