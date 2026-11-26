package config

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestValidatePoolPlanIdentity(t *testing.T) {
	for _, tc := range []struct{ name, key, step, runner, wantError string }{
		{"step default", "", "step-1", "rspec", ""},
		{"explicit key", "shared", "", "rspec", ""},
		{"missing key", "", "", "rspec", "POOL_KEY"},
		{"jest", "shared", "", "jest", ""},
		{"unsupported runner", "shared", "", "cypress", "requires rspec, jest, playwright or pytest"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := New()
			c.AccessToken, c.OrganizationSlug, c.SuiteSlug = "token", "acme", "suite"
			c.OIDC, c.OIDCLifetime, c.BuildkiteAgentCommand = true, 20*time.Minute, "./mock-buildkite-agent"
			c.PoolKey, c.StepID, c.TestRunner = tc.key, tc.step, tc.runner
			c.BuildID, c.PipelineSlug = "build", "pipeline"
			err := c.ValidateForPoolPlan()
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
				return
			}
			require.NoError(t, err)
			if tc.key == "" {
				require.Equal(t, tc.step, c.PoolKey)
			}
			require.Empty(t, c.Identifier, "pool validation must not create a static plan identifier")
			require.Zero(t, c.Parallelism, "pool planning does not require lane sizing")
		})
	}
}

func TestValidatePoolExecAndConsumeRunners(t *testing.T) {
	for _, tc := range []struct {
		name, command, runner, resultPath, wantError string
		testCommand, retryCommand                    string
		missingPoolID, poolFallback                  bool
	}{
		{name: "exec requires a pool ID", command: "exec", runner: "rspec", missingPoolID: true, wantError: "BUILDKITE_TEST_ENGINE_POOL_ID must be set; create the pool with bktec pool plan"},
		{name: "pool plan fallback needs no pool ID", command: "exec", runner: "rspec", missingPoolID: true, poolFallback: true},
		{name: "consume requires a pool ID", command: "consume", runner: "jest", resultPath: "jest.json", missingPoolID: true, wantError: "BUILDKITE_TEST_ENGINE_POOL_ID must be set; create the pool with bktec pool plan"},
		{name: "exec runs rspec", command: "exec", runner: "rspec"},
		{name: "exec runs pytest", command: "exec", runner: "pytest"},
		{name: "exec rejects a pytest JUnit command", command: "exec", runner: "pytest", testCommand: "pytest {{testExamples}} --junit-xml={{resultPath}}", wantError: "BUILDKITE_TEST_ENGINE_TEST_CMD buildkite-pytest reports JSON; remove --junit-xml"},
		{name: "exec rejects a pytest JUnit retry command", command: "exec", runner: "pytest", retryCommand: "pytest --junit-xml=out.xml {{testExamples}}", wantError: "BUILDKITE_TEST_ENGINE_RETRY_CMD buildkite-pytest reports JSON"},
		{name: "exec has no jest runner", command: "exec", runner: "jest", wantError: "a persistent runner after -- requires rspec or pytest"},
		{name: "consume runs jest", command: "consume", runner: "jest", resultPath: "jest.json"},
		{name: "consume runs playwright", command: "consume", runner: "playwright", resultPath: "results.json"},
		{name: "consume runs pytest with its default result path", command: "consume", runner: "pytest"},
		{name: "consume rejects pytest fail-fast", command: "consume", runner: "pytest", testCommand: "pytest -q --maxfail=2 {{testExamples}}", wantError: "BUILDKITE_TEST_ENGINE_TEST_CMD"},
		{name: "consume rejects pytest fail-fast retries", command: "consume", runner: "pytest", retryCommand: "pytest -x {{testExamples}}", wantError: "BUILDKITE_TEST_ENGINE_RETRY_CMD"},
		{name: "consume runs jest, playwright or pytest", command: "consume", runner: "rspec", resultPath: "out.json", wantError: "running the test command per lease requires jest, playwright or pytest"},
		{name: "consume reads the result path", command: "consume", runner: "jest", wantError: "BUILDKITE_TEST_ENGINE_RESULT_PATH"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := New()
			c.AccessToken, c.OrganizationSlug, c.SuiteSlug = "header.payload.signature", "acme", "suite"
			c.OIDC, c.OIDCLifetime, c.BuildkiteAgentCommand = true, 20*time.Minute, "./mock-buildkite-agent"
			c.PoolID, c.TestRunner, c.ResultPath = "pool-1", tc.runner, tc.resultPath
			if tc.missingPoolID {
				c.PoolID = ""
			}
			c.PoolFallback = tc.poolFallback
			c.TestCommand, c.RetryCommand = tc.testCommand, tc.retryCommand
			validate := c.ValidateForPoolExec
			if tc.command == "consume" {
				validate = c.ValidateForPoolConsume
			}
			err := validate()
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
				return
			}
			require.NoError(t, err)
			if tc.command == "consume" {
				require.Equal(t, "mocktoken", c.UploadToken, "consume resolves the collector upload token like run")
			}
		})
	}
}

func TestValidatePoolPlanningOptions(t *testing.T) {
	for _, tc := range []struct {
		name, wantError, cost   string
		durationMS, maxAttempts int
		retryCount              int
		wantCosts               map[string]int
	}{
		{name: "defaults"},
		{name: "cost override", cost: "duration_p90_ms=60000", wantCosts: map[string]int{"duration_p90_ms": 60_000}},
		{name: "deprecated duration budgets p50", durationMS: 60_000, wantCosts: map[string]int{"duration_p50_ms": 60_000}},
		{name: "max attempts override", maxAttempts: 25},
		{name: "cost and deprecated duration", cost: "duration_p90_ms=60000", durationMS: 60_000, wantError: "pool-lease-cost cannot be combined"},
		{name: "cost without budget", cost: "duration_p50_ms", wantError: "pool-lease-cost must be"},
		{name: "cost without dimension", cost: "=60000", wantError: "pool-lease-cost must be"},
		{name: "zero cost budget", cost: "duration_p50_ms=0", wantError: "pool-lease-cost must be"},
		{name: "non-integer cost budget", cost: "duration_p50_ms=1m", wantError: "pool-lease-cost must be"},
		{name: "negative duration", durationMS: -1, wantError: "pool-lease-duration-ms"},
		{name: "negative attempts", maxAttempts: -1, wantError: "pool-lease-max-attempts"},
		{name: "pool retries", retryCount: 2},
		{name: "negative pool retries", retryCount: -1, wantError: "pool-retry-count"},
		{name: "pool retries overflow initial attempt", retryCount: math.MaxInt, wantError: "pool-retry-count"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := New()
			c.AccessToken, c.OrganizationSlug, c.SuiteSlug = "token", "acme", "suite"
			c.OIDC, c.OIDCLifetime, c.BuildkiteAgentCommand = true, 20*time.Minute, "./mock-buildkite-agent"
			c.PoolKey, c.TestRunner = "shared", "rspec"
			c.BuildID, c.PipelineSlug = "build", "pipeline"
			c.PoolLeaseCost, c.PoolLeaseDurationMS, c.PoolLeaseMaxAttempts = tc.cost, tc.durationMS, tc.maxAttempts
			c.PoolRetryCount = tc.retryCount
			err := c.ValidateForPoolPlan()
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.wantCosts, c.PoolLeaseCosts)
			}
		})
	}
}

func TestValidatePoolDynamicParallelism(t *testing.T) {
	for _, tc := range []struct {
		name           string
		targetTime     time.Duration
		maxParallelism int
		wantError      string
	}{
		{name: "omitted"},
		{name: "maximum only", maxParallelism: 120},
		{name: "target and maximum", targetTime: 2 * time.Minute, maxParallelism: 120},
		{name: "target requires maximum", targetTime: 2 * time.Minute, wantError: "max-parallelism must be set when target-time is set"},
		{name: "maximum is bounded", maxParallelism: 1001, wantError: "must be between 0 and 1000"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := New()
			c.AccessToken, c.OrganizationSlug, c.SuiteSlug = "header.payload.signature", "acme", "suite"
			c.PoolKey, c.TestRunner = "shared", "rspec"
			c.BuildID, c.PipelineSlug = "build", "pipeline"
			c.TargetTime, c.MaxParallelism = tc.targetTime, tc.maxParallelism
			err := c.ValidateForPoolPlan()
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestPoolOIDCClaims(t *testing.T) {
	// The fake agent returns its arguments instead of a token so we can inspect
	// both the pool-plan initial mint and the worker refresh contract.
	agent := filepath.Join(t.TempDir(), "agent")
	require.NoError(t, os.WriteFile(agent, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\"\n"), 0700))
	c := New()
	c.PoolID, c.OrganizationSlug, c.SuiteSlug, c.TestRunner = "pool-1", "acme", "suite", "rspec"
	c.AccessToken = "ordinary-api-token"
	c.OIDC, c.OIDCLifetime, c.BuildkiteAgentCommand = true, 20*time.Minute, agent
	require.NoError(t, c.ValidateForPoolExec())
	want := "oidc\nrequest-token\n--audience\nhttps://api.buildkite.com/v2/analytics/organizations/acme/suites/suite\n--lifetime\n1200"
	require.Equal(t, want+"\n--claim\norganization_id,pipeline_id,build_id,job_id", c.AccessToken)
	refreshed, err := c.RequestSchedulerOIDCToken(context.Background())
	require.NoError(t, err)
	require.Equal(t, c.AccessToken, refreshed)
	ordinary, err := c.RequestOIDCToken(context.Background())
	require.NoError(t, err)
	require.Equal(t, want, ordinary, "existing plan/run OIDC contract must not change")
}

func TestPoolRejectsDisabledOIDCWithAPIToken(t *testing.T) {
	c := New()
	c.AccessToken = "ordinary-api-token"
	require.ErrorContains(t, c.ValidateForPoolPlan(), "Test Scheduler pools require an OIDC token with job identity claims")
}

func TestPoolAcceptsSuppliedOIDCTokenWithoutAgent(t *testing.T) {
	c := New()
	c.PoolID, c.OrganizationSlug, c.SuiteSlug, c.TestRunner = "pool-1", "acme", "suite", "rspec"
	c.AccessToken = "header.payload.signature"
	c.OIDC = false
	require.NoError(t, c.ValidateForPoolExec())
	require.Equal(t, "header.payload.signature", c.AccessToken)
}
