package command

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/buildkite/test-engine-client/internal/config"
)

func getUploadConfig(serverURL string, filePath string) *config.Config {
	cfg := config.New()
	cfg.AccessToken = "test-token"
	cfg.OrganizationSlug = "my-org"
	cfg.ServerBaseUrl = serverURL
	cfg.UploadFile = filePath
	return &cfg
}

func TestUploadCommitMetadata_HappyPath(t *testing.T) {
	var uploadReceived bool
	var uploadedContent string

	s3Server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uploadReceived = true

		_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil {
			t.Errorf("parsing content type: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

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
				uploadedContent = string(data)
			}
			p.Close()
		}

		w.WriteHeader(http.StatusNoContent)
	}))
	defer s3Server.Close()

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/access-token":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"uuid":   "token-uuid",
				"scopes": []string{"write_suites"},
			})
		case "/v2/analytics/organizations/my-org/commit-metadata-backfill/presigned-upload":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"uri": "s3://bucket/test.tar.gz",
				"form": map[string]interface{}{
					"method":     "POST",
					"url":        s3Server.URL,
					"data":       map[string]string{"key": "test.tar.gz"},
					"file_input": "file",
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer apiServer.Close()

	// Create a temp file to upload
	tmpFile, err := os.CreateTemp(t.TempDir(), "test-tarball-*.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	tmpFile.WriteString("fake tarball content")
	tmpFile.Close()

	cfg := getUploadConfig(apiServer.URL, tmpFile.Name())

	err = UploadCommitMetadata(context.Background(), cfg)
	if err != nil {
		t.Fatalf("UploadCommitMetadata error: %v", err)
	}

	if !uploadReceived {
		t.Error("expected upload to S3, but no upload was received")
	}
	if uploadedContent != "fake tarball content" {
		t.Errorf("uploaded content: got %q, want %q", uploadedContent, "fake tarball content")
	}
}

func TestUploadCommitMetadata_FileNotFound(t *testing.T) {
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/access-token":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"uuid":   "token-uuid",
				"scopes": []string{"write_suites"},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer apiServer.Close()

	cfg := getUploadConfig(apiServer.URL, "/nonexistent/file.tar.gz")

	err := UploadCommitMetadata(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
	if !strings.Contains(err.Error(), "file not found") {
		t.Errorf("expected 'file not found' in error, got: %v", err)
	}
}

func TestUploadCommitMetadata_ScopeCheckFails(t *testing.T) {
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/access-token":
			// Token missing write_suites
			json.NewEncoder(w).Encode(map[string]interface{}{
				"uuid":   "token-uuid",
				"scopes": []string{"read_suites"},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer apiServer.Close()

	tmpFile, err := os.CreateTemp(t.TempDir(), "test-tarball-*.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	tmpFile.WriteString("fake tarball content")
	tmpFile.Close()

	cfg := getUploadConfig(apiServer.URL, tmpFile.Name())

	err = UploadCommitMetadata(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected error for missing write_suites scope, got nil")
	}
	if !strings.Contains(err.Error(), "token scope check failed") {
		t.Errorf("expected scope check error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "write_suites") {
		t.Errorf("expected write_suites in error, got: %v", err)
	}
}
