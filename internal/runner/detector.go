package runner

import (
	"errors"

	"github.com/buildkite/test-engine-client/internal/config"
	"github.com/buildkite/test-engine-client/internal/plan"
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
	Run(result *RunResult, testCases []plan.TestCase, retry bool) error
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
	case "jest":
		return NewJest(runnerConfig), nil
	case "cypress":
		return NewCypress(runnerConfig), nil
	case "playwright":
		return NewPlaywright(runnerConfig), nil
	default:
		return nil, errors.New("runner value is invalid, possible values are 'rspec', 'jest', 'cypress', 'playwright'")
	}
}
