package runner

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
}

func (rc RunnerConfig) LocationPrefix() string {
	return rc.locationPrefix
}

func (rc RunnerConfig) UploadToken() string {
	return rc.uploadToken
}
