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

func TestRelayBatchesRequests(t *testing.T) {
	// ExportTraceServiceRequest.resource_spans -> ResourceSpans.scope_spans
	// -> ScopeSpans.spans -> Span.name. Each request has a distinct test span.
	trace := func(name string) []byte {
		field := func(number protowire.Number, value []byte) []byte {
			return protowire.AppendBytes(protowire.AppendTag(nil, number, protowire.BytesType), value)
		}
		return field(1, field(2, field(2, field(5, []byte(name)))))
	}
	a, b, c := trace("first test"), trace("second test"), trace("third test")
	join := func(parts ...[]byte) []byte { return bytes.Join(parts, nil) }
	large := trace(strings.Repeat("x", 300*1024))
	// Four length-delimited fields contribute 16 bytes at this size.
	half := trace(strings.Repeat("z", 256*1024-16))
	oversized := trace(strings.Repeat("y", 600*1024))
	tests := []struct {
		name         string
		bodies       [][]byte
		streams      []string
		gzip         bool
		statuses     []int
		wantBodies   [][]byte
		wantStream   []string
		wantEncoding []string
		wait         bool
		maxRequests  int
	}{
		{name: "drain", bodies: [][]byte{a, b, c}, wantBodies: [][]byte{join(a, b, c)}},
		{name: "mixed encoding", bodies: [][]byte{a, b, c}, gzip: true, wantBodies: [][]byte{join(a, b, c)}},
		{name: "size cap", bodies: [][]byte{large, large, c}, gzip: true, wantBodies: [][]byte{large, join(large, c)}, wantEncoding: []string{"", "gzip"}, wait: true},
		{name: "exact size cap", bodies: [][]byte{half, half, c}, wantBodies: [][]byte{join(half, half), c}, wantEncoding: []string{"gzip", ""}, wait: true},
		{name: "oversized alone", bodies: [][]byte{a, oversized, c}, wantBodies: [][]byte{a, oversized, c}, wantEncoding: []string{"", "", ""}},
		{name: "gzip oversized alone", bodies: [][]byte{a, oversized, c}, gzip: true, wantBodies: [][]byte{a, oversized, c}, wantEncoding: []string{"", "gzip", ""}},
		{name: "streams", bodies: [][]byte{a, b, c}, gzip: true, streams: []string{"test", "child", "test"}, wantBodies: [][]byte{join(a, c), b}, wantStream: []string{"test", "child"}},
		{name: "child unmerged", bodies: [][]byte{a, b}, streams: []string{"child", "child"}, wantBodies: [][]byte{a, b}, wantStream: []string{"child", "child"}, wantEncoding: []string{"", ""}},
		{name: "unlabelled unmerged", bodies: [][]byte{a, b}, streams: []string{"", ""}, wantBodies: [][]byte{a, b}, wantStream: []string{"", ""}, wantEncoding: []string{"", ""}},
		{name: "retry whole batch", bodies: [][]byte{a, b, c}, statuses: []int{429, 503, 200}, wantBodies: [][]byte{join(a, b, c), join(a, b, c), join(a, b, c)}},
		{name: "refuse whole batch", bodies: [][]byte{a, b, c}, statuses: []int{403}, wantBodies: [][]byte{join(a, b, c)}},
		{name: "many small requests", bodies: slices.Repeat([][]byte{a}, 101), wantBodies: [][]byte{bytes.Repeat(a, 101)}},
		{name: "request cap", maxRequests: 3, bodies: slices.Repeat([][]byte{a}, 4), wantBodies: [][]byte{bytes.Repeat(a, 3), a}, wantEncoding: []string{"gzip", ""}, wait: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			type receivedBatch struct {
				body     []byte
				stream   string
				encoding string
				at       time.Time
			}
			received := make(chan receivedBatch, 200)
			var attempts atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				wireBody, _ := io.ReadAll(req.Body)
				wireReader := bytes.NewReader(wireBody)
				var reader io.Reader = wireReader
				if req.Header.Get("Content-Encoding") == "gzip" {
					gz, err := gzip.NewReader(wireReader)
					if err != nil {
						t.Error(err)
						w.WriteHeader(400)
						return
					}
					defer gz.Close()
					gz.Multistream(false)
					reader = gz
				}
				body, err := io.ReadAll(reader)
				if err != nil {
					t.Error(err)
				}
				if wireReader.Len() != 0 {
					t.Error("upstream body has trailing bytes or multiple gzip members")
				}
				if len(wireBody) > maxRequestBytes {
					t.Errorf("upstream body is %d bytes, exceeds receiver limit", len(wireBody))
				}
				received <- receivedBatch{body, req.Header.Get("Buildkite-Tests-Span-Stream"), req.Header.Get("Content-Encoding"), time.Now()}
				attempt := int(attempts.Add(1)) - 1
				status := http.StatusOK
				if attempt < len(tc.statuses) {
					status = tc.statuses[attempt]
				}
				w.Header().Set("Retry-After", "0")
				w.WriteHeader(status)
			}))
			defer upstream.Close()
			relay := newTestRelay(t, Config{
				UpstreamEndpoint: upstream.URL,
				RunKey:           "build-id",
				TokenSource:      staticToken("oidc-token"),
				batchWindow:      time.Second,
				maxBatchRequests: tc.maxRequests,
			})
			start := time.Now()
			var acceptedBytes int64
			for i, body := range tc.bodies {
				encoding := ""
				if tc.gzip && i%2 == 1 {
					var compressed bytes.Buffer
					writer := gzip.NewWriter(&compressed)
					_, _ = writer.Write(body)
					_ = writer.Close()
					body = compressed.Bytes()
					encoding = "gzip"
				}
				stream := "test"
				if len(tc.streams) > 0 {
					stream = tc.streams[i]
				}
				resp := postTraces(t, relay, body, "Bearer "+relay.token, encoding, stream)
				_ = resp.Body.Close()
				if resp.StatusCode != 200 {
					t.Fatalf("local status = %d, want 200", resp.StatusCode)
				}
				acceptedBytes += int64(len(body))
			}
			var got []receivedBatch
			if tc.wait {
				select {
				case first := <-received:
					got = append(got, first)
					if elapsed := first.at.Sub(start); elapsed >= 900*time.Millisecond {
						t.Errorf("full batch waited %s instead of sending immediately", elapsed)
					}
				case <-time.After(3 * time.Second):
					t.Error("batch did not flush without drain")
				}
			}
			drainCtx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			defer cancel()
			report := relay.Drain(drainCtx)
			close(received)
			for item := range received {
				got = append(got, item)
			}
			if len(got) != len(tc.wantBodies) {
				t.Fatalf("upstream attempts = %d, want %d", len(got), len(tc.wantBodies))
			}
			for i, item := range got {
				if !bytes.Equal(item.body, tc.wantBodies[i]) {
					t.Errorf("batch %d body differs: got %d bytes, want %d", i, len(item.body), len(tc.wantBodies[i]))
				}
				encoding := "gzip"
				if len(tc.wantEncoding) > 0 {
					encoding = tc.wantEncoding[i]
				}
				if item.encoding != encoding {
					t.Errorf("batch %d encoding = %q, want %q", i, item.encoding, encoding)
				}
				stream := "test"
				if len(tc.wantStream) > 0 {
					stream = tc.wantStream[i]
				}
				if item.stream != stream {
					t.Errorf("batch %d stream = %q, want %q", i, item.stream, stream)
				}
			}
			if slices.Contains(tc.statuses, 403) {
				want := Report{DroppedPermanentRequests: 3, DroppedBytes: acceptedBytes, Refusals: []Refusal{{Status: 403, Requests: 3, TestRequests: 3}}}
				if diff := cmp.Diff(want, report); diff != "" {
					t.Errorf("refused batch report (-want +got):\n%s", diff)
				}
			} else if report.ForwardedRequests != len(tc.bodies) || report.ForwardedBytes != acceptedBytes || report.DroppedBytes != 0 {
				t.Errorf("report = %+v, want all %d original requests (%d bytes) forwarded", report, len(tc.bodies), acceptedBytes)
			}
		})
	}
}

func TestRelayForwardsIdleTestRequestsImmediately(t *testing.T) {
	type upload struct {
		body     []byte
		encoding string
		at       time.Time
	}
	received := make(chan upload, 4)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		received <- upload{body, req.Header.Get("Content-Encoding"), time.Now()}
	}))
	defer upstream.Close()
	const window = 500 * time.Millisecond
	relay := newTestRelay(t, Config{UpstreamEndpoint: upstream.URL, RunKey: "build-id", TokenSource: staticToken("token"), batchWindow: window})
	relay.mu.Lock()
	relay.lastTestUpload = time.Time{}
	relay.mu.Unlock()

	post := func(body string) time.Time {
		t.Helper()
		resp := postTraces(t, relay, []byte(body), "Bearer "+relay.token, "", "test")
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("POST status = %d, want 200", resp.StatusCode)
		}
		return time.Now()
	}
	next := func() upload {
		t.Helper()
		select {
		case item := <-received:
			return item
		case <-time.After(3 * time.Second):
			t.Fatal("upload did not arrive")
			return upload{}
		}
	}

	posted := post("a")
	first := next()
	if string(first.body) != "a" || first.encoding != "" || first.at.Sub(posted) > 200*time.Millisecond {
		t.Errorf("idle request uploaded as %q (%q) after %s, want it forwarded unchanged and immediately", first.body, first.encoding, first.at.Sub(posted))
	}

	// Requests arriving within the window after an upload wait for it and merge.
	post("b")
	time.Sleep(window / 2)
	post("c")
	second := next()
	if gap := second.at.Sub(first.at); gap < window-100*time.Millisecond || gap > window+400*time.Millisecond {
		t.Errorf("busy batch sent %s after the previous upload, want about %s", gap, window)
	}
	reader, err := gzip.NewReader(bytes.NewReader(second.body))
	if err != nil {
		t.Fatal(err)
	}
	merged, _ := io.ReadAll(reader)
	if string(merged) != "bc" {
		t.Errorf("busy batch = %q, want bc", merged)
	}

	// Once a window passes without an upload, requests go immediately again.
	time.Sleep(window + 200*time.Millisecond)
	posted = post("d")
	third := next()
	if string(third.body) != "d" || third.at.Sub(posted) > 200*time.Millisecond {
		t.Errorf("request after idle window uploaded as %q after %s, want it immediately", third.body, third.at.Sub(posted))
	}

	drainCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if report := relay.Drain(drainCtx); report.ForwardedRequests != 4 {
		t.Errorf("forwarded %d requests, want 4", report.ForwardedRequests)
	}
}

func TestRelayKeepsStandaloneGzipBodiesUnchanged(t *testing.T) {
	compress := func(body []byte) []byte {
		var buffer bytes.Buffer
		writer := gzip.NewWriter(&buffer)
		writer.Name = "collector-original-header"
		_, _ = writer.Write(body)
		_ = writer.Close()
		return buffer.Bytes()
	}
	valid := compress([]byte("standalone child spans"))
	badChecksum := bytes.Clone(valid)
	badChecksum[len(badChecksum)-8] ^= 0xff
	for _, tc := range []struct {
		name   string
		body   []byte
		stream string
	}{
		{"valid isolated stream", valid, "child"},
		{"invalid header", []byte("not gzip"), "test"},
		{"truncated", valid[:len(valid)-4], "test"},
		{"invalid checksum", badChecksum, "test"},
		{"oversized decompressed body", compress(bytes.Repeat([]byte("x"), 600*1024)), "test"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			type upload struct {
				body     []byte
				encoding string
			}
			received := make(chan upload, 3)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				body, _ := io.ReadAll(req.Body)
				received <- upload{body, req.Header.Get("Content-Encoding")}
			}))
			defer upstream.Close()
			relay := newTestRelay(t, Config{UpstreamEndpoint: upstream.URL, RunKey: "build-id", TokenSource: staticToken("token"), batchWindow: time.Second})
			for i, body := range [][]byte{[]byte("before"), tc.body, []byte("after")} {
				encoding, stream := "", "test"
				if i == 1 {
					encoding, stream = "gzip", tc.stream
				}
				resp := postTraces(t, relay, body, "Bearer "+relay.token, encoding, stream)
				_ = resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					t.Fatalf("POST status = %d, want accepted", resp.StatusCode)
				}
			}
			drainCtx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			report := relay.Drain(drainCtx)
			if report.ForwardedRequests != 3 || report.ForwardedBytes != int64(11+len(tc.body)) || report.DroppedBytes != 0 {
				t.Errorf("report = %+v, want all three original requests forwarded", report)
			}
			close(received)
			var uploads []upload
			for item := range received {
				uploads = append(uploads, item)
			}
			if tc.stream == "child" {
				if len(uploads) != 2 {
					t.Fatalf("uploads = %d, want test batch and standalone child", len(uploads))
				}
				reader, err := gzip.NewReader(bytes.NewReader(uploads[0].body))
				if err != nil {
					t.Fatal(err)
				}
				defer reader.Close()
				body, _ := io.ReadAll(reader)
				if string(body) != "beforeafter" {
					t.Errorf("merged test body = %q, want beforeafter", body)
				}
				uploads = uploads[1:]
			} else {
				if len(uploads) != 3 || string(uploads[0].body) != "before" || string(uploads[2].body) != "after" {
					t.Fatal("unbatchable gzip did not remain between its neighbouring requests")
				}
				uploads = uploads[1:2]
			}
			if uploads[0].encoding != "gzip" || !bytes.Equal(uploads[0].body, tc.body) {
				t.Error("standalone gzip request changed its bytes or encoding")
			}
		})
	}
}

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
	body := bytes.Repeat([]byte("full"), 1024)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var reader io.Reader = req.Body
		if req.Header.Get("Content-Encoding") == "gzip" {
			gz, err := gzip.NewReader(req.Body)
			if err != nil {
				t.Error(err)
				return
			}
			defer gz.Close()
			reader = gz
			close(started)
			<-release
		}
		got, _ := io.ReadAll(reader)
		if !bytes.Equal(got, bytes.Repeat(body, 2)) {
			t.Error("upstream did not receive the full payload")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	relay := newTestRelay(t, Config{
		UpstreamEndpoint: upstream.URL,
		RunKey:           "build-id",
		QueueCapacity:    2 * (len(body) + queueRequestOverhead),
		TokenSource:      staticToken("oidc-token"),
		batchWindow:      100 * time.Millisecond,
	})

	for range 2 {
		resp := postTraces(t, relay, body, "Bearer "+relay.token, "", "test")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("POST status = %d, want 200 before upstream delivery completes", resp.StatusCode)
		}
		_ = resp.Body.Close()
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("upstream delivery did not start")
	}

	second := postTraces(t, relay, nil, "Bearer "+relay.token, "")
	if second.StatusCode != http.StatusTooManyRequests {
		t.Errorf("second POST status = %d, want 429", second.StatusCode)
	}
	if second.Header.Get("Retry-After") != "1" {
		t.Error("full queue did not return Retry-After: 1")
	}
	_ = second.Body.Close()

	close(release)
	// Reuse almost the entire queue. Releasing only the compressed batch size
	// instead of both original bodies would leave this request returning 429.
	deadline := time.Now().Add(time.Second)
	for {
		resp := postTraces(t, relay, bytes.Repeat(body, 2), "Bearer "+relay.token, "")
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			break
		}
		if resp.StatusCode != http.StatusTooManyRequests || time.Now().After(deadline) {
			t.Fatalf("queue capacity was not released: status %d", resp.StatusCode)
		}
		time.Sleep(time.Millisecond)
	}
	drainCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	report := relay.Drain(drainCtx)
	if report.ForwardedRequests != 3 || report.ForwardedBytes != int64(4*len(body)) || report.DroppedBytes != 0 {
		t.Errorf("Drain report = %+v, want three forwarded requests and no silent drop", report)
	}
}

func TestRelayRetriesRetryableResponses(t *testing.T) {
	var requests atomic.Int32
	type attempt struct {
		body []byte
		at   time.Time
	}
	attempts := make(chan attempt, 3)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		requestNumber := requests.Add(1)
		if got := req.Header.Get("Buildkite-Tests-Span-Stream"); got != "test" {
			t.Errorf("attempt %d Buildkite-Tests-Span-Stream = %q, want test", requestNumber, got)
		}
		if req.Header.Get("Content-Encoding") != "gzip" {
			t.Error("merged retry must retain gzip encoding")
		}
		body, _ := io.ReadAll(req.Body)
		attempts <- attempt{body, time.Now()}
		if requestNumber == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		if requestNumber == 2 {
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
		QueueCapacity:    2 * (7 + queueRequestOverhead),
		batchWindow:      100 * time.Millisecond,
	})
	for range 2 {
		resp := postTraces(t, relay, []byte("request"), `Token token="`+relay.token+`"`, "", "test")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("POST status = %d, want 200", resp.StatusCode)
		}
		_ = resp.Body.Close()
	}
	var got []attempt
	for range 3 {
		select {
		case item := <-attempts:
			got = append(got, item)
			if len(got) == 1 {
				resp := postTraces(t, relay, nil, "Bearer "+relay.token, "")
				_ = resp.Body.Close()
				if resp.StatusCode != http.StatusTooManyRequests {
					t.Errorf("queue status during retry = %d, want 429", resp.StatusCode)
				}
			}
		case <-time.After(3 * time.Second):
			t.Fatal("merged batch retry did not arrive")
		}
	}
	if delay := got[1].at.Sub(got[0].at); delay < time.Second {
		t.Errorf("retry delay = %s, ignored Retry-After: 1", delay)
	}
	if !bytes.Equal(got[0].body, got[1].body) || !bytes.Equal(got[0].body, got[2].body) {
		t.Error("retry changed the merged gzip body")
	}
	reader, err := gzip.NewReader(bytes.NewReader(got[0].body))
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	decoded, _ := io.ReadAll(reader)
	if string(decoded) != "requestrequest" {
		t.Errorf("retried body = %q, want both original requests", decoded)
	}

	drainCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	report := stripLatencyStats(t, relay.Drain(drainCtx))
	if got := requests.Load(); got != 3 {
		t.Errorf("upstream requests = %d, want 3", got)
	}
	if !cmp.Equal(report, Report{ForwardedRequests: 2, ForwardedBytes: 14, ForwardedSize: singleStat(int64(len(got[0].body)))}) {
		t.Errorf("Drain report = %+v, want two original requests in one upstream upload", report)
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
	received := make(chan struct{}, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		requests.Add(1)
		body, _ := io.ReadAll(req.Body)
		index := int(body[0])
		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(cases[index].status)
		_, _ = w.Write(cases[index].body)
		received <- struct{}{}
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
				<-received // Each fixture needs its own upstream refusal reason.
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
	received := make(chan struct{}, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		status := http.StatusForbidden
		if string(body) == "unauthorized" {
			status = http.StatusUnauthorized
		}
		w.WriteHeader(status)
		_, _ = w.Write(append([]byte{0x08, 0x07, 0x12, byte(len(body))}, body...))
		received <- struct{}{}
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
		<-received // Keep distinct-message fixtures in separate batches.
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
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Header.Get("Content-Encoding") != "gzip" || req.Header.Get("Buildkite-Tests-Span-Stream") != "test" {
			t.Error("expected a merged test batch in flight at the drain deadline")
		}
		close(started)
		<-release
	}))
	defer upstream.Close()

	body := []byte("request")
	relay := newTestRelay(t, Config{
		UpstreamEndpoint: upstream.URL,
		RunKey:           "build-id",
		TokenSource:      staticToken("oidc-token"),
		batchWindow:      500 * time.Millisecond,
	})
	for _, stream := range []string{"test", "child", "test"} {
		resp := postTraces(t, relay, body, "Bearer "+relay.token, "", stream)
		_ = resp.Body.Close()
	}
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("upstream delivery did not start")
	}

	drainCtx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	report := relay.Drain(drainCtx)
	close(release)
	want := Report{DroppedDeadlineRequests: 3, DroppedDeadlineTestRequests: 2, DroppedBytes: int64(3 * len(body))}
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
	want := Report{DroppedDeadlineRequests: 1, DroppedDeadlineUnlabelledRequests: 1, DroppedBytes: int64(len(body))}
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
	want := Report{DroppedDeadlineRequests: 1, DroppedDeadlineUnlabelledRequests: 1, DroppedBytes: int64(len(body))}
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
	if config.batchWindow == 0 {
		config.batchWindow = 10 * time.Millisecond
	}
	relay, err := New(config)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	// Start as if a test upload just happened, so test requests posted together
	// wait for the window and merge deterministically. Tests of idle forwarding
	// reset this.
	relay.mu.Lock()
	relay.lastTestUpload = time.Now()
	relay.mu.Unlock()
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
