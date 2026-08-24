package otlprelay

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRelayForwardsOpaqueGzipRequestWithTrustedHeaders(t *testing.T) {
	body := []byte("opaque compressed protobuf bytes")
	type receivedRequest struct {
		body            []byte
		authorization   string
		runKey          string
		contentType     string
		contentEncoding string
	}
	received := make(chan receivedRequest, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		gotBody, err := io.ReadAll(req.Body)
		if err != nil {
			t.Errorf("read upstream request: %v", err)
		}
		received <- receivedRequest{
			body:            gotBody,
			authorization:   req.Header.Get("Authorization"),
			runKey:          req.Header.Get("Buildkite-Tests-Run-Key"),
			contentType:     req.Header.Get("Content-Type"),
			contentEncoding: req.Header.Get("Content-Encoding"),
		}
		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	var tokenRequests atomic.Int32
	relay := newTestRelay(t, Config{
		UpstreamEndpoint: upstream.URL,
		RunKey:           "build-id",
		TokenSource: func(context.Context) (string, error) {
			tokenRequests.Add(1)
			return "oidc-token", nil
		},
	})

	resp := postTraces(t, relay, body, "Bearer "+relay.token, "gzip")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /v1/traces status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); got != "application/x-protobuf" {
		t.Errorf("response Content-Type = %q, want application/x-protobuf", got)
	}
	_ = resp.Body.Close()

	drainCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	report := relay.Drain(drainCtx)
	if report != (Report{ForwardedRequests: 1}) {
		t.Errorf("Drain report = %+v, want one forwarded request", report)
	}

	got := <-received
	if !bytes.Equal(got.body, body) {
		t.Errorf("upstream body = %q, want byte-exact %q", got.body, body)
	}
	if got.authorization != `Token token="oidc-token"` {
		t.Errorf("upstream Authorization = %q", got.authorization)
	}
	if got.runKey != "build-id" {
		t.Errorf("upstream Buildkite-Tests-Run-Key = %q, want build-id", got.runKey)
	}
	if got.contentType != "application/x-protobuf" {
		t.Errorf("upstream Content-Type = %q", got.contentType)
	}
	if got.contentEncoding != "gzip" {
		t.Errorf("upstream Content-Encoding = %q, want gzip", got.contentEncoding)
	}
	if got := tokenRequests.Load(); got != 1 {
		t.Errorf("OIDC token requests = %d, want 1", got)
	}
}

func TestRelayFailsToStartWhenInitialOIDCTokenIsUnavailable(t *testing.T) {
	_, err := New(Config{
		UpstreamEndpoint: "http://127.0.0.1:1/v1/traces",
		RunKey:           "build-id",
		TokenSource: func(context.Context) (string, error) {
			return "", fmt.Errorf("agent unavailable")
		},
	})
	if err == nil || !strings.Contains(err.Error(), "agent unavailable") {
		t.Fatalf("New() error = %v, want initial credential error", err)
	}
}

func TestRelayAcknowledgesBeforeDeliveryAndReturns429WhenByteQueueIsFull(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	body := []byte("full")
	relay := newTestRelay(t, Config{
		UpstreamEndpoint: upstream.URL,
		RunKey:           "build-id",
		QueueCapacity:    len(body),
		TokenSource:      staticToken("oidc-token"),
	})

	first := postTraces(t, relay, body, "Bearer "+relay.token, "")
	if first.StatusCode != http.StatusOK {
		t.Fatalf("first POST status = %d, want 200", first.StatusCode)
	}
	_ = first.Body.Close()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("upstream delivery did not start")
	}

	second := postTraces(t, relay, []byte("x"), "Bearer "+relay.token, "")
	if second.StatusCode != http.StatusTooManyRequests {
		t.Errorf("second POST status = %d, want 429", second.StatusCode)
	}
	_ = second.Body.Close()

	close(release)
	drainCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	report := relay.Drain(drainCtx)
	if report.ForwardedRequests != 1 || report.DroppedBytes != 0 {
		t.Errorf("Drain report = %+v, want one forwarded request and no silent drop", report)
	}
}

func TestRelayRetriesRetryableResponses(t *testing.T) {
	var requests atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	relay := newTestRelay(t, Config{
		UpstreamEndpoint: upstream.URL,
		RunKey:           "build-id",
		TokenSource:      staticToken("oidc-token"),
	})
	resp := postTraces(t, relay, []byte("request"), `Token token="`+relay.token+`"`, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST status = %d, want 200", resp.StatusCode)
	}
	_ = resp.Body.Close()

	drainCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	report := relay.Drain(drainCtx)
	if got := requests.Load(); got != 2 {
		t.Errorf("upstream requests = %d, want 2", got)
	}
	if report != (Report{ForwardedRequests: 1}) {
		t.Errorf("Drain report = %+v, want one forwarded request", report)
	}
}

func TestRelayRefreshesExpiredOIDCTokenWhileRetrying(t *testing.T) {
	var requests atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		requestNumber := requests.Add(1)
		if requestNumber == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if got := req.Header.Get("Authorization"); got != `Token token="fresh-token"` {
			t.Errorf("refreshed Authorization = %q", got)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	var tokenRequests atomic.Int32
	relay := newTestRelay(t, Config{
		UpstreamEndpoint: upstream.URL,
		RunKey:           "build-id",
		TokenSource: func(context.Context) (string, error) {
			if tokenRequests.Add(1) == 1 {
				payload := base64.RawURLEncoding.EncodeToString([]byte(`{"exp":1}`))
				return "header." + payload + ".signature", nil
			}
			return "fresh-token", nil
		},
	})
	resp := postTraces(t, relay, []byte("request"), "Bearer "+relay.token, "")
	_ = resp.Body.Close()

	drainCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	report := relay.Drain(drainCtx)
	if report != (Report{ForwardedRequests: 1}) {
		t.Errorf("Drain report = %+v, want one forwarded request", report)
	}
	if got := tokenRequests.Load(); got != 2 {
		t.Errorf("OIDC token requests = %d, want refresh after expiry", got)
	}
}

func TestRelayDropsPermanent4xx(t *testing.T) {
	var requests atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusForbidden)
	}))
	defer upstream.Close()

	body := []byte("request")
	relay := newTestRelay(t, Config{
		UpstreamEndpoint: upstream.URL,
		RunKey:           "build-id",
		TokenSource:      staticToken("oidc-token"),
	})
	resp := postTraces(t, relay, body, "Bearer "+relay.token, "")
	_ = resp.Body.Close()

	drainCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	report := relay.Drain(drainCtx)
	if got := requests.Load(); got != 1 {
		t.Errorf("upstream requests = %d, want no retry", got)
	}
	want := Report{DroppedPermanentRequests: 1, DroppedBytes: int64(len(body))}
	if report != want {
		t.Errorf("Drain report = %+v, want %+v", report, want)
	}
}

func TestRelayDrainDeadlineCancelsDeliveryAndReportsDrops(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
	}))
	defer upstream.Close()

	body := []byte("request")
	relay := newTestRelay(t, Config{
		UpstreamEndpoint: upstream.URL,
		RunKey:           "build-id",
		TokenSource:      staticToken("oidc-token"),
	})
	resp := postTraces(t, relay, body, "Bearer "+relay.token, "")
	_ = resp.Body.Close()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("upstream delivery did not start")
	}

	drainCtx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	report := relay.Drain(drainCtx)
	close(release)
	want := Report{DroppedDeadlineRequests: 1, DroppedBytes: int64(len(body))}
	if report != want {
		t.Errorf("Drain report = %+v, want %+v", report, want)
	}
}

func TestRelayDrainLimitsRetryAttempts(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var requests atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) == 1 {
			close(started)
			<-release
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer upstream.Close()

	body := []byte("request")
	relay := newTestRelay(t, Config{
		UpstreamEndpoint: upstream.URL,
		RunKey:           "build-id",
		TokenSource:      staticToken("oidc-token"),
	})
	resp := postTraces(t, relay, body, "Bearer "+relay.token, "")
	_ = resp.Body.Close()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("upstream delivery did not start")
	}

	drainCtx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()
	reportCh := make(chan Report, 1)
	go func() { reportCh <- relay.Drain(drainCtx) }()
	for {
		relay.mu.Lock()
		draining := relay.draining
		relay.mu.Unlock()
		if draining {
			break
		}
		time.Sleep(time.Millisecond)
	}
	close(release)
	report := <-reportCh

	if got := requests.Load(); got > 4 {
		t.Errorf("upstream requests during drain = %d, want at most 4", got)
	}
	want := Report{DroppedDeadlineRequests: 1, DroppedBytes: int64(len(body))}
	if report != want {
		t.Errorf("Drain report = %+v, want %+v", report, want)
	}
}

func TestRelayDrainInterruptsExistingRetryDelay(t *testing.T) {
	var requests atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer upstream.Close()

	body := []byte("request")
	relay := newTestRelay(t, Config{
		UpstreamEndpoint: upstream.URL,
		RunKey:           "build-id",
		TokenSource:      staticToken("oidc-token"),
	})
	resp := postTraces(t, relay, body, "Bearer "+relay.token, "")
	_ = resp.Body.Close()
	deadline := time.Now().Add(time.Second)
	for requests.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if requests.Load() == 0 {
		t.Fatal("upstream delivery did not start")
	}
	time.Sleep(20 * time.Millisecond)

	drainCtx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()
	report := relay.Drain(drainCtx)

	if got := requests.Load(); got < 2 || got > 4 {
		t.Errorf("upstream requests = %d, want 2 to 4 after interrupting Retry-After", got)
	}
	want := Report{DroppedDeadlineRequests: 1, DroppedBytes: int64(len(body))}
	if report != want {
		t.Errorf("Drain report = %+v, want %+v", report, want)
	}
}

func TestRelayRejectsInvalidLocalRequestsSynchronously(t *testing.T) {
	relay := newTestRelay(t, Config{
		UpstreamEndpoint: "http://127.0.0.1:1/v1/traces",
		RunKey:           "build-id",
		TokenSource:      staticToken("oidc-token"),
	})

	tests := []struct {
		name        string
		method      string
		path        string
		auth        string
		contentType string
		encoding    string
		body        []byte
		wantStatus  int
	}{
		{name: "authorization", method: http.MethodPost, path: "/v1/traces", contentType: "application/x-protobuf", body: []byte("x"), wantStatus: http.StatusUnauthorized},
		{name: "path", method: http.MethodPost, path: "/v1/metrics", auth: "Bearer " + relay.token, contentType: "application/x-protobuf", body: []byte("x"), wantStatus: http.StatusNotFound},
		{name: "method", method: http.MethodGet, path: "/v1/traces", auth: "Bearer " + relay.token, contentType: "application/x-protobuf", wantStatus: http.StatusMethodNotAllowed},
		{name: "content type", method: http.MethodPost, path: "/v1/traces", auth: "Bearer " + relay.token, contentType: "application/json", body: []byte("x"), wantStatus: http.StatusUnsupportedMediaType},
		{name: "content encoding", method: http.MethodPost, path: "/v1/traces", auth: "Bearer " + relay.token, contentType: "application/x-protobuf", encoding: "br", body: []byte("x"), wantStatus: http.StatusUnsupportedMediaType},
		{name: "wire size", method: http.MethodPost, path: "/v1/traces", auth: "Bearer " + relay.token, contentType: "application/x-protobuf", body: bytes.Repeat([]byte("x"), maxRequestBytes+1), wantStatus: http.StatusRequestEntityTooLarge},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req, err := http.NewRequest(test.method, relay.endpoint+test.path, bytes.NewReader(test.body))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Authorization", test.auth)
			req.Header.Set("Content-Type", test.contentType)
			if test.encoding != "" {
				req.Header.Set("Content-Encoding", test.encoding)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("local request: %v", err)
			}
			_ = resp.Body.Close()
			if resp.StatusCode != test.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, test.wantStatus)
			}
		})
	}

	drainCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if report := relay.Drain(drainCtx); report != (Report{}) {
		t.Errorf("Drain report = %+v, want empty", report)
	}
}

func TestRelayAcceptsExistingCollectorToken(t *testing.T) {
	relay := newTestRelay(t, Config{
		UpstreamEndpoint: "http://127.0.0.1:1/v1/traces",
		RunKey:           "build-id",
		CollectorToken:   "existing-upload-token",
		TokenSource:      staticToken("oidc-token"),
	})

	if !relay.authorized(`Token token="existing-upload-token"`) {
		t.Error("existing collector token was not accepted by the local relay")
	}
	if relay.authorized(`Token token="different-token"`) {
		t.Error("unexpected collector token was accepted by the local relay")
	}

	drainCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	relay.Drain(drainCtx)
}

func TestRelayEnvironmentUsesStandardVariablesAndPreservesResourceAttributes(t *testing.T) {
	relay := newTestRelay(t, Config{
		UpstreamEndpoint: "http://127.0.0.1:1/v1/traces",
		RunKey:           "build-id",
		TokenSource:      staticToken("oidc-token"),
	})
	environment := relay.Environment("service.name=example")

	if got := environment["OTEL_EXPORTER_OTLP_ENDPOINT"]; got != relay.endpoint {
		t.Errorf("OTEL_EXPORTER_OTLP_ENDPOINT = %q, want %q", got, relay.endpoint)
	}
	if got := environment["OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"]; got != relay.endpoint+"/v1/traces" {
		t.Errorf("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT = %q", got)
	}
	if got := environment["OTEL_EXPORTER_OTLP_TRACES_HEADERS"]; got != "authorization=Bearer%20"+relay.token {
		t.Errorf("OTEL_EXPORTER_OTLP_TRACES_HEADERS = %q", got)
	}
	if got := environment["OTEL_RESOURCE_ATTRIBUTES"]; got != "service.name=example,"+ResourceAttribute+"="+relay.endpoint+"/v1/traces" {
		t.Errorf("OTEL_RESOURCE_ATTRIBUTES = %q", got)
	}
	if got := environment["BUILDKITE_TESTS_OTLP_TOKEN"]; got != relay.token {
		t.Errorf("BUILDKITE_TESTS_OTLP_TOKEN does not contain the local token")
	}
	if _, ok := environment["BUILDKITE_ANALYTICS_TOKEN"]; ok {
		t.Error("relay environment overrides the collector upload token")
	}
	if strings.Contains(environment["OTEL_EXPORTER_OTLP_ENDPOINT"], relay.token) {
		t.Error("advertised endpoint contains local credentials")
	}

	drainCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	relay.Drain(drainCtx)
}

func TestTokenRefreshTimeUsesJWTExpiry(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	payload := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"exp":%d}`, now.Add(time.Hour).Unix())))
	token := "header." + payload + ".signature"

	got := tokenRefreshTime(token, now, 24*time.Hour)
	want := now.Add(55 * time.Minute)
	if !got.Equal(want) {
		t.Errorf("tokenRefreshTime() = %s, want %s", got, want)
	}
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	tests := []struct {
		value string
		want  time.Duration
		ok    bool
	}{
		{value: "12", want: 12 * time.Second, ok: true},
		{value: now.Add(20 * time.Second).UTC().Format(http.TimeFormat), want: 20 * time.Second, ok: true},
		{value: "invalid"},
	}
	for _, test := range tests {
		got, ok := parseRetryAfter(test.value, now)
		if got != test.want || ok != test.ok {
			t.Errorf("parseRetryAfter(%q) = (%s, %t), want (%s, %t)", test.value, got, ok, test.want, test.ok)
		}
	}
}

func newTestRelay(t *testing.T, config Config) *Relay {
	t.Helper()
	config.initialBackoff = time.Millisecond
	config.maxBackoff = 5 * time.Millisecond
	relay, err := New(config)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return relay
}

func staticToken(token string) func(context.Context) (string, error) {
	return func(context.Context) (string, error) { return token, nil }
}

func postTraces(t *testing.T, relay *Relay, body []byte, authorization, contentEncoding string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, relay.endpoint+"/v1/traces", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", authorization)
	req.Header.Set("Content-Type", "application/x-protobuf")
	if contentEncoding != "" {
		req.Header.Set("Content-Encoding", contentEncoding)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST local OTLP request: %v", err)
	}
	return resp
}
