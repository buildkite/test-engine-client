package runner

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/buildkite/test-engine-client/internal/debug"
	"github.com/buildkite/test-engine-client/internal/plan"
	"github.com/kballard/go-shellquote"
)

type Pytest struct {
	RunnerConfig
}

func (p Pytest) Name() string {
	return "pytest"
}

func NewPytest(c RunnerConfig) Pytest {
	if !checkPythonPackageInstalled("buildkite_test_collector") { // python import only use underscore
		fmt.Fprintln(os.Stderr, "Error: Required Python package 'buildkite-test-collector' is not installed.")
		fmt.Fprintln(os.Stderr, "Please install it with: pip install buildkite-test-collector.")
		os.Exit(1)
	}

	if c.TestCommand == "" {
		c.TestCommand = "pytest {{testExamples}} --json={{resultPath}}"
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

	return Pytest{
		RunnerConfig: c,
	}
}

func (p Pytest) Run(result *RunResult, testCases []plan.TestCase, retry bool) error {
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

func (p Pytest) GetFiles() ([]string, error) {
	debug.Println("Discovering test files with include pattern:", p.TestFilePattern, "exclude pattern:", p.TestFileExcludePattern)
	files, err := discoverTestFiles(p.TestFilePattern, p.TestFileExcludePattern)
	debug.Println("Discovered", len(files), "files")

	if err != nil {
		return nil, err
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("no files found with pattern %q and exclude pattern %q", p.TestFilePattern, p.TestFileExcludePattern)
	}

	return files, nil
}

// GetExamples returns an array of test examples within the given files.
// It uses `pytest --collect-only -q` to enumerate individual tests.
func (p Pytest) GetExamples(files []string) ([]plan.TestCase, error) {
	if len(files) == 0 {
		return []plan.TestCase{}, nil
	}

	args := append([]string{"--collect-only", "-q"}, files...)
	cmd := exec.Command("pytest", args...)

	output, err := cmd.Output()
	if err != nil {
		// Include stderr in error message for debugging
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("pytest collection failed: %s", exitErr.Stderr)
		}
		return nil, fmt.Errorf("pytest collection failed: %w", err)
	}

	return parsePytestCollectOutput(string(output))
}

// parsePytestCollectOutput parses the output of `pytest --collect-only -q`
// and returns a list of test cases.
//
// Example output:
//
//	test_sample.py::test_happy
//	test_auth.py::TestLogin::test_success
//	test_auth.py::test_param[value1]
//
//	3 tests collected in 0.05s
func parsePytestCollectOutput(output string) ([]plan.TestCase, error) {
	var testCases []plan.TestCase

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)

		// Skip empty lines and summary lines (lines without ::)
		if line == "" || !strings.Contains(line, "::") {
			continue
		}

		// Parse node ID: "file.py::TestClass::test_method" or "file.py::test_func"
		testCases = append(testCases, mapNodeIdToTestCase(line))
	}

	return testCases, nil
}

// mapNodeIdToTestCase converts a pytest node ID to a TestCase.
// Node ID format: file_path::class::method or file_path::function
// Must match the format used by buildkite-test-collector for pytest.
func mapNodeIdToTestCase(nodeId string) plan.TestCase {
	parts := strings.SplitN(nodeId, "::", 2)
	scope := parts[0] // file path
	name := ""
	if len(parts) > 1 {
		name = parts[1] // everything after first ::
	}

	return plan.TestCase{
		Identifier: nodeId,
		Path:       nodeId,
		Scope:      scope,
		Name:       name,
		Format:     plan.TestCaseFormatExample,
	}
}

func (p Pytest) commandNameAndArgs(cmd string, testCases []string) (string, []string, error) {
	testExamples := strings.Join(testCases, " ")

	if strings.Contains(cmd, "{{testExamples}}") {
		cmd = strings.Replace(cmd, "{{testExamples}}", testExamples, 1)
	} else {
		cmd = cmd + " " + testExamples
	}

	cmd = strings.Replace(cmd, "{{resultPath}}", p.ResultPath, 1)

	args, err := shellquote.Split(cmd)

	if err != nil {
		return "", []string{}, err
	}

	return args[0], args[1:], nil
}

func getRandomTempFilename() string {
	tempDir, err := os.MkdirTemp("", "bktec-pytest-*")
	if err != nil {
		panic(err)
	}
	return filepath.Join(tempDir, "pytest-results.json")
}

func checkPythonPackageInstalled(pkgName string) bool {
	// This is the most reliable way I can find. Hopefully it should work regardless of if user uses pip, poetry or uv
	pythonCmd := exec.Command("python", "-c", "import importlib.util, sys; print(importlib.util.find_spec(sys.argv[1]) is not None)", pkgName)
	output, err := pythonCmd.Output()

	return err == nil && strings.TrimSpace(string(output)) == "True"
}
