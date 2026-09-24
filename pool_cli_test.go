package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
			t.Setenv("BUILDKITE_TEST_ENGINE_API_ACCESS_TOKEN", "ordinary-api-token")
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
			require.Equal(t, "ordinary-api-token", cfg.AccessToken, "pool ID is rejected before authentication")
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

func TestPoolPlanCLIAuthentication(t *testing.T) {
	for _, tc := range []struct {
		name, envToken, wantToken string
		args                      []string
	}{
		{name: "ordinary API token falls back to agent OIDC", envToken: "ordinary-api-token", wantToken: "claimed-oidc-token"},
		{name: "provided OIDC from environment", envToken: "header.payload.signature", wantToken: "header.payload.signature"},
		{name: "provided OIDC flag without agent minting", envToken: "ordinary-api-token", wantToken: "flag.payload.signature", args: []string{"--access-token", "flag.payload.signature", "--no-oidc"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg = config.New()
			t.Cleanup(func() { cfg = config.New() })
			requests := make(chan string, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v2/organizations/acme/test-scheduler/pools/plan" {
					requests <- r.Header.Get("Authorization")
					w.WriteHeader(http.StatusAccepted)
					io.WriteString(w, `{"id":"pool-1","state":"planning"}`)
					return
				}
				if r.URL.Path == "/v2/analytics/organizations/acme/suites/suite/test_plan/filter_tests" {
					io.WriteString(w, `{"tests":[]}`)
					return
				}
				t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
				w.WriteHeader(http.StatusNotFound)
			}))
			defer server.Close()
			list := filepath.Join(t.TempDir(), "files")
			require.NoError(t, os.WriteFile(list, []byte("spec/a_spec.rb\n"), 0600))
			agent := filepath.Join(t.TempDir(), "agent")
			require.NoError(t, os.WriteFile(agent, []byte("#!/bin/sh\nprintf 'claimed-oidc-token'\n"), 0700))
			t.Setenv("BUILDKITE_TEST_ENGINE_API_ACCESS_TOKEN", tc.envToken)
			t.Setenv("BUILDKITE_TEST_ENGINE_BASE_URL", server.URL)
			t.Setenv("BUILDKITE_ORGANIZATION_SLUG", "acme")
			t.Setenv("BUILDKITE_TEST_ENGINE_SUITE_SLUG", "suite")
			t.Setenv("BUILDKITE_TEST_ENGINE_TEST_RUNNER", "rspec")
			t.Setenv("BUILDKITE_BUILD_ID", "build-1")
			t.Setenv("BUILDKITE_PIPELINE_SLUG", "pipeline")
			t.Setenv("BUILDKITE_TEST_ENGINE_POOL_KEY", "shared")
			t.Setenv("BUILDKITE_TEST_ENGINE_POOL_ID", "")
			planCmd := *poolCommand.Commands[0]
			planCmd.Flags = poolPlanCommandFlags()
			planCmd.MutuallyExclusiveFlags = []cli.MutuallyExclusiveFlags{{Required: true, Flags: [][]cli.Flag{{freshFlag(jsonFlag)}, {freshFlag(pipelineUploadFlag)}}}}
			cmd := &cli.Command{Name: "bktec", Commands: []*cli.Command{{Name: "pool", Commands: []*cli.Command{&planCmd}}}}
			args := append([]string{"bktec", "pool", "plan", "--json", "--files", list, "--buildkite-agent-command", agent}, tc.args...)
			require.NoError(t, cmd.Run(context.Background(), args))
			require.Equal(t, "Bearer "+tc.wantToken, <-requests)
		})
	}
}
