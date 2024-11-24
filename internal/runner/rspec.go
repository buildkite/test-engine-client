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

type Rspec struct {
	RunnerConfig
}

func NewRspec(r RunnerConfig) Rspec {
	if r.TestCommand == "" {
		r.TestCommand = "bundle exec rspec --format progress --format json --out {{resultPath}} {{testExamples}}"
	}

	if r.TestFilePattern == "" {
		r.TestFilePattern = "spec/**/*_spec.rb"
	}

	if r.RetryTestCommand == "" {
		r.RetryTestCommand = r.TestCommand
	}

	return Rspec{
		RunnerConfig: r,
	}
}

func (r Rspec) Name() string {
	return "RSpec"
}

// GetFiles returns an array of file names using the discovery pattern.
func (r Rspec) GetFiles() ([]string, error) {
	debug.Println("Discovering test files with include pattern:", r.TestFilePattern, "exclude pattern:", r.TestFileExcludePattern)
	files, err := discoverTestFiles(r.TestFilePattern, r.TestFileExcludePattern)
	debug.Println("Discovered", len(files), "files")

	// rspec test in Test Engine is stored with leading "./"
	// therefore, we need to add "./" to the file path
	// to match the test path in Test Engine
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
func (r Rspec) Run(result *RunResult, testCases []plan.TestCase, retry bool) error {
	command := r.TestCommand

	if retry {
		command = r.RetryTestCommand
	}

	testPaths := make([]string, len(testCases))
	for i, tc := range testCases {
		testPaths[i] = tc.Path
	}

	commandName, commandArgs, err := r.commandNameAndArgs(command, testPaths)
	if err != nil {
		result.err = err
		return fmt.Errorf("failed to build command: %w", err)
	}

	cmd := exec.Command(commandName, commandArgs...)

	err = runAndForwardSignal(cmd)

	if ProcessSignaledError := new(ProcessSignaledError); errors.As(err, &ProcessSignaledError) {
		result.err = err
		return err
	}

	report, parseErr := r.ParseReport(r.ResultPath)
	if parseErr != nil {
		// If we can't parse the report, it indicates a failure in the rspec command itself (as opposed to the tests failing),
		// therefore we need to bubble up the error.
		fmt.Println("Buildkite Test Engine Client: Failed to read Rspec output, tests will not be retried.")
		result.err = err
		return err
	}

	for _, example := range report.Examples {
		var status TestStatus
		switch example.Status {
		case "failed":
			status = TestStatusFailed
		case "passed":
			status = TestStatusPassed
		}

		result.RecordTestResult(mapExampleToTestCase(example), status)
	}

	return nil
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
	} else {
		words = slices.Replace(words, idx, idx+1, testCases...)
	}

	idx = slices.Index(words, "{{resultPath}}")
	if idx >= 0 {
		words = slices.Replace(words, idx, idx+1, r.ResultPath)
	}

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

	defer func() {
		f.Close()
		os.Remove(f.Name())
	}()

	cmdName, cmdArgs, err := r.commandNameAndArgs(r.TestCommand, files)
	if err != nil {
		return nil, err
	}

	cmdArgs = append(cmdArgs, "--dry-run", "--format", "json", "--out", f.Name(), "--format", "progress")

	debug.Printf("Running `%s %s` for dry run", cmdName, strings.Join(cmdArgs, " "))

	output, err := exec.Command(cmdName, cmdArgs...).CombinedOutput()

	if err != nil {
		return []plan.TestCase{}, fmt.Errorf("failed to run rspec dry run: %s", output)
	}

	report, err := r.ParseReport(f.Name())
	if err != nil {
		return []plan.TestCase{}, err
	}

	var testCases []plan.TestCase
	for _, example := range report.Examples {
		testCases = append(testCases, mapExampleToTestCase(example))
	}

	return testCases, nil
}

func mapExampleToTestCase(example RspecExample) plan.TestCase {
	// The scope and name has to match with the scope generated by Buildkite test collector.
	// In Buildkite test collector, the scope is generated from `example_group.metadata[:full_description]`
	// that doesn't include the test description.
	// However, the `example_group.metadata` attribute is not available in the RSpec JSON report.
	// The RSpec JSON report only contains the `full_description` attribute that includes the test description.
	// Therefore, we need to remove the test description from the `full_description` attribute to match the scope.
	// For more details, see:
	// [Buildkite Test Collector - RSpec implementation](https://github.com/buildkite/test-collector-ruby/blob/2d641486e42f666dd07ffed4cbf2cd0f9dc97619/lib/buildkite/test_collector/rspec_plugin/trace.rb#L27)
	scope := strings.TrimSuffix(example.FullDescription, " "+example.Description)
	return plan.TestCase{
		Identifier: example.Id,
		Name:       example.Description,
		Path:       example.Id,
		Scope:      scope,
	}
}
