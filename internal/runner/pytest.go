package runner

import (
	"encoding/json"
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
	if c.TestCommand == "" {
		c.TestCommand = "pytest {{testExamples}} --json={{resultPath}}"
	}

	if c.TestFilePattern == "" {
		c.TestFilePattern = "**/{*_test,test_*}.py"
	}

	if c.RetryTestCommand == "" {
		c.RetryTestCommand = c.TestCommand
	}

	if c.ResultPath == "" && checkPythonPackageInstalled("buildkite-test-collector") {
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

func (p Pytest) GetExamples(files []string) ([]plan.TestCase, error) {
	return nil, fmt.Errorf("not supported in pytest")
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

// TestEngineTest represents a Test Engine test result object.
// Some attributes such as `history` and `failure_reason` are omitted as they are not needed by bktec.
// Ref: https://buildkite.com/docs/test-engine/importing-json#json-test-results-data-reference-test-result-objects
//
// Currently, only pytest uses result from test collector. If we use this somewhere else in the future, we may want to extract this struct.
type TestEngineTest struct {
	Id       string
	Name     string
	Scope    string
	Location string
	FileName string `json:"file_name,omitempty"`
	Result   TestStatus
}

func ParsePytestCollectorResult(path string) ([]TestEngineTest, error) {
	var results []TestEngineTest
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read json: %v", err)
	}

	if err := json.Unmarshal(data, &results); err != nil {
		return nil, fmt.Errorf("failed to parse json: %v", err)
	}

	return results, nil
}

func getRandomTempFilename() string {
	tempDir, err := os.MkdirTemp("", "bktec-pytest-*")
	if err != nil {
		panic(err)
	}
	return filepath.Join(tempDir, "pytest-results.json")
}

func checkPythonPackageInstalled(pkgName string) bool {
	cmd := exec.Command("pip", "show", pkgName)
	_, err := cmd.CombinedOutput()

	return err == nil
}
