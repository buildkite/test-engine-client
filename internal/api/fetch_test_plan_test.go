package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/buildkite/test-engine-client/internal/plan"
	"github.com/google/go-cmp/cmp"
)

func TestFetchTestPlan(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("request method = %q, want %q", r.Method, http.MethodGet)
		}
		if r.URL.Path != "/v2/analytics/organizations/buildkite/suites/rspec/test_plan" {
			t.Errorf("request path = %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("identifier"); got != "abc123" {
			t.Errorf("identifier query = %q", got)
		}
		if got := r.URL.Query().Get("job_retry_count"); got != "0" {
			t.Errorf("job_retry_count query = %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer asdf1234" {
			t.Errorf("Authorization header = %q", got)
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = io.WriteString(w, `{
			"tasks": {
				"1": {
					"node_number": 1,
					"tests": [{
						"path": "sky_spec.rb:2",
						"format": "example",
						"estimated_duration": 1000,
						"identifier": "sky_spec.rb[1,1]",
						"name": "is blue",
						"scope": "sky"
					}]
				}
			}
		}`)
	}))
	defer svr.Close()

	c := NewClient(ClientConfig{
		AccessToken:      "asdf1234",
		OrganizationSlug: "buildkite",
		ServerBaseUrl:    svr.URL,
	})

	got, err := c.FetchTestPlan(context.Background(), "rspec", "abc123", 0)
	if err != nil {
		t.Fatalf("FetchTestPlan() error = %v", err)
	}

	want := plan.TestPlan{
		Tasks: map[string]*plan.Task{
			"1": {
				NodeNumber: 1,
				Tests: []plan.TestCase{{
					Path:              "sky_spec.rb:2",
					Identifier:        "sky_spec.rb[1,1]",
					Name:              "is blue",
					Scope:             "sky",
					Format:            "example",
					EstimatedDuration: 1000,
				}},
			},
		},
	}

	if diff := cmp.Diff(got, &want); diff != "" {
		t.Errorf("FetchTestPlan() diff (-got +want):\n%s", diff)
	}
}

func TestFetchTestPlan_NotFound(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"message": "Not found"}`)
	}))
	defer svr.Close()

	c := NewClient(ClientConfig{
		AccessToken:      "asdf1234",
		OrganizationSlug: "buildkite",
		ServerBaseUrl:    svr.URL,
	})

	got, err := c.FetchTestPlan(context.Background(), "rspec", "abc123", 0)
	if err != nil {
		t.Fatalf("FetchTestPlan() error = %v", err)
	}

	if got != nil {
		t.Errorf("FetchTestPlan() = %v, want nil", got)
	}
}

func TestFetchTestPlan_BadRequest(t *testing.T) {
	requestCount := 0
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		http.Error(w, `{"message": "bad request"}`, http.StatusBadRequest)
	}))
	defer svr.Close()

	cfg := ClientConfig{
		AccessToken:      "asdf1234",
		OrganizationSlug: "my-org",
		ServerBaseUrl:    svr.URL,
	}

	c := NewClient(cfg)
	got, err := c.FetchTestPlan(context.Background(), "my-suite", "xyz", 0)

	if requestCount > 1 {
		t.Errorf("http request count = %v, want %d", requestCount, 1)
	}

	if err.Error() != "bad request" {
		t.Errorf("FetchTestPlan() error = %v, want %v", err, "bad request")
	}

	if got != nil {
		t.Errorf("FetchTestPlan() = %v, want nil", got)
	}
}

func TestFetchTestPlan_InternalServerError(t *testing.T) {
	originalTimeout := retryTimeout
	retryTimeout = 1 * time.Millisecond
	t.Cleanup(func() {
		retryTimeout = originalTimeout
	})

	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer svr.Close()

	cfg := ClientConfig{
		AccessToken:      "asdf1234",
		OrganizationSlug: "my-org",
		ServerBaseUrl:    svr.URL,
	}

	c := NewClient(cfg)
	got, err := c.FetchTestPlan(context.Background(), "my-suite", "xyz", 0)

	if !errors.Is(err, ErrRetryTimeout) {
		t.Errorf("FetchTestPlan() error = %v, want %v", err, ErrRetryTimeout)
	}

	if got != nil {
		t.Errorf("FetchTestPlan() = %v, want nil", got)
	}
}
