package config

import "time"

// Config is the internal representation of the complete test engine client configuration.
type Config struct {
	// AccessToken is the access token for the API.
	AccessToken string `json:"-"`
	// Branch is the string value of the git branch name, used by Buildkite only.
	Branch  string `json:"BUILDKITE_BRANCH"`
	BuildId string `json:"BUILDKITE_BUILD_ID"`
	// Enable debug output
	DebugEnabled bool `json:"BUILDKITE_TEST_ENGINE_DEBUG_ENABLED"`
	// FailOnNoTests causes the client to exit with an error if no tests are assigned to the node
	FailOnNoTests bool `json:"BUILDKITE_TEST_ENGINE_FAIL_ON_NO_TESTS"`
	// Identifier is the identifier of the build.
	Identifier string `json:"BUILDKITE_TEST_ENGINE_IDENTIFIER"`
	JobId      string `json:"BUILDKITE_JOB_ID"`
	// JobRetryCount is the count of the number of times the job has been retried.
	JobRetryCount int `json:"BUILDKITE_RETRY_COUNT"`
	// LocationPrefix is prepended to test file paths when requesting a test plan.
	// Use this when the test collector is configured to report files with a path prefix,
	// so the test plan API can correctly match and bin-pack them across nodes.
	LocationPrefix string `json:"BUILDKITE_TEST_ENGINE_LOCATION_PREFIX"`
	// MaxParallelism is the maximum parallelism when calculating parallelism dynamically.
	MaxParallelism int `json:"BUILDKITE_TEST_ENGINE_MAX_PARALLELISM"`
	// MaxRetries is the maximum number of retries for a failed test.
	MaxRetries int `json:"BUILDKITE_TEST_ENGINE_RETRY_COUNT"`
	// Metadata is additional key/value data sent to the test plan API.
	Metadata map[string]string `json:"-"`
	// Node index is index of the current node.
	NodeIndex int `json:"BUILDKITE_PARALLEL_JOB"`
	// OrganizationSlug is the slug of the organization.
	OrganizationSlug string `json:"BUILDKITE_ORGANIZATION_SLUG"`
	// Parallelism is the number of parallel tasks to run.
	Parallelism int `json:"BUILDKITE_PARALLEL_JOB_COUNT"`
	// ResultPath is the path to the result file.
	ResultPath string `json:"-"`
	// RetryCommand is the command to run the retry tests.
	RetryCommand string `json:"BUILDKITE_TEST_ENGINE_RETRY_CMD"`
	// RetryForMutedTest indicates whether a failed muted test should be retried.
	// This is default to true because we want more signal for our flaky detection system.
	RetryForMutedTest bool `json:"-"`
	// SelectionParams are additional key/value parameters for the strategy.
	SelectionParams map[string]string `json:"-"`
	// SelectionStrategy is the selection strategy sent to the test plan API.
	SelectionStrategy string `json:"BUILDKITE_TEST_ENGINE_SELECTION_STRATEGY"`
	// ServerBaseUrl is the base URL of the test plan server.
	ServerBaseUrl string `json:"-"`
	// SplitByExample is the flag to enable split the test by example.
	SplitByExample bool   `json:"BUILDKITE_TEST_ENGINE_SPLIT_BY_EXAMPLE"`
	StepId         string `json:"BUILDKITE_STEP_ID"`
	// SuiteSlug is the slug of the suite.
	SuiteSlug string `json:"BUILDKITE_TEST_ENGINE_SUITE_SLUG"`
	// TagFilters filters test examples by execution tags.
	TagFilters string `json:"BUILDKITE_TEST_ENGINE_TAG_FILTERS"`
	// TargetTime is the target time in seconds for the test plan.
	TargetTime time.Duration `json:"BUILDKITE_TEST_ENGINE_TARGET_TIME"`
	// TestCommand is the command to run the tests.
	TestCommand string `json:"BUILDKITE_TEST_ENGINE_TEST_CMD"`
	// TestFileExcludePattern is the pattern to exclude the test files.
	TestFileExcludePattern string `json:"BUILDKITE_TEST_ENGINE_TEST_FILE_EXCLUDE_PATTERN"`
	// TestFilePattern is the pattern to match the test files.
	TestFilePattern string `json:"BUILDKITE_TEST_ENGINE_TEST_FILE_PATTERN"`
	// TestRunner is the name of the runner.
	TestRunner string `json:"BUILDKITE_TEST_ENGINE_TEST_RUNNER"`
	// errs is a map of environment variables name and the validation errors associated with them.
	errs InvalidConfigError
}

func New() Config {
	return Config{errs: InvalidConfigError{}}
}
