package runner

import (
	"errors"
	"os/exec"

	"github.com/buildkite/test-splitter/internal/plan"
)

type RunnerConfig struct {
	TestCommand            string
	TestFilePattern        string
	TestFileExcludePattern string
	RetryTestCommand       string
}

type TestRunner interface {
	Command(testCases []string) (*exec.Cmd, error)
	GetExamples(files []string) ([]plan.TestCase, error)
	GetFiles() ([]string, error)
	RetryCommand() (*exec.Cmd, error)
	Name() string
}

func DetectRunner(runner string, cfg RunnerConfig) (TestRunner, error) {
	switch runner {
	case "rspec":
		return NewRspec(cfg), nil
	default:
		return nil, errors.New("Runner value is invalid, possible values are 'rspec'")
	}
}
