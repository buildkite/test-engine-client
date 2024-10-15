package runner

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/buildkite/test-engine-client/internal/plan"
)

var _ = TestRunner(Cypress{})

type Cypress struct {
	RunnerConfig
}

func (c Cypress) Name() string {
	return "Cypress"
}

func NewCypress(c RunnerConfig) Cypress {
	// TODO: set default command, patterns, etc.

	return Cypress{c}
}

func (c Cypress) Run(testCases []string, retry bool) (RunResult, error) {
	// TODO: implement custom command
	cmdName := "yarn"
	cmdArgs := []string{"cypress", "run", "--spec"}

	cmdArgs = append(cmdArgs, testCases...)
	cmd := exec.Command(cmdName, cmdArgs...)

	fmt.Printf("%s %s\n", cmdName, strings.Join(cmdArgs, " "))
	err := runAndForwardSignal(cmd)

	if err == nil { // note: returning success early
		return RunResult{Status: RunStatusPassed}, nil
	}

	return RunResult{}, nil
}

func (c Cypress) GetFiles() ([]string, error) {
	return nil, nil
}

func (c Cypress) GetExamples(files []string) ([]plan.TestCase, error) {
	return nil, nil
}
