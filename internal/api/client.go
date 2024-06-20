package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strconv"
	"time"

	"github.com/buildkite/roko"
	"github.com/buildkite/test-splitter/internal/debug"
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

var (
	retryTimeout = 130 * time.Second
	initialDelay = 3000 * time.Millisecond
)

var ErrRetryTimeout = errors.New("request retry timeout")

type errorResponse struct {
	Message string `json:"message"`
}

type httpRequest struct {
	Method string
	URL    string
	Body   any
}

// DoWithRetry sends http request with retries.
// Successful API response (status code 200) is JSON decoded and stored in the value pointed to by v.
// The request will be retried when the server returns 429 or 5xx status code, or when there is a network error.
// After reaching the retry timeout, the function will return ErrRetryTimeout.
// The request will not be retried when the server returns 4xx status code,
// and the error message will be returned as an error.
func (c *Client) DoWithRetry(ctx context.Context, req httpRequest, v interface{}) (*http.Response, error) {
	r := roko.NewRetrier(
		roko.TryForever(),
		roko.WithStrategy(roko.ExponentialSubsecond(initialDelay)),
		roko.WithJitter(),
	)

	retryContext, cancelRetryContext := context.WithTimeout(ctx, retryTimeout)
	defer cancelRetryContext()

	// retry loop
	debug.Printf("Sending request %s %s", req.Method, req.URL)
	resp, err := roko.DoFunc(retryContext, r, func(r *roko.Retrier) (*http.Response, error) {
		if r.AttemptCount() > 0 {
			debug.Printf("Retrying requests, attempt %d", r.AttemptCount())
		}

		reqBody, err := json.Marshal(req.Body)
		if err != nil {
			r.Break()
			return nil, fmt.Errorf("converting body to json: %w", err)
		}

		// Each request times out after 15 seconds, chosen to provide some
		// headroom on top of the goal p99 time to fetch of 10s.
		reqContext, cancelReqContext := context.WithTimeout(ctx, 15*time.Second)
		defer cancelReqContext()

		req, err := http.NewRequestWithContext(reqContext, req.Method, req.URL, bytes.NewBuffer(reqBody))
		if err != nil {
			r.Break()
			return nil, fmt.Errorf("creating request: %w", err)
		}
		req.Header.Add("Content-Type", "application/json")

		resp, err := c.httpClient.Do(req)

		// If we get an error before getting a response,
		// which means there is a network error (e.g. protocol error, timeout),
		// we should return and retry.
		if err != nil {
			debug.Printf("Error sending request: %v", err)
			return nil, err
		}

		debug.Printf("Response code %d", resp.StatusCode)

		// If we get a 429, we should return and retry after the rate limit resets.
		if resp.StatusCode == http.StatusTooManyRequests {
			if rateLimitReset, err := strconv.Atoi(resp.Header.Get("RateLimit-Reset")); err == nil {
				r.SetNextInterval(time.Duration(rateLimitReset) * time.Second)
			}
			return resp, fmt.Errorf("response code: 429")
		}

		// If we get a 5xx, we should return and retry
		if resp.StatusCode >= 500 {
			return resp, fmt.Errorf("response code: %d", resp.StatusCode)
		}

		// Other than above cases, we should break from the retry loop.
		r.Break()

		responseBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("reading response body: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			var errorResp errorResponse
			err = json.Unmarshal(responseBody, &errorResp)
			if err != nil {
				return resp, fmt.Errorf("parsing response: %w", err)
			}
			return resp, fmt.Errorf(errorResp.Message)
		}

		// parse response
		err = json.Unmarshal(responseBody, v)
		if err != nil {
			return nil, fmt.Errorf("parsing response: %w", err)
		}

		return resp, nil
	})

	if errors.Is(err, context.DeadlineExceeded) {
		return resp, ErrRetryTimeout
	}

	return resp, err
}
