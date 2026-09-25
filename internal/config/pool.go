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
	// The Scheduler requires a suite-scoped job OIDC token, not an ordinary API
	// token that may be set in the environment. JWT shape distinguishes obvious
	// API tokens; the Scheduler validates the supplied token's signature and claims.
	if c.AccessToken != "" && strings.Count(c.AccessToken, ".") != 2 {
		if !c.OIDC {
			c.errs.appendFieldError("BUILDKITE_TEST_ENGINE_API_ACCESS_TOKEN", "Test Scheduler pools require a suite-scoped OIDC token, not an ordinary API token")
			return c.errs
		}
		c.AccessToken = ""
	}
	c.validateAPI(schedulerOIDCClaims)
	if c.PoolID == "" {
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
		if c.TestRunner != "rspec" {
			c.errs.appendFieldError("BUILDKITE_TEST_ENGINE_TEST_RUNNER", "pool planning requires rspec")
		}
		if c.TagFilters != "" {
			c.errs.appendFieldError("BUILDKITE_TEST_ENGINE_TAG_FILTERS", "tag filtering is only supported for the pytest test runner")
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
