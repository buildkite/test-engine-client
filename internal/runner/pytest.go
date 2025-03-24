package runner

import (
	"fmt"
	"os/exec"
	"slices"

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
		c.TestCommand = "pytest {{testExamples}}"
	}

	if c.TestFilePattern == "" {
		c.TestFilePattern = "**/{*_test,test_*}.py"
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
	cmdName, cmdArgs, err := p.commandNameAndArgs(p.TestCommand, testPaths)
	if err != nil {
		return fmt.Errorf("failed to build command: %w", err)
	}

	cmd := exec.Command(cmdName, cmdArgs...)

	err = runAndForwardSignal(cmd)

	return err
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

	return words[0], words[1:], nil
}
