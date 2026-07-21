package command

import (
	"context"
	"fmt"
	"strings"

	"github.com/buildkite/test-engine-client/v3/internal/api"
	"github.com/buildkite/test-engine-client/v3/internal/config"
	"github.com/buildkite/test-engine-client/v3/internal/debug"
	"github.com/buildkite/test-engine-client/v3/internal/plan"
	"github.com/buildkite/test-engine-client/v3/internal/runner"
)

type requestMode uint8

const (
	requestByFile requestMode = iota
	requestBySelector
)

type requestTarget struct {
	value string
	file  api.TestPlanFile
}

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
func createRequestParam(ctx context.Context, cfg *config.Config, testTargets []string, client api.Client, runner runner.TestRunner) (api.TestPlanParams, error) {
	selectorSplitting := shouldUseSelectorSplitting(runner)
	mode := requestByFile
	if selectorSplitting {
		mode = requestBySelector
	}

	testParams, err := requestTestParams(ctx, cfg, client, newRequestTargets(testTargets, runner.LocationPrefix()), runner, mode)
	if err != nil {
		return api.TestPlanParams{}, err
	}

	params := api.TestPlanParams{
		Identifier:     cfg.Identifier,
		Parallelism:    cfg.Parallelism,
		MaxParallelism: cfg.MaxParallelism,
		TargetTime:     cfg.TargetTime.Seconds(),
		Branch:         cfg.Branch,
		Selection:      buildSelectionParams(cfg.SelectionStrategy, cfg.SelectionParams),
		Metadata:       cfg.Metadata,
		Runner:         cfg.TestRunner,
		Tests:          testParams,
	}
	if selectorSplitting {
		params.LocationPrefix = cfg.LocationPrefix
	}

	return params, nil
}

func newRequestTargets(values []string, prefix string) []requestTarget {
	targets := make([]requestTarget, 0, len(values))
	for _, value := range values {
		targets = append(targets, requestTarget{
			value: value,
			file:  api.TestPlanFile{Path: prefixPath(value, prefix)},
		})
	}
	return targets
}

func requestTestParams(ctx context.Context, cfg *config.Config, client api.Client, targets []requestTarget, runner runner.TestRunner, mode requestMode) (api.TestPlanParamsTest, error) {
	features := runner.SupportedFeatures()
	if !features.SplitByExample {
		return encodeRequestTargets(targets, mode), nil
	}

	if mode == requestByFile && cfg.SplitByExample {
		debug.Println("Splitting by example")
	}

	// If tag filtering is enabled, we must split all files to allow to enable filtering.
	// Tag filtering is currently only supported for pytest.
	if cfg.TagFilters != "" && runner.Name() == "pytest" {
		return expandAllTargets(targets, runner)
	}

	if mode == requestBySelector && !shouldFilterAndSplitSelectorFiles(cfg, runner) {
		return encodeRequestTargets(targets, mode), nil
	}

	label := "files"
	if mode == requestBySelector {
		label = "selector-backed files"
	}
	return filterAndExpandTargets(ctx, cfg, client, targets, runner, mode, label)
}

func shouldUseSelectorSplitting(runner runner.TestRunner) bool {
	return runner.SupportedFeatures().SplitBySelector
}

func shouldFilterAndSplitSelectorFiles(cfg *config.Config, runner runner.TestRunner) bool {
	features := runner.SupportedFeatures()
	needsFilteredFiles := cfg.SplitByExample || features.Skip
	return features.SplitByExample && needsFilteredFiles
}

func selectorParamsFromValues(values []string) []api.TestPlanParamsSelector {
	selectors := make([]api.TestPlanParamsSelector, 0, len(values))
	for _, value := range values {
		selectors = append(selectors, api.TestPlanParamsSelector{Value: value})
	}
	return selectors
}

func exampleParamsFromTestCases(testCases []plan.TestCase) []api.TestPlanExample {
	examples := make([]api.TestPlanExample, 0, len(testCases))
	for _, testCase := range testCases {
		examples = append(examples, api.TestPlanExample{
			Format:     testCase.Format,
			Identifier: testCase.Identifier,
			Name:       testCase.Name,
			Path:       testCase.Path,
			Scope:      testCase.Scope,
		})
	}
	return examples
}

func encodeRequestTargets(targets []requestTarget, mode requestMode) api.TestPlanParamsTest {
	if mode == requestBySelector {
		values := make([]string, 0, len(targets))
		for _, target := range targets {
			values = append(values, target.value)
		}
		return api.TestPlanParamsTest{Selectors: selectorParamsFromValues(values)}
	}

	files := make([]api.TestPlanFile, 0, len(targets))
	for _, target := range targets {
		files = append(files, target.file)
	}
	return api.TestPlanParamsTest{Files: files}
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

func getExamplesWithPrefix(filePaths []string, testRunner runner.TestRunner) ([]plan.TestCase, error) {
	prefix := testRunner.LocationPrefix()
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

	discoverer, ok := testRunner.(runner.ExampleDiscoverer)
	if !ok {
		return nil, fmt.Errorf("runner %q advertises split by example but does not implement example discovery", testRunner.Name())
	}

	examples, err := discoverer.GetExamples(trimmedPaths)
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

// expandAllTargets splits every target into examples to support tag filtering.
func expandAllTargets(targets []requestTarget, runner runner.TestRunner) (api.TestPlanParamsTest, error) {
	debug.Printf("Splitting all %d files", len(targets))
	filePaths := make([]string, 0, len(targets))
	for _, target := range targets {
		filePaths = append(filePaths, target.file.Path)
	}

	examples, err := getExamplesWithPrefix(filePaths, runner)
	if err != nil {
		return api.TestPlanParamsTest{}, err
	}

	debug.Printf("Got %d examples from all files", len(examples))

	return api.TestPlanParamsTest{
		Examples: exampleParamsFromTestCases(examples),
	}, nil
}

// filterAndExpandTargets filters targets through the Test Engine API, expands filtered targets
// into examples, and encodes the remaining targets according to the request mode.
func filterAndExpandTargets(ctx context.Context, cfg *config.Config, client api.Client, targets []requestTarget, runner runner.TestRunner, mode requestMode, label string) (api.TestPlanParamsTest, error) {
	files := make([]api.TestPlanFile, 0, len(targets))
	for _, target := range targets {
		files = append(files, target.file)
	}

	examples, filteredFilesMap, err := filterAndExpandFiles(ctx, cfg, client, files, runner, label)
	if err != nil {
		return api.TestPlanParamsTest{}, err
	}

	if len(filteredFilesMap) == 0 {
		return encodeRequestTargets(targets, mode), nil
	}

	remainingTargets := make([]requestTarget, 0, len(targets)-len(filteredFilesMap))
	for _, target := range targets {
		if _, ok := filteredFilesMap[target.file.Path]; !ok {
			remainingTargets = append(remainingTargets, target)
		}
	}

	params := encodeRequestTargets(remainingTargets, mode)
	params.Examples = examples
	return params, nil
}

func filterAndExpandFiles(ctx context.Context, cfg *config.Config, client api.Client, files []api.TestPlanFile, runner runner.TestRunner, label string) ([]api.TestPlanExample, map[string]bool, error) {
	debug.Printf("Filtering %d %s", len(files), label)
	filteredFiles, err := client.FilterTests(ctx, cfg.SuiteSlug, api.FilterTestsParams{
		Files:          files,
		Env:            cfg.EnvPayload(),
		MaxParallelism: cfg.MaxParallelism,
		TargetTime:     cfg.TargetTime.Seconds(),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("filter tests: %w", err)
	}

	filteredFilesMap := make(map[string]bool, len(filteredFiles))
	if len(filteredFiles) == 0 {
		debug.Printf("No filtered %s found", label)
		return nil, filteredFilesMap, nil
	}

	debug.Printf("Filtered %d %s", len(filteredFiles), label)
	debug.Printf("Getting examples for %d filtered %s", len(filteredFiles), label)

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
		return nil, nil, err
	}

	debug.Printf("Got %d examples within the filtered %s", len(examples), label)
	return exampleParamsFromTestCases(examples), filteredFilesMap, nil
}
