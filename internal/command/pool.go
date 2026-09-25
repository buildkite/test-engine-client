package command

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/buildkite/test-engine-client/v3/internal/api"
	"github.com/buildkite/test-engine-client/v3/internal/config"
	"github.com/buildkite/test-engine-client/v3/internal/debug"
	"github.com/buildkite/test-engine-client/v3/internal/git"
	"github.com/buildkite/test-engine-client/v3/internal/runner"
)

// ResolvePool is shared by pool plan and pool exec. It stops after durable
// resolution; execution must call client.WaitForPool before dispatching work.
func ResolvePool(ctx context.Context, cfg *config.Config, testFileList string, client *api.Client) (api.Pool, error) {
	if cfg.PoolID != "" {
		return client.GetPool(ctx, cfg.PoolID)
	}
	if cfg.TestRunner != "rspec" {
		return api.Pool{}, fmt.Errorf("pool planning requires the rspec runner")
	}
	if cfg.SelectionStrategy != "" || cfg.CollectGitMetadata {
		autoCollectGitMetadata(ctx, cfg, &git.ExecGitRunner{})
	}
	testRunner, err := runner.DetectRunner(cfg)
	if err != nil {
		return api.Pool{}, err
	}
	targets, err := getTestTargets(cfg, testRunner, testFileList)
	if err != nil {
		return api.Pool{}, err
	}
	// Both discovery and example expansion can return nondeterministic order.
	// The server retains array order when fingerprinting the accepted request.
	slices.Sort(targets)
	params, err := createRequestParam(ctx, cfg, targets, *client, testRunner)
	if err != nil {
		return api.Pool{}, err
	}
	slices.SortFunc(params.Tests.Selectors, func(a, b api.TestPlanParamsSelector) int {
		return strings.Compare(a.Value, b.Value)
	})
	slices.SortFunc(params.Tests.Examples, func(a, b api.TestPlanExample) int {
		// Include every field so ties on path or identifier remain deterministic.
		return cmp.Or(
			cmp.Compare(a.Path, b.Path), cmp.Compare(a.Identifier, b.Identifier),
			cmp.Compare(a.Scope, b.Scope), cmp.Compare(a.Name, b.Name), cmp.Compare(a.Format, b.Format),
		)
	})
	request := api.PoolPlanParams{
		Suite: cfg.SuiteSlug, Pipeline: cfg.PipelineSlug, BuildID: cfg.BuildID, Key: cfg.PoolKey,
		Plan: api.PoolPlan{
			Runner: params.Runner, Branch: params.Branch, Tests: params.Tests,
			Selection: params.Selection, LocationPrefix: params.LocationPrefix, Metadata: params.Metadata,
		},
	}
	if cfg.PoolLeaseDurationMS != 0 || cfg.PoolLeaseMaxAttempts != 0 {
		request.Lease = &api.PoolPlanLease{MaxAttempts: cfg.PoolLeaseMaxAttempts}
		if cfg.PoolLeaseDurationMS != 0 {
			request.Lease.Costs = &api.PoolPlanLeaseCosts{DurationP90MS: cfg.PoolLeaseDurationMS}
		}
	}
	debug.Printf("Creating or reusing pool with key %s", cfg.PoolKey)
	return client.PlanPool(ctx, request)
}

func PoolPlan(ctx context.Context, cfg *config.Config, testFileList string, output PlanOutput, template string) error {
	if cfg.PoolID != "" {
		return fmt.Errorf("pool plan requires a pool key, not a pool ID; pass the ID to pool exec")
	}
	client := api.NewClient(api.ClientConfig{
		ServerBaseURL: cfg.ServerBaseURL, OrganizationSlug: cfg.OrganizationSlug, AccessToken: cfg.AccessToken,
	})
	pool, err := ResolvePool(ctx, cfg, testFileList, client)
	if err != nil {
		return err
	}
	debug.Printf("Pool %s resolved (state=%s)", pool.ID, pool.State)
	switch output {
	case PlanOutputJSON:
		return json.NewEncoder(planWriter).Encode(map[string]string{"BUILDKITE_TEST_ENGINE_POOL_ID": pool.ID})
	case PlanOutputPipelineUpload:
		cmd := makePipelineUploadCommand(template)
		cmd.Env = append(os.Environ(), "BUILDKITE_TEST_ENGINE_POOL_ID="+pool.ID)
		return cmd.Run()
	default:
		return fmt.Errorf("unknown pool plan output format %v", output)
	}
}
