package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/buildkite/test-engine-client/internal/config"
	"github.com/buildkite/test-engine-client/internal/runner"
)

func TestPostTestPlanMetadata(t *testing.T) {
	cfg := config.New()
	cfg.Parallelism = 3
	cfg.NodeIndex = 1
	cfg.SuiteSlug = "my_slug"
	cfg.Identifier = "abc123"

	params := TestPlanMetadataParams{
		Version: "0.7.0",
		Env:     &cfg,
		Timeline: []Timeline{
			{Event: "test_start", Timestamp: "2024-06-20T04:46:13.60977Z"},
			{Event: "test_end", Timestamp: "2024-06-20T04:49:09.609793Z"},
		},
		Statistics: runner.RunStatistics{Total: 3},
	}

	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("request method = %q, want %q", r.Method, http.MethodPost)
		}
		if r.URL.Path != "/v2/analytics/organizations/buildkite/suites/rspec/test_plan_metadata" {
			t.Errorf("request path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer asdf1234" {
			t.Errorf("Authorization header = %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type header = %q", got)
		}

		assertJSONBody(t, r.Body, `{
			"version": "0.7.0",
			"env": {
				"BUILDKITE_BRANCH": "",
				"BUILDKITE_BUILD_ID": "",
				"BUILDKITE_TEST_ENGINE_DEBUG_ENABLED": false,
				"BUILDKITE_TEST_ENGINE_FAIL_ON_NO_TESTS": false,
				"BUILDKITE_TEST_ENGINE_IDENTIFIER": "abc123",
				"BUILDKITE_JOB_ID": "",
				"BUILDKITE_RETRY_COUNT": 0,
				"BUILDKITE_TEST_ENGINE_LOCATION_PREFIX": "",
				"BUILDKITE_TEST_ENGINE_MAX_PARALLELISM": 0,
				"BUILDKITE_TEST_ENGINE_RETRY_COUNT": 0,
				"BUILDKITE_PARALLEL_JOB": 1,
				"BUILDKITE_ORGANIZATION_SLUG": "",
				"BUILDKITE_PARALLEL_JOB_COUNT": 3,
				"BUILDKITE_TEST_ENGINE_RETRY_CMD": "",
				"BUILDKITE_TEST_ENGINE_SELECTION_STRATEGY": "",
				"BUILDKITE_TEST_ENGINE_SPLIT_BY_EXAMPLE": false,
				"BUILDKITE_STEP_ID": "",
				"BUILDKITE_TEST_ENGINE_SUITE_SLUG": "my_slug",
				"BUILDKITE_TEST_ENGINE_TAG_FILTERS": "",
				"BUILDKITE_TEST_ENGINE_TARGET_TIME": 0,
				"BUILDKITE_TEST_ENGINE_TEST_CMD": "",
				"BUILDKITE_TEST_ENGINE_TEST_FILE_EXCLUDE_PATTERN": "",
				"BUILDKITE_TEST_ENGINE_TEST_FILE_PATTERN": "",
				"BUILDKITE_TEST_ENGINE_TEST_RUNNER": ""
			},
			"timeline": [
				{"event": "test_start", "timestamp": "2024-06-20T04:46:13.60977Z"},
				{"event": "test_end", "timestamp": "2024-06-20T04:49:09.609793Z"}
			],
			"statistics": {
				"total": 3,
				"passed_on_first_run": 0,
				"passed_on_retry": 0,
				"muted_passed": 0,
				"muted_failed": 0,
				"failed": 0,
				"skipped": 0
			}
		}`)

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = io.WriteString(w, `{"head": "no_content"}`)
	}))
	defer svr.Close()

	c := NewClient(ClientConfig{
		AccessToken:      "asdf1234",
		OrganizationSlug: "buildkite",
		ServerBaseUrl:    svr.URL,
	})

	_, err := c.DoWithRetry(context.Background(), httpRequest{
		Method: http.MethodPost,
		URL:    fmt.Sprintf("%s/v2/analytics/organizations/%s/suites/%s/test_plan_metadata", c.ServerBaseUrl, c.OrganizationSlug, "rspec"),
		Body:   params,
	}, nil)
	if err != nil {
		t.Errorf("PostTestPlanMetadata() error = %v", err)
	}
}

func TestPostTestPlanMetadata_NotFound(t *testing.T) {
	cfg := config.New()
	cfg.Parallelism = 3
	cfg.NodeIndex = 1
	cfg.SuiteSlug = "my_slug"
	cfg.Identifier = "abc123"

	params := TestPlanMetadataParams{
		Version: "0.7.0",
		Env:     &cfg,
		Timeline: []Timeline{
			{Event: "test_start", Timestamp: "2024-06-20T04:46:13.60977Z"},
			{Event: "test_end", Timestamp: "2024-06-20T04:49:09.609793Z"},
		},
		Statistics: runner.RunStatistics{Total: 3},
	}

	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"message": "Test plan not found"}`)
	}))
	defer svr.Close()

	c := NewClient(ClientConfig{
		AccessToken:      "asdf1234",
		OrganizationSlug: "buildkite",
		ServerBaseUrl:    svr.URL,
	})

	_, err := c.DoWithRetry(context.Background(), httpRequest{
		Method: http.MethodPost,
		URL:    fmt.Sprintf("%s/v2/analytics/organizations/%s/suites/%s/test_plan_metadata", c.ServerBaseUrl, c.OrganizationSlug, "rspec"),
		Body:   params,
	}, nil)

	if err == nil {
		t.Errorf("PostTestPlanMetadata() error = %v, want %v", err, "Test plan not found")
	}
}
