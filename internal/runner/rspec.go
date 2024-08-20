package runner

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"

	"github.com/buildkite/test-splitter/internal/debug"
	"github.com/buildkite/test-splitter/internal/plan"
	"github.com/kballard/go-shellquote"
)

// In future, Rspec will implement an interface that defines
// behaviour common to all test runners.
// For now, Rspec provides rspec specific behaviour to execute
// and report on tests in the Rspec framework.
type Rspec struct {
	TestCommand            string
	TestFileExcludePattern string
	TestFilePattern        string
	RetryTestCommand       string
}

func NewRspec(r Rspec) *Rspec {
	if r.TestCommand == "" {
		r.TestCommand = "bundle exec rspec --format progress {{testExamples}}"
	}

	if r.TestFilePattern == "" {
		r.TestFilePattern = "spec/**/*_spec.rb"
	}

	if r.RetryTestCommand == "" {
		r.RetryTestCommand = r.TestCommand
	}

	return &r
}

func (r Rspec) Name() string {
	return "RSpec"
}

// GetFiles returns an array of file names using the discovery pattern.
func (r Rspec) GetFiles() ([]string, error) {
	debug.Println("Discovering test files with include pattern:", r.TestFilePattern, "exclude pattern:", r.TestFileExcludePattern)
	files, err := discoverTestFiles(r.TestFilePattern, r.TestFileExcludePattern)
	debug.Println("Discovered", len(files), "files")

	// rspec test in Test Analytics is stored with leading "./"
	// therefore, we need to add "./" to the file path
	// to match the test path in Test Analytics
	for i, file := range files {
		files[i] = "./" + file
	}

	if err != nil {
		return nil, err
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("no files found with pattern %q and exclude pattern %q", r.TestFilePattern, r.TestFileExcludePattern)
	}

	return files, nil
}

// Run executes the test command with the given test cases.
// If retry is true, it will run the command using the retry test command,
// otherwise it will use the test command.
//
// Error is returned if the command fails to run, exits prematurely, or if the
// output cannot be parsed.
//
// Test failure is not considered an error, and is instead returned as a RunResult.
func (r Rspec) Run(testCases []string, retry bool) (RunResult, error) {
	var command string

	if retry {
		command = r.RetryTestCommand
	} else {
		command = r.TestCommand
	}

	commandName, commandArgs, err := r.commandNameAndArgs(command, testCases)
	if err != nil {
		return RunResult{Status: RunStatusError}, fmt.Errorf("failed to build command: %w", err)
	}

	// Create a temporary file to store the JSON output of the rspec run.
	// This is a temporary solution, until we have an option to specify the output file.
	f, err := os.CreateTemp("", "rspec-*.json")
	if err != nil {
		return RunResult{Status: RunStatusError}, fmt.Errorf("failed to create temporary file for rspec output: %v", err)
	}
	defer f.Close()
	defer os.Remove(f.Name())

	fmt.Printf("%s %s\n", commandName, strings.Join(commandArgs, " "))
	commandArgs = append(commandArgs, "--format", "json", "--out", f.Name())
	cmd := exec.Command(commandName, commandArgs...)

	err = runAndForwardSignal(cmd)

	if err == nil {
		return RunResult{Status: RunStatusPassed}, nil
	}

	if ProcessSignaledError := new(ProcessSignaledError); errors.As(err, &ProcessSignaledError) {
		return RunResult{Status: RunStatusError}, err
	}

	if exitError := new(exec.ExitError); errors.As(err, &exitError) {
		report, parseErr := r.ParseReport(f.Name())
		if parseErr != nil {
			// If we can't parse the report, it indicates a failure in the rspec command itself (as opposed to the tests failing),
			// therefore we need to bubble up the error.
			fmt.Println("Buildkite Test Splitter: Failed to read Rspec output, tests will not be retried.")
			return RunResult{Status: RunStatusError}, err
		}

		if report.Summary.FailureCount > 0 {
			var failedTests []string
			for _, example := range report.Examples {
				if example.Status == "failed" {
					failedTests = append(failedTests, example.Id)
				}
			}
			return RunResult{Status: RunStatusFailed, FailedTests: failedTests}, nil
		}
	}

	return RunResult{Status: RunStatusError}, err
}

// RspecExample represents a single test example in an Rspec report.
type RspecExample struct {
	Id              string  `json:"id"`
	Description     string  `json:"description"`
	FullDescription string  `json:"full_description"`
	Status          string  `json:"status"`
	FilePath        string  `json:"file_path"`
	LineNumber      int     `json:"line_number"`
	RunTime         float64 `json:"run_time"`
}

// RspecReport is the structure for Rspec JSON report.
type RspecReport struct {
	Version  string         `json:"version"`
	Seed     int            `json:"seed"`
	Examples []RspecExample `json:"examples"`
	Summary  struct {
		ExampleCount int `json:"example_count"`
		FailureCount int `json:"failure_count"`
		PendingCount int `json:"pending_count"`
	}
}

func (r Rspec) ParseReport(path string) (RspecReport, error) {
	var report RspecReport
	data, err := os.ReadFile(path)
	if err != nil {
		return RspecReport{}, fmt.Errorf("failed to read rspec output: %v", err)
	}

	if err := json.Unmarshal(data, &report); err != nil {
		return RspecReport{}, fmt.Errorf("failed to parse rspec output: %s", err)
	}

	return report, nil
}

// commandNameAndArgs replaces the "{{testExamples}}" placeholder in the test command with the test cases.
// It returns the command name and arguments to run the tests.
func (r Rspec) commandNameAndArgs(cmd string, testCases []string) (string, []string, error) {
	words, err := shellquote.Split(cmd)
	if err != nil {
		return "", []string{}, err
	}
	idx := slices.Index(words, "{{testExamples}}")
	if idx < 0 {
		words = append(words, testCases...)
		return words[0], words[1:], nil
	}
	words = slices.Replace(words, idx, idx+1, testCases...)
	return words[0], words[1:], nil
}

// GetExamples returns an array of test examples within the given files.
func (r Rspec) GetExamples(files []string) ([]plan.TestCase, error) {
	// Create a temporary file to store the JSON output of the rspec dry run.
	// We cannot simply read the dry run output from stdout because
	// users may have custom formatters that do not output JSON.
	f, err := os.CreateTemp("", "dry-run-*.json")
	if err != nil {
		return []plan.TestCase{}, fmt.Errorf("failed to create temporary file for rspec dry run: %v", err)
	}
	defer f.Close()
	defer os.Remove(f.Name())

	cmdName, cmdArgs, err := r.commandNameAndArgs(r.TestCommand, files)
	if err != nil {
		return nil, err
	}

	cmdArgs = append(cmdArgs, "--dry-run", "--format", "json", "--out", f.Name())
	if debug.Enabled {
		cmdArgs = append(cmdArgs, "--format", "progress")
	}

	debug.Println("Running `rspec --dry-run`")

	output, err := exec.Command(cmdName, cmdArgs...).CombinedOutput()
	debug.Println(string(output))

	if err != nil {
		return []plan.TestCase{}, fmt.Errorf("failed to run rspec dry run: %s", output)
	}

	report, err := r.ParseReport(f.Name())
	if err != nil {
		return []plan.TestCase{}, err
	}

	var testCases []plan.TestCase
	for _, example := range report.Examples {
		testCases = append(testCases, plan.TestCase{
			Identifier: example.Id,
			Name:       example.Description,
			Path:       fmt.Sprintf("%s:%d", example.FilePath, example.LineNumber),
			Scope:      example.FullDescription,
		})
	}

	return testCases, nil
}
