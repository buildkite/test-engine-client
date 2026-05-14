package upload

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/buildkite/test-engine-client/internal/debug"
)

// S3ForbiddenError is returned when S3 rejects the upload with 403 Forbidden.
// The common case is "the presigned POST policy has expired", which is
// recoverable by re-fetching a fresh presigned URL and retrying. Less common
// permanent 403s (bucket-policy denial, VPC endpoint policy, etc.) also land
// here -- a single retry against a fresh URL is harmless in those cases (it
// just produces the same error a second time and then surfaces).
//
// 4xx responses other than 403 -- in particular the 400 Bad Request that S3
// returns for policy-condition mismatches and signature mismatches -- do not
// match this type; they surface as the generic upload error.
//
// Callers can use errors.As to detect this case. The Body field preserves the
// raw S3 response so the original error remains diagnosable in logs.
type S3ForbiddenError struct {
	Status int
	Body   string
}

func (e *S3ForbiddenError) Error() string {
	return fmt.Sprintf("S3 upload rejected with status %d: %s", e.Status, e.Body)
}

// PresignedUploadForm describes the S3 presigned POST form returned by the
// Buildkite API. All Data fields must be sent as form fields before the file.
type PresignedUploadForm struct {
	Method    string            `json:"method"`
	URL       string            `json:"url"`
	Data      map[string]string `json:"data"`
	FileInput string            `json:"file_input"`
}

// UploadToS3 uploads a file to S3 using a presigned POST form.
//
// The presigned form fields handle S3 authorisation. No bearer token or
// additional auth headers are needed -- the POST goes directly to S3.
//
// The form data fields must be sent BEFORE the file field. S3 requires this
// ordering for presigned POSTs.
//
// The multipart body is buffered in memory so the HTTP client can set the
// Content-Length header, which S3 requires for presigned POST uploads.
func UploadToS3(ctx context.Context, filePath string, form PresignedUploadForm) error {
	debug.Printf("Uploading %s to %s", filePath, form.URL)

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	// Write form data fields in deterministic order for testability.
	keys := make([]string, 0, len(form.Data))
	for k := range form.Data {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		if err := writer.WriteField(k, form.Data[k]); err != nil {
			return fmt.Errorf("writing form field %s: %w", k, err)
		}
	}

	// Attach the file
	fw, err := writer.CreateFormFile(form.FileInput, filepath.Base(filePath))
	if err != nil {
		return fmt.Errorf("creating form file: %w", err)
	}

	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("opening file %s: %w", filePath, err)
	}
	defer f.Close()

	if _, err := io.Copy(fw, f); err != nil {
		return fmt.Errorf("copying file content: %w", err)
	}

	if err := writer.Close(); err != nil {
		return fmt.Errorf("closing multipart writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, form.Method, form.URL, &buf)
	if err != nil {
		return fmt.Errorf("creating upload request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("uploading to S3: %w", err)
	}
	defer resp.Body.Close()

	// S3 returns 204 No Content on success for presigned POSTs
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		bodyStr := string(body)

		// Classify any 403 as retryable. The common case is "presigned POST
		// policy expired", which is recoverable by requesting a fresh URL.
		// The previous version of this matcher pattern-matched on the
		// literal S3 message text ("Policy expired."), which is brittle --
		// AWS does not version that string, and a wording change would
		// silently disable the retry path. Status alone is a more durable
		// signal, and the cases we'd want to exclude (policy condition
		// mismatch, signature mismatch) return 400 Bad Request, not 403,
		// so they fall through to the generic error path below. A 403 that
		// is genuinely permanent (e.g. bucket-policy denial) costs one
		// wasted retry against a fresh URL, then surfaces.
		if resp.StatusCode == http.StatusForbidden {
			return &S3ForbiddenError{Status: resp.StatusCode, Body: bodyStr}
		}

		return fmt.Errorf("S3 upload failed with status %d: %s", resp.StatusCode, bodyStr)
	}

	debug.Printf("Upload successful: %d", resp.StatusCode)
	return nil
}
