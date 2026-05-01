package command

import (
	"context"
	"fmt"
	"strings"

	"github.com/buildkite/test-engine-client/internal/api"
	"github.com/buildkite/test-engine-client/internal/config"
	"github.com/buildkite/test-engine-client/internal/debug"
	"github.com/buildkite/test-engine-client/internal/plan"
)

// createRequestParam generates the parameters needed for a test plan request.
//
// For the Rspec, Cucumber, Pytest, and Playwright runners, it fetches test files through the Test Engine API
// that are slow or contain skipped tests. These files are then split into examples
// The remaining files are sent as is.
//
// If location prefix is configured, the file paths are prefixed when making the request to the Test Engine API,
// so that it can correctly identify the files.
//
// If tag filtering is enabled, all files are split into examples to support filtering.
// Currently only the Pytest runner supports tag filtering.
func createRequestParam(ctx context.Context, cfg *config.Config, files []string, client api.Client, runner TestRunner) (api.TestPlanParams, error) {
	testFiles := []plan.TestCase{}

	for _, file := range files {
		testFiles = append(testFiles, plan.TestCase{
			Path: prefixPath(file, runner.LocationPrefix()),
		})
	}

	// Splitting files by example is only supported for rspec, cucumber, pytest, and playwright runners
	if runner.Name() != "RSpec" && runner.Name() != "Cucumber" && runner.Name() != "pytest" && runner.Name() != "Playwright" {
		params := api.TestPlanParams{
			Identifier:     cfg.Identifier,
			Parallelism:    cfg.Parallelism,
			MaxParallelism: cfg.MaxParallelism,
			TargetTime:     cfg.TargetTime.Seconds(),
			Branch:         cfg.Branch,
			Selection:      buildSelectionParams(cfg.SelectionStrategy, cfg.SelectionParams),
			Metadata:       cfg.Metadata,
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

	var testParams api.TestPlanParamsTest
	var err error

	// If tag filtering is enabled, we must split all files to allow to enable filtering.
	// Tag filtering is currently only supported for pytest.
	if cfg.TagFilters != "" && runner.Name() == "pytest" {
		testParams, err = splitAllFiles(testFiles, runner)
	} else {
		// The SplitByExample flag indicates whether to split slow files into examples.
		// Regardless of the flag's state, the API will still return other test files that need to
		// be split by example, such as those containing skipped tests.
		// Therefore, we must fetch and split files even when SplitByExample is disabled.
		testParams, err = filterAndSplitFiles(ctx, cfg, client, testFiles, runner)
	}

	if err != nil {
		return api.TestPlanParams{}, err
	}

	return api.TestPlanParams{
		Identifier:     cfg.Identifier,
		Parallelism:    cfg.Parallelism,
		MaxParallelism: cfg.MaxParallelism,
		TargetTime:     cfg.TargetTime.Seconds(),
		Branch:         cfg.Branch,
		Selection:      buildSelectionParams(cfg.SelectionStrategy, cfg.SelectionParams),
		Metadata:       cfg.Metadata,
		Runner:         cfg.TestRunner,
		Tests:          testParams,
	}, nil
}

// buildSelectionParams returns the selection payload sent to the Test Engine
// API, or nil when no strategy was requested.
//
// Beyond the empty string, a handful of human-intuitive sentinel values
// ("none", "off", "false", "disabled", "no", plus whitespace and case
// variants) are also coerced to nil. This is defence-in-depth against a
// recurring foot-gun: pipelines that set BUILDKITE_TEST_ENGINE_SELECTION_STRATEGY
// to a human-readable "turn it off" value get a confusing 400 from the
// server, which only accepts the strict allowlist (random, manual,
// rspec_changed_files, xgboost). See TE-5641 / TE-5638 for context. Every
// other value, including typos, is still forwarded verbatim so the backend
// remains authoritative for strategy validation.
func buildSelectionParams(strategy string, params map[string]string) *api.SelectionParams {
	switch strings.ToLower(strings.TrimSpace(strategy)) {
	case "", "none", "off", "false", "disabled", "no":
		return nil
	}

	return &api.SelectionParams{
		Strategy: strategy,
		Params:   params,
	}
}

func getExamplesWithPrefix(filePaths []string, runner TestRunner) ([]plan.TestCase, error) {
	prefix := runner.LocationPrefix()
	trimmedPaths := make([]string, len(filePaths))

	// runner.GetExamples will call the test runner with the file paths.
	// Because the test runner expects the file paths without the prefix (it doesn't know about the prefix),
	// we need to trim the prefix before passing the file paths to the runner.
	for i, filePath := range filePaths {
		path, err := trimFilePathPrefix(filePath, prefix)
		if err != nil {
			return nil, fmt.Errorf("trim file path prefix: %w", err)
		}
		trimmedPaths[i] = path
	}

	examples, err := runner.GetExamples(trimmedPaths)
	if err != nil {
		return nil, fmt.Errorf("get examples: %w", err)
	}

	// After getting the examples from the runner, we need to re-apply the prefix to the example paths
	// before sending them to the Test Engine API.
	if prefix != "" {
		for i := range examples {
			// The 'Identifier' field in an example may not always be a file path.
			// Since the Test Engine API only uses the 'Path' field, we only apply the prefix to 'Path'.
			examples[i].Path = prefixPath(examples[i].Path, prefix)
		}
	}

	return examples, nil
}

// Splits all the test files into examples to support tag filtering.
func splitAllFiles(files []plan.TestCase, runner TestRunner) (api.TestPlanParamsTest, error) {
	debug.Printf("Splitting all %d files", len(files))
	filePaths := make([]string, 0, len(files))
	for _, file := range files {
		filePaths = append(filePaths, file.Path)
	}

	examples, err := getExamplesWithPrefix(filePaths, runner)
	if err != nil {
		return api.TestPlanParamsTest{}, err
	}

	debug.Printf("Got %d examples from all files", len(examples))

	return api.TestPlanParamsTest{
		Examples: examples,
	}, nil
}

// filterAndSplitFiles filters the test files through the Test Engine API and splits the filtered files into examples.
// It returns the test plan parameters with the examples from the filtered files and the remaining files that are not filtered.
// An error is returned if there is a failure in any of the process.
func filterAndSplitFiles(ctx context.Context, cfg *config.Config, client api.Client, allTestFiles []plan.TestCase, runner TestRunner) (api.TestPlanParamsTest, error) {
	// Filter files that need to be split.
	debug.Printf("Filtering %d files", len(allTestFiles))
	filteredFiles, err := client.FilterTests(ctx, cfg.SuiteSlug, api.FilterTestsParams{
		Files: allTestFiles,
		Env:   cfg,
	})
	if err != nil {
		return api.TestPlanParamsTest{}, fmt.Errorf("filter tests: %w", err)
	}

	// If no files are filtered, return all the files.
	if len(filteredFiles) == 0 {
		debug.Println("No filtered files found")
		return api.TestPlanParamsTest{
			Files: allTestFiles,
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

	// The filtered files returned by the API include the location prefix in their paths,
	// so we should trim the prefix before passing the file paths to the runner to get the examples,
	// then re-apply the prefix to the example paths collected by the runner.
	examples, err := getExamplesWithPrefix(filteredFilesPath, runner)
	if err != nil {
		return api.TestPlanParamsTest{}, err
	}

	debug.Printf("Got %d examples within the filtered files", len(examples))

	// Get the remaining files that are not filtered.
	remainingFiles := make([]plan.TestCase, 0, len(allTestFiles)-len(filteredFiles))
	for _, file := range allTestFiles {
		if _, ok := filteredFilesMap[file.Path]; !ok {
			remainingFiles = append(remainingFiles, file)
		}
	}

	return api.TestPlanParamsTest{
		Examples: examples,
		Files:    remainingFiles,
	}, nil
}
