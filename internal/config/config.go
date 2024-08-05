package config

import "time"

// Config is the internal representation of the complete test splitter client configuration.
type Config struct {
	// AccessToken is the access token for the API.
	AccessToken string
	// Identifier is the identifier of the build.
	Identifier string
	// MaxRetries is the maximum number of retries for a failed test.
	MaxRetries int
	// RetryCommand is the command to run the retry tests.
	RetryCommand string
	// Mode is the mode of the test splitter.
	Mode string
	// Node index is index of the current node.
	NodeIndex int
	// OrganizationSlug is the slug of the organization.
	OrganizationSlug string
	// Parallelism is the number of parallel tasks to run.
	Parallelism int
	// ServerBaseUrl is the base URL of the test splitter server.
	ServerBaseUrl string
	// SlowFileThreshold is the threshold to consider a file as slow. Value is a number in milliseconds.
	SlowFileThreshold time.Duration
	// SplitByExample is the flag to enable split the test by example.
	SplitByExample bool
	// SuiteSlug is the slug of the suite.
	SuiteSlug string
	// TestCommand is the command to run the tests.
	TestCommand string
	// TestFilePattern is the pattern to match the test files.
	TestFilePattern string
	// TestFileExcludePattern is the pattern to exclude the test files.
	TestFileExcludePattern string
	// TestRunner is the name of the runner.
	TestRunner string
	// Branch is the string value of the git branch name, used by Buildkite only.
	Branch string
}

// New wraps the readFromEnv and validate functions to create a new Config struct.
// It returns Config struct and an InvalidConfigError if there is an invalid configuration.
func New() (Config, error) {
	var errs InvalidConfigError

	c := Config{}

	if err := c.readFromEnv(); err != nil {
		errs = append(errs, err.(InvalidConfigError)...)
	}

	if err := c.validate(); err != nil {
		errs = append(errs, err.(InvalidConfigError)...)
	}

	if len(errs) > 0 {
		return Config{}, errs
	}

	return c, nil
}
