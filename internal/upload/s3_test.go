package upload

import (
	"context"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func createTempFile(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "upload-test-*")
	if err != nil {
		t.Fatalf("creating temp file: %v", err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("writing temp file: %v", err)
	}
	f.Close()
	return f.Name()
}

func TestUploadToS3_SendsFormFields(t *testing.T) {
	var receivedFields map[string]string

	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || !strings.HasPrefix(mediaType, "multipart/") {
			t.Errorf("expected multipart content type, got %q", r.Header.Get("Content-Type"))
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		mr := multipart.NewReader(r.Body, params["boundary"])
		receivedFields = make(map[string]string)
		for {
			p, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Errorf("reading part: %v", err)
				break
			}
			if p.FileName() == "" {
				data, _ := io.ReadAll(p)
				receivedFields[p.FormName()] = string(data)
			}
			p.Close()
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer svr.Close()

	tmpFile := createTempFile(t, "file content")

	form := PresignedUploadForm{
		Method:    "POST",
		URL:       svr.URL,
		Data:      map[string]string{"key": "backfill/test.tar.gz", "acl": "private", "policy": "abc123"},
		FileInput: "file",
	}

	if err := UploadToS3(context.Background(), tmpFile, form); err != nil {
		t.Fatalf("UploadToS3 error: %v", err)
	}

	for k, v := range form.Data {
		if receivedFields[k] != v {
			t.Errorf("field %q: got %q, want %q", k, receivedFields[k], v)
		}
	}
}

func TestUploadToS3_SendsFile(t *testing.T) {
	fileContent := "this is the tarball content"
	var receivedFile string
	var receivedFilename string

	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		mr := multipart.NewReader(r.Body, params["boundary"])
		for {
			p, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				break
			}
			if p.FileName() != "" {
				data, _ := io.ReadAll(p)
				receivedFile = string(data)
				receivedFilename = p.FileName()
			}
			p.Close()
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer svr.Close()

	tmpFile := createTempFile(t, fileContent)

	form := PresignedUploadForm{
		Method:    "POST",
		URL:       svr.URL,
		Data:      map[string]string{"key": "test.tar.gz"},
		FileInput: "file",
	}

	if err := UploadToS3(context.Background(), tmpFile, form); err != nil {
		t.Fatalf("UploadToS3 error: %v", err)
	}

	if receivedFile != fileContent {
		t.Errorf("file content: got %q, want %q", receivedFile, fileContent)
	}
	if receivedFilename != filepath.Base(tmpFile) {
		t.Errorf("filename: got %q, want %q", receivedFilename, filepath.Base(tmpFile))
	}
}

func TestUploadToS3_FieldOrder(t *testing.T) {
	// S3 requires form data fields before the file field
	var fieldOrder []string

	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		mr := multipart.NewReader(r.Body, params["boundary"])
		for {
			p, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				break
			}
			if p.FileName() != "" {
				fieldOrder = append(fieldOrder, "<file>")
			} else {
				fieldOrder = append(fieldOrder, p.FormName())
			}
			p.Close()
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer svr.Close()

	tmpFile := createTempFile(t, "content")

	form := PresignedUploadForm{
		Method:    "POST",
		URL:       svr.URL,
		Data:      map[string]string{"key": "test.tar.gz", "acl": "private"},
		FileInput: "file",
	}

	if err := UploadToS3(context.Background(), tmpFile, form); err != nil {
		t.Fatalf("UploadToS3 error: %v", err)
	}

	// All data fields should come before the file
	fileIdx := -1
	for i, name := range fieldOrder {
		if name == "<file>" {
			fileIdx = i
			break
		}
	}
	if fileIdx == -1 {
		t.Fatal("file field not found in multipart form")
	}
	if fileIdx != len(fieldOrder)-1 {
		t.Errorf("file should be last field, but was at index %d of %d", fileIdx, len(fieldOrder)-1)
	}
}

func TestUploadToS3_ServerError(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte("Access Denied"))
	}))
	defer svr.Close()

	tmpFile := createTempFile(t, "content")

	form := PresignedUploadForm{
		Method:    "POST",
		URL:       svr.URL,
		Data:      map[string]string{"key": "test.tar.gz"},
		FileInput: "file",
	}

	err := UploadToS3(context.Background(), tmpFile, form)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("expected 403 in error, got: %v", err)
	}
}

// TestUploadToS3_ReturnsPresignedURLExpiredError pins the typed-error
// contract that BackfillCommitMetadata's expired-URL refresh path relies on
// (TE-5834). S3 returns 403 with an XML body containing "Request has expired"
// when a presigned URL is used past its signature TTL; we surface that as a
// distinguishable PresignedURLExpiredError so callers can request a fresh URL
// and retry, while leaving other 403 cases (bucket policy, etc.) as opaque
// errors to avoid masking real configuration problems behind an unbounded
// retry loop.
func TestUploadToS3_ReturnsPresignedURLExpiredError(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<Error>
  <Code>AccessDenied</Code>
  <Message>Request has expired</Message>
  <Expires>2026-05-14T10:00:00Z</Expires>
  <ServerTime>2026-05-14T11:30:00Z</ServerTime>
</Error>`))
	}))
	defer svr.Close()

	tmpFile := createTempFile(t, "content")

	form := PresignedUploadForm{
		Method:    "POST",
		URL:       svr.URL,
		Data:      map[string]string{"key": "test.tar.gz"},
		FileInput: "file",
	}

	err := UploadToS3(context.Background(), tmpFile, form)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var expired *PresignedURLExpiredError
	if !errors.As(err, &expired) {
		t.Fatalf("expected PresignedURLExpiredError, got %T: %v", err, err)
	}
	if expired.Status != http.StatusForbidden {
		t.Errorf("expired.Status: got %d, want %d", expired.Status, http.StatusForbidden)
	}
	if !strings.Contains(expired.Body, "Request has expired") {
		t.Errorf("expired.Body should preserve raw S3 response, got: %s", expired.Body)
	}
}

// TestUploadToS3_GenericForbiddenIsNotExpiredError pins the negative side of
// the typed-error contract: a 403 without the "Request has expired" marker
// must not be classified as PresignedURLExpiredError. This is what stops the
// refresh-and-retry path from masking legitimate auth or bucket-policy
// failures.
func TestUploadToS3_GenericForbiddenIsNotExpiredError(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<Error>
  <Code>AccessDenied</Code>
  <Message>The bucket policy does not allow this operation.</Message>
</Error>`))
	}))
	defer svr.Close()

	tmpFile := createTempFile(t, "content")

	form := PresignedUploadForm{
		Method:    "POST",
		URL:       svr.URL,
		Data:      map[string]string{"key": "test.tar.gz"},
		FileInput: "file",
	}

	err := UploadToS3(context.Background(), tmpFile, form)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var expired *PresignedURLExpiredError
	if errors.As(err, &expired) {
		t.Fatalf("did not expect PresignedURLExpiredError for generic 403, got: %v", err)
	}
}

func TestUploadToS3_FileNotFound(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer svr.Close()

	form := PresignedUploadForm{
		Method:    "POST",
		URL:       svr.URL,
		Data:      map[string]string{"key": "test.tar.gz"},
		FileInput: "file",
	}

	err := UploadToS3(context.Background(), "/nonexistent/file.tar.gz", form)
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}
