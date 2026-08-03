package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/buildkite/test-engine-client/v3/internal/plan"
	"github.com/google/go-cmp/cmp"
)

func TestCreateTestPlan(t *testing.T) {
	params := TestPlanParams{
		Runner:      "rspec",
		Branch:      "tet-123-add-branch-name",
		Identifier:  "abc123",
		Parallelism: 3,
		Selection: &SelectionParams{
			Strategy: "least-reliable",
			Params: map[string]string{
				"top": "100",
			},
		},
		Metadata: map[string]string{
			"git_diff": "line1\nline2",
		},
		Tests: TestPlanParamsTest{
			Files: []TestPlanFile{
				{Path: "sky_spec.rb"},
			},
		},
	}

	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("request method = %q, want %q", r.Method, http.MethodPost)
		}
		if r.URL.Path != "/v2/analytics/organizations/buildkite/suites/rspec/test_plan" {
			t.Errorf("request path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer asdf1234" {
			t.Errorf("Authorization header = %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type header = %q", got)
		}

		assertJSONBody(t, r.Body, `{
			"runner": "rspec",
			"identifier": "abc123",
			"parallelism": 3,
			"branch": "tet-123-add-branch-name",
			"tests": {"files": [{"path": "sky_spec.rb"}]},
			"selection": {"strategy": "least-reliable", "params": {"top": "100"}},
			"metadata": {"git_diff": "line1\nline2"}
		}`)

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = io.WriteString(w, `{
			"tasks": {
				"0": {"node_number": 0, "tests": [{"path": "sky_spec.rb", "format": "file", "estimated_duration": 1000}]},
				"1": {"node_number": 1, "tests": []},
				"2": {"node_number": 2, "tests": []}
			}
		}`)
	}))
	defer svr.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	apiClient := NewClient(ClientConfig{
		AccessToken:      "asdf1234",
		OrganizationSlug: "buildkite",
		ServerBaseURL:    svr.URL,
	})

	got, err := apiClient.CreateTestPlan(ctx, "rspec", params)
	if err != nil {
		t.Fatalf("CreateTestPlan() error = %v", err)
	}

	want := plan.TestPlan{
		Tasks: map[string]*plan.Task{
			"0": {
				NodeNumber: 0,
				Tests: []plan.TestCase{{
					Path:              "sky_spec.rb",
					Format:            "file",
					EstimatedDuration: 1000,
				}},
			},
			"1": {NodeNumber: 1, Tests: []plan.TestCase{}},
			"2": {NodeNumber: 2, Tests: []plan.TestCase{}},
		},
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("CreateTestPlan() diff (-got +want):\n%s", diff)
	}
}

func TestCreateTestPlanRaw(t *testing.T) {
	// Include a field the client struct does not model to confirm the raw bytes
	// are returned verbatim.
	serverBody := `{"identifier":"facecafe","parallelism":2,"tasks":{"0":{"node_number":0,"tests":[{"path":"sky_spec.rb"}]}},"server_only":"kept"}`

	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = io.WriteString(w, serverBody)
	}))
	defer svr.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	apiClient := NewClient(ClientConfig{
		AccessToken:      "asdf1234",
		OrganizationSlug: "buildkite",
		ServerBaseURL:    svr.URL,
	})

	gotPlan, gotRaw, err := apiClient.CreateTestPlanRaw(ctx, "rspec", TestPlanParams{Identifier: "facecafe"})
	if err != nil {
		t.Fatalf("CreateTestPlanRaw() error = %v", err)
	}

	// Raw bytes are returned unmodified.
	if string(gotRaw) != serverBody {
		t.Errorf("CreateTestPlanRaw() raw = %s, want %s", gotRaw, serverBody)
	}

	// The decoded plan is also returned for control-flow decisions.
	wantPlan := plan.TestPlan{
		Identifier:  "facecafe",
		Parallelism: 2,
		Tasks: map[string]*plan.Task{
			"0": {NodeNumber: 0, Tests: []plan.TestCase{{Path: "sky_spec.rb"}}},
		},
	}
	if diff := cmp.Diff(gotPlan, wantPlan); diff != "" {
		t.Errorf("CreateTestPlanRaw() plan diff (-got +want):\n%s", diff)
	}
}

func TestCreateTestPlan_SplitByExample(t *testing.T) {
	params := TestPlanParams{
		Identifier:  "abc123",
		Parallelism: 3,
		Selection: &SelectionParams{
			Strategy: "percent",
			Params: map[string]string{
				"percent": "40",
			},
		},
		Metadata: map[string]string{
			"source": "cli",
		},
		Tests: TestPlanParamsTest{
			Files: []TestPlanFile{
				{Path: "sky_spec.rb"},
			},
			Examples: []TestPlanExample{
				{
					Format:     plan.TestCaseFormatExample,
					Path:       "sea_spec.rb:4",
					Name:       "is blue",
					Scope:      "sea",
					Identifier: "sea_spec.rb[1,1]",
				},
			},
		},
		Runner: "rspec",
	}

	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertJSONBody(t, r.Body, `{
			"runner": "rspec",
			"identifier": "abc123",
			"parallelism": 3,
			"branch": "",
			"tests": {
				"files": [{"path": "sky_spec.rb"}],
				"examples": [{
					"format": "example",
					"path": "sea_spec.rb:4",
					"name": "is blue",
					"scope": "sea",
					"identifier": "sea_spec.rb[1,1]"
				}]
			},
			"selection": {"strategy": "percent", "params": {"percent": "40"}},
			"metadata": {"source": "cli"}
		}`)

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = io.WriteString(w, `{
			"tasks": {
				"0": {"node_number": 0, "tests": [{"path": "sea_spec.rb:4", "name": "is blue", "scope": "sea", "identifier": "sea_spec.rb[1,1]", "format": "example", "estimated_duration": 1000}]},
				"1": {"node_number": 1, "tests": [{"path": "sky_spec.rb", "format": "file", "estimated_duration": 1000}]},
				"2": {"node_number": 2, "tests": []}
			}
		}`)
	}))
	defer svr.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	apiClient := NewClient(ClientConfig{
		AccessToken:      "asdf1234",
		OrganizationSlug: "buildkite",
		ServerBaseURL:    svr.URL,
	})

	got, err := apiClient.CreateTestPlan(ctx, "rspec", params)
	if err != nil {
		t.Fatalf("CreateTestPlan() error = %v", err)
	}

	want := plan.TestPlan{
		Tasks: map[string]*plan.Task{
			"0": {
				NodeNumber: 0,
				Tests: []plan.TestCase{{
					Path:              "sea_spec.rb:4",
					Name:              "is blue",
					Scope:             "sea",
					Identifier:        "sea_spec.rb[1,1]",
					Format:            "example",
					EstimatedDuration: 1000,
				}},
			},
			"1": {
				NodeNumber: 1,
				Tests: []plan.TestCase{{
					Path:              "sky_spec.rb",
					Format:            "file",
					EstimatedDuration: 1000,
				}},
			},
			"2": {NodeNumber: 2, Tests: []plan.TestCase{}},
		},
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("CreateTestPlan() diff (-got +want):\n%s", diff)
	}
}

func TestCreateTestPlan_Selector(t *testing.T) {
	params := TestPlanParams{
		Runner:         "gotest",
		Identifier:     "abc123",
		Parallelism:    2,
		LocationPrefix: "components/backend",
		Tests: TestPlanParamsTest{
			Selectors: []TestPlanParamsSelector{
				{Value: "github.com/buildkite/test-engine-client/internal/api"},
				{Value: "github.com/buildkite/test-engine-client/internal/runner"},
			},
		},
	}

	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertJSONBody(t, r.Body, `{
			"runner": "gotest",
			"identifier": "abc123",
			"parallelism": 2,
			"branch": "",
			"location_prefix": "components/backend",
			"tests": {
				"selectors": [
					{"value": "github.com/buildkite/test-engine-client/internal/api"},
					{"value": "github.com/buildkite/test-engine-client/internal/runner"}
				]
			}
		}`)

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = io.WriteString(w, `{
			"tasks": {
				"0": {"node_number": 0, "tests": [{"value": "github.com/buildkite/test-engine-client/internal/api", "format": "selector", "estimated_duration": 3000, "timing_sample_size": 7}]},
				"1": {"node_number": 1, "tests": [{"value": "github.com/buildkite/test-engine-client/internal/runner", "format": "selector", "estimated_duration": 500, "timing_sample_size": 2}]}
			},
			"timing_metadata": {
				"selector": {"median_duration": 1750, "default_duration": 1000}
			}
		}`)
	}))
	defer svr.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	apiClient := NewClient(ClientConfig{
		AccessToken:      "asdf1234",
		OrganizationSlug: "buildkite",
		ServerBaseURL:    svr.URL,
	})

	got, err := apiClient.CreateTestPlan(ctx, "gotest", params)
	if err != nil {
		t.Fatalf("CreateTestPlan() error = %v", err)
	}

	medianDuration := 1750.0
	want := plan.TestPlan{
		Tasks: map[string]*plan.Task{
			"0": {
				NodeNumber: 0,
				Tests: []plan.TestCase{{
					Value:             "github.com/buildkite/test-engine-client/internal/api",
					Format:            plan.TestCaseFormatSelector,
					EstimatedDuration: 3000,
					TimingSampleSize:  7,
				}},
			},
			"1": {
				NodeNumber: 1,
				Tests: []plan.TestCase{{
					Value:             "github.com/buildkite/test-engine-client/internal/runner",
					Format:            plan.TestCaseFormatSelector,
					EstimatedDuration: 500,
					TimingSampleSize:  2,
				}},
			},
		},
		TimingMetadata: &plan.TimingMetadata{
			Selector: &plan.FormatTimingMetadata{MedianDuration: &medianDuration, DefaultDuration: 1000},
		},
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("CreateTestPlan() diff (-got +want):\n%s", diff)
	}
}

func TestCreateTestPlan_BadRequest(t *testing.T) {
	requestCount := 0
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		http.Error(w, `{"message": "bad request"}`, http.StatusBadRequest)
	}))
	defer svr.Close()

	ctx := context.Background()
	params := TestPlanParams{}
	apiClient := NewClient(ClientConfig{
		ServerBaseURL: svr.URL,
	})

	got, err := apiClient.CreateTestPlan(ctx, "my-suite", params)

	wantTestPlan := plan.TestPlan{}

	if requestCount > 1 {
		t.Errorf("http request count = %v, want %d", requestCount, 1)
	}

	if diff := cmp.Diff(got, wantTestPlan); diff != "" {
		t.Errorf("CreateTestPlan() diff (-got +want):\n%s", diff)
	}

	if err.Error() != "bad request" {
		t.Errorf("CreateTestPlan() error = %v, want %v", err, ErrRetryTimeout)
	}
}

func TestCreateTestPlan_MutedTests(t *testing.T) {
	params := TestPlanParams{
		Runner:      "rspec",
		Branch:      "tet-123-add-branch-name",
		Identifier:  "abc123",
		Parallelism: 3,
		Tests: TestPlanParamsTest{
			Files: []TestPlanFile{
				{Path: "sky_spec.rb"},
			},
		},
	}

	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertJSONBody(t, r.Body, `{
			"runner": "rspec",
			"identifier": "abc123",
			"parallelism": 3,
			"branch": "tet-123-add-branch-name",
			"tests": {"files": [{"path": "sky_spec.rb"}]}
		}`)

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = io.WriteString(w, `{
			"tasks": {
				"0": {"node_number": 0, "tests": [{"path": "sky_spec.rb", "format": "file", "estimated_duration": 1000}]},
				"1": {"node_number": 1, "tests": []},
				"2": {"node_number": 2, "tests": []}
			},
			"muted_tests": [{"path": "./turtle_spec.rb:3", "scope": "turtle", "name": "is green"}]
		}`)
	}))
	defer svr.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	apiClient := NewClient(ClientConfig{
		AccessToken:      "asdf1234",
		OrganizationSlug: "buildkite",
		ServerBaseURL:    svr.URL,
	})

	got, err := apiClient.CreateTestPlan(ctx, "rspec", params)
	if err != nil {
		t.Fatalf("CreateTestPlan() error = %v", err)
	}

	want := plan.TestPlan{
		Tasks: map[string]*plan.Task{
			"0": {
				NodeNumber: 0,
				Tests: []plan.TestCase{{
					Path:              "sky_spec.rb",
					Format:            "file",
					EstimatedDuration: 1000,
				}},
			},
			"1": {NodeNumber: 1, Tests: []plan.TestCase{}},
			"2": {NodeNumber: 2, Tests: []plan.TestCase{}},
		},
		MutedTests: []plan.TestCase{{Name: "is green", Path: "./turtle_spec.rb:3", Scope: "turtle"}},
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("CreateTestPlan() diff (-got +want):\n%s", diff)
	}
}

func TestCreateTestPlan_InternalServerError(t *testing.T) {
	originalTimeout := retryTimeout
	retryTimeout = 1 * time.Millisecond
	t.Cleanup(func() {
		retryTimeout = originalTimeout
	})

	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer svr.Close()

	params := TestPlanParams{}
	apiClient := NewClient(ClientConfig{
		ServerBaseURL: svr.URL,
	})

	got, err := apiClient.CreateTestPlan(context.Background(), "my-suite", params)

	wantTestPlan := plan.TestPlan{}

	if diff := cmp.Diff(got, wantTestPlan); diff != "" {
		t.Errorf("CreateTestPlan() diff (-got +want):\n%s", diff)
	}

	if !errors.Is(err, ErrRetryTimeout) {
		t.Errorf("CreateTestPlan() want %v, got %v", ErrRetryTimeout, err)
	}
}
