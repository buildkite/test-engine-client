package command

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

// poolPlanningTimeout bounds creating or fetching a pool and waiting for its
// planning, like the retry budget of run and plan's API requests. Test
// Scheduler keeps retrying planning for up to 15 minutes, which is too long to
// hold up a build, so the pool commands fall back to a local split instead.
var poolPlanningTimeout = 130 * time.Second

var errPoolPlanningTimeout = errors.New("test pool planning timed out")

// ResolvePool is shared by pool plan, pool exec and pool consume. It creates or
// reuses the pool, or fetches a supplied pool ID, and, if wait is set, waits for
// planning to finish. Test discovery is not counted against poolPlanningTimeout.
func ResolvePool(ctx context.Context, cfg *config.Config, testFileList string, client *api.Client, wait bool) (pool api.Pool, err error) {
	var request api.PoolPlanParams
	if cfg.PoolID == "" {
		if request, err = poolPlanRequest(ctx, cfg, testFileList, client); err != nil {
			return api.Pool{}, err
		}
	}
	planCtx, cancel := context.WithTimeout(ctx, poolPlanningTimeout)
	defer cancel()
	defer func() {
		// Only the planning deadline is a fallback; the job's own cancellation isn't.
		if err != nil && ctx.Err() == nil && planCtx.Err() != nil {
			err = fmt.Errorf("%w after %s: %w", errPoolPlanningTimeout, poolPlanningTimeout, err)
		}
	}()
	if cfg.PoolID != "" {
		debug.Printf("Fetching supplied pool with ID %s", cfg.PoolID)
		if pool, err = client.GetPool(planCtx, cfg.PoolID); err != nil {
			return api.Pool{}, err
		}
		debug.Printf("Pool resolved (state=%s)", pool.State)
		// Like run's "Using existing plan": the pool was planned elsewhere.
		fmt.Fprintf(os.Stderr, "\nUsing existing pool\n  ID: %s\n", boundedRequestValue(pool.ID))
	} else {
		debug.Printf("Creating or reusing pool with key %s", cfg.PoolKey)
		if pool, err = client.PlanPool(planCtx, request); err != nil {
			return api.Pool{}, err
		}
		debug.Printf("Pool %s resolved (state=%s)", pool.ID, pool.State)
		fmt.Fprintln(os.Stderr, "\nPlanning test pool")
		fmt.Fprintf(os.Stderr, "  Key: %s\n", boundedRequestValue(cfg.PoolKey))
		fmt.Fprintf(os.Stderr, "  ID: %s\n", boundedRequestValue(pool.ID))
	}
	if !wait || pool.State != "planning" {
		return pool, nil
	}
	fmt.Fprintln(os.Stderr, "  Waiting for planning to finish...")
	if pool, err = client.WaitForPool(planCtx, pool.ID); err != nil {
		return api.Pool{}, err
	}
	debug.Printf("Pool planned (state=%s)", pool.State)
	return pool, nil
}

// poolPlanRequest discovers the tests and builds the request that creates or
// reuses the pool for cfg.PoolKey.
func poolPlanRequest(ctx context.Context, cfg *config.Config, testFileList string, client *api.Client) (api.PoolPlanParams, error) {
	if cfg.TestRunner != "rspec" && cfg.TestRunner != "jest" && cfg.TestRunner != "playwright" && cfg.TestRunner != "pytest" {
		return api.PoolPlanParams{}, fmt.Errorf("pool planning requires the rspec, jest, playwright or pytest runner")
	}
	printRequested(os.Stderr, cfg)
	for dimension, budget := range cfg.PoolLeaseCosts {
		fmt.Fprintf(os.Stderr, "  Pool lease cost: %s=%d\n", dimension, budget)
	}
	if cfg.PoolLeaseMaxAttempts != 0 {
		fmt.Fprintf(os.Stderr, "  Pool lease max attempts: %d\n", cfg.PoolLeaseMaxAttempts)
	}
	autoCollectGitMetadata(ctx, cfg, newGitRunner())
	testRunner, err := runner.DetectRunner(cfg)
	if err != nil {
		return api.PoolPlanParams{}, err
	}
	targets, err := getTestTargets(cfg, testRunner, testFileList)
	if err != nil {
		return api.PoolPlanParams{}, err
	}
	// Both discovery and example expansion can return nondeterministic order.
	// The server retains array order when fingerprinting the accepted request.
	slices.Sort(targets)
	params, err := createRequestParam(ctx, cfg, targets, *client, testRunner)
	if err != nil {
		return api.PoolPlanParams{}, err
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
	if len(cfg.PoolLeaseCosts) > 0 || cfg.PoolLeaseMaxAttempts != 0 {
		request.Lease = &api.PoolPlanLease{Costs: cfg.PoolLeaseCosts, MaxAttempts: cfg.PoolLeaseMaxAttempts}
	}
	return request, nil
}

func PoolPlan(ctx context.Context, cfg *config.Config, testFileList string, output PlanOutput, template string) error {
	if cfg.PoolID != "" {
		return fmt.Errorf("pool plan requires a pool key, not a pool ID; pass the ID to pool exec or pool consume")
	}
	printPlanningBanner(os.Stderr)
	client := api.NewClient(api.ClientConfig{
		ServerBaseURL: cfg.ServerBaseURL, OrganizationSlug: cfg.OrganizationSlug, AccessToken: cfg.AccessToken,
	})
	// Dynamic sizing needs the planned pool's recommended parallelism.
	pool, err := ResolvePool(ctx, cfg, testFileList, client, cfg.MaxParallelism > 0)
	if err != nil {
		return poolPlanFallback(ctx, cfg, err, output, template)
	}
	env := map[string]string{"BUILDKITE_TEST_ENGINE_POOL_ID": pool.ID}
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
// plan or serve the pool: retries ran out, the pool's planning failed, or the
// pool wasn't ready within poolPlanningTimeout. Rejected requests (4xx) and the
// caller's own cancellation are not fallbacks.
func poolSchedulerUnavailable(ctx context.Context, err error) bool {
	var errored *api.PoolErroredError
	return ctx.Err() == nil && (errors.Is(err, api.ErrRetryTimeout) || errors.Is(err, errPoolPlanningTimeout) || errors.As(err, &errored))
}

// warnPoolFallback prints the same warnings as the run and plan fallbacks.
func warnPoolFallback(err error) {
	var errored *api.PoolErroredError
	if errors.As(err, &errored) {
		printWarn("Error Plan", fmt.Sprintf("Test Scheduler failed to plan test pool %s: %s", errored.ID, errored.Message))
		return
	}
	if errors.Is(err, errPoolPlanningTimeout) {
		printWarn("Timeout", fmt.Sprintf("Test Scheduler did not finish planning the test pool within %s", poolPlanningTimeout))
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
