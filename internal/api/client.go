package api

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
)

// client is a client for the test splitter API.
// It contains the organization slug, server base URL, and an HTTP client.
type client struct {
	OrganizationSlug string
	ServerBaseUrl    string
	httpClient       *http.Client
}

// ClientConfig is the configuration for the test splitter API client.
type ClientConfig struct {
	AccessToken      string
	OrganizationSlug string
	ServerBaseUrl    string
	DebugEnabled     bool
}

// authTransport is a middleware for the HTTP client.
type authTransport struct {
	accessToken  string
	debugEnabled bool
}

// RoundTrip adds the Authorization header to all requests made by the HTTP client.
func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.debugEnabled {
		fmt.Println("--- 🕵️ DEBUG")
		fmt.Println("Request:")
		fmt.Printf("%s %s\n\n", req.Method, req.URL.String())
	}

	// IMPORTANT: We need to set the token after printing the request to avoid leaking the token in logs.
	req.Header.Set("Authorization", "Bearer "+t.accessToken)

	resp, err := http.DefaultTransport.RoundTrip(req)

	if t.debugEnabled {
		body, _ := io.ReadAll(resp.Body)
		defer resp.Body.Close()

		// We need to replace the body with a new reader, so it can be read again by the caller.
		resp.Body = io.NopCloser(bytes.NewBuffer(body))

		fmt.Println("Response:")
		fmt.Println(resp.Status)
		fmt.Println(string(body))
	}

	return resp, err
}

// NewClient creates a new client for the test splitter API with the given configuration.
// It also creates an HTTP client with an authTransport middleware.
func NewClient(cfg ClientConfig) *client {
	httpClient := &http.Client{
		Transport: &authTransport{
			accessToken:  cfg.AccessToken,
			debugEnabled: cfg.DebugEnabled,
		},
	}

	return &client{
		OrganizationSlug: cfg.OrganizationSlug,
		ServerBaseUrl:    cfg.ServerBaseUrl,
		httpClient:       httpClient,
	}
}
