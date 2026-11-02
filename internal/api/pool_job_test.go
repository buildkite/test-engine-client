package api

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWaitForPoolJob(t *testing.T) {
	consuming := `{"pool":{"id":"p","state":"consuming"},"job":{"id":"j","participated_in_terminal_failure":false}}`
	passed := `{"pool":{"id":"p","state":"consumed"},"job":{"id":"j","participated_in_terminal_failure":false}}`
	failed := `{"pool":{"id":"p","state":"consumed"},"job":{"id":"j","participated_in_terminal_failure":true}}`
	for _, tc := range []struct {
		name      string
		responses []string
		status    int
		want      bool
		wantError string
		wait      time.Duration
	}{
		{name: "unrelated failures", responses: []string{passed}},
		{name: "participated", responses: []string{failed}, want: true},
		{name: "not final until consumed", responses: []string{consuming, failed}, want: true, wait: time.Second},
		{name: "retry elsewhere passed", responses: []string{consuming, passed}, wait: time.Second},
		{name: "never consumed", responses: []string{consuming}, wantError: "context deadline exceeded", wait: 30 * time.Second},
		{name: "missing participation", responses: []string{`{"pool":{"id":"p","state":"consumed"},"job":{"id":"j"}}`}, wantError: "invalid test pool job response"},
		{name: "wrong job", responses: []string{strings.Replace(passed, `"j"`, `"other"`, 1)}, wantError: "invalid test pool job response"},
		{name: "unavailable", responses: []string{"Not Found"}, status: 404, wantError: "job API unavailable"},
		{name: "forbidden", responses: []string{`{"message":"forbidden"}`}, status: 403, wantError: "forbidden"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				client := NewClient(ClientConfig{ServerBaseURL: "https://example.test", OrganizationSlug: "acme"})
				requests := 0
				client.httpClient.Transport = poolTransport(func(r *http.Request) (*http.Response, error) {
					require.Equal(t, "GET", r.Method)
					require.Equal(t, "/v2/organizations/acme/test-scheduler/pools/p/jobs/j", r.URL.Path)
					body := tc.responses[min(requests, len(tc.responses)-1)]
					requests++
					status := tc.status
					if status == 0 {
						status = 200
					}
					return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
				})
				start := time.Now()
				failed, err := client.WaitForPoolJob(context.Background(), "p", "j")
				if tc.wantError != "" {
					require.ErrorContains(t, err, tc.wantError)
				} else {
					require.NoError(t, err)
					require.Equal(t, tc.want, failed)
				}
				require.Equal(t, tc.wait, time.Since(start))
			})
		})
	}
}
