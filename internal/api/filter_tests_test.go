package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/buildkite/test-engine-client/internal/config"
	"github.com/buildkite/test-engine-client/internal/plan"
	"github.com/google/go-cmp/cmp"
)

func TestFilterTests_SlowFiles(t *testing.T) {
	cfg := config.New()
	cfg.Parallelism = 3
	cfg.SplitByExample = true

	params := FilterTestsParams{
		Files: []plan.TestCase{
			{Path: "./cat_spec.rb"},
			{Path: "./dog_spec.rb"},
			{Path: "./turtle_spec.rb"},
		},
		Env: &cfg,
	}

	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("request method = %q, want %q", r.Method, http.MethodPost)
		}
		if r.URL.Path != "/v2/analytics/organizations/buildkite/suites/rspec/test_plan/filter_tests" {
			t.Errorf("request path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer asdf1234" {
			t.Errorf("Authorization header = %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type header = %q", got)
		}

		assertJSONBody(t, r.Body, `{
			"files": [
				{"path": "./cat_spec.rb"},
				{"path": "./dog_spec.rb"},
				{"path": "./turtle_spec.rb"}
			],
			"env": {
				"BUILDKITE_BRANCH": "",
				"BUILDKITE_BUILD_ID": "",
				"BUILDKITE_TEST_ENGINE_DEBUG_ENABLED": false,
				"BUILDKITE_TEST_ENGINE_FAIL_ON_NO_TESTS": false,
				"BUILDKITE_TEST_ENGINE_IDENTIFIER": "",
				"BUILDKITE_JOB_ID": "",
				"BUILDKITE_RETRY_COUNT": 0,
				"BUILDKITE_TEST_ENGINE_LOCATION_PREFIX": "",
				"BUILDKITE_TEST_ENGINE_MAX_PARALLELISM": 0,
				"BUILDKITE_TEST_ENGINE_RETRY_COUNT": 0,
				"BUILDKITE_PARALLEL_JOB": 0,
				"BUILDKITE_ORGANIZATION_SLUG": "",
				"BUILDKITE_PARALLEL_JOB_COUNT": 3,
				"BUILDKITE_TEST_ENGINE_RETRY_CMD": "",
				"BUILDKITE_TEST_ENGINE_SELECTION_STRATEGY": "",
				"BUILDKITE_TEST_ENGINE_SPLIT_BY_EXAMPLE": true,
				"BUILDKITE_STEP_ID": "",
				"BUILDKITE_TEST_ENGINE_SUITE_SLUG": "",
				"BUILDKITE_TEST_ENGINE_TAG_FILTERS": "",
				"BUILDKITE_TEST_ENGINE_TARGET_TIME": 0,
				"BUILDKITE_TEST_ENGINE_TEST_CMD": "",
				"BUILDKITE_TEST_ENGINE_TEST_FILE_EXCLUDE_PATTERN": "",
				"BUILDKITE_TEST_ENGINE_TEST_FILE_PATTERN": "",
				"BUILDKITE_TEST_ENGINE_TEST_RUNNER": ""
			}
		}`)

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = io.WriteString(w, `{"tests": [{"path": "./turtle_spec.rb"}]}`)
	}))
	defer svr.Close()

	c := NewClient(ClientConfig{
		AccessToken:      "asdf1234",
		OrganizationSlug: "buildkite",
		ServerBaseUrl:    svr.URL,
	})
	got, err := c.FilterTests(context.Background(), "rspec", params)
	if err != nil {
		t.Fatalf("FilterTests() error = %v", err)
	}
	want := []FilteredTest{
		{Path: "./turtle_spec.rb"},
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("FilterTests() diff (-got +want):\n%s", diff)
	}
}

func TestFilterTests_InternalServerError(t *testing.T) {
	originalTimeout := retryTimeout
	retryTimeout = 1 * time.Millisecond
	t.Cleanup(func() {
		retryTimeout = originalTimeout
	})

	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message": "something went wrong"}`, http.StatusInternalServerError)
	}))
	defer svr.Close()

	c := NewClient(ClientConfig{
		OrganizationSlug: "msy-org",
		ServerBaseUrl:    svr.URL,
	})

	_, err := c.FilterTests(context.Background(), "my-suite", FilterTestsParams{
		Files: []plan.TestCase{},
	})

	if !errors.Is(err, ErrRetryTimeout) {
		t.Errorf("FilterTests() error = %v, want %v", err, ErrRetryTimeout)
	}
}
