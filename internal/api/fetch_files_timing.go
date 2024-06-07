package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type fetchFilesTimingParams struct {
	Paths []string `json:"paths"`
}

// FetchFilesTiming fetches the timing of the requested files from the server.
// The server only returns timings for the files that has been run before.
func (c Client) FetchFilesTiming(suiteSlug string, files []string) (map[string]time.Duration, error) {
	url := fmt.Sprintf("%s/v2/analytics/organizations/%s/suites/%s/test_files", c.ServerBaseUrl, c.OrganizationSlug, suiteSlug)

	requestBody, err := json.Marshal(fetchFilesTimingParams{
		Paths: files,
	})

	if err != nil {
		return nil, fmt.Errorf("converting params to JSON: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(requestBody))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errorResp errorResponse
		json.Unmarshal(responseBody, &errorResp)
		return nil, fmt.Errorf(errorResp.Message)
	}

	var filesTiming map[string]float64
	err = json.Unmarshal(responseBody, &filesTiming)
	if err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	result := map[string]time.Duration{}

	for path, duration := range filesTiming {
		result[path] = time.Duration(duration * float64(time.Second))
	}

	return result, nil
}
