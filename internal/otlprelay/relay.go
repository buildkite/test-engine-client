package otlprelay

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	DefaultUpstreamEndpoint = "https://tests-otlp.buildkite.com/v1/traces"
	ResourceAttribute       = "buildkite.otlp.endpoint"

	maxRequestBytes           = 900 * 1024
	defaultQueueCapacity      = 64 * 1024 * 1024
	queueRequestOverhead      = 1024 // Account for object and slice overhead, even with an empty body.
	defaultRequestTimeout     = 30 * time.Second
	defaultInitialBackoff     = 250 * time.Millisecond
	defaultMaxBackoff         = 5 * time.Second
	defaultTokenLifetime      = 2 * time.Hour
	defaultTokenRefreshLeeway = 5 * time.Minute
)

// ErrTerminalCredential marks a credential failure that will never succeed if
// retried, such as the Buildkite API refusing to mint a token for a disallowed
// audience. TokenSource implementations wrap errors with it (check with
// errors.Is). At startup a terminal failure fails relay creation; mid-run the
// relay stops calling TokenSource and drops requests as permanent failures.
// All other TokenSource errors are treated as transient and retried.
var ErrTerminalCredential = errors.New("terminal OTLP relay credential failure")

// Config contains the trusted values that bktec adds when forwarding an OTLP
// request. The test process supplies only the opaque protobuf body and its
// content encoding.
type Config struct {
	UpstreamEndpoint string
	RunKey           string
	QueueCapacity    int
	TokenLifetime    time.Duration
	CollectorToken   string
	// InitialToken optionally seeds the upstream credential with a token that
	// was already minted for the same audience (bktec's collector upload
	// token). When set, TokenSource is only called once the seed needs
	// refreshing, and relay startup never blocks on token minting.
	InitialToken string
	TokenSource  func(context.Context) (string, error)
	HTTPClient   *http.Client
	Logf         func(string, ...any)

	initialBackoff time.Duration
	maxBackoff     time.Duration
}

// Report summarizes requests delivered or dropped by a relay.
type Report struct {
	ForwardedRequests        int
	DroppedPermanentRequests int
	DroppedDeadlineRequests  int
	DroppedBytes             int64
}

type request struct {
	body            []byte
	contentEncoding string
}

type deliveryResult int

const (
	deliveryForwarded deliveryResult = iota
	deliveryPermanentFailure
	deliveryDeadline
)

// Relay accepts authenticated OTLP/HTTP protobuf requests on loopback and
// forwards their byte-exact bodies to Buildkite from one background worker.
type Relay struct {
	config Config

	endpoint string
	token    string

	server *http.Server
	client *http.Client

	ctx          context.Context
	cancel       context.CancelFunc
	wake         chan struct{}
	drainStarted chan struct{}
	done         chan struct{}

	mu              sync.Mutex
	queue           []*request
	queuedSize      int
	accepting       bool
	draining        bool
	drainDeadline   time.Time
	drainRetryDelay time.Duration
	report          Report

	// upstreamToken, tokenRefreshAt, and credentialErr are only touched by New
	// (before the worker goroutine starts) and by the single forward worker, so
	// they need no locking. credentialErr, once set, records a terminal
	// credential failure: TokenSource is never called again.
	upstreamToken       string
	tokenRefreshAt      time.Time
	credentialErr       error
	credentialErrLogged bool
}

// New starts a relay on a random 127.0.0.1 TCP port.
func New(config Config) (*Relay, error) {
	if config.UpstreamEndpoint == "" {
		config.UpstreamEndpoint = DefaultUpstreamEndpoint
	}
	upstreamURL, err := url.ParseRequestURI(config.UpstreamEndpoint)
	if err != nil || upstreamURL.Host == "" || (upstreamURL.Scheme != "http" && upstreamURL.Scheme != "https") {
		return nil, fmt.Errorf("invalid OTLP upstream endpoint %q", config.UpstreamEndpoint)
	}
	if !validRunKey(config.RunKey) {
		return nil, fmt.Errorf("invalid Buildkite test run key")
	}
	if config.TokenSource == nil {
		return nil, fmt.Errorf("OTLP relay token source is required")
	}
	if config.QueueCapacity == 0 {
		config.QueueCapacity = defaultQueueCapacity
	}
	if config.QueueCapacity < 1 {
		return nil, fmt.Errorf("OTLP relay queue capacity must be greater than zero")
	}
	if config.TokenLifetime <= 0 {
		config.TokenLifetime = defaultTokenLifetime
	}
	if config.initialBackoff <= 0 {
		config.initialBackoff = defaultInitialBackoff
	}
	if config.maxBackoff <= 0 {
		config.maxBackoff = defaultMaxBackoff
	}
	if config.Logf == nil {
		config.Logf = func(string, ...any) {}
	}

	localTokenBytes := make([]byte, 32)
	if _, err := rand.Read(localTokenBytes); err != nil {
		return nil, fmt.Errorf("generate OTLP relay token: %w", err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("start OTLP relay listener: %w", err)
	}

	client := config.HTTPClient
	if client == nil {
		client = &http.Client{
			Timeout: defaultRequestTimeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	relay := &Relay{
		config:       config,
		endpoint:     "http://" + listener.Addr().String(),
		token:        base64.RawURLEncoding.EncodeToString(localTokenBytes),
		client:       client,
		ctx:          ctx,
		cancel:       cancel,
		wake:         make(chan struct{}, 1),
		drainStarted: make(chan struct{}),
		done:         make(chan struct{}),
		accepting:    true,
	}
	// A missing upstream credential must not fail the job: unless the failure
	// is known-terminal, start anyway and let the forward worker keep retrying
	// while requests buffer in the queue (they are dropped and reported at the
	// drain deadline if the credential never arrives).
	if token := strings.TrimSpace(config.InitialToken); token != "" {
		relay.upstreamToken = token
		relay.tokenRefreshAt = tokenRefreshTime(token, time.Now(), config.TokenLifetime)
	} else if _, err := relay.authorizationToken(); err != nil {
		if errors.Is(err, ErrTerminalCredential) {
			cancel()
			_ = listener.Close()
			return nil, fmt.Errorf("get initial OTLP relay credential: %w", err)
		}
		relay.config.Logf("Buildkite Test Engine Client: OTLP relay starting without an upstream credential; requests will be buffered while it retries in the background: %v", err)
	}
	relay.server = &http.Server{
		Handler:           relay,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		MaxHeaderBytes:    16 * 1024,
	}

	go func() {
		if err := relay.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			relay.config.Logf("Buildkite Test Engine Client: OTLP relay listener stopped: %v", err)
		}
	}()
	go relay.forward()

	return relay, nil
}

// Endpoint is the credential-free loopback base URL advertised to OTel SDKs.
func (r *Relay) Endpoint() string {
	return r.endpoint
}

// Environment returns the child-process environment needed by standard OTel
// SDKs and by Buildkite's Ruby test collector. Existing resource attributes
// are retained.
func (r *Relay) Environment(resourceAttributes string) map[string]string {
	traceEndpoint := r.endpoint + "/v1/traces"
	headers := "authorization=" + url.PathEscape("Bearer "+r.token)
	relayAttribute := ResourceAttribute + "=" + traceEndpoint
	if resourceAttributes != "" {
		relayAttribute = resourceAttributes + "," + relayAttribute
	}

	return map[string]string{
		"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT": traceEndpoint,
		"OTEL_EXPORTER_OTLP_TRACES_PROTOCOL": "http/protobuf",
		"OTEL_EXPORTER_OTLP_TRACES_HEADERS":  headers,
		"OTEL_RESOURCE_ATTRIBUTES":           relayAttribute,
		"BUILDKITE_ANALYTICS_OTLP_ENDPOINT":  traceEndpoint,
		"BUILDKITE_TESTS_OTLP_TOKEN":         r.token,
	}
}

func (r *Relay) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if !r.authorized(req.Header.Get("Authorization")) {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if req.URL.Path != "/v1/traces" {
		http.NotFound(w, req)
		return
	}
	if req.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if req.Header.Get("Content-Type") != "application/x-protobuf" {
		w.WriteHeader(http.StatusUnsupportedMediaType)
		return
	}

	contentEncoding := strings.ToLower(strings.TrimSpace(req.Header.Get("Content-Encoding")))
	if contentEncoding != "" && contentEncoding != "gzip" {
		w.WriteHeader(http.StatusUnsupportedMediaType)
		return
	}
	if req.ContentLength > maxRequestBytes {
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		return
	}

	body, err := io.ReadAll(io.LimitReader(req.Body, maxRequestBytes+1))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if len(body) > maxRequestBytes {
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		return
	}

	if !r.enqueue(&request{body: body, contentEncoding: contentEncoding}) {
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
		return
	}

	w.Header().Set("Content-Type", "application/x-protobuf")
	w.WriteHeader(http.StatusOK)
}

func (r *Relay) authorized(got string) bool {
	bearer := "Bearer " + r.token
	collector := `Token token="` + r.token + `"`
	valid := subtle.ConstantTimeCompare([]byte(got), []byte(bearer)) |
		subtle.ConstantTimeCompare([]byte(got), []byte(collector))
	if r.config.CollectorToken != "" {
		existingCollector := `Token token="` + r.config.CollectorToken + `"`
		valid |= subtle.ConstantTimeCompare([]byte(got), []byte(existingCollector))
	}
	return valid == 1
}

func (r *Relay) enqueue(req *request) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	size := len(req.body) + queueRequestOverhead
	if !r.accepting || r.queuedSize+size > r.config.QueueCapacity {
		return false
	}
	r.queue = append(r.queue, req)
	r.queuedSize += size
	r.signal()
	return true
}

func (r *Relay) signal() {
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

func (r *Relay) forward() {
	defer close(r.done)

	for {
		req, ok := r.nextRequest()
		if !ok {
			if r.ctx.Err() != nil {
				r.dropRemainingAtDeadline()
			}
			return
		}

		result := r.deliver(req)
		switch result {
		case deliveryForwarded:
			r.complete(req, true)
		case deliveryPermanentFailure:
			r.complete(req, false)
		case deliveryDeadline:
			r.dropRemainingAtDeadline()
			return
		}
	}
}

func (r *Relay) nextRequest() (*request, bool) {
	for {
		r.mu.Lock()
		if len(r.queue) > 0 {
			req := r.queue[0]
			r.mu.Unlock()
			return req, true
		}
		if r.draining {
			r.mu.Unlock()
			return nil, false
		}
		r.mu.Unlock()

		select {
		case <-r.wake:
		case <-r.ctx.Done():
			return nil, false
		}
	}
}

func (r *Relay) deliver(queued *request) deliveryResult {
	backoff := r.config.initialBackoff
	for {
		token, err := r.authorizationToken()
		if err != nil && errors.Is(err, ErrTerminalCredential) {
			if !r.credentialErrLogged {
				r.credentialErrLogged = true
				r.config.Logf("Buildkite Test Engine Client: OTLP relay credential was refused and will not be retried; dropping OTLP requests: %v", err)
			}
			return deliveryPermanentFailure
		}
		var status int
		var retryAfter time.Duration
		var hasRetryAfter bool
		if err == nil {
			status, retryAfter, hasRetryAfter, err = r.send(queued, token)
		}

		if err == nil {
			switch {
			case status >= 200 && status < 300:
				return deliveryForwarded
			case status != http.StatusRequestTimeout && status != http.StatusTooManyRequests && status < 500:
				r.config.Logf("Buildkite Test Engine Client: OTLP relay dropped a request after upstream returned HTTP %d", status)
				return deliveryPermanentFailure
			}
		}
		if r.ctx.Err() != nil {
			return deliveryDeadline
		}

		delay := backoff
		if hasRetryAfter {
			delay = retryAfter
		}
		if !r.waitForRetry(delay) {
			return deliveryDeadline
		}
		backoff = min(backoff*2, r.config.maxBackoff)
	}
}

func (r *Relay) waitForRetry(delay time.Duration) bool {
	for {
		r.mu.Lock()
		draining := r.draining
		r.mu.Unlock()

		timer := time.NewTimer(r.clampRetryDelay(delay))
		drainStarted := r.drainStarted
		if draining {
			drainStarted = nil
		}
		select {
		case <-timer.C:
			return true
		case <-drainStarted:
			timer.Stop()
		case <-r.ctx.Done():
			timer.Stop()
			return false
		}
	}
}

func (r *Relay) send(queued *request, token string) (int, time.Duration, bool, error) {
	req, err := http.NewRequestWithContext(r.ctx, http.MethodPost, r.config.UpstreamEndpoint, bytes.NewReader(queued.body))
	if err != nil {
		return 0, 0, false, err
	}
	req.Header.Set("Content-Type", "application/x-protobuf")
	if queued.contentEncoding != "" {
		req.Header.Set("Content-Encoding", queued.contentEncoding)
	}
	req.Header.Set("Authorization", `Token token="`+token+`"`)
	req.Header.Set("Buildkite-Tests-Run-Key", r.config.RunKey)

	resp, err := r.client.Do(req)
	if err != nil {
		return 0, 0, false, err
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4*1024))
	_ = resp.Body.Close()

	retryAfter, hasRetryAfter := parseRetryAfter(resp.Header.Get("Retry-After"), time.Now())
	return resp.StatusCode, retryAfter, hasRetryAfter, nil
}

func (r *Relay) authorizationToken() (string, error) {
	if r.credentialErr != nil {
		return "", r.credentialErr
	}

	now := time.Now()
	if r.upstreamToken != "" && now.Before(r.tokenRefreshAt) {
		return r.upstreamToken, nil
	}

	token, err := r.config.TokenSource(r.ctx)
	if err != nil {
		if errors.Is(err, ErrTerminalCredential) {
			r.credentialErr = err
		}
		return "", err
	}
	if strings.TrimSpace(token) == "" {
		return "", fmt.Errorf("OIDC token source returned an empty token")
	}
	r.upstreamToken = strings.TrimSpace(token)
	r.tokenRefreshAt = tokenRefreshTime(r.upstreamToken, now, r.config.TokenLifetime)
	return r.upstreamToken, nil
}

func tokenRefreshTime(token string, now time.Time, fallbackLifetime time.Duration) time.Time {
	expiresAt := now.Add(fallbackLifetime)
	parts := strings.Split(token, ".")
	if len(parts) == 3 {
		if payload, err := base64.RawURLEncoding.DecodeString(parts[1]); err == nil {
			var claims struct {
				ExpiresAt int64 `json:"exp"`
			}
			if json.Unmarshal(payload, &claims) == nil && claims.ExpiresAt > 0 {
				expiresAt = time.Unix(claims.ExpiresAt, 0)
			}
		}
	}

	lifetime := expiresAt.Sub(now)
	if lifetime <= 0 {
		return now
	}
	leeway := min(defaultTokenRefreshLeeway, lifetime/10)
	return expiresAt.Add(-leeway)
}

func parseRetryAfter(value string, now time.Time) (time.Duration, bool) {
	if value == "" {
		return 0, false
	}
	if seconds, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second, true
	}
	if retryAt, err := http.ParseTime(value); err == nil {
		return max(retryAt.Sub(now), 0), true
	}
	return 0, false
}

func (r *Relay) clampRetryDelay(delay time.Duration) time.Duration {
	r.mu.Lock()
	deadline := r.drainDeadline
	drainDelay := r.drainRetryDelay
	r.mu.Unlock()
	if deadline.IsZero() {
		return delay
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return 0
	}
	return min(drainDelay, remaining)
}

func (r *Relay) complete(req *request, forwarded bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.queue) == 0 || r.queue[0] != req {
		return
	}
	r.queue[0] = nil
	r.queue = r.queue[1:]
	r.queuedSize -= len(req.body) + queueRequestOverhead
	if forwarded {
		r.report.ForwardedRequests++
	} else {
		r.report.DroppedPermanentRequests++
		r.report.DroppedBytes += int64(len(req.body))
	}
}

func (r *Relay) dropRemainingAtDeadline() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.report.DroppedDeadlineRequests += len(r.queue)
	for _, req := range r.queue {
		r.report.DroppedBytes += int64(len(req.body))
	}
	r.queue = nil
	r.queuedSize = 0
}

// Drain stops accepting requests and waits for queued requests to be delivered
// until ctx expires. The deadline always wins over an in-flight request or
// retry delay.
func (r *Relay) Drain(ctx context.Context) Report {
	r.mu.Lock()
	firstDrain := !r.draining
	r.draining = true
	r.accepting = false
	if deadline, ok := ctx.Deadline(); ok {
		r.drainDeadline = deadline
		r.drainRetryDelay = max(time.Until(deadline)/3, time.Nanosecond)
	}
	r.mu.Unlock()
	if firstDrain {
		close(r.drainStarted)
	}
	r.signal()

	if firstDrain {
		if err := r.server.Shutdown(ctx); err != nil {
			_ = r.server.Close()
		}
	}

	select {
	case <-r.done:
	case <-ctx.Done():
		r.cancel()
		_ = r.server.Close()
		<-r.done
	}
	r.cancel()

	r.mu.Lock()
	defer r.mu.Unlock()
	return r.report
}

func validRunKey(runKey string) bool {
	if len(runKey) < 1 || len(runKey) > 255 {
		return false
	}
	for _, char := range []byte(runKey) {
		if char < '!' || char > '~' {
			return false
		}
	}
	return true
}
