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

	"github.com/google/go-cmp/cmp"
)

func TestFetchFilesTiming(t *testing.T) {
	files := []string{"apple_spec.rb", "banana_spec.rb", "cherry_spec.rb", "dragonfruit_spec.rb"}

	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("request method = %q, want %q", r.Method, http.MethodPost)
		}
		if r.URL.Path != "/v2/analytics/organizations/buildkite/suites/rspec/test_files" {
			t.Errorf("request path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer asdf1234" {
			t.Errorf("Authorization header = %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type header = %q", got)
		}

		var got fetchFilesTimingParams
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decoding request body: %v", err)
		}
		want := fetchFilesTimingParams{Paths: files}
		if diff := cmp.Diff(got, want); diff != "" {
			t.Errorf("request body diff (-got +want):\n%s", diff)
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = io.WriteString(w, `{
			"./apple_spec.rb": 1121,
			"./banana_spec.rb": 3121,
			"./cherry_spec.rb": 2143
		}`)
	}))
	defer svr.Close()

	c := NewClient(ClientConfig{
		AccessToken:      "asdf1234",
		OrganizationSlug: "buildkite",
		ServerBaseUrl:    svr.URL,
	})
	got, err := c.FetchFilesTiming(context.Background(), "rspec", files)
	if err != nil {
		t.Fatalf("FetchFilesTiming() error = %v", err)
	}
	want := map[string]time.Duration{
		"./apple_spec.rb":  1121 * time.Millisecond,
		"./banana_spec.rb": 3121 * time.Millisecond,
		"./cherry_spec.rb": 2143 * time.Millisecond,
	}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("FetchFilesTiming() diff (-got +want):\n%s", diff)
	}
}

func TestFetchFilesTiming_BadRequest(t *testing.T) {
	requestCount := 0
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		http.Error(w, `{"message": "bad request"}`, http.StatusBadRequest)
	}))
	defer svr.Close()

	c := NewClient(ClientConfig{
		OrganizationSlug: "my-org",
		ServerBaseUrl:    svr.URL,
	})

	files := []string{"apple_spec.rb", "banana_spec.rb"}
	_, err := c.FetchFilesTiming(context.Background(), "my-suite", files)

	if requestCount > 1 {
		t.Errorf("http request count = %v, want  %d", requestCount, 1)
	}

	if err.Error() != "bad request" {
		t.Errorf("FetchFilesTiming() error = %v, want %v", err, ErrRetryTimeout)
	}
}

func TestFetchFilesTiming_InternalServerError(t *testing.T) {
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
		OrganizationSlug: "my-org",
		ServerBaseUrl:    svr.URL,
	})

	files := []string{"apple_spec.rb", "banana_spec.rb"}
	_, err := c.FetchFilesTiming(context.Background(), "my-suite", files)

	if !errors.Is(err, ErrRetryTimeout) {
		t.Errorf("FetchFilesTiming() error = %v, want %v", err, ErrRetryTimeout)
	}
}
