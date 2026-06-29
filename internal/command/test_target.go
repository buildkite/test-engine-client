package command

import (
	"github.com/buildkite/test-engine-client/v2/internal/config"
	"github.com/buildkite/test-engine-client/v2/internal/runner"
)

func getTestTargets(cfg *config.Config, runner runner.TestRunner, testFileList string) ([]string, error) {
	if shouldUseSelectorSplitting(cfg, runner) {
		return runner.GetSelectors()
	}

	return getTestFiles(testFileList, runner)
}
