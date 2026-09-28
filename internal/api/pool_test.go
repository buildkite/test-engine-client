package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPlanPoolContract(t *testing.T) {
	oldDelay := initialDelay
	initialDelay = time.Millisecond
	t.Cleanup(func() { initialDelay = oldDelay })
	for _, tc := range []struct {
		name      string
		status    int
		body      string
		wantError string
		retries   bool
	}{
		{"accepted", 202, `{"id":"pool-1","state":"planning"}`, "", false},
		{"contention", 409, `{"message":"Still creating test pool, please retry."}`, "", true},
		{"fingerprint conflict", 409, `{"message":"A test pool already exists for this build and key with a different planning request; use a new key"}`, "different planning request", false},
		{"errored", 409, `{"id":"pool-1","state":"errored","error":{"message":"Test pool failed."}}`, "pool-1 is errored: Test pool failed.", false},
		{"expired", 410, `{"message":"Test pool has expired; use a new key"}`, "expired", false},
		{"unknown conflict", 409, `{"message":"Another conflict"}`, "Another conflict", false},
		{"invalid", 422, `{"message":"Invalid plan"}`, "Invalid plan", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := 0
			var firstBody string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				require.Equal(t, "POST", r.Method)
				require.Equal(t, "/v2/organizations/acme/test-scheduler/pools/plan", r.URL.Path)
				require.Equal(t, "Bearer token", r.Header.Get("Authorization"))
				require.Equal(t, "application/json", r.Header.Get("Content-Type"))
				body, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				if requests == 1 {
					firstBody = string(body)
				} else {
					require.Equal(t, firstBody, string(body), "contention must replay the identical request")
				}
				w.Header().Set("Location", "https://api.buildkite.com/v2/organizations/acme/test-scheduler/pools/pool-1")
				if tc.retries && requests > 1 {
					w.WriteHeader(202)
					io.WriteString(w, `{"id":"pool-1","state":"planning"}`)
					return
				}
				w.WriteHeader(tc.status)
				io.WriteString(w, tc.body)
			}))
			defer server.Close()
			client := NewClient(ClientConfig{ServerBaseURL: server.URL, OrganizationSlug: "acme", AccessToken: "token"})
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			pool, err := client.PlanPool(ctx, PoolPlanParams{Suite: "suite", Pipeline: "pipeline", BuildID: "build", Key: "key", Plan: PoolPlan{Runner: "rspec", Tests: TestPlanParamsTest{Selectors: []TestPlanParamsSelector{{Value: "spec/a_spec.rb"}}}}})
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
			} else {
				require.NoError(t, err)
				require.Equal(t, "pool-1", pool.ID)
				require.Equal(t, "planning", pool.State)
				require.Nil(t, pool.MutedTests)
				require.Equal(t, "https://api.buildkite.com/v2/organizations/acme/test-scheduler/pools/pool-1", pool.Location)
			}
			wantRequests := 1
			if tc.retries {
				wantRequests = 2
			}
			require.Equal(t, wantRequests, requests)
			require.JSONEq(t, `{"suite":"suite","pipeline":"pipeline","build_id":"build","key":"key","plan":{"runner":"rspec","branch":"","tests":{"selectors":[{"value":"spec/a_spec.rb"}]}}}`, firstBody)
		})
	}
}

// Use an in-memory transport in a synctest bubble to exercise actual backoff
// and cancellation without sleeps or network scheduling affecting the clock.
type poolTransport func(*http.Request) (*http.Response, error)

func (f poolTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestWaitForPool(t *testing.T) {
	for _, tc := range []struct {
		name         string
		responses    []string
		wantError    string
		wantState    string
		wantMuted    int
		wantMutedNil bool
	}{
		{"planning then snapshot", []string{`{"id":"p","state":"planning"}`, `{"id":"p","state":"consuming","muted_tests":[{"scope":"User","name":"works","path":"user_spec.rb:42"}]}`}, "", "consuming", 1, false},
		{"planning then populating", []string{`{"id":"p","state":"planning"}`, `{"id":"p","state":"populating"}`}, "", "populating", 0, true},
		{"empty selected plan", []string{`{"id":"p","state":"consumed","muted_tests":[]}`}, "", "consumed", 0, false},
		{"missing muted tests on ready pool", []string{`{"id":"p","state":"consuming"}`}, "", "consuming", 0, true},
		{"null muted tests on consumed pool", []string{`{"id":"p","state":"consumed","muted_tests":null}`}, "", "consumed", 0, true},
		{"errored after planning", []string{`{"id":"p","state":"planning"}`, `{"id":"p","state":"errored","error":{"message":"Test pool failed."}}`}, "is errored: Test pool failed.", "", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				requests := 0
				client := NewClient(ClientConfig{ServerBaseURL: "https://example.test", OrganizationSlug: "acme"})
				client.httpClient.Transport = poolTransport(func(r *http.Request) (*http.Response, error) {
					require.Equal(t, "GET", r.Method)
					require.Equal(t, "/v2/organizations/acme/test-scheduler/pools/p", r.URL.Path)
					require.Less(t, requests, len(tc.responses))
					body := tc.responses[requests]
					requests++
					return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
				})
				start := time.Now()
				pool, err := client.WaitForPool(context.Background(), "p")
				if tc.wantError != "" {
					require.ErrorContains(t, err, tc.wantError)
				} else {
					require.NoError(t, err)
					require.Equal(t, tc.wantState, pool.State)
					if tc.wantMutedNil {
						require.Nil(t, pool.MutedTests)
					} else {
						require.NotNil(t, pool.MutedTests)
					}
					require.Len(t, pool.MutedTests, tc.wantMuted)
					if tc.wantMuted > 0 {
						require.Equal(t, "User", pool.MutedTests[0].Scope)
						require.Equal(t, "works", pool.MutedTests[0].Name)
						require.Equal(t, "user_spec.rb:42", pool.MutedTests[0].Path)
					}
				}
				require.Equal(t, len(tc.responses), requests)
				require.Equal(t, time.Duration(len(tc.responses)-1)*time.Second, time.Since(start))
			})
		})
	}
}

func TestWaitForPoolCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		requests := 0
		client := NewClient(ClientConfig{ServerBaseURL: "https://example.test"})
		client.httpClient.Transport = poolTransport(func(r *http.Request) (*http.Response, error) {
			requests++
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"id":"p","state":"planning"}`)), Header: http.Header{}}, nil
		})
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		_, err := client.WaitForPool(ctx, "p")
		require.True(t, errors.Is(err, context.DeadlineExceeded))
		require.Equal(t, 3, requests, "polls at 0, 1, and 3 seconds, then cancellation during backoff")
	})
}
