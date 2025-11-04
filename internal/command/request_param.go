package command

import (
	"context"
	"fmt"

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

	if cfg.MaxParallelism != 0 && cfg.Parallelism == 0 {
		cfg.Parallelism = 1
	}

	// Splitting files by example is only supported for rspec runner & cucumber
	if runner.Name() != "RSpec" && runner.Name() != "Cucumber" {
		params := api.TestPlanParams{
			Identifier:     cfg.Identifier,
			Parallelism:    cfg.Parallelism,
			MaxParallelism: cfg.MaxParallelism,
			TargetTime:     cfg.TargetTime.Seconds(),
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
		TargetTime:     cfg.TargetTime.Seconds(),
		Branch:         cfg.Branch,
		Runner:         cfg.TestRunner,
		Tests:          testParams,
	}, nil
}

// filterAndSplitFiles filters the test files through the Test Engine API and splits the filtered files into examples.
// It returns the test plan parameters with the examples from the filtered files and the remaining files.
// An error is returned if there is a failure in any of the process.
func filterAndSplitFiles(ctx context.Context, cfg config.Config, client api.Client, files []plan.TestCase, runner TestRunner) (api.TestPlanParamsTest, error) {
	// Filter files that need to be split.
	debug.Printf("Filtering %d files", len(files))
	filteredFiles, err := client.FilterTests(ctx, cfg.SuiteSlug, api.FilterTestsParams{
		Files: files,
		Env:   cfg,
	})
	if err != nil {
		return api.TestPlanParamsTest{}, fmt.Errorf("filter tests: %w", err)
	}

	// If no files are filtered, return the all files.
	if len(filteredFiles) == 0 {
		debug.Println("No filtered files found")
		return api.TestPlanParamsTest{
			Files: files,
		}, nil
	}

	debug.Printf("Filtered %d files", len(filteredFiles))
	debug.Printf("Getting examples for %d filtered files", len(filteredFiles))

	filteredFilesMap := make(map[string]bool, len(filteredFiles))
	filteredFilesPath := make([]string, 0, len(filteredFiles))
	for _, file := range filteredFiles {
		filteredFilesMap[file.Path] = true
		filteredFilesPath = append(filteredFilesPath, file.Path)
	}

	examples, err := runner.GetExamples(filteredFilesPath)
	if err != nil {
		return api.TestPlanParamsTest{}, fmt.Errorf("get examples: %w", err)
	}

	debug.Printf("Got %d examples within the filtered files", len(examples))

	// Get the remaining files that are not filtered.
	remainingFiles := make([]plan.TestCase, 0, len(files)-len(filteredFiles))
	for _, file := range files {
		if _, ok := filteredFilesMap[file.Path]; !ok {
			remainingFiles = append(remainingFiles, file)
		}
	}

	return api.TestPlanParamsTest{
		Examples: examples,
		Files:    remainingFiles,
	}, nil
}
