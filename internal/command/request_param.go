package command

import (
	"context"

	"github.com/buildkite/test-engine-client/internal/api"
	"github.com/buildkite/test-engine-client/internal/config"
	"github.com/buildkite/test-engine-client/internal/debug"
	"github.com/buildkite/test-engine-client/internal/plan"
)

// createRequestParam generates the parameters needed for a test plan request.
// For runners other than "rspec", it constructs the test plan parameters with all test files.
// For the "rspec" runner, it filters the test files through the Test Engine API and splits the filtered files into examples.
func createRequestParam(ctx context.Context, cfg config.Config, files []string, client api.Client, runner TestRunner) (api.TestPlanParams, error) {
	testFiles := []plan.TestCase{}
	for _, file := range files {
		testFiles = append(testFiles, plan.TestCase{
			Path: file,
		})
	}

	// Splitting files by example is only supported for rspec runner & cucumber
	if runner.Name() != "RSpec" && runner.Name() != "Cucumber" {
		params := api.TestPlanParams{
			Identifier:     cfg.Identifier,
			Parallelism:    cfg.Parallelism,
			MaxParallelism: cfg.MaxParallelism,
			Branch:         cfg.Branch,
			Runner:         cfg.TestRunner,
			Tests: api.TestPlanParamsTest{
				Files: testFiles,
			},
		}

		// This is a workaround for the fact that the pytest-pants runner is not
		// supported by the Test Engine API. For now, we use the pytest runner. At
		// some point, there may be a difference between the two runners, but for
		// now the response from the Test Engine API is the same for both runners.
		if cfg.TestRunner == "pytest-pants" {
			params.Runner = "pytest"
		}

		return params, nil
	}

	if cfg.SplitByExample {
		debug.Println("Splitting by example")
	}

	// The SplitByExample flag indicates whether to filter slow files for splitting by example.
	// Regardless of the flag's state, the API will still filter other files that need to be split by example, such as those containing skipped tests.
	// Therefore, we must filter and split files even when SplitByExample is disabled.
	testParams, err := filterAndSplitFiles(ctx, cfg, client, testFiles, runner)
	if err != nil {
		return api.TestPlanParams{}, err
	}

	return api.TestPlanParams{
		Identifier:     cfg.Identifier,
		Parallelism:    cfg.Parallelism,
		MaxParallelism: cfg.MaxParallelism,
		Branch:         cfg.Branch,
		Runner:         cfg.TestRunner,
		Tests:          testParams,
	}, nil
}
