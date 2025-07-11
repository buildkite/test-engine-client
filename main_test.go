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

	"github.com/buildkite/test-engine-client/internal/api"
	"github.com/buildkite/test-engine-client/internal/config"
	"github.com/buildkite/test-engine-client/internal/env"
	"github.com/buildkite/test-engine-client/internal/plan"
	"github.com/buildkite/test-engine-client/internal/runner"
	"github.com/buildkite/test-engine-client/internal/version"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
)

func TestRunTestsWithRetry(t *testing.T) {
	testRunner := runner.NewRspec(runner.RunnerConfig{
		TestCommand: "rspec --format json --out {{resultPath}}",
		ResultPath:  "tmp/rspec.json",
	})
	maxRetries := 3
	testCases := []plan.TestCase{
		{
			Path: "./testdata/rspec/spec/fruits/apple_spec.rb",
		},
	}
	timeline := []api.Timeline{}
	testResult, err := runTestsWithRetry(testRunner, &testCases, maxRetries, []plan.TestCase{}, &timeline, true)

	t.Cleanup(func() {
		os.Remove(testRunner.ResultPath)
	})

	if err != nil {
		t.Errorf("runTestsWithRetry(...) error = %v", err)
	}

	if testResult.Status() != runner.RunStatusPassed {
		t.Errorf("runTestsWithRetry(...) testResult.Status = %v, want %v", testResult.Status(), runner.RunStatusPassed)
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

func TestRunTestsWithRetry_TestPassedAfterRetry(t *testing.T) {
	testRunner := runner.NewRspec(runner.RunnerConfig{
		TestCommand: "rspec --format json --out {{resultPath}}",
		// Simulate test passing on the second retry
		RetryTestCommand: "./testdata/retry.sh rspec --format json --out {{resultPath}}",
		ResultPath:       "tmp/rspec.json",
	})
	maxRetries := 2
	testCases := []plan.TestCase{
		{
			Path: "./testdata/rspec/spec/fruits/apple_spec.rb",
		},
		{
			Path: "./testdata/rspec/spec/fruits/tomato_spec.rb",
		},
	}
	timeline := []api.Timeline{}
	testResult, err := runTestsWithRetry(testRunner, &testCases, maxRetries, []plan.TestCase{}, &timeline, true)

	t.Cleanup(func() {
		os.Remove(testRunner.ResultPath)
	})

	if err != nil {
		t.Errorf("runTestsWithRetry(...) error = %v", err)
	}

	if testResult.Status() != runner.RunStatusPassed {
		t.Errorf("runTestsWithRetry(...) testResult.Status = %v, want %v", testResult.Status(), runner.RunStatusPassed)
	}

	retriedTestCases := []plan.TestCase{
		{
			Scope:      "Tomato",
			Name:       "is vegetable",
			Path:       "./testdata/rspec/spec/fruits/tomato_spec.rb[1:2]",
			Identifier: "./testdata/rspec/spec/fruits/tomato_spec.rb[1:2]",
		},
	}

	if diff := cmp.Diff(testCases, retriedTestCases); diff != "" {
		t.Errorf("testCases diff (-got +want):\n%s", diff)
	}

	if len(timeline) != 4 {
		t.Errorf("timeline length = %v, want %d", len(timeline), 4)
	}

	events := []string{}
	for _, event := range timeline {
		events = append(events, event.Event)
	}
	if diff := cmp.Diff(events, []string{"test_start", "test_end", "retry_1_start", "retry_1_end"}); diff != "" {
		t.Errorf("timeline events diff (-got +want):\n%s", diff)
	}
}

func TestRunTestsWithRetry_TestFailedAfterRetry(t *testing.T) {
	testRunner := runner.NewRspec(runner.RunnerConfig{
		TestCommand:      "rspec --format json --out {{resultPath}}",
		RetryTestCommand: "rspec --format json --out {{resultPath}}",
		ResultPath:       "tmp/rspec.json",
	})
	maxRetries := 2
	testCases := []plan.TestCase{
		{
			Path: "testdata/rspec/spec/fruits/apple_spec.rb",
		},
		{
			Path: "testdata/rspec/spec/fruits/tomato_spec.rb",
		},
	}
	timeline := []api.Timeline{}
	testResult, err := runTestsWithRetry(testRunner, &testCases, maxRetries, []plan.TestCase{}, &timeline, true)

	t.Cleanup(func() {
		os.Remove(testRunner.ResultPath)
	})

	if err != nil {
		t.Errorf("runTestsWithRetry(...) error = %v", err)
	}

	if testResult.Status() != runner.RunStatusFailed {
		t.Errorf("runTestsWithRetry(...) testResult.Status = %v, want %v", testResult.Status(), runner.RunStatusFailed)
	}

	wantFailedTests := []plan.TestCase{
		{
			Scope:      "Tomato",
			Name:       "is vegetable",
			Path:       "./testdata/rspec/spec/fruits/tomato_spec.rb[1:2]",
			Identifier: "./testdata/rspec/spec/fruits/tomato_spec.rb[1:2]",
		},
	}

	if diff := cmp.Diff(testResult.FailedTests(), wantFailedTests); diff != "" {
		t.Errorf("runTestsWithRetry(...) testResult.FailedTests diff (-got +want):\n%s", diff)
	}

	if diff := cmp.Diff(testCases, wantFailedTests); diff != "" {
		t.Errorf("testCases diff (-got +want):\n%s", diff)
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

func TestRunTestsWithRetry_NoRetryForMutedTest(t *testing.T) {
	testRunner := runner.NewRspec(runner.RunnerConfig{
		TestCommand:      "rspec --format json --out {{resultPath}}  --format documentation",
		ResultPath:       "tmp/rspec.json",
		RetryTestCommand: "rspec --format json --out {{resultPath}}",
	})
	maxRetries := 1
	testCases := []plan.TestCase{
		{
			Path: "./testdata/rspec/spec/fruits/apple_spec.rb",
		},
		{
			// File with failed tests that are muted
			Path: "./testdata/rspec/spec/fruits/tomato_spec.rb",
		},
	}
	mutedTests := []plan.TestCase{
		{Path: "apple_spec.rb:6", Scope: "Apple", Name: "is red"},
		{Path: "tomato_spec.rb:6", Scope: "Tomato", Name: "is vegetable"},
	}
	timeline := []api.Timeline{}
	testResult, err := runTestsWithRetry(testRunner, &testCases, maxRetries, mutedTests, &timeline, false)

	t.Cleanup(func() {
		os.Remove(testRunner.ResultPath)
	})

	if err != nil {
		t.Errorf("runTestsWithRetry(...) error = %v", err)
	}

	if testResult.Status() != runner.RunStatusPassed {
		t.Errorf("runTestsWithRetry(...) testResult.Status = %v, want %v", testResult.Status(), runner.RunStatusPassed)
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

func TestRunTestsWithRetry_RetryForMutedTest(t *testing.T) {
	testRunner := runner.NewRspec(runner.RunnerConfig{
		TestCommand:      "rspec --format json --out {{resultPath}}  --format documentation",
		ResultPath:       "tmp/rspec.json",
		RetryTestCommand: "rspec --format json --out {{resultPath}}",
	})
	maxRetries := 1
	testCases := []plan.TestCase{
		{
			Path: "./testdata/rspec/spec/fruits/apple_spec.rb",
		},
		{
			// File with failed tests that are muted
			Path: "./testdata/rspec/spec/fruits/tomato_spec.rb",
		},
	}
	mutedTests := []plan.TestCase{
		{Path: "tomato_spec.rb:6", Scope: "Tomato", Name: "is vegetable"},
	}
	timeline := []api.Timeline{}
	testResult, err := runTestsWithRetry(testRunner, &testCases, maxRetries, mutedTests, &timeline, true)

	t.Cleanup(func() {
		os.Remove(testRunner.ResultPath)
	})

	assert.NoError(t, err)

	if testResult.Status() != runner.RunStatusPassed {
		t.Errorf("runTestsWithRetry(...) testResult.Status = %v, want %v", testResult.Status(), runner.RunStatusPassed)
	}

	wantFailedTests := []plan.TestCase{
		{
			Scope:      "Tomato",
			Name:       "is vegetable",
			Path:       "./testdata/rspec/spec/fruits/tomato_spec.rb[1:2]",
			Identifier: "./testdata/rspec/spec/fruits/tomato_spec.rb[1:2]",
		},
	}

	if diff := cmp.Diff(testResult.FailedMutedTests(), wantFailedTests); diff != "" {
		t.Errorf("runTestsWithRetry(...) testResult.FailedTests diff (-got +want):\n%s", diff)
	}

	if diff := cmp.Diff(testCases, wantFailedTests); diff != "" {
		t.Errorf("testCases diff (-got +want):\n%s", diff)
	}

	if len(timeline) != 4 {
		t.Errorf("timeline length = %v, want %d", len(timeline), 4)
	}

	events := []string{}
	for _, event := range timeline {
		events = append(events, event.Event)
	}
	if diff := cmp.Diff(events, []string{"test_start", "test_end", "retry_1_start", "retry_1_end"}); diff != "" {
		t.Errorf("timeline events diff (-got +want):\n%s", diff)
	}
}

func TestRunTestsWithRetry_ExecError(t *testing.T) {
	testRunner := runner.NewRspec(runner.RunnerConfig{
		TestCommand: "foobar",
	})
	testCases := []plan.TestCase{}
	timeline := []api.Timeline{}
	testResult, err := runTestsWithRetry(testRunner, &testCases, 0, []plan.TestCase{}, &timeline, true)

	var execError *exec.Error
	if !errors.As(err, &execError) {
		t.Errorf("runTestsWithRetry(%q) error type = %T (%v), want *exec.Error", testCases, err, err)
	}

	if testResult.Status() != runner.RunStatusUnknown {
		t.Errorf("runTestsWithRetry(...) testResult.Status = %v, want %v", testResult.Status(), runner.RunStatusUnknown)
	}
}

func TestRunTestsWithRetry_CommandError(t *testing.T) {
	testRunner := runner.NewRspec(runner.RunnerConfig{
		TestCommand: "rspec --invalid-option",
	})
	maxRetries := 2
	testCases := []plan.TestCase{
		{Path: "testdata/rspec/spec/fruits/fig_spec.rb"},
	}
	timeline := []api.Timeline{}
	testResult, err := runTestsWithRetry(testRunner, &testCases, maxRetries, []plan.TestCase{}, &timeline, true)

	exitError := new(exec.ExitError)
	if !errors.As(err, &exitError) {
		t.Errorf("runTestsWithRetry(...) error type = %T (%v), want *exec.ExitError", err, err)
	}

	if testResult.Status() != runner.RunStatusUnknown {
		t.Errorf("runTestsWithRetry(...) testResult.Status = %v, want %v", testResult.Status(), runner.RunStatusUnknown)
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

func TestRunTestsWithRetry_RunResultError(t *testing.T) {
	testRunner := runner.NewRspec(runner.RunnerConfig{
		TestCommand:      "rspec --format json --out {{resultPath}}",
		RetryTestCommand: "rspec --format json --out {{resultPath}}",
		ResultPath:       "tmp/rspec.json",
	})
	maxRetries := 2
	testCases := []plan.TestCase{
		{
			Path: "testdata/rspec/spec/fruits/apple_spec.rb",
		},
		{
			Path: "testdata/rspec/spec/fruits/bad_syntax.rb",
		},
	}
	timeline := []api.Timeline{}
	testResult, err := runTestsWithRetry(testRunner, &testCases, maxRetries, []plan.TestCase{}, &timeline, true)

	t.Cleanup(func() {
		os.Remove(testRunner.ResultPath)
	})

	if err != nil {
		t.Errorf("runTestsWithRetry(...) error = %v", err)
	}

	if testResult.Status() != runner.RunStatusError {
		t.Errorf("runTestsWithRetry(...) testResult.Status = %v, want %v", testResult.Status(), runner.RunStatusError)
	}

	// If RunResult.Status() is RunStatusError, and there are no failed tests, it shouldn't do retry.
	if len(timeline) != 2 {
		t.Errorf("timeline length = %v, want %d", len(timeline), 2)
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
		Env:           env.Map{},
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
		Env:              env.Map{},
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
		Env:           env.Map{},
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
		Env:           env.Map{},
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
		Env:           env.Map{},
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

func TestFetchOrCreateTestPlan_BillingError(t *testing.T) {
	files := []string{"apple", "banana"}
	testRunner := runner.Rspec{}

	// mock server to return 403 with a billing error
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message": "Billing Error: please update your plan"}`, http.StatusForbidden)
	}))
	defer svr.Close()

	ctx := context.Background()

	cfg := config.Config{
		NodeIndex:     0,
		Parallelism:   2,
		Identifier:    "identifier",
		Branch:        "",
		ServerBaseUrl: svr.URL,
		Env:           env.Map{},
	}
	apiClient := api.NewClient(api.ClientConfig{
		ServerBaseUrl: cfg.ServerBaseUrl,
	})

	// we want the function to return a fallback plan
	want := plan.CreateFallbackPlan(files, cfg.Parallelism)

	got, err := fetchOrCreateTestPlan(ctx, apiClient, cfg, files, testRunner)
	if err != nil {
		t.Errorf("fetchOrCreateTestPlan(ctx, %v, %v) error = %v", cfg, files, err)
	}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("fetchOrCreateTestPlan(ctx, %v, %v) diff (-got +want):\n%s", cfg, files, diff)
	}
}

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
		Env:              env.Map{},
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

	got, err := createRequestParam(context.Background(), cfg, files, *client, runner.Rspec{
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
				Env:              env.Map{},
			}

			client := api.NewClient(api.ClientConfig{
				ServerBaseUrl: svr.URL,
			})
			files := []string{
				"testdata/fruits/apple.spec.js",
				"testdata/fruits/banana.spec.js",
				"testdata/fruits/cherry.spec.js",
			}

			got, err := createRequestParam(context.Background(), cfg, files, *client, r)

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
			Env:              env.Map{},
		}

		client := api.NewClient(api.ClientConfig{
			ServerBaseUrl: svr.URL,
		})
		files := []string{
			"test/apple_test.py",
			"test/banana_test.py",
			"test/cherry_test.py",
		}

		got, err := createRequestParam(context.Background(), cfg, files, *client, runner)

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
		Env:              env.Map{},
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
		Env:              env.Map{},
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

	got, err := createRequestParam(context.Background(), cfg, files, *client, runner.Rspec{
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

func TestSendMetadata(t *testing.T) {
	originalVersion := version.Version
	version.Version = "0.1.0"
	defer func() {
		version.Version = originalVersion
	}()

	timeline := []api.Timeline{
		{Event: "test_start", Timestamp: "2024-06-20T04:46:13.60977Z"},
		{Event: "test_end", Timestamp: "2024-06-20T04:49:09.609793Z"},
	}

	env := env.Map{
		"BUILDKITE_BUILD_ID":                  "xyz",
		"BUILDKITE_JOB_ID":                    "abc",
		"BUILDKITE_STEP_ID":                   "pqr",
		"BUILDKITE_ORGANIZATION_SLUG":         "buildkite",
		"BUILDKITE_PARALLEL_JOB_COUNT":        "10",
		"BUILDKITE_PARALLEL_JOB":              "5",
		"BUILDKITE_TEST_ENGINE_DEBUG_ENABLED": "true",
		"BUILDKITE_TEST_ENGINE_RETRY_COUNT":   "2",
		"BUILDKITE_TEST_ENGINE_RETRY_CMD":     "bundle exec rspec --only-failures",
		"BUILDKITE_TEST_ENGINE_SUITE_SLUG":    "rspec",
		"BUILDKITE_TEST_ENGINE_TEST_CMD":      "bundle exec rspec",
		"BUILDKITE_TEST_ENGINE_TEST_RUNNER":   "rspec",
		"BUILDKITE_RETRY_COUNT":               "0",
	}

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
			Env: map[string]string{
				"BUILDKITE_BUILD_ID":                  "xyz",
				"BUILDKITE_JOB_ID":                    "abc",
				"BUILDKITE_ORGANIZATION_SLUG":         "buildkite",
				"BUILDKITE_PARALLEL_JOB_COUNT":        "10",
				"BUILDKITE_PARALLEL_JOB":              "5",
				"BUILDKITE_TEST_ENGINE_DEBUG_ENABLED": "true",
				// ensure that the identifier is included in the request
				"BUILDKITE_TEST_ENGINE_IDENTIFIER":  "fruitsabc",
				"BUILDKITE_TEST_ENGINE_RETRY_COUNT": "2",
				"BUILDKITE_TEST_ENGINE_RETRY_CMD":   "bundle exec rspec --only-failures",
				"BUILDKITE_TEST_ENGINE_SUITE_SLUG":  "rspec",
				"BUILDKITE_TEST_ENGINE_TEST_CMD":    "bundle exec rspec",
				"BUILDKITE_STEP_ID":                 "pqr",
				// ensure that empty env vars is included in the request
				"BUILDKITE_TEST_ENGINE_SPLIT_BY_EXAMPLE":          "",
				"BUILDKITE_TEST_ENGINE_TEST_FILE_EXCLUDE_PATTERN": "",
				"BUILDKITE_TEST_ENGINE_TEST_FILE_PATTERN":         "",
				"BUILDKITE_TEST_ENGINE_TEST_RUNNER":               "rspec",
				"BUILDKITE_BRANCH":                                "",
				"BUILDKITE_RETRY_COUNT":                           "0",
			},
			Statistics: runner.RunStatistics{
				Total: 3,
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
		Env:              env,
	}
	client := api.NewClient(api.ClientConfig{
		ServerBaseUrl: cfg.ServerBaseUrl,
	})

	statistics := runner.RunStatistics{
		Total: 3,
	}

	sendMetadata(context.Background(), client, cfg, timeline, statistics)
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
		Env:              env.Map{},
	}
	client := api.NewClient(api.ClientConfig{
		ServerBaseUrl: cfg.ServerBaseUrl,
	})

	timeline := []api.Timeline{}

	statistics := runner.RunStatistics{
		Total: 3,
	}

	sendMetadata(context.Background(), client, cfg, timeline, statistics)
}
