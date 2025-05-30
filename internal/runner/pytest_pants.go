package runner

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/buildkite/test-engine-client/internal/plan"
	"github.com/kballard/go-shellquote"
)

type PytestPants struct {
	RunnerConfig
}

func (p PytestPants) Name() string {
	return "pytest"
}

func NewPytestPants(c RunnerConfig) PytestPants {
	fmt.Fprintln(os.Stderr, "Info: Python package 'buildkite-test-collector' is required and will not be verified by bktec. Please ensure it is added to the pants resolve used by pytest.")

	if c.TestCommand == "" {
		fmt.Fprintln(os.Stderr, "Error: The test command must be set via BUILDKITE_TEST_ENGINE_TEST_COMMAND.")
		os.Exit(1)
	}

	if c.TestFilePattern != "" || c.TestFileExcludePattern != "" {
		fmt.Fprintln(os.Stderr, "Warning: Pants test runner variant does not support discovering test files. Please ensure the test command is set correctly via BUILDKITE_TEST_ENGINE_TEST_COMMAND and do *not* set either:")
		fmt.Fprintf(os.Stderr, "  BUILDKITE_TEST_ENGINE_TEST_FILE_PATTERN=%q\n", c.TestFilePattern)
		fmt.Fprintf(os.Stderr, "  BUILDKITE_TEST_ENGINE_TEST_FILE_EXCLUDE_PATTERN=%q\n", c.TestFileExcludePattern)
	}

	if c.TestFilePattern == "" {
		c.TestFilePattern = "**/{*_test,test_*}.py"
	}

	if c.RetryTestCommand == "" {
		c.RetryTestCommand = c.TestCommand
	}

	if c.ResultPath == "" {
		c.ResultPath = getRandomTempFilename()
	}

	return PytestPants{
		RunnerConfig: c,
	}
}

func (p PytestPants) Run(result *RunResult, testCases []plan.TestCase, retry bool) error {
	testPaths := make([]string, len(testCases))
	for i, tc := range testCases {
		testPaths[i] = tc.Path
	}

	command := p.TestCommand

	if retry {
		command = p.RetryTestCommand
	}

	cmdName, cmdArgs, err := p.commandNameAndArgs(command, testPaths)
	if err != nil {
		return fmt.Errorf("failed to build command: %w", err)
	}

	cmd := exec.Command(cmdName, cmdArgs...)

	err = runAndForwardSignal(cmd)

	// Only rescue exit code 1 because it indicates a test failures.
	// Ref: https://docs.pytest.org/en/7.1.x/reference/exit-codes.html
	if exitError := new(exec.ExitError); errors.As(err, &exitError) && exitError.ExitCode() != 1 {
		return err
	}

	tests, parseErr := ParsePytestCollectorResult(p.ResultPath)

	if parseErr != nil {
		fmt.Println("Buildkite Test Engine Client: Failed to read json output, failed tests will not be retried.")
		return err
	}

	for _, test := range tests {

		result.RecordTestResult(plan.TestCase{
			Identifier: test.Id,
			Format:     plan.TestCaseFormatExample,
			Scope:      test.Scope,
			Name:       test.Name,
			// pytest can execute individual test using node id, which is a filename, classname (if any), and function, separated by `::`.
			// Ref: https://docs.pytest.org/en/6.2.x/usage.html#nodeids
			Path: fmt.Sprintf("%s::%s", test.Scope, test.Name),
		}, test.Result)
	}

	return nil
}

func (p PytestPants) GetFiles() ([]string, error) {
	return []string{}, nil
}

func (p PytestPants) GetExamples(files []string) ([]plan.TestCase, error) {
	return nil, fmt.Errorf("not supported in pytest pants")
}

func (p PytestPants) commandNameAndArgs(cmd string, testCases []string) (string, []string, error) {
	if strings.Contains(cmd, "{{testExamples}}") {
		return "", []string{}, fmt.Errorf("currently, bktec does not support dynamically injecting {{testExamples}}. Please ensure the test command in BUILDKITE_TEST_ENGINE_TEST_COMMAND does *not* include {{testExamples}}")
	}

	if !strings.Contains(cmd, "{{resultPath}}") {
		return "", []string{}, fmt.Errorf("please ensure the test command in BUILDKITE_TEST_ENGINE_TEST_COMMAND includes --json={{resultPath}}")
	}

	cmd = strings.Replace(cmd, "{{resultPath}}", p.ResultPath, 1)

	args, err := shellquote.Split(cmd)

	if err != nil {
		return "", []string{}, err
	}

	return args[0], args[1:], nil
}
