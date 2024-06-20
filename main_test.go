package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/buildkite/test-splitter/internal/api"
	"github.com/buildkite/test-splitter/internal/config"
	"github.com/buildkite/test-splitter/internal/plan"
	"github.com/buildkite/test-splitter/internal/runner"
	"github.com/google/go-cmp/cmp"
)

func TestRetryFailedTests(t *testing.T) {
	testRunner := runner.NewRspec("true")
	maxRetries := 3
	exitCode := retryFailedTests(testRunner, maxRetries)
	want := 0
	if exitCode != want {
		t.Errorf("retryFailedTests(%v, %v) = %v, want %v", testRunner, maxRetries, exitCode, want)
	}
}

func TestRetryFailedTests_Failure(t *testing.T) {
	testRunner := runner.NewRspec("false")
	maxRetries := 3
	exitCode := retryFailedTests(testRunner, maxRetries)
	want := 1
	if exitCode != want {
		t.Errorf("retryFailedTests(%v, %v) = %v, want %v", testRunner, maxRetries, exitCode, want)
	}
}

func TestFetchOrCreateTestPlan(t *testing.T) {
	files := []string{"apple"}
	testRunner := runner.NewRspec("")

	// mock server to return a test plan
	response := `{
	"tasks": {
		"0": {
			"node_number": 0,
			"tests": [
				{
					"path": "apple",
					"format": "file"
				}
			]
		}
	}
}`
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// simulate cache miss for GET test_plan so it will trigger the test plan creation
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusNotFound)
		} else {
			fmt.Fprint(w, response)
		}
	}))
	defer svr.Close()

	ctx := context.Background()
	cfg := config.Config{
		NodeIndex:     0,
		Parallelism:   10,
		Identifier:    "identifier",
		ServerBaseUrl: svr.URL,
	}

	// we want the function to return the test plan fetched from the server
	want := plan.TestPlan{
		Tasks: map[string]*plan.Task{
			"0": {
				NodeNumber: 0,
				Tests:      []plan.TestCase{{Path: "apple", Format: plan.TestCaseFormatFile}},
			},
		},
	}

	got, err := fetchOrCreateTestPlan(ctx, cfg, files, testRunner)
	if err != nil {
		t.Errorf("fetchOrCreateTestPlan(ctx, %v, %v) error = %v", cfg, files, err)
	}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("fetchOrCreateTestPlan(ctx, %v, %v) diff (-got +want):\n%s", cfg, files, diff)
	}
}

func TestFetchOrCreateTestPlan_CachedPlan(t *testing.T) {
	cachedPlan := `{
	"tasks": {
		"0": {
			"node_number": 0,
			"tests": [
				{
					"path": "apple",
					"format": "file"
				}
			]
		}
	}
}`

	newPlan := `{
	"tasks": {
		"0": {
			"node_number": 0,
			"tests": [
				{
					"path": "banana",
					"format": "file"
				}
			]
		}
	}
}`

	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			fmt.Fprint(w, cachedPlan)
		} else {
			fmt.Fprint(w, newPlan)
		}
	}))
	defer svr.Close()

	cfg := config.Config{
		NodeIndex:        0,
		Parallelism:      10,
		Identifier:       "identifier",
		ServerBaseUrl:    svr.URL,
		OrganizationSlug: "org",
		SuiteSlug:        "suite",
	}

	tests := []string{"banana"}
	testRunner := runner.NewRspec("")

	want := plan.TestPlan{
		Tasks: map[string]*plan.Task{
			"0": {
				NodeNumber: 0,
				Tests:      []plan.TestCase{{Path: "apple", Format: plan.TestCaseFormatFile}},
			},
		},
	}

	got, err := fetchOrCreateTestPlan(context.Background(), cfg, tests, testRunner)
	if err != nil {
		t.Errorf("fetchOrCreateTestPlan(ctx, %v, %v) error = %v", cfg, tests, err)
	}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("fetchOrCreateTestPlan(ctx, %v, %v) diff (-got +want):\n%s", cfg, tests, diff)
	}
}

func TestFetchOrCreateTestPlan_PlanError(t *testing.T) {
	files := []string{"apple", "banana", "cherry", "mango"}
	TestRunner := runner.NewRspec("")

	// mock server to return an error plan
	response := `{
	"tasks": {}
}`
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, response)
	}))
	defer svr.Close()

	ctx := context.Background()
	cfg := config.Config{
		NodeIndex:     0,
		Parallelism:   2,
		Identifier:    "identifier",
		ServerBaseUrl: svr.URL,
	}

	// we want the function to return a fallback plan
	want := plan.CreateFallbackPlan(files, cfg.Parallelism)

	got, err := fetchOrCreateTestPlan(ctx, cfg, files, TestRunner)
	if err != nil {
		t.Errorf("fetchOrCreateTestPlan(ctx, %v, %v) error = %v", cfg, files, err)
	}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("fetchOrCreateTestPlan(ctx, %v, %v) diff (-got +want):\n%s", cfg, files, diff)
	}
}

func TestFetchOrCreateTestPlan_InternalServerError(t *testing.T) {
	files := []string{"red", "orange", "yellow", "green", "blue", "indigo", "violet"}
	testRunner := runner.NewRspec("")

	// mock server to return a 500 Internal Server Error
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}))
	defer svr.Close()

	// set the fetch timeout to 1 second so we don't wait too long
	ctx := context.Background()
	fetchCtx, cancel := context.WithTimeout(ctx, 1*time.Millisecond)
	defer cancel()

	cfg := config.Config{
		NodeIndex:     0,
		Parallelism:   3,
		Identifier:    "identifier",
		ServerBaseUrl: svr.URL,
	}

	// we want the function to return a fallback plan
	want := plan.CreateFallbackPlan(files, cfg.Parallelism)

	got, err := fetchOrCreateTestPlan(fetchCtx, cfg, files, testRunner)
	if err != nil {
		t.Errorf("fetchOrCreateTestPlan(ctx, %v, %v) error = %v", cfg, files, err)
	}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("fetchOrCreateTestPlan(ctx, %v, %v) diff (-got +want):\n%s", cfg, files, diff)
	}
}

func TestFetchOrCreateTestPlan_BadRequest(t *testing.T) {
	files := []string{"apple", "banana"}
	testRunner := runner.NewRspec("")

	// mock server to return 400 Bad Request
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Bad Request", http.StatusBadRequest)
	}))
	defer svr.Close()

	ctx := context.Background()

	cfg := config.Config{
		NodeIndex:     0,
		Parallelism:   2,
		Identifier:    "identifier",
		ServerBaseUrl: svr.URL,
	}

	// we want the function to return an empty test plan and an error
	want := plan.TestPlan{}

	got, err := fetchOrCreateTestPlan(ctx, cfg, files, testRunner)
	if err == nil {
		t.Errorf("fetchOrCreateTestPlan(ctx, %v, %v) want error, got %v", cfg, files, err)
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("fetchOrCreateTestPlan(ctx, %v, %v) diff (-got +want):\n%s", cfg, files, diff)
	}
}

func TestCreateRequestParams_SplitByFile(t *testing.T) {
	cfg := config.Config{
		OrganizationSlug: "my-org",
		SuiteSlug:        "my-suite",
		Identifier:       "identifier",
		Parallelism:      7,
	}

	client := api.NewClient(api.ClientConfig{})
	got, err := createRequestParam(context.Background(), cfg, []string{"apple", "banana"}, *client, runner.NewRspec(""))
	want := api.TestPlanParams{
		Identifier:  "identifier",
		Parallelism: 7,
		Tests: api.TestPlanParamsTest{
			Files: []plan.TestCase{
				{Path: "apple"},
				{Path: "banana"},
			},
		},
	}

	if err != nil {
		t.Errorf("createRequestParam() error = %v", err)
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("createRequestParam() diff (-got +want):\n%s", diff)
	}
}

func TestCreateRequestParams_SplitByExample(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `
{
	"test/spec/fruits/apple_spec.rb": 100000,
	"test/spec/fruits/banana_spec.rb": 180000,
	"test/spec/fruits/cherry_spec.rb": 120000,
	"test/spec/fruits/dragonfruit_spec.rb": 50000,
	"test/spec/fruits/elderberry_spec.rb": 40000,
	"test/spec/fruits/fig_spec.rb": 200000,
	"test/spec/fruits/grape_spec.rb": 30000
}`)
	}))
	defer svr.Close()

	cfg := config.Config{
		OrganizationSlug:  "my-org",
		SuiteSlug:         "my-suite",
		Identifier:        "identifier",
		Parallelism:       7,
		SplitByExample:    true,
		SlowFileThreshold: 3 * time.Minute,
	}

	client := api.NewClient(api.ClientConfig{
		ServerBaseUrl: svr.URL,
	})
	files := []string{
		"test/spec/fruits/apple_spec.rb",
		"test/spec/fruits/banana_spec.rb",
		"test/spec/fruits/cherry_spec.rb",
		"test/spec/fruits/dragonfruit_spec.rb",
		"test/spec/fruits/elderberry_spec.rb",
		"test/spec/fruits/fig_spec.rb",
		"test/spec/fruits/grape_spec.rb",
	}

	got, err := createRequestParam(context.Background(), cfg, files, *client, runner.NewRspec("rspec"))

	if err != nil {
		t.Errorf("createRequestParam() error = %v", err)
	}

	// slow files (more than or equal to 3minutes): banana_spec.rb, fig_spec.rb
	// the rest: apple_spec.rb, cherry_spec.rb, dragonfruit_spec.rb, elderberry_spec.rb, grape_spec.rb
	want := api.TestPlanParams{
		Identifier:  "identifier",
		Parallelism: 7,
		Tests: api.TestPlanParamsTest{
			Files: []plan.TestCase{
				{Path: "test/spec/fruits/apple_spec.rb"},
				{Path: "test/spec/fruits/cherry_spec.rb"},
				{Path: "test/spec/fruits/dragonfruit_spec.rb"},
				{Path: "test/spec/fruits/elderberry_spec.rb"},
				{Path: "test/spec/fruits/grape_spec.rb"},
			},
			Examples: []plan.TestCase{
				{
					Identifier: "./test/spec/fruits/banana_spec.rb[1:1]",
					Name:       "is yellow",
					Path:       "./test/spec/fruits/banana_spec.rb:2",
					Scope:      "Banana is yellow",
				},
				{
					Identifier: "./test/spec/fruits/banana_spec.rb[1:2:1]",
					Name:       "is green",
					Path:       "./test/spec/fruits/banana_spec.rb:7",
					Scope:      "Banana when not ripe is green",
				},
				{
					Identifier: "./test/spec/fruits/fig_spec.rb[1:1]",
					Name:       "is purple",
					Path:       "./test/spec/fruits/fig_spec.rb:2",
					Scope:      "Fig is purple",
				},
			},
		},
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("createRequestParam() diff (-got +want):\n%s", diff)
	}
}

func TestCreateRequestParams_SplitByExample_NoFileTiming(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))

	defer svr.Close()

	cfg := config.Config{
		OrganizationSlug: "my-org",
		SuiteSlug:        "my-suite",
		Identifier:       "identifier",
		Parallelism:      7,
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

	_, err := createRequestParam(context.Background(), cfg, files, *client, runner.NewRspec(""))

	if err == nil {
		t.Errorf("createRequestParam() want error, got nil")
	}
}

func TestCreateRequestParams_SplitByExample_MissingSomeOfTiming(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `
{
	"test/spec/fruits/apple_spec.rb": 100000,
	"test/spec/fruits/banana_spec.rb": 200000,
	"test/spec/fruits/cherry_spec.rb": 120000,
	"test/spec/fruits/dragonfruit_spec.rb": 50000,
	"test/spec/fruits/elderberry_spec.rb": 40000
}`)
	}))
	defer svr.Close()

	cfg := config.Config{
		OrganizationSlug:  "my-org",
		SuiteSlug:         "my-suite",
		Identifier:        "identifier",
		Parallelism:       7,
		SplitByExample:    true,
		SlowFileThreshold: 3 * time.Minute,
	}

	client := api.NewClient(api.ClientConfig{
		ServerBaseUrl: svr.URL,
	})
	files := []string{
		"test/spec/fruits/apple_spec.rb",
		"test/spec/fruits/banana_spec.rb",
		"test/spec/fruits/cherry_spec.rb",
		"test/spec/fruits/dragonfruit_spec.rb",
		"test/spec/fruits/elderberry_spec.rb",
		"test/spec/fruits/fig_spec.rb",
		"test/spec/fruits/grape_spec.rb",
	}

	got, err := createRequestParam(context.Background(), cfg, files, *client, runner.NewRspec("rspec"))

	if err != nil {
		t.Errorf("createRequestParam() error = %v", err)
	}

	// slow files (more than or equal to 3minutes): banana_spec.rb, fig_spec.rb
	// the rest: apple_spec.rb, cherry_spec.rb, dragonfruit_spec.rb, elderberry_spec.rb, grape_spec.rb
	want := api.TestPlanParams{
		Identifier:  "identifier",
		Parallelism: 7,
		Tests: api.TestPlanParamsTest{
			Files: []plan.TestCase{
				{Path: "test/spec/fruits/apple_spec.rb"},
				{Path: "test/spec/fruits/cherry_spec.rb"},
				{Path: "test/spec/fruits/dragonfruit_spec.rb"},
				{Path: "test/spec/fruits/elderberry_spec.rb"},
				{Path: "test/spec/fruits/fig_spec.rb"},
				{Path: "test/spec/fruits/grape_spec.rb"},
			},
			Examples: []plan.TestCase{
				{
					Identifier: "./test/spec/fruits/banana_spec.rb[1:1]",
					Name:       "is yellow",
					Path:       "./test/spec/fruits/banana_spec.rb:2",
					Scope:      "Banana is yellow",
				},
				{
					Identifier: "./test/spec/fruits/banana_spec.rb[1:2:1]",
					Name:       "is green",
					Path:       "./test/spec/fruits/banana_spec.rb:7",
					Scope:      "Banana when not ripe is green",
				},
			},
		},
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("createRequestParam() diff (-got +want):\n%s", diff)
	}
}

func TestCreateRequestParams_SplitByExample_NoSlowFiles(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `
{
	"test/spec/fruits/apple_spec.rb": 100000,
	"test/spec/fruits/banana_spec.rb": 100000,
	"test/spec/fruits/cherry_spec.rb": 120000,
	"test/spec/fruits/dragonfruit_spec.rb": 50000,
	"test/spec/fruits/elderberry_spec.rb": 40000
}`)
	}))
	defer svr.Close()

	cfg := config.Config{
		OrganizationSlug:  "my-org",
		SuiteSlug:         "my-suite",
		Identifier:        "identifier",
		Parallelism:       7,
		SplitByExample:    true,
		SlowFileThreshold: 3 * time.Minute,
	}

	client := api.NewClient(api.ClientConfig{
		ServerBaseUrl: svr.URL,
	})
	files := []string{
		"test/spec/fruits/apple_spec.rb",
		"test/spec/fruits/banana_spec.rb",
		"test/spec/fruits/cherry_spec.rb",
		"test/spec/fruits/dragonfruit_spec.rb",
		"test/spec/fruits/elderberry_spec.rb",
		"test/spec/fruits/fig_spec.rb",
		"test/spec/fruits/grape_spec.rb",
	}

	got, err := createRequestParam(context.Background(), cfg, files, *client, runner.NewRspec("rspec"))

	if err != nil {
		t.Errorf("createRequestParam() error = %v", err)
	}

	want := api.TestPlanParams{
		Identifier:  "identifier",
		Parallelism: 7,
		Tests: api.TestPlanParamsTest{
			Files: []plan.TestCase{
				{Path: "test/spec/fruits/apple_spec.rb"},
				{Path: "test/spec/fruits/banana_spec.rb"},
				{Path: "test/spec/fruits/cherry_spec.rb"},
				{Path: "test/spec/fruits/dragonfruit_spec.rb"},
				{Path: "test/spec/fruits/elderberry_spec.rb"},
				{Path: "test/spec/fruits/fig_spec.rb"},
				{Path: "test/spec/fruits/grape_spec.rb"},
			},
		},
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("createRequestParam() diff (-got +want):\n%s", diff)
	}
}
