package config

import (
	"context"
	"slices"
	"strconv"
	"strings"

	"github.com/kballard/go-shellquote"
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
		switch {
		case c.PoolLeaseCost != "" && c.PoolLeaseDurationMS != 0:
			c.errs.appendFieldError("pool-lease-cost", "cannot be combined with the deprecated pool-lease-duration-ms")
		case c.PoolLeaseCost != "":
			// Test Scheduler validates the dimension, so new ones need no bktec change.
			dimension, value, _ := strings.Cut(c.PoolLeaseCost, "=")
			budget, err := strconv.Atoi(value)
			if dimension == "" || err != nil || budget <= 0 {
				c.errs.appendFieldError("pool-lease-cost", "must be <dimension>=<positive budget>, such as duration_p50_ms=100000")
			} else {
				c.PoolLeaseCosts = map[string]int{dimension: budget}
			}
		case c.PoolLeaseDurationMS > 0:
			// p50 matches the Scheduler's default cost dimension.
			c.PoolLeaseCosts = map[string]int{"duration_p50_ms": c.PoolLeaseDurationMS}
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
	if c.PoolID == "" && c.TestRunner != "rspec" && c.TestRunner != "pytest" {
		c.errs.appendFieldError("BUILDKITE_TEST_ENGINE_TEST_RUNNER", "pool exec requires rspec or pytest; use pool consume for jest or playwright")
	}
	c.PersistentRunner = true
	// buildkite-pytest reports buildkite-test-collector's JSON, so pytest
	// tests are planned with its scopes. A command with --junit-xml asks for
	// JUnit scopes, which wouldn't match.
	if c.TestRunner == "pytest" {
		for field, command := range map[string]string{"BUILDKITE_TEST_ENGINE_TEST_CMD": c.TestCommand, "BUILDKITE_TEST_ENGINE_RETRY_CMD": c.RetryCommand} {
			if strings.Contains(command, "--junit-xml") {
				c.errs.appendFieldError(field, "pool exec runs pytest with buildkite-pytest, which reports JSON; remove --junit-xml")
			}
		}
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
	if c.TestRunner == "pytest" {
		for field, command := range map[string]string{"BUILDKITE_TEST_ENGINE_TEST_CMD": c.TestCommand, "BUILDKITE_TEST_ENGINE_RETRY_CMD": c.RetryCommand} {
			if pytestStopsEarly(command) {
				c.errs.appendFieldError(field, "pool consume doesn't support pytest options that stop at a failure, such as -x, --maxfail or --stepwise")
			}
		}
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

// pytestStopsEarly reports whether a pytest command stops at a failure. The
// tests it doesn't run are missing from the report, so their leased files
// would pass.
func pytestStopsEarly(command string) bool {
	words, err := shellquote.Split(command)
	if err != nil {
		return false
	}
	return slices.ContainsFunc(words, func(word string) bool {
		option, _, _ := strings.Cut(word, "=")
		switch option {
		case "-x", "--exitfirst", "--maxfail", "--sw", "--stepwise", "--sw-skip", "--stepwise-skip":
			return true
		}
		return false
	})
}
