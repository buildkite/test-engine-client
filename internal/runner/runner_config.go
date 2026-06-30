package runner

import "fmt"

type RunnerConfig struct {
	TestRunner string

	locationPrefix string
	// ResultPath is used internally so bktec can read result from Test Runner.
	// User typically don't need to worry about setting this except in in RSpec and playwright.
	// In playwright, for example, it can only be configured via a config file, therefore it's mandatory for user to set.
	ResultPath             string
	RetryTestCommand       string
	TagFilters             string
	TestCommand            string
	TestFileExcludePattern string
	TestFilePattern        string
	uploadToken            string

	// SelectorSplitting enables selector-based splitting for this runner.
	SelectorSplitting bool
	// SelectorListPath points at a file containing the selectors to run.
	SelectorListPath string
}

// splitBySelectorList reports whether the runner is splitting work using a
// user-provided selector list instead of discovered test files. In this mode
// test file discovery is skipped, so TestFilePattern isn't required.
func (rc RunnerConfig) splitBySelectorList() bool {
	return rc.SelectorSplitting && rc.SelectorListPath != ""
}

func (rc RunnerConfig) LocationPrefix() string {
	return rc.locationPrefix
}

func (rc RunnerConfig) UploadToken() string {
	return rc.uploadToken
}

// ResultFormat returns an empty string by default.
// Runners that support raw result uploads should override this.
func (rc RunnerConfig) ResultFormat() string {
	return ""
}

// ResultFilePath returns the path to the runner's raw result file.
func (rc RunnerConfig) ResultFilePath() string {
	return rc.ResultPath
}

// GetSelectors returns a "not supported" error by default. Selector based
// splitting is currently only implemented by the Go runner, which overrides
// this. Every other runner inherits this default until it gains selector
// support.
func (rc RunnerConfig) GetSelectors() ([]string, error) {
	return nil, fmt.Errorf("selector based splitting is not supported for the %s runner", rc.TestRunner)
}
