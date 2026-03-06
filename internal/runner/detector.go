package runner

import (
	"fmt"

	"github.com/buildkite/test-engine-client/internal/config"
)

func DetectRunner(cfg *config.Config) (TestRunner, error) {
	runnerConfig := RunnerConfig{
		TestRunner:             cfg.TestRunner,
		TestCommand:            cfg.TestCommand,
		TestFilePattern:        cfg.TestFilePattern,
		TestFileExcludePattern: cfg.TestFileExcludePattern,
		RetryTestCommand:       cfg.RetryCommand,
		ResultPath:             cfg.ResultPath,
		TagFilters:             cfg.TagFilters,
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
