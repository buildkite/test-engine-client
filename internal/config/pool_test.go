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
		{"unsupported runner", "", "shared", "", "pytest", "requires rspec"},
		{"precreated skips discovery validation", "pool-1", "", "", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := New()
			c.AccessToken, c.OrganizationSlug, c.SuiteSlug = "token", "acme", "suite"
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

func TestValidatePoolLeaseOptions(t *testing.T) {
	for _, tc := range []struct {
		name, poolID, wantError string
		durationMS, maxAttempts int
	}{
		{name: "defaults"},
		{name: "duration override", durationMS: 60_000},
		{name: "max attempts override", maxAttempts: 25},
		{name: "negative duration", durationMS: -1, wantError: "pool-lease-duration-ms"},
		{name: "negative attempts", maxAttempts: -1, wantError: "pool-lease-max-attempts"},
		{name: "existing pool ignores planning options", poolID: "pool-1", durationMS: 60_000, maxAttempts: 25},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := New()
			c.AccessToken, c.OrganizationSlug, c.SuiteSlug = "token", "acme", "suite"
			c.PoolID, c.PoolKey, c.TestRunner = tc.poolID, "shared", "rspec"
			c.BuildID, c.PipelineSlug = "build", "pipeline"
			c.PoolLeaseDurationMS, c.PoolLeaseMaxAttempts = tc.durationMS, tc.maxAttempts
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
