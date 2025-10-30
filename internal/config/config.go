package config

import "time"

// Config is the internal representation of the complete test engine client configuration.
type Config struct {
	BuildId string `json:"BUILDKITE_BUILD_ID"`
	JobId   string `json:"BUILDKITE_JOB_ID"`
	StepId  string `json:"BUILDKITE_STEP_ID"`
	// AccessToken is the access token for the API.
	AccessToken string `json:"-"`
	// Identifier is the identifier of the build.
	Identifier string `json:"BUILDKITE_TEST_ENGINE_IDENTIFIER"`
	// MaxRetries is the maximum number of retries for a failed test.
	MaxRetries int `json:"BUILDKITE_TEST_ENGINE_RETRY_COUNT"`
	// RetryCommand is the command to run the retry tests.
	RetryCommand string `json:"BUILDKITE_TEST_ENGINE_RETRY_CMD"`
	// Node index is index of the current node.
	NodeIndex int `json:"BUILDKITE_PARALLEL_JOB"`
	// OrganizationSlug is the slug of the organization.
	OrganizationSlug string `json:"BUILDKITE_ORGANIZATION_SLUG"`
	// Parallelism is the number of parallel tasks to run.
	Parallelism int `json:"BUILDKITE_PARALLEL_JOB_COUNT"`
	// Maximum parallelism when calculating parallelism dynamically.
	MaxParallelism int `json:"-"`
	// TargetTime is the target time in seconds for the test plan.
	TargetTime time.Duration `json:"-"`
	// The path to the result file.
	ResultPath string `json:"-"`
	// Whether a failed muted test should be retried.
	// This is default to true because we want more signal for our flaky detection system.
	RetryForMutedTest bool `json:"-"`
	// ServerBaseUrl is the base URL of the test plan server.
	ServerBaseUrl string `json:"-"`
	// SplitByExample is the flag to enable split the test by example.
	SplitByExample bool `json:"BUILDKITE_TEST_ENGINE_SPLIT_BY_EXAMPLE"`
	// SuiteSlug is the slug of the suite.
	SuiteSlug string `json:"BUILDKITE_TEST_ENGINE_SUITE_SLUG"`
	// TestCommand is the command to run the tests.
	TestCommand string `json:"BUILDKITE_TEST_ENGINE_TEST_CMD"`
	// TestFilePattern is the pattern to match the test files.
	TestFilePattern string `json:"BUILDKITE_TEST_ENGINE_TEST_FILE_PATTERN"`
	// TestFileExcludePattern is the pattern to exclude the test files.
	TestFileExcludePattern string `json:"BUILDKITE_TEST_ENGINE_TEST_FILE_EXCLUDE_PATTERN"`
	// TestRunner is the name of the runner.
	TestRunner string `json:"BUILDKITE_TEST_ENGINE_TEST_RUNNER"`
	// Branch is the string value of the git branch name, used by Buildkite only.
	Branch string `json:"BUILDKITE_BRANCH"`
	// JobRetryCount is the count of the number of times the job has been retried.
	JobRetryCount int `json:"BUILDKITE_RETRY_COUNT"`
	// Enable debug output
	DebugEnabled bool `json:"BUILDKITE_TEST_ENGINE_DEBUG_ENABLED"`
	// errs is a map of environment variables name and the validation errors associated with them.
	errs InvalidConfigError
}

func New() Config {
	return Config{errs: InvalidConfigError{}}
}
