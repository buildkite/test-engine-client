package config

import (
	"fmt"
	"net/url"
	"time"
)

// Checks common to all commands
func (c *Config) validate() error {
	if c.MaxRetries < 0 {
		c.errs.appendFieldError("BUILDKITE_TEST_ENGINE_RETRY_COUNT", "was %d, must be greater than or equal to 0", c.MaxRetries)
	}

	if c.Identifier == "" {
		if c.BuildId != "" && c.StepId != "" {
			c.Identifier = fmt.Sprintf("%s/%s", c.BuildId, c.StepId)
		} else {
			if c.BuildId == "" {
				c.errs.appendFieldError("BUILDKITE_BUILD_ID", "must not be blank")
			}
			if c.StepId == "" {
				c.errs.appendFieldError("BUILDKITE_STEP_ID", "must not be blank")
			}
		}
	}

	if c.ServerBaseUrl == "" {
		c.ServerBaseUrl = "https://api.buildkite.com"
	} else {
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

	runnersWithoutResultPath := map[string]bool{
		"cypress":      true,
		"pytest":       true,
		"pytest-pants": true,
		"custom":       true,
	}
	if c.ResultPath == "" && !runnersWithoutResultPath[c.TestRunner] {
		c.errs.appendFieldError("BUILDKITE_TEST_ENGINE_RESULT_PATH", "must not be blank")
	}

	if c.TestRunner == "" {
		c.errs.appendFieldError("BUILDKITE_TEST_ENGINE_TEST_RUNNER", "must not be blank")
	}

	if c.TestRunner == "custom" {
		if c.TestCommand == "" {
			c.errs.appendFieldError("BUILDKITE_TEST_ENGINE_TEST_CMD", "must not be blank when using the custom test runner")
		}
		if c.TestFilePattern == "" {
			c.errs.appendFieldError("BUILDKITE_TEST_ENGINE_TEST_FILE_PATTERN", "must not be blank when using the custom test runner")
		}
	}

	if len(c.errs) > 0 {
		return c.errs
	}

	return nil
}

// Validation for the `bktec run` command
func (c *Config) ValidateForRun() error {
	_ = c.validate()

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

	if len(c.errs) > 0 {
		return c.errs
	}

	return nil
}

// Validation for the `bktec plan` command
func (c *Config) ValidateForPlan() error {
	_ = c.validate()

	if c.TargetTime != 0 {
		if c.TargetTime <= 0 {
			c.errs.appendFieldError("target-time", "was %s, must be greater than 0", c.TargetTime.String())
		}

		if c.TargetTime > time.Hour*24 {
			c.errs.appendFieldError("target-time", "was %s, must be less than or equal to 24 hours", c.TargetTime.String())
		}

		if c.MaxParallelism == 0 {
			c.errs.appendFieldError("max-parallelism", "must be set when target-time is set")
		}
	}

	if c.MaxParallelism != 0 {
		if c.MaxParallelism < 0 || c.MaxParallelism > 1000 {
			c.errs.appendFieldError("max-parallelism", "was %d, must be between 0 and 1000", c.MaxParallelism)
		}
	}

	if len(c.errs) > 0 {
		return c.errs
	}

	return nil
}
