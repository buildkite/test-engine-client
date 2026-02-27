package command

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/buildkite/test-engine-client/internal/api"
	"github.com/buildkite/test-engine-client/internal/config"
	"github.com/buildkite/test-engine-client/internal/debug"
	"github.com/buildkite/test-engine-client/internal/plan"
	"github.com/buildkite/test-engine-client/internal/runner"
	"github.com/buildkite/test-engine-client/internal/version"
	"github.com/olekukonko/tablewriter"
)

type TestRunner interface {
	// Run takes testCases as input, executes the test against the test cases, and mutates the runner.RunResult with the test results.
	Run(result *runner.RunResult, testCases []plan.TestCase, retry bool) error
	// GetExamples discovers all tests within given files.
	// This function is only used for split by example use case. Currently only supported by RSpec.
	GetExamples(files []string) ([]plan.TestCase, error)
	// GetFiles discover all test files that the runner should execute.
	// This is sent to server-side when creating test plan.
	// This is also used to obtain a fallback non-intelligent test splitting mechanism.
	GetFiles() ([]string, error)
	Name() string
	GetLocationPrefix() string
}

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
		ServerBaseUrl:    cfg.ServerBaseUrl,
		AccessToken:      cfg.AccessToken,
		OrganizationSlug: cfg.OrganizationSlug,
	})

	testPlan, err := fetchOrCreateTestPlan(ctx, apiClient, cfg, files, testRunner)
	if err != nil {
		return fmt.Errorf("couldn't fetch or create test plan: %w", err)
	}

	debug.Printf("My favourite ice cream is %s", testPlan.Experiment)

	// get plan for this node
	thisNodeTask := testPlan.Tasks[strconv.Itoa(cfg.NodeIndex)]

	locationPrefix := testRunner.GetLocationPrefix()
	if locationPrefix != "" {
		for i, test := range thisNodeTask.Tests {
			if test.Format == plan.TestCaseFormatFile {
				relPath, err := filepath.Rel(locationPrefix, test.Path)
				if err != nil {
					debug.Printf("Failed to get relative path for %s with location prefix %s: %v. Using original path.", test.Path, locationPrefix, err)
				} else {
					thisNodeTask.Tests[i].Path = relPath
				}
			}
		}
	}

	// execute tests
	var timeline []api.Timeline
	runResult, err := runTestsWithRetry(testRunner, &thisNodeTask.Tests, cfg.MaxRetries, testPlan.MutedTests, &timeline, cfg.RetryForMutedTest, cfg.FailOnNoTests)

	// Handle errors that prevent the runner from finishing.
	// By finishing, it means that the runner has completed with a readable result.
	if err != nil {
		// runner terminated by signal: exit with 128 + signal number
		if ProcessSignaledError := new(runner.ProcessSignaledError); errors.As(err, &ProcessSignaledError) {
			logSignalAndExit(testRunner.Name(), ProcessSignaledError.Signal)
		}

		// runner exited with error: exit with the exit code
		if exitError := new(exec.ExitError); errors.As(err, &exitError) {
			return fmt.Errorf("%s exited with error: %w", testRunner.Name(), err)
		}

		return err
	}

	// At this point, the runner is expected to have completed

	if !testPlan.Fallback {
		sendMetadata(ctx, apiClient, cfg, timeline, runResult.Statistics())
	}

	printReport(runResult, testPlan.SkippedTests, testRunner.Name())

	if runResult.Status() == runner.RunStatusFailed || runResult.Status() == runner.RunStatusError {
		os.Exit(1)
	}
	return nil
}

func printStartUpMessage() {
	const green = "\033[32m"
	const reset = "\033[0m"
	fmt.Println("+++ Buildkite Test Engine Client: bktec " + version.Version + "\n")
	fmt.Println(green + Logo + reset)
}

func printReport(runResult runner.RunResult, testsSkippedByTestEngine []plan.TestCase, runnerName string) {
	fmt.Println("+++ ========== Buildkite Test Engine Report  ==========")

	switch runResult.Status() {
	case runner.RunStatusPassed:
		fmt.Println("✅ All tests passed.")
	case runner.RunStatusFailed:
		fmt.Println("❌ Some tests failed.")
	case runner.RunStatusError:
		fmt.Printf("🚨 %s\n", runResult.Error())
	}
	fmt.Println("")

	// Print statistics
	statistics := runResult.Statistics()
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

	fmt.Println("===================================================")
}

func createTimestamp() string {
	return time.Now().Format(time.RFC3339Nano)
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
func runTestsWithRetry(testRunner TestRunner, testsCases *[]plan.TestCase, maxRetries int, mutedTests []plan.TestCase, timeline *[]api.Timeline, retryForMutedTest bool, failOnNoTests bool) (runner.RunResult, error) {
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

		// Don't retry if there is an error that is not a test failure.
		if err != nil {
			return *runResult, err
		}

		// Don't retry if we've reached max retries.
		if attemptCount == maxRetries {
			return *runResult, nil
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
			return *runResult, nil
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
func fetchOrCreateTestPlan(ctx context.Context, apiClient *api.Client, cfg *config.Config, files []string, testRunner TestRunner) (plan.TestPlan, error) {
	debug.Println("Fetching test plan")

	// Fetch the plan from the server's cache.
	cachedPlan, err := apiClient.FetchTestPlan(ctx, cfg.SuiteSlug, cfg.Identifier, cfg.JobRetryCount)

	handleError := func(err error) (plan.TestPlan, error) {
		if errors.Is(err, api.ErrRetryTimeout) {
			fmt.Println("⚠️ Could not fetch or create plan from server, falling back to non-intelligent splitting. Your build may take longer than usual.")
			p := plan.CreateFallbackPlan(files, cfg.Parallelism)
			return p, nil
		}

		if billingError := new(api.BillingError); errors.As(err, &billingError) {
			fmt.Println(billingError.Message)
			fmt.Println("⚠️ Falling back to non-intelligent splitting. Your build may take longer than usual.")
			p := plan.CreateFallbackPlan(files, cfg.Parallelism)
			return p, nil
		}

		return plan.TestPlan{}, err
	}

	if err != nil {
		return handleError(err)
	}

	if cachedPlan != nil {
		// The server can return an "error" plan indicated by an empty task list (i.e. `{"tasks": {}}`).
		// In this case, we should create a fallback plan.
		if len(cachedPlan.Tasks) == 0 {
			fmt.Println("⚠️ Error plan received, falling back to non-intelligent splitting. Your build may take longer than usual.")
			testPlan := plan.CreateFallbackPlan(files, cfg.Parallelism)
			return testPlan, nil
		}

		debug.Printf("Test plan found. Identifier: %q", cfg.Identifier)
		return *cachedPlan, nil
	}

	debug.Println("No test plan found, creating a new plan")
	// If the cache is empty, create a new plan.
	params, err := createRequestParam(ctx, cfg, files, *apiClient, testRunner)
	if err != nil {
		return handleError(err)
	}

	debug.Println("Creating test plan")
	testPlan, err := apiClient.CreateTestPlan(ctx, cfg.SuiteSlug, params)

	if err != nil {
		return handleError(err)
	}

	// The server can return an "error" plan indicated by an empty task list (i.e. `{"tasks": {}}`).
	// In this case, we should create a fallback plan.
	if len(testPlan.Tasks) == 0 {
		fmt.Println("⚠️ Error plan received, falling back to non-intelligent splitting. Your build may take longer than usual.")
		testPlan = plan.CreateFallbackPlan(files, cfg.Parallelism)
		return testPlan, nil
	}

	debug.Printf("Test plan created. Identifier: %q", cfg.Identifier)
	return testPlan, nil
}
