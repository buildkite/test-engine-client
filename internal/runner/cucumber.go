package runner

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"

	"github.com/buildkite/test-engine-client/internal/debug"
	"github.com/buildkite/test-engine-client/internal/plan"
	"github.com/kballard/go-shellquote"
)

// Cucumber implements TestRunner for Cucumber (Ruby implementation).
// It follows very similar behaviour to the RSpec runner. We rely on the JSON formatter
// so users MUST include `--format json --out {{resultPath}}` in their custom commands.
//
// We treat every Scenario as an individual test case. A scenario is considered failed
// if any step in it failed or has undefined status. "pending" and "skipped" are
// mapped to TestStatusSkipped.

type Cucumber struct {
	RunnerConfig
}

func NewCucumber(c RunnerConfig) Cucumber {
	if c.TestCommand == "" {
		// The pretty formatter gives a nice progress bar in the console, the JSON formatter is required for bktec.
		c.TestCommand = "cucumber --format pretty --format json --out {{resultPath}} {{testExamples}}"
	}

	if c.TestFilePattern == "" {
		c.TestFilePattern = "features/**/*.feature"
	}

	if c.RetryTestCommand == "" {
		c.RetryTestCommand = c.TestCommand
	}

	return Cucumber{
		RunnerConfig: c,
	}
}

func (c Cucumber) Name() string {
	return "Cucumber"
}

// GetFiles returns the list of feature files based on include / exclude pattern.
func (c Cucumber) GetFiles() ([]string, error) {
	debug.Println("Discovering test files with include pattern:", c.TestFilePattern, "exclude pattern:", c.TestFileExcludePattern)
	files, err := discoverTestFiles(c.TestFilePattern, c.TestFileExcludePattern)
	debug.Println("Discovered", len(files), "files")

	if err != nil {
		return nil, err
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("no files found with pattern %q and exclude pattern %q", c.TestFilePattern, c.TestFileExcludePattern)
	}

	return files, nil
}

// Run executes the Cucumber command and records results.
func (c Cucumber) Run(result *RunResult, testCases []plan.TestCase, retry bool) error {
	command := c.TestCommand
	if retry {
		command = c.RetryTestCommand
	}

	testPaths := make([]string, len(testCases))
	for i, tc := range testCases {
		testPaths[i] = tc.Path
	}

	commandName, commandArgs, err := c.commandNameAndArgs(command, testPaths)
	if err != nil {
		return fmt.Errorf("failed to build command: %w", err)
	}

	cmd := exec.Command(commandName, commandArgs...)

	err = runAndForwardSignal(cmd)
	if ProcessSignaledError := new(ProcessSignaledError); errors.As(err, &ProcessSignaledError) {
		return err
	}

	report, parseErr := c.ParseReport(c.ResultPath)
	if parseErr != nil {
		fmt.Println("Buildkite Test Engine Client: Failed to read Cucumber JSON output, tests will not be retried.")
		return err
	}

	// Iterate scenarios.
	for _, feature := range report {
		for _, scenario := range feature.Elements {
			if scenario.Type != "scenario" {
				continue
			}
			status := scenario.AggregatedStatus()
			var testStatus TestStatus
			switch status {
			case "failed", "undefined", "errored":
				testStatus = TestStatusFailed
			case "passed":
				testStatus = TestStatusPassed
			case "pending", "skipped" /* cucumber-js uses skipped */ :
				testStatus = TestStatusSkipped
			default:
				testStatus = TestStatusSkipped
			}

			fileLinePath := fmt.Sprintf("%s:%d", feature.URI, scenario.Line)
			testCaseForResult := plan.TestCase{
				Identifier: fileLinePath, // Use file:line as the primary identifier
				Name:       scenario.Name,
				Scope:      feature.Name,
				Path:       fileLinePath,
			}

			result.RecordTestResult(testCaseForResult, testStatus)
		}
	}

	// Determine if there were any errors outside of scenarios. Cucumber does not
	// provide such count – we rely on process exit status already handled above.

	return nil
}

// CucumberFeature and CucumberElement structs would be defined, likely in a separate parser file.
// For brevity, they are assumed here.

// mapScenarioToTestCase maps a Cucumber scenario (element) to a plan.TestCase
func mapScenarioToTestCase(featureURI string, scenario CucumberElement) plan.TestCase {
	// Cucumber scenarios are identified by file_path:line_number
	identifier := fmt.Sprintf("%s:%d", featureURI, scenario.Line)
	return plan.TestCase{
		Path:       identifier,
		Name:       scenario.Name,
		Identifier: identifier, // Or scenario.ID if it's more suitable and consistently available
	}
}

// GetExamples returns an array of test scenarios within the given feature files.
func (c Cucumber) GetExamples(files []string) ([]plan.TestCase, error) {
	if len(files) == 0 {
		return []plan.TestCase{}, nil
	}

	// Create a temporary file to store the JSON output of the cucumber dry run.
	f, err := os.CreateTemp("", "cucumber-dry-run-*.json")
	if err != nil {
		return nil, fmt.Errorf("failed to create temporary file for cucumber dry run: %w", err)
	}
	debug.Printf("Created temp file for cucumber dry run: %s", f.Name())

	defer func() {
		closeErr := f.Close()
		if closeErr != nil {
			debug.Printf("Error closing temp file %s: %v", f.Name(), closeErr)
		}
		removeErr := os.Remove(f.Name())
		if removeErr != nil {
			debug.Printf("Error removing temp file %s: %v", f.Name(), removeErr)
		}
	}()

	cmdName, _, err := c.commandNameAndArgs(c.TestCommand, files)
	if err != nil {
		return nil, err
	}

	dryRunArgs := append(
		[]string{"--dry-run", "--format", "json", "--out", f.Name(), "--format", "progress"},
		files...
	)

	debug.Printf("Running `%s %s` for dry run", cmdName, strings.Join(dryRunArgs, " "))

	output, err := exec.Command(cmdName, dryRunArgs...).CombinedOutput()
	if err != nil {
		return []plan.TestCase{}, fmt.Errorf("failed to run Cucumber dry run: %s", output)
	}

	dryRunReport, parseErr := parseCucumberDryRunJSONOutput(f.Name()) // Use parser from cucumber_result_parser.go
	if parseErr != nil {
		return nil, fmt.Errorf("failed to parse cucumber dry run JSON report from %s: %w", f.Name(), parseErr)
	}

	var testCases []plan.TestCase
	for _, feature := range dryRunReport {
		for _, scenario := range feature.Elements {
			if scenario.Type == "scenario" { // Only include scenarios, not scenario outlines directly (examples are handled differently)
				testCases = append(testCases, mapScenarioToTestCase(feature.URI, scenario))
			} else if scenario.Type == "scenario_outline" && scenario.Keyword == "Scenario Outline" {
				// Scenario outlines themselves aren't runnable directly by path:line of the outline.
				// Cucumber expands them into concrete scenarios based on their Examples tables.
				// The JSON from a dry run might already include these expanded examples as individual 'scenario' type elements.
				// If not, we'd need to parse scenario.Examples and generate test cases for each example row.
				// For now, we assume the JSON includes expanded examples as type: "scenario".
				// If the dry run JSON for outlines is different, this part needs adjustment.
				// Let's log if we encounter an outline to see its structure.
				debug.Printf("Encountered Scenario Outline: %s:%d. Its examples might be listed as separate scenarios.", feature.URI, scenario.Line)
			}
		}
	}

	return testCases, nil
}

// commandNameAndArgs replaces placeholders and returns command + args.
func (c Cucumber) commandNameAndArgs(cmd string, testCases []string) (string, []string, error) {
	words, err := shellquote.Split(cmd)
	if err != nil {
		return "", []string{}, err
	}

	idx := slices.Index(words, "{{testExamples}}")
	if idx < 0 {
		words = append(words, testCases...)
	} else {
		words = slices.Replace(words, idx, idx+1, testCases...)
	}

	idx = slices.Index(words, "{{resultPath}}")
	if idx >= 0 {
		words = slices.Replace(words, idx, idx+1, c.ResultPath)
	}

	return words[0], words[1:], nil
}

// ---------------- Report parsing -------------------
// ParseReport now uses CucumberFeature from cucumber_result_parser.go

func (c Cucumber) ParseReport(path string) ([]CucumberFeature, error) {
	var report []CucumberFeature
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read cucumber output: %v", err)
	}

	if err := json.Unmarshal(data, &report); err != nil {
		return nil, fmt.Errorf("failed to parse cucumber output: %s", err)
	}

	return report, nil
}
