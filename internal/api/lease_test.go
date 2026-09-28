package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"syscall"
	"testing"
	"testing/synctest"
	"time"
)

type leaseTransport func(*http.Request) (*http.Response, error)

func (f leaseTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestLeaseTTLOnAcquireAndHeartbeat(t *testing.T) {
	expires := time.Now().Add(10 * time.Minute).UTC().Format(time.RFC3339Nano)
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body struct {
			TTL      int      `json:"lease_ttl_seconds"`
			LeaseIDs []string `json:"lease_ids"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.TTL != 600 {
			t.Errorf("lease request body=%+v err=%v", body, err)
		}
		switch r.URL.Path {
		case "/v2/organizations/org/test-scheduler/pools/pool/leases":
			if len(body.LeaseIDs) != 0 {
				t.Errorf("acquire lease IDs=%v", body.LeaseIDs)
			}
			fmt.Fprint(w, `{"lease":null,"pool":{"state":"consumed"}}`)
		case "/v2/organizations/org/test-scheduler/pools/pool/leases/heartbeat":
			if len(body.LeaseIDs) != 1 || body.LeaseIDs[0] != "lease" {
				t.Errorf("heartbeat lease IDs=%v", body.LeaseIDs)
			}
			fmt.Fprintf(w, `{"leases":[{"lease_id":"lease","expires_at":%q}]}`, expires)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	c := NewClient(ClientConfig{ServerBaseURL: server.URL, OrganizationSlug: "org"})
	if _, err := c.AcquireLease(context.Background(), "pool"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.HeartbeatLease(context.Background(), "pool", "lease"); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("calls=%d, want 2", calls)
	}
}

func TestLeaseAmbiguousFailuresAreNotRetried(t *testing.T) {
	for _, cause := range []error{io.EOF, syscall.ECONNRESET, &net.OpError{Op: "read", Err: context.DeadlineExceeded}} {
		t.Run(cause.Error(), func(t *testing.T) {
			calls := 0
			c := NewClient(ClientConfig{ServerBaseURL: "http://scheduler", OrganizationSlug: "org"})
			c.httpClient.Transport = leaseTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.GetBody != nil {
					t.Error("lease can be automatically replayed")
				}
				return nil, cause
			})
			_, err := c.AcquireLease(context.Background(), "pool")
			if err == nil || calls != 1 {
				t.Fatalf("calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestLeaseRetryPolicy(t *testing.T) {
	for _, tc := range []struct {
		name    string
		code    int
		network error
		want    int
	}{
		{name: "rate limited", code: 429, want: 2},
		{name: "open timeout", network: &net.OpError{Op: "dial", Err: context.DeadlineExceeded}, want: 2},
		{name: "ambiguous server error", code: 503, want: 1},
		{name: "conflict", code: 409, want: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			c := NewClient(ClientConfig{ServerBaseURL: "http://scheduler"})
			c.httpClient.Transport = leaseTransport(func(*http.Request) (*http.Response, error) {
				calls++
				if calls == 1 && tc.network != nil {
					return nil, tc.network
				}
				code := 200
				if calls == 1 {
					code = tc.code
				}
				return &http.Response{StatusCode: code, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"lease":null,"pool":{"state":"consumed"}}`))}, nil
			})
			_, err := c.AcquireLease(context.Background(), "pool")
			if calls != tc.want || (tc.want == 2 && err != nil) {
				t.Fatalf("calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestLeaseRateLimitWaitsForMostSpecificReset(t *testing.T) {
	headers := func(pairs ...string) http.Header {
		h := http.Header{}
		for i := 0; i < len(pairs); i += 2 {
			h.Set(pairs[i], pairs[i+1])
		}
		return h
	}
	for _, tc := range []struct {
		name    string
		headers http.Header
		body    int
		want    time.Duration
	}{
		{
			name: "job header",
			headers: headers(
				"RateLimit-Job-Remaining", "0",
				"RateLimit-Job-Reset", "8",
				"RateLimit-Remaining", "10",
				"RateLimit-Reset", "2",
				"RateLimit-Pool-Remaining", "10",
				"RateLimit-Pool-Reset", "3",
			),
			body: 1,
			want: 8 * time.Second,
		},
		{
			name: "pool header",
			headers: headers(
				"RateLimit-Remaining", "10",
				"RateLimit-Reset", "2",
				"RateLimit-Pool-Remaining", "0",
				"RateLimit-Pool-Reset", "6",
			),
			body: 1,
			want: 6 * time.Second,
		},
		{
			name: "multiple exhausted limits",
			headers: headers(
				"RateLimit-Remaining", "0",
				"RateLimit-Reset", "4",
				"RateLimit-Pool-Remaining", "0",
				"RateLimit-Pool-Reset", "7",
			),
			body: 1,
			want: 7 * time.Second,
		},
		{name: "body fallback", headers: http.Header{}, body: 4, want: 4 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				calls := 0
				c := NewClient(ClientConfig{ServerBaseURL: "http://scheduler"})
				c.httpClient.Transport = leaseTransport(func(*http.Request) (*http.Response, error) {
					calls++
					if calls == 1 {
						return &http.Response{StatusCode: 429, Header: tc.headers, Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{"reset":%d}`, tc.body)))}, nil
					}
					return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"lease":null,"pool":{"state":"consumed"}}`))}, nil
				})
				start := time.Now()
				if _, err := c.AcquireLease(context.Background(), "pool"); err != nil {
					t.Fatal(err)
				}
				if calls != 2 || time.Since(start) != tc.want {
					t.Fatalf("calls=%d wait=%s, want two calls %s apart", calls, time.Since(start), tc.want)
				}
			})
		})
	}
}

func TestCompletionAcceptsSuccessfulResponseWithoutParsingBody(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
	}{{"current response", `{"leases":`, http.StatusOK}, {"empty response", "", http.StatusNoContent}} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v2/organizations/org/test-scheduler/pools/pool/leases/complete" {
					t.Errorf("path=%s", r.URL.Path)
				}
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()
			c := NewClient(ClientConfig{ServerBaseURL: server.URL, OrganizationSlug: "org"})
			if err := c.CompleteLease(context.Background(), "pool", "lease", []AttemptResult{{AttemptID: "original", Result: "passed"}}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCompletionReplaysIdenticalBodyAfterAmbiguousFailure(t *testing.T) {
	for _, tc := range []struct {
		name  string
		first func() (*http.Response, error)
	}{
		{"transport failure", func() (*http.Response, error) { return nil, io.EOF }},
		{"server failure", func() (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusServiceUnavailable, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{}`))}, nil
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			var first string
			c := NewClient(ClientConfig{ServerBaseURL: "http://scheduler", OrganizationSlug: "org"})
			c.httpClient.Transport = leaseTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				raw, _ := io.ReadAll(r.Body)
				if calls == 1 {
					first = string(raw)
					return tc.first()
				}
				if string(raw) != first || !strings.Contains(first, `"attempt_id":"original"`) {
					t.Errorf("replay changed: %s vs %s", first, raw)
				}
				return &http.Response{StatusCode: http.StatusNoContent, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
			})
			if err := c.CompleteLease(context.Background(), "pool", "lease", []AttemptResult{{AttemptID: "original", Result: "passed"}}); err != nil {
				t.Fatal(err)
			}
			if calls != 2 {
				t.Fatalf("calls=%d", calls)
			}
		})
	}
}

func TestHeartbeatRetriesTransientFailureButNotOwnershipLoss(t *testing.T) {
	calls := 0
	c := NewClient(ClientConfig{ServerBaseURL: "http://scheduler"})
	c.httpClient.Transport = leaseTransport(func(*http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return nil, io.EOF
		}
		return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
	})
	_, err := c.HeartbeatLease(context.Background(), "pool", "lease")
	var status *LeaseHTTPError
	if calls != 2 || !errors.As(err, &status) || status.Status != 404 {
		t.Fatalf("calls=%d error=%v", calls, err)
	}
}

func TestTokenProviderUsedForEveryRequest(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer refreshed" {
			t.Error("provider not used")
		}
		io.WriteString(w, `{"lease":null,"pool":{"state":"consumed"}}`)
	}))
	defer server.Close()
	c := NewClient(ClientConfig{ServerBaseURL: server.URL, TokenProvider: func(context.Context) (string, error) { calls++; return "refreshed", nil }})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	for range 2 {
		if _, err := c.AcquireLease(ctx, "pool"); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 2 {
		t.Fatalf("provider calls=%d", calls)
	}
}
