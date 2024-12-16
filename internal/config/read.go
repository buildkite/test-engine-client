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
// - BUILDKITE_TEST_ENGINE_API_ACCESS_TOKEN (AccessToken)
// - BUILDKITE_TEST_ENGINE_BASE_URL (ServerBaseUrl)
// - BUILDKITE_TEST_ENGINE_RETRY_COUNT (MaxRetries)
// - BUILDKITE_TEST_ENGINE_RETRY_CMD (RetryCommand)
// - BUILDKITE_TEST_ENGINE_SPLIT_BY_EXAMPLE (SplitByExample)
// - BUILDKITE_TEST_ENGINE_SUITE_SLUG (SuiteSlug)
// - BUILDKITE_TEST_ENGINE_TEST_CMD (TestCommand)
// - BUILDKITE_TEST_ENGINE_TEST_FILE_PATTERN (TestFilePattern)
// - BUILDKITE_TEST_ENGINE_TEST_FILE_EXCLUDE_PATTERN (TestFileExcludePattern)
// - BUILDKITE_BRANCH (Branch)
// - BUILDKITE_RETRY_COUNT (JobRetryCount)
//
// If we are going to support other CI environment in the future,
// we will need to change where we read the configuration from.
func (c *Config) readFromEnv() error {

	c.AccessToken = os.Getenv("BUILDKITE_TEST_ENGINE_API_ACCESS_TOKEN")
	c.OrganizationSlug = os.Getenv("BUILDKITE_ORGANIZATION_SLUG")
	c.SuiteSlug = os.Getenv("BUILDKITE_TEST_ENGINE_SUITE_SLUG")

	buildId := os.Getenv("BUILDKITE_BUILD_ID")
	if buildId == "" {
		c.errs.appendFieldError("BUILDKITE_BUILD_ID", "must not be blank")
	}

	stepId := os.Getenv("BUILDKITE_STEP_ID")
	if stepId == "" {
		c.errs.appendFieldError("BUILDKITE_STEP_ID", "must not be blank")
	}

	c.Identifier = fmt.Sprintf("%s/%s", buildId, stepId)

	c.ServerBaseUrl = getEnvWithDefault("BUILDKITE_TEST_ENGINE_BASE_URL", "https://api.buildkite.com")
	c.TestCommand = os.Getenv("BUILDKITE_TEST_ENGINE_TEST_CMD")
	c.TestFilePattern = os.Getenv("BUILDKITE_TEST_ENGINE_TEST_FILE_PATTERN")
	c.TestFileExcludePattern = os.Getenv("BUILDKITE_TEST_ENGINE_TEST_FILE_EXCLUDE_PATTERN")
	c.TestRunner = os.Getenv("BUILDKITE_TEST_ENGINE_TEST_RUNNER")
	c.ResultPath = os.Getenv("BUILDKITE_TEST_ENGINE_RESULT_PATH")

	c.SplitByExample = strings.ToLower(os.Getenv("BUILDKITE_TEST_ENGINE_SPLIT_BY_EXAMPLE")) == "true"

	// used by Buildkite only, for experimental plans
	c.Branch = os.Getenv("BUILDKITE_BRANCH")

	JobRetryCount, err := getIntEnvWithDefault("BUILDKITE_RETRY_COUNT", 0)
	c.JobRetryCount = JobRetryCount
	if err != nil {
		c.errs.appendFieldError("BUILDKITE_RETRY_COUNT", "was %q, must be a number", os.Getenv("BUILDKITE_RETRY_COUNT"))
	}

	MaxRetries, err := getIntEnvWithDefault("BUILDKITE_TEST_ENGINE_RETRY_COUNT", 0)
	c.MaxRetries = MaxRetries
	if err != nil {
		c.errs.appendFieldError("BUILDKITE_TEST_ENGINE_RETRY_COUNT", "was %q, must be a number", os.Getenv("BUILDKITE_TEST_ENGINE_RETRY_COUNT"))
	}
	c.RetryCommand = os.Getenv("BUILDKITE_TEST_ENGINE_RETRY_CMD")

	parallelism := os.Getenv("BUILDKITE_PARALLEL_JOB_COUNT")
	parallelismInt, err := strconv.Atoi(parallelism)
	if err != nil {
		c.errs.appendFieldError("BUILDKITE_PARALLEL_JOB_COUNT", "was %q, must be a number", parallelism)
	}
	c.Parallelism = parallelismInt

	nodeIndex := os.Getenv("BUILDKITE_PARALLEL_JOB")
	nodeIndexInt, err := strconv.Atoi(nodeIndex)
	if err != nil {
		c.errs.appendFieldError("BUILDKITE_PARALLEL_JOB", "was %q, must be a number", nodeIndex)
	}
	c.NodeIndex = nodeIndexInt

	if len(c.errs) > 0 {
		return c.errs
	}
	return nil
}
