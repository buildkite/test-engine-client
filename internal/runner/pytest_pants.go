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
	return "pytest-pants"
}

func NewPytestPants(c RunnerConfig) PytestPants {
	fmt.Fprintln(os.Stderr, "Info: Python package 'buildkite-test-collector' is required and will not be verified by bktec. Please ensure it is added to the pants resolve used by pytest. See https://github.com/buildkite/test-engine-client/blob/main/docs/pytest-pants.md for more information.")

	if c.TestCommand == "" {
		fmt.Fprintln(os.Stderr, "Error: The test command must be set via BUILDKITE_TEST_ENGINE_TEST_CMD.")
		os.Exit(1)
	}

	if c.TestFilePattern != "" || c.TestFileExcludePattern != "" {
		fmt.Fprintln(os.Stderr, "Warning: Pants test runner variant does not support discovering test files. Please ensure the test command is set correctly via BUILDKITE_TEST_ENGINE_TEST_CMD and do *not* set either:")
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

func (p PytestPants) SupportedFeatures() SupportedFeatures {
	return SupportedFeatures{
		SplitByFile:     false,
		SplitByExample:  false,
		FilterTestFiles: false,
		AutoRetry:       true,
		Mute:            true,
		Skip:            false,
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

	tests, parseErr := parseTestEngineTestResult(p.ResultPath)

	if parseErr != nil {
		fmt.Println("Buildkite Test Engine Client: Failed to read json output, failed tests will not be retried:", parseErr)
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
		return "", []string{}, fmt.Errorf("currently, bktec does not support dynamically injecting {{testExamples}}. Please ensure the test command in BUILDKITE_TEST_ENGINE_TEST_CMD does *not* include {{testExamples}}")
	}

	// Split command into parts before and after the first --
	parts := strings.SplitN(cmd, "--", 2)
	if len(parts) != 2 {
		return "", []string{}, fmt.Errorf("please ensure the test command in BUILDKITE_TEST_ENGINE_TEST_CMD includes a -- separator")
	}

	// Check that both required flags are after the --
	afterDash := parts[1]
	if !strings.Contains(afterDash, "--json={{resultPath}}") {
		return "", []string{}, fmt.Errorf("please ensure the test command in BUILDKITE_TEST_ENGINE_TEST_CMD includes --json={{resultPath}} after the -- separator")
	}

	if !strings.Contains(afterDash, "--merge-json") {
		return "", []string{}, fmt.Errorf("please ensure the test command in BUILDKITE_TEST_ENGINE_TEST_CMD includes --merge-json after the -- separator")
	}

	cmd = strings.Replace(cmd, "{{resultPath}}", p.ResultPath, 1)

	args, err := shellquote.Split(cmd)

	if err != nil {
		return "", []string{}, err
	}

	return args[0], args[1:], nil
}
