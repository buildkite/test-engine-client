package config

import "github.com/buildkite/test-engine-client/internal/env"

// Config is the internal representation of the complete test engine client configuration.
type Config struct {
	// AccessToken is the access token for the API.
	AccessToken string
	// Identifier is the identifier of the build.
	Identifier string
	// MaxRetries is the maximum number of retries for a failed test.
	MaxRetries int
	// RetryCommand is the command to run the retry tests.
	RetryCommand string
	// Node index is index of the current node.
	NodeIndex int
	// OrganizationSlug is the slug of the organization.
	OrganizationSlug string
	// Parallelism is the number of parallel tasks to run.
	Parallelism int
	// The path to the result file.
	ResultPath string
	// ServerBaseUrl is the base URL of the test plan server.
	ServerBaseUrl string
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
	// JobRetryCount is the count of the number of times the job has been retried.
	JobRetryCount int
	// Env provides access to environment variables.
	// It's public because many tests in other packages reference it (perhaps they should not).
	Env env.Env
	// errs is a map of environment variables name and the validation errors associated with them.
	errs InvalidConfigError
}

// New wraps the readFromEnv and validate functions to create a new Config struct.
// It returns Config struct and an InvalidConfigError if there is an invalid configuration.
func New(env env.Env) (Config, error) {
	c := Config{
		Env:  env,
		errs: InvalidConfigError{},
	}

	// TODO: remove error from readFromEnv and validate functions
	_ = c.readFromEnv(env)
	_ = c.validate()

	if len(c.errs) > 0 {
		return Config{}, c.errs
	}

	return c, nil
}
