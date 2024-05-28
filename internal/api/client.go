package api

import (
	"fmt"
	"net/http"
	"runtime"
)

// client is a client for the test splitter API.
// It contains the organization slug, server base URL, and an HTTP client.
type Client struct {
	OrganizationSlug string
	ServerBaseUrl    string
	httpClient       *http.Client
}

// ClientConfig is the configuration for the test splitter API client.
type ClientConfig struct {
	AccessToken      string
	OrganizationSlug string
	ServerBaseUrl    string
	Version          string
}

// authTransport is a middleware for the HTTP client.
type authTransport struct {
	accessToken string
	version     string
}

// RoundTrip adds the Authorization header to all requests made by the HTTP client.
func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", "Bearer "+t.accessToken)
	req.Header.Set("User-Agent", fmt.Sprintf("Buildkite Test Splitter/%s (%s/%s)", t.version, runtime.GOOS, runtime.GOARCH))
	return http.DefaultTransport.RoundTrip(req)
}

// NewClient creates a new client for the test splitter API with the given configuration.
// It also creates an HTTP client with an authTransport middleware.
func NewClient(cfg ClientConfig) *Client {
	httpClient := &http.Client{
		Transport: &authTransport{
			accessToken: cfg.AccessToken,
			version:     cfg.Version,
		},
	}

	return &Client{
		OrganizationSlug: cfg.OrganizationSlug,
		ServerBaseUrl:    cfg.ServerBaseUrl,
		httpClient:       httpClient,
	}
}
