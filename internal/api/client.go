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
	"strings"
	"time"

	"github.com/buildkite/roko"
	"github.com/buildkite/test-engine-client/v2/internal/debug"
	"github.com/buildkite/test-engine-client/v2/internal/version"
)

// client is a client for the test plan API.
// It contains the organization slug, server base URL, and an HTTP client.
type Client struct {
	OrganizationSlug string
	ServerBaseURL    string
	UploadBaseURL    string
	httpClient       *http.Client
}

// ClientConfig is the configuration for the test plan API client.
type ClientConfig struct {
	AccessToken      string
	UploadBaseURL    string
	OrganizationSlug string
	ServerBaseURL    string
}

// authTransport is a middleware for the HTTP client.
type authTransport struct {
	accessToken string
}

// RoundTrip adds the Authorization and User-Agent headers to all requests made
// by the HTTP client. If Authorization is already set on the request it is left
// unchanged, allowing callers to supply a different auth scheme (e.g. Token).
func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Header.Get("Authorization") == "" {
		req.Header.Set("Authorization", "Bearer "+t.accessToken)
	}
	req.Header.Set("User-Agent", fmt.Sprintf(
		"Buildkite Test Engine Client/%s (%s/%s)",
		version.Version, runtime.GOOS, runtime.GOARCH,
	))
	return http.DefaultTransport.RoundTrip(req)
}

// NewClient creates a new client for the test plan API with the given configuration.
// It also creates an HTTP client with an authTransport middleware.
func NewClient(cfg ClientConfig) *Client {
	httpClient := &http.Client{
		Transport: &authTransport{
			accessToken: cfg.AccessToken,
		},
	}

	return &Client{
		OrganizationSlug: cfg.OrganizationSlug,
		ServerBaseURL:    cfg.ServerBaseURL,
		UploadBaseURL:    cfg.UploadBaseURL,
		httpClient:       httpClient,
	}
}

var (
	retryTimeout = 130 * time.Second
	initialDelay = 3000 * time.Millisecond
)

var ErrRetryTimeout = errors.New("request retry timeout")

type BillingError struct {
	Message string
}

func (e *BillingError) Error() string {
	return e.Message
}

type AuthError struct {
	Message string
}

func (e *AuthError) Error() string {
	return e.Message
}

type ForbiddenError struct {
	Message string
}

func (e *ForbiddenError) Error() string {
	return e.Message
}

type NotFoundError struct {
	Message string
}

func (e *NotFoundError) Error() string {
	return e.Message
}

type BadRequestError struct {
	Message string
}

func (e *BadRequestError) Error() string {
	return e.Message
}

type UnprocessableEntityError struct {
	Message string
}

func (e *UnprocessableEntityError) Error() string {
	return e.Message
}

type responseError struct {
	Message string `json:"message"`
}

func (e *responseError) Error() string {
	return e.Message
}

type httpRequest struct {
	Method string
	URL    string
	Body   any
}

// doWithRetry runs the request built by newRequest with retries. It holds the
// shared retry mechanics used by doJSONWithRetry and UploadTestResults.
//
// The request is retried when the server returns 429, 409, or 5xx, or when there
// is a network error. After reaching the retry timeout, the function returns
// ErrRetryTimeout. newRequest builds a fresh request for each attempt (so a body
// can be re-sent on retry).
//
// On a response that is not retried, doWithRetry returns it with a nil error;
// the caller is responsible for reading and closing the response body.
func (c *Client) doWithRetry(
	ctx context.Context,
	perAttemptTimeout time.Duration,
	newRequest func(ctx context.Context) (*http.Request, error),
) (*http.Response, error) {
	r := roko.NewRetrier(
		roko.TryForever(),
		roko.WithStrategy(roko.ExponentialSubsecond(initialDelay)),
		roko.WithJitter(),
	)

	retryContext, cancelRetryContext := context.WithTimeout(ctx, retryTimeout)
	defer cancelRetryContext()

	// retry loop
	resp, err := roko.DoFunc(retryContext, r, func(r *roko.Retrier) (*http.Response, error) {
		if r.AttemptCount() > 0 {
			debug.Printf("Retrying requests, attempt %d", r.AttemptCount())
		}

		reqContext, cancelReqContext := context.WithTimeout(ctx, perAttemptTimeout)
		defer cancelReqContext()

		req, err := newRequest(reqContext)
		if err != nil {
			r.Break()
			return nil, err
		}

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

		// If we get a 409, we aren't the first client to create the plan so return
		// and retry
		if resp.StatusCode == http.StatusConflict {
			return resp, fmt.Errorf("response code: %d", resp.StatusCode)
		}

		// If we get a 5xx, we should return and retry
		if resp.StatusCode >= 500 {
			return resp, fmt.Errorf("response code: %d", resp.StatusCode)
		}

		// Other than above cases, we stop retrying and let the caller handle the
		// response.
		r.Break()
		return resp, nil
	})

	if errors.Is(err, context.DeadlineExceeded) {
		return resp, ErrRetryTimeout
	}

	return resp, err
}

// doJSONWithRetry sends a JSON HTTP request via doWithRetry.
// The request body (if any) is JSON encoded with a Content-Type of
// application/json, and a successful API response (status code 200) is JSON
// decoded and stored in the value pointed to by v. A non-200 response is
// returned as a typed error.
// For non-JSON requests (e.g. multipart uploads), use doWithRetry directly.
// See doWithRetry for the retry behavior.
func (c *Client) doJSONWithRetry(ctx context.Context, reqOptions httpRequest, v interface{}) (*http.Response, error) {
	debug.Printf("Sending request %s %s", reqOptions.Method, reqOptions.URL)

	// Each request times out after 15 seconds, chosen to provide some
	// headroom on top of the goal p99 time to fetch of 10s.
	timeOut := 15 * time.Second
	newRequest := func(reqContext context.Context) (*http.Request, error) {
		req, err := http.NewRequestWithContext(reqContext, reqOptions.Method, reqOptions.URL, nil)
		if err != nil {
			return nil, fmt.Errorf("creating request: %w", err)
		}

		if reqOptions.Method != http.MethodGet && reqOptions.Body != nil {
			// add body to request
			reqBody, err := json.Marshal(reqOptions.Body)
			if err != nil {
				return nil, fmt.Errorf("converting body to json: %w", err)
			}
			req.Body = io.NopCloser(bytes.NewReader(reqBody))
		}

		req.Header.Add("Content-Type", "application/json")
		return req, nil
	}

	resp, err := c.doWithRetry(ctx, timeOut, newRequest)
	if err != nil {
		return resp, err
	}

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var respError responseError
		err = json.Unmarshal(responseBody, &respError)
		if err != nil {
			return resp, fmt.Errorf("parsing response: %w", err)
		}

		// 5xx and 429 are handled by doWithRetry and trigger retries; here we
		// only classify 4xx responses into typed errors.
		switch resp.StatusCode {
		case http.StatusUnauthorized:
			return resp, &AuthError{Message: respError.Message}
		case http.StatusForbidden:
			if strings.HasPrefix(respError.Message, "Billing Error") {
				return resp, &BillingError{Message: respError.Message}
			}
			return resp, &ForbiddenError{Message: respError.Message}
		case http.StatusNotFound:
			return resp, &NotFoundError{Message: respError.Message}
		case http.StatusBadRequest:
			return resp, &BadRequestError{Message: respError.Message}
		case http.StatusUnprocessableEntity:
			return resp, &UnprocessableEntityError{Message: respError.Message}
		default:
			return resp, &respError
		}
	}

	// parse response
	if v != nil && len(responseBody) > 0 {
		err = json.Unmarshal(responseBody, v)
		if err != nil {
			return nil, fmt.Errorf("parsing response: %w", err)
		}
	}

	return resp, nil
}
