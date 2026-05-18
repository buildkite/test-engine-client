package runner

import (
	"errors"
	"fmt"
	"strings"

	"github.com/buildkite/test-engine-client/v2/internal/debug"
	"github.com/buildkite/test-engine-client/v2/internal/plan"
	"github.com/kballard/go-shellquote"
)

type Custom struct {
	RunnerConfig
}

func NewCustom(r RunnerConfig) (Custom, error) {
	if r.TestCommand == "" {
		return Custom{}, errors.New("test command must be provided for custom runner")
	}

	if r.TestFilePattern == "" {
		return Custom{}, errors.New("test file pattern must be provided for custom runner")
	}

	if r.RetryTestCommand == "" {
		r.RetryTestCommand = r.TestCommand
	}

	return Custom{
		RunnerConfig: r,
	}, nil
}

func (r Custom) Name() string {
	return "Custom test runner"
}

// GetFiles returns an array of file names using the discovery pattern.
func (r Custom) GetFiles() ([]string, error) {
	debug.Println("Discovering test files with include pattern:", r.TestFilePattern, "exclude pattern:", r.TestFileExcludePattern)
	files, err := discoverTestFiles(r.TestFilePattern, r.TestFileExcludePattern)
	debug.Println("Discovered", len(files), "files")

	if err != nil {
		return nil, err
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("no files found with pattern %q and exclude pattern %q", r.TestFilePattern, r.TestFileExcludePattern)
	}

	return files, nil
}

func (r Custom) GetExamples(files []string) ([]plan.TestCase, error) {
	return nil, fmt.Errorf("not supported for custom runner")
}

func (r Custom) Run(result *RunResult, testCases []plan.TestCase, retry bool) error {
	cmd, err := buildCommand(r, testCases, retry)
	if err != nil {
		return err
	}

	cmdErr := runAndForwardSignal(cmd)

	// If the result path is not set, bubble up the error directly.
	if r.ResultPath == "" {
		return cmdErr
	}

	tests, parseErr := parseTestEngineTestResult(r.ResultPath)
	if parseErr != nil {
		fmt.Printf("Buildkite Test Engine Client: Failed to read json output: %v\n", parseErr)
		// We don't want to fail the build if we fail to parse the report,
		// therefore we return the command error (which can be nil), instead of the parse error.
		return cmdErr
	}

	for _, test := range tests {
		result.RecordTestResult(plan.TestCase{
			Identifier: test.ID,
			Format:     plan.TestCaseFormatExample,
			Scope:      test.Scope,
			Name:       test.Name,
			// We don't support retry for custom runner because each runner may have different way to target individual test cases.
			// Therefore, we just use file name and line number as the test path for now.
			Path: fmt.Sprintf("%s:%s", test.FileName, test.Location),
		}, test.Result)
	}

	// Return any command error after processing the report
	return cmdErr
}

func (r Custom) CommandNameAndArgs(testCases []plan.TestCase, retry bool) (string, []string, error) {
	cmd := r.TestCommand
	if retry {
		cmd = r.RetryTestCommand
	}

	testPaths := pathsFromTestCases(testCases)

	cmd = strings.Replace(cmd, "{{testExamples}}", strings.Join(testPaths, " "), 1)

	words, err := shellquote.Split(cmd)
	if err != nil {
		return "", []string{}, err
	}

	return words[0], words[1:], nil
}
