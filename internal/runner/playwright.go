package runner

import (
	"fmt"
	"os/exec"
	"slices"
	"strings"

	"github.com/buildkite/test-engine-client/internal/debug"
	"github.com/buildkite/test-engine-client/internal/plan"
	"github.com/kballard/go-shellquote"
)

var _ = TestRunner(Playwright{})

type Playwright struct {
	RunnerConfig
}

func (p Playwright) Name() string {
	return "Playwright"
}

func NewPlaywright(p RunnerConfig) Playwright {
	if p.TestCommand == "" {
		p.TestCommand = "npx playwright test"
	}

	if p.TestFilePattern == "" {
		p.TestFilePattern = "**/{*.spec,*.test}.{ts,js}"
	}

	if p.TestFileExcludePattern == "" {
		p.TestFileExcludePattern = "**/node_modules"
	}

	return Playwright{p}
}

func (p Playwright) Run(testCases []string, retry bool) (RunResult, error) {
	cmdName, cmdArgs, err := p.commandNameAndArgs(p.TestCommand, testCases)
	if err != nil {
		return RunResult{Status: RunStatusError}, fmt.Errorf("failed to build command: %w", err)
	}

	cmd := exec.Command(cmdName, cmdArgs...)

	fmt.Printf("%s %s\n", cmdName, strings.Join(cmdArgs, " "))
	err = runAndForwardSignal(cmd)

	if err == nil { // note: returning success early
		return RunResult{Status: RunStatusPassed}, nil
	}

	return RunResult{Status: RunStatusError}, err
}

func (p Playwright) commandNameAndArgs(cmd string, testCases []string) (string, []string, error) {
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

func (p Playwright) GetFiles() ([]string, error) {
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

func (p Playwright) GetExamples(files []string) ([]plan.TestCase, error) {
	return nil, fmt.Errorf("not supported in Playwright")
}
