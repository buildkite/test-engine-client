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
// This method bypasses DoWithRetry because it needs a custom Accept header and
// text response parsing. It still uses the client's httpClient which has the
// authTransport middleware (Bearer token + User-Agent).
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
