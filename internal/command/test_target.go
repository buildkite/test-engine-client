package command

import (
	"github.com/buildkite/test-engine-client/v2/internal/config"
	"github.com/buildkite/test-engine-client/v2/internal/runner"
)

func getTestTargets(cfg *config.Config, testRunner runner.TestRunnerWithTargetDiscovery, testFileList string) ([]string, error) {
	if shouldUseSelectorSplitting(cfg, testRunner) {
		if cfg.SelectorListPath != "" {
			return getRowsFromFile(cfg.SelectorListPath)
		}
		return testRunner.DiscoverTestTargets()
	}

	if testFileList != "" {
		return getRowsFromFile(testFileList)
	}
	return testRunner.DiscoverTestTargets()
}
