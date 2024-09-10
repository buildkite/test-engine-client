package config

import (
	"net/url"
)

// validate checks if the Config struct is valid and returns InvalidConfigError if it's invalid.
func (c *Config) validate() error {

	if c.MaxRetries < 0 {
		c.errs.appendFieldError("BUILDKITE_SPLITTER_RETRY_COUNT", "was %d, must be greater than or equal to 0", c.MaxRetries)
	}

	if c.errs["BUILDKITE_PARALLEL_JOB_COUNT"] == nil {
		if got, min := c.Parallelism, 1; got < min {
			c.errs.appendFieldError("BUILDKITE_PARALLEL_JOB_COUNT", "was %d, must be greater than or equal to %d", got, min)
		}

		if got, max := c.Parallelism, 1000; got > max {
			c.errs.appendFieldError("BUILDKITE_PARALLEL_JOB_COUNT", "was %d, must not be greater than %d", got, max)
		}
	}

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

	if c.ServerBaseUrl != "" {
		if _, err := url.ParseRequestURI(c.ServerBaseUrl); err != nil {
			c.errs.appendFieldError("BUILDKITE_SPLITTER_BASE_URL", "must be a valid URL")
		}
	}

	if c.AccessToken == "" {
		c.errs.appendFieldError("BUILDKITE_SPLITTER_API_ACCESS_TOKEN", "must not be blank")
	}

	if c.OrganizationSlug == "" {
		c.errs.appendFieldError("BUILDKITE_ORGANIZATION_SLUG", "must not be blank")
	}

	if c.SuiteSlug == "" {
		c.errs.appendFieldError("BUILDKITE_SPLITTER_SUITE_SLUG", "must not be blank")
	}

	if c.ResultPath == "" {
		c.errs.appendFieldError("ResultPath", "must not be blank")
	}

	if len(c.errs) > 0 {
		return c.errs
	}

	return nil
}
