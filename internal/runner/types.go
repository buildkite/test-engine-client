package runner

import "github.com/buildkite/test-engine-client/internal/plan"

type RunnerConfig struct {
	TestRunner             string
	TestCommand            string
	TestFilePattern        string
	TestFileExcludePattern string
	RetryTestCommand       string
	TagFilters             string
	// ResultPath is used internally so bktec can read result from Test Runner.
	// User typically don't need to worry about setting this except in in RSpec and playwright.
	// In playwright, for example, it can only be configured via a config file, therefore it's mandatory for user to set.
	ResultPath string
}

type TestRunner interface {
	// Run takes testCases as input, executes the test against the test cases, and mutates the runner.RunResult with the test results.
	Run(result *RunResult, testCases []plan.TestCase, retry bool) error
	// GetExamples discovers all tests within given files.
	// This function is only used for split by example use case. Currently only supported by RSpec.
	GetExamples(files []string) ([]plan.TestCase, error)
	// GetFiles discover all test files that the runner should execute.
	// This is sent to server-side when creating test plan.
	// This is also used to obtain a fallback non-intelligent test splitting mechanism.
	GetFiles() ([]string, error)
	Name() string
	SupportedFeatures() SupportedFeatures
}

type SupportedFeatures struct {
	SplitByFile     bool
	SplitByExample  bool
	FilterTestFiles bool
	AutoRetry       bool
	Mute            bool
	Skip            bool
}
