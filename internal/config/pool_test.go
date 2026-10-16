package config

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestValidatePoolPlanIdentity(t *testing.T) {
	for _, tc := range []struct{ name, id, key, step, runner, wantError string }{
		{"step default", "", "", "step-1", "rspec", ""},
		{"explicit key", "", "shared", "", "rspec", ""},
		{"missing key", "", "", "", "rspec", "POOL_KEY"},
		{"jest", "", "shared", "", "jest", ""},
		{"unsupported runner", "", "shared", "", "cypress", "requires rspec, jest, playwright or pytest"},
		{"precreated skips discovery validation", "pool-1", "", "", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := New()
			c.AccessToken, c.OrganizationSlug, c.SuiteSlug = "token", "acme", "suite"
			c.OIDC, c.OIDCLifetime, c.BuildkiteAgentCommand = true, 20*time.Minute, "./mock-buildkite-agent"
			c.PoolID, c.PoolKey, c.StepID, c.TestRunner = tc.id, tc.key, tc.step, tc.runner
			if tc.id == "" {
				c.BuildID, c.PipelineSlug = "build", "pipeline"
			}
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
		name, command, poolID, runner, resultPath, wantError string
		testCommand, retryCommand                            string
		splitByExample                                       bool
	}{
		{name: "exec plans rspec", command: "exec", runner: "rspec"},
		{name: "exec plans pytest", command: "exec", runner: "pytest"},
		{name: "exec has no jest runner", command: "exec", runner: "jest", wantError: "pool exec requires rspec or pytest"},
		{name: "exec with pool ID", command: "exec", poolID: "pool-1"},
		{name: "consume plans jest", command: "consume", runner: "jest", resultPath: "jest.json"},
		{name: "consume plans playwright", command: "consume", runner: "playwright", resultPath: "results.json"},
		{name: "consume plans playwright files only", command: "consume", runner: "playwright", resultPath: "results.json", splitByExample: true, wantError: "split by example is not supported for Playwright pools"},
		{name: "consume plans pytest with its default result path", command: "consume", runner: "pytest"},
		{name: "consume rejects pytest fail-fast", command: "consume", runner: "pytest", testCommand: "pytest -q --maxfail=2 {{testExamples}}", wantError: "BUILDKITE_TEST_ENGINE_TEST_CMD"},
		{name: "consume rejects pytest fail-fast retries", command: "consume", runner: "pytest", retryCommand: "pytest -x {{testExamples}}", wantError: "BUILDKITE_TEST_ENGINE_RETRY_CMD"},
		{name: "consume with pool ID still runs jest, playwright or pytest", command: "consume", poolID: "pool-1", runner: "rspec", resultPath: "out.json", wantError: "pool consume requires jest, playwright or pytest"},
		{name: "consume reads the result path", command: "consume", poolID: "pool-1", runner: "jest", wantError: "BUILDKITE_TEST_ENGINE_RESULT_PATH"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := New()
			c.AccessToken, c.OrganizationSlug, c.SuiteSlug = "header.payload.signature", "acme", "suite"
			c.OIDC, c.OIDCLifetime, c.BuildkiteAgentCommand = true, 20*time.Minute, "./mock-buildkite-agent"
			c.PoolID, c.PoolKey, c.TestRunner, c.ResultPath, c.SplitByExample = tc.poolID, "shared", tc.runner, tc.resultPath, tc.splitByExample
			c.TestCommand, c.RetryCommand = tc.testCommand, tc.retryCommand
			c.BuildID, c.PipelineSlug = "build", "pipeline"
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

func TestValidatePoolLeaseOptions(t *testing.T) {
	for _, tc := range []struct {
		name, poolID, wantError, cost string
		durationMS, maxAttempts       int
		wantCosts                     map[string]int
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
		{name: "existing pool ignores planning options", poolID: "pool-1", cost: "bogus", durationMS: 60_000, maxAttempts: 25},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := New()
			c.AccessToken, c.OrganizationSlug, c.SuiteSlug = "token", "acme", "suite"
			c.OIDC, c.OIDCLifetime, c.BuildkiteAgentCommand = true, 20*time.Minute, "./mock-buildkite-agent"
			c.PoolID, c.PoolKey, c.TestRunner = tc.poolID, "shared", "rspec"
			c.BuildID, c.PipelineSlug = "build", "pipeline"
			c.PoolLeaseCost, c.PoolLeaseDurationMS, c.PoolLeaseMaxAttempts = tc.cost, tc.durationMS, tc.maxAttempts
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
		poolID         string
		targetTime     time.Duration
		maxParallelism int
		wantError      string
	}{
		{name: "omitted"},
		{name: "maximum only", maxParallelism: 120},
		{name: "target and maximum", targetTime: 2 * time.Minute, maxParallelism: 120},
		{name: "target requires maximum", targetTime: 2 * time.Minute, wantError: "max-parallelism must be set when target-time is set"},
		{name: "maximum is bounded", maxParallelism: 1001, wantError: "must be between 0 and 1000"},
		{name: "existing pool ignores planning sizing", poolID: "pool-1", targetTime: 2 * time.Minute},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := New()
			c.AccessToken, c.OrganizationSlug, c.SuiteSlug = "header.payload.signature", "acme", "suite"
			c.PoolID, c.PoolKey, c.TestRunner = tc.poolID, "shared", "rspec"
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
	c.PoolID, c.OrganizationSlug, c.SuiteSlug = "pool-1", "acme", "suite"
	c.AccessToken = "ordinary-api-token"
	c.OIDC, c.OIDCLifetime, c.BuildkiteAgentCommand = true, 20*time.Minute, agent
	require.NoError(t, c.ValidateForPoolPlan())
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
	c.PoolID, c.OrganizationSlug, c.SuiteSlug = "pool-1", "acme", "suite"
	c.AccessToken = "header.payload.signature"
	c.OIDC = false
	require.NoError(t, c.ValidateForPoolPlan())
	require.Equal(t, "header.payload.signature", c.AccessToken)
}
