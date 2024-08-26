package runner

import (
	"errors"

	"github.com/buildkite/test-splitter/internal/config"
	"github.com/buildkite/test-splitter/internal/plan"
)

type RunnerConfig struct {
	TestRunner             string
	TestCommand            string
	TestFilePattern        string
	TestFileExcludePattern string
	RetryTestCommand       string
	ResultPath             string
}

type TestRunner interface {
	Run(testCases []string, retry bool) (RunResult, error)
	GetExamples(files []string) ([]plan.TestCase, error)
	GetFiles() ([]string, error)
	Name() string
}

func DetectRunner(cfg config.Config) (TestRunner, error) {
	var runnerConfig = RunnerConfig{
		TestRunner:             cfg.TestRunner,
		TestCommand:            cfg.TestCommand,
		TestFilePattern:        cfg.TestFilePattern,
		TestFileExcludePattern: cfg.TestFileExcludePattern,
		RetryTestCommand:       cfg.RetryCommand,
		ResultPath:             cfg.ResultPath,
	}

	switch cfg.TestRunner {
	case "rspec":
		return NewRspec(runnerConfig), nil
	default:
		return nil, errors.New("runner value is invalid, possible values are 'rspec'")
	}
}
