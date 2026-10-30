package otlprelay

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/encoding/protowire"
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
	report := stripLatencyStats(t, relay.Drain(drainCtx))
	if !cmp.Equal(report, Report{ForwardedRequests: 1, ForwardedBytes: int64(len(body)), ForwardedSize: singleStat(int64(len(body)))}) {
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

func TestRelayForwardsSpanStreamHeader(t *testing.T) {
	tests := []struct {
		name       string
		spanStream []string
		want       []string
	}{
		{name: "test", spanStream: []string{"test"}, want: []string{"test"}},
		{name: "child", spanStream: []string{"child"}, want: []string{"child"}},
		{name: "absent"},
		{name: "empty", spanStream: []string{""}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			received := make(chan []string, 1)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				received <- req.Header.Values("Buildkite-Tests-Span-Stream")
				w.WriteHeader(http.StatusOK)
			}))
			defer upstream.Close()

			relay := newTestRelay(t, Config{
				UpstreamEndpoint: upstream.URL,
				RunKey:           "build-id",
				TokenSource:      staticToken("oidc-token"),
			})
			resp := postTraces(t, relay, []byte("spans"), `Token token="`+relay.token+`"`, "", test.spanStream...)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("POST /v1/traces status = %d, want 200", resp.StatusCode)
			}
			_ = resp.Body.Close()

			drainCtx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if report := relay.Drain(drainCtx); report.ForwardedRequests != 1 {
				t.Fatalf("Drain report = %+v, want one forwarded request", report)
			}
			if got := <-received; !slices.Equal(got, test.want) {
				t.Errorf("upstream Buildkite-Tests-Span-Stream = %q, want %q", got, test.want)
			}
		})
	}
}

func TestRelayFailsToStartWhenInitialCredentialFailureIsTerminal(t *testing.T) {
	_, err := New(Config{
		UpstreamEndpoint: "http://127.0.0.1:1/v1/traces",
		RunKey:           "build-id",
		TokenSource: func(context.Context) (string, error) {
			return "", fmt.Errorf("%w: audience not allowed", ErrTerminalCredential)
		},
	})
	if err == nil || !strings.Contains(err.Error(), "audience not allowed") {
		t.Fatalf("New() error = %v, want initial credential error", err)
	}
}

// A transient credential failure (endpoint outage, timeout) must not fail the
// job: the relay starts without a credential, buffers requests, and delivers
// them once the token source recovers.
func TestRelayStartsWithoutCredentialAndDeliversAfterRecovery(t *testing.T) {
	received := make(chan string, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		received <- req.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	var logMu sync.Mutex
	var logs strings.Builder
	var tokenRequests atomic.Int32
	relay := newTestRelay(t, Config{
		UpstreamEndpoint: upstream.URL,
		RunKey:           "build-id",
		TokenSource: func(context.Context) (string, error) {
			if tokenRequests.Add(1) < 3 {
				return "", fmt.Errorf("504 Gateway Timeout")
			}
			return "oidc-token", nil
		},
		Logf: func(format string, args ...any) {
			logMu.Lock()
			defer logMu.Unlock()
			fmt.Fprintf(&logs, format+"\n", args...)
		},
	})

	resp := postTraces(t, relay, []byte("spans"), "Bearer "+relay.token, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /v1/traces status = %d, want 200", resp.StatusCode)
	}
	_ = resp.Body.Close()

	drainCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	report := stripLatencyStats(t, relay.Drain(drainCtx))
	if !cmp.Equal(report, Report{ForwardedRequests: 1, ForwardedBytes: 5, ForwardedSize: singleStat[int64](5)}) {
		t.Errorf("Drain report = %+v, want one forwarded request", report)
	}

	if got := <-received; got != `Token token="oidc-token"` {
		t.Errorf("upstream Authorization = %q, want recovered token", got)
	}
	logMu.Lock()
	defer logMu.Unlock()
	if !strings.Contains(logs.String(), "starting without an upstream credential") {
		t.Errorf("logs = %q, want a startup warning about the missing credential", logs.String())
	}
}

// A seeded initial token (bktec's OIDC-minted collector upload token, same
// audience) is used as the upstream credential without calling TokenSource.
func TestRelaySeededInitialTokenSkipsTokenSource(t *testing.T) {
	received := make(chan string, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		received <- req.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	var tokenRequests atomic.Int32
	relay := newTestRelay(t, Config{
		UpstreamEndpoint: upstream.URL,
		RunKey:           "build-id",
		InitialToken:     "seeded-token",
		TokenSource: func(context.Context) (string, error) {
			tokenRequests.Add(1)
			return "minted-token", nil
		},
	})

	resp := postTraces(t, relay, []byte("spans"), "Bearer "+relay.token, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /v1/traces status = %d, want 200", resp.StatusCode)
	}
	_ = resp.Body.Close()

	drainCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	report := stripLatencyStats(t, relay.Drain(drainCtx))
	if !cmp.Equal(report, Report{ForwardedRequests: 1, ForwardedBytes: 5, ForwardedSize: singleStat[int64](5)}) {
		t.Errorf("Drain report = %+v, want one forwarded request", report)
	}

	if got := <-received; got != `Token token="seeded-token"` {
		t.Errorf("upstream Authorization = %q, want seeded token", got)
	}
	if got := tokenRequests.Load(); got != 0 {
		t.Errorf("TokenSource calls = %d, want 0 (seed should be used)", got)
	}
}

// Once a credential failure is known to be terminal, the relay stops calling
// TokenSource and drops requests as permanent failures instead of retrying.
func TestRelayStopsMintingAfterTerminalCredentialFailure(t *testing.T) {
	var tokenRequests atomic.Int32
	relay := newTestRelay(t, Config{
		UpstreamEndpoint: "http://127.0.0.1:1/v1/traces",
		RunKey:           "build-id",
		// Seed with an immediately expiring token so the first delivery needs a
		// refresh, which is terminally refused.
		InitialToken:  "seeded-token",
		TokenLifetime: time.Nanosecond,
		TokenSource: func(context.Context) (string, error) {
			tokenRequests.Add(1)
			return "", fmt.Errorf("%w: audience not allowed", ErrTerminalCredential)
		},
	})

	for range 2 {
		resp := postTraces(t, relay, []byte("spans"), "Bearer "+relay.token, "")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("POST /v1/traces status = %d, want 200", resp.StatusCode)
		}
		_ = resp.Body.Close()
	}

	drainCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	report := relay.Drain(drainCtx)
	if report.DroppedPermanentRequests != 2 {
		t.Errorf("Drain report = %+v, want two permanently dropped requests", report)
	}
	if got := tokenRequests.Load(); got != 1 {
		t.Errorf("TokenSource calls = %d, want 1 (terminal failures must not be retried)", got)
	}
}

func TestRelayAcknowledgesBeforeDeliveryAndReturns429WhenQueueIsFull(t *testing.T) {
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
		QueueCapacity:    len(body) + queueRequestOverhead,
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

	second := postTraces(t, relay, nil, "Bearer "+relay.token, "")
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
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		requestNumber := requests.Add(1)
		if got := req.Header.Get("Buildkite-Tests-Span-Stream"); got != "child" {
			t.Errorf("attempt %d Buildkite-Tests-Span-Stream = %q, want child", requestNumber, got)
		}
		if requestNumber == 1 {
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
	resp := postTraces(t, relay, []byte("request"), `Token token="`+relay.token+`"`, "", "child")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST status = %d, want 200", resp.StatusCode)
	}
	_ = resp.Body.Close()

	drainCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	report := stripLatencyStats(t, relay.Drain(drainCtx))
	if got := requests.Load(); got != 2 {
		t.Errorf("upstream requests = %d, want 2", got)
	}
	if !cmp.Equal(report, Report{ForwardedRequests: 1, ForwardedBytes: 7, ForwardedSize: singleStat[int64](7)}) {
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
	report := stripLatencyStats(t, relay.Drain(drainCtx))
	if !cmp.Equal(report, Report{ForwardedRequests: 1, ForwardedBytes: 7, ForwardedSize: singleStat[int64](7)}) {
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
	want := Report{DroppedPermanentRequests: 1, DroppedBytes: int64(len(body)), Refusals: []Refusal{{Status: 403, Requests: 1, UnlabelledRequests: 1}}}
	if !cmp.Equal(report, want) {
		t.Errorf("Drain report = %+v, want %+v", report, want)
	}
}

func TestRelayReportsDistinctRefusals(t *testing.T) {
	const planMessage = "OTel trace ingest is not available on your organization's plan"
	const capMessage = "OTel trace ingest limit reached for this billing period"
	const multilineMessage = "refused\n+++ injected group\r\x1b[2K\ttry again"
	// google.rpc.Status wire fixtures: code = PERMISSION_DENIED (7), message
	// = field 2. Keep the fixtures independent of the production decoder.
	planBody := append([]byte{0x08, 0x07, 0x12, byte(len(planMessage))}, planMessage...)
	capBody := append([]byte{0x08, 0x07, 0x12, byte(len(capMessage))}, capMessage...)
	multilineBody := append([]byte{0x08, 0x07, 0x12, byte(len(multilineMessage))}, multilineMessage...)
	cases := []struct {
		body    []byte
		status  int
		streams []string
		repeat  int
		want    Refusal
	}{
		{planBody, 403, []string{"test", "child", "", "TEST", "unknown"}, 20,
			Refusal{Status: 403, Message: planMessage, Requests: 100, TestRequests: 40, ChildRequests: 20, UnlabelledRequests: 40}},
		{capBody, 403, []string{"child"}, 2,
			Refusal{Status: 403, Message: capMessage, Requests: 2, ChildRequests: 2}},
		{planBody, 401, []string{"test"}, 3,
			Refusal{Status: 401, Message: planMessage, Requests: 3, TestRequests: 3}},
		{[]byte("<html>forbidden</html>"), 403, []string{""}, 4,
			Refusal{Status: 403, Requests: 4, UnlabelledRequests: 4}},
		{multilineBody, 403, []string{"test"}, 1,
			Refusal{Status: 403, Message: "refused +++ injected group  [2K try again", Requests: 1, TestRequests: 1}},
	}
	var requests atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		requests.Add(1)
		body, _ := io.ReadAll(req.Body)
		index := int(body[0])
		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(cases[index].status)
		_, _ = w.Write(cases[index].body)
	}))
	defer upstream.Close()
	var logs []string
	relay := newTestRelay(t, Config{
		UpstreamEndpoint: upstream.URL,
		RunKey:           "build-id",
		TokenSource:      staticToken("oidc-token"),
		Logf: func(format string, args ...any) {
			logs = append(logs, fmt.Sprintf(format, args...))
		},
	})
	var wantRefusals []Refusal
	for i, tc := range cases {
		wantRefusals = append(wantRefusals, tc.want)
		for range tc.repeat {
			for _, stream := range tc.streams {
				resp := postTraces(t, relay, []byte{byte(i)}, "Bearer "+relay.token, "", stream)
				_ = resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					t.Fatalf("local response = %d, want accepted request", resp.StatusCode)
				}
			}
		}
	}
	drainCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	report := relay.Drain(drainCtx)
	if diff := cmp.Diff(wantRefusals, report.Refusals); diff != "" {
		t.Errorf("refusals (-want +got):\n%s", diff)
	}
	if report.DroppedPermanentRequests != 110 || report.ForwardedRequests != 0 || requests.Load() != 110 {
		t.Errorf("report = %+v, upstream requests = %d, want 110 permanent drops without retries", report, requests.Load())
	}
	wantLogs := []string{
		"bktec OTLP relay: dropped a request after upstream returned HTTP 403: " + planMessage,
		"bktec OTLP relay: dropped a request after upstream returned HTTP 403: " + capMessage,
		"bktec OTLP relay: dropped a request after upstream returned HTTP 401: " + planMessage,
		"bktec OTLP relay: dropped a request after upstream returned HTTP 403",
		"bktec OTLP relay: dropped a request after upstream returned HTTP 403: refused +++ injected group  [2K try again",
	}
	if diff := cmp.Diff(wantLogs, logs); diff != "" {
		t.Errorf("logs (-want +got):\n%s", diff)
	}
	t.Logf("refusal output:\n%s", strings.Join(logs, "\n"))
}

func TestRelayBoundsDistinctRefusals(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		status := http.StatusForbidden
		if string(body) == "unauthorized" {
			status = http.StatusUnauthorized
		}
		w.WriteHeader(status)
		_, _ = w.Write(append([]byte{0x08, 0x07, 0x12, byte(len(body))}, body...))
	}))
	defer upstream.Close()
	var logs []string
	relay := newTestRelay(t, Config{
		UpstreamEndpoint: upstream.URL,
		RunKey:           "build-id",
		TokenSource:      staticToken("oidc-token"),
		Logf: func(format string, args ...any) {
			logs = append(logs, fmt.Sprintf(format, args...))
		},
	})
	send := func(message, stream string) {
		resp := postTraces(t, relay, []byte(message), "Bearer "+relay.token, "", stream)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("local response = %d, want accepted request", resp.StatusCode)
		}
	}
	var want []Refusal
	for i := range 32 {
		message := fmt.Sprintf("refused batch %d", i)
		send(message, "child")
		want = append(want, Refusal{Status: 403, Message: message, Requests: 1, ChildRequests: 1})
	}
	for i := range 128 {
		send(fmt.Sprintf("another refused batch %d", i), []string{"test", "child", "test", "unknown"}[i%4])
	}
	// A retained reason must still accumulate after the limit. A new HTTP
	// status must keep its own counts rather than being merged with 403s.
	send("refused batch 0", "test")
	send("unauthorized", "unknown")
	send("unauthorized", "unknown")
	want[0].Requests++
	want[0].TestRequests++
	want = append(want,
		Refusal{Status: 403, Message: "further distinct refusal messages omitted", Requests: 128, TestRequests: 64, ChildRequests: 32, UnlabelledRequests: 32},
		Refusal{Status: 401, Message: "further distinct refusal messages omitted", Requests: 2, UnlabelledRequests: 2},
	)
	drainCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	report := relay.Drain(drainCtx)
	if diff := cmp.Diff(want, report.Refusals); diff != "" {
		t.Errorf("refusals (-want +got):\n%s", diff)
	}
	if report.DroppedPermanentRequests != 163 || report.ForwardedRequests != 0 || report.DroppedDeadlineRequests != 0 {
		t.Errorf("report = %+v, want all 163 requests counted as permanent drops", report)
	}
	if len(logs) != 34 {
		t.Errorf("logged %d refusals, want 32 detailed reasons and 2 overflow statuses", len(logs))
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
	if !cmp.Equal(report, want) {
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
	if !cmp.Equal(report, want) {
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
	if !cmp.Equal(report, want) {
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
	if report := relay.Drain(drainCtx); !cmp.Equal(report, Report{}) {
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

	if _, ok := environment["OTEL_EXPORTER_OTLP_ENDPOINT"]; ok {
		t.Error("relay environment overrides the generic OTLP endpoint")
	}
	if _, ok := environment["OTEL_EXPORTER_OTLP_PROTOCOL"]; ok {
		t.Error("relay environment overrides the generic OTLP protocol")
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
	if strings.Contains(environment["OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"], relay.token) {
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

func TestSummarize(t *testing.T) {
	if got := summarize[int64](nil); got != (Stats[int64]{}) {
		t.Errorf("summarize(nil) = %+v, want zero stats", got)
	}
	samples := []int64{100, 10, 20, 30, 40, 50, 60, 70, 80, 90}
	want := Stats[int64]{P50: 50, P90: 90, Max: 100}
	if got := summarize(samples); got != want {
		t.Errorf("summarize = %+v, want %+v", got, want)
	}
	if samples[0] != 100 {
		t.Error("summarize mutated its input")
	}
}

func TestSamplerBoundsMemoryAndKeepsExactMax(t *testing.T) {
	var s sampler[int64]
	const n = 3 * maxStatSamples
	for value := int64(1); value <= n; value++ {
		s.observe(value)
	}
	if len(s.samples) != maxStatSamples {
		t.Errorf("len(samples) = %d, want capped at %d", len(s.samples), maxStatSamples)
	}
	stats := s.stats()
	if stats.Max != n {
		t.Errorf("Max = %d, want exact %d", stats.Max, n)
	}
	if stats.P50 < 1 || stats.P50 > n || stats.P50 > stats.P90 || stats.P90 > stats.Max {
		t.Errorf("stats = %+v, want ordered estimates within observed range", stats)
	}
}

// singleStat is the expected distribution when exactly one value was observed.
func singleStat[T ~int64](value T) Stats[T] {
	return Stats[T]{P50: value, P90: value, Max: value}
}

// stripLatencyStats checks that latency stats are present and ordered exactly
// when requests were forwarded, then zeroes them so callers can compare the
// rest of the report exactly (latency values are nondeterministic).
func stripLatencyStats(t *testing.T, report Report) Report {
	t.Helper()
	latency := report.ForwardedLatency
	if report.ForwardedRequests > 0 {
		if latency.Max <= 0 || latency.P50 > latency.P90 || latency.P90 > latency.Max {
			t.Errorf("ForwardedLatency = %+v, want ordered positive stats", latency)
		}
	} else if latency != (Stats[time.Duration]{}) {
		t.Errorf("ForwardedLatency = %+v, want zero stats without forwarded requests", latency)
	}
	report.ForwardedLatency = Stats[time.Duration]{}
	return report
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

func postTraces(t *testing.T, relay *Relay, body []byte, authorization, contentEncoding string, spanStream ...string) *http.Response {
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
	for _, value := range spanStream {
		req.Header.Add("Buildkite-Tests-Span-Stream", value)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST local OTLP request: %v", err)
	}
	return resp
}

func TestRelayBatchingPacksRequestsByteExactInOrder(t *testing.T) {
	endpoint, uploads := recordingUpstream(t, nil)
	relay := newTestRelay(t, Config{
		UpstreamEndpoint:     endpoint,
		RunKey:               "build-id",
		TokenSource:          staticToken("oidc-token"),
		ExperimentalBatching: true,
		maxLinger:            time.Hour,
	})
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	_, _ = writer.Write([]byte("opaque protobuf spans"))
	_ = writer.Close()
	posts := []envelopeItem{
		{body: compressed.String(), contentEncoding: "gzip"},
		{body: "plain\x00spans\xff"},
		{},
		{body: "third", contentEncoding: "gzip"},
	}
	var acceptedBytes int64
	for _, post := range posts {
		mustPostTraces(t, relay, []byte(post.body), post.contentEncoding, "test")
		acceptedBytes += int64(len(post.body))
	}

	drainCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	report := relay.Drain(drainCtx)
	if report.ForwardedRequests != 4 || report.ForwardedBytes != acceptedBytes || report.UpstreamUploads != 1 || report.DroppedBytes != 0 {
		t.Errorf("Drain report = %+v, want 4 requests (%d bytes) forwarded in 1 upload", report, acceptedBytes)
	}
	got := receivedUploads(uploads)
	if len(got) != 1 {
		t.Fatalf("upstream uploads = %d, want 1", len(got))
	}
	upload := got[0]
	if upload.path != "/v1/traces" {
		t.Errorf("upstream path = %q, want /v1/traces", upload.path)
	}
	if upload.contentType != "application/x-buildkite-otlp-batch+protobuf" {
		t.Errorf("upstream Content-Type = %q", upload.contentType)
	}
	if upload.contentEncoding != nil {
		t.Errorf("upstream Content-Encoding = %q, want none", upload.contentEncoding)
	}
	if upload.authorization != `Token token="oidc-token"` || upload.runKey != "build-id" {
		t.Errorf("upstream Authorization = %q, Buildkite-Tests-Run-Key = %q", upload.authorization, upload.runKey)
	}
	if !slices.Equal(upload.spanStream, []string{"test"}) {
		t.Errorf("upstream Buildkite-Tests-Span-Stream = %q, want test", upload.spanStream)
	}
	if diff := cmp.Diff(posts, decodeEnvelope(t, upload.body), cmp.AllowUnexported(envelopeItem{})); diff != "" {
		t.Errorf("envelope items (-want +got):\n%s", diff)
	}
	// As in proto3, empty fields are omitted: the identity item has no
	// content_encoding and the empty item is an empty TraceRequest.
	want := []byte{0x0a, byte(compressed.Len() + 8), 0x0a, byte(compressed.Len())}
	want = append(want, compressed.Bytes()...)
	want = append(want, 0x12, 4, 'g', 'z', 'i', 'p')
	want = append(want, 0x0a, 14, 0x0a, 12, 'p', 'l', 'a', 'i', 'n', 0x00, 's', 'p', 'a', 'n', 's', 0xff)
	want = append(want, 0x0a, 0)
	want = append(want, 0x0a, 13, 0x0a, 5, 't', 'h', 'i', 'r', 'd', 0x12, 4, 'g', 'z', 'i', 'p')
	if !bytes.Equal(upload.body, want) {
		t.Errorf("envelope = %x, want %x", upload.body, want)
	}
}

func TestRelayBatchingKeepsSpanStreamsInSeparateEnvelopes(t *testing.T) {
	endpoint, uploads := recordingUpstream(t, nil)
	relay := newTestRelay(t, Config{
		UpstreamEndpoint:     endpoint,
		RunKey:               "build-id",
		TokenSource:          staticToken("oidc-token"),
		ExperimentalBatching: true,
		maxLinger:            time.Hour,
	})
	for _, post := range []struct{ body, stream string }{
		{"test 1", "test"}, {"child 1", "child"}, {"test 2", "test"},
		{"unlabelled 1", ""}, {"child 2", "child"}, {"test 3", "test"},
	} {
		mustPostTraces(t, relay, []byte(post.body), "", post.stream)
	}

	drainCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if report := relay.Drain(drainCtx); report.ForwardedRequests != 6 || report.UpstreamUploads != 3 {
		t.Errorf("Drain report = %+v, want 6 requests forwarded in 3 uploads", report)
	}
	want := []struct {
		stream []string
		bodies []string
	}{
		{[]string{"test"}, []string{"test 1", "test 2", "test 3"}},
		{[]string{"child"}, []string{"child 1", "child 2"}},
		{nil, []string{"unlabelled 1"}},
	}
	got := receivedUploads(uploads)
	if len(got) != len(want) {
		t.Fatalf("upstream uploads = %d, want %d", len(got), len(want))
	}
	for i, upload := range got {
		if !slices.Equal(upload.spanStream, want[i].stream) {
			t.Errorf("upload %d Buildkite-Tests-Span-Stream = %q, want %q", i, upload.spanStream, want[i].stream)
		}
		if bodies := itemBodies(decodeEnvelope(t, upload.body)); !slices.Equal(bodies, want[i].bodies) {
			t.Errorf("upload %d items = %q, want %q", i, bodies, want[i].bodies)
		}
	}
}

func TestRelayBatchingCapsRequestsAndEncodedBytes(t *testing.T) {
	// A 9-byte gzip item encodes as 19 envelope bytes: a 2-byte requests
	// field header around an 11-byte body field and a 6-byte encoding field.
	const itemBytes = 19
	tests := []struct {
		name        string
		maxRequests int
		maxBytes    int
		bodyBytes   int
		itemBytes   int
		posts       int
		wantItems   []int
	}{
		{name: "request cap", maxRequests: 3, posts: 7, wantItems: []int{3, 3, 1}},
		{name: "bytes reach the cap exactly", maxBytes: 2 * itemBytes, posts: 3, wantItems: []int{2, 1}},
		{name: "one more item would cross the cap", maxBytes: 2*itemBytes - 1, posts: 3, wantItems: []int{1, 1, 1}},
		{name: "one item fills the cap", maxBytes: itemBytes, posts: 2, wantItems: []int{1, 1}},
		// A 120-byte gzip body has a 128-byte inner message, so its outer
		// length needs two bytes: 1 + 2 + 128 = 131 bytes per item.
		{name: "two-byte message length", bodyBytes: 120, itemBytes: 131, maxBytes: 261, posts: 3, wantItems: []int{1, 1, 1}},
		// At 128 body bytes both length prefixes need two bytes:
		// 1 + 2 + (1 + 2 + 128 + 6) = 140 bytes per item.
		{name: "two-byte body length at exact cap", bodyBytes: 128, itemBytes: 140, maxBytes: 280, posts: 3, wantItems: []int{2, 1}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			encodedItemBytes := itemBytes
			if test.itemBytes != 0 {
				encodedItemBytes = test.itemBytes
			}
			endpoint, uploads := recordingUpstream(t, nil)
			relay := newTestRelay(t, Config{
				UpstreamEndpoint:     endpoint,
				RunKey:               "build-id",
				TokenSource:          staticToken("oidc-token"),
				ExperimentalBatching: true,
				maxLinger:            time.Hour,
				maxBatchRequests:     test.maxRequests,
				maxBatchBytes:        test.maxBytes,
			})
			var wantBodies []string
			for i := range test.posts {
				body := fmt.Sprintf("request-%d", i)
				if test.bodyBytes > len(body) {
					body += strings.Repeat("x", test.bodyBytes-len(body))
				}
				mustPostTraces(t, relay, []byte(body), "gzip", "test")
				wantBodies = append(wantBodies, body)
			}

			drainCtx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if report := relay.Drain(drainCtx); report.ForwardedRequests != test.posts || report.UpstreamUploads != len(test.wantItems) {
				t.Errorf("Drain report = %+v, want %d requests forwarded in %d uploads", report, test.posts, len(test.wantItems))
			}
			var gotItems []int
			var gotBodies []string
			for _, upload := range receivedUploads(uploads) {
				if upload.path != "/v1/traces" {
					t.Errorf("upstream path = %q, want /v1/traces", upload.path)
				}
				items := decodeEnvelope(t, upload.body)
				if len(upload.body) != len(items)*encodedItemBytes {
					t.Errorf("envelope is %d bytes, want %d for %d items", len(upload.body), len(items)*encodedItemBytes, len(items))
				}
				if test.maxBytes > 0 && len(upload.body) > test.maxBytes {
					t.Errorf("envelope is %d bytes, over the %d-byte cap", len(upload.body), test.maxBytes)
				}
				gotItems = append(gotItems, len(items))
				gotBodies = append(gotBodies, itemBodies(items)...)
			}
			if !slices.Equal(gotItems, test.wantItems) {
				t.Errorf("items per envelope = %v, want %v", gotItems, test.wantItems)
			}
			if !slices.Equal(gotBodies, wantBodies) {
				t.Errorf("forwarded items = %q, want %q", gotBodies, wantBodies)
			}
		})
	}
}

func TestRelayBatchingSendsOversizedRequestAlone(t *testing.T) {
	endpoint, uploads := recordingUpstream(t, nil)
	relay := newTestRelay(t, Config{
		UpstreamEndpoint:     endpoint,
		RunKey:               "build-id",
		TokenSource:          staticToken("oidc-token"),
		ExperimentalBatching: true,
		maxLinger:            time.Hour,
		// Fits a 9-byte gzip item (19 envelope bytes) but not a 10-byte one.
		maxBatchBytes: 19,
	})
	mustPostTraces(t, relay, []byte("before-01"), "gzip", "test")
	mustPostTraces(t, relay, []byte("oversized!"), "gzip", "test")
	mustPostTraces(t, relay, []byte("after-001"), "gzip", "test")

	drainCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if report := relay.Drain(drainCtx); report.ForwardedRequests != 3 || report.UpstreamUploads != 3 {
		t.Errorf("Drain report = %+v, want 3 requests forwarded in 3 uploads", report)
	}
	got := receivedUploads(uploads)
	if len(got) != 3 {
		t.Fatalf("upstream uploads = %d, want 3", len(got))
	}
	for i, want := range []string{"before-01", "after-001"} {
		upload := got[2*i]
		if upload.path != "/v1/traces" || upload.contentType != "application/x-buildkite-otlp-batch+protobuf" || !slices.Equal(itemBodies(decodeEnvelope(t, upload.body)), []string{want}) {
			t.Errorf("upload %d = %s %q, want an envelope with %q", 2*i, upload.path, upload.body, want)
		}
	}
	single := got[1]
	if single.path != "/v1/traces" || string(single.body) != "oversized!" {
		t.Errorf("oversized upload = %s %q, want the original body at /v1/traces", single.path, single.body)
	}
	if single.contentType != "application/x-protobuf" || !slices.Equal(single.contentEncoding, []string{"gzip"}) || !slices.Equal(single.spanStream, []string{"test"}) {
		t.Errorf("oversized upload Content-Type = %q, Content-Encoding = %q, Buildkite-Tests-Span-Stream = %q, want its original headers",
			single.contentType, single.contentEncoding, single.spanStream)
	}
	if single.authorization != `Token token="oidc-token"` || single.runKey != "build-id" {
		t.Errorf("oversized upload Authorization = %q, Buildkite-Tests-Run-Key = %q", single.authorization, single.runKey)
	}
}

func TestRelayBatchingLingersForMoreRequests(t *testing.T) {
	const linger = 250 * time.Millisecond
	endpoint, uploads := recordingUpstream(t, nil)
	relay := newTestRelay(t, Config{
		UpstreamEndpoint:     endpoint,
		RunKey:               "build-id",
		TokenSource:          staticToken("oidc-token"),
		ExperimentalBatching: true,
		maxLinger:            linger,
	})
	start := time.Now()
	mustPostTraces(t, relay, []byte("first"), "", "test")
	mustPostTraces(t, relay, []byte("second"), "", "test")

	upload := nextUpload(t, uploads)
	if elapsed := upload.at.Sub(start); elapsed < linger {
		t.Errorf("batch sent %s after its first request, want it to linger for %s", elapsed, linger)
	}
	if bodies := itemBodies(decodeEnvelope(t, upload.body)); !slices.Equal(bodies, []string{"first", "second"}) {
		t.Errorf("envelope items = %q, want both requests", bodies)
	}

	drainCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if report := relay.Drain(drainCtx); report.ForwardedRequests != 2 || report.UpstreamUploads != 1 {
		t.Errorf("Drain report = %+v, want 2 requests forwarded in 1 upload", report)
	}
}

func TestRelayBatchingSendsLoneRequestAfterDefaultLinger(t *testing.T) {
	endpoint, uploads := recordingUpstream(t, nil)
	relay := newTestRelay(t, Config{
		UpstreamEndpoint:     endpoint,
		RunKey:               "build-id",
		TokenSource:          staticToken("oidc-token"),
		ExperimentalBatching: true,
	})
	start := time.Now()
	mustPostTraces(t, relay, []byte("lone"), "gzip", "child")

	upload := nextUpload(t, uploads)
	if elapsed := upload.at.Sub(start); elapsed < 25*time.Millisecond || elapsed > 500*time.Millisecond {
		t.Errorf("lone request sent after %s, want about the 25ms linger", elapsed)
	}
	if upload.path != "/v1/traces" || !slices.Equal(upload.spanStream, []string{"child"}) {
		t.Errorf("lone request upload = %s with stream %q, want a child envelope", upload.path, upload.spanStream)
	}
	want := []envelopeItem{{body: "lone", contentEncoding: "gzip"}}
	if diff := cmp.Diff(want, decodeEnvelope(t, upload.body), cmp.AllowUnexported(envelopeItem{})); diff != "" {
		t.Errorf("envelope items (-want +got):\n%s", diff)
	}

	drainCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	relay.Drain(drainCtx)
}

func TestRelayBatchingDoesNotLingerAgainAfterInFlightUpload(t *testing.T) {
	const linger = 500 * time.Millisecond
	release := make(chan struct{})
	endpoint, uploads := recordingUpstream(t, func(w http.ResponseWriter, attempt int) {
		if attempt == 1 {
			<-release
		}
		w.WriteHeader(http.StatusOK)
	})
	relay := newTestRelay(t, Config{
		UpstreamEndpoint:     endpoint,
		RunKey:               "build-id",
		TokenSource:          staticToken("oidc-token"),
		ExperimentalBatching: true,
		maxLinger:            linger,
		maxBatchRequests:     2,
	})
	mustPostTraces(t, relay, []byte("first"), "", "test")
	mustPostTraces(t, relay, []byte("second"), "", "test")
	nextUpload(t, uploads)
	mustPostTraces(t, relay, []byte("queued during upload"), "", "test")
	time.Sleep(linger + 50*time.Millisecond)
	start := time.Now()
	close(release)
	got := nextUpload(t, uploads)
	if elapsed := got.at.Sub(start); elapsed > linger/2 {
		t.Errorf("aged request waited %s after upload finished, want no new linger", elapsed)
	}
	if bodies := itemBodies(decodeEnvelope(t, got.body)); !slices.Equal(bodies, []string{"queued during upload"}) {
		t.Errorf("aged upload items = %q", bodies)
	}
	drainCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if report := relay.Drain(drainCtx); report.ForwardedRequests != 3 || report.UpstreamUploads != 2 {
		t.Errorf("Drain report = %+v, want 3 requests in 2 uploads", report)
	}
}

func TestRelayBatchingDrainFlushesWithoutLinger(t *testing.T) {
	endpoint, uploads := recordingUpstream(t, nil)
	relay := newTestRelay(t, Config{
		UpstreamEndpoint:     endpoint,
		RunKey:               "build-id",
		TokenSource:          staticToken("oidc-token"),
		ExperimentalBatching: true,
		maxLinger:            time.Hour,
	})
	mustPostTraces(t, relay, []byte("first"), "", "test")
	mustPostTraces(t, relay, []byte("second"), "", "test")
	time.Sleep(50 * time.Millisecond)
	if got := receivedUploads(uploads); len(got) != 0 {
		t.Fatalf("upstream uploads before drain = %d, want the batch to linger", len(got))
	}

	start := time.Now()
	drainCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	report := relay.Drain(drainCtx)
	if took := time.Since(start); took > time.Second {
		t.Errorf("Drain took %s, want it to flush without waiting for the linger", took)
	}
	if report.ForwardedRequests != 2 || report.UpstreamUploads != 1 || report.DroppedDeadlineRequests != 0 {
		t.Errorf("Drain report = %+v, want 2 requests forwarded in 1 upload", report)
	}
	got := receivedUploads(uploads)
	if len(got) != 1 || !slices.Equal(itemBodies(decodeEnvelope(t, got[0].body)), []string{"first", "second"}) {
		t.Errorf("upstream uploads = %+v, want one envelope with both requests", got)
	}
}

func TestRelayBatchingRetriesIdenticalEnvelopeAndHoldsCapacity(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	endpoint, uploads := recordingUpstream(t, func(w http.ResponseWriter, attempt int) {
		switch attempt {
		case 1:
			close(started)
			<-release
			w.WriteHeader(http.StatusServiceUnavailable)
		case 2:
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
		default:
			w.WriteHeader(http.StatusOK)
		}
	})
	body := []byte("request")
	relay := newTestRelay(t, Config{
		UpstreamEndpoint:     endpoint,
		RunKey:               "build-id",
		QueueCapacity:        2 * (len(body) + queueRequestOverhead),
		TokenSource:          staticToken("oidc-token"),
		ExperimentalBatching: true,
		maxLinger:            time.Hour,
		maxBatchRequests:     2,
	})
	mustPostTraces(t, relay, body, "gzip", "test")
	mustPostTraces(t, relay, body, "", "test")
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("full batch was not sent")
	}
	// Members keep their queue capacity until the envelope succeeds.
	resp := postTraces(t, relay, body, "Bearer "+relay.token, "", "test")
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("POST during delivery status = %d, want 429", resp.StatusCode)
	}
	close(release)

	deadline := time.Now().Add(time.Second)
	for {
		resp := postTraces(t, relay, body, "Bearer "+relay.token, "", "test")
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("POST after delivery status = %d, want capacity released", resp.StatusCode)
		}
		time.Sleep(time.Millisecond)
	}

	drainCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	report := relay.Drain(drainCtx)
	if report.ForwardedRequests != 3 || report.UpstreamUploads != 4 || report.DroppedBytes != 0 {
		t.Errorf("Drain report = %+v, want 3 requests forwarded with 4 upload attempts", report)
	}
	got := receivedUploads(uploads)
	if len(got) != 4 {
		t.Fatalf("upstream attempts = %d, want 3 for the retried envelope and 1 after", len(got))
	}
	for i, retry := range got[1:3] {
		if !bytes.Equal(retry.body, got[0].body) || retry.path != got[0].path || retry.contentType != got[0].contentType || !slices.Equal(retry.spanStream, got[0].spanStream) {
			t.Errorf("retry %d = %s %q %x, want the identical envelope %s %q %x", i+1, retry.path, retry.contentType, retry.body, got[0].path, got[0].contentType, got[0].body)
		}
	}
	want := []envelopeItem{{body: "request", contentEncoding: "gzip"}, {body: "request"}}
	if diff := cmp.Diff(want, decodeEnvelope(t, got[0].body), cmp.AllowUnexported(envelopeItem{})); diff != "" {
		t.Errorf("retried envelope items (-want +got):\n%s", diff)
	}
}

func TestRelayBatchingDeadlineCountsInFlightAndQueuedMembers(t *testing.T) {
	endpoint, uploads := recordingUpstream(t, func(w http.ResponseWriter, _ int) {
		w.Header().Set("Retry-After", "3600")
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	relay := newTestRelay(t, Config{
		UpstreamEndpoint:     endpoint,
		RunKey:               "build-id",
		TokenSource:          staticToken("oidc-token"),
		ExperimentalBatching: true,
		maxLinger:            time.Hour,
		maxBatchRequests:     2,
	})
	for range 4 {
		mustPostTraces(t, relay, []byte("queued"), "", "test")
	}
	nextUpload(t, uploads)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	report := relay.Drain(ctx)
	if report.ForwardedRequests != 0 || report.DroppedPermanentRequests != 0 || report.DroppedDeadlineRequests != 4 || report.DroppedBytes != 24 {
		t.Errorf("Drain report = %+v, want all 4 original requests (24 bytes) dropped at deadline", report)
	}
}

func TestRelayBatchingRefusalDropsEveryMemberPerStream(t *testing.T) {
	const message = "OTel trace ingest is not available on your organization's plan"
	endpoint, uploads := recordingUpstream(t, func(w http.ResponseWriter, _ int) {
		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write(append([]byte{0x08, 0x07, 0x12, byte(len(message))}, message...))
	})
	var logs []string
	relay := newTestRelay(t, Config{
		UpstreamEndpoint:     endpoint,
		RunKey:               "build-id",
		TokenSource:          staticToken("oidc-token"),
		ExperimentalBatching: true,
		maxLinger:            time.Hour,
		Logf: func(format string, args ...any) {
			logs = append(logs, fmt.Sprintf(format, args...))
		},
	})
	var acceptedBytes int64
	for _, post := range []struct{ body, stream string }{
		{"test 1", "test"}, {"child 1", "child"}, {"test 2", "test"},
		{"unlabelled 1", ""}, {"child 2", "child"}, {"test 3", "test"},
	} {
		mustPostTraces(t, relay, []byte(post.body), "", post.stream)
		acceptedBytes += int64(len(post.body))
	}

	drainCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	report := relay.Drain(drainCtx)
	want := Report{
		Batching:                 true,
		UpstreamUploads:          3,
		DroppedPermanentRequests: 6,
		DroppedBytes:             acceptedBytes,
		Refusals:                 []Refusal{{Status: 403, Message: message, Requests: 6, TestRequests: 3, ChildRequests: 2, UnlabelledRequests: 1}},
	}
	if diff := cmp.Diff(want, report); diff != "" {
		t.Errorf("Drain report (-want +got):\n%s", diff)
	}
	if got := receivedUploads(uploads); len(got) != 3 {
		t.Errorf("upstream attempts = %d, want one per stream without retries", len(got))
	}
	wantLogs := []string{"bktec OTLP relay: dropped 3 requests after upstream returned HTTP 403: " + message}
	if diff := cmp.Diff(wantLogs, logs); diff != "" {
		t.Errorf("logs (-want +got):\n%s", diff)
	}
}

func TestRelayWithoutBatchingForwardsEachRequestAlone(t *testing.T) {
	endpoint, uploads := recordingUpstream(t, nil)
	relay := newTestRelay(t, Config{
		UpstreamEndpoint: endpoint,
		RunKey:           "build-id",
		TokenSource:      staticToken("oidc-token"),
	})
	posts := []struct{ body, encoding, stream string }{
		{"first", "gzip", "test"}, {"second", "", "test"}, {"third", "gzip", "child"},
	}
	for _, post := range posts {
		mustPostTraces(t, relay, []byte(post.body), post.encoding, post.stream)
	}

	drainCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if report := relay.Drain(drainCtx); report.ForwardedRequests != 3 || report.UpstreamUploads != 0 {
		t.Errorf("Drain report = %+v, want 3 requests forwarded without upload counts", report)
	}
	got := receivedUploads(uploads)
	if len(got) != len(posts) {
		t.Fatalf("upstream uploads = %d, want %d", len(got), len(posts))
	}
	for i, post := range posts {
		upload := got[i]
		var wantEncoding []string
		if post.encoding != "" {
			wantEncoding = []string{post.encoding}
		}
		if upload.path != "/v1/traces" || upload.contentType != "application/x-protobuf" || string(upload.body) != post.body ||
			!slices.Equal(upload.contentEncoding, wantEncoding) || !slices.Equal(upload.spanStream, []string{post.stream}) {
			t.Errorf("upload %d = %s %q %q (encoding %q, stream %q), want request %+v unchanged",
				i, upload.path, upload.contentType, upload.body, upload.contentEncoding, upload.spanStream, post)
		}
	}
}

type upstreamUpload struct {
	path            string
	contentType     string
	contentEncoding []string
	spanStream      []string
	authorization   string
	runKey          string
	body            []byte
	at              time.Time
}

// recordingUpstream starts a fake upstream that records each POST, then lets
// respond write the response for that attempt (counted from 1), or returns
// 200 when respond is nil. It returns the upstream OTLP traces endpoint.
func recordingUpstream(t *testing.T, respond func(w http.ResponseWriter, attempt int)) (string, chan upstreamUpload) {
	t.Helper()
	uploads := make(chan upstreamUpload, 100)
	var attempts atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		at := time.Now()
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Errorf("read upstream request: %v", err)
		}
		uploads <- upstreamUpload{
			path:            req.URL.Path,
			contentType:     req.Header.Get("Content-Type"),
			contentEncoding: req.Header.Values("Content-Encoding"),
			spanStream:      req.Header.Values("Buildkite-Tests-Span-Stream"),
			authorization:   req.Header.Get("Authorization"),
			runKey:          req.Header.Get("Buildkite-Tests-Run-Key"),
			body:            body,
			at:              at,
		}
		if respond == nil {
			w.WriteHeader(http.StatusOK)
			return
		}
		respond(w, int(attempts.Add(1)))
	}))
	t.Cleanup(upstream.Close)
	return upstream.URL + "/v1/traces", uploads
}

// receivedUploads returns the uploads recorded so far.
func receivedUploads(uploads chan upstreamUpload) []upstreamUpload {
	var got []upstreamUpload
	for {
		select {
		case upload := <-uploads:
			got = append(got, upload)
		default:
			return got
		}
	}
}

func nextUpload(t *testing.T, uploads chan upstreamUpload) upstreamUpload {
	t.Helper()
	select {
	case upload := <-uploads:
		return upload
	case <-time.After(3 * time.Second):
		t.Fatal("upstream upload did not arrive")
		return upstreamUpload{}
	}
}

type envelopeItem struct {
	body            string
	contentEncoding string
}

// decodeEnvelope decodes a TraceRequestBatch: repeated TraceRequest requests
// = 1, where TraceRequest has bytes body = 1 and string content_encoding = 2.
func decodeEnvelope(t *testing.T, envelope []byte) []envelopeItem {
	t.Helper()
	consumeBytesField := func(b []byte) (protowire.Number, []byte, int) {
		number, wireType, n := protowire.ConsumeTag(b)
		if n < 0 || wireType != protowire.BytesType {
			t.Fatalf("envelope has an invalid tag or wire type %d: %x", wireType, b)
		}
		value, m := protowire.ConsumeBytes(b[n:])
		if m < 0 {
			t.Fatalf("envelope has a truncated field %d: %x", number, b)
		}
		return number, value, n + m
	}
	var items []envelopeItem
	for len(envelope) > 0 {
		number, encoded, n := consumeBytesField(envelope)
		envelope = envelope[n:]
		if number != 1 {
			t.Fatalf("envelope field %d, want requests (1)", number)
		}
		var item envelopeItem
		for len(encoded) > 0 {
			number, value, n := consumeBytesField(encoded)
			encoded = encoded[n:]
			switch number {
			case 1:
				item.body = string(value)
			case 2:
				item.contentEncoding = string(value)
			default:
				t.Fatalf("TraceRequest field %d, want body (1) or content_encoding (2)", number)
			}
		}
		items = append(items, item)
	}
	return items
}

func itemBodies(items []envelopeItem) []string {
	var bodies []string
	for _, item := range items {
		bodies = append(bodies, item.body)
	}
	return bodies
}

func mustPostTraces(t *testing.T, relay *Relay, body []byte, contentEncoding, spanStream string) {
	t.Helper()
	resp := postTraces(t, relay, body, "Bearer "+relay.token, contentEncoding, spanStream)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /v1/traces status = %d, want 200", resp.StatusCode)
	}
}
