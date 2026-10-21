package runner

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"

	"github.com/buildkite/test-engine-client/v3/internal/plan"
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
	// error is an error outside the tests, such as a failed build, that
	// retrying failed tests can't fix.
	error error
	// erroredFiles lists test files that failed to run, such as files that
	// didn't load. They error the run, but the other files' failed tests can
	// still be retried.
	erroredFiles []string
}

func NewRunResult(mutedTests []plan.TestCase) *RunResult {
	r := &RunResult{
		tests:           make(map[string]*TestResult),
		mutedTestLookup: make(map[string]bool),
	}

	for _, testCase := range mutedTests {
		identifier := mutedTestIdentifier(testCase)
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
	if r.mutedTestLookup[mutedTestIdentifier(testCase)] {
		test.Muted = true
	}
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

func (r *RunResult) SkippedTests() []plan.TestCase {
	var skippedTests []plan.TestCase

	for _, test := range r.tests {
		if test.Status == TestStatusSkipped {
			skippedTests = append(skippedTests, test.TestCase)
		}
	}
	return skippedTests
}

// NotRunTests returns tests that were expected to run but did not.
// They are not retry candidates because they may depend on earlier tests.
func (r *RunResult) NotRunTests() []plan.TestCase {
	var tests []plan.TestCase
	for _, test := range r.tests {
		if test.Status == TestStatusNotRun {
			tests = append(tests, test.TestCase)
		}
	}
	return tests
}

func (r *RunResult) FailedMutedTests() []plan.TestCase {
	var failedTests []plan.TestCase

	for _, test := range r.tests {
		if test.Status == TestStatusFailed && test.Muted {
			failedTests = append(failedTests, test.TestCase)
		}
	}
	return failedTests
}

func (r *RunResult) passedTestsCount() int {
	count := 0
	for _, test := range r.tests {
		if test.Status == TestStatusPassed {
			count++
		}
	}
	return count
}

func (r *RunResult) skippedTestsCount() int {
	count := 0
	for _, test := range r.tests {
		if test.Status == TestStatusSkipped {
			count++
		}
	}
	return count
}

// OnlyMutedFailures reports whether muted test failures are the only reason
// the command failed. Errors and tests that did not run cannot be ignored.
func (r *RunResult) OnlyMutedFailures() bool {
	if r.Status() == RunStatusError || r.Status() == RunStatusUnknown || len(r.NotRunTests()) > 0 {
		return false
	}

	failed, mutedFailed := 0, 0

	for _, test := range r.tests {
		if test.Status == TestStatusFailed {
			failed++
			if test.Muted {
				mutedFailed++
			}
		}
	}

	return failed > 0 && failed == mutedFailed
}

// Status returns the overall status of the test run.
// If there is an error, it returns RunStatusError.
// If there are failed tests or tests that did not run, it returns RunStatusFailed.
// Otherwise, it returns RunStatusPassed.
func (r *RunResult) Status() RunStatus {
	if r.error != nil || len(r.erroredFiles) > 0 {
		return RunStatusError
	}

	if len(r.tests) == 0 {
		return RunStatusUnknown
	}

	if len(r.FailedTests()) > 0 || len(r.NotRunTests()) > 0 {
		return RunStatusFailed
	}

	if r.passedTestsCount()+r.skippedTestsCount()+len(r.FailedMutedTests()) == len(r.tests) {
		return RunStatusPassed
	}

	return RunStatusUnknown
}

func (r *RunResult) Error() error {
	switch {
	case r.error != nil:
		return r.error
	case len(r.erroredFiles) == 1:
		return fmt.Errorf("1 test file failed to run")
	case len(r.erroredFiles) > 1:
		return fmt.Errorf("%d test files failed to run", len(r.erroredFiles))
	}
	return nil
}

// ErroredFiles returns the test files that failed to run.
func (r *RunResult) ErroredFiles() []string {
	return r.erroredFiles
}

// Retryable reports whether failed tests can be retried despite the run's
// status: only test files, not the whole run, have errored.
func (r *RunResult) Retryable() bool {
	return r.error == nil
}

func (r *RunResult) recordErroredFile(path string) {
	if !slices.Contains(r.erroredFiles, path) {
		r.erroredFiles = append(r.erroredFiles, path)
	}
}

type RunStatistics struct {
	Total            int `json:"total"`
	PassedOnFirstRun int `json:"passed_on_first_run"`
	PassedOnRetry    int `json:"passed_on_retry"`
	MutedPassed      int `json:"muted_passed"`
	MutedFailed      int `json:"muted_failed"`
	Failed           int `json:"failed"`
	Skipped          int `json:"skipped"`
	NotRun           int `json:"not_run,omitempty"`
}

func (r *RunResult) Statistics() RunStatistics {
	stat := &RunStatistics{}

	for _, testResult := range r.tests {
		switch {
		case testResult.Status == TestStatusNotRun:
			stat.NotRun++
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
			stat.Skipped++
		}
	}

	stat.Total = len(r.tests)

	return *stat
}

// TestEngineTest represents a Test Engine test result object.
// Some attributes such as `history` and `failure_reason` are omitted as they are not needed by bktec.
// Ref: https://buildkite.com/docs/test-engine/importing-json#json-test-results-data-reference-test-result-objects
//
// Currently, only pytest and custom runner uses result from test collector.
type TestEngineTest struct {
	ID       string
	Name     string
	Scope    string
	Location string
	FileName string `json:"file_name,omitempty"`
	Result   TestStatus
	Tags     map[string]string `json:"tags,omitempty"`
}

func parseTestEngineTestResult(path string) ([]TestEngineTest, error) {
	var results []TestEngineTest
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read json: %v", err)
	}

	if err := json.Unmarshal(data, &results); err != nil {
		return nil, fmt.Errorf("failed to parse json: %v", err)
	}

	return results, nil
}
