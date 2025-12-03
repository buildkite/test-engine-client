package runner

import (
	"errors"
	"fmt"
	"os/exec"
	"slices"

	"github.com/buildkite/test-engine-client/internal/debug"
	"github.com/buildkite/test-engine-client/internal/plan"
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
	testPaths := make([]string, len(testCases))

	for i, tc := range testCases {
		testPaths[i] = tc.Path
	}
	cmdName, cmdArgs, err := r.commandNameAndArgs(r.TestCommand, testPaths)

	if err != nil {
		return fmt.Errorf("failed to build command: %w", err)
	}

	cmd := exec.Command(cmdName, cmdArgs...)

	err = runAndForwardSignal(cmd)

	return err
}

func (r Custom) commandNameAndArgs(cmd string, testCases []string) (string, []string, error) {
	words, err := shellquote.Split(cmd)
	if err != nil {
		return "", []string{}, err
	}
	idx := slices.Index(words, "{{testExamples}}")

	if idx >= 0 {
		words = slices.Replace(words, idx, idx+1, testCases...)
	}

	return words[0], words[1:], nil
}
