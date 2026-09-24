package main

import (
	"context"
	"testing"

	"github.com/buildkite/test-engine-client/v3/internal/config"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
)

func TestPoolPlanCLIRejectsExistingID(t *testing.T) {
	for _, override := range []bool{false, true} {
		t.Run(map[bool]string{false: "environment", true: "flag overrides environment"}[override], func(t *testing.T) {
			cfg = config.New()
			t.Cleanup(func() { cfg = config.New() })
			t.Setenv("BUILDKITE_TEST_ENGINE_POOL_ID", "env-pool")
			wantID := "env-pool"
			if override {
				wantID = "flag-pool"
			}
			// Rebuild flags so parse state cannot leak between invocations.
			planCmd := *poolCommand.Commands[0]
			planCmd.Flags = poolPlanCommandFlags()
			planCmd.MutuallyExclusiveFlags = []cli.MutuallyExclusiveFlags{{Required: true, Flags: [][]cli.Flag{{freshFlag(jsonFlag)}, {freshFlag(pipelineUploadFlag)}}}}
			cmd := &cli.Command{Name: "bktec", Commands: []*cli.Command{{Name: "pool", Commands: []*cli.Command{&planCmd}}}}
			args := []string{"bktec", "pool", "plan", "--json"}
			if override {
				args = append(args, "--pool-id", "flag-pool")
			}
			require.ErrorContains(t, cmd.Run(context.Background(), args), "pool exec; omit it to plan a pool")
			require.Equal(t, wantID, cfg.PoolID)
			require.Empty(t, cfg.AccessToken, "reject before authentication or discovery")
		})
	}
}

func TestPoolPlanCLILeaseOptions(t *testing.T) {
	cfg = config.New()
	t.Cleanup(func() { cfg = config.New() })
	t.Setenv("BUILDKITE_TEST_ENGINE_POOL_LEASE_DURATION_MS", "50000")
	t.Setenv("BUILDKITE_TEST_ENGINE_POOL_LEASE_MAX_ATTEMPTS", "70")
	planCmd := *poolCommand.Commands[0]
	planCmd.Flags = poolPlanCommandFlags()
	planCmd.MutuallyExclusiveFlags = []cli.MutuallyExclusiveFlags{{Required: true, Flags: [][]cli.Flag{{freshFlag(jsonFlag)}, {freshFlag(pipelineUploadFlag)}}}}
	cmd := &cli.Command{Name: "bktec", Commands: []*cli.Command{{Name: "pool", Commands: []*cli.Command{&planCmd}}}}
	err := cmd.Run(context.Background(), []string{"bktec", "pool", "plan", "--json", "--pool-lease-max-attempts", "25"})
	require.Error(t, err, "missing authentication/build context stops before planning")
	require.Equal(t, 50_000, cfg.PoolLeaseDurationMS, "environment supplies the duration budget")
	require.Equal(t, 25, cfg.PoolLeaseMaxAttempts, "the flag overrides the environment")
}
