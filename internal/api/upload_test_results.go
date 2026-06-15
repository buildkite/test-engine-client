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

var (
	uploadRetryTimeout = 30 * time.Second
)

// UploadTestResults POSTs the raw result file from the runner to the
// Test Engine analytics API at <c.UploadBaseURL>/v1/uploads.
// The format parameter tells the ingestion gear how to parse the file
// (e.g. "rspec-json", "jest-json").
// Errors are returned to the caller to be logged and suppressed — this upload
// is best-effort and must not fail the build.
//
// Transient failures (network errors, 429, 5xx) are retried via doWithRetry
// before giving up. The multipart body is built once and re-sent on each attempt.
func (c *Client) UploadTestResults(ctx context.Context, token string, filePath string, format string, locationPrefix string, tags map[string]string) error {
	body, contentType, err := buildTestResultsMultipartBody(filePath, format, locationPrefix, tags)
	if err != nil {
		return err
	}
	// Keep the body content so newRequest can wrap it in a fresh bytes.Reader
	// for each attempt. We can't reuse one reader across retries because a reader
	// is drained once it's read, leaving nothing to re-send.
	bodyBytes := body.Bytes()

	uploadURL := strings.TrimRight(c.UploadBaseURL, "/") + "/v1/uploads"

	newRequest := func(reqCtx context.Context) (*http.Request, error) {
		req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, uploadURL, bytes.NewReader(bodyBytes))
		if err != nil {
			return nil, fmt.Errorf("creating upload request: %w", err)
		}
		req.Header.Set("Authorization", fmt.Sprintf("Token token=%s", token))
		req.Header.Set("Content-Type", contentType)
		return req, nil
	}

	// Upload requests are usually faster than Test Plan requests, so each attempt
	// uses a shorter timeout (5s instead of 15s).
	// This is a best-effort call, so retries also use a smaller total budget
	// (30s instead of 130s) to avoid delaying the build.
	resp, err := c.doWithRetry(ctx, 5*time.Second, uploadRetryTimeout, newRequest)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("upload failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

func buildTestResultsMultipartBody(filePath string, format string, locationPrefix string, tags map[string]string) (*bytes.Buffer, string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	if err := w.WriteField("format", format); err != nil {
		return nil, "", fmt.Errorf("writing format field: %w", err)
	}

	buildID := os.Getenv("BUILDKITE_BUILD_ID")
	runEnv := map[string]string{
		"CI":              "buildkite",
		"key":             buildID,
		"branch":          os.Getenv("BUILDKITE_BRANCH"),
		"commit_sha":      os.Getenv("BUILDKITE_COMMIT"),
		"number":          os.Getenv("BUILDKITE_BUILD_NUMBER"),
		"url":             os.Getenv("BUILDKITE_BUILD_URL"),
		"job_id":          os.Getenv("BUILDKITE_JOB_ID"),
		"message":         os.Getenv("BUILDKITE_MESSAGE"),
		"location_prefix": locationPrefix,
	}
	if buildID != "" {
		cwd, _ := os.Getwd()
		runEnv["cwd"] = cwd
	}
	for k, v := range runEnv {
		if err := w.WriteField(fmt.Sprintf("run_env[%s]", k), v); err != nil {
			return nil, "", fmt.Errorf("writing run_env[%s]: %w", k, err)
		}
	}

	for k, v := range tags {
		if err := w.WriteField(fmt.Sprintf("tags[%s]", k), v); err != nil {
			return nil, "", fmt.Errorf("writing tags[%s]: %w", k, err)
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
