package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
)

func (c *Config) fetchFromEnv() error {
	var errs []error
	c.SuiteToken = os.Getenv("BUILDKITE_SUITE_TOKEN")
	c.Identifier = os.Getenv("BUILDKITE_BUILD_ID")
	c.ServerBaseUrl = getEnvWithDefault("BUILDKITE_SPLITTER_BASE_URL", "https://buildkite.com")
	c.Mode = getEnvWithDefault("BUILDKITE_SPLITTER_MODE", "static")

	parallelism := os.Getenv("BUILDKITE_PARALLEL_JOB_COUNT")
	parallelismInt, err := strconv.Atoi(parallelism)
	if err != nil {
		errs = append(errs, fmt.Errorf("%w: %s", ErrInvalidConfig, "parallelism must be an integer"))
	} else {
		c.Parallelism = parallelismInt
	}

	nodeIndex := os.Getenv("BUILDKITE_PARALLEL_JOB")
	nodeIndexInt, err := strconv.Atoi(nodeIndex)
	if err != nil {
		errs = append(errs, fmt.Errorf("%w: %s", ErrInvalidConfig, "node index must be an integer"))
	} else {
		c.NodeIndex = nodeIndexInt
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}
