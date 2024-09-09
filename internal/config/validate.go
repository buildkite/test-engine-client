package config

import (
	"net/url"
)

// validate checks if the Config struct is valid and returns InvalidConfigError if it's invalid.
func (c *Config) validate() error {
	var errs InvalidConfigError

	if c.MaxRetries < 0 {
		errs.appendFieldError("BUILDKITE_SPLITTER_RETRY_COUNT", "was %d, must be greater than or equal to 0", c.MaxRetries)
	}

	if c.Parallelism == nil {
		errs.appendFieldError("BUILDKITE_PARALLEL_JOB_COUNT", "not set in environment")
	} else if got, min := *c.Parallelism, 1; got < min {
		errs.appendFieldError("BUILDKITE_PARALLEL_JOB_COUNT", "was %d, must be greater than or equal to %d", got, min)
	}

	if c.NodeIndex == nil {
		errs.appendFieldError("BUILDKITE_PARALLEL_JOB", "not set in environment")
	} else {
		if got, min := *c.NodeIndex, 0; got < 0 {
			errs.appendFieldError("BUILDKITE_PARALLEL_JOB", "was %d, must be greater than or equal to %d", got, min)
		}
		if c.Parallelism != nil {
			if got, max := *c.NodeIndex, *c.Parallelism-1; got > max {
				errs.appendFieldError("BUILDKITE_PARALLEL_JOB", "was %d, must not be greater than %d", got, max)
			}
		}
	}

	if c.ServerBaseUrl != "" {
		if _, err := url.ParseRequestURI(c.ServerBaseUrl); err != nil {
			errs.appendFieldError("BUILDKITE_SPLITTER_BASE_URL", "must be a valid URL")
		}
	}

	if c.AccessToken == "" {
		errs.appendFieldError("BUILDKITE_SPLITTER_API_ACCESS_TOKEN", "must not be blank")
	}

	if c.OrganizationSlug == "" {
		errs.appendFieldError("BUILDKITE_ORGANIZATION_SLUG", "must not be blank")
	}

	if c.SuiteSlug == "" {
		errs.appendFieldError("BUILDKITE_SPLITTER_SUITE_SLUG", "must not be blank")
	}

	if c.ResultPath == "" {
		errs.appendFieldError("ResultPath", "must not be blank")
	}

	if len(errs) > 0 {
		return errs
	}

	return nil
}
