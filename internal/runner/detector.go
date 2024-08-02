package runner

import (
	"errors"

	"github.com/buildkite/test-splitter/internal/config"
)

func DetectRunner(cfg config.Config) (*Rspec, error) {
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
