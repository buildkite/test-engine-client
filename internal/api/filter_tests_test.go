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

	"github.com/buildkite/test-engine-client/internal/config"
	"github.com/buildkite/test-engine-client/internal/plan"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
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

		var got FilterTestsParams
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decoding request body: %v", err)
		}
		if diff := cmp.Diff(got, params, cmpopts.IgnoreUnexported(config.Config{})); diff != "" {
			t.Errorf("request body diff (-got +want):\n%s", diff)
		}

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
