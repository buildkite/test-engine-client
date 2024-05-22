package config

import (
	"fmt"
	"os"
	"strconv"
)

// readFromEnv reads the configuration from environment variables and sets it to the Config struct.
// It returns an InvalidConfigError if there is an error while reading the configuration
// such as when parallelism and node index are not numbers, and
// set default values for ServerBaseUrl and Mode if they are not set.
//
// Currently, it reads the following environment variables:
// - BUILDKITE_API_ACCESS_TOKEN (AccessToken)
// - BUILDKITE_ORGANIZATION_SLUG (OrganizationSlug)
// - BUILDKITE_PARALLEL_JOB_COUNT (Parallelism)
// - BUILDKITE_PARALLEL_JOB (NodeIndex)
// - BUILDKITE_SPLITTER_IDENTIFIER (Identifier)
// - BUILDKITE_SPLITTER_BASE_URL (ServerBaseUrl)
// - BUILDKITE_SPLITTER_MODE (Mode)
// - BUILDKITE_SPLITTER_SUITE_SLUG (SuiteSlug)
// - BUILDKITE_SPLITTER_TEST_CMD (TestCommand)
//
// If we are going to support other CI environment in the future,
// we will need to change where we read the configuration from.
func (c *Config) readFromEnv() error {
	var errs InvalidConfigError

	c.AccessToken = os.Getenv("BUILDKITE_API_ACCESS_TOKEN")
	c.OrganizationSlug = os.Getenv("BUILDKITE_ORGANIZATION_SLUG")
	c.SuiteSlug = os.Getenv("BUILDKITE_SPLITTER_SUITE_SLUG")

	c.Identifier = getEnvWithDefault("BUILDKITE_SPLITTER_IDENTIFIER", fmt.Sprintf("%s/%s", os.Getenv("BUILDKITE_BUILD_ID"), os.Getenv("BUILDKITE_STEP_ID")))
	c.ServerBaseUrl = getEnvWithDefault("BUILDKITE_SPLITTER_BASE_URL", "https://api.buildkite.com")
	c.Mode = getEnvWithDefault("BUILDKITE_SPLITTER_MODE", "static")
	c.TestCommand = os.Getenv("BUILDKITE_SPLITTER_TEST_CMD")

	parallelism := os.Getenv("BUILDKITE_PARALLEL_JOB_COUNT")
	parallelismInt, err := strconv.Atoi(parallelism)
	c.Parallelism = parallelismInt
	if err != nil {
		errs.appendFieldError("Parallelism", "was %q, must be a number", parallelism)
	}

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
