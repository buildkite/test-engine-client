package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
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
