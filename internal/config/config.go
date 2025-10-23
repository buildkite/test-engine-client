package config

// Config is the internal representation of the complete test engine client configuration.
type Config struct {
	BuildId string
	JobId   string
	StepId  string
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
	// Maximum parallelism when calculating parallelism dynamically.
	MaxParallelism int
	// The path to the result file.
	ResultPath string
	// Whether a failed muted test should be retried.
	// This is default to true because we want more signal for our flaky detection system.
	RetryForMutedTest bool
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
	// Enable debug output
	DebugEnabled bool
	// errs is a map of environment variables name and the validation errors associated with them.
	errs InvalidConfigError
}

func NewEmpty() Config {
	return Config{errs: InvalidConfigError{}}
}

// New wraps the readFromEnv and validate functions to create a new Config struct.
// It returns Config struct and an InvalidConfigError if there is an invalid configuration.
func New(env map[string]string) (Config, error) {
	c := Config{
		errs: InvalidConfigError{},
	}

	c.readFromEnv(env)
	_ = c.Validate()

	if len(c.errs) > 0 {
		return Config{}, c.errs
	}

	return c, nil
}
