package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/buildkite/test-splitter/internal/api"
	"github.com/buildkite/test-splitter/internal/config"
	"github.com/buildkite/test-splitter/internal/plan"
	"github.com/buildkite/test-splitter/internal/runner"
	"github.com/google/go-cmp/cmp"
)

func TestRunTestsWithRetry(t *testing.T) {
	testRunner := runner.Rspec{
		TestCommand: "rspec",
	}
	maxRetries := 3
	testCases := []string{"test/spec/fruits/apple_spec.rb"}
	timeline := []api.Timeline{}
	testResult, err := runTestsWithRetry(testRunner, testCases, maxRetries, &timeline)

	if err != nil {
		t.Errorf("runTestsWithRetry(...) error = %v", err)
	}

	if testResult.Status != runner.TestStatusPassed {
		t.Errorf("runTestsWithRetry(...) testResult.Status = %v, want %v", testResult.Status, runner.TestStatusPassed)
	}

	if len(timeline) != 2 {
		t.Errorf("timeline length = %v, want %d", len(timeline), 2)
	}

	events := []string{}
	for _, event := range timeline {
		events = append(events, event.Event)
	}
	if diff := cmp.Diff(events, []string{"test_start", "test_end"}); diff != "" {
		t.Errorf("timeline events diff (-got +want):\n%s", diff)
	}
}

func TestRunTestsWithRetry_TestFailed(t *testing.T) {
	testRunner := runner.Rspec{
		TestCommand: "rspec",
	}
	maxRetries := 2
	testCases := []string{"test/spec/fruits/apple_spec.rb", "test/spec/fruits/tomato_spec.rb"}
	timeline := []api.Timeline{}
	testResult, err := runTestsWithRetry(testRunner, testCases, maxRetries, &timeline)

	if err != nil {
		t.Errorf("runTestsWithRetry(...) error = %v", err)
	}

	if testResult.Status != runner.TestStatusFailed {
		t.Errorf("runTestsWithRetry(...) testResult.Status = %v, want %v", testResult.Status, runner.TestStatusPassed)
	}

	if diff := cmp.Diff(testResult.FailedTests, []string{"./test/spec/fruits/tomato_spec.rb[1:2]"}); diff != "" {
		t.Errorf("runTestsWithRetry(...) testResult.FailedTests diff (-got +want):\n%s", diff)
	}

	if len(timeline) != 6 {
		t.Errorf("timeline length = %v, want %d", len(timeline), 6)
	}

	events := []string{}
	for _, event := range timeline {
		events = append(events, event.Event)
	}
	if diff := cmp.Diff(events, []string{"test_start", "test_end", "retry_1_start", "retry_1_end", "retry_2_start", "retry_2_end"}); diff != "" {
		t.Errorf("timeline events diff (-got +want):\n%s", diff)
	}
}

func TestRunTestsWithRetry_Error(t *testing.T) {
	testRunner := runner.Rspec{
		TestCommand: "rspec --invalid-option",
	}
	maxRetries := 2
	testCases := []string{"test/spec/fruits/fig_spec.rb"}
	timeline := []api.Timeline{}
	testResult, err := runTestsWithRetry(testRunner, testCases, maxRetries, &timeline)

	exitError := new(exec.ExitError)
	if !errors.As(err, &exitError) {
		t.Errorf("Expected exec.ExitError, but got %v", err)
	}

	if testResult.Status != runner.TestStatusError {
		t.Errorf("runTestsWithRetry(...) testResult.Status = %v, want %v", testResult.Status, runner.TestStatusError)
	}

	if len(timeline) != 2 {
		t.Errorf("timeline length = %v, want %d", len(timeline), 2)
	}

	events := []string{}
	for _, event := range timeline {
		events = append(events, event.Event)
	}
	if diff := cmp.Diff(events, []string{"test_start", "test_end"}); diff != "" {
		t.Errorf("timeline events diff (-got +want):\n%s", diff)
	}
}

func TestFetchOrCreateTestPlan(t *testing.T) {
	files := []string{"apple"}
	testRunner := runner.Rspec{}

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
	apiClient := api.NewClient(api.ClientConfig{
		ServerBaseUrl: cfg.ServerBaseUrl,
	})

	// we want the function to return the test plan fetched from the server
	want := plan.TestPlan{
		Tasks: map[string]*plan.Task{
			"0": {
				NodeNumber: 0,
				Tests:      []plan.TestCase{{Path: "apple", Format: plan.TestCaseFormatFile}},
			},
		},
	}

	got, err := fetchOrCreateTestPlan(ctx, apiClient, cfg, files, testRunner)
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
		Branch:           "tat-123/my-cool-feature",
	}
	apiClient := api.NewClient(api.ClientConfig{
		ServerBaseUrl:    cfg.ServerBaseUrl,
		OrganizationSlug: cfg.OrganizationSlug,
	})

	tests := []string{"banana"}
	testRunner := runner.Rspec{}

	want := plan.TestPlan{
		Tasks: map[string]*plan.Task{
			"0": {
				NodeNumber: 0,
				Tests:      []plan.TestCase{{Path: "apple", Format: plan.TestCaseFormatFile}},
			},
		},
	}

	got, err := fetchOrCreateTestPlan(context.Background(), apiClient, cfg, tests, testRunner)
	if err != nil {
		t.Errorf("fetchOrCreateTestPlan(ctx, %v, %v) error = %v", cfg, tests, err)
	}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("fetchOrCreateTestPlan(ctx, %v, %v) diff (-got +want):\n%s", cfg, tests, diff)
	}
}

func TestFetchOrCreateTestPlan_PlanError(t *testing.T) {
	files := []string{"apple", "banana", "cherry", "mango"}
	TestRunner := runner.Rspec{}

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
		Branch:        "tat-123/my-cool-feature",
		ServerBaseUrl: svr.URL,
	}
	apiClient := api.NewClient(api.ClientConfig{
		ServerBaseUrl: cfg.ServerBaseUrl,
	})

	// we want the function to return a fallback plan
	want := plan.CreateFallbackPlan(files, cfg.Parallelism)

	got, err := fetchOrCreateTestPlan(ctx, apiClient, cfg, files, TestRunner)
	if err != nil {
		t.Errorf("fetchOrCreateTestPlan(ctx, %v, %v) error = %v", cfg, files, err)
	}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("fetchOrCreateTestPlan(ctx, %v, %v) diff (-got +want):\n%s", cfg, files, diff)
	}
}

func TestFetchOrCreateTestPlan_InternalServerError(t *testing.T) {
	files := []string{"red", "orange", "yellow", "green", "blue", "indigo", "violet"}
	testRunner := runner.Rspec{}

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
		Branch:        "tat-123/my-cool-feature",
		ServerBaseUrl: svr.URL,
	}
	apiClient := api.NewClient(api.ClientConfig{
		ServerBaseUrl: cfg.ServerBaseUrl,
	})

	// we want the function to return a fallback plan
	want := plan.CreateFallbackPlan(files, cfg.Parallelism)

	got, err := fetchOrCreateTestPlan(fetchCtx, apiClient, cfg, files, testRunner)
	if err != nil {
		t.Errorf("fetchOrCreateTestPlan(ctx, %v, %v) error = %v", cfg, files, err)
	}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("fetchOrCreateTestPlan(ctx, %v, %v) diff (-got +want):\n%s", cfg, files, diff)
	}
}

func TestFetchOrCreateTestPlan_BadRequest(t *testing.T) {
	files := []string{"apple", "banana"}
	testRunner := runner.Rspec{}

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
		Branch:        "",
		ServerBaseUrl: svr.URL,
	}
	apiClient := api.NewClient(api.ClientConfig{
		ServerBaseUrl: cfg.ServerBaseUrl,
	})

	// we want the function to return an empty test plan and an error
	want := plan.TestPlan{}

	got, err := fetchOrCreateTestPlan(ctx, apiClient, cfg, files, testRunner)
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
		Branch:           "",
	}

	client := api.NewClient(api.ClientConfig{})
	got, err := createRequestParam(context.Background(), cfg, []string{"apple", "banana"}, *client, runner.Rspec{})
	want := api.TestPlanParams{
		Identifier:  "identifier",
		Parallelism: 7,
		Branch:      "",
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
		Branch:            "",
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

	got, err := createRequestParam(context.Background(), cfg, files, *client, runner.Rspec{
		TestCommand: "rspec",
	})

	if err != nil {
		t.Errorf("createRequestParam() error = %v", err)
	}

	// slow files (more than or equal to 3minutes): banana_spec.rb, fig_spec.rb
	// the rest: apple_spec.rb, cherry_spec.rb, dragonfruit_spec.rb, elderberry_spec.rb, grape_spec.rb
	want := api.TestPlanParams{
		Identifier:  "identifier",
		Parallelism: 7,
		Branch:      "",
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

	_, err := createRequestParam(context.Background(), cfg, files, *client, runner.Rspec{})

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
		Branch:            "",
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

	got, err := createRequestParam(context.Background(), cfg, files, *client, runner.Rspec{
		TestCommand: "rspec",
	})

	if err != nil {
		t.Errorf("createRequestParam() error = %v", err)
	}

	// slow files (more than or equal to 3minutes): banana_spec.rb, fig_spec.rb
	// the rest: apple_spec.rb, cherry_spec.rb, dragonfruit_spec.rb, elderberry_spec.rb, grape_spec.rb
	want := api.TestPlanParams{
		Identifier:  "identifier",
		Parallelism: 7,
		Branch:      "",
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
		Branch:            "",
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

	got, err := createRequestParam(context.Background(), cfg, files, *client, runner.Rspec{
		TestCommand: "rspec",
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

func TestSendMetadata(t *testing.T) {
	originalVersion := Version
	Version = "0.1.0"
	defer func() {
		Version = originalVersion
	}()

	timeline := []api.Timeline{
		{Event: "test_start", Timestamp: "2024-06-20T04:46:13.60977Z"},
		{Event: "test_end", Timestamp: "2024-06-20T04:49:09.609793Z"},
	}

	env := map[string]string{
		"BUILDKITE_BUILD_ID":               "xyz",
		"BUILDKITE_JOB_ID":                 "abc",
		"BUILDKITE_STEP_ID":                "pqr",
		"BUILDKITE_ORGANIZATION_SLUG":      "buildkite",
		"BUILDKITE_PARALLEL_JOB_COUNT":     "10",
		"BUILDKITE_PARALLEL_JOB":           "5",
		"BUILDKITE_SPLITTER_DEBUG_ENABLED": "true",
		"BUILDKITE_SPLITTER_RETRY_COUNT":   "2",
		"BUILDKITE_SPLITTER_RETRY_CMD":     "bundle exec rspec --only-failures",
		"BUILDKITE_SPLITTER_SUITE_SLUG":    "rspec",
		"BUILDKITE_SPLITTER_TEST_CMD":      "bundle exec rspec",
		"BUILDKITE_SPLITTER_TEST_RUNNER":   "rspec",
	}
	for k, v := range env {
		_ = os.Setenv(k, v)
	}
	defer os.Clearenv()

	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}

		var got api.TestPlanMetadataParams

		err = json.Unmarshal(b, &got)
		if err != nil {
			t.Fatal(err)
		}

		want := api.TestPlanMetadataParams{
			Version:  "0.1.0",
			Timeline: timeline,
			SplitterEnv: map[string]string{
				"BUILDKITE_BUILD_ID":               "xyz",
				"BUILDKITE_JOB_ID":                 "abc",
				"BUILDKITE_ORGANIZATION_SLUG":      "buildkite",
				"BUILDKITE_PARALLEL_JOB_COUNT":     "10",
				"BUILDKITE_PARALLEL_JOB":           "5",
				"BUILDKITE_SPLITTER_DEBUG_ENABLED": "true",
				// ensure that the identifier is included in the request
				"BUILDKITE_SPLITTER_IDENTIFIER":  "fruitsabc",
				"BUILDKITE_SPLITTER_RETRY_COUNT": "2",
				"BUILDKITE_SPLITTER_RETRY_CMD":   "bundle exec rspec --only-failures",
				"BUILDKITE_SPLITTER_SUITE_SLUG":  "rspec",
				"BUILDKITE_SPLITTER_TEST_CMD":    "bundle exec rspec",
				"BUILDKITE_STEP_ID":              "pqr",
				// ensure that empty env vars is included in the request
				"BUILDKITE_SPLITTER_SLOW_FILE_THRESHOLD":       "",
				"BUILDKITE_SPLITTER_SPLIT_BY_EXAMPLE":          "",
				"BUILDKITE_SPLITTER_TEST_FILE_EXCLUDE_PATTERN": "",
				"BUILDKITE_SPLITTER_TEST_FILE_PATTERN":         "",
				"BUILDKITE_SPLITTER_TEST_RUNNER":               "rspec",
				"BUILDKITE_BRANCH":                             "",
			},
		}

		if diff := cmp.Diff(got, want); diff != "" {
			t.Errorf("sendMetadata() request params diff (-got +want):\n%s", diff)
			w.WriteHeader(http.StatusBadRequest)
		} else {
			w.WriteHeader(http.StatusOK)
		}

	}))
	defer svr.Close()

	cfg := config.Config{
		OrganizationSlug: "buildkite",
		SuiteSlug:        "rspec",
		Identifier:       "fruitsabc",
		ServerBaseUrl:    svr.URL,
	}
	client := api.NewClient(api.ClientConfig{
		ServerBaseUrl: cfg.ServerBaseUrl,
	})

	sendMetadata(context.Background(), client, cfg, timeline)
}

func TestSendMetadata_Unauthorized(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message": "Unauthorized"}`, http.StatusUnauthorized)
	}))
	defer svr.Close()

	cfg := config.Config{
		OrganizationSlug: "my-org",
		SuiteSlug:        "my-suite",
		Identifier:       "identifier",
		ServerBaseUrl:    svr.URL,
	}
	client := api.NewClient(api.ClientConfig{
		ServerBaseUrl: cfg.ServerBaseUrl,
	})

	timeline := []api.Timeline{}

	sendMetadata(context.Background(), client, cfg, timeline)
}
