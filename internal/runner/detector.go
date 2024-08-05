package runner

import (
	"errors"
	"os/exec"

	"github.com/buildkite/test-splitter/internal/config"
	"github.com/buildkite/test-splitter/internal/plan"
)

type TestRunner interface {
	Command(testCases []string) (*exec.Cmd, error)
	GetExamples(files []string) ([]plan.TestCase, error)
	GetFiles() ([]string, error)
	RetryCommand() (*exec.Cmd, error)
	Name() string
}

func DetectRunner(cfg config.Config) (TestRunner, error) {
	switch cfg.TestRunner {
	case "rspec":
		return NewRspec(Rspec{
			TestCommand:            cfg.TestCommand,
			TestFilePattern:        cfg.TestFilePattern,
			TestFileExcludePattern: cfg.TestFileExcludePattern,
			RetryTestCommand:       cfg.RetryCommand,
		}), nil
	default:
		return nil, errors.New("Runner value is invalid, possible values are 'rspec'")
	}
}
