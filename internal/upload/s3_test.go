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
// (TE-5834). The response body is the literal response captured from S3 in a
// sandbox account after waiting past the policy's `expiration` timestamp;
// keeping the real shape here means a future maintainer changing the matcher
// can't accidentally introduce a string the real S3 never sends.
func TestUploadToS3_ReturnsPresignedURLExpiredError(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<Error><Code>AccessDenied</Code><Message>Invalid according to Policy: Policy expired.</Message><RequestId>P7XG7F93RENQV3B6</RequestId><HostId>lZUQ47E9D7XbgAiipMfWIbgJj4WVl1/34BihhSMiDaHGEAjE0FtKZ9QAh4psrJ8S1tFRvaGcoBeuEUnICTnXu9c6uOWe7ELO</HostId></Error>`))
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
	if !strings.Contains(expired.Body, "Policy expired.") {
		t.Errorf("expired.Body should preserve raw S3 response, got: %s", expired.Body)
	}
}

// TestUploadToS3_PolicyConditionMismatchIsNotExpiredError uses the real-world
// shape of a non-expiry presigned-POST failure: when the submitted form
// violates a signed condition (e.g. the key doesn't match the policy's
// `eq $key` condition), S3 returns 400 Bad Request, not 403. Verified in a
// sandbox account.
//
// This is the case the retry path must not catch. If a future change widens
// the matcher to e.g. "any AccessDenied response", this test fails because
// (a) the status is 400, not the 403 the matcher gates on, and (b) the
// message string ("Policy Condition failed") isn't the expired marker.
func TestUploadToS3_PolicyConditionMismatchIsNotExpiredError(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<Error><Code>AccessDenied</Code><Message>Invalid according to Policy: Policy Condition failed: ["eq", "$key", "test-policy.txt"]</Message><RequestId>0000000000000000</RequestId><HostId>HOST</HostId></Error>`))
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
		t.Fatalf("did not expect PresignedURLExpiredError for policy condition mismatch (400), got: %v", err)
	}
}

// TestUploadToS3_GenericForbiddenIsNotExpiredError pins the matcher's
// behaviour for the hypothetical case of a 403 whose body doesn't contain
// the expired marker. This isn't a real S3 shape today (signature mismatch
// returns 400, not 403; bucket-policy denial would have a different code
// like "Forbidden" or "InvalidAccessKeyId") but the test pins the
// defensive: status 403 alone is not sufficient to trigger a refresh,
// the body marker is required too.
func TestUploadToS3_GenericForbiddenIsNotExpiredError(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<Error><Code>AccessDenied</Code><Message>The bucket policy does not allow this operation.</Message></Error>`))
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
