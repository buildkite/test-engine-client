package command

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/buildkite/test-engine-client/v3/internal/api"
	"github.com/buildkite/test-engine-client/v3/internal/config"
	"github.com/buildkite/test-engine-client/v3/internal/debug"
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
	printRequested(os.Stderr, cfg)
	if cfg.PoolLeaseDurationMS != 0 {
		fmt.Fprintf(os.Stderr, "  Pool lease duration budget: %s\n", time.Duration(cfg.PoolLeaseDurationMS)*time.Millisecond)
	}
	if cfg.PoolLeaseMaxAttempts != 0 {
		fmt.Fprintf(os.Stderr, "  Pool lease max attempts: %d\n", cfg.PoolLeaseMaxAttempts)
	}
	autoCollectGitMetadata(ctx, cfg, newGitRunner())
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
			MaxParallelism: params.MaxParallelism, TargetTime: params.TargetTime,
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

// printPlanningPool starts the section for a pool created or reused from a
// key. Callers print any waiting or parallelism lines after it.
func printPlanningPool(w io.Writer, cfg *config.Config, pool api.Pool) {
	fmt.Fprintln(w, "\nPlanning test pool")
	fmt.Fprintf(w, "  Key: %s\n", boundedRequestValue(cfg.PoolKey))
	fmt.Fprintf(w, "  ID: %s\n", boundedRequestValue(pool.ID))
}

func PoolPlan(ctx context.Context, cfg *config.Config, testFileList string, output PlanOutput, template string) error {
	if cfg.PoolID != "" {
		return fmt.Errorf("pool plan requires a pool key, not a pool ID; pass the ID to pool exec")
	}
	printPlanningBanner(os.Stderr)
	client := api.NewClient(api.ClientConfig{
		ServerBaseURL: cfg.ServerBaseURL, OrganizationSlug: cfg.OrganizationSlug, AccessToken: cfg.AccessToken,
	})
	pool, err := ResolvePool(ctx, cfg, testFileList, client)
	if err != nil {
		return poolPlanFallback(ctx, cfg, err, output, template)
	}
	debug.Printf("Pool %s resolved (state=%s)", pool.ID, pool.State)
	printPlanningPool(os.Stderr, cfg, pool)
	env := map[string]string{"BUILDKITE_TEST_ENGINE_POOL_ID": pool.ID}
	if cfg.MaxParallelism > 0 && pool.State == "planning" {
		fmt.Fprintln(os.Stderr, "  Waiting for planning to finish...")
		pool, err = client.WaitForPool(ctx, pool.ID)
		if err != nil {
			return poolPlanFallback(ctx, cfg, err, output, template)
		}
		debug.Printf("Pool %s planned (state=%s)", pool.ID, pool.State)
	}
	if cfg.MaxParallelism > 0 {
		parallelism := cfg.MaxParallelism
		if pool.Parallelism == nil {
			// Like bktec plan's local fallback, keep the build running at the
			// requested ceiling. Workers lease from the shared pool as they start,
			// so work spreads across up to this many jobs rather than being capped
			// at what the server would have recommended.
			fmt.Fprintf(os.Stderr, "⚠️ Test pool %s did not return recommended parallelism; falling back to --max-parallelism (%d).\n", pool.ID, parallelism)
		} else {
			parallelism = *pool.Parallelism
			fmt.Fprintf(os.Stderr, "  Parallelism: %d (recommended)\n", parallelism)
		}
		env["BUILDKITE_TEST_ENGINE_PARALLELISM"] = strconv.Itoa(parallelism)
	}
	return writePoolPlan(env, output, template)
}

// poolSchedulerUnavailable reports whether err means Test Scheduler could not
// plan or serve the pool: retries ran out, or the pool's planning failed.
// Rejected requests (4xx) and the caller's own cancellation are not fallbacks.
func poolSchedulerUnavailable(ctx context.Context, err error) bool {
	var errored *api.PoolErroredError
	return ctx.Err() == nil && (errors.Is(err, api.ErrRetryTimeout) || errors.As(err, &errored))
}

// warnPoolFallback prints the same warnings as the run and plan fallbacks.
func warnPoolFallback(err error) {
	var errored *api.PoolErroredError
	if errors.As(err, &errored) {
		printWarn("Error Plan", fmt.Sprintf("Test Scheduler failed to plan test pool %s: %s", errored.ID, errored.Message))
		return
	}
	// A retry timeout is recoverable, so handleError only prints its warning.
	_ = handleError(err)
}

// poolFatal formats a rejected API request like the fatal errors of run and
// plan. Unlike them, every 4xx is fatal here, because the pool commands fall
// back only when Test Scheduler is unavailable. Other errors are unchanged.
func poolFatal(err error) error {
	var (
		pool       *api.PoolError
		auth       *api.AuthError
		forbidden  *api.ForbiddenError
		billing    *api.BillingError
		badRequest *api.BadRequestError
		notFound   *api.NotFoundError
		disabled   *api.UnprocessableEntityError
	)
	switch {
	case errors.As(err, &pool):
		switch pool.StatusCode {
		case http.StatusBadRequest, http.StatusUnprocessableEntity:
			return fatal("Invalid Request", pool.Message)
		case http.StatusUnauthorized:
			return fatal("Authentication Failed", pool.Message)
		case http.StatusForbidden:
			return fatal("Access Denied", pool.Message)
		case http.StatusNotFound:
			return fatal("Not Found", pool.Message)
		case http.StatusConflict:
			return fatal("Test Pool Conflict", pool.Message)
		case http.StatusGone:
			return fatal("Test Pool Expired", pool.Message)
		}
		return fatal("Test Scheduler Error", pool)
	case errors.As(err, &auth):
		return fatal("Authentication Failed", auth.Message)
	case errors.As(err, &billing):
		return fatal("Billing Error", billing.Message)
	case errors.As(err, &forbidden):
		return fatal("Access Denied", forbidden.Message)
	case errors.As(err, &badRequest):
		return fatal("Invalid Request", badRequest.Message)
	case errors.As(err, &notFound):
		return fatal("Not Found", notFound.Message)
	case errors.As(err, &disabled):
		return fatal("Unavailable", disabled.Message)
	}
	return err
}

// poolPlanFallback mirrors bktec plan's local fallback when Test Scheduler is
// unavailable. It exports an empty pool ID so workers still try to create or
// reuse a pool themselves, then fall back to a static split if they can't.
func poolPlanFallback(ctx context.Context, cfg *config.Config, err error, output PlanOutput, template string) error {
	if !poolSchedulerUnavailable(ctx, err) {
		return poolFatal(err)
	}
	warnPoolFallback(err)
	fallback := makeFallbackPlan(cfg)
	printPlanningSummary(os.Stderr, fallback, "local fallback", cfg)
	return writePoolPlan(map[string]string{
		"BUILDKITE_TEST_ENGINE_POOL_ID":     "",
		"BUILDKITE_TEST_ENGINE_PARALLELISM": strconv.Itoa(fallback.Parallelism),
	}, output, template)
}

func writePoolPlan(env map[string]string, output PlanOutput, template string) error {
	switch output {
	case PlanOutputJSON:
		return json.NewEncoder(planWriter).Encode(env)
	case PlanOutputPipelineUpload:
		cmd := makePipelineUploadCommand(template)
		cmd.Env = os.Environ()
		for name, value := range env {
			cmd.Env = append(cmd.Env, name+"="+value)
		}
		return cmd.Run()
	default:
		return fmt.Errorf("unknown pool plan output format %v", output)
	}
}
