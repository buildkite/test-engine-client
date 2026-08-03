package command

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/buildkite/test-engine-client/v3/internal/api"
	"github.com/buildkite/test-engine-client/v3/internal/config"
	"github.com/buildkite/test-engine-client/v3/internal/plan"
	"github.com/buildkite/test-engine-client/v3/internal/runner"
	"github.com/google/go-cmp/cmp"
)

func TestCreateRequestParams(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `
{
	"tests": [
		{ "path": "testdata/rspec/spec/fruits/banana_spec.rb", "reason": "slow file" },
		{ "path": "testdata/rspec/spec/fruits/fig_spec.rb", "reason": "slow file" }
	]
}`)
	}))
	defer svr.Close()

	cfg := config.Config{
		OrganizationSlug: "my-org",
		SuiteSlug:        "my-suite",
		Identifier:       "identifier",
		Parallelism:      7,
		Branch:           "",
		TestRunner:       "rspec",
	}

	client := api.NewClient(api.ClientConfig{
		ServerBaseURL: svr.URL,
	})
	files := []string{
		"testdata/rspec/spec/fruits/apple_spec.rb",
		"testdata/rspec/spec/fruits/banana_spec.rb",
		"testdata/rspec/spec/fruits/cherry_spec.rb",
		"testdata/rspec/spec/fruits/dragonfruit_spec.rb",
		"testdata/rspec/spec/fruits/elderberry_spec.rb",
		"testdata/rspec/spec/fruits/fig_spec.rb",
		"testdata/rspec/spec/fruits/grape_spec.rb",
	}

	got, err := createRequestParam(context.Background(), &cfg, files, *client, runner.Rspec{
		RunnerConfig: runner.RunnerConfig{
			TestCommand: "rspec",
		},
	})
	if err != nil {
		t.Errorf("createRequestParam() error = %v", err)
	}

	// filtered files: banana_spec.rb, fig_spec.rb
	// the rest: apple_spec.rb, cherry_spec.rb, dragonfruit_spec.rb, elderberry_spec.rb, grape_spec.rb
	want := api.TestPlanParams{
		Identifier:  "identifier",
		Parallelism: 7,
		Branch:      "",
		Runner:      "rspec",
		Tests: api.TestPlanParamsTest{
			Examples: []api.TestPlanExample{
				{
					Identifier: "./testdata/rspec/spec/fruits/banana_spec.rb[1:1]",
					Name:       "is yellow",
					Path:       "./testdata/rspec/spec/fruits/banana_spec.rb[1:1]",
					Scope:      "Banana",
				},
				{
					Identifier: "./testdata/rspec/spec/fruits/banana_spec.rb[1:2:1]",
					Name:       "is green",
					Path:       "./testdata/rspec/spec/fruits/banana_spec.rb[1:2:1]",
					Scope:      "Banana when not ripe",
				},
				{
					Identifier: "./testdata/rspec/spec/fruits/fig_spec.rb[1:1]",
					Name:       "is purple",
					Path:       "./testdata/rspec/spec/fruits/fig_spec.rb[1:1]",
					Scope:      "Fig",
				},
			},
			Selectors: []api.TestPlanParamsSelector{
				{Value: "testdata/rspec/spec/fruits/apple_spec.rb"},
				{Value: "testdata/rspec/spec/fruits/cherry_spec.rb"},
				{Value: "testdata/rspec/spec/fruits/dragonfruit_spec.rb"},
				{Value: "testdata/rspec/spec/fruits/elderberry_spec.rb"},
				{Value: "testdata/rspec/spec/fruits/grape_spec.rb"},
			},
		},
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("createRequestParam() diff (-got +want):\n%s", diff)
	}
}

func TestCreateRequestParams_NonRSpec(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `
{
	"tests": [
		{ "path": "testdata/jest/banana.spec.js", "reason": "slow file" },
		{ "path": "testdata/jest/fig.spec.js", "reason": "slow file" }
	]
}`)
	}))
	defer svr.Close()

	runners := []runner.TestRunner{
		runner.Jest{}, runner.Cypress{},
	}

	for _, r := range runners {
		t.Run(r.Name(), func(t *testing.T) {
			cfg := config.Config{
				OrganizationSlug: "my-org",
				SuiteSlug:        "my-suite",
				Identifier:       "identifier",
				Parallelism:      7,
				Branch:           "",
				TestRunner:       r.Name(),
			}

			client := api.NewClient(api.ClientConfig{
				ServerBaseURL: svr.URL,
			})
			files := []string{
				"testdata/fruits/apple.spec.js",
				"testdata/fruits/banana.spec.js",
				"testdata/fruits/cherry.spec.js",
			}

			got, err := createRequestParam(context.Background(), &cfg, files, *client, r)
			if err != nil {
				t.Errorf("createRequestParam() error = %v", err)
			}

			want := api.TestPlanParams{
				Identifier:  "identifier",
				Parallelism: 7,
				Branch:      "",
				Runner:      r.Name(),
				Tests: api.TestPlanParamsTest{
					Selectors: []api.TestPlanParamsSelector{
						{Value: "testdata/fruits/apple.spec.js"},
						{Value: "testdata/fruits/banana.spec.js"},
						{Value: "testdata/fruits/cherry.spec.js"},
					},
				},
			}

			if diff := cmp.Diff(got, want); diff != "" {
				t.Errorf("createRequestParam() diff (-got +want):\n%s", diff)
			}
		})
	}
}

func TestCreateRequestParams_GoTestSelectorSplitting(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected API request to %s", r.URL.Path)
	}))
	defer svr.Close()

	cfg := config.Config{
		Identifier:        "identifier",
		Parallelism:       2,
		MaxParallelism:    4,
		Branch:            "main",
		TestRunner:        "gotest",
		LocationPrefix:    "my/project",
		SelectionStrategy: "least-reliable",
		SelectionParams: map[string]string{
			"top": "100",
		},
		Metadata: map[string]string{
			"git_diff": "line1\nline2",
		},
	}

	testRunner, err := runner.DetectRunner(&cfg)
	if err != nil {
		t.Fatalf("DetectRunner() error = %v", err)
	}

	client := api.NewClient(api.ClientConfig{
		ServerBaseURL: svr.URL,
	})
	packages := []string{
		"github.com/buildkite/test-engine-client/internal/api",
		"github.com/buildkite/test-engine-client/internal/runner",
	}

	got, err := createRequestParam(context.Background(), &cfg, packages, *client, testRunner)
	if err != nil {
		t.Errorf("createRequestParam() error = %v", err)
	}

	want := api.TestPlanParams{
		Identifier:     "identifier",
		Parallelism:    2,
		MaxParallelism: 4,
		Branch:         "main",
		LocationPrefix: "my/project",
		Runner:         "gotest",
		Selection: &api.SelectionParams{
			Strategy: "least-reliable",
			Params: map[string]string{
				"top": "100",
			},
		},
		Metadata: map[string]string{
			"git_diff": "line1\nline2",
		},
		Tests: api.TestPlanParamsTest{
			Selectors: []api.TestPlanParamsSelector{
				{Value: "github.com/buildkite/test-engine-client/internal/api"},
				{Value: "github.com/buildkite/test-engine-client/internal/runner"},
			},
		},
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("createRequestParam() diff (-got +want):\n%s", diff)
	}
}

func TestCreateRequestParams_RSpecSelectorSplittingExpandsFilteredFiles(t *testing.T) {
	filterRequestCount := 0
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		filterRequestCount++
		fmt.Fprint(w, `
{
	"tests": [
		{ "path": "testdata/rspec/spec/fruits/banana_spec.rb", "reason": "file contains 1 or more skipped tests" }
	]
}`)
	}))
	defer svr.Close()

	cfg := config.Config{
		OrganizationSlug: "my-org",
		SuiteSlug:        "my-suite",
		Identifier:       "identifier",
		Parallelism:      2,
		Branch:           "main",
		TestRunner:       "rspec",
	}

	client := api.NewClient(api.ClientConfig{
		ServerBaseURL: svr.URL,
	})
	selectors := []string{
		"testdata/rspec/spec/fruits/apple_spec.rb",
		"testdata/rspec/spec/fruits/banana_spec.rb",
		"testdata/rspec/spec/fruits/cherry_spec.rb",
	}

	testRunner := metadataTestRunner{
		name: "rspec",
		supportedFeatures: runner.SupportedFeatures{
			SplitByExample:  true,
			SplitBySelector: true,
			FilterTestFiles: true,
			Skip:            true,
		},
		examples: []plan.TestCase{
			{
				Identifier: "testdata/rspec/spec/fruits/banana_spec.rb[1:1]",
				Name:       "is yellow",
				Path:       "testdata/rspec/spec/fruits/banana_spec.rb[1:1]",
				Scope:      "Banana",
			},
		},
	}

	got, err := createRequestParam(context.Background(), &cfg, selectors, *client, testRunner)
	if err != nil {
		t.Errorf("createRequestParam() error = %v", err)
	}

	if filterRequestCount != 1 {
		t.Errorf("filter request count = %d, want 1", filterRequestCount)
	}

	want := api.TestPlanParams{
		Identifier:  "identifier",
		Parallelism: 2,
		Branch:      "main",
		Runner:      "rspec",
		Tests: api.TestPlanParamsTest{
			Examples: []api.TestPlanExample{
				{
					Identifier: "testdata/rspec/spec/fruits/banana_spec.rb[1:1]",
					Name:       "is yellow",
					Path:       "testdata/rspec/spec/fruits/banana_spec.rb[1:1]",
					Scope:      "Banana",
				},
			},
			Selectors: []api.TestPlanParamsSelector{
				{Value: "testdata/rspec/spec/fruits/apple_spec.rb"},
				{Value: "testdata/rspec/spec/fruits/cherry_spec.rb"},
			},
		},
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("createRequestParam() diff (-got +want):\n%s", diff)
	}
}

func TestCreateRequestParams_RSpecSelectorSplittingNoFilteredFiles(t *testing.T) {
	filterRequestCount := 0
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		filterRequestCount++
		fmt.Fprint(w, `{"tests": []}`)
	}))
	defer svr.Close()

	cfg := config.Config{
		OrganizationSlug: "my-org",
		SuiteSlug:        "my-suite",
		Identifier:       "identifier",
		Parallelism:      2,
		Branch:           "main",
		TestRunner:       "rspec",
	}

	client := api.NewClient(api.ClientConfig{
		ServerBaseURL: svr.URL,
	})
	selectors := []string{
		"testdata/rspec/spec/fruits/apple_spec.rb",
		"testdata/rspec/spec/fruits/banana_spec.rb",
	}

	testRunner := metadataTestRunner{
		name: "rspec",
		supportedFeatures: runner.SupportedFeatures{
			SplitByExample:  true,
			SplitBySelector: true,
			FilterTestFiles: true,
			Skip:            true,
		},
	}

	got, err := createRequestParam(context.Background(), &cfg, selectors, *client, testRunner)
	if err != nil {
		t.Errorf("createRequestParam() error = %v", err)
	}

	if filterRequestCount != 1 {
		t.Errorf("filter request count = %d, want 1", filterRequestCount)
	}

	want := api.TestPlanParams{
		Identifier:  "identifier",
		Parallelism: 2,
		Branch:      "main",
		Runner:      "rspec",
		Tests: api.TestPlanParamsTest{
			Selectors: []api.TestPlanParamsSelector{
				{Value: "testdata/rspec/spec/fruits/apple_spec.rb"},
				{Value: "testdata/rspec/spec/fruits/banana_spec.rb"},
			},
		},
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("createRequestParam() diff (-got +want):\n%s", diff)
	}
}

func TestCreateRequestParams_RSpecSelectorSplittingWithLocationPrefix(t *testing.T) {
	var gotFilterParams api.FilterTestsParams
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotFilterParams); err != nil {
			t.Fatalf("decoding filter_tests request: %v", err)
		}
		fmt.Fprint(w, `
{
	"tests": [
		{ "path": "my/project/testdata/rspec/spec/fruits/banana_spec.rb", "reason": "file contains 1 or more skipped tests" }
	]
}`)
	}))
	defer svr.Close()

	cfg := config.Config{
		OrganizationSlug: "my-org",
		SuiteSlug:        "my-suite",
		Identifier:       "identifier",
		Parallelism:      2,
		MaxParallelism:   10,
		TargetTime:       5 * time.Minute,
		Branch:           "main",
		TestRunner:       "rspec",
		LocationPrefix:   "my/project",
	}

	client := api.NewClient(api.ClientConfig{
		ServerBaseURL: svr.URL,
	})
	selectors := []string{
		"testdata/rspec/spec/fruits/apple_spec.rb",
		"testdata/rspec/spec/fruits/banana_spec.rb",
		"testdata/rspec/spec/fruits/cherry_spec.rb",
	}

	testRunner := metadataTestRunner{
		name:           "rspec",
		locationPrefix: "my/project",
		supportedFeatures: runner.SupportedFeatures{
			SplitByExample:  true,
			SplitBySelector: true,
			FilterTestFiles: true,
			Skip:            true,
		},
		examples: []plan.TestCase{
			{
				Identifier: "testdata/rspec/spec/fruits/banana_spec.rb[1:1]",
				Name:       "is yellow",
				Path:       "testdata/rspec/spec/fruits/banana_spec.rb[1:1]",
				Scope:      "Banana",
			},
		},
	}

	got, err := createRequestParam(context.Background(), &cfg, selectors, *client, testRunner)
	if err != nil {
		t.Errorf("createRequestParam() error = %v", err)
	}

	wantFilterFiles := []api.TestPlanFile{
		{Path: "my/project/testdata/rspec/spec/fruits/apple_spec.rb"},
		{Path: "my/project/testdata/rspec/spec/fruits/banana_spec.rb"},
		{Path: "my/project/testdata/rspec/spec/fruits/cherry_spec.rb"},
	}
	if diff := cmp.Diff(gotFilterParams.Files, wantFilterFiles); diff != "" {
		t.Errorf("filter_tests files diff (-got +want):\n%s", diff)
	}

	if gotFilterParams.MaxParallelism != 10 {
		t.Errorf("filter_tests max_parallelism = %d, want 10", gotFilterParams.MaxParallelism)
	}
	if gotFilterParams.TargetTime != 300 {
		t.Errorf("filter_tests target_time = %v, want 300", gotFilterParams.TargetTime)
	}

	want := api.TestPlanParams{
		Identifier:     "identifier",
		Parallelism:    2,
		MaxParallelism: 10,
		TargetTime:     300,
		Branch:         "main",
		LocationPrefix: "my/project",
		Runner:         "rspec",
		Tests: api.TestPlanParamsTest{
			Examples: []api.TestPlanExample{
				{
					Identifier: "testdata/rspec/spec/fruits/banana_spec.rb[1:1]",
					Name:       "is yellow",
					Path:       "my/project/testdata/rspec/spec/fruits/banana_spec.rb[1:1]",
					Scope:      "Banana",
				},
			},
			Selectors: []api.TestPlanParamsSelector{
				{Value: "testdata/rspec/spec/fruits/apple_spec.rb"},
				{Value: "testdata/rspec/spec/fruits/cherry_spec.rb"},
			},
		},
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("createRequestParam() diff (-got +want):\n%s", diff)
	}
}

func TestCreateRequestParams_RSpecSelectorSplittingWithDotLocationPrefix(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `
{
	"tests": [
		{ "path": "./testdata/rspec/spec/fruits/banana_spec.rb", "reason": "file contains 1 or more skipped tests" }
	]
}`)
	}))
	defer svr.Close()

	cfg := config.Config{
		OrganizationSlug: "my-org",
		SuiteSlug:        "my-suite",
		Identifier:       "identifier",
		Parallelism:      2,
		Branch:           "main",
		TestRunner:       "rspec",
		LocationPrefix:   "./",
	}

	client := api.NewClient(api.ClientConfig{
		ServerBaseURL: svr.URL,
	})
	selectors := []string{
		"testdata/rspec/spec/fruits/apple_spec.rb",
		"testdata/rspec/spec/fruits/banana_spec.rb",
	}

	testRunner := metadataTestRunner{
		name:           "rspec",
		locationPrefix: "./",
		supportedFeatures: runner.SupportedFeatures{
			SplitByExample:  true,
			SplitBySelector: true,
			FilterTestFiles: true,
			Skip:            true,
		},
		examples: []plan.TestCase{
			{
				Identifier: "./testdata/rspec/spec/fruits/banana_spec.rb[1:1]",
				Name:       "is yellow",
				Path:       "./testdata/rspec/spec/fruits/banana_spec.rb[1:1]",
				Scope:      "Banana",
			},
		},
	}

	got, err := createRequestParam(context.Background(), &cfg, selectors, *client, testRunner)
	if err != nil {
		t.Errorf("createRequestParam() error = %v", err)
	}

	want := api.TestPlanParams{
		Identifier:     "identifier",
		Parallelism:    2,
		Branch:         "main",
		LocationPrefix: "./",
		Runner:         "rspec",
		Tests: api.TestPlanParamsTest{
			Examples: []api.TestPlanExample{
				{
					Identifier: "./testdata/rspec/spec/fruits/banana_spec.rb[1:1]",
					Name:       "is yellow",
					Path:       "./testdata/rspec/spec/fruits/banana_spec.rb[1:1]",
					Scope:      "Banana",
				},
			},
			Selectors: []api.TestPlanParamsSelector{
				{Value: "testdata/rspec/spec/fruits/apple_spec.rb"},
			},
		},
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("createRequestParam() diff (-got +want):\n%s", diff)
	}
}
func TestShouldFilterAndSplitSelectorFiles(t *testing.T) {
	custom, err := runner.NewCustom(runner.RunnerConfig{
		TestCommand:     "echo {{testExamples}}",
		TestFilePattern: "tests/**/*",
	})
	if err != nil {
		t.Fatalf("NewCustom() error = %v", err)
	}

	cases := []struct {
		name       string
		testRunner runner.TestRunner
		split      bool
		want       bool
	}{
		{
			name:       "RSpec selector-backed files support skipped-test expansion",
			testRunner: runner.NewRspec(runner.RunnerConfig{}),
			want:       true,
		},
		{
			name:       "Cucumber selector-backed files support skipped-test expansion",
			testRunner: runner.NewCucumber(runner.RunnerConfig{}),
			want:       true,
		},
		{
			name:       "Go selectors are not file-backed skipped-test runners",
			testRunner: runner.NewGoTest(runner.RunnerConfig{}),
			want:       false,
		},
		{
			name:       "custom selectors do not support Test Engine skipped tests",
			testRunner: custom,
			want:       false,
		},
		{
			name:       "selector-backed file runners without skip support do not use skipped-test expansion",
			testRunner: runner.NewJest(runner.RunnerConfig{}),
			want:       false,
		},
		{
			name:       "Playwright selector-backed files support slow-file expansion when split by example is enabled",
			testRunner: runner.NewPlaywright(runner.RunnerConfig{}),
			split:      true,
			want:       true,
		},
		{
			name:       "Playwright selector-backed files do not expand when split by example is disabled",
			testRunner: runner.NewPlaywright(runner.RunnerConfig{}),
			want:       false,
		},
		{
			name:       "pytest selector-backed files support slow-file expansion when split by example is enabled",
			testRunner: runner.Pytest{},
			split:      true,
			want:       true,
		},
		{
			name:       "pytest selector-backed files support skipped-test expansion",
			testRunner: runner.Pytest{},
			want:       true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := config.Config{SplitByExample: c.split}
			if got := shouldFilterAndSplitSelectorFiles(&cfg, c.testRunner); got != c.want {
				t.Errorf("shouldFilterAndSplitSelectorFiles(%s) = %v, want %v", c.testRunner.Name(), got, c.want)
			}
		})
	}
}

func TestEncodeRequestTargets(t *testing.T) {
	targets := newRequestTargets([]string{"first_test", "second_test"}, "project")
	want := api.TestPlanParamsTest{
		Selectors: []api.TestPlanParamsSelector{{Value: "first_test"}, {Value: "second_test"}},
	}

	if diff := cmp.Diff(encodeRequestTargets(targets), want); diff != "" {
		t.Errorf("encodeRequestTargets() diff (-got +want):\n%s", diff)
	}
}

func TestFilterAndExpandTargets(t *testing.T) {
	var gotFilterParams api.FilterTestsParams
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotFilterParams); err != nil {
			t.Fatalf("decoding filter_tests request: %v", err)
		}
		fmt.Fprint(w, `{"tests":[{"path":"project/middle_test","reason":"slow file"}]}`)
	}))
	defer svr.Close()

	client := api.NewClient(api.ClientConfig{ServerBaseURL: svr.URL})
	targets := newRequestTargets([]string{"first_test", "middle_test", "third_test"}, "project")
	var exampleFiles []string
	testRunner := metadataTestRunner{
		name:           "playwright",
		locationPrefix: "project",
		exampleFiles:   &exampleFiles,
		examples: []plan.TestCase{
			{
				Format:     plan.TestCaseFormatExample,
				Identifier: "middle_test::example",
				Name:       "example",
				Path:       "middle_test::example",
			},
		},
	}

	want := api.TestPlanParamsTest{
		Selectors: []api.TestPlanParamsSelector{{Value: "first_test"}, {Value: "third_test"}},
	}

	got, err := filterAndExpandTargets(context.Background(), &config.Config{}, *client, targets, testRunner)
	if err != nil {
		t.Fatalf("filterAndExpandTargets() error = %v", err)
	}

	want.Examples = []api.TestPlanExample{
		{
			Format:     plan.TestCaseFormatExample,
			Identifier: "middle_test::example",
			Name:       "example",
			Path:       "project/middle_test::example",
		},
	}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("filterAndExpandTargets() diff (-got +want):\n%s", diff)
	}

	wantFilterFiles := []api.TestPlanFile{{Path: "project/first_test"}, {Path: "project/middle_test"}, {Path: "project/third_test"}}
	if diff := cmp.Diff(gotFilterParams.Files, wantFilterFiles); diff != "" {
		t.Errorf("filter_tests files diff (-got +want):\n%s", diff)
	}
	if diff := cmp.Diff(exampleFiles, []string{"middle_test"}); diff != "" {
		t.Errorf("GetExamples() files diff (-got +want):\n%s", diff)
	}
}

func TestCreateRequestParams_SelectorSplittingExpandsSlowFilesForSplitByExampleRunners(t *testing.T) {
	for _, testRunner := range []string{"playwright", "pytest"} {
		t.Run(testRunner, func(t *testing.T) {
			filterRequestCount := 0
			svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				filterRequestCount++
				fmt.Fprint(w, `
{
	"tests": [
		{ "path": "tests/slow_test", "reason": "slow file" }
	]
}`)
			}))
			defer svr.Close()

			cfg := config.Config{
				OrganizationSlug: "my-org",
				SuiteSlug:        "my-suite",
				Identifier:       "identifier",
				Parallelism:      2,
				TestRunner:       testRunner,
				SplitByExample:   true,
			}

			client := api.NewClient(api.ClientConfig{ServerBaseURL: svr.URL})
			selectors := []string{"tests/fast_test", "tests/slow_test"}
			stubRunner := metadataTestRunner{
				name: testRunner,
				supportedFeatures: runner.SupportedFeatures{
					SplitByExample:  true,
					SplitBySelector: true,
					FilterTestFiles: true,
				},
				examples: []plan.TestCase{
					{
						Identifier: "tests/slow_test::example",
						Path:       "tests/slow_test::example",
						Scope:      "tests/slow_test",
						Name:       "example",
						Format:     plan.TestCaseFormatExample,
					},
				},
			}

			got, err := createRequestParam(context.Background(), &cfg, selectors, *client, stubRunner)
			if err != nil {
				t.Fatalf("createRequestParam() error = %v", err)
			}

			if filterRequestCount != 1 {
				t.Errorf("filter request count = %d, want 1", filterRequestCount)
			}

			want := api.TestPlanParams{
				Identifier:  "identifier",
				Parallelism: 2,
				Runner:      testRunner,
				Tests: api.TestPlanParamsTest{
					Examples: exampleParamsFromTestCases(stubRunner.examples),
					Selectors: []api.TestPlanParamsSelector{
						{Value: "tests/fast_test"},
					},
				},
			}

			if diff := cmp.Diff(got, want); diff != "" {
				t.Errorf("createRequestParam() diff (-got +want):\n%s", diff)
			}
		})
	}
}

func TestCreateRequestParams_WithSelectionAndMetadata_NonRSpec(t *testing.T) {
	cfg := config.Config{
		Identifier:        "identifier",
		Parallelism:       2,
		Branch:            "main",
		TestRunner:        "jest",
		SelectionStrategy: "least-reliable",
		SelectionParams: map[string]string{
			"top": "100",
		},
		Metadata: map[string]string{
			"git_diff": "line1\nline2",
			"source":   "cli",
		},
	}

	client := api.NewClient(api.ClientConfig{
		ServerBaseURL: "http://example.com",
	})

	files := []string{
		"testdata/fruits/apple.spec.js",
		"testdata/fruits/banana.spec.js",
	}

	got, err := createRequestParam(context.Background(), &cfg, files, *client, runner.Jest{})
	if err != nil {
		t.Errorf("createRequestParam() error = %v", err)
	}

	want := api.TestPlanParams{
		Identifier:  "identifier",
		Parallelism: 2,
		Branch:      "main",
		Runner:      "jest",
		Selection: &api.SelectionParams{
			Strategy: "least-reliable",
			Params: map[string]string{
				"top": "100",
			},
		},
		Metadata: map[string]string{
			"git_diff": "line1\nline2",
			"source":   "cli",
		},
		Tests: api.TestPlanParamsTest{
			Selectors: []api.TestPlanParamsSelector{
				{Value: "testdata/fruits/apple.spec.js"},
				{Value: "testdata/fruits/banana.spec.js"},
			},
		},
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("createRequestParam() diff (-got +want):\n%s", diff)
	}
}

func TestCreateRequestParams_WithSelectionAndMetadata_SplitAllFilesBranch(t *testing.T) {
	cfg := config.Config{
		Identifier:        "identifier",
		Parallelism:       2,
		Branch:            "main",
		TestRunner:        "pytest",
		TagFilters:        "team:frontend",
		SelectionStrategy: "percent",
		SelectionParams: map[string]string{
			"percent": "40",
		},
		Metadata: map[string]string{
			"git_diff": "line1\nline2",
		},
	}

	client := api.NewClient(api.ClientConfig{
		ServerBaseURL: "http://example.com",
	})

	files := []string{
		"test_sample.py",
	}

	stubRunner := metadataTestRunner{
		name: "pytest",
		examples: []plan.TestCase{
			{
				Identifier: "test_sample.py::test_happy",
				Path:       "test_sample.py::test_happy",
				Scope:      "test_sample.py",
				Name:       "test_happy",
				Format:     plan.TestCaseFormatExample,
			},
		},
	}

	got, err := createRequestParam(context.Background(), &cfg, files, *client, stubRunner)
	if err != nil {
		t.Errorf("createRequestParam() error = %v", err)
	}

	want := api.TestPlanParams{
		Identifier:  "identifier",
		Parallelism: 2,
		Branch:      "main",
		Runner:      "pytest",
		Selection: &api.SelectionParams{
			Strategy: "percent",
			Params: map[string]string{
				"percent": "40",
			},
		},
		Metadata: map[string]string{
			"git_diff": "line1\nline2",
		},
		Tests: api.TestPlanParamsTest{
			Examples: []api.TestPlanExample{
				{
					Identifier: "test_sample.py::test_happy",
					Path:       "test_sample.py::test_happy",
					Scope:      "test_sample.py",
					Name:       "test_happy",
					Format:     plan.TestCaseFormatExample,
				},
			},
		},
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("createRequestParam() diff (-got +want):\n%s", diff)
	}
}

type metadataTestRunner struct {
	name              string
	examples          []plan.TestCase
	exampleFiles      *[]string
	locationPrefix    string
	supportedFeatures runner.SupportedFeatures
}

func (r metadataTestRunner) SupportedFeatures() runner.SupportedFeatures {
	if r.supportedFeatures != (runner.SupportedFeatures{}) {
		return r.supportedFeatures
	}

	return runner.SupportedFeatures{
		SplitByExample: true,
	}
}

func (r metadataTestRunner) Name() string {
	return r.name
}

func (r metadataTestRunner) GetExamples(files []string) ([]plan.TestCase, error) {
	if r.exampleFiles != nil {
		*r.exampleFiles = append((*r.exampleFiles)[:0], files...)
	}
	return r.examples, nil
}

func (r metadataTestRunner) LocationPrefix() string {
	return r.locationPrefix
}

func (r metadataTestRunner) UploadToken() string {
	return ""
}

func (r metadataTestRunner) ResultFormat() string {
	return ""
}

func (r metadataTestRunner) ResultFilePath() string {
	return ""
}

func (r metadataTestRunner) Run(result *runner.RunResult, testCases []plan.TestCase, retry bool) error {
	return nil
}

func (r metadataTestRunner) CommandNameAndArgs(testCases []plan.TestCase, retry bool) (string, []string, error) {
	paths := make([]string, len(testCases))
	for i, tc := range testCases {
		paths[i] = tc.Path
	}

	return "metadata-test-runner", paths, nil
}

type mismatchedExampleRunner struct {
	runner.Jest
}

func (m mismatchedExampleRunner) SupportedFeatures() runner.SupportedFeatures {
	return runner.SupportedFeatures{SplitByExample: true}
}

func TestGetExamplesWithPrefix_RequiresExampleDiscoverer(t *testing.T) {
	_, err := getExamplesWithPrefix([]string{"example.spec.js"}, mismatchedExampleRunner{})
	want := `runner "Jest" advertises split by example but does not implement example discovery`
	if err == nil || err.Error() != want {
		t.Errorf("getExamplesWithPrefix() error = %v, want %q", err, want)
	}
}

func TestCreateRequestParams_FilterTestsError(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{ "message": "forbidden" }`, http.StatusForbidden)
	}))

	defer svr.Close()

	cfg := config.Config{
		OrganizationSlug: "my-org",
		SuiteSlug:        "my-suite",
		Identifier:       "identifier",
		Parallelism:      7,
		Branch:           "",
		SplitByExample:   true,
	}

	client := api.NewClient(api.ClientConfig{
		ServerBaseURL: svr.URL,
	})
	files := []string{
		"apple_spec.rb",
		"banana_spec.rb",
		"cherry_spec.rb",
		"dragonfruit_spec.rb",
		"elderberry_spec.rb",
		"fig_spec.rb",
		"grape_spec.rb",
	}

	_, err := createRequestParam(context.Background(), &cfg, files, *client, runner.Rspec{})

	if err.Error() != "filter tests: forbidden" {
		t.Errorf("createRequestParam() error = %v, want forbidden error", err)
	}
}

func TestCreateRequestParams_NoFilteredFiles(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `
{
	"files": []
}`)
	}))
	defer svr.Close()

	cfg := config.Config{
		OrganizationSlug: "my-org",
		SuiteSlug:        "my-suite",
		Identifier:       "identifier",
		Parallelism:      7,
		Branch:           "",
		SplitByExample:   true,
	}

	client := api.NewClient(api.ClientConfig{
		ServerBaseURL: svr.URL,
	})
	files := []string{
		"testdata/rspec/spec/fruits/apple_spec.rb",
		"testdata/rspec/spec/fruits/banana_spec.rb",
		"testdata/rspec/spec/fruits/cherry_spec.rb",
		"testdata/rspec/spec/fruits/dragonfruit_spec.rb",
		"testdata/rspec/spec/fruits/elderberry_spec.rb",
		"testdata/rspec/spec/fruits/fig_spec.rb",
		"testdata/rspec/spec/fruits/grape_spec.rb",
	}

	got, err := createRequestParam(context.Background(), &cfg, files, *client, runner.Rspec{
		RunnerConfig: runner.RunnerConfig{
			TestCommand: "rspec",
		},
	})
	if err != nil {
		t.Errorf("createRequestParam() error = %v", err)
	}

	want := api.TestPlanParams{
		Identifier:  "identifier",
		Parallelism: 7,
		Branch:      "",
		Tests: api.TestPlanParamsTest{
			Selectors: []api.TestPlanParamsSelector{
				{Value: "testdata/rspec/spec/fruits/apple_spec.rb"},
				{Value: "testdata/rspec/spec/fruits/banana_spec.rb"},
				{Value: "testdata/rspec/spec/fruits/cherry_spec.rb"},
				{Value: "testdata/rspec/spec/fruits/dragonfruit_spec.rb"},
				{Value: "testdata/rspec/spec/fruits/elderberry_spec.rb"},
				{Value: "testdata/rspec/spec/fruits/fig_spec.rb"},
				{Value: "testdata/rspec/spec/fruits/grape_spec.rb"},
			},
		},
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("createRequestParam() diff (-got +want):\n%s", diff)
	}
}

func TestCreateRequestParams_WithTagFilters(t *testing.T) {
	cfg := config.Config{
		OrganizationSlug: "my-org",
		SuiteSlug:        "my-suite",
		Identifier:       "identifier",
		Parallelism:      2,
		Branch:           "main",
		TestRunner:       "pytest",
		TagFilters:       "team:frontend",
	}

	client := api.NewClient(api.ClientConfig{
		ServerBaseURL: "example.com",
	})

	files := []string{
		"../runner/testdata/pytest/failed_test.py",
		"../runner/testdata/pytest/test_sample.py",
		"../runner/testdata/pytest/spells/test_expelliarmus.py",
	}

	got, err := createRequestParam(context.Background(), &cfg, files, *client, runner.Pytest{
		RunnerConfig: runner.RunnerConfig{
			TestCommand: "pytest",
			TagFilters:  "team:frontend",
		},
	})
	if err != nil {
		t.Errorf("createRequestParam() error = %v", err)
	}

	want := api.TestPlanParams{
		Identifier:  "identifier",
		Parallelism: 2,
		Branch:      "main",
		Runner:      "pytest",
		Tests: api.TestPlanParamsTest{
			Examples: []api.TestPlanExample{
				{
					Format:     "example",
					Identifier: "runner/testdata/pytest/test_sample.py::test_happy",
					Name:       "test_happy",
					Path:       "runner/testdata/pytest/test_sample.py::test_happy",
					Scope:      "runner/testdata/pytest/test_sample.py",
				},
				{
					Format:     "example",
					Identifier: "runner/testdata/pytest/spells/test_expelliarmus.py::TestExpelliarmus::test_knocks_wand_out",
					Name:       "test_knocks_wand_out",
					Path:       "runner/testdata/pytest/spells/test_expelliarmus.py::TestExpelliarmus::test_knocks_wand_out",
					Scope:      "runner/testdata/pytest/spells/test_expelliarmus.py::TestExpelliarmus",
				},
			},
		},
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("createRequestParam() diff (-got +want):\n%s", diff)
	}
}

// When selector splitting and tag filters are both enabled, every file is expanded into
// tag-filtered examples so that nothing goes out as a raw selector that would ignore the
// tag filter.
func TestCreateRequestParams_SelectorSplittingWithTagFilters(t *testing.T) {
	cfg := config.Config{
		OrganizationSlug: "my-org",
		SuiteSlug:        "my-suite",
		Identifier:       "identifier",
		Parallelism:      2,
		Branch:           "main",
		TestRunner:       "pytest",
		TagFilters:       "team:frontend",
	}

	client := api.NewClient(api.ClientConfig{
		ServerBaseURL: "example.com",
	})

	files := []string{
		"../runner/testdata/pytest/failed_test.py",
		"../runner/testdata/pytest/test_sample.py",
		"../runner/testdata/pytest/spells/test_expelliarmus.py",
	}

	got, err := createRequestParam(context.Background(), &cfg, files, *client, runner.Pytest{
		RunnerConfig: runner.RunnerConfig{
			TestCommand: "pytest",
			TagFilters:  "team:frontend",
		},
	})
	if err != nil {
		t.Errorf("createRequestParam() error = %v", err)
	}

	want := api.TestPlanParams{
		Identifier:  "identifier",
		Parallelism: 2,
		Branch:      "main",
		Runner:      "pytest",
		Tests: api.TestPlanParamsTest{
			Examples: []api.TestPlanExample{
				{
					Format:     "example",
					Identifier: "runner/testdata/pytest/test_sample.py::test_happy",
					Name:       "test_happy",
					Path:       "runner/testdata/pytest/test_sample.py::test_happy",
					Scope:      "runner/testdata/pytest/test_sample.py",
				},
				{
					Format:     "example",
					Identifier: "runner/testdata/pytest/spells/test_expelliarmus.py::TestExpelliarmus::test_knocks_wand_out",
					Name:       "test_knocks_wand_out",
					Path:       "runner/testdata/pytest/spells/test_expelliarmus.py::TestExpelliarmus::test_knocks_wand_out",
					Scope:      "runner/testdata/pytest/spells/test_expelliarmus.py::TestExpelliarmus",
				},
			},
		},
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("createRequestParam() diff (-got +want):\n%s", diff)
	}
}

func TestCreateRequestParams_WithTagFilters_NonPytest(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `
{
	"tests": []
}`)
	}))
	defer svr.Close()

	cfg := config.Config{
		OrganizationSlug: "my-org",
		SuiteSlug:        "my-suite",
		Identifier:       "identifier",
		Parallelism:      2,
		Branch:           "main",
		TestRunner:       "rspec",
		TagFilters:       "team:frontend",
	}

	client := api.NewClient(api.ClientConfig{
		ServerBaseURL: svr.URL,
	})

	files := []string{
		"testdata/rspec/spec/fruits/apple_spec.rb",
		"testdata/rspec/spec/fruits/banana_spec.rb",
	}

	got, err := createRequestParam(context.Background(), &cfg, files, *client, runner.Rspec{
		RunnerConfig: runner.RunnerConfig{
			TestCommand: "rspec",
		},
	})
	if err != nil {
		t.Errorf("createRequestParam() error = %v", err)
	}

	want := api.TestPlanParams{
		Identifier:  "identifier",
		Parallelism: 2,
		Branch:      "main",
		Runner:      "rspec",
		Tests: api.TestPlanParamsTest{
			Selectors: []api.TestPlanParamsSelector{
				{Value: "testdata/rspec/spec/fruits/apple_spec.rb"},
				{Value: "testdata/rspec/spec/fruits/banana_spec.rb"},
			},
		},
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("createRequestParam() diff (-got +want):\n%s", diff)
	}
}

func TestCreateRequestParams_WithLocationPrefix(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `
	{
		"tests": []
	}`)
	}))
	defer svr.Close()

	client := api.NewClient(api.ClientConfig{
		ServerBaseURL: svr.URL,
	})

	cfg := config.Config{
		OrganizationSlug: "my-org",
		SuiteSlug:        "my-suite",
		Identifier:       "identifier",
		Parallelism:      7,
		Branch:           "",
		TestRunner:       "rspec",
	}

	files := []string{
		"testdata/rspec/spec/fruits/apple_spec.rb",
		"testdata/rspec/spec/fruits/banana_spec.rb",
		"testdata/rspec/spec/fruits/cherry_spec.rb",
	}

	cases := []struct {
		prefix string
	}{
		{
			prefix: "./",
		},
		{
			prefix: "monorepo/project-abc",
		},
	}

	for _, c := range cases {
		t.Run(fmt.Sprintf("location prefix: %s", c.prefix), func(t *testing.T) {
			cfg.LocationPrefix = c.prefix
			runner, err := runner.DetectRunner(&cfg)
			if err != nil {
				t.Fatalf("DetectRunner() error = %v", err)
			}

			got, err := createRequestParam(context.Background(), &cfg, files, *client, runner)
			if err != nil {
				t.Errorf("createRequestParam() error = %v", err)
			}

			want := api.TestPlanParams{
				Identifier:     "identifier",
				Parallelism:    7,
				Branch:         "",
				LocationPrefix: c.prefix,
				Runner:         "rspec",
				Tests: api.TestPlanParamsTest{
					Selectors: []api.TestPlanParamsSelector{
						{Value: "testdata/rspec/spec/fruits/apple_spec.rb"},
						{Value: "testdata/rspec/spec/fruits/banana_spec.rb"},
						{Value: "testdata/rspec/spec/fruits/cherry_spec.rb"},
					},
				},
			}

			if diff := cmp.Diff(got, want); diff != "" {
				t.Errorf("createRequestParam() diff (-got +want):\n%s", diff)
			}
		})
	}
}

func TestBuildSelectionParams(t *testing.T) {
	params := map[string]string{"top": "100"}

	t.Run("forwards real strategies verbatim", func(t *testing.T) {
		for _, strategy := range []string{"random", "manual", "rspec_changed_files", "xgboost", "least-reliable"} {
			got := buildSelectionParams(strategy, params)
			want := &api.SelectionParams{Strategy: strategy, Params: params}
			if diff := cmp.Diff(got, want); diff != "" {
				t.Errorf("buildSelectionParams(%q) diff (-got +want):\n%s", strategy, diff)
			}
		}
	})

	// Defence-in-depth for TE-5641: coerce human-intuitive "no selection"
	// sentinels to nil instead of forwarding them to the Test Engine API,
	// which only accepts the strict allowlist. Covers case and whitespace
	// variants too.
	t.Run("coerces sentinel values to nil", func(t *testing.T) {
		sentinels := []string{
			"",
			"none", "NONE", "None",
			"off", "OFF",
			"false", "FALSE", "False",
			"disabled", "DISABLED",
			"no", "NO",
			" none ", "\tnone\n", "  ", "\t",
			" off", "false ", " disabled\t",
		}
		for _, strategy := range sentinels {
			if got := buildSelectionParams(strategy, params); got != nil {
				t.Errorf("buildSelectionParams(%q) = %+v, want nil", strategy, got)
			}
		}
	})

	// Unknown values are still forwarded so the backend stays authoritative
	// for strategy validation; that's by design (see TE-5641 notes).
	t.Run("forwards unknown strategies verbatim", func(t *testing.T) {
		got := buildSelectionParams("not-a-real-strategy", params)
		want := &api.SelectionParams{Strategy: "not-a-real-strategy", Params: params}
		if diff := cmp.Diff(got, want); diff != "" {
			t.Errorf("buildSelectionParams diff (-got +want):\n%s", diff)
		}
	})
}

func TestCreateRequestParams_WithLocationPrefix_SplitByExample(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `
{
	"tests": [
		{ "path": "my/project/testdata/rspec/spec/fruits/banana_spec.rb", "reason": "slow file" }
	]
}`)
	}))
	defer svr.Close()

	cfg := config.Config{
		OrganizationSlug: "my-org",
		SuiteSlug:        "my-suite",
		Identifier:       "identifier",
		Parallelism:      7,
		Branch:           "",
		TestRunner:       "rspec",
		TestCommand:      "rspec",
		LocationPrefix:   "my/project",
	}

	runner, err := runner.DetectRunner(&cfg)
	if err != nil {
		t.Fatalf("DetectRunner() error = %v", err)
	}

	client := api.NewClient(api.ClientConfig{
		ServerBaseURL: svr.URL,
	})
	files := []string{
		"testdata/rspec/spec/fruits/apple_spec.rb",
		"testdata/rspec/spec/fruits/banana_spec.rb",
		"testdata/rspec/spec/fruits/cherry_spec.rb",
	}

	got, err := createRequestParam(context.Background(), &cfg, files, *client, runner)
	if err != nil {
		t.Errorf("createRequestParam() error = %v", err)
	}

	// filtered files: banana_spec.rb
	// the rest: apple_spec.rb, cherry_spec.rb
	want := api.TestPlanParams{
		Identifier:     "identifier",
		Parallelism:    7,
		Branch:         "",
		LocationPrefix: "my/project",
		Runner:         "rspec",
		Tests: api.TestPlanParamsTest{
			Examples: []api.TestPlanExample{
				{
					Identifier: "./testdata/rspec/spec/fruits/banana_spec.rb[1:1]",
					Name:       "is yellow",
					Path:       "my/project/testdata/rspec/spec/fruits/banana_spec.rb[1:1]",
					Scope:      "Banana",
				},
				{
					Identifier: "./testdata/rspec/spec/fruits/banana_spec.rb[1:2:1]",
					Name:       "is green",
					Path:       "my/project/testdata/rspec/spec/fruits/banana_spec.rb[1:2:1]",
					Scope:      "Banana when not ripe",
				},
			},
			Selectors: []api.TestPlanParamsSelector{
				{Value: "testdata/rspec/spec/fruits/apple_spec.rb"},
				{Value: "testdata/rspec/spec/fruits/cherry_spec.rb"},
			},
		},
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("createRequestParam() diff (-got +want):\n%s", diff)
	}
}
