package runner

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"

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
//
// NOTE: Splitting by example for Cucumber is not currently supported – GetExamples
// returns an error so the server will fall-back to file-level splitting.

type Cucumber struct {
	RunnerConfig
}

func NewCucumber(c RunnerConfig) Cucumber {
	if c.TestCommand == "" {
		// The pretty formatter gives a nice progress bar in the console, the JSON formatter is required for bktec.
		c.TestCommand = "bundle exec cucumber --format pretty --format json --out {{resultPath}} {{testExamples}}"
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

			testCase := plan.TestCase{
				Identifier: scenario.ID,
				Name:       scenario.Name,
				Scope:      feature.Name,
				// Running an individual scenario in cucumber can be done using line-number.
				Path: fmt.Sprintf("%s:%d", feature.URI, scenario.Line),
			}

			result.RecordTestResult(testCase, testStatus)
		}
	}

	// Determine if there were any errors outside of scenarios. Cucumber does not
	// provide such count – we rely on process exit status already handled above.

	return nil
}

// GetExamples is not supported for Cucumber at the moment.
func (c Cucumber) GetExamples(files []string) ([]plan.TestCase, error) {
	return nil, fmt.Errorf("not supported in Cucumber")
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

// CucumberReport is a slice of Feature results.
// We use a subset of the official JSON schema that we need.

type CucumberReport []CucumberFeature

type CucumberFeature struct {
	URI      string            `json:"uri"`
	Name     string            `json:"name"`
	Elements []CucumberElement `json:"elements"`
}

type CucumberElement struct {
	ID      string         `json:"id"`
	Name    string         `json:"name"`
	Line    int            `json:"line"`
	Steps   []CucumberStep `json:"steps"`
	Keyword string         `json:"keyword"`
}

type CucumberStep struct {
	Result struct {
		Status string `json:"status"`
	} `json:"result"`
}

// AggregatedStatus returns overall scenario status based on its steps.
func (e CucumberElement) AggregatedStatus() string {
	status := "passed"
	for _, step := range e.Steps {
		switch step.Result.Status {
		case "failed", "undefined", "errored":
			return "failed"
		case "pending", "skipped":
			status = "pending" // treat as skipped unless fail found
		}
	}
	return status
}

func (c Cucumber) ParseReport(path string) (CucumberReport, error) {
	var report CucumberReport
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read cucumber output: %v", err)
	}

	if err := json.Unmarshal(data, &report); err != nil {
		return nil, fmt.Errorf("failed to parse cucumber output: %s", err)
	}

	return report, nil
}
