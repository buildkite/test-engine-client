package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"runtime"
	"strconv"
	"time"

	"github.com/buildkite/roko"
	"github.com/buildkite/test-splitter/internal/debug"
)

var ErrRetryTimeout = errors.New("request retry timeout")

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

var retryTimeout = 130 * time.Second
var initialDelay = 3000 * time.Millisecond

// DoWithRetry sends http request with retries.
// It retries on request errors (e.g. redirects, protocol errors), 429, and 5xx status codes.
// It uses an exponential backoff strategy with jitter, except for 429 status codes where it waits until the rate limit resets.
// DeadlineExceeded error is returned if the request cannot be fulfilled within the retry timeout which is 130 seconds.
// 130 seconds is chosen to allow for 2 API rate limit windows.
func (c *Client) DoWithRetry(ctx context.Context, method string, url string, body any) (*http.Response, error) {
	r := roko.NewRetrier(
		roko.TryForever(),
		roko.WithStrategy(roko.ExponentialSubsecond(initialDelay)),
		roko.WithJitter(),
	)

	retryContext, cancel := context.WithTimeout(ctx, retryTimeout)
	defer cancel()

	debug.Printf("Sending request %s %s", method, url)
	resp, err := roko.DoFunc(retryContext, r, func(r *roko.Retrier) (*http.Response, error) {
		if r.AttemptCount() > 0 {
			debug.Printf("Retrying requests, attempt %d", r.AttemptCount())
		}

		reqBody, err := json.Marshal(body)
		if err != nil {
			r.Break()
			return nil, fmt.Errorf("converting body to json: %w", err)
		}

		req, err := http.NewRequest(method, url, bytes.NewBuffer(reqBody))
		if err != nil {
			r.Break()
			return nil, fmt.Errorf("creating request: %w", err)
		}
		req.Header.Add("Content-Type", "application/json")

		resp, err := c.httpClient.Do(req)

		// If we get an error before getting a response,
		// which means there is a redirect or protocol error,
		// we should retry.
		if err != nil {
			debug.Printf("Error sending request: %v", err)
			return nil, err
		}

		debug.Printf("Response code %d", resp.StatusCode)

		// If we get a 429, we should wait until the rate limit resets and retry.
		if resp.StatusCode == http.StatusTooManyRequests {
			if rateLimitReset, err := strconv.Atoi(resp.Header.Get("RateLimit-Reset")); err == nil {
				r.SetNextInterval(time.Duration(rateLimitReset) * time.Second)
			}
			return resp, fmt.Errorf("response code: 429")
		}

		// If we get a 5xx, we should retry
		if resp.StatusCode >= 500 {
			return resp, fmt.Errorf("response code: %d", resp.StatusCode)
		}

		// If we get a 4xx, we should not retry
		if resp.StatusCode >= 400 {
			r.Break()
		}

		return resp, nil
	})

	if errors.Is(err, context.DeadlineExceeded) {
		return resp, ErrRetryTimeout
	}

	return resp, err
}
