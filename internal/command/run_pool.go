package command

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"time"

	"github.com/buildkite/test-engine-client/v2/internal/api"
	"github.com/buildkite/test-engine-client/v2/internal/config"
	"github.com/buildkite/test-engine-client/v2/internal/debug"
	"github.com/buildkite/test-engine-client/v2/internal/plan"
	"github.com/buildkite/test-engine-client/v2/internal/runner"
)

var (
	// poolResolveTimeout is how long to wait for the pool to be created by the
	// plan step before giving up.
	poolResolveTimeout = 5 * time.Minute
	// poolResolveInterval is how often to poll for the pool while waiting for
	// the plan step to create it.
	poolResolveInterval = 5 * time.Second
	// poolIdleInterval is how long to wait before re-attempting a lease when
	// the pool has no waiting work but is not yet drained.
	poolIdleInterval = 5 * time.Second
	// leaseTTLSeconds is the lease expiry requested on lease creation and
	// renewal.
	leaseTTLSeconds = 300
	// leaseHeartbeatInterval is how often the heartbeat goroutine renews the
	// active lease.
	leaseHeartbeatInterval = 60 * time.Second
)

// RunTestSchedulerPool leases batches of tests from a Test Scheduler pool and
// runs them until the pool is drained, instead of running a static test plan
// split.
func RunTestSchedulerPool(ctx context.Context, cfg *config.Config) error {
	printStartUpMessage()

	testRunner, err := runner.DetectRunner(cfg)
	if err != nil {
		return fmt.Errorf("unsupported value for BUILDKITE_TEST_ENGINE_TEST_RUNNER: %w", err)
	}

	// apiClient is used by the existing execution path (test result uploads),
	// authenticated with the regular access token.
	apiClient := api.NewClient(api.ClientConfig{
		ServerBaseURL:    cfg.ServerBaseURL,
		UploadBaseURL:    cfg.UploadBaseURL,
		AccessToken:      cfg.AccessToken,
		OrganizationSlug: cfg.OrganizationSlug,
	})

	schedulerClient, claims, err := newSchedulerClient(cfg)
	if err != nil {
		return err
	}

	pool, err := resolveSchedulerPool(ctx, schedulerClient, claims, cfg.PoolName)
	if err != nil {
		return err
	}

	fmt.Printf("Using test pool %s (%s)\n", pool.ID, pool.State)

	attemptsExecuted := 0
	leaseCount := 0
	completeFailures := 0
	// failureErr is the run error from the first failed batch. It wraps the
	// test runner's exec.ExitError so the process exits with the same code as
	// a static-split run would.
	var failureErr error

	for {
		leaseResponse, err := schedulerClient.CreateSchedulerLease(ctx, pool.ID, api.CreateSchedulerLeaseParams{
			TargetCostLimit: cfg.TargetCostLimit,
			LeaseTTLSeconds: leaseTTLSeconds,
		})
		if err != nil {
			return fmt.Errorf("acquiring lease from test pool: %w", err)
		}

		lease := leaseResponse.Lease
		if lease == nil {
			metrics, err := schedulerClient.FetchSchedulerPoolMetrics(ctx, pool.ID)
			if err != nil {
				return fmt.Errorf("fetching test pool metrics: %w", err)
			}
			if metrics.Drained {
				break
			}
			debug.Printf("No work available, %d entries still leased by other workers", metrics.LeasedEntriesCount)
			if err := sleepContext(ctx, poolIdleInterval); err != nil {
				return err
			}
			continue
		}

		leaseCount++
		attemptsExecuted += len(lease.Attempts)

		testCases, err := testCasesFromLeaseAttempts(lease.Attempts, testRunner.LocationPrefix())
		if err != nil {
			return err
		}

		fmt.Printf("Leased %d attempts (lease %s)\n", len(lease.Attempts), lease.ID)

		stopHeartbeat := startLeaseHeartbeat(ctx, schedulerClient, pool.ID, lease.ID)

		var timeline []api.Timeline
		runResult, runErr := runTestsWithRetry(ctx, apiClient, cfg, testRunner, &testCases, cfg.MaxRetries, nil, &timeline, cfg.RetryForMutedTest, false)

		stopHeartbeat()

		// Abort immediately and propagate the error if the process was terminated by a signal,
		// since the test results may be unreliable and cannot be trusted.
		if ProcessSignaledError := new(runner.ProcessSignaledError); errors.As(runErr, &ProcessSignaledError) {
			logSignalAndExit(testRunner.Name(), ProcessSignaledError.Signal)
		}

		printReport(runResult, nil, testRunner.Name())

		if exitError := new(exec.ExitError); errors.As(runErr, &exitError) {
			// Mirror the static path: an exit code 1 alongside only muted
			// failures is treated as a pass.
			if !(exitError.ExitCode() == 1 && runResult.OnlyMutedFailures()) && failureErr == nil {
				failureErr = fmt.Errorf("%s exited with error: %w", testRunner.Name(), runErr)
			}
		} else if runErr != nil {
			// A non-exit error (e.g. the runner failed to start) is not a test
			// failure; abort instead of marking entries completed.
			return runErr
		}

		if _, err := schedulerClient.CompleteSchedulerLeases(ctx, pool.ID, api.CompleteSchedulerLeasesParams{
			Leases: []api.SchedulerLeaseRef{{ID: lease.ID}},
		}); err != nil {
			// The lease may have expired (e.g. the batch ran longer than the
			// heartbeats kept it alive); its entries will be re-leased by
			// another worker, so log and continue.
			fmt.Printf("⚠️ Failed to complete lease %s: %v\n", lease.ID, err)
			completeFailures++
		}
	}

	fmt.Printf("Pool drained: executed %d attempts across %d leases\n", attemptsExecuted, leaseCount)
	if completeFailures > 0 {
		fmt.Printf("⚠️ %d lease(s) could not be completed; their entries may have been re-run by other workers\n", completeFailures)
	}

	return failureErr
}

// resolveSchedulerPool looks up the pool created by the plan step, polling
// until it appears. The run step can start before the plan step has created
// the pool, so not finding it immediately is expected.
func resolveSchedulerPool(ctx context.Context, client *api.Client, claims api.OIDCClaims, key string) (api.SchedulerPool, error) {
	params := api.FetchSchedulerPoolsParams{
		PipelineID: claims.PipelineID,
		BuildID:    claims.BuildID,
		Key:        key,
	}

	deadline := time.Now().Add(poolResolveTimeout)
	for {
		pools, err := client.FetchSchedulerPools(ctx, params)
		if err != nil {
			return api.SchedulerPool{}, fmt.Errorf("fetching test pools: %w", err)
		}
		if len(pools) > 0 {
			return pools[0], nil
		}

		if time.Now().After(deadline) {
			return api.SchedulerPool{}, fmt.Errorf("timed out after %s waiting for test pool %q; ensure `bktec plan --test-scheduler --pool %s` runs in this build", poolResolveTimeout, key, key)
		}

		debug.Printf("Test pool %q not found yet, retrying in %s", key, poolResolveInterval)
		if err := sleepContext(ctx, poolResolveInterval); err != nil {
			return api.SchedulerPool{}, err
		}
	}
}

// testCasesFromLeaseAttempts converts leased attempts into test cases for the
// test runner. Paths include the location prefix sent at plan creation, which
// is trimmed before running, mirroring the static-split path.
func testCasesFromLeaseAttempts(attempts []api.SchedulerLeaseAttempt, locationPrefix string) ([]plan.TestCase, error) {
	testCases := make([]plan.TestCase, 0, len(attempts))
	for _, attempt := range attempts {
		switch attempt.SelectorType {
		case "file", "example":
			var selector struct {
				Path string `json:"path"`
			}
			if err := json.Unmarshal(attempt.Selector, &selector); err != nil {
				return nil, fmt.Errorf("parsing selector of test pool attempt %s: %w", attempt.ID, err)
			}
			if selector.Path == "" {
				return nil, fmt.Errorf("test pool attempt %s has no path in its selector", attempt.ID)
			}

			path := selector.Path
			if locationPrefix != "" {
				trimmed, err := trimFilePathPrefix(path, locationPrefix)
				if err != nil {
					return nil, fmt.Errorf("failed to trim path prefix: %w", err)
				}
				path = trimmed
			}

			testCases = append(testCases, plan.TestCase{Path: path})
		default:
			return nil, fmt.Errorf("unsupported test pool attempt selector type %q (attempt %s); upgrade bktec to a version that supports it", attempt.SelectorType, attempt.ID)
		}
	}
	return testCases, nil
}

// startLeaseHeartbeat renews the lease in the background while a batch is
// running. Heartbeat errors are logged and ignored; if the lease expires the
// server re-leases its entries to another worker and the complete call
// surfaces a conflict.
func startLeaseHeartbeat(ctx context.Context, client *api.Client, poolID string, leaseID string) (stop func()) {
	heartbeatCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})

	go func() {
		defer close(done)
		ticker := time.NewTicker(leaseHeartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeatCtx.Done():
				return
			case <-ticker.C:
				_, err := client.HeartbeatSchedulerLeases(heartbeatCtx, poolID, api.HeartbeatSchedulerLeasesParams{
					LeaseIDs:        []string{leaseID},
					LeaseTTLSeconds: leaseTTLSeconds,
				})
				if err != nil && !errors.Is(err, context.Canceled) {
					fmt.Printf("⚠️ Failed to heartbeat lease %s: %v\n", leaseID, err)
				}
			}
		}
	}()

	return func() {
		cancel()
		<-done
	}
}

// sleepContext sleeps for the given duration, returning early with the
// context's error if it is cancelled.
func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
