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
// - BUILDKITE_BUILD_ID (Identifier)
// - BUILDKITE_PARALLEL_JOB_COUNT (Parallelism)
// - BUILDKITE_PARALLEL_JOB (NodeIndex)
// - BUILDKITE_SPLITTER_BASE_URL (ServerBaseUrl)
// - BUILDKITE_SPLITTER_MODE (Mode)
// - BUILDKITE_SUITE_TOKEN (SuiteToken)
//
// If we are going to support other CI environment in the future,
// we will need to change where we read the configuration from.
func (c *Config) readFromEnv() error {
	var errs InvalidConfigError

	c.SuiteToken = os.Getenv("BUILDKITE_SUITE_TOKEN")
	c.Identifier = os.Getenv("BUILDKITE_BUILD_ID")

	c.ServerBaseUrl = getEnvWithDefault("BUILDKITE_SPLITTER_BASE_URL", "https://buildkite.com")
	c.Mode = getEnvWithDefault("BUILDKITE_SPLITTER_MODE", "static")

	parallelism := os.Getenv("BUILDKITE_PARALLEL_JOB_COUNT")
	parallelismInt, err := strconv.Atoi(parallelism)
	if err != nil {
		errs.appendFieldError("Parallelism", "must be a number")
	} else {
		c.Parallelism = parallelismInt
	}

	nodeIndex := os.Getenv("BUILDKITE_PARALLEL_JOB")
	nodeIndexInt, err := strconv.Atoi(nodeIndex)
	if err != nil {
		errs.appendFieldError("NodeIndex", "must be a number")
	} else {
		c.NodeIndex = nodeIndexInt
	}

	if len(errs) > 0 {
		return errs
	}
	return nil
}
