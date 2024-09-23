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

var _ = TestRunner(Jest{})

type Jest struct {
	RunnerConfig
}

func NewJest(j RunnerConfig) Jest {
	if j.TestCommand == "" {
		j.TestCommand = "yarn test {{testExamples}} --json --testLocationInResults --outputFile {{resultPath}}"
	}

	if j.TestFilePattern == "" {
		j.TestFilePattern = "**/{__tests__/**/*,*.spec,*.test}.{ts,js,tsx,jsx}"
	}

	if j.TestFileExcludePattern == "" {
		j.TestFileExcludePattern = "node_modules"
	}

	if j.RetryTestCommand == "" {
		j.RetryTestCommand = "yarn test --testNamePattern '{{testNamePattern}}' --json --testLocationInResults --outputFile {{resultPath}}"
	}

	return Jest{j}
}

func (j Jest) Name() string {
	return "Jest"
}

// GetFiles returns an array of file names using the discovery pattern.
func (j Jest) GetFiles() ([]string, error) {
	debug.Println("Discovering test files with include pattern:", j.TestFilePattern, "exclude pattern:", j.TestFileExcludePattern)
	files, err := discoverTestFiles(j.TestFilePattern, j.TestFileExcludePattern)
	debug.Println("Discovered", len(files), "files")

	if err != nil {
		return nil, err
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("no files found with pattern %q and exclude pattern %q", j.TestFilePattern, j.TestFileExcludePattern)
	}

	return files, nil
}

func (j Jest) Run(testCases []string, retry bool) (RunResult, error) {
	var cmd *exec.Cmd
	var err error

	if !retry {
		commandName, commandArgs, err := j.commandNameAndArgs(j.TestCommand, testCases)
		if err != nil {
			return RunResult{Status: RunStatusError}, fmt.Errorf("failed to build command: %w", err)
		}

		cmd = exec.Command(commandName, commandArgs...)
	} else {
		commandName, commandArgs, err := j.retryCommandNameAndArgs(j.RetryTestCommand, testCases)
		if err != nil {
			return RunResult{Status: RunStatusError}, fmt.Errorf("failed to build command: %w", err)
		}

		cmd = exec.Command(commandName, commandArgs...)
	}

	err = runAndForwardSignal(cmd)

	if err == nil { // note: returning success early
		return RunResult{Status: RunStatusPassed}, nil
	}

	if ProcessSignaledError := new(ProcessSignaledError); errors.As(err, &ProcessSignaledError) {
		return RunResult{Status: RunStatusError}, err
	}

	if exitError := new(exec.ExitError); errors.As(err, &exitError) {
		report, parseErr := j.ParseReport(j.ResultPath)
		if parseErr != nil {
			fmt.Println("Buildkite Test Engine Client: Failed to read Jest output, tests will not be retried.")
			return RunResult{Status: RunStatusError}, err
		}

		if report.NumFailedTests > 0 {
			var failedTests []string
			for _, testResult := range report.TestResults {
				for _, example := range testResult.AssertionResults {
					if example.Status == "failed" {
						failedTests = append(failedTests, example.Name)
					}
				}
			}

			return RunResult{Status: RunStatusFailed, FailedTests: failedTests}, nil
		}
	}

	return RunResult{Status: RunStatusError}, err
}

type JestExample struct {
	Name   string `json:"fullName"`
	Status string `json:"status"`
}

type JestReport struct {
	NumFailedTests int
	TestResults    []struct {
		AssertionResults []JestExample
	}
}

func (j Jest) ParseReport(path string) (JestReport, error) {
	var report JestReport
	data, err := os.ReadFile(path)
	if err != nil {
		return JestReport{}, fmt.Errorf("failed to read Jest output: %v", err)
	}

	if err := json.Unmarshal(data, &report); err != nil {
		return JestReport{}, fmt.Errorf("failed to parse Jest output: %s", err)
	}

	return report, nil
}

func (j Jest) commandNameAndArgs(cmd string, testCases []string) (string, []string, error) {
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

	outputIdx := slices.Index(words, "{{resultPath}}")
	if outputIdx < 0 {
		err := fmt.Errorf("couldn't find '{{resultPath}}' sentinel in command, exiting.")
		return "", []string{}, err
	}
	words = slices.Replace(words, outputIdx, outputIdx+1, j.ResultPath)

	return words[0], words[1:], nil
}

func (j Jest) retryCommandNameAndArgs(cmd string, testCases []string) (string, []string, error) {
	words, err := shellquote.Split(cmd)
	if err != nil {
		return "", []string{}, err
	}

	idx := slices.Index(words, "{{testNamePattern}}")
	if idx < 0 {
		err := fmt.Errorf("couldn't find '{{testNamePattern}}' sentinel in retry command")
		return "", []string{}, err
	}

	testNamePattern := fmt.Sprintf("(%s)", strings.Join(testCases, "|"))

	words = slices.Replace(words, idx, idx+1, testNamePattern)

	outputIdx := slices.Index(words, "{{resultPath}}")
	if outputIdx < 0 {
		err := fmt.Errorf("couldn't find '{{resultPath}}' sentinel in retry command, exiting.")
		return "", []string{}, err
	}
	words = slices.Replace(words, outputIdx, outputIdx+1, j.ResultPath)

	return words[0], words[1:], err
}

func (j Jest) GetExamples(files []string) ([]plan.TestCase, error) {
	return nil, fmt.Errorf("not supported in Jest")
}
