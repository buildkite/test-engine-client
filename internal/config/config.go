package config

import (
	"fmt"

	"github.com/urfave/cli/v3"
)

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

// New wraps the readFromEnv and validate functions to create a new Config struct.
// It returns Config struct and an InvalidConfigError if there is an invalid configuration.
func New(env map[string]string) (Config, error) {
	c := Config{
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

// Create and validate a Config struct from a cli.Command.
// Returns an InvalidConfigError if there is an invalid configuration.
func NewFromCliCommand(cmd *cli.Command) (Config, error) {
	c := Config{
		errs: InvalidConfigError{},
	}

	c.AccessToken = cmd.String("access-token")
	c.OrganizationSlug = cmd.String("organization-slug")
	c.SuiteSlug = cmd.String("suite-slug")

	buildId := cmd.String("build-id")
	if buildId == "" {
		c.errs.appendFieldError("BUILDKITE_BUILD_ID", "must not be blank")
	}

	stepId := cmd.String("step-id")
	if stepId == "" {
		c.errs.appendFieldError("BUILDKITE_STEP_ID", "must not be blank")
	}

	c.Identifier = fmt.Sprintf("%s/%s", buildId, stepId)

	c.ServerBaseUrl = cmd.String("base-url")
	c.TestCommand = cmd.String("test-command")
	c.TestFilePattern = cmd.String("test-file-pattern")
	c.TestFileExcludePattern = cmd.String("test-file-exclude-pattern")
	c.TestRunner = cmd.String("test-runner")
	c.RetryForMutedTest = !cmd.Bool("disable-retry-muted")
	c.ResultPath = cmd.String("result-path")

	c.SplitByExample = cmd.Bool("split-by-example")

	c.Branch = cmd.String("branch")

	c.JobRetryCount = cmd.Int("retry-count")

	c.MaxRetries = cmd.Int("test-engine-retry-count")
	c.RetryCommand = cmd.String("retry-command")

	c.Parallelism = cmd.Int("parallelism")
	c.NodeIndex = cmd.Int("parallel-job")

	_ = c.validate()

	if len(c.errs) > 0 {
		return c, c.errs
	}
	return c, nil
}
