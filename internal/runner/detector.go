package runner

import (
	"fmt"

	"github.com/buildkite/test-engine-client/internal/config"
	"github.com/buildkite/test-engine-client/internal/plan"
)

type RunnerConfig struct {
	TestRunner string

	LocationPrefix string
	// ResultPath is used internally so bktec can read result from Test Runner.
	// User typically don't need to worry about setting this except in in RSpec and playwright.
	// In playwright, for example, it can only be configured via a config file, therefore it's mandatory for user to set.
	ResultPath             string
	RetryTestCommand       string
	TagFilters             string
	TestCommand            string
	TestFileExcludePattern string
	TestFilePattern        string
}

func (c RunnerConfig) GetLocationPrefix() string {
	return c.LocationPrefix
}

type TestRunner interface {
	Run(result *RunResult, testCases []plan.TestCase, retry bool) error
	GetExamples(files []string) ([]plan.TestCase, error)
	GetFiles() ([]string, error)
	Name() string
	GetLocationPrefix() string
}

func DetectRunner(cfg *config.Config) (TestRunner, error) {
	runnerConfig := RunnerConfig{
		TestRunner: cfg.TestRunner,

		LocationPrefix:         cfg.LocationPrefix,
		ResultPath:             cfg.ResultPath,
		RetryTestCommand:       cfg.RetryCommand,
		TagFilters:             cfg.TagFilters,
		TestCommand:            cfg.TestCommand,
		TestFileExcludePattern: cfg.TestFileExcludePattern,
		TestFilePattern:        cfg.TestFilePattern,
	}

	switch testRunner := cfg.TestRunner; testRunner {
	case "rspec":
		return NewRspec(runnerConfig), nil
	case "jest":
		return NewJest(runnerConfig), nil
	case "cypress":
		return NewCypress(runnerConfig), nil
	case "playwright":
		return NewPlaywright(runnerConfig), nil
	case "pytest":
		return NewPytest(runnerConfig), nil
	case "pytest-pants":
		return NewPytestPants(runnerConfig), nil
	case "gotest":
		return NewGoTest(runnerConfig), nil
	case "cucumber":
		return NewCucumber(runnerConfig), nil
	case "custom":
		return NewCustom(runnerConfig)
	default:
		// Update the error message to include the new runner
		return nil, fmt.Errorf("runner value %q is invalid, possible values are 'rspec', 'jest', 'cypress', 'playwright', 'pytest', 'pytest-pants', 'gotest', 'cucumber', or 'custom'", testRunner)
	}
}
