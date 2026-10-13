package config

import (
	"context"
	"strings"
)

const schedulerOIDCClaims = "organization_id,pipeline_id,build_id,job_id"

// RequestSchedulerOIDCToken requests the suite audience with the job identity
// claims required by the Scheduler, including when refreshing a worker's token.
func (c *Config) RequestSchedulerOIDCToken(ctx context.Context) (string, error) {
	return c.requestOIDCToken(ctx, schedulerOIDCClaims)
}

// ValidateForPoolPlan does not require static-plan identifiers, lane sizing,
// execution output paths, or discovery inputs when an existing pool is supplied.
func (c *Config) ValidateForPoolPlan() error {
	// The Scheduler requires an OIDC token with job identity claims, not an
	// ordinary API token that may be set in the environment. JWT shape
	// distinguishes obvious API tokens; the Scheduler validates the supplied
	// token's signature and claims.
	if c.AccessToken != "" && strings.Count(c.AccessToken, ".") != 2 {
		if !c.OIDC {
			c.errs.appendFieldError("BUILDKITE_TEST_ENGINE_API_ACCESS_TOKEN", "Test Scheduler pools require an OIDC token with job identity claims, not an ordinary API token")
			return c.errs
		}
		c.AccessToken = ""
	}
	c.validateAPI(schedulerOIDCClaims)
	if c.PoolID == "" {
		c.validateDynamicParallelism()
		if c.PoolLeaseDurationMS < 0 {
			c.errs.appendFieldError("pool-lease-duration-ms", "must be 0 (server default) or a positive millisecond budget")
		}
		if c.PoolLeaseMaxAttempts < 0 {
			c.errs.appendFieldError("pool-lease-max-attempts", "must be 0 (server default) or a positive attempt limit")
		}
		if c.PoolKey == "" {
			c.PoolKey = c.StepID
		}
		if c.PoolKey == "" {
			c.errs.appendFieldError("BUILDKITE_TEST_ENGINE_POOL_KEY", "must be set when BUILDKITE_STEP_ID is absent")
		}
		if c.BuildID == "" {
			c.errs.appendFieldError("BUILDKITE_BUILD_ID", "must not be blank")
		}
		if c.PipelineSlug == "" {
			c.errs.appendFieldError("BUILDKITE_PIPELINE_SLUG", "must not be blank")
		}
		if c.TestRunner != "rspec" && c.TestRunner != "jest" && c.TestRunner != "playwright" && c.TestRunner != "pytest" {
			c.errs.appendFieldError("BUILDKITE_TEST_ENGINE_TEST_RUNNER", "pool planning requires rspec, jest, playwright or pytest")
		}
		// A Playwright example is one test in one project, but its file:line runs
		// the test in every project, so pools only lease Playwright files.
		if c.TestRunner == "playwright" && c.SplitByExample {
			c.errs.appendFieldError("BUILDKITE_TEST_ENGINE_SPLIT_BY_EXAMPLE", "split by example is not supported for Playwright pools")
		}
		if c.TagFilters != "" {
			c.errs.appendFieldError("BUILDKITE_TEST_ENGINE_TAG_FILTERS", "tag filtering is not supported for test pools")
		}
		if c.SelectionStrategy == "" && len(c.SelectionParams) > 0 {
			c.errs.appendFieldError("selection-param", "selection strategy must be set when selection params are provided")
		}
	}
	if len(c.errs) > 0 {
		return c.errs
	}
	return nil
}

// ValidateForPoolExec only allows the runners that have a persistent runner.
func (c *Config) ValidateForPoolExec() error {
	if c.PoolID == "" && c.TestRunner != "rspec" {
		c.errs.appendFieldError("BUILDKITE_TEST_ENGINE_TEST_RUNNER", "pool exec requires rspec; use pool consume for jest, playwright or pytest")
	}
	return c.ValidateForPoolPlan()
}

// ValidateForPoolConsume also checks what running the test command needs, and
// resolves the collector upload token like bktec run.
func (c *Config) ValidateForPoolConsume() error {
	if c.TestRunner != "jest" && c.TestRunner != "playwright" && c.TestRunner != "pytest" {
		c.errs.appendFieldError("BUILDKITE_TEST_ENGINE_TEST_RUNNER", "pool consume requires jest, playwright or pytest")
	}
	// Like bktec run, pytest writes its report to a temporary file by default.
	if c.ResultPath == "" && c.TestRunner != "pytest" {
		c.errs.appendFieldError("BUILDKITE_TEST_ENGINE_RESULT_PATH", "must not be blank")
	}
	if err := c.ValidateForPoolPlan(); err != nil {
		return err
	}
	c.resolveUploadToken()
	if len(c.errs) > 0 {
		return c.errs
	}
	return nil
}
