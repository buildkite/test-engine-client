package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFetchSuite(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("request method = %q, want %q", r.Method, http.MethodGet)
		}
		if r.URL.Path != "/v2/analytics/organizations/buildkite/suites/rspec" {
			t.Errorf("request path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer asdf1234" {
			t.Errorf("Authorization header = %q", got)
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = io.WriteString(w, `{"id": "9e0c4d39-7b9f-4f8a-9a3b-2f1d6f9a7e21", "slug": "rspec"}`)
	}))
	defer svr.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	apiClient := NewClient(ClientConfig{
		AccessToken:      "asdf1234",
		OrganizationSlug: "buildkite",
		ServerBaseURL:    svr.URL,
	})

	suite, err := apiClient.FetchSuite(ctx, "rspec")
	if err != nil {
		t.Fatalf("FetchSuite() error = %v", err)
	}

	if want := "9e0c4d39-7b9f-4f8a-9a3b-2f1d6f9a7e21"; suite.ID != want {
		t.Errorf("FetchSuite() suite.ID = %q, want %q", suite.ID, want)
	}
}

func TestFetchSuite_NotFound(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message": "Suite not found"}`, http.StatusNotFound)
	}))
	defer svr.Close()

	apiClient := NewClient(ClientConfig{
		AccessToken:      "asdf1234",
		OrganizationSlug: "buildkite",
		ServerBaseURL:    svr.URL,
	})

	_, err := apiClient.FetchSuite(context.Background(), "rspec")

	if notFoundError := new(NotFoundError); !errors.As(err, &notFoundError) {
		t.Errorf("FetchSuite() error type = %T, want %T", err, NotFoundError{})
	}
}
