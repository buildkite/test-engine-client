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
	"sync"
	"syscall"
	"testing"
	"testing/synctest"
	"time"
	"uuid"
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

// fakeLeaseScheduler commits one lease per idempotency key and replays it for
// retries with that key, as the Scheduler does.
type fakeLeaseScheduler struct {
	mu      sync.Mutex
	commits map[string]int
	keys    []string
}

func (f *fakeLeaseScheduler) acquire(t *testing.T, r *http.Request) *http.Response {
	key := r.Header.Get("Idempotency-Key")
	if u, err := uuid.Parse(key); err != nil || u[6]>>4 != 7 {
		t.Errorf("Idempotency-Key %q is not a UUIDv7", key)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.commits == nil {
		f.commits = map[string]int{}
	}
	if f.commits[key] == 0 {
		f.keys = append(f.keys, key)
	}
	f.commits[key] = 1
	body := fmt.Sprintf(`{"lease":{"id":%q,"expires_at":"2030-01-01T00:00:00Z","attempts":[{"id":"attempt-%d"}]},"pool":{"state":"consuming"}}`, key, len(f.keys))
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

type failingBody struct{}

func (failingBody) Read([]byte) (int, error) { return 0, syscall.ECONNRESET }
func (failingBody) Close() error             { return nil }

func TestAcquireLeaseReplaysCommittedLeaseAfterAmbiguousFailure(t *testing.T) {
	for _, tc := range []struct {
		name string
		lose func(r *http.Request, committed *http.Response) (*http.Response, error)
	}{
		{"response lost", func(*http.Request, *http.Response) (*http.Response, error) { return nil, io.EOF }},
		{"unreadable response", func(_ *http.Request, committed *http.Response) (*http.Response, error) {
			committed.Body = failingBody{}
			return committed, nil
		}},
		{"server error after commit", func(*http.Request, *http.Response) (*http.Response, error) {
			return &http.Response{StatusCode: 502, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{}`))}, nil
		}},
		{"timed out while server commits", func(r *http.Request, _ *http.Response) (*http.Response, error) {
			// The retry is a duplicate of a request the server is still handling.
			<-r.Context().Done()
			return nil, r.Context().Err()
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				scheduler := &fakeLeaseScheduler{}
				calls := 0
				c := NewClient(ClientConfig{ServerBaseURL: "http://scheduler", OrganizationSlug: "org"})
				c.httpClient.Transport = leaseTransport(func(r *http.Request) (*http.Response, error) {
					calls++
					committed := scheduler.acquire(t, r)
					if calls == 1 {
						return tc.lose(r, committed)
					}
					return committed, nil
				})
				var leases []*Lease
				for range 2 {
					response, err := c.AcquireLease(context.Background(), "pool")
					if err != nil {
						t.Fatal(err)
					}
					leases = append(leases, response.Lease)
				}
				if calls != 3 || len(scheduler.keys) != 2 {
					t.Fatalf("calls=%d keys=%v, want one replayed acquisition and one new key", calls, scheduler.keys)
				}
				for i, lease := range leases {
					if lease.ID != scheduler.keys[i] || len(lease.Attempts) != 1 || lease.Attempts[0].ID != fmt.Sprintf("attempt-%d", i+1) {
						t.Errorf("lease %d=%+v, want lease for key %s", i, lease, scheduler.keys[i])
					}
				}
			})
		})
	}
}

func TestAcquireLeaseRetriesWithSameKeyUntilDeadline(t *testing.T) {
	for _, cause := range []error{io.EOF, syscall.ECONNRESET, &net.OpError{Op: "read", Err: context.DeadlineExceeded}, nil} {
		name := "server error"
		if cause != nil {
			name = cause.Error()
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				keys := map[string]bool{}
				calls := 0
				c := NewClient(ClientConfig{ServerBaseURL: "http://scheduler", OrganizationSlug: "org"})
				c.httpClient.Transport = leaseTransport(func(r *http.Request) (*http.Response, error) {
					calls++
					keys[r.Header.Get("Idempotency-Key")] = true
					if r.GetBody != nil {
						t.Error("net/http can replay the lease request")
					}
					if cause != nil {
						return nil, cause
					}
					return &http.Response{StatusCode: 503, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{}`))}, nil
				})
				start := time.Now()
				_, err := c.AcquireLease(context.Background(), "pool")
				if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) != 90*time.Second || calls < 3 || len(keys) != 1 {
					t.Fatalf("err=%v elapsed=%s calls=%d keys=%v", err, time.Since(start), calls, keys)
				}
			})
		})
	}
}

func TestAcquireLeaseStartsOverWithNewKeyWhenLeaseIsNotReplayable(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		scheduler := &fakeLeaseScheduler{}
		var keys []string
		c := NewClient(ClientConfig{ServerBaseURL: "http://scheduler", OrganizationSlug: "org"})
		c.httpClient.Transport = leaseTransport(func(r *http.Request) (*http.Response, error) {
			keys = append(keys, r.Header.Get("Idempotency-Key"))
			switch len(keys) {
			case 1:
				return nil, io.EOF
			case 2:
				return &http.Response{StatusCode: 503, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{}`))}, nil
			case 3:
				// For example, the lease committed by the first request has expired.
				return &http.Response{StatusCode: 409, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"message":"Lease cannot be replayed","code":"LEASE_NOT_REPLAYABLE"}`))}, nil
			}
			return scheduler.acquire(t, r), nil
		})
		response, err := c.AcquireLease(context.Background(), "pool")
		if err != nil {
			t.Fatal(err)
		}
		if len(keys) != 4 || keys[1] != keys[0] || keys[2] != keys[0] || keys[3] == keys[0] || response.Lease.ID != keys[3] {
			t.Fatalf("keys=%v lease=%+v, want three requests with one key, then a lease for a new key", keys, response.Lease)
		}
	})
}

func TestAcquireLeaseReturnsOtherConflicts(t *testing.T) {
	calls := 0
	c := NewClient(ClientConfig{ServerBaseURL: "http://scheduler"})
	c.httpClient.Transport = leaseTransport(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 409, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"message":"test pool is not available for leasing"}`))}, nil
	})
	_, err := c.AcquireLease(context.Background(), "pool")
	var leaseError *LeaseHTTPError
	if calls != 1 || !errors.As(err, &leaseError) || leaseError.Status != 409 {
		t.Fatalf("calls=%d err=%v, want one request returning HTTP 409", calls, err)
	}
}

func TestAcquireLeaseRejectsLeaseForAnotherKey(t *testing.T) {
	c := NewClient(ClientConfig{ServerBaseURL: "http://scheduler"})
	c.httpClient.Transport = leaseTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"lease":{"id":"server-chosen","attempts":[]},"pool":{"state":"consuming"}}`))}, nil
	})
	if _, err := c.AcquireLease(context.Background(), "pool"); err == nil {
		t.Fatal("accepted a lease that ignored the idempotency key")
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
		{name: "ambiguous server error", code: 503, want: 2},
		{name: "idempotency conflict", code: 409, want: 1},
		{name: "invalid idempotency key", code: 400, want: 1},
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

func TestReleaseLeaseReturnsAllActiveAttemptsToPool(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/organizations/org/test-scheduler/pools/pool/leases/release" {
			t.Errorf("path=%s", r.URL.Path)
		}
		var body struct {
			Leases []struct {
				ID         string   `json:"id"`
				AttemptIDs []string `json:"attempt_ids"`
				Reason     string   `json:"reason"`
			} `json:"leases"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if len(body.Leases) != 1 || body.Leases[0].ID != "lease" || body.Leases[0].AttemptIDs != nil || body.Leases[0].Reason != "Worker shutdown" {
			t.Errorf("release body=%+v", body)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	c := NewClient(ClientConfig{ServerBaseURL: server.URL, OrganizationSlug: "org"})
	if err := c.ReleaseLease(context.Background(), "pool", "lease"); err != nil {
		t.Fatal(err)
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

func TestHeartbeatResponseContract(t *testing.T) {
	expires := time.Now().Add(7 * time.Minute).UTC().Truncate(time.Second)
	active := fmt.Sprintf(`{"leases":[{"lease_id":"lease","expires_at":%q}]}`, expires.Format(time.RFC3339))
	terminal := `{"pool":{"id":"pool","state":"consumed"},"leases":[]}`
	for _, tc := range []struct {
		name    string
		status  int
		body    string
		want    LeaseHeartbeat
		wantErr bool
	}{
		{name: "active renewal", status: 200, body: active, want: LeaseHeartbeat{ExpiresAt: expires}},
		{name: "consumed", status: 200, body: terminal, want: LeaseHeartbeat{PoolConsumed: true}},
		{name: "empty leases alone", status: 200, body: `{"leases":[]}`, wantErr: true},
		{name: "empty active leases", status: 200, body: `{"pool":{"id":"pool","state":"consuming"},"leases":[]}`, wantErr: true},
		{name: "wrong pool", status: 200, body: `{"pool":{"id":"other","state":"consumed"},"leases":[]}`, wantErr: true},
		{name: "missing pool ID", status: 200, body: `{"pool":{"state":"consumed"},"leases":[]}`, wantErr: true},
		{name: "missing leases", status: 200, body: `{"pool":{"id":"pool","state":"consumed"}}`, wantErr: true},
		{name: "null leases", status: 200, body: `{"pool":{"id":"pool","state":"consumed"},"leases":null}`, wantErr: true},
		{name: "consumed with renewal", status: 200, body: strings.Replace(active, `{`, `{"pool":{"id":"pool","state":"consumed"},`, 1), wantErr: true},
		{name: "wrong active lease", status: 200, body: strings.Replace(active, `"lease"`, `"other"`, 1), wantErr: true},
		{name: "expired renewal", status: 200, body: `{"leases":[{"lease_id":"lease","expires_at":"2000-01-01T00:00:00Z"}]}`, wantErr: true},
		{name: "missing expiry", status: 200, body: `{"leases":[{"lease_id":"lease"}]}`, wantErr: true},
		{name: "generic success", status: 204, wantErr: true},
		{name: "non-200 terminal", status: 202, body: terminal, wantErr: true},
		{name: "legacy consumed conflict", status: 409, body: `{"message":"test pool is not available for lease heartbeat"}`, wantErr: true},
		{name: "authentication", status: 401, body: terminal, wantErr: true},
		{name: "expired lease", status: 404, body: terminal, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := NewClient(ClientConfig{ServerBaseURL: "http://scheduler"})
			calls := 0
			c.httpClient.Transport = leaseTransport(func(*http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})
			got, err := c.HeartbeatLease(context.Background(), "pool", "lease")
			if (err != nil) != tc.wantErr || got != tc.want || calls != 1 {
				t.Fatalf("heartbeat=%+v error=%v calls=%d, want %+v error=%t and one request", got, err, calls, tc.want, tc.wantErr)
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

func TestHeartbeatExposesStructuredUnprocessableEntityCode(t *testing.T) {
	for _, tc := range []struct {
		name, body, wantCode string
	}{
		{"maximum lifetime", `{"code":"MAXIMUM_LIFETIME","message":"wording may change"}`, LeaseErrorCodeMaximumLifetime},
		{"unrelated validation", `{"code":"INVALID_LEASE_TTL","message":"invalid TTL"}`, "INVALID_LEASE_TTL"},
		{"invalid response", `<html>upstream error</html>`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := NewClient(ClientConfig{ServerBaseURL: "http://scheduler"})
			c.httpClient.Transport = leaseTransport(func(*http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusUnprocessableEntity,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(tc.body)),
				}, nil
			})
			_, err := c.HeartbeatLease(context.Background(), "pool", "lease")
			var leaseError *LeaseHTTPError
			if !errors.As(err, &leaseError) || leaseError.Status != http.StatusUnprocessableEntity || leaseError.Code != tc.wantCode {
				t.Fatalf("error=%v, want HTTP 422 code %q", err, tc.wantCode)
			}
		})
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
