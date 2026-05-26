package api

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// UploadTestResults POSTs the raw result file from the runner to the
// Test Engine analytics API at <c.UploadBaseURL>/v1/uploads.
// The format parameter tells the ingestion gear how to parse the file
// (e.g. "rspec-json", "jest-json").
// Errors are returned to the caller to be logged and suppressed — this upload
// is best-effort and must not fail the build.
func (c *Client) UploadTestResults(ctx context.Context, token string, filePath string, format string, locationPrefix string) error {
	body, contentType, err := buildTestResultsMultipartBody(filePath, format, locationPrefix)
	if err != nil {
		return err
	}

	uploadURL := strings.TrimRight(c.UploadBaseURL, "/") + "/v1/uploads"
	uploadCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(uploadCtx, http.MethodPost, uploadURL, body)
	if err != nil {
		return fmt.Errorf("creating upload request: %w", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Token token=%s", token))
	req.Header.Set("Content-Type", contentType)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("uploading test results: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("upload failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

func buildTestResultsMultipartBody(filePath string, format string, locationPrefix string) (*bytes.Buffer, string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	if err := w.WriteField("format", format); err != nil {
		return nil, "", fmt.Errorf("writing format field: %w", err)
	}

	runEnv := map[string]string{
		"CI":              "buildkite",
		"key":             os.Getenv("BUILDKITE_BUILD_ID"),
		"branch":          os.Getenv("BUILDKITE_BRANCH"),
		"commit_sha":      os.Getenv("BUILDKITE_COMMIT"),
		"number":          os.Getenv("BUILDKITE_BUILD_NUMBER"),
		"url":             os.Getenv("BUILDKITE_BUILD_URL"),
		"job_id":          os.Getenv("BUILDKITE_JOB_ID"),
		"message":         os.Getenv("BUILDKITE_MESSAGE"),
		"location_prefix": locationPrefix,
	}
	for k, v := range runEnv {
		if err := w.WriteField(fmt.Sprintf("run_env[%s]", k), v); err != nil {
			return nil, "", fmt.Errorf("writing run_env[%s]: %w", k, err)
		}
	}

	fw, err := w.CreateFormFile("data", filepath.Base(filePath))
	if err != nil {
		return nil, "", fmt.Errorf("creating form file: %w", err)
	}
	f, err := os.Open(filePath)
	if err != nil {
		return nil, "", fmt.Errorf("opening result file: %w", err)
	}
	defer f.Close()
	if _, err := io.Copy(fw, f); err != nil {
		return nil, "", fmt.Errorf("copying file content: %w", err)
	}

	if err := w.Close(); err != nil {
		return nil, "", fmt.Errorf("closing multipart writer: %w", err)
	}

	return &buf, w.FormDataContentType(), nil
}
