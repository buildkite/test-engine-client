package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
)

type fetchFilesTimingParams struct {
	Paths []string `json:"paths"`
}

type fileTiming struct {
	Path     string
	Duration int
}

// FetchFilesTiming fetches the timing of the given files from the server.
// It returns the timing of the files in descending order.
func (c Client) FetchFilesTiming(suiteSlug string, files []string) ([]fileTiming, error) {
	url := fmt.Sprintf("%s/v2/analytics/organizations/%s/suites/%s/test_files", c.ServerBaseUrl, c.OrganizationSlug, suiteSlug)

	requestBody, err := json.Marshal(fetchFilesTimingParams{
		Paths: files,
	})

	if err != nil {
		return nil, fmt.Errorf("converting params to JSON: %w", err)
	}

	req, err := http.NewRequest(http.MethodGet, url, bytes.NewBuffer(requestBody))
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

	var filesTiming map[string]int
	err = json.Unmarshal(responseBody, &filesTiming)
	if err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	var result []fileTiming
	for path, duration := range filesTiming {
		result = append(result, fileTiming{
			Path:     path,
			Duration: duration,
		})
	}

	slices.SortFunc(result, func(a, b fileTiming) int {
		return b.Duration - a.Duration
	})

	return result, nil
}
