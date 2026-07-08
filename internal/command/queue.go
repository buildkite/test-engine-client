package command

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/buildkite/test-engine-client/v2/internal/api"
	"github.com/buildkite/test-engine-client/v2/internal/config"
	"github.com/buildkite/test-engine-client/v2/internal/plan"
	"github.com/buildkite/test-engine-client/v2/internal/runner"
)

const testSchedulerEntryChunkSize = 100

type queueAttempt struct {
	LeaseID   string
	AttemptID string
	TestCase  plan.TestCase
}

func planQueue(ctx context.Context, cfg *config.Config, testTargets []string, apiClient *api.Client, testRunner runner.TestRunner, outputFormat PlanOutput, template string) error {
	params, err := createRequestParam(ctx, cfg, testTargets, *apiClient, testRunner)
	if err != nil {
		return err
	}

	testCases := testCasesFromPlanParams(params.Tests)
	if len(testCases) == 0 {
		fmt.Fprintln(os.Stderr, "⚠️ No tests discovered; no Test Scheduler entries were uploaded.")
	}

	pool, err := createOrVerifySchedulerPool(ctx, cfg, apiClient)
	if err != nil {
		return err
	}

	if err := uploadSchedulerEntries(ctx, apiClient, pool.ID, testCases); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "Buildkite Test Engine Client: uploaded %d Test Scheduler queue entries to pool %s\n", len(testCases), pool.ID)

	parallelism := cfg.MaxParallelism
	if parallelism == 0 {
		parallelism = cfg.Parallelism
	}
	if parallelism == 0 {
		parallelism = 1
	}

	summary := map[string]string{
		"BUILDKITE_TEST_ENGINE_QUEUE":             "true",
		"BUILDKITE_TEST_ENGINE_TEST_POOL_ID":      pool.ID,
		"BUILDKITE_TEST_ENGINE_QUEUE_KEY":         cfg.QueueKey,
		"BUILDKITE_TEST_ENGINE_LEASE_TTL_SECONDS": fmt.Sprintf("%d", cfg.LeaseTTLSeconds),
		"BUILDKITE_TEST_ENGINE_TARGET_COST_LIMIT": fmt.Sprintf("%g", cfg.TargetCostLimit),
		"BUILDKITE_TEST_ENGINE_PLAN_IDENTIFIER":   cfg.Identifier,
		"BUILDKITE_TEST_ENGINE_PARALLELISM":       fmt.Sprintf("%d", parallelism),
	}

	switch outputFormat {
	case PlanOutputJSON:
		return writeJSON(planWriter, summary)
	case PlanOutputPlanOut:
		out, closeOut, err := planOutWriter(cfg.PlanOut)
		if err != nil {
			return err
		}
		defer closeOut()
		return writeJSON(out, summary)
	case PlanOutputPipelineUpload:
		cmd := makePipelineUploadCommand(template)
		env := os.Environ()
		for key, value := range summary {
			env = append(env, fmt.Sprintf("%s=%s", key, value))
		}
		cmd.Env = env
		fmt.Fprintf(planWriter, "Executing buildkite-agent pipeline upload with BUILDKITE_TEST_ENGINE_QUEUE=true BUILDKITE_TEST_ENGINE_TEST_POOL_ID=%s BUILDKITE_TEST_ENGINE_PARALLELISM=%d\n", pool.ID, parallelism)
		return cmd.Run()
	default:
		return fmt.Errorf("unknown plan format %v", outputFormat)
	}
}

func writeJSON(w interface{ Write([]byte) (int, error) }, v any) error {
	encoded, err := jsonMarshal(v)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(encoded))
	return err
}

var jsonMarshal = func(v any) ([]byte, error) {
	return json.Marshal(v)
}

func createOrVerifySchedulerPool(ctx context.Context, cfg *config.Config, apiClient *api.Client) (api.TestSchedulerPool, error) {
	pool, err := apiClient.CreateTestSchedulerPool(ctx, api.CreateTestSchedulerPoolParams{
		Suite:      cfg.SuiteSlug,
		Pipeline:   cfg.PipelineSlug,
		BuildID:    cfg.BuildID,
		Key:        cfg.QueueKey,
		TTLSeconds: cfg.TestPoolTTLSeconds,
	})
	if err == nil {
		return pool, nil
	}

	var conflict *api.ConflictError
	if !errors.As(err, &conflict) {
		return api.TestSchedulerPool{}, err
	}

	if cfg.TestPoolID == "" {
		return api.TestSchedulerPool{}, fmt.Errorf("A pool already exists for this build/key, but the current API cannot look it up by key. Rerun with a pool ID or use a unique key")
	}

	pool, err = apiClient.GetTestSchedulerPool(ctx, cfg.TestPoolID)
	if err != nil {
		return api.TestSchedulerPool{}, fmt.Errorf("verifying existing Test Scheduler pool %s: %w", cfg.TestPoolID, err)
	}
	if pool.BuildID != cfg.BuildID || pool.Key != cfg.QueueKey {
		return api.TestSchedulerPool{}, fmt.Errorf("existing Test Scheduler pool %s does not match build/key (got build_id=%q key=%q, want build_id=%q key=%q)", pool.ID, pool.BuildID, pool.Key, cfg.BuildID, cfg.QueueKey)
	}
	return pool, nil
}

func uploadSchedulerEntries(ctx context.Context, apiClient *api.Client, poolID string, testCases []plan.TestCase) error {
	for start := 0; start < len(testCases); start += testSchedulerEntryChunkSize {
		end := start + testSchedulerEntryChunkSize
		if end > len(testCases) {
			end = len(testCases)
		}

		entries := make([]api.TestSchedulerEntryParams, 0, end-start)
		for _, testCase := range testCases[start:end] {
			entries = append(entries, schedulerEntryFromTestCase(testCase))
		}

		if _, err := apiClient.CreateTestSchedulerEntries(ctx, poolID, entries); err != nil {
			return fmt.Errorf("uploading Test Scheduler entries %d-%d: %w", start+1, end, err)
		}
	}
	return nil
}

func testCasesFromPlanParams(params api.TestPlanParamsTest) []plan.TestCase {
	testCases := make([]plan.TestCase, 0, len(params.Files)+len(params.Examples)+len(params.Selectors))
	for _, testCase := range params.Files {
		testCases = append(testCases, normalizeTestCaseFormat(testCase))
	}
	for _, testCase := range params.Examples {
		testCases = append(testCases, normalizeTestCaseFormat(testCase))
	}
	for _, selector := range params.Selectors {
		testCases = append(testCases, plan.TestCase{Value: selector.Value, Format: plan.TestCaseFormatSelector})
	}
	return testCases
}

func normalizeTestCaseFormat(testCase plan.TestCase) plan.TestCase {
	if testCase.Format == "" {
		testCase.Format = plan.TestCaseFormatFile
	}
	return testCase
}

func schedulerEntryFromTestCase(testCase plan.TestCase) api.TestSchedulerEntryParams {
	testCase = normalizeTestCaseFormat(testCase)
	return api.TestSchedulerEntryParams{
		SelectorType: "custom",
		Selector: api.TestSchedulerEntrySelector{
			Format:            testCase.Format,
			Path:              testCase.Path,
			Value:             testCase.Value,
			Identifier:        testCase.Identifier,
			Name:              testCase.Name,
			Scope:             testCase.Scope,
			EstimatedDuration: testCase.EstimatedDuration,
			TimingSampleSize:  testCase.TimingSampleSize,
		},
		Costs: map[string]float64{"custom": 1},
	}
}

func runQueue(ctx context.Context, cfg *config.Config, apiClient *api.Client, testRunner runner.TestRunner) error {
	var firstRunErr error
	var firstCompletionErr error

	for {
		lease, err := apiClient.CreateTestSchedulerLease(ctx, cfg.TestPoolID, api.CreateTestSchedulerLeaseParams{
			TargetCostLimit: map[string]float64{"custom": cfg.TargetCostLimit},
			LeaseTTLSeconds: cfg.LeaseTTLSeconds,
		})
		if err != nil {
			return fmt.Errorf("leasing Test Scheduler attempts: %w", err)
		}
		if lease == nil {
			break
		}

		attempts, testCases, err := queueAttemptsFromLease(lease, testRunner.LocationPrefix())
		if err != nil {
			return err
		}
		printLeasedQueueAttempts(lease.ID, attempts)

		var timeline []api.Timeline
		runCases := append([]plan.TestCase(nil), testCases...)
		runResult, runErr := runTestsWithRetry(ctx, apiClient, cfg, testRunner, &runCases, cfg.MaxRetries, nil, &timeline, cfg.RetryForMutedTest, true)

		completion := completionParamsForAttempts(attempts, runResult)
		if _, err := apiClient.CompleteTestSchedulerLeases(ctx, cfg.TestPoolID, completion); err != nil {
			firstCompletionErr = err
			break
		}
		printCompletedQueueAttempts(attempts, runResult)

		printReport(runResult, nil, testRunner.Name())

		if firstRunErr == nil && runErr != nil {
			firstRunErr = runErr
		}
	}

	if firstCompletionErr != nil {
		return fmt.Errorf("completing Test Scheduler lease: %w", firstCompletionErr)
	}
	if firstRunErr != nil {
		if exitError := new(exec.ExitError); errors.As(firstRunErr, &exitError) {
			return fmt.Errorf("%s exited with error: %w", testRunner.Name(), firstRunErr)
		}
	}
	return firstRunErr
}

func printLeasedQueueAttempts(leaseID string, attempts []queueAttempt) {
	fmt.Printf("+++ Buildkite Test Engine Client: Leased %d Test Scheduler spec(s) from lease %s\n", len(attempts), leaseID)
	for _, attempt := range attempts {
		fmt.Printf("Buildkite Test Engine Client: leased %s (attempt %s)\n", queueTestCaseLabel(attempt.TestCase), attempt.AttemptID)
	}
}

func printCompletedQueueAttempts(attempts []queueAttempt, runResult runner.RunResult) {
	if len(attempts) == 0 {
		return
	}
	result := schedulerResultForRun(runResult)
	fmt.Printf("+++ Buildkite Test Engine Client: Completed %d Test Scheduler spec(s) for lease %s\n", len(attempts), attempts[0].LeaseID)
	for _, attempt := range attempts {
		fmt.Printf("Buildkite Test Engine Client: completed %s as %s (attempt %s)\n", queueTestCaseLabel(attempt.TestCase), result, attempt.AttemptID)
	}
}

func queueTestCaseLabel(testCase plan.TestCase) string {
	if testCase.Path != "" && testCase.Name != "" {
		return fmt.Sprintf("%s (%s)", testCase.Path, testCase.Name)
	}
	if testCase.Path != "" {
		return testCase.Path
	}
	if testCase.Value != "" {
		return testCase.Value
	}
	if testCase.Identifier != "" {
		return testCase.Identifier
	}
	return "<unknown>"
}

func queueAttemptsFromLease(lease *api.TestSchedulerLease, locationPrefix string) ([]queueAttempt, []plan.TestCase, error) {
	attempts := make([]queueAttempt, 0, len(lease.Attempts))
	testCases := make([]plan.TestCase, 0, len(lease.Attempts))
	for _, attempt := range lease.Attempts {
		if attempt.SelectorType != "custom" {
			return nil, nil, fmt.Errorf("unsupported Test Scheduler selector_type %q", attempt.SelectorType)
		}
		testCase := testCaseFromSchedulerSelector(attempt.Selector)
		if locationPrefix != "" && testCase.Format != plan.TestCaseFormatSelector {
			path, err := trimFilePathPrefix(testCase.Path, locationPrefix)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to trim path prefix: %w", err)
			}
			testCase.Path = path
		}
		attempts = append(attempts, queueAttempt{LeaseID: lease.ID, AttemptID: attempt.ID, TestCase: testCase})
		testCases = append(testCases, testCase)
	}
	return attempts, testCases, nil
}

func testCaseFromSchedulerSelector(selector api.TestSchedulerEntrySelector) plan.TestCase {
	return normalizeTestCaseFormat(plan.TestCase{
		Format:            selector.Format,
		Path:              selector.Path,
		Value:             selector.Value,
		Identifier:        selector.Identifier,
		Name:              selector.Name,
		Scope:             selector.Scope,
		EstimatedDuration: selector.EstimatedDuration,
		TimingSampleSize:  selector.TimingSampleSize,
	})
}

func completionParamsForAttempts(attempts []queueAttempt, runResult runner.RunResult) api.CompleteTestSchedulerLeasesParams {
	completionAttempts := make([]api.CompleteTestSchedulerAttemptParams, 0, len(attempts))
	result := schedulerResultForRun(runResult)
	for _, attempt := range attempts {
		completionAttempts = append(completionAttempts, api.CompleteTestSchedulerAttemptParams{
			AttemptID: attempt.AttemptID,
			Result:    result,
		})
	}
	leaseID := ""
	if len(attempts) > 0 {
		leaseID = attempts[0].LeaseID
	}
	return api.CompleteTestSchedulerLeasesParams{
		Leases: []api.CompleteTestSchedulerLeaseParams{{
			LeaseID:  leaseID,
			Attempts: completionAttempts,
		}},
	}
}

func schedulerResultForRun(runResult runner.RunResult) string {
	switch runResult.Status() {
	case runner.RunStatusPassed:
		return "passed"
	case runner.RunStatusFailed:
		return "failed"
	default:
		return "errored"
	}
}
