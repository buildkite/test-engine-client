package command

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"time"

	"github.com/buildkite/test-engine-client/v2/internal/api"
	"github.com/buildkite/test-engine-client/v2/internal/config"
	"github.com/buildkite/test-engine-client/v2/internal/debug"
	"github.com/buildkite/test-engine-client/v2/internal/plan"
	"github.com/buildkite/test-engine-client/v2/internal/runner"
	"github.com/buildkite/test-engine-client/v2/internal/version"
	"github.com/olekukonko/tablewriter"
)

const Logo = `
______ ______ _____
___  /____  /___  /____________
__  __ \_  //_/  __/  _ \  ___/
_  /_/ /  ,<  / /_ /  __/ /__
/_.___//_/|_| \__/ \___/\___/
`

func Run(ctx context.Context, cfg *config.Config, testListFilename string) error {
	printStartUpMessage()

	testRunner, err := runner.DetectRunner(cfg)
	if err != nil {
		return fmt.Errorf("unsupported value for BUILDKITE_TEST_ENGINE_TEST_RUNNER: %w", err)
	}

	files, err := getTestFiles(testListFilename, testRunner)
	if err != nil {
		return err
	}

	// get plan
	apiClient := api.NewClient(api.ClientConfig{
		ServerBaseURL:    cfg.ServerBaseURL,
		UploadBaseURL:    cfg.UploadBaseURL,
		AccessToken:      cfg.AccessToken,
		OrganizationSlug: cfg.OrganizationSlug,
	})

	testPlan, err := fetchOrCreateTestPlan(ctx, apiClient, cfg, files, testRunner)
	if err != nil {
		return err
	}

	debug.Printf("My favourite ice cream is %s", testPlan.Experiment)

	plan.PrintSplitSummary(os.Stdout, testPlan)

	// get plan for this node
	thisNodeTask := testPlan.Tasks[strconv.Itoa(cfg.NodeIndex)]

	// When a runner can mute tests but cannot skip them, fall back to muting the
	// tests Test Engine wanted to skip.
	//
	// Test Engine excludes skipped tests from a node's task list, but runners that
	// split by file (e.g. Jest) still receive the whole file and run every test in
	// it, including the ones marked for skipping. Such runners have no way to skip
	// an individual test, so the skipped test executes anyway and its failure would
	// fail the build. Treating it as muted lets it run while suppressing its
	// failure. Muting is matched by scope and name, so the skipped test is muted
	// regardless of which file or node it belongs to.
	features := testRunner.SupportedFeatures()
	if !features.Skip && features.Mute && len(testPlan.SkippedTests) > 0 {
		testPlan.MutedTests = append(testPlan.MutedTests, testPlan.SkippedTests...)
		testPlan.SkippedTests = nil
	}

	// File paths sent to the API for test plan creation include the location prefix to match Test Engine records.
	// However, the test runner expects file paths without the prefix, so we need to remove it before running the tests.
	locationPrefix := testRunner.LocationPrefix()
	if locationPrefix != "" && !testPlan.Fallback {
		for i, test := range thisNodeTask.Tests {
			path, err := trimFilePathPrefix(test.Path, locationPrefix)
			if err != nil {
				return fmt.Errorf("failed to trim path prefix: %w", err)
			}
			thisNodeTask.Tests[i].Path = path
		}
	}

	// execute tests
	var timeline []api.Timeline
	runResult, runErr := runTestsWithRetry(ctx, apiClient, cfg, testRunner, &thisNodeTask.Tests, cfg.MaxRetries, testPlan.MutedTests, &timeline, cfg.RetryForMutedTest, cfg.FailOnNoTests)

	// Abort immediately and propagate the error if the process was terminated by a signal,
	// since the test results may be unreliable and cannot be trusted.
	if ProcessSignaledError := new(runner.ProcessSignaledError); errors.As(runErr, &ProcessSignaledError) {
		logSignalAndExit(testRunner.Name(), ProcessSignaledError.Signal)
	}

	printReport(runResult, testPlan.SkippedTests, testRunner.Name())
	if !testPlan.Fallback {
		sendMetadata(ctx, apiClient, cfg, timeline, runResult.Statistics())
	}

	if exitError := new(exec.ExitError); errors.As(runErr, &exitError) {
		// We can't definitively confirm the non-zero exit was caused by muted test failures,
		// since runners like rspec or jest can exit with code 1 for non-test-failure reasons too.
		// However, checking for exit code 1 alongside a passing report is a best-effort approximation
		// to reduce the risk of incorrectly suppressing a real error.
		if exitError.ExitCode() == 1 && runResult.OnlyMutedFailures() {
			return nil
		}
		return fmt.Errorf("%s exited with error: %w", testRunner.Name(), runErr)
	}

	return runErr
}

func printStartUpMessage() {
	const green = "\033[32m"
	const reset = "\033[0m"
	fmt.Println("+++ Buildkite Test Engine Client: bktec " + version.Version + "\n")
	fmt.Println(green + Logo + reset)
}

func printReport(runResult runner.RunResult, testsSkippedByTestEngine []plan.TestCase, runnerName string) {
	status := runResult.Status()
	if status == runner.RunStatusUnknown {
		return
	}

	fmt.Println("+++ ========== Buildkite Test Engine Report  ==========")

	switch status {
	case runner.RunStatusPassed:
		if len(runResult.FailedMutedTests()) > 0 {
			fmt.Println("✅ Build passed. Some muted tests failed.")
		} else {
			fmt.Println("✅ All tests passed.")
		}
	case runner.RunStatusFailed:
		fmt.Println("❌ Some tests failed.")
	case runner.RunStatusError:
		fmt.Printf("🚨 %s\n", runResult.Error())
	}

	// Print statistics
	statistics := runResult.Statistics()
	if statistics.Total > 0 {
		fmt.Println("")
		data := [][]string{
			{"Passed", "first run", strconv.Itoa(statistics.PassedOnFirstRun)},
			{"Passed", "on retry", strconv.Itoa(statistics.PassedOnRetry)},
			{"Muted", "passed", strconv.Itoa(statistics.MutedPassed)},
			{"Muted", "failed", strconv.Itoa(statistics.MutedFailed)},
			{"Failed", "", strconv.Itoa(statistics.Failed)},
			{"Skipped", "", strconv.Itoa(statistics.Skipped)},
		}
		table := tablewriter.NewWriter(os.Stdout)
		table.AppendBulk(data)
		table.SetFooter([]string{"", "Total", strconv.Itoa(statistics.Total)})
		table.SetFooterAlignment(tablewriter.ALIGN_RIGHT)
		table.SetAutoMergeCellsByColumnIndex([]int{0, 1})
		table.SetRowLine(true)
		table.Render()

		// Print muted and failed tests
		mutedTests := runResult.MutedTests()
		if len(mutedTests) > 0 {
			fmt.Println("")
			fmt.Println("+++ Muted Tests:")
			for _, mutedTest := range runResult.MutedTests() {
				fmt.Printf("- %s %s (%s)\n", mutedTest.Scope, mutedTest.Name, mutedTest.Status)
			}
		}

		failedTests := runResult.FailedTests()
		if len(failedTests) > 0 {
			fmt.Println("")
			fmt.Println("+++ Failed Tests:")
			for _, failedTests := range runResult.FailedTests() {
				fmt.Printf("- %s %s\n", failedTests.Scope, failedTests.Name)
			}
		}

		testsSkippedByRunner := runResult.SkippedTests()
		if len(testsSkippedByRunner) > 0 {
			fmt.Println("")
			fmt.Printf("+++ Skipped by %s:\n", runnerName)
			for _, skippedTest := range testsSkippedByRunner {
				fmt.Printf("- %s %s\n", skippedTest.Scope, skippedTest.Name)
			}
		}

		if len(testsSkippedByTestEngine) > 0 {
			fmt.Println("")
			fmt.Println("+++ Skipped by Test Engine:")
			for _, skippedTest := range testsSkippedByTestEngine {
				fmt.Printf("- %s %s\n", skippedTest.Scope, skippedTest.Name)
			}
		}
	}
	fmt.Println("===================================================")
}

func createTimestamp() string {
	return time.Now().Format(time.RFC3339Nano)
}

func uploadResults(ctx context.Context, apiClient *api.Client, cfg *config.Config, testRunner runner.TestRunner) {
	if !cfg.UploadResults || cfg.UploadToken == "" {
		return
	}
	format := testRunner.ResultFormat()
	if format == "" {
		return
	}
	if _, err := os.Stat(testRunner.ResultFilePath()); err != nil {
		return
	}
	fmt.Println("Buildkite Test Engine Client: Uploading test results to Test Engine")
	if err := apiClient.UploadTestResults(ctx, cfg.UploadToken, testRunner.ResultFilePath(), format, testRunner.LocationPrefix()); err != nil {
		fmt.Printf("Buildkite Test Engine Client: Failed to upload test results to Test Engine: %v\n", err)
	}
}

func sendMetadata(ctx context.Context, apiClient *api.Client, cfg *config.Config, timeline []api.Timeline, statistics runner.RunStatistics) {
	err := apiClient.PostTestPlanMetadata(ctx, cfg.SuiteSlug, cfg.Identifier, api.TestPlanMetadataParams{
		Timeline:   timeline,
		Env:        cfg,
		Version:    version.Version,
		Statistics: statistics,
	})

	// Error is suppressed because we don't want to fail the build if we can't send metadata.
	if err != nil {
		fmt.Printf("Failed to send metadata to Test Engine: %v\n", err)
	}
}

// runTestsWithRetry can be considered the core of bktec.
// It invoke testRunner, orchestrate retries.
// It returns RunResult.
//
// For next reader, there is a small caveat with current implementation:
// - testCases and timeline are both expected to be mutated.
// - testCases in this case serve both as input and output -> we should probably change it.
func runTestsWithRetry(ctx context.Context, apiClient *api.Client, cfg *config.Config, testRunner runner.TestRunner, testsCases *[]plan.TestCase, maxRetries int, mutedTests []plan.TestCase, timeline *[]api.Timeline, retryForMutedTest bool, failOnNoTests bool) (runner.RunResult, error) {
	attemptCount := 0

	// Create a new run result with muted tests to keep track of the results.
	runResult := runner.NewRunResult(mutedTests)

	// If there are no test cases to run, skip invoking the test runner
	if len(*testsCases) == 0 {
		if failOnNoTests {
			return *runResult, fmt.Errorf("no tests assigned to this node")
		}
		fmt.Printf("+++ Buildkite Test Engine Client: No tests to run on this node\n")
		return *runResult, nil
	}

	if cfg.UploadResults && cfg.UploadToken == "" {
		fmt.Println("Buildkite Test Engine Client: Warning: upload-results is enabled but no upload token was provided. Test results will not be uploaded.")
	}

	for attemptCount <= maxRetries {
		if attemptCount == 0 {
			fmt.Printf("+++ Buildkite Test Engine Client: Running tests\n")
			*timeline = append(*timeline, api.Timeline{
				Event:     "test_start",
				Timestamp: createTimestamp(),
			})
		} else {
			fmt.Printf("+++ Buildkite Test Engine Client: ♻️ Attempt %d of %d to retry failing tests\n", attemptCount, maxRetries)
			*timeline = append(*timeline, api.Timeline{
				Event:     fmt.Sprintf("retry_%d_start", attemptCount),
				Timestamp: createTimestamp(),
			})
		}

		err := testRunner.Run(runResult, *testsCases, attemptCount > 0)

		uploadResults(ctx, apiClient, cfg, testRunner)

		if attemptCount == 0 {
			*timeline = append(*timeline, api.Timeline{
				Event:     "test_end",
				Timestamp: createTimestamp(),
			})
		} else {
			*timeline = append(*timeline, api.Timeline{
				Event:     fmt.Sprintf("retry_%d_end", attemptCount),
				Timestamp: createTimestamp(),
			})
		}

		// Don't retry if we've reached max retries.
		if attemptCount == maxRetries {
			return *runResult, err
		}

		failedTests := runResult.FailedTests()
		failedMutedTests := runResult.FailedMutedTests()

		shouldRetryForHardFailedTests := len(failedTests) > 0
		shouldRetryForMutedTests := retryForMutedTest && len(failedMutedTests) > 0
		shouldRetry := shouldRetryForHardFailedTests || shouldRetryForMutedTests

		if shouldRetry {
			*testsCases = failedTests

			if shouldRetryForMutedTests {
				*testsCases = append(*testsCases, failedMutedTests...)
			}

			attemptCount++
		} else {
			return *runResult, err
		}
	}

	return *runResult, nil
}

func logSignalAndExit(name string, signal syscall.Signal) {
	fmt.Printf("Buildkite Test Engine Client: %s was terminated with signal: %v\n", name, signal)

	// Exit with 128 + signal number, the standard convention.
	exitCode := 128 + int(signal)
	os.Exit(exitCode)
}

// fetchOrCreateTestPlan fetches a test plan from the server, or creates a
// fallback plan if the server is unavailable or returns an error plan.
func fetchOrCreateTestPlan(ctx context.Context, apiClient *api.Client, cfg *config.Config, files []string, testRunner runner.TestRunner) (plan.TestPlan, error) {
	debug.Println("Fetching test plan")

	// Fetch the plan from the server's cache.
	cachedPlan, err := apiClient.FetchTestPlan(ctx, cfg.SuiteSlug, cfg.Identifier, cfg.JobRetryCount)

	if err != nil {
		if handledErr := handleError(err); handledErr != nil {
			return plan.TestPlan{}, handledErr
		}
		return plan.CreateFallbackPlan(files, cfg.Parallelism), nil
	}

	if cachedPlan != nil {
		// The server can return an "error" plan indicated by an empty task list (i.e. `{"tasks": {}}`).
		// In this case, we should create a fallback plan.
		if len(cachedPlan.Tasks) == 0 {
			warnErrorPlan()
			return plan.CreateFallbackPlan(files, cfg.Parallelism), nil
		}

		debug.Printf("Test plan found. Identifier: %q", cfg.Identifier)
		return *cachedPlan, nil
	}

	debug.Println("No test plan found, creating a new plan")
	// If the cache is empty, create a new plan.
	params, err := createRequestParam(ctx, cfg, files, *apiClient, testRunner)
	if err != nil {
		if handledErr := handleError(err); handledErr != nil {
			return plan.TestPlan{}, handledErr
		}
		return plan.CreateFallbackPlan(files, cfg.Parallelism), nil
	}

	debug.Println("Creating test plan")
	testPlan, err := apiClient.CreateTestPlan(ctx, cfg.SuiteSlug, params)

	if err != nil {
		if handledErr := handleError(err); handledErr != nil {
			return plan.TestPlan{}, handledErr
		}
		return plan.CreateFallbackPlan(files, cfg.Parallelism), nil
	}

	// The server can return an "error" plan indicated by an empty task list (i.e. `{"tasks": {}}`).
	// In this case, we should create a fallback plan.
	if len(testPlan.Tasks) == 0 {
		warnErrorPlan()
		return plan.CreateFallbackPlan(files, cfg.Parallelism), nil
	}

	debug.Printf("Test plan created. Identifier: %q", cfg.Identifier)
	return testPlan, nil
}
