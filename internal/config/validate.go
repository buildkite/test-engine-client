package config

import (
	"net/url"
)

// validate checks if the Config struct is valid and returns InvalidConfigError if it's invalid.
func (c *Config) validate() error {

	if c.MaxRetries < 0 {
		c.errs.appendFieldError("BUILDKITE_TEST_ENGINE_RETRY_COUNT", "was %d, must be greater than or equal to 0", c.MaxRetries)
	}

	// We validate BUILDKITE_PARALLEL_JOB and BUILDKITE_PARALLEL_JOB_COUNT in two steps.
	// 1. Validate the type and presence of BUILDKITE_PARALLEL_JOB and BUILDKITE_PARALLEL_JOB_COUNT when reading them from the environment. See readFromEnv() in ./read.go.
	// 2. Validate the range of BUILDKITE_PARALLEL_JOB and BUILDKITE_PARALLEL_JOB_COUNT
	//
	// This is the second step. We don't validate the range of BUILDKITE_PARALLEL_JOB and BUILDKITE_PARALLEL_JOB_COUNT if the first validation step fails.
	//
	// The order of the range validation matters.
	// The range validation of BUILDKITE_PARALLEL_JOB depends on the result of BUILDKITE_PARALLEL_JOB_COUNT validation at the first step.
	// We need to validate the range of BUILDKITE_PARALLEL_JOB first before we add the range validation error to BUILDKITE_PARALLEL_JOB_COUNT.
	if c.errs["BUILDKITE_PARALLEL_JOB"] == nil {
		if got, min := c.NodeIndex, 0; got < 0 {
			c.errs.appendFieldError("BUILDKITE_PARALLEL_JOB", "was %d, must be greater than or equal to %d", got, min)
		}

		if c.errs["BUILDKITE_PARALLEL_JOB_COUNT"] == nil {
			if got, max := c.NodeIndex, c.Parallelism-1; got > max {
				c.errs.appendFieldError("BUILDKITE_PARALLEL_JOB", "was %d, must not be greater than %d", got, max)
			}
		}
	}

	if c.errs["BUILDKITE_PARALLEL_JOB_COUNT"] == nil {
		if got, min := c.Parallelism, 1; got < min {
			c.errs.appendFieldError("BUILDKITE_PARALLEL_JOB_COUNT", "was %d, must be greater than or equal to %d", got, min)
		}

		if got, max := c.Parallelism, 1000; got > max {
			c.errs.appendFieldError("BUILDKITE_PARALLEL_JOB_COUNT", "was %d, must not be greater than %d", got, max)
		}
	}

	if c.ServerBaseUrl != "" {
		if _, err := url.ParseRequestURI(c.ServerBaseUrl); err != nil {
			c.errs.appendFieldError("BUILDKITE_TEST_ENGINE_BASE_URL", "must be a valid URL")
		}
	}

	if c.AccessToken == "" {
		c.errs.appendFieldError("BUILDKITE_TEST_ENGINE_API_ACCESS_TOKEN", "must not be blank")
	}

	if c.OrganizationSlug == "" {
		c.errs.appendFieldError("BUILDKITE_ORGANIZATION_SLUG", "must not be blank")
	}

	if c.SuiteSlug == "" {
		c.errs.appendFieldError("BUILDKITE_TEST_ENGINE_SUITE_SLUG", "must not be blank")
	}

	if c.ResultPath == "" {
		c.errs.appendFieldError("BUILDKITE_TEST_ENGINE_RESULT_PATH", "must not be blank")
	}

	if len(c.errs) > 0 {
		return c.errs
	}

	return nil
}
