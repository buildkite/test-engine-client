package command

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
	"github.com/buildkite/test-engine-client/internal/plan"
	"github.com/buildkite/test-engine-client/internal/runner"
	"github.com/buildkite/test-engine-client/internal/version"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
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
	testResult, err := runTestsWithRetry(testRunner, &testCases, maxRetries, []plan.TestCase{}, &timeline, true, false)

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
	testResult, err := runTestsWithRetry(testRunner, &testCases, maxRetries, []plan.TestCase{}, &timeline, true, false)

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
	testResult, err := runTestsWithRetry(testRunner, &testCases, maxRetries, []plan.TestCase{}, &timeline, true, false)

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
	testResult, err := runTestsWithRetry(testRunner, &testCases, maxRetries, mutedTests, &timeline, false, false)

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
	testResult, err := runTestsWithRetry(testRunner, &testCases, maxRetries, mutedTests, &timeline, true, false)

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
	testCases := []plan.TestCase{
		{Path: "testdata/rspec/spec/fruits/fig_spec.rb"},
	}
	timeline := []api.Timeline{}
	testResult, err := runTestsWithRetry(testRunner, &testCases, 0, []plan.TestCase{}, &timeline, true, false)

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
	testResult, err := runTestsWithRetry(testRunner, &testCases, maxRetries, []plan.TestCase{}, &timeline, true, false)

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
	testResult, err := runTestsWithRetry(testRunner, &testCases, maxRetries, []plan.TestCase{}, &timeline, true, false)

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

	got, err := fetchOrCreateTestPlan(ctx, apiClient, &cfg, files, testRunner)
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

	got, err := fetchOrCreateTestPlan(context.Background(), apiClient, &cfg, tests, testRunner)
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

	got, err := fetchOrCreateTestPlan(ctx, apiClient, &cfg, files, TestRunner)
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

	got, err := fetchOrCreateTestPlan(fetchCtx, apiClient, &cfg, files, testRunner)
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

	got, err := fetchOrCreateTestPlan(ctx, apiClient, &cfg, files, testRunner)
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
	}
	apiClient := api.NewClient(api.ClientConfig{
		ServerBaseUrl: cfg.ServerBaseUrl,
	})

	// we want the function to return a fallback plan
	want := plan.CreateFallbackPlan(files, cfg.Parallelism)

	got, err := fetchOrCreateTestPlan(ctx, apiClient, &cfg, files, testRunner)
	if err != nil {
		t.Errorf("fetchOrCreateTestPlan(ctx, %v, %v) error = %v", cfg, files, err)
	}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("fetchOrCreateTestPlan(ctx, %v, %v) diff (-got +want):\n%s", cfg, files, diff)
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

	cfg := config.Config{
		BuildId:          "xyz",
		JobId:            "abc",
		StepId:           "pqr",
		OrganizationSlug: "buildkite",
		Parallelism:      10,
		NodeIndex:        5,
		MaxRetries:       2,
		RetryCommand:     "bundle exec rspec --only-failures",
		SuiteSlug:        "rspec",
		TestCommand:      "bundle exec rspec",
		TestRunner:       "rspec",
		JobRetryCount:    0,
		Identifier:       "fruitsabc",
		DebugEnabled:     true,
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
			Env:      &cfg,
			Statistics: runner.RunStatistics{
				Total: 3,
			},
		}

		if diff := cmp.Diff(got, want, cmpopts.IgnoreUnexported(config.Config{})); diff != "" {
			t.Errorf("sendMetadata() request params diff (-got +want):\n%s", diff)
			w.WriteHeader(http.StatusBadRequest)
		} else {
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer svr.Close()

	client := api.NewClient(api.ClientConfig{
		ServerBaseUrl: svr.URL,
	})

	statistics := runner.RunStatistics{
		Total: 3,
	}

	sendMetadata(context.Background(), client, &cfg, timeline, statistics)
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

	statistics := runner.RunStatistics{
		Total: 3,
	}

	sendMetadata(context.Background(), client, &cfg, timeline, statistics)
}

func TestRunTestsWithRetry_NoTestCases_Success(t *testing.T) {
	testRunner := runner.NewRspec(runner.RunnerConfig{
		TestCommand: "rspec --format json --out {{resultPath}}",
		ResultPath:  "tmp/rspec.json",
	})
	maxRetries := 3
	testCases := []plan.TestCase{}
	timeline := []api.Timeline{}
	failOnNoTests := false

	testResult, err := runTestsWithRetry(testRunner, &testCases, maxRetries, []plan.TestCase{}, &timeline, true, failOnNoTests)
	if err != nil {
		t.Errorf("runTestsWithRetry(...) error = %v, want nil", err)
	}

	if testResult.Status() != runner.RunStatusUnknown {
		t.Errorf("runTestsWithRetry(...) testResult.Status = %v, want %v", testResult.Status(), runner.RunStatusUnknown)
	}

	if len(timeline) != 0 {
		t.Errorf("timeline length = %v, want 0", len(timeline))
	}
}

func TestRunTestsWithRetry_NoTestCases_Error(t *testing.T) {
	testRunner := runner.NewRspec(runner.RunnerConfig{
		TestCommand: "rspec --format json --out {{resultPath}}",
		ResultPath:  "tmp/rspec.json",
	})
	maxRetries := 3
	testCases := []plan.TestCase{}
	timeline := []api.Timeline{}
	failOnNoTests := true

	testResult, err := runTestsWithRetry(testRunner, &testCases, maxRetries, []plan.TestCase{}, &timeline, true, failOnNoTests)

	if err == nil {
		t.Errorf("runTestsWithRetry(...) error = nil, want error")
	}

	if err.Error() != "no tests assigned to this node" {
		t.Errorf("runTestsWithRetry(...) error = %v, want 'no tests assigned to this node'", err.Error())
	}

	if testResult.Status() != runner.RunStatusUnknown {
		t.Errorf("runTestsWithRetry(...) testResult.Status = %v, want %v", testResult.Status(), runner.RunStatusUnknown)
	}

	if len(timeline) != 0 {
		t.Errorf("timeline length = %v, want 0", len(timeline))
	}
}
