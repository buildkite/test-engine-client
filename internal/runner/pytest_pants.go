package runner

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/buildkite/test-engine-client/v2/internal/plan"
	"github.com/kballard/go-shellquote"
)

type PytestPants struct {
	RunnerConfig
	resultFormat string
}

func (p PytestPants) Name() string {
	return "pytest-pants"
}

func NewPytestPants(c RunnerConfig) PytestPants {
	if c.TestCommand == "" {
		fmt.Fprintln(os.Stderr, "Error: The test command must be set via BUILDKITE_TEST_ENGINE_TEST_CMD.")
		os.Exit(1)
	}

	var resultFormat string
	switch {
	case strings.Contains(c.TestCommand, "--junit-xml"):
		resultFormat = "junit"
	case strings.Contains(c.TestCommand, "--json="):
		resultFormat = "json"
		fmt.Fprintln(os.Stderr, "Info: Python package 'buildkite-test-collector' is required and will not be verified by bktec. Please ensure it is added to the pants resolve used by pytest. See https://github.com/buildkite/test-engine-client/blob/main/docs/pytest-pants.md for more information.")
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
		if resultFormat == "junit" {
			c.ResultPath = getRandomTempFilename("pytest-results.xml")
		} else {
			c.ResultPath = getRandomTempFilename("pytest-results.json")
		}
	}

	return PytestPants{
		RunnerConfig: c,
		resultFormat: resultFormat,
	}
}

func (p PytestPants) ResultFormat() string {
	return p.resultFormat
}

func (p PytestPants) SupportedFeatures() SupportedFeatures {
	return SupportedFeatures{
		SplitByFile:     false,
		SplitByExample:  false,
		FilterTestFiles: false,
		FilterTestByTag: false,
		AutoRetry:       true,
		Mute:            true,
		Skip:            false,
	}
}

func (p PytestPants) Run(result *RunResult, testCases []plan.TestCase, retry bool) error {
	cmd, err := buildCommand(p, testCases, retry)
	if err != nil {
		return err
	}

	cmdErr := runAndForwardSignal(cmd)

	// Only rescue exit code 1 because it indicates a test failures.
	// Ref: https://docs.pytest.org/en/7.1.x/reference/exit-codes.html
	if exitError := new(exec.ExitError); errors.As(cmdErr, &exitError) && exitError.ExitCode() != 1 {
		return cmdErr
	}

	var parseErr error
	if p.resultFormat == "junit" {
		parseErr = p.runParseJUnit(result)
	} else {
		parseErr = p.runParseJSON(result)
	}

	if parseErr != nil {
		fmt.Printf("Buildkite Test Engine Client: Failed to read test output, failed tests will not be retried: %v\n", parseErr)
	}

	return cmdErr
}

func (p PytestPants) runParseJSON(result *RunResult) error {
	tests, parseErr := parseTestEngineTestResult(p.ResultPath)
	if parseErr != nil {
		return parseErr
	}

	for _, test := range tests {
		result.RecordTestResult(plan.TestCase{
			Identifier: test.ID,
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

func (p PytestPants) runParseJUnit(result *RunResult) error {
	tests, parseErr := loadAndParseJUnitXML(p.ResultPath)
	if parseErr != nil {
		return parseErr
	}

	for _, test := range tests {
		scope, path := pytestNodeIDFromJUnit(test.Classname, test.Name)
		result.RecordTestResult(plan.TestCase{
			Identifier: path,
			Format:     plan.TestCaseFormatExample,
			Scope:      scope,
			Name:       test.Name,
			Path:       path,
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

func (p PytestPants) CommandNameAndArgs(testCases []plan.TestCase, retry bool) (string, []string, error) {
	cmd := p.TestCommand
	if retry {
		cmd = p.RetryTestCommand
	}

	if strings.Contains(cmd, "{{testExamples}}") {
		return "", []string{}, fmt.Errorf("currently, bktec does not support dynamically injecting {{testExamples}}. Please ensure the test command in BUILDKITE_TEST_ENGINE_TEST_CMD does *not* include {{testExamples}}")
	}

	// Split command into parts before and after the first --
	parts := strings.SplitN(cmd, "--", 2)
	if len(parts) != 2 {
		return "", []string{}, fmt.Errorf("please ensure the test command in BUILDKITE_TEST_ENGINE_TEST_CMD includes a -- separator")
	}

	afterDash := parts[1]

	switch p.resultFormat {
	case "junit":
		if !strings.Contains(afterDash, "--junit-xml={{resultPath}}") {
			return "", []string{}, fmt.Errorf("please ensure the test command in BUILDKITE_TEST_ENGINE_TEST_CMD includes --junit-xml={{resultPath}} after the -- separator")
		}
	case "json":
		if !strings.Contains(afterDash, "--json={{resultPath}}") {
			return "", []string{}, fmt.Errorf("please ensure the test command in BUILDKITE_TEST_ENGINE_TEST_CMD includes --json={{resultPath}} after the -- separator")
		}
		if !strings.Contains(afterDash, "--merge-json") {
			return "", []string{}, fmt.Errorf("please ensure the test command in BUILDKITE_TEST_ENGINE_TEST_CMD includes --merge-json after the -- separator")
		}
	default:
		return "", []string{}, fmt.Errorf("please ensure the test command in BUILDKITE_TEST_ENGINE_TEST_CMD includes either --junit-xml={{resultPath}} or --json={{resultPath}} after the -- separator")
	}

	cmd = strings.Replace(cmd, "{{resultPath}}", p.ResultPath, 1)

	args, err := shellquote.Split(cmd)

	if err != nil {
		return "", []string{}, err
	}

	return args[0], args[1:], nil
}
