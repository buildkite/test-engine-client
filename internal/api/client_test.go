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

	if !strings.Contains(resp.Request.Header.Get("User-Agent"), "Buildkite Test Splitter/0.5.1") {
		t.Errorf("User-agent header = %v, want %v", resp.Request.Header.Get("User-Agent"), "Buildkite Test Splitter/0.5.1 ...")
	}
}

func TestDoWithRetry_Success(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		io.Copy(w, r.Body)
	}))
	defer svr.Close()

	cfg := ClientConfig{
		ServerBaseUrl: svr.URL,
	}
	c := NewClient(cfg)

	resp, err := c.DoWithRetry(context.Background(), http.MethodPost, svr.URL, map[string]string{"message": "hello"})

	if err != nil {
		t.Errorf("DoWithRetry(ctx, req) error = %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("DoWithRetry(ctx, req) status code = %v, want %v", resp.StatusCode, http.StatusOK)
	}

	responseBody, _ := io.ReadAll(resp.Body)
	want := `{"message":"hello"}`
	if string(responseBody) != want {
		t.Errorf("DoWithRetry(ctx, req) body = %v, want %v", string(responseBody), want)
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
	resp, err := c.DoWithRetry(context.Background(), http.MethodGet, "http://build.kite", nil)

	fmt.Println(resp)

	// it retries the request and returns ErrRetryTimeout with nil response.
	if !errors.Is(err, ErrRetryTimeout) {
		t.Errorf("DoWithRetry(ctx, req) error = %v, want %v", err, ErrRetryTimeout)
	}

	if resp != nil {
		t.Errorf("DoWithRetry(ctx, req) = %v, want nil", resp)
	}
}

func TestDoWithRetry_429(t *testing.T) {
	originalTimeout := retryTimeout
	retryTimeout = 2 * time.Second
	t.Cleanup(func() {
		retryTimeout = originalTimeout
	})

	callCount := 0

	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
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
	resp, err := c.DoWithRetry(context.Background(), http.MethodGet, svr.URL, nil)

	// it retries the request and returns ErrRetryTimeout with the 429 status code.
	if callCount != 2 {
		t.Errorf("http request count = %v, want %v", callCount, 2)
	}

	if !errors.Is(err, ErrRetryTimeout) {
		t.Errorf("DoWithRetry(ctx, req) error = %v, want %v", err, ErrRetryTimeout)
	}

	if resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("DoWithRetry(ctx, req) status code = %v, want %v", resp.StatusCode, http.StatusTooManyRequests)
	}
}

func TestDoWithRetry_500(t *testing.T) {
	originalTimeout := retryTimeout
	originalInitialDelay := initialDelay

	retryTimeout = 2 * time.Second
	initialDelay = 100 * time.Millisecond
	t.Cleanup(func() {
		retryTimeout = originalTimeout
		initialDelay = originalInitialDelay
	})

	callCount := 0

	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++

		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer svr.Close()

	cfg := ClientConfig{
		AccessToken:      "asdf1234",
		OrganizationSlug: "my-org",
		ServerBaseUrl:    svr.URL,
	}

	c := NewClient(cfg)

	resp, err := c.DoWithRetry(context.Background(), http.MethodPost, svr.URL, nil)

	// it retries the request and returns ErrRetryTimeout with the 500 status code.
	if callCount < 2 {
		t.Errorf("http request count = %v, want at least 2", callCount)
	}

	if !errors.Is(err, ErrRetryTimeout) {
		t.Errorf("DoWithRetry(ctx, req) error = %v, want %v", err, ErrRetryTimeout)
	}

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("DoWithRetry(ctx, req) status code = %v, want %v", resp.StatusCode, http.StatusInternalServerError)
	}
}

func TestDoWithRetry_403(t *testing.T) {
	originalTimeout := retryTimeout
	originalInitialDelay := initialDelay

	retryTimeout = 500 * time.Millisecond
	initialDelay = 100 * time.Millisecond

	t.Cleanup(func() {
		retryTimeout = originalTimeout
		initialDelay = originalInitialDelay
	})

	callCount := 0

	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusForbidden)
	}))
	defer svr.Close()

	cfg := ClientConfig{
		AccessToken:      "asdf1234",
		OrganizationSlug: "my-org",
		ServerBaseUrl:    svr.URL,
	}

	c := NewClient(cfg)
	resp, err := c.DoWithRetry(context.Background(), http.MethodGet, svr.URL, nil)

	// it returns immediately with the 403 status code.
	if callCount > 1 {
		t.Errorf("http request count = %v, want 1", callCount)
	}

	if err != nil {
		t.Errorf("DoWithRetry(ctx, req) error = %v", err)
	}

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("DoWithRetry(ctx, req) status code = %v, want %v", resp.StatusCode, http.StatusForbidden)
	}
}
