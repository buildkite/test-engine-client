package main

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/buildkite/test-engine-client/v3/internal/config"
	"github.com/urfave/cli/v3"
)

func TestPoolExecArgumentsWithoutGate(t *testing.T) {
	var definition *cli.Command
	for _, c := range poolCommand.Commands {
		if c.Name == "exec" {
			definition = c
		}
	}
	if definition == nil {
		t.Fatal("pool exec not registered")
	}
	cmd := *definition
	cmd.Flags = freshFlags(definition.Flags)
	if err := cmd.Run(context.Background(), []string{"exec"}); err == nil || !strings.Contains(err.Error(), "requires -- <persistent-runner>") {
		t.Fatalf("runner validation: %v", err)
	}
	t.Setenv("BUILDKITE_TEST_SCHEDULER_LOCAL_RETRY_COUNT", "3")
	cmd = *definition
	cmd.Flags = freshFlags(definition.Flags)
	cmd.Action = func(_ context.Context, c *cli.Command) error {
		if !reflect.DeepEqual(c.Args().Slice(), []string{"bundle", "exec", "runner", "--flag", "argument with spaces"}) {
			t.Fatal(c.Args().Slice())
		}
		if c.Int("local-retry-count") != 3 {
			t.Fatal("environment retry count missing")
		}
		return nil
	}
	if err := cmd.Run(context.Background(), []string{"exec", "--", "bundle", "exec", "runner", "--flag", "argument with spaces"}); err != nil {
		t.Fatal(err)
	}
	cmd = *definition
	cmd.Flags = freshFlags(definition.Flags)
	if err := cmd.Run(context.Background(), []string{"exec", "--local-retry-count=-1", "--", "runner"}); err == nil || !strings.Contains(err.Error(), "negative") {
		t.Fatalf("retry validation: %v", err)
	}
}

func TestPoolExecCLILeasePlanningOptions(t *testing.T) {
	cfg = config.New()
	t.Cleanup(func() { cfg = config.New() })
	t.Setenv("BUILDKITE_TEST_ENGINE_POOL_LEASE_DURATION_MS", "50000")
	t.Setenv("BUILDKITE_TEST_ENGINE_POOL_LEASE_MAX_ATTEMPTS", "70")
	execCmd := *poolCommand.Commands[1]
	execCmd.Flags = poolExecCommandFlags()
	execCmd.Action = func(_ context.Context, c *cli.Command) error {
		if cfg.PoolLeaseDurationMS != 50_000 || cfg.PoolLeaseMaxAttempts != 25 {
			t.Errorf("planning overrides: duration=%d attempts=%d", cfg.PoolLeaseDurationMS, cfg.PoolLeaseMaxAttempts)
		}
		if !reflect.DeepEqual(c.Args().Slice(), []string{"bundle", "exec", "buildkite-rspec"}) {
			t.Errorf("runner args: %v", c.Args().Slice())
		}
		return nil
	}
	cmd := &cli.Command{Name: "bktec", Commands: []*cli.Command{{Name: "pool", Commands: []*cli.Command{&execCmd}}}}
	if err := cmd.Run(context.Background(), []string{"bktec", "pool", "exec", "--pool-lease-max-attempts", "25", "--", "bundle", "exec", "buildkite-rspec"}); err != nil {
		t.Fatal(err)
	}
}

func TestPoolExecCLIBindsSuppliedOIDCToken(t *testing.T) {
	cfg = config.New()
	t.Cleanup(func() { cfg = config.New() })
	t.Setenv("BUILDKITE_TEST_ENGINE_API_ACCESS_TOKEN", "header.payload.signature")
	execCmd := *poolCommand.Commands[1]
	execCmd.Flags = poolExecCommandFlags()
	execCmd.Action = func(_ context.Context, _ *cli.Command) error {
		if cfg.AccessToken != "header.payload.signature" {
			t.Fatalf("pool exec ignored supplied token")
		}
		return nil
	}
	cmd := &cli.Command{Name: "bktec", Commands: []*cli.Command{{Name: "pool", Commands: []*cli.Command{&execCmd}}}}
	if err := cmd.Run(context.Background(), []string{"bktec", "pool", "exec", "--no-oidc", "--", "buildkite-rspec"}); err != nil {
		t.Fatal(err)
	}
}
