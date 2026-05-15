package runner

import (
	"fmt"
	"slices"
	"strings"

	"github.com/buildkite/test-engine-client/v2/internal/debug"
	"github.com/buildkite/test-engine-client/v2/internal/plan"
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

	if c.RetryTestCommand == "" {
		c.RetryTestCommand = c.TestCommand
	}

	if c.TestFilePattern == "" {
		c.TestFilePattern = "**/*.cy.{js,jsx,ts,tsx}"
	}

	return Cypress{
		RunnerConfig: c,
	}
}

func (c Cypress) Run(result *RunResult, testCases []plan.TestCase, retry bool) error {
	cmd, err := buildCommand(c, testCases, retry)
	if err != nil {
		return err
	}

	err = runAndForwardSignal(cmd)

	return err
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

func (c Cypress) CommandNameAndArgs(testCases []plan.TestCase, retry bool) (string, []string, error) {
	cmd := c.TestCommand
	if retry {
		cmd = c.RetryTestCommand
	}
	words, err := shellquote.Split(cmd)
	if err != nil {
		return "", []string{}, err
	}
	idx := slices.Index(words, "{{testExamples}}")

	testPaths := pathsFromTestCases(testCases)

	specs := strings.Join(testPaths, ",")
	if idx < 0 {
		words = append(words, "--spec", specs)
	} else {
		words[idx] = specs
	}

	return words[0], words[1:], nil
}
