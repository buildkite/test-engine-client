package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func TestNewClient(t *testing.T) {
	// Create a new client with the given configuration.
	cfg := ClientConfig{
		AccessToken:      "asdf1234",
		OrganizationSlug: "my-org",
		ServerBaseUrl:    "http://example.com",
	}
	c := NewClient(cfg)

	// Check if the client has the correct organization slug.
	if c.OrganizationSlug != cfg.OrganizationSlug {
		t.Errorf("NewClient() = %v, want %v", c.OrganizationSlug, cfg.OrganizationSlug)
	}

	// Check if the client has the correct server base URL.
	if c.ServerBaseUrl != cfg.ServerBaseUrl {
		t.Errorf("NewClient() = %v, want %v", c.ServerBaseUrl, cfg.ServerBaseUrl)
	}

	// Check if the client has an HTTP client.
	if c.httpClient == nil {
		t.Errorf("NewClient() = nil, want not nil")
	}
}

func TestHttpClient_AttachAccessTokenToRequest(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer svr.Close()

	// Create a new client with the given configuration.
	cfg := ClientConfig{
		AccessToken:      "asdf1234",
		OrganizationSlug: "my-org",
		ServerBaseUrl:    svr.URL,
	}

	c := NewClient(cfg)
	resp, _ := c.httpClient.Get(svr.URL)

	if resp.Request.Header.Get("Authorization") != "Bearer asdf1234" {
		t.Errorf("Request Authorization header = %v, want %v", resp.Request.Header.Get("Authorization"), "Bearer asdf1234")
	}
}

func TestHttpClient_AttachUserAgentToRequest(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer svr.Close()

	// Create a new client with the given configuration.
	cfg := ClientConfig{
		AccessToken:      "asdf1234",
		OrganizationSlug: "my-org",
		ServerBaseUrl:    svr.URL,
		Version:          "0.5.1",
	}

	c := NewClient(cfg)
	resp, _ := c.httpClient.Get(svr.URL)

	if !strings.Contains(resp.Request.Header.Get("User-Agent"), "Buildkite Test Engine Client/0.5.1") {
		t.Errorf("User-agent header = %v, want %v", resp.Request.Header.Get("User-Agent"), "Buildkite Test Engine Client/0.5.1 ...")
	}
}

func TestDoWithRetry_Succesful_POST(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		io.Copy(w, r.Body)
	}))
	defer svr.Close()

	cfg := ClientConfig{
		ServerBaseUrl: svr.URL,
	}
	c := NewClient(cfg)

	var got map[string]string

	resp, err := c.DoWithRetry(context.Background(), httpRequest{
		Method: http.MethodPost,
		URL:    svr.URL,
		Body:   map[string]string{"message": "hello"},
	}, &got)

	if err != nil {
		t.Errorf("DoWithRetry() error = %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("DoWithRetry() status code = %v, want %v", resp.StatusCode, http.StatusOK)
	}

	want := map[string]string{"message": "hello"}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("DoWithRetry() diff (-got +want):\n%s", diff)
	}
}

func TestDoWithRetry_Succesful_GET(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)

		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}

		if len(bodyBytes) > 0 {
			t.Errorf("request body = %q, want empty", string(bodyBytes))
		}

	}))
	defer svr.Close()

	cfg := ClientConfig{
		ServerBaseUrl: svr.URL,
	}
	c := NewClient(cfg)

	var got map[string]string

	resp, err := c.DoWithRetry(context.Background(), httpRequest{
		Method: http.MethodGet,
		URL:    svr.URL,
	}, &got)

	if err != nil {
		t.Errorf("DoWithRetry() error = %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("DoWithRetry() status code = %v, want %v", resp.StatusCode, http.StatusOK)
	}
}

func TestDoWithRetry_RequestError(t *testing.T) {
	originalTimeout := retryTimeout
	retryTimeout = 300 * time.Millisecond
	t.Cleanup(func() {
		retryTimeout = originalTimeout
	})

	cfg := ClientConfig{
		AccessToken:      "asdf1234",
		OrganizationSlug: "my-org",
	}

	c := NewClient(cfg)
	resp, err := c.DoWithRetry(context.Background(), httpRequest{
		Method: http.MethodGet,
		URL:    "http://build.kite",
	}, nil)

	fmt.Println(resp)

	// it retries the request and returns ErrRetryTimeout with nil response.
	if !errors.Is(err, ErrRetryTimeout) {
		t.Errorf("DoWithRetry() error = %v, want %v", err, ErrRetryTimeout)
	}

	if resp != nil {
		t.Errorf("DoWithRetry() = %v, want nil", resp)
	}
}

func TestDoWithRetry_429(t *testing.T) {
	originalTimeout := retryTimeout
	retryTimeout = 1500 * time.Millisecond
	t.Cleanup(func() {
		retryTimeout = originalTimeout
	})

	requestCount := 0

	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.Header().Set("RateLimit-Reset", "1")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer svr.Close()

	cfg := ClientConfig{
		AccessToken:      "asdf1234",
		OrganizationSlug: "my-org",
		ServerBaseUrl:    svr.URL,
	}

	c := NewClient(cfg)
	resp, err := c.DoWithRetry(context.Background(), httpRequest{
		Method: http.MethodGet,
		URL:    svr.URL,
	}, nil)

	// it retries the request and returns ErrRetryTimeout with the 429 status code.
	if requestCount != 2 {
		t.Errorf("http request count = %v, want %v", requestCount, 2)
	}

	if !errors.Is(err, ErrRetryTimeout) {
		t.Errorf("DoWithRetry() error = %v, want %v", err, ErrRetryTimeout)
	}

	if resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("DoWithRetry() status code = %v, want %v", resp.StatusCode, http.StatusTooManyRequests)
	}
}

func TestDoWithRetry_500(t *testing.T) {
	originalTimeout := retryTimeout
	originalInitialDelay := initialDelay

	retryTimeout = 1000 * time.Millisecond
	initialDelay = 1 * time.Millisecond
	t.Cleanup(func() {
		retryTimeout = originalTimeout
		initialDelay = originalInitialDelay
	})

	requestCount := 0

	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++

		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer svr.Close()

	cfg := ClientConfig{
		AccessToken:      "asdf1234",
		OrganizationSlug: "my-org",
		ServerBaseUrl:    svr.URL,
	}

	c := NewClient(cfg)

	resp, err := c.DoWithRetry(context.Background(), httpRequest{
		Method: http.MethodGet,
		URL:    svr.URL,
	}, nil)

	// it retries the request and returns ErrRetryTimeout with the 500 status code.
	if requestCount < 2 {
		t.Errorf("http request count = %v, want at least %d", requestCount, 2)
	}

	if !errors.Is(err, ErrRetryTimeout) {
		t.Errorf("DoWithRetry() error = %v, want %v", err, ErrRetryTimeout)
	}

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("DoWithRetry() status code = %v, want %v", resp.StatusCode, http.StatusInternalServerError)
	}
}

func TestDoWithRetry_403(t *testing.T) {
	requestCount := 0

	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		http.Error(w, `{"message": "forbidden"}`, http.StatusForbidden)
	}))
	defer svr.Close()

	cfg := ClientConfig{
		AccessToken:      "asdf1234",
		OrganizationSlug: "my-org",
		ServerBaseUrl:    svr.URL,
	}

	c := NewClient(cfg)
	resp, err := c.DoWithRetry(context.Background(), httpRequest{
		Method: http.MethodGet,
		URL:    svr.URL,
	}, nil)

	// it returns immediately with the 403 status code.
	if requestCount > 1 {
		t.Errorf("http request count = %v, want %d", requestCount, 1)
	}

	if err.Error() != "forbidden" {
		t.Errorf("DoWithRetry() error = %v, want %v", err, "forbidden")
	}

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("DoWithRetry() status code = %v, want %v", resp.StatusCode, http.StatusForbidden)
	}
}
