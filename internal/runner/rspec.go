package runner

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"slices"

	"github.com/buildkite/test-splitter/internal/debug"
	"github.com/buildkite/test-splitter/internal/plan"
	"github.com/kballard/go-shellquote"
)

// In future, Rspec will implement an interface that defines
// behaviour common to all test runners.
// For now, Rspec provides rspec specific behaviour to execute
// and report on tests in the Rspec framework.
type Rspec struct {
	TestCommand      string
	RetryTestCommand string
}

func NewRspec(testCommand string, retryCommand string) Rspec {
	if testCommand == "" {
		testCommand = "bundle exec rspec {{testExamples}}"
	}

	return Rspec{
		TestCommand:      testCommand,
		RetryTestCommand: retryCommand,
	}
}

func (r Rspec) Name() string {
	return "RSpec"
}

// GetFiles returns an array of file names using the discovery pattern.
func (r Rspec) GetFiles() ([]string, error) {
	pattern := r.discoveryPattern()

	debug.Println("Discovering test files with include pattern:", pattern.IncludePattern, "exclude pattern:", pattern.ExcludePattern)
	files, err := discoverTestFiles(pattern)
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
		return nil, fmt.Errorf("no files found with pattern %q and exclude pattern %q", pattern.IncludePattern, pattern.ExcludePattern)
	}

	return files, nil
}

func (r Rspec) RetryCommand() (*exec.Cmd, error) {
	words := []string{}

	if r.RetryTestCommand == "" {
		// use test command to build retry command if retry command is not provided
		// remove all occurrences of "{{testExamples}}" from the test command and append "--only-failures"
		splits, err := shellquote.Split(r.TestCommand)
		if err != nil {
			return nil, err
		}
		splits = slices.DeleteFunc(splits, func(n string) bool {
			return n == "{{testExamples}}"
		})
		splits = slices.Insert(splits, len(splits), "--only-failures")
		words = append(words, splits...)
	} else {
		splits, err := shellquote.Split(r.RetryTestCommand)
		if err != nil {
			return nil, err
		}
		words = append(words, splits...)
	}

	fmt.Println(shellquote.Join(words...))

	cmd := exec.Command(words[0], words[1:]...)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	return cmd, nil
}

// Command returns an exec.Cmd that will run the rspec command
func (r Rspec) Command(testCases []string) (*exec.Cmd, error) {
	commandName, commandArgs, err := r.commandNameAndArgs(testCases)
	if err != nil {
		return nil, err
	}
	fmt.Println(shellquote.Join(append([]string{commandName}, commandArgs...)...))

	cmd := exec.Command(commandName, commandArgs...)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	return cmd, nil
}

// discoveryPattern returns the pattern to use for discovering test files.
// It uses the BUILDKITE_SPLITTER_TEST_FILE_PATTERN and BUILDKITE_SPLITTER_TEST_FILE_EXCLUDE_PATTERN.
// If BUILDKITE_SPLITTER_TEST_FILE_PATTERN is not set, it defaults to "spec/**/*_spec.rb"
func (Rspec) discoveryPattern() DiscoveryPattern {
	includePattern := os.Getenv("BUILDKITE_SPLITTER_TEST_FILE_PATTERN")

	if includePattern == "" {
		includePattern = "spec/**/*_spec.rb"
	}

	excludePattern := os.Getenv("BUILDKITE_SPLITTER_TEST_FILE_EXCLUDE_PATTERN")

	return DiscoveryPattern{
		IncludePattern: includePattern,
		ExcludePattern: excludePattern,
	}
}

// commandNameAndArgs returns the command name and arguments to run the Rspec tests
func (r Rspec) commandNameAndArgs(testCases []string) (string, []string, error) {
	words, err := shellquote.Split(r.TestCommand)
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

	cmdName, cmdArgs, err := r.commandNameAndArgs(files)
	if err != nil {
		return nil, err
	}

	cmdArgs = append(cmdArgs, "--dry-run", "--format", "json", "--out", f.Name())

	debug.Println("Running `rspec --dry-run`")

	output, err := exec.Command(cmdName, cmdArgs...).CombinedOutput()

	if err != nil {
		return []plan.TestCase{}, fmt.Errorf("failed to run rspec dry run: %s", output)
	}

	var report RspecReport
	data, err := os.ReadFile(f.Name())
	if err != nil {
		return []plan.TestCase{}, fmt.Errorf("failed to read rspec dry run output: %v", err)
	}

	if err := json.Unmarshal(data, &report); err != nil {
		return []plan.TestCase{}, fmt.Errorf("failed to parse rspec dry run output: %s", output)
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
