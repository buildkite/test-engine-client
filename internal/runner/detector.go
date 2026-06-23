package runner

import (
	"fmt"
	"os"

	"github.com/buildkite/test-engine-client/v2/internal/config"
)

const temporaryTestEngineClientSelectorTagsEnv = "BUILDKITE_TEST_ENGINE_CLIENT_EMIT_SELECTOR_TAGS"

func DetectRunner(cfg *config.Config) (TestRunner, error) {
	runnerConfig := RunnerConfig{
		TestRunner: cfg.TestRunner,

		locationPrefix:         cfg.LocationPrefix,
		ResultPath:             cfg.ResultPath,
		RetryTestCommand:       cfg.RetryCommand,
		TagFilters:             cfg.TagFilters,
		TestCommand:            cfg.TestCommand,
		TestFileExcludePattern: cfg.TestFileExcludePattern,
		TestFilePattern:        cfg.TestFilePattern,
		EmitSelectorTags:       emitSelectorTagsForTestEngineClient(),
		uploadToken:            cfg.UploadToken,
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
	case "nunit":
		return NewNUnit(runnerConfig), nil
	case "custom":
		return NewCustom(runnerConfig)
	default:
		// Update the error message to include the new runner
		return nil, fmt.Errorf("runner value %q is invalid, possible values are 'rspec', 'jest', 'cypress', 'playwright', 'pytest', 'pytest-pants', 'gotest', 'cucumber', 'nunit', or 'custom'", testRunner)
	}
}

func emitSelectorTagsForTestEngineClient() bool {
	// Temporary dogfood switch for validating selector-based smart splitting with
	// execution custom tags in Buildkite's own test-engine-client pipeline. This
	// should be removed when selector tags are promoted to core tags.
	return os.Getenv(temporaryTestEngineClientSelectorTagsEnv) == "true" &&
		os.Getenv("BUILDKITE_ORGANIZATION_SLUG") == "buildkite" &&
		os.Getenv("BUILDKITE_PIPELINE_SLUG") == "test-engine-client"
}
