package command

import (
	"github.com/buildkite/test-engine-client/v2/internal/config"
	"github.com/buildkite/test-engine-client/v2/internal/runner"
)

func getTestTargets(cfg *config.Config, runner runner.TestRunner, testFileList string) ([]string, error) {
	if shouldUseSelectorSplitting(cfg, runner) {
		selectors, err := runner.GetSelectors()
		if err != nil {
			return nil, err
		}
		return selectors, nil
	} else {
		files, err := getTestFiles(testFileList, runner)
		if err != nil {
			return nil, err
		}
		return files, nil
	}
}
