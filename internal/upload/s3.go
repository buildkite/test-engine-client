package upload

import (
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"sort"

	"github.com/buildkite/test-engine-client/internal/debug"
)

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
// Uses an io.Pipe to stream the multipart body without buffering the entire
// file in memory.
func UploadToS3(filePath string, form PresignedUploadForm) error {
	debug.Printf("Uploading %s to %s", filePath, form.URL)

	pr, pw := io.Pipe()
	writer := multipart.NewWriter(pw)

	// Write the multipart form in a goroutine so the pipe reader can
	// stream directly to the HTTP request body.
	errCh := make(chan error, 1)
	go func() {
		defer pw.Close()

		// Write form data fields in deterministic order for testability.
		keys := make([]string, 0, len(form.Data))
		for k := range form.Data {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		for _, k := range keys {
			if err := writer.WriteField(k, form.Data[k]); err != nil {
				errCh <- fmt.Errorf("writing form field %s: %w", k, err)
				return
			}
		}

		// Attach the file
		fw, err := writer.CreateFormFile(form.FileInput, filepath.Base(filePath))
		if err != nil {
			errCh <- fmt.Errorf("creating form file: %w", err)
			return
		}

		f, err := os.Open(filePath)
		if err != nil {
			errCh <- fmt.Errorf("opening file %s: %w", filePath, err)
			return
		}
		defer f.Close()

		if _, err := io.Copy(fw, f); err != nil {
			errCh <- fmt.Errorf("copying file content: %w", err)
			return
		}

		if err := writer.Close(); err != nil {
			errCh <- fmt.Errorf("closing multipart writer: %w", err)
			return
		}

		errCh <- nil
	}()

	req, err := http.NewRequest(form.Method, form.URL, pr)
	if err != nil {
		return fmt.Errorf("creating upload request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("uploading to S3: %w", err)
	}
	defer resp.Body.Close()

	// Check for write goroutine errors
	if writeErr := <-errCh; writeErr != nil {
		return writeErr
	}

	// S3 returns 204 No Content on success for presigned POSTs
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("S3 upload failed with status %d: %s", resp.StatusCode, string(body))
	}

	debug.Printf("Upload successful: %d", resp.StatusCode)
	return nil
}
