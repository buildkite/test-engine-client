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
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
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
	defaultMaxLinger          = 250 * time.Millisecond
	defaultChildLinger        = time.Second
	defaultMaxBatchRequests   = 1000
	defaultMaxBatchBytes      = 750 * 1024
)

// With batching enabled, queued requests are packed byte-for-byte into this
// envelope and POSTed to the upstream traces endpoint with
// batchContentType. The receiver publishes each member as its own Kafka record.
//
//	package buildkite.test_engine.otlp.v1;
//
//	message TraceRequestBatch {
//	  repeated TraceRequest requests = 1;
//	}
//	message TraceRequest {
//	  bytes body = 1;              // exact bytes bktec received
//	  string content_encoding = 2; // "" (identity) or "gzip"
//	}
const (
	batchContentType                 = "application/vnd.buildkite.otlp-trace-batch.v1+protobuf"
	batchRequestsField               = protowire.Number(1)
	traceRequestBodyField            = protowire.Number(1)
	traceRequestContentEncodingField = protowire.Number(2)
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
	// ExperimentalBatching opts in to opaque envelopes of one span stream.
	// An envelope refused with HTTP 415 disables batching for the run.
	ExperimentalBatching bool

	initialBackoff time.Duration
	maxBackoff     time.Duration
	// A batch waits up to maxLinger after its oldest request was queued for
	// more requests, unless it reaches maxBatchRequests or the encoded
	// envelope reaches maxBatchBytes first.
	maxLinger        time.Duration
	maxBatchRequests int
	maxBatchBytes    int
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

// maxStatSamples bounds each reservoir to 32 KiB regardless of request volume.
// The relay uses two reservoirs, plus two per stream when batching is enabled.
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

// Report summarizes requests delivered or dropped by a relay. ForwardedSize
// and ForwardedLatency cover forwarded requests only; latency is the duration
// of each request's successful upstream POST, excluding failed attempts and
// retry backoff. When Batching is enabled, UpstreamUploads counts upstream
// POST attempts, including retries and refusals.
type Report struct {
	ForwardedRequests                 int
	ForwardedBytes                    int64
	Batching                          bool
	UpstreamUploads                   int
	ForwardedSize                     Stats[int64]
	ForwardedLatency                  Stats[time.Duration]
	DroppedPermanentRequests          int
	DroppedDeadlineRequests           int
	DroppedDeadlineTestRequests       int
	DroppedDeadlineChildRequests      int
	DroppedDeadlineUnlabelledRequests int
	DroppedBytes                      int64
	Refusals                          []Refusal
	Streams                           []StreamReport
}

// StreamReport measures accepted requests and upstream HTTP attempts. Unknown
// streams are included in unlabelled. ItemsPerUpload includes retries; QueueWait
// measures each member once, at its first HTTP attempt (excluding retry backoff).
// DrainTime ends when the stream's last pending request completes or is dropped.
type StreamReport struct {
	Stream         string
	RequestsIn     int
	UploadsOut     int
	ItemsPerUpload Stats[int64]
	QueueWait      Stats[time.Duration]
	DrainTime      time.Duration
}

type streamMetrics struct {
	StreamReport
	items   sampler[int64]
	wait    sampler[time.Duration]
	pending int
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
	enqueuedAt      time.Time
	started         bool
}

// upload is one upstream POST: a single request forwarded as-is, or a batch
// envelope whose body packs its members.
type upload struct {
	request
	members  []*request
	envelope bool
}

// Queues share the total admission budget, including in-flight members.
// Children additionally have a half-budget cap to leave room for results.
type requestQueue struct {
	requests []*request
	size     int
	wake     chan struct{}
}

type deliveryResult int

const (
	deliveryForwarded deliveryResult = iota
	deliveryPermanentFailure
	deliveryDeadline
	deliveryBatchUnsupported
)

// Relay accepts authenticated OTLP/HTTP protobuf requests on loopback and
// forwards their byte-exact bodies to Buildkite. Children have a separate
// worker so child retries and in-flight uploads cannot strand test results.
type Relay struct {
	config Config

	endpoint string
	token    string

	server *http.Server
	client *http.Client

	ctx             context.Context
	cancel          context.CancelFunc
	drainStarted    chan struct{}
	batchingStopped chan struct{}
	done            chan struct{}

	mu               sync.Mutex
	results          requestQueue
	children         requestQueue
	queuedSize       int
	accepting        bool
	draining         bool
	drainDeadline    time.Time
	drainRetryDelay  time.Duration
	report           Report
	sizeSampler      sampler[int64]
	latencySampler   sampler[time.Duration]
	streams          [3]streamMetrics
	drainStart       time.Time
	batchingDisabled bool

	// Serialize credential refreshes and terminal-error logging across workers.
	credentialMu        sync.Mutex
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
	if config.maxLinger <= 0 {
		config.maxLinger = defaultMaxLinger
	}
	if config.maxBatchRequests <= 0 {
		config.maxBatchRequests = defaultMaxBatchRequests
	}
	if config.maxBatchBytes <= 0 {
		config.maxBatchBytes = defaultMaxBatchBytes
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
		config:          config,
		endpoint:        "http://" + listener.Addr().String(),
		token:           base64.RawURLEncoding.EncodeToString(localTokenBytes),
		client:          client,
		ctx:             ctx,
		cancel:          cancel,
		results:         requestQueue{wake: make(chan struct{}, 1)},
		children:        requestQueue{wake: make(chan struct{}, 1)},
		drainStarted:    make(chan struct{}),
		batchingStopped: make(chan struct{}),
		done:            make(chan struct{}),
		accepting:       true,
		report:          Report{Batching: config.ExperimentalBatching},
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
	queue := &r.results
	if streamKind(req.spanStream) == "child" {
		queue = &r.children
		if queue.size+size > r.config.QueueCapacity/2 {
			return false
		}
	}
	req.enqueuedAt = time.Now()
	queue.requests = append(queue.requests, req)
	queue.size += size
	r.queuedSize += size
	if r.config.ExperimentalBatching {
		metrics := &r.streams[streamIndex(req.spanStream)]
		metrics.RequestsIn++
		metrics.pending++
	}
	queue.signal()
	return true
}

func (q *requestQueue) signal() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

func (r *Relay) forward() {
	defer close(r.done)

	var workers sync.WaitGroup
	for _, queue := range []*requestQueue{&r.results, &r.children} {
		workers.Go(func() { r.forwardQueue(queue) })
	}
	workers.Wait()
	// Only account for leftovers after both workers record their final outcome.
	r.dropRemainingAtDeadline()
}

func (r *Relay) forwardQueue(queue *requestQueue) {
	for {
		next, ok := r.nextUpload(queue)
		if !ok {
			return
		}

		result, latency := r.deliver(next)
		switch result {
		case deliveryForwarded:
			r.complete(queue, next, true, latency)
		case deliveryPermanentFailure:
			r.complete(queue, next, false, 0)
		case deliveryDeadline:
			return
		case deliveryBatchUnsupported:
			// Members remain queued. The next iteration sends them through the
			// normal single-request path, without recording a refusal or drop.
		}
	}
}

func (r *Relay) nextUpload(queue *requestQueue) (*upload, bool) {
	if r.config.ExperimentalBatching {
		return r.nextBatch(queue)
	}
	req, ok := r.nextRequest(queue)
	if !ok {
		return nil, false
	}
	return singleUpload(req), true
}

func singleUpload(req *request) *upload {
	return &upload{request: *req, members: []*request{req}}
}

// nextBatch collects the queue head and later queued requests of the same
// span stream, in order, so per-stream order is preserved. It waits for more
// requests until the batch is full or maxLinger has passed since the head was
// queued; draining sends immediately. A request too large for any envelope is
// sent alone, unchanged.
func (r *Relay) nextBatch(queue *requestQueue) (*upload, bool) {
	var members []*request
	envelopeSize := 0
	index := 0
	for {
		if r.ctx.Err() != nil {
			return nil, false
		}
		r.mu.Lock()
		if r.batchingDisabled {
			r.mu.Unlock()
			req, ok := r.nextRequest(queue)
			if !ok {
				return nil, false
			}
			return singleUpload(req), true
		}
		if index < len(queue.requests) {
			req := queue.requests[index]
			index++
			r.mu.Unlock()
			if len(members) > 0 && req.spanStream != members[0].spanStream {
				continue
			}
			itemSize := envelopeItemSize(req)
			if len(members) == 0 && itemSize > r.config.maxBatchBytes {
				return singleUpload(req), true
			}
			if envelopeSize+itemSize > r.config.maxBatchBytes {
				break
			}
			members = append(members, req)
			envelopeSize += itemSize
			if len(members) >= r.config.maxBatchRequests || envelopeSize == r.config.maxBatchBytes {
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
		if len(members) > 0 {
			linger := r.config.maxLinger
			if streamKind(members[0].spanStream) == "child" {
				linger = defaultChildLinger
			}
			remaining := time.Until(members[0].enqueuedAt.Add(linger))
			if remaining <= 0 {
				break
			}
			timer = time.NewTimer(remaining)
			timeout = timer.C
		}
		select {
		case <-queue.wake:
		case <-timeout:
		case <-r.ctx.Done():
		}
		if timer != nil {
			timer.Stop()
		}
	}
	if len(members) == 0 {
		return nil, false
	}
	body := make([]byte, 0, envelopeSize)
	for _, req := range members {
		body = protowire.AppendTag(body, batchRequestsField, protowire.BytesType)
		//nolint:gosec // Size is non-negative and bounded by the accepted 900 KiB body plus framing.
		body = protowire.AppendVarint(body, uint64(traceRequestSize(req)))
		if len(req.body) > 0 {
			body = protowire.AppendTag(body, traceRequestBodyField, protowire.BytesType)
			body = protowire.AppendBytes(body, req.body)
		}
		if req.contentEncoding != "" {
			body = protowire.AppendTag(body, traceRequestContentEncodingField, protowire.BytesType)
			body = protowire.AppendString(body, req.contentEncoding)
		}
	}
	return &upload{
		request:  request{body: body, spanStream: members[0].spanStream},
		members:  members,
		envelope: true,
	}, true
}

// envelopeItemSize is the exact number of bytes req adds to a batch envelope,
// including its field tag and length prefix.
func envelopeItemSize(req *request) int {
	return protowire.SizeTag(batchRequestsField) + protowire.SizeBytes(traceRequestSize(req))
}

// traceRequestSize is the encoded size of req as a TraceRequest. As in proto3,
// empty fields are omitted.
func traceRequestSize(req *request) int {
	size := 0
	if len(req.body) > 0 {
		size += protowire.SizeTag(traceRequestBodyField) + protowire.SizeBytes(len(req.body))
	}
	if req.contentEncoding != "" {
		size += protowire.SizeTag(traceRequestContentEncodingField) + protowire.SizeBytes(len(req.contentEncoding))
	}
	return size
}

func (r *Relay) nextRequest(queue *requestQueue) (*request, bool) {
	for {
		r.mu.Lock()
		if len(queue.requests) > 0 {
			req := queue.requests[0]
			r.mu.Unlock()
			return req, true
		}
		if r.draining {
			r.mu.Unlock()
			return nil, false
		}
		r.mu.Unlock()

		select {
		case <-queue.wake:
		case <-r.ctx.Done():
			return nil, false
		}
	}
}

// deliver returns the delivery result and, for a forwarded upload, the
// duration of the successful upstream POST.
func (r *Relay) deliver(queued *upload) (deliveryResult, time.Duration) {
	backoff := r.config.initialBackoff
	for {
		if queued.envelope {
			r.mu.Lock()
			disabled := r.batchingDisabled
			r.mu.Unlock()
			if disabled {
				return deliveryBatchUnsupported, 0
			}
		}
		token, err := r.authorizationToken()
		if err != nil && errors.Is(err, ErrTerminalCredential) {
			r.credentialMu.Lock()
			if !r.credentialErrLogged {
				r.credentialErrLogged = true
				r.config.Logf("bktec OTLP relay: credential was refused and will not be retried; dropping OTLP requests: %v", err)
			}
			r.credentialMu.Unlock()
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
			case status == http.StatusUnsupportedMediaType && queued.envelope:
				r.mu.Lock()
				first := !r.batchingDisabled
				r.batchingDisabled = true
				if first {
					close(r.batchingStopped)
				}
				r.mu.Unlock()
				if first {
					r.config.Logf("bktec OTLP relay: upstream does not support trace batches (HTTP 415); disabling batching for this run and resending requests individually")
					r.results.signal()
					r.children.signal()
				}
				return deliveryBatchUnsupported, 0
			case status != http.StatusRequestTimeout && status != http.StatusTooManyRequests && status < 500:
				r.recordRefusal(status, message, queued.spanStream, len(queued.members))
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
		if !r.waitForRetry(delay, queued.envelope) {
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
	switch streamKind(stream) {
	case "test":
		refusal.TestRequests += count
	case "child":
		refusal.ChildRequests += count
	default:
		refusal.UnlabelledRequests += count
	}
	r.mu.Unlock()
	if first {
		dropped := "a request"
		if count > 1 {
			dropped = fmt.Sprintf("%d requests", count)
		}
		if message == "" {
			r.config.Logf("bktec OTLP relay: dropped %s after upstream returned HTTP %d", dropped, status)
		} else {
			r.config.Logf("bktec OTLP relay: dropped %s after upstream returned HTTP %d: %s", dropped, status, message)
		}
	}
}

func (r *Relay) waitForRetry(delay time.Duration, envelope bool) bool {
	var batchingStopped <-chan struct{}
	if envelope {
		batchingStopped = r.batchingStopped
	}
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
			// A deadline-clamped timer must not retry while Drain wakes to cancel.
			r.mu.Lock()
			deadline := r.drainDeadline
			r.mu.Unlock()
			return r.ctx.Err() == nil && (deadline.IsZero() || time.Now().Before(deadline))
		case <-drainStarted:
			timer.Stop()
		case <-batchingStopped:
			timer.Stop()
			// deliver rechecks batchingDisabled before another attempt, so
			// these members go single without waiting for envelope backoff.
			return true
		case <-r.ctx.Done():
			timer.Stop()
			return false
		}
	}
}

func (r *Relay) send(queued *upload, token string) (*http.Response, error) {
	contentType := "application/x-protobuf"
	if queued.envelope {
		contentType = batchContentType
	}
	req, err := http.NewRequestWithContext(r.ctx, http.MethodPost, r.config.UpstreamEndpoint, bytes.NewReader(queued.body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", contentType)
	if queued.contentEncoding != "" {
		req.Header.Set("Content-Encoding", queued.contentEncoding)
	}
	req.Header.Set("Authorization", `Token token="`+token+`"`)
	req.Header.Set("Buildkite-Tests-Run-Key", r.config.RunKey)
	if queued.spanStream != "" {
		req.Header.Set("Buildkite-Tests-Span-Stream", queued.spanStream)
	}

	if r.config.ExperimentalBatching {
		r.mu.Lock()
		r.report.UpstreamUploads++
		metrics := &r.streams[streamIndex(queued.spanStream)]
		metrics.UploadsOut++
		metrics.items.observe(int64(len(queued.members)))
		for _, member := range queued.members {
			if !member.started {
				metrics.wait.observe(time.Since(member.enqueuedAt))
				member.started = true
			}
		}
		r.mu.Unlock()
	}
	return r.client.Do(req)
}

func (r *Relay) authorizationToken() (string, error) {
	r.credentialMu.Lock()
	defer r.credentialMu.Unlock()

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

func (r *Relay) complete(queue *requestQueue, done *upload, forwarded bool, latency time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !queue.dequeue(done.members) {
		return
	}
	for _, req := range done.members {
		size := len(req.body) + queueRequestOverhead
		r.queuedSize -= size
		queue.size -= size
		if r.config.ExperimentalBatching {
			metrics := &r.streams[streamIndex(req.spanStream)]
			metrics.pending--
			if r.draining && metrics.pending == 0 {
				metrics.DrainTime = time.Since(r.drainStart)
			}
		}
		if forwarded {
			r.report.ForwardedRequests++
			r.report.ForwardedBytes += int64(len(req.body))
			r.sizeSampler.observe(int64(len(req.body)))
			r.latencySampler.observe(latency)
		} else {
			r.report.DroppedPermanentRequests++
			r.report.DroppedBytes += int64(len(req.body))
		}
	}
}

// dequeue removes members, an in-order subsequence of the queue, and reports
// whether all were found. Requests skipped between members shift back into
// the gaps, so the rest of the queue never moves.
func (q *requestQueue) dequeue(members []*request) bool {
	last, found := -1, 0
	for i := 0; i < len(q.requests) && found < len(members); i++ {
		if q.requests[i] == members[found] {
			last, found = i, found+1
		}
	}
	if found < len(members) {
		return false
	}
	free := last
	for i := last; i >= 0; i-- {
		if found > 0 && q.requests[i] == members[found-1] {
			found--
			continue
		}
		q.requests[free] = q.requests[i]
		free--
	}
	clear(q.requests[:free+1])
	q.requests = q.requests[free+1:]
	return true
}

// Unknown labels may carry test results; treat them as unlabelled for reporting.
func streamKind(stream string) string {
	switch kind := strings.ToLower(strings.TrimSpace(stream)); kind {
	case "test", "child":
		return kind
	default:
		return ""
	}
}

func streamIndex(stream string) int {
	switch streamKind(stream) {
	case "test":
		return 0
	case "child":
		return 1
	default:
		return 2
	}
}

func (r *Relay) dropRemainingAtDeadline() {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, queue := range []*requestQueue{&r.results, &r.children} {
		r.report.DroppedDeadlineRequests += len(queue.requests)
		for _, req := range queue.requests {
			r.report.DroppedBytes += int64(len(req.body))
			switch streamKind(req.spanStream) {
			case "test":
				r.report.DroppedDeadlineTestRequests++
			case "child":
				r.report.DroppedDeadlineChildRequests++
			default:
				r.report.DroppedDeadlineUnlabelledRequests++
			}
		}
		queue.requests = nil
		queue.size = 0
	}
	for i := range r.streams {
		metrics := &r.streams[i]
		if metrics.pending > 0 {
			metrics.DrainTime = time.Since(r.drainStart)
			metrics.pending = 0
		}
	}
	r.queuedSize = 0
}

// Drain stops accepting requests and waits for queued requests to be delivered
// until ctx expires. The deadline always wins over an in-flight request or
// retry delay.
func (r *Relay) Drain(ctx context.Context) Report {
	r.mu.Lock()
	firstDrain := !r.draining
	if firstDrain {
		r.drainStart = time.Now()
	}
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
	r.results.signal()
	r.children.signal()

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
	if report.Batching {
		for i, name := range []string{"test", "child", "unlabelled"} {
			metrics := &r.streams[i]
			stream := metrics.StreamReport
			stream.Stream = name
			stream.ItemsPerUpload = metrics.items.stats()
			stream.QueueWait = metrics.wait.stats()
			report.Streams = append(report.Streams, stream)
		}
	}
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
