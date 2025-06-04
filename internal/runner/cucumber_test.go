package runner

import (
	"errors"
	"os"
	"sort"
	"testing"

	"github.com/buildkite/test-engine-client/internal/plan"
	"github.com/google/go-cmp/cmp"
	"github.com/kballard/go-shellquote"
)

func TestNewCucumber(t *testing.T) {
	cases := []struct {
		input RunnerConfig
		want  RunnerConfig
	}{
		// default
		{
			input: RunnerConfig{},
			want: RunnerConfig{
				TestCommand:            "cucumber --format pretty --format json --out {{resultPath}} {{testExamples}}",
				TestFilePattern:        "features/**/*.feature",
				TestFileExcludePattern: "",
				RetryTestCommand:       "cucumber --format pretty --format json --out {{resultPath}} {{testExamples}}",
			},
		},
		// custom
		{
			input: RunnerConfig{
				TestCommand:            "cucumber --format json --out {{resultPath}} {{testExamples}}",
				TestFilePattern:        "features/api/**/*.feature",
				TestFileExcludePattern: "features/experimental",
				RetryTestCommand:       "cucumber --format json --out {{resultPath}} {{testExamples}}",
			},
			want: RunnerConfig{
				TestCommand:            "cucumber --format json --out {{resultPath}} {{testExamples}}",
				TestFilePattern:        "features/api/**/*.feature",
				TestFileExcludePattern: "features/experimental",
				RetryTestCommand:       "cucumber --format json --out {{resultPath}} {{testExamples}}",
			},
		},
	}

	for _, c := range cases {
		got := NewCucumber(c.input)
		if diff := cmp.Diff(got.RunnerConfig, c.want); diff != "" {
			t.Errorf("NewCucumber(%v) diff (-got +want):\n%s", c.input, diff)
		}
	}
}
func TestCucumberRun(t *testing.T) {
	changeCwd(t, "./testdata/cucumber")

	cucumber := NewCucumber(RunnerConfig{
		TestCommand: "cucumber --format json --out {{resultPath}}",
		ResultPath:  "tmp/cucumber.json",
	})

	t.Cleanup(func() {
		os.RemoveAll("tmp") // Clean up the whole tmp directory
	})

	// Create the directory for the results
	if err := os.MkdirAll("tmp", 0755); err != nil {
		t.Fatalf("could not create tmp directory: %v", err)
	}

	testCases := []plan.TestCase{
		{Path: "./features/spells/expelliarmus.feature"},
	}
	result := NewRunResult([]plan.TestCase{})
	err := cucumber.Run(result, testCases, false)

	if err != nil {
		t.Errorf("Cucumber.Run(%q) error = %v", testCases, err)
	}

	if result.Status() != RunStatusPassed {
		// Attempt to read and log the content of cucumber.json for debugging
		jsonContent, readErr := os.ReadFile("tmp/cucumber.json")
		if readErr != nil {
			t.Logf("Failed to read tmp/cucumber.json: %v", readErr)
		} else {
			t.Logf("Content of tmp/cucumber.json:\n%s", string(jsonContent))
		}
		t.Errorf("Cucumber.Run(%q) RunResult.Status = %v, want %v", testCases, result.Status(), RunStatusPassed)
	}
}

func TestCucumberRun_TestFailed(t *testing.T) {
	changeCwd(t, "./testdata/cucumber")

	cucumber := NewCucumber(RunnerConfig{
		TestCommand: "cucumber --format json --out {{resultPath}}",
		ResultPath:  "tmp/cucumber.json",
	})

	t.Cleanup(func() {
		os.RemoveAll("tmp") // Clean up the whole tmp directory
	})

	// Create the directory for the results
	if err := os.MkdirAll("tmp", 0755); err != nil {
		t.Fatalf("could not create tmp directory: %v", err)
	}

	testCases := []plan.TestCase{
		{Path: "./features/failure.feature"},
	}
	result := NewRunResult([]plan.TestCase{})
	err := cucumber.Run(result, testCases, false)

	if err != nil {
		t.Errorf("Cucumber.Run(%q) error = %v", testCases, err)
	}

	if result.Status() != RunStatusFailed {
		t.Errorf("Cucumber.Run(%q) RunResult.Status = %v, want %v", testCases, result.Status(), RunStatusFailed)
	}

	if len(result.FailedTests()) == 0 {
		t.Errorf("Cucumber.Run(%q) expected failed tests but got none", testCases)
	}
}

func TestCucumberGetFiles(t *testing.T) {
	cucumber := NewCucumber(RunnerConfig{
		TestFilePattern: "testdata/cucumber/features/**/*.feature",
	})

	got, err := cucumber.GetFiles()
	if err != nil {
		t.Errorf("Cucumber.GetFiles() error = %v", err)
	}

	want := []string{
		"testdata/cucumber/features/another_feature.feature",
		"testdata/cucumber/features/failure.feature",
		"testdata/cucumber/features/simple_scenarios.feature",
		"testdata/cucumber/features/spells/expelliarmus.feature",
	}

	sort.Strings(got)
	sort.Strings(want)

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("Cucumber.GetFiles() diff (-got +want):\n%s", diff)
	}
}

func TestCucumberGetExamples(t *testing.T) {
	changeCwd(t, "./testdata/cucumber")

	// Configure the Cucumber runner.
	// c.Dir will be used as the CWD for the cucumber command by GetExamples.
	// Feature file paths passed to GetExamples should be relative to this Dir.
	cucumber := NewCucumber(RunnerConfig{
		TestCommand: "cucumber", // Base command; GetExamples adds necessary formatters.
	})

	files := []string{
		"features/simple_scenarios.feature",
		"features/another_feature.feature",
	}

	got, err := cucumber.GetExamples(files)
	if err != nil {
		t.Fatalf("Cucumber.GetExamples(%v) error = %v", files, err)
	}

	want := []plan.TestCase{
		{
			Path:       "features/simple_scenarios.feature:5",
			Name:       "First simple scenario",
			Identifier: "features/simple_scenarios.feature:5",
		},
		{
			Path:       "features/simple_scenarios.feature:11",
			Name:       "Second simple scenario",
			Identifier: "features/simple_scenarios.feature:11",
		},
		{
			Path:       "features/simple_scenarios.feature:15",
			Name:       "A pending scenario",
			Identifier: "features/simple_scenarios.feature:15",
		},
		{
			Path:       "features/simple_scenarios.feature:19",
			Name:       "A skipped scenario",
			Identifier: "features/simple_scenarios.feature:19",
		},
		{
			Path:       "features/simple_scenarios.feature:23",
			Name:       "A failing scenario",
			Identifier: "features/simple_scenarios.feature:23",
		},
		{
			Path:       "features/another_feature.feature:3",
			Name:       "Scenario in another feature",
			Identifier: "features/another_feature.feature:3",
		},
	}

	// Sort slices for stable comparison, as order from GetExamples might not be guaranteed.
	sort.Slice(got, func(i, j int) bool { return got[i].Path < got[j].Path })
	sort.Slice(want, func(i, j int) bool { return want[i].Path < want[j].Path })

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Cucumber.GetExamples() diff (-want +got):\n%s", diff)
	}

	// Test with no files provided
	gotEmpty, errEmpty := cucumber.GetExamples([]string{})
	if errEmpty != nil {
		t.Fatalf("Cucumber.GetExamples([]string{}) error = %v", errEmpty)
	}
	if len(gotEmpty) != 0 {
		t.Errorf("Cucumber.GetExamples([]string{}) got %d examples, want 0", len(gotEmpty))
	}

	// Test with a feature file that contains no scenarios.
	// Create a temporary empty feature file for this purpose.
	emptyFeatureFilePath := "features/empty_for_test.feature"
	f, err := os.Create(emptyFeatureFilePath)
	if err != nil {
		t.Fatalf("Failed to create empty feature file %s: %v", emptyFeatureFilePath, err)
	}
	f.Close() // Close the file immediately after creation.
	defer os.Remove(emptyFeatureFilePath) // Clean up the empty file.

	// Path to GetExamples is relative to c.Dir
	gotFromEmptyFeature, errFromEmptyFeature := cucumber.GetExamples([]string{"features/empty_for_test.feature"})
	// Cucumber's --dry-run might exit with a non-zero status if a feature file is empty or has no scenarios,
	// which GetExamples would then report as an error.
	// Or, it might exit successfully and produce an empty JSON array.
	// If an error occurs, we log it. If scenarios are returned, it's a test failure.
	if errFromEmptyFeature != nil {
		t.Logf("Cucumber.GetExamples with an empty feature file returned an error (this may be expected behavior from cucumber CLI): %v", errFromEmptyFeature)
	}
	if len(gotFromEmptyFeature) != 0 {
		t.Errorf("Cucumber.GetExamples with an empty feature file: got %d examples, want 0. Error (if any): %v", len(gotFromEmptyFeature), errFromEmptyFeature)
	}
}

func TestCucumberRun_IndividualScenarios(t *testing.T) {
	changeCwd(t, "./testdata/cucumber")

	cucumber := NewCucumber(RunnerConfig{
		TestCommand: "cucumber --format json --out {{resultPath}} {{testExamples}}",
		ResultPath:  "tmp/cucumber_individual.json",
	})

	t.Cleanup(func() {
		os.RemoveAll("tmp") // Clean up the whole tmp directory
	})

	// Create the directory for the results
	if err := os.MkdirAll("tmp", 0755); err != nil {
		t.Fatalf("could not create tmp directory: %v", err)
	}

	// Define the subset of scenarios to run
	// These identifiers must match what GetExamples produces and what Run uses for recording.
	individualTestCases := []plan.TestCase{
		{
			Path:       "features/simple_scenarios.feature:5",
			Name:       "First simple scenario",
			Identifier: "features/simple_scenarios.feature:5",
		},
		{
			Path:       "features/another_feature.feature:3",
			Name:       "Scenario in another feature",
			Identifier: "features/another_feature.feature:3",
		},
	}

	result := NewRunResult([]plan.TestCase{}) // No muted tests for this test
	err := cucumber.Run(result, individualTestCases, false)

	if err != nil {
		t.Errorf("Cucumber.Run() with individual scenarios error = %v", err)
	}

	if result.Status() != RunStatusPassed {
		jsonContent, readErr := os.ReadFile(cucumber.ResultPath)
		if readErr != nil {
			t.Logf("Failed to read %s: %v", cucumber.ResultPath, readErr)
		} else {
			t.Logf("Content of %s:\n%s", cucumber.ResultPath, string(jsonContent))
		}
		t.Errorf("Cucumber.Run() with individual scenarios RunResult.Status = %v, want %v", result.Status(), RunStatusPassed)
	}

	// Verify that only the specified tests were run and passed
	stats := result.Statistics()
	if stats.Total != len(individualTestCases) {
		t.Errorf("Expected %d total tests, got %d", len(individualTestCases), stats.Total)
	}
	if stats.PassedOnFirstRun != len(individualTestCases) {
		t.Errorf("Expected %d passed tests, got %d", len(individualTestCases), stats.PassedOnFirstRun)
	}
	if stats.Failed != 0 {
		t.Errorf("Expected 0 failed tests, got %d", stats.Failed)
	}
	if stats.Skipped != 0 {
		t.Errorf("Expected 0 skipped tests, got %d", stats.Skipped)
	}
}

func TestCucumberRun_ScenarioStatuses(t *testing.T) {
	changeCwd(t, "./testdata/cucumber")

	cucumberRunner := NewCucumber(RunnerConfig{
		TestCommand: "cucumber --format json --out {{resultPath}}", // {{testExamples}} will be appended by the runner if not present
		ResultPath:  "tmp/cucumber_statuses.json",
	})

	t.Cleanup(func() {
		os.RemoveAll("tmp")
	})

	if err := os.MkdirAll("tmp", 0755); err != nil {
		t.Fatalf("could not create tmp directory: %v", err)
	}

	// 1. Discover scenarios from simple_scenarios.feature
	featureFiles := []string{"features/simple_scenarios.feature"}
	discoveredScenarios, err := cucumberRunner.GetExamples(featureFiles)
	if err != nil {
		t.Fatalf("Cucumber.GetExamples(%v) error = %v", featureFiles, err)
	}

	expectedScenarioCount := 5 // 2 passing, 1 pending, 1 skipped, 1 failing
	if len(discoveredScenarios) != expectedScenarioCount {
		t.Errorf("Expected %d scenarios from GetExamples, got %d", expectedScenarioCount, len(discoveredScenarios))
		for i, sc := range discoveredScenarios {
			t.Logf("Discovered scenario %d: ID=%s, Name=%s, Path=%s", i, sc.Identifier, sc.Name, sc.Path)
		}
	}

	// 2. Run all discovered scenarios
	result := NewRunResult([]plan.TestCase{}) // No muted tests
	// runErr will capture errors from the cucumber command execution itself (e.g., command not found, non-zero exit for reasons other than test failures)
	// Test failures themselves are recorded in the result object and don't necessarily cause cucumberRunner.Run to return an error.
	_ = cucumberRunner.Run(result, discoveredScenarios, false) // We expect test failures, so the error from Run() might be misleading here if it only reflects cucumber's exit code.

	// 3. Assert RunResult status and statistics
	if result.Status() != RunStatusFailed {
		// Log the JSON output if the overall status is not what we expect.
		jsonContent, readErr := os.ReadFile(cucumberRunner.ResultPath)
		if readErr != nil {
			t.Logf("Failed to read result file %s: %v", cucumberRunner.ResultPath, readErr)
		} else {
			t.Logf("Content of result file %s:\n%s", cucumberRunner.ResultPath, string(jsonContent))
		}
		t.Errorf("RunResult.Status() = %v, want %v", result.Status(), RunStatusFailed)
	}

	stats := result.Statistics()
	if stats.Total != expectedScenarioCount {
		t.Errorf("Statistics: Total = %d, want %d", stats.Total, expectedScenarioCount)
	}

	expectedPassed := 2
	expectedFailed := 1
	expectedSkipped := 2 // Pending maps to skipped

	if stats.PassedOnFirstRun != expectedPassed {
		t.Errorf("Statistics: PassedOnFirstRun = %d, want %d", stats.PassedOnFirstRun, expectedPassed)
	}
	if stats.Failed != expectedFailed {
		t.Errorf("Statistics: Failed = %d, want %d", stats.Failed, expectedFailed)
	}
	if stats.Skipped != expectedSkipped {
		t.Errorf("Statistics: Skipped = %d, want %d", stats.Skipped, expectedSkipped)
	}

	// 4. Optional: Verify status of specific scenarios by identifier
	// Ensure test identifiers match exactly what GetExamples produces and Run uses.
	// These are based on the line numbers in simple_scenarios.feature
	// Keys are Scope/Name/Path as stored by RunResult
	wantStatuses := map[string]TestStatus{ // Use TestStatus from runner package
		"Simple Scenarios/First simple scenario/features/simple_scenarios.feature:5":   TestStatusPassed,
		"Simple Scenarios/Second simple scenario/features/simple_scenarios.feature:11":  TestStatusPassed,
		"Simple Scenarios/A pending scenario/features/simple_scenarios.feature:15":    TestStatusSkipped, // Pending scenario
		"Simple Scenarios/A skipped scenario/features/simple_scenarios.feature:19":   TestStatusSkipped, // Skipped scenario
		"Simple Scenarios/A failing scenario/features/simple_scenarios.feature:23":    TestStatusFailed,  // Failing scenario
	}

	if len(result.tests) != expectedScenarioCount { // Access result.tests map directly
		t.Errorf("Expected %d test results, got %d", expectedScenarioCount, len(result.tests))
	}

	for identifier, testCaseResult := range result.tests { // Iterate over map
		expectedStatus, ok := wantStatuses[identifier] // Use identifier from map key
		if !ok {
			t.Errorf("Unexpected test case identifier in results: %s", identifier)
			continue
		}
		if testCaseResult.Status != expectedStatus {
			t.Errorf("Status for %s: got %v, want %v", identifier, testCaseResult.Status, expectedStatus)
		}
	}
}

func TestCucumberCommandNameAndArgs_WithInterpolationPlaceholder(t *testing.T) {
	testCases := []string{"features/spells/expelliarmus.feature", "features/failure.feature"}
	testCommand := "cucumber --format json --out {{resultPath}} {{testExamples}}"

	c := NewCucumber(RunnerConfig{
		TestCommand: testCommand,
		ResultPath:  "cucumber.json",
	})

	gotName, gotArgs, err := c.commandNameAndArgs(testCommand, testCases)
	if err != nil {
		t.Errorf("commandNameAndArgs(%q, %q) error = %v", testCases, testCommand, err)
	}

	wantName := "cucumber"
	wantArgs := []string{"--format", "json", "--out", "cucumber.json", "features/spells/expelliarmus.feature", "features/failure.feature"}

	if diff := cmp.Diff(gotName, wantName); diff != "" {
		t.Errorf("commandNameAndArgs(%q, %q) diff (-got +want):\n%s", testCases, testCommand, diff)
	}
	if diff := cmp.Diff(gotArgs, wantArgs); diff != "" {
		t.Errorf("commandNameAndArgs(%q, %q) diff (-got +want):\n%s", testCases, testCommand, diff)
	}
}

func TestCucumberCommandNameAndArgs_WithoutTestExamplesPlaceholder(t *testing.T) {
	testCases := []string{"features/spells/expelliarmus.feature", "features/failure.feature"}
	testCommand := "cucumber --format json --out {{resultPath}}"

	c := NewCucumber(RunnerConfig{
		TestCommand: testCommand,
		ResultPath:  "cucumber.json",
	})

	gotName, gotArgs, err := c.commandNameAndArgs(testCommand, testCases)
	if err != nil {
		t.Errorf("commandNameAndArgs(%q, %q) error = %v", testCases, testCommand, err)
	}

	wantName := "cucumber"
	wantArgs := []string{"--format", "json", "--out", "cucumber.json", "features/spells/expelliarmus.feature", "features/failure.feature"}

	if diff := cmp.Diff(gotName, wantName); diff != "" {
		t.Errorf("commandNameAndArgs(%q, %q) diff (-got +want):\n%s", testCases, testCommand, diff)
	}
	if diff := cmp.Diff(gotArgs, wantArgs); diff != "" {
		t.Errorf("commandNameAndArgs(%q, %q) diff (-got +want):\n%s", testCases, testCommand, diff)
	}
}

func TestCucumberCommandNameAndArgs_InvalidTestCommand(t *testing.T) {
	testCases := []string{"features/spells/expelliarmus.feature", "features/failure.feature"}
	testCommand := "cucumber --format json --out '{{resultPath}} {{testExamples}}"

	c := NewCucumber(RunnerConfig{
		TestCommand: testCommand,
	})

	gotName, gotArgs, err := c.commandNameAndArgs(testCommand, testCases)

	wantName := ""
	wantArgs := []string{}

	if diff := cmp.Diff(gotName, wantName); diff != "" {
		t.Errorf("commandNameAndArgs() diff (-got +want):\n%s", diff)
	}
	if diff := cmp.Diff(gotArgs, wantArgs); diff != "" {
		t.Errorf("commandNameAndArgs() diff (-got +want):\n%s", diff)
	}
	if !errors.Is(err, shellquote.UnterminatedSingleQuoteError) {
		t.Errorf("commandNameAndArgs() error = %v, want %v", err, shellquote.UnterminatedSingleQuoteError)
	}
}

func TestCucumberGetExamples_WithOtherFormatters(t *testing.T) {
	changeCwd(t, "./testdata/cucumber")

	files := []string{"features/simple_scenarios.feature"}
	want := []plan.TestCase{
		{
			Path:       "features/simple_scenarios.feature:5",
			Name:       "First simple scenario",
			Identifier: "features/simple_scenarios.feature:5",
		},
		{
			Path:       "features/simple_scenarios.feature:11",
			Name:       "Second simple scenario",
			Identifier: "features/simple_scenarios.feature:11",
		},
		{
			Path:       "features/simple_scenarios.feature:15",
			Name:       "A pending scenario",
			Identifier: "features/simple_scenarios.feature:15",
		},
		{
			Path:       "features/simple_scenarios.feature:19",
			Name:       "A skipped scenario",
			Identifier: "features/simple_scenarios.feature:19",
		},
		{
			Path:       "features/simple_scenarios.feature:23",
			Name:       "A failing scenario",
			Identifier: "features/simple_scenarios.feature:23",
		},
	}
	sort.Slice(want, func(i, j int) bool { return want[i].Path < want[j].Path })

	// Create a temporary file to store the JSON output of the cucumber dry run for one of the commands.
	// So we don't end up with a lot of files after running this test.
	// We'll clean up the file after the test.
	// Ensure tmp directory exists (it's created by other tests, but good for standalone robustness)
	if err := os.MkdirAll("tmp", 0755); err != nil {
		t.Fatalf("could not create tmp directory: %v", err)
	}
	f, err := os.CreateTemp("tmp", "cucumber-*.html") 
	if err != nil {
		t.Fatalf("os.CreateTemp() error = %v", err)
	}
	f.Close()
	// The global Cleanup in TestMain or individual test Cleanups for "tmp" should handle this.
	// os.Remove(f.Name()) // Not strictly needed if tmp is cleaned up globally.

	commands := []string{
		"cucumber --format progress",
		"cucumber --format pretty",
		"cucumber --format html --out " + f.Name(),
		"cucumber --format progress --format json --out some_other.json", // GetExamples should override this json output for dry-run
	}

	for _, command := range commands {
		cucumberRunner := NewCucumber(RunnerConfig{ // Renamed to avoid conflict with package name
			TestCommand: command,
		})
		t.Run(command, func(t *testing.T) {
			got, err := cucumberRunner.GetExamples(files)
			if err != nil {
				t.Fatalf("Cucumber.GetExamples(%v) with TestCommand '%s' error = %v", files, command, err)
			}

			sort.Slice(got, func(i, j int) bool { return got[i].Path < got[j].Path })

			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("Cucumber.GetExamples(%v) with TestCommand '%s' diff (-want +got):\n%s", files, command, diff)
			}
		})
	}
}
