package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// readFromEnv reads the configuration from environment variables and sets it to the Config struct.
// It returns an InvalidConfigError if there is an error while reading the configuration
// such as when parallelism and node index are not numbers, and set a default
// value for ServerBaseUrl if they are not set.
//
// Currently, it reads the following environment variables:
// - BUILDKITE_ORGANIZATION_SLUG (OrganizationSlug)
// - BUILDKITE_PARALLEL_JOB_COUNT (Parallelism)
// - BUILDKITE_PARALLEL_JOB (NodeIndex)
// - BUILDKITE_SPLITTER_API_ACCESS_TOKEN (AccessToken)
// - BUILDKITE_SPLITTER_BASE_URL (ServerBaseUrl)
// - BUILDKITE_SPLITTER_RETRY_COUNT (MaxRetries)
// - BUILDKITE_SPLITTER_RETRY_CMD (RetryCommand)
// - BUILDKITE_SPLITTER_SPLIT_BY_EXAMPLE (SplitByExample)
// - BUILDKITE_SPLITTER_SUITE_SLUG (SuiteSlug)
// - BUILDKITE_SPLITTER_TEST_CMD (TestCommand)
// - BUILDKITE_SPLITTER_TEST_FILE_PATTERN (TestFilePattern)
// - BUILDKITE_SPLITTER_TEST_FILE_EXCLUDE_PATTERN (TestFileExcludePattern)
// - BUILDKITE_BRANCH (Branch)
//
// If we are going to support other CI environment in the future,
// we will need to change where we read the configuration from.
func (c *Config) readFromEnv() error {
	var errs InvalidConfigError

	c.AccessToken = os.Getenv("BUILDKITE_SPLITTER_API_ACCESS_TOKEN")
	c.OrganizationSlug = os.Getenv("BUILDKITE_ORGANIZATION_SLUG")
	c.SuiteSlug = os.Getenv("BUILDKITE_SPLITTER_SUITE_SLUG")

	buildId := os.Getenv("BUILDKITE_BUILD_ID")
	if buildId == "" {
		errs.appendFieldError("BUILDKITE_BUILD_ID", "must not be blank")
	}

	stepId := os.Getenv("BUILDKITE_STEP_ID")
	if stepId == "" {
		errs.appendFieldError("BUILDKITE_STEP_ID", "must not be blank")
	}

	c.Identifier = fmt.Sprintf("%s/%s", buildId, stepId)

	c.ServerBaseUrl = getEnvWithDefault("BUILDKITE_SPLITTER_BASE_URL", "https://api.buildkite.com")
	c.TestCommand = os.Getenv("BUILDKITE_SPLITTER_TEST_CMD")
	c.TestFilePattern = os.Getenv("BUILDKITE_SPLITTER_TEST_FILE_PATTERN")
	c.TestFileExcludePattern = os.Getenv("BUILDKITE_SPLITTER_TEST_FILE_EXCLUDE_PATTERN")
	c.TestRunner = getEnvWithDefault("BUILDKITE_SPLITTER_TEST_RUNNER", "rspec")
	c.ResultPath = os.Getenv("BUILDKITE_SPLITTER_RESULT_PATH")

	c.SplitByExample = strings.ToLower(os.Getenv("BUILDKITE_SPLITTER_SPLIT_BY_EXAMPLE")) == "true"

	// used by Buildkite only, for experimental plans
	c.Branch = os.Getenv("BUILDKITE_BRANCH")

	MaxRetries, err := getIntEnvWithDefault("BUILDKITE_SPLITTER_RETRY_COUNT", 0)
	c.MaxRetries = MaxRetries
	if err != nil {
		errs.appendFieldError("BUILDKITE_SPLITTER_RETRY_COUNT", "was %q, must be a number", os.Getenv("BUILDKITE_SPLITTER_RETRY_COUNT"))
	}
	c.RetryCommand = os.Getenv("BUILDKITE_SPLITTER_RETRY_CMD")

	parallelism := os.Getenv("BUILDKITE_PARALLEL_JOB_COUNT")
	parallelismInt, err := strconv.Atoi(parallelism)
	c.Parallelism = parallelismInt
	if err != nil {
		errs.appendFieldError("BUILDKITE_PARALLEL_JOB_COUNT", "was %q, must be a number", parallelism)
	}

	nodeIndex := os.Getenv("BUILDKITE_PARALLEL_JOB")
	nodeIndexInt, err := strconv.Atoi(nodeIndex)
	c.NodeIndex = nodeIndexInt
	if err != nil {
		errs.appendFieldError("BUILDKITE_PARALLEL_JOB", "was %q, must be a number", nodeIndex)
	}

	if len(errs) > 0 {
		return errs
	}
	return nil
}
