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

type Cypress struct {
	RunnerConfig
}

func (c Cypress) Name() string {
	return "Cypress"
}

func NewCypress(c RunnerConfig) Cypress {
	if c.TestCommand == "" {
		c.TestCommand = "npx cypress run --spec {{testExamples}}"
	}

	if c.TestFilePattern == "" {
		c.TestFilePattern = "**/*.cy.{js,jsx,ts,tsx}"
	}

	return Cypress{c}
}

func (c Cypress) Run(testCases []string, retry bool) (RunResult, error) {
	cmdName, cmdArgs, err := c.commandNameAndArgs(c.TestCommand, testCases)
	if err != nil {
		return RunResult{Status: RunStatusError}, fmt.Errorf("failed to build command: %w", err)
	}

	cmd := exec.Command(cmdName, cmdArgs...)

	fmt.Printf("%s %s\n", cmdName, strings.Join(cmdArgs, " "))
	err = runAndForwardSignal(cmd)

	if err != nil {
		return RunResult{Status: RunStatusError}, err
	}

	return RunResult{Status: RunStatusPassed}, nil
}

func (c Cypress) GetFiles() ([]string, error) {
	debug.Println("Discovering test files with include pattern:", c.TestFilePattern, "exclude pattern:", c.TestFileExcludePattern)
	files, err := discoverTestFiles(c.TestFilePattern, c.TestFileExcludePattern)
	debug.Println("Discovered", len(files), "files")

	if err != nil {
		return nil, err
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("no files found with pattern %q and exclude pattern %q", c.TestFilePattern, c.TestFileExcludePattern)
	}

	return files, nil
}

func (c Cypress) GetExamples(files []string) ([]plan.TestCase, error) {
	return nil, fmt.Errorf("not supported in Cypress")
}

func (c Cypress) commandNameAndArgs(cmd string, testCases []string) (string, []string, error) {
	words, err := shellquote.Split(cmd)
	if err != nil {
		return "", []string{}, err
	}
	idx := slices.Index(words, "{{testExamples}}")
	if idx < 0 {
		words = append(words, "--spec")
		words = append(words, testCases...)
	} else {
		words = slices.Replace(words, idx, idx+1, testCases...)
	}

	return words[0], words[1:], nil
}
