package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/buildkite/test-engine-client/internal/config"
	"github.com/buildkite/test-engine-client/internal/runner"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
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

		var got TestPlanMetadataParams
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decoding request body: %v", err)
		}
		if diff := cmp.Diff(got, params, cmpopts.IgnoreUnexported(config.Config{})); diff != "" {
			t.Errorf("request body diff (-got +want):\n%s", diff)
		}

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
