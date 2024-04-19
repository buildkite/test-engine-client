package config

import (
	"os"
	"strconv"
)

// readFromEnv reads the configuration from environment variables and sets it to the Config struct.
// It returns an InvalidConfigError if there is an error while reading the configuration
// such as when parallelism and node index are not numbers, and
// set default values for ServerBaseUrl and Mode if they are not set.
//
// Currently, it reads the following environment variables:
// - BUILDKITE_SPLITTER_IDENTIFIER (Identifier)
// - BUILDKITE_PARALLEL_JOB_COUNT (Parallelism)
// - BUILDKITE_PARALLEL_JOB (NodeIndex)
// - BUILDKITE_SPLITTER_BASE_URL (ServerBaseUrl)
// - BUILDKITE_SPLITTER_MODE (Mode)
// - BUILDKITE_SPLITTER_SUITE_TOKEN (SuiteToken)
//
// If we are going to support other CI environment in the future,
// we will need to change where we read the configuration from.
func (c *Config) readFromEnv() error {
	var errs InvalidConfigError

	c.SuiteToken = getEnvWithDefault("BUILDKITE_SPLITTER_SUITE_TOKEN", os.Getenv("BUILDKITE_ANALYTICS_TOKEN"))
	c.Identifier = getEnvWithDefault("BUILDKITE_SPLITTER_IDENTIFIER", os.Getenv("BUILDKITE_BUILD_ID"))
	c.ServerBaseUrl = getEnvWithDefault("BUILDKITE_SPLITTER_BASE_URL", "https://buildkite.com")
	c.Mode = getEnvWithDefault("BUILDKITE_SPLITTER_MODE", "static")

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
