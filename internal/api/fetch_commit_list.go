package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/buildkite/test-engine-client/internal/debug"
)

// FetchCommitList fetches the list of commit SHAs that need metadata from the
// commits API endpoint.
//
// Endpoint: GET /v2/analytics/organizations/:org/suites/:suite/commits?days=N
// Uses Accept: text/plain to receive newline-delimited SHAs for simpler parsing.
//
// This method does not use DoWithRetry because DoWithRetry assumes JSON
// responses and sets Content-Type: application/json. This endpoint requires a
// custom Accept: text/plain header and returns a plain-text body. Adding retry
// support would require refactoring DoWithRetry to support custom headers and
// non-JSON response parsing, which isn't warranted for this single call site.
// The authTransport middleware (Bearer token + User-Agent) is still applied
// via the client's httpClient.
func (c Client) FetchCommitList(ctx context.Context, suiteSlug string, days int) ([]string, error) {
	url := fmt.Sprintf(
		"%s/v2/analytics/organizations/%s/suites/%s/commits?days=%d",
		c.ServerBaseUrl, c.OrganizationSlug, suiteSlug, days)

	debug.Printf("Fetching commit list: GET %s", url)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Accept", "text/plain")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching commit list: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("fetching commit list: status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading commit list response: %w", err)
	}

	text := strings.TrimSpace(string(body))
	if text == "" {
		return nil, nil
	}

	commits := strings.Split(text, "\n")
	return commits, nil
}
