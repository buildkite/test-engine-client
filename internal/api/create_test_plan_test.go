package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/buildkite/test-engine-client/internal/plan"
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
			Files: []plan.TestCase{
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

		var got TestPlanParams
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decoding request body: %v", err)
		}
		if diff := cmp.Diff(got, params); diff != "" {
			t.Errorf("request body diff (-got +want):\n%s", diff)
		}

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
		ServerBaseUrl:    svr.URL,
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
			Files: []plan.TestCase{
				{Path: "sky_spec.rb"},
			},
			Examples: []plan.TestCase{
				{
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
		var got TestPlanParams
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decoding request body: %v", err)
		}
		if diff := cmp.Diff(got, params); diff != "" {
			t.Errorf("request body diff (-got +want):\n%s", diff)
		}

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
		ServerBaseUrl:    svr.URL,
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
		ServerBaseUrl: svr.URL,
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
			Files: []plan.TestCase{
				{Path: "sky_spec.rb"},
			},
		},
	}

	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var got TestPlanParams
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decoding request body: %v", err)
		}
		if diff := cmp.Diff(got, params); diff != "" {
			t.Errorf("request body diff (-got +want):\n%s", diff)
		}

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
		ServerBaseUrl:    svr.URL,
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
		ServerBaseUrl: svr.URL,
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
