package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// readFromEnv reads the configuration from environment variables and sets it to the Config struct.
// It returns an InvalidConfigError if there is an error while reading the configuration
// such as when parallelism and node index are not numbers, and
// set default values for ServerBaseUrl and Mode if they are not set.
//
// Currently, it reads the following environment variables:
// - BUILDKITE_ORGANIZATION_SLUG (OrganizationSlug)
// - BUILDKITE_PARALLEL_JOB_COUNT (Parallelism)
// - BUILDKITE_PARALLEL_JOB (NodeIndex)
// - BUILDKITE_SPLITTER_API_ACCESS_TOKEN (AccessToken)
// - BUILDKITE_SPLITTER_BASE_URL (ServerBaseUrl)
// - BUILDKITE_SPLITTER_MODE (Mode)
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

	c.Identifier = fmt.Sprintf("%s/%s", os.Getenv("BUILDKITE_BUILD_ID"), os.Getenv("BUILDKITE_STEP_ID"))
	c.ServerBaseUrl = getEnvWithDefault("BUILDKITE_SPLITTER_BASE_URL", "https://api.buildkite.com")
	c.Mode = getEnvWithDefault("BUILDKITE_SPLITTER_MODE", "static")
	c.TestCommand = os.Getenv("BUILDKITE_SPLITTER_TEST_CMD")
	c.TestFilePattern = os.Getenv("BUILDKITE_SPLITTER_TEST_FILE_PATTERN")
	c.TestFileExcludePattern = os.Getenv("BUILDKITE_SPLITTER_TEST_FILE_EXCLUDE_PATTERN")
	c.TestRunner = getEnvWithDefault("BUILDKITE_SPLITTER_TEST_RUNNER", "rspec")

	c.SplitByExample = strings.ToLower(os.Getenv("BUILDKITE_SPLITTER_SPLIT_BY_EXAMPLE")) == "true"

	// used by Buildkite only, for experimental plans
	c.Branch = os.Getenv("BUILDKITE_BRANCH")

	MaxRetries, err := getIntEnvWithDefault("BUILDKITE_SPLITTER_RETRY_COUNT", 0)
	c.MaxRetries = MaxRetries
	if err != nil {
		errs.appendFieldError("MaxRetries", "was %q, must be a number", os.Getenv("BUILDKITE_SPLITTER_RETRY_COUNT"))
	}
	c.RetryCommand = os.Getenv("BUILDKITE_SPLITTER_RETRY_CMD")

	parallelism := os.Getenv("BUILDKITE_PARALLEL_JOB_COUNT")
	parallelismInt, err := strconv.Atoi(parallelism)
	c.Parallelism = parallelismInt
	if err != nil {
		errs.appendFieldError("Parallelism", "was %q, must be a number", parallelism)
	}

	slowFileThreshold := getEnvWithDefault("BUILDKITE_SPLITTER_SLOW_FILE_THRESHOLD", "180000")
	slowFileThresholdInt, err := strconv.Atoi(slowFileThreshold)
	if err != nil {
		errs.appendFieldError("SlowFileThreshold", "was %q, must be a number", slowFileThreshold)
	}
	c.SlowFileThreshold = time.Duration(slowFileThresholdInt) * time.Millisecond

	nodeIndex := os.Getenv("BUILDKITE_PARALLEL_JOB")
	nodeIndexInt, err := strconv.Atoi(nodeIndex)
	c.NodeIndex = nodeIndexInt
	if err != nil {
		errs.appendFieldError("NodeIndex", "was %q, must be a number", nodeIndex)
	}

	if len(errs) > 0 {
		return errs
	}
	return nil
}
