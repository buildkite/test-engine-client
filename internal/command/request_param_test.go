package command

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/buildkite/test-engine-client/internal/api"
	"github.com/buildkite/test-engine-client/internal/config"
	"github.com/buildkite/test-engine-client/internal/plan"
	"github.com/buildkite/test-engine-client/internal/runner"
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
		ServerBaseUrl: svr.URL,
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
			Files: []plan.TestCase{
				{Path: "testdata/rspec/spec/fruits/apple_spec.rb"},
				{Path: "testdata/rspec/spec/fruits/cherry_spec.rb"},
				{Path: "testdata/rspec/spec/fruits/dragonfruit_spec.rb"},
				{Path: "testdata/rspec/spec/fruits/elderberry_spec.rb"},
				{Path: "testdata/rspec/spec/fruits/grape_spec.rb"},
			},
			Examples: []plan.TestCase{
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

	runners := []TestRunner{
		runner.Jest{}, runner.Playwright{}, runner.Cypress{},
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
				ServerBaseUrl: svr.URL,
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
					Files: []plan.TestCase{
						{Path: "testdata/fruits/apple.spec.js"},
						{Path: "testdata/fruits/banana.spec.js"},
						{Path: "testdata/fruits/cherry.spec.js"},
					},
				},
			}

			if diff := cmp.Diff(got, want); diff != "" {
				t.Errorf("createRequestParam() diff (-got +want):\n%s", diff)
			}
		})
	}
}

func TestCreateRequestParams_PytestPants(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `
{
	"tests": [
		{ "path": "test/banana_test.py", "reason": "slow file" },
		{ "path": "test/fig_test.py", "reason": "slow file" }
	]
}`)
	}))
	defer svr.Close()

	runner := runner.PytestPants{}

	t.Run(runner.Name(), func(t *testing.T) {
		cfg := config.Config{
			OrganizationSlug: "my-org",
			SuiteSlug:        "my-suite",
			Identifier:       "identifier",
			Parallelism:      7,
			Branch:           "",
			TestRunner:       runner.Name(),
		}

		client := api.NewClient(api.ClientConfig{
			ServerBaseUrl: svr.URL,
		})
		files := []string{
			"test/apple_test.py",
			"test/banana_test.py",
			"test/cherry_test.py",
		}

		got, err := createRequestParam(context.Background(), &cfg, files, *client, runner)
		if err != nil {
			t.Errorf("createRequestParam() error = %v", err)
		}

		want := api.TestPlanParams{
			Identifier:  "identifier",
			Parallelism: 7,
			Branch:      "",
			Runner:      "pytest",
			Tests: api.TestPlanParamsTest{
				Files: []plan.TestCase{
					{Path: "test/apple_test.py"},
					{Path: "test/banana_test.py"},
					{Path: "test/cherry_test.py"},
				},
			},
		}

		if diff := cmp.Diff(got, want); diff != "" {
			t.Errorf("createRequestParam() diff (-got +want):\n%s", diff)
		}
	})
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
		ServerBaseUrl: "http://example.com",
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
			Files: []plan.TestCase{
				{Path: "testdata/fruits/apple.spec.js"},
				{Path: "testdata/fruits/banana.spec.js"},
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
		ServerBaseUrl: "http://example.com",
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
			Examples: []plan.TestCase{
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
	name     string
	examples []plan.TestCase
}

func (r metadataTestRunner) Name() string {
	return r.name
}

func (r metadataTestRunner) GetExamples(files []string) ([]plan.TestCase, error) {
	return r.examples, nil
}

func (r metadataTestRunner) GetFiles() ([]string, error) {
	return nil, nil
}

func (r metadataTestRunner) GetLocationPrefix() string {
	return ""
}

func (r metadataTestRunner) Run(result *runner.RunResult, testCases []plan.TestCase, retry bool) error {
	return nil
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
		ServerBaseUrl: svr.URL,
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
		ServerBaseUrl: svr.URL,
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
			Files: []plan.TestCase{
				{Path: "testdata/rspec/spec/fruits/apple_spec.rb"},
				{Path: "testdata/rspec/spec/fruits/banana_spec.rb"},
				{Path: "testdata/rspec/spec/fruits/cherry_spec.rb"},
				{Path: "testdata/rspec/spec/fruits/dragonfruit_spec.rb"},
				{Path: "testdata/rspec/spec/fruits/elderberry_spec.rb"},
				{Path: "testdata/rspec/spec/fruits/fig_spec.rb"},
				{Path: "testdata/rspec/spec/fruits/grape_spec.rb"},
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
		ServerBaseUrl: "example.com",
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
			Examples: []plan.TestCase{
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
		ServerBaseUrl: svr.URL,
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
			Files: []plan.TestCase{
				{Path: "testdata/rspec/spec/fruits/apple_spec.rb"},
				{Path: "testdata/rspec/spec/fruits/banana_spec.rb"},
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
		ServerBaseUrl: svr.URL,
	})

	cfg := config.Config{
		OrganizationSlug: "my-org",
		SuiteSlug:        "my-suite",
		Identifier:       "identifier",
		Parallelism:      7,
		Branch:           "",
		TestRunner:       "jest",
	}

	files := []string{
		"testdata/rspec/spec/fruits/apple_spec.rb",
		"testdata/rspec/spec/fruits/banana_spec.rb",
		"testdata/rspec/spec/fruits/cherry_spec.rb",
	}

	cases := []struct {
		prefix    string
		wantFiles []plan.TestCase
	}{
		{
			prefix: "",
			wantFiles: []plan.TestCase{
				{Path: "testdata/rspec/spec/fruits/apple_spec.rb"},
				{Path: "testdata/rspec/spec/fruits/banana_spec.rb"},
				{Path: "testdata/rspec/spec/fruits/cherry_spec.rb"},
			},
		},
		{
			prefix: "./",
			wantFiles: []plan.TestCase{
				{Path: "./testdata/rspec/spec/fruits/apple_spec.rb"},
				{Path: "./testdata/rspec/spec/fruits/banana_spec.rb"},
				{Path: "./testdata/rspec/spec/fruits/cherry_spec.rb"},
			},
		},
		{
			prefix: "monorepo/project-abc",
			wantFiles: []plan.TestCase{
				{Path: "monorepo/project-abc/testdata/rspec/spec/fruits/apple_spec.rb"},
				{Path: "monorepo/project-abc/testdata/rspec/spec/fruits/banana_spec.rb"},
				{Path: "monorepo/project-abc/testdata/rspec/spec/fruits/cherry_spec.rb"},
			},
		},
	}

	for _, c := range cases {
		t.Run(fmt.Sprintf("location prefix: %s", c.prefix), func(t *testing.T) {
			cfg.LocationPrefix = c.prefix
			got, err := createRequestParam(context.Background(), &cfg, files, *client, runner.Jest{
				RunnerConfig: runner.RunnerConfig{},
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
				Runner:      "jest",
				Tests: api.TestPlanParamsTest{
					Files: c.wantFiles,
				},
			}

			if diff := cmp.Diff(got, want); diff != "" {
				t.Errorf("createRequestParam() diff (-got +want):\n%s", diff)
			}
		})
	}
}
