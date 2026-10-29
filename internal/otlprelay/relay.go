package otlprelay

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	mathrand "math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	rpcstatus "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/protobuf/proto"
)

const (
	DefaultUpstreamEndpoint = "https://tests-otlp.buildkite.com/v1/traces"
	ResourceAttribute       = "buildkite.otlp.endpoint"

	maxRequestBytes           = 900 * 1024
	defaultQueueCapacity      = 64 * 1024 * 1024
	queueRequestOverhead      = 1024 // Account for object and slice overhead, even with an empty body.
	maxBatchBytes             = 512 * 1024
	maxBatchRequests          = 100
	defaultBatchWindow        = time.Second
	defaultRequestTimeout     = 30 * time.Second
	defaultInitialBackoff     = 250 * time.Millisecond
	defaultMaxBackoff         = 5 * time.Second
	defaultTokenLifetime      = 2 * time.Hour
	defaultTokenRefreshLeeway = 5 * time.Minute
)

// Keep detailed reasons bounded even if a receiver includes a different request
// ID in every message. Further reasons are counted together per HTTP status.
const maxRefusalMessages = 32

// ErrTerminalCredential marks a credential failure that will never succeed if
// retried, such as the Buildkite API refusing to mint a token for a disallowed
// audience. TokenSource implementations wrap errors with it (check with
// errors.Is). At startup a terminal failure fails relay creation; mid-run the
// relay stops calling TokenSource and drops requests as permanent failures.
// All other TokenSource errors are treated as transient and retried.
var ErrTerminalCredential = errors.New("terminal OTLP relay credential failure")

// Config contains the trusted values that bktec adds when forwarding an OTLP
// request. The test process supplies only the opaque protobuf body, its
// content encoding, and an optional span stream label.
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
	batchWindow    time.Duration
}

// Stats summarizes the distribution of a per-request measurement.
type Stats[T ~int64] struct {
	P50 T
	P90 T
	Max T
}

// summarize computes distribution stats over samples without mutating them.
// The zero Stats is returned for no samples. Percentiles use the nearest-rank
// method.
func summarize[T ~int64](samples []T) Stats[T] {
	if len(samples) == 0 {
		return Stats[T]{}
	}
	sorted := slices.Clone(samples)
	slices.Sort(sorted)
	n := len(sorted)
	return Stats[T]{
		P50: sorted[(n+1)/2-1],
		P90: sorted[(9*n+9)/10-1],
		Max: sorted[n-1],
	}
}

// maxStatSamples bounds the memory retained for percentile estimation to
// 2 reservoirs x 4096 samples x 8 bytes = 64KB regardless of request volume.
const maxStatSamples = 4096

// sampler observes per-request measurements with bounded memory: an exact
// running max plus a fixed-size uniform reservoir (Algorithm R) for
// percentiles. Results are exact until the reservoir fills; beyond that
// percentiles are unbiased estimates.
type sampler[T ~int64] struct {
	samples []T
	seen    int
	max     T
}

func (s *sampler[T]) observe(value T) {
	s.seen++
	if value > s.max {
		s.max = value
	}
	if len(s.samples) < maxStatSamples {
		s.samples = append(s.samples, value)
		return
	}
	//nolint:gosec // Reservoir sampling does not require cryptographic randomness.
	if slot := mathrand.IntN(s.seen); slot < len(s.samples) {
		s.samples[slot] = value
	}
}

func (s *sampler[T]) stats() Stats[T] {
	stats := summarize(s.samples)
	stats.Max = s.max
	return stats
}

// Report counts original accepted requests and bytes delivered or dropped.
// ForwardedSize and ForwardedLatency measure successful upstream batches;
// latency excludes failed attempts and retry backoff.
type Report struct {
	ForwardedRequests        int
	ForwardedBytes           int64
	ForwardedSize            Stats[int64]
	ForwardedLatency         Stats[time.Duration]
	DroppedPermanentRequests int
	DroppedDeadlineRequests  int
	DroppedBytes             int64
	Refusals                 []Refusal
}

// Refusal counts permanently refused requests with the same HTTP status and
// printable server message, or an omission notice after the detail limit.
// Requests without a recognized stream may include test results.
type Refusal struct {
	Status             int
	Message            string
	Requests           int
	TestRequests       int
	ChildRequests      int
	UnlabelledRequests int
}

type request struct {
	body            []byte
	contentEncoding string
	spanStream      string
	queuedAt        time.Time
}

type batch struct {
	request
	members []*request
}

type deliveryResult int

const (
	deliveryForwarded deliveryResult = iota
	deliveryPermanentFailure
	deliveryDeadline
)

// Relay accepts authenticated OTLP/HTTP protobuf requests on loopback and
// batches them by span stream for delivery from one background worker.
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
	sizeSampler     sampler[int64]
	latencySampler  sampler[time.Duration]

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
	if config.batchWindow <= 0 {
		config.batchWindow = defaultBatchWindow
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
		relay.config.Logf("bktec OTLP relay: starting without an upstream credential; requests will be buffered while it retries in the background: %v", err)
	}
	relay.server = &http.Server{
		Handler:           relay,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		MaxHeaderBytes:    16 * 1024,
	}

	go func() {
		if err := relay.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			relay.config.Logf("bktec OTLP relay: listener stopped: %v", err)
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

	// Collectors send "test" or "child"; the receiver validates it.
	spanStream := strings.TrimSpace(req.Header.Get("Buildkite-Tests-Span-Stream"))

	if !r.enqueue(&request{body: body, contentEncoding: contentEncoding, spanStream: spanStream}) {
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
	req.queuedAt = time.Now()
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
		req, ok := r.nextBatch()
		if !ok {
			if r.ctx.Err() != nil {
				r.dropRemainingAtDeadline()
			}
			return
		}

		result, latency := r.deliver(&req.request, len(req.members))
		switch result {
		case deliveryForwarded:
			r.complete(req, true, latency)
		case deliveryPermanentFailure:
			r.complete(req, false, 0)
		case deliveryDeadline:
			r.dropRemainingAtDeadline()
			return
		}
	}
}

func (r *Relay) nextBatch() (*batch, bool) {
	var pending batch
	var body []byte
	index := 0
	for {
		if r.ctx.Err() != nil {
			return nil, false
		}
		r.mu.Lock()
		if index < len(r.queue) {
			req := r.queue[index]
			index++
			r.mu.Unlock()
			if len(pending.members) > 0 && req.spanStream != pending.spanStream {
				continue
			}
			decoded, ok := batchBody(req)
			if !ok || len(body)+len(decoded) > maxBatchBytes {
				if len(pending.members) == 0 {
					// Preserve oversized or invalid gzip requests as-is.
					return &batch{request: *req, members: []*request{req}}, true
				}
				break
			}
			if len(pending.members) == 0 {
				pending.request = *req
			}
			pending.members = append(pending.members, req)
			body = append(body, decoded...)
			if len(body) >= maxBatchBytes || len(pending.members) >= maxBatchRequests {
				break
			}
			continue
		}
		draining := r.draining
		r.mu.Unlock()
		if draining {
			break
		}

		var timeout <-chan time.Time
		var timer *time.Timer
		if len(pending.members) > 0 {
			remaining := time.Until(pending.queuedAt.Add(r.config.batchWindow))
			if remaining <= 0 {
				break
			}
			timer = time.NewTimer(remaining)
			timeout = timer.C
		}
		select {
		case <-r.wake:
		case <-timeout:
		case <-r.ctx.Done():
		}
		if timer != nil {
			timer.Stop()
		}
	}
	if len(pending.members) == 0 {
		return nil, false
	}
	if len(pending.members) > 1 {
		// ExportTraceServiceRequest contains repeated resource_spans. Protobuf
		// concatenation merges those fields without decoding/re-encoding spans.
		var compressed bytes.Buffer
		writer := gzip.NewWriter(&compressed)
		// bytes.Buffer writes cannot fail.
		_, _ = writer.Write(body)
		_ = writer.Close()
		pending.body = compressed.Bytes()
		pending.contentEncoding = "gzip"
	}
	return &pending, true
}

// Bound decompression as well as batch size. Requests that cannot fit in a
// batch are forwarded alone, retaining their original encoding and bytes.
func batchBody(req *request) ([]byte, bool) {
	if req.contentEncoding != "gzip" {
		return req.body, len(req.body) <= maxBatchBytes
	}
	reader, err := gzip.NewReader(bytes.NewReader(req.body))
	if err != nil {
		return nil, false
	}
	defer reader.Close()
	body, err := io.ReadAll(io.LimitReader(reader, maxBatchBytes+1))
	return body, err == nil && len(body) <= maxBatchBytes
}

// deliver returns the delivery result and, for a forwarded request, the
// duration of the successful upstream POST.
func (r *Relay) deliver(queued *request, count int) (deliveryResult, time.Duration) {
	backoff := r.config.initialBackoff
	for {
		token, err := r.authorizationToken()
		if err != nil && errors.Is(err, ErrTerminalCredential) {
			if !r.credentialErrLogged {
				r.credentialErrLogged = true
				r.config.Logf("bktec OTLP relay: credential was refused and will not be retried; dropping OTLP requests: %v", err)
			}
			return deliveryPermanentFailure, 0
		}
		var status int
		var retryAfter time.Duration
		var hasRetryAfter bool
		var sendLatency time.Duration
		var message string
		if err == nil {
			sendStart := time.Now()
			var resp *http.Response
			resp, err = r.send(queued, token)
			if err == nil {
				status = resp.StatusCode
				retryAfter, hasRetryAfter = parseRetryAfter(resp.Header.Get("Retry-After"), time.Now())
				body, readErr := io.ReadAll(io.LimitReader(resp.Body, 4*1024+1))
				_ = resp.Body.Close()
				// The receiver returns a binary google.rpc.Status, matching our
				// protobuf requests. Never print an undecodable or truncated body.
				if readErr == nil && len(body) <= 4*1024 {
					var refusal rpcstatus.Status
					if proto.Unmarshal(body, &refusal) == nil {
						// Receiver text must not inject log lines, Buildkite group
						// markers, or terminal controls into refusal diagnostics.
						message = strings.Map(func(char rune) rune {
							if !unicode.IsPrint(char) {
								return ' '
							}
							return char
						}, refusal.GetMessage())
					}
				}
			}
			sendLatency = time.Since(sendStart)
		}

		if err == nil {
			switch {
			case status >= 200 && status < 300:
				return deliveryForwarded, sendLatency
			case status != http.StatusRequestTimeout && status != http.StatusTooManyRequests && status < 500:
				r.recordRefusal(status, message, queued.spanStream, count)
				return deliveryPermanentFailure, 0
			}
		}
		if r.ctx.Err() != nil {
			return deliveryDeadline, 0
		}

		delay := backoff
		if hasRetryAfter {
			delay = retryAfter
		}
		if !r.waitForRetry(delay) {
			return deliveryDeadline, 0
		}
		backoff = min(backoff*2, r.config.maxBackoff)
	}
}

func (r *Relay) recordRefusal(status int, message, stream string, count int) {
	r.mu.Lock()
	first := false
	index := slices.IndexFunc(r.report.Refusals, func(refusal Refusal) bool {
		return refusal.Status == status && refusal.Message == message
	})
	if index == -1 && len(r.report.Refusals) >= maxRefusalMessages {
		message = "further distinct refusal messages omitted"
		index = slices.IndexFunc(r.report.Refusals, func(refusal Refusal) bool {
			return refusal.Status == status && refusal.Message == message
		})
	}
	if index == -1 {
		first = true
		index = len(r.report.Refusals)
		r.report.Refusals = append(r.report.Refusals, Refusal{Status: status, Message: message})
	}
	refusal := &r.report.Refusals[index]
	refusal.Requests += count
	switch strings.ToLower(strings.TrimSpace(stream)) {
	case "test":
		refusal.TestRequests += count
	case "child":
		refusal.ChildRequests += count
	default:
		refusal.UnlabelledRequests += count
	}
	r.mu.Unlock()
	if first {
		if message == "" {
			r.config.Logf("bktec OTLP relay: dropped a request after upstream returned HTTP %d", status)
		} else {
			r.config.Logf("bktec OTLP relay: dropped a request after upstream returned HTTP %d: %s", status, message)
		}
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
			// An expired deadline clamps the delay to zero. Do not retry in a
			// tight loop while Drain's goroutine is still waking up to cancel.
			r.mu.Lock()
			deadline := r.drainDeadline
			r.mu.Unlock()
			return r.ctx.Err() == nil && (deadline.IsZero() || time.Now().Before(deadline))
		case <-drainStarted:
			timer.Stop()
		case <-r.ctx.Done():
			timer.Stop()
			return false
		}
	}
}

func (r *Relay) send(queued *request, token string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(r.ctx, http.MethodPost, r.config.UpstreamEndpoint, bytes.NewReader(queued.body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-protobuf")
	if queued.contentEncoding != "" {
		req.Header.Set("Content-Encoding", queued.contentEncoding)
	}
	req.Header.Set("Authorization", `Token token="`+token+`"`)
	req.Header.Set("Buildkite-Tests-Run-Key", r.config.RunKey)
	if queued.spanStream != "" {
		req.Header.Set("Buildkite-Tests-Span-Stream", queued.spanStream)
	}

	return r.client.Do(req)
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

func (r *Relay) complete(req *batch, forwarded bool, latency time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()

	member := 0
	r.queue = slices.DeleteFunc(r.queue, func(queued *request) bool {
		if member >= len(req.members) || queued != req.members[member] {
			return false
		}
		member++
		r.queuedSize -= len(queued.body) + queueRequestOverhead
		if forwarded {
			r.report.ForwardedRequests++
			r.report.ForwardedBytes += int64(len(queued.body))
		} else {
			r.report.DroppedPermanentRequests++
			r.report.DroppedBytes += int64(len(queued.body))
		}
		return true
	})
	if forwarded {
		r.sizeSampler.observe(int64(len(req.body)))
		r.latencySampler.observe(latency)
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
	r.report.ForwardedSize = r.sizeSampler.stats()
	r.report.ForwardedLatency = r.latencySampler.stats()
	report := r.report
	report.Refusals = slices.Clone(report.Refusals)
	return report
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
