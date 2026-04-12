package command

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/buildkite/test-engine-client/internal/config"
	"github.com/buildkite/test-engine-client/internal/git"
	"github.com/buildkite/test-engine-client/internal/packaging"
)

func getBackfillConfig(serverURL string) *config.Config {
	cfg := config.New()
	cfg.AccessToken = "test-token"
	cfg.OrganizationSlug = "my-org"
	cfg.SuiteSlug = "my-suite"
	cfg.ServerBaseUrl = serverURL
	cfg.Concurrency = 10
	cfg.Days = 90
	cfg.Remote = "origin"
	return &cfg
}

func newFakeGitRunner() *git.FakeGitRunner {
	record := "abc123\x1fdef456\x1fAlice\x1falice@example.com\x1f2026-03-15T10:00:00+00:00\x1fGitHub\x1fnoreply@github.com\x1f2026-03-15T10:00:00+00:00\x1fFix the thing"
	gitLogOutput := record + "\x1e"

	return &git.FakeGitRunner{
		Responses: map[string]string{
			// Default branch detection
			"symbolic-ref --short refs/remotes/origin/HEAD": "origin/main\n",
			// Mainline cache (--since matches default Days=90)
			"log --first-parent --since=90 days ago --format=%H %P origin/main": "abc123 def456\ndef456\n",
			// Fork-point + diffs for abc123
			"merge-base origin/main abc123":                "def456\n",
			"diff --no-ext-diff --name-only def456 abc123": "file1.go\n",
			"diff --no-ext-diff --numstat def456 abc123":   "10\t5\tfile1.go\n",
			"diff --no-ext-diff def456 abc123":             "diff --git a/file1.go...\n",
			"diff --no-ext-diff --raw def456 abc123":       ":100644 100644 aaa bbb M\tfile1.go\n",
		},
		StdinResponses: map[string]func(string) string{
			// cat-file: abc123 exists
			"cat-file --batch-check": func(_ string) string {
				return "abc123 commit 271\n"
			},
			// Bulk metadata
			fmt.Sprintf("log --no-walk --stdin --format=%s", git.MetadataFormat): func(_ string) string {
				return gitLogOutput
			},
		},
	}
}

// writeTokenScopes writes a JSON response for GET /v2/access-token with the given scopes.
func writeTokenScopes(w http.ResponseWriter, scopes ...string) {
	json.NewEncoder(w).Encode(map[string]interface{}{
		"uuid":   "token-uuid",
		"scopes": scopes,
	})
}

func TestBackfillCommitMetadata_HappyPath(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/access-token":
			writeTokenScopes(w, "read_suites", "write_suites")
		case "/v2/analytics/organizations/my-org/suites/my-suite/commits":
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte("abc123\n"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer svr.Close()

	cfg := getBackfillConfig(svr.URL)
	cfg.Output = t.TempDir() + "/output.tar.gz"

	runner := newFakeGitRunner()

	err := BackfillCommitMetadata(context.Background(), cfg, runner)
	if err != nil {
		t.Fatalf("BackfillCommitMetadata error: %v", err)
	}

	// Verify the output file exists and is a valid tarball
	files := packaging.ReadTarball(t, cfg.Output)

	if !packaging.HasTarEntry(files, "/commit-metadata.jsonl") {
		t.Error("tarball missing commit-metadata.jsonl")
	}
	if !packaging.HasTarEntry(files, "/metadata.json") {
		t.Error("tarball missing metadata.json")
	}

	// Verify JSONL content
	jsonl := packaging.FindTarEntry(t, files, "/commit-metadata.jsonl")
	lines := strings.Split(strings.TrimSpace(jsonl), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 JSONL line, got %d", len(lines))
	}

	var record packaging.CommitRecord
	if err := json.Unmarshal([]byte(lines[0]), &record); err != nil {
		t.Fatalf("parsing JSONL: %v", err)
	}
	if record.CommitSHA != "abc123" {
		t.Errorf("CommitSHA: got %q, want %q", record.CommitSHA, "abc123")
	}
	if record.AuthorName != "Alice" {
		t.Errorf("AuthorName: got %q, want %q", record.AuthorName, "Alice")
	}
	if record.FilesChanged != "file1.go" {
		t.Errorf("FilesChanged: got %q, want %q", record.FilesChanged, "file1.go")
	}
	if record.SchemaVersion != 1 {
		t.Errorf("SchemaVersion: got %d, want 1", record.SchemaVersion)
	}
}

func TestBackfillCommitMetadata_SkipDiffs(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/access-token":
			writeTokenScopes(w, "read_suites", "write_suites")
		case "/v2/analytics/organizations/my-org/suites/my-suite/commits":
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte("abc123\n"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer svr.Close()

	cfg := getBackfillConfig(svr.URL)
	cfg.Output = t.TempDir() + "/output.tar.gz"
	cfg.SkipDiffs = true

	runner := newFakeGitRunner()

	err := BackfillCommitMetadata(context.Background(), cfg, runner)
	if err != nil {
		t.Fatalf("BackfillCommitMetadata error: %v", err)
	}

	// Read and verify the output
	files := packaging.ReadTarball(t, cfg.Output)
	line := strings.TrimSpace(packaging.FindTarEntry(t, files, "/commit-metadata.jsonl"))

	if strings.Contains(line, `"git_diff"`) {
		t.Error("expected git_diff to be omitted with --skip-diffs")
	}
	if strings.Contains(line, `"git_diff_raw"`) {
		t.Error("expected git_diff_raw to be omitted with --skip-diffs")
	}
	// files_changed should still be present
	if !strings.Contains(line, `"files_changed"`) {
		t.Error("expected files_changed to still be present")
	}
}

func TestBackfillCommitMetadata_NoCommits(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/access-token":
			writeTokenScopes(w, "read_suites", "write_suites")
		case "/v2/analytics/organizations/my-org/suites/my-suite/commits":
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte(""))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer svr.Close()

	cfg := getBackfillConfig(svr.URL)

	err := BackfillCommitMetadata(context.Background(), cfg, nil)
	if err != nil {
		t.Fatalf("expected no error for empty commit list, got: %v", err)
	}
}

func TestBackfillCommitMetadata_AllCommitsMissing(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/access-token":
			writeTokenScopes(w, "read_suites", "write_suites")
		case "/v2/analytics/organizations/my-org/suites/my-suite/commits":
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte("missing111\n"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer svr.Close()

	cfg := getBackfillConfig(svr.URL)

	runner := &git.FakeGitRunner{
		Responses: map[string]string{
			"symbolic-ref --short refs/remotes/origin/HEAD":                     "origin/main\n",
			"log --first-parent --since=90 days ago --format=%H %P origin/main": "abc123 def456\n",
		},
		StdinResponses: map[string]func(string) string{
			"cat-file --batch-check": func(_ string) string {
				return "missing111 missing\n"
			},
		},
	}
	err := BackfillCommitMetadata(context.Background(), cfg, runner)
	if err != nil {
		t.Fatalf("expected no error when all commits missing, got: %v", err)
	}
}

func TestBackfillCommitMetadata_APIError(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/access-token":
			writeTokenScopes(w, "read_suites", "write_suites")
		default:
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("internal server error"))
		}
	}))
	defer svr.Close()

	cfg := getBackfillConfig(svr.URL)

	err := BackfillCommitMetadata(context.Background(), cfg, nil)
	if err == nil {
		t.Fatal("expected error for API failure, got nil")
	}
	if !strings.Contains(err.Error(), "fetching commit list") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestBackfillCommitMetadata_DaysParam(t *testing.T) {
	var receivedDays string
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/access-token":
			writeTokenScopes(w, "read_suites", "write_suites")
		case "/v2/analytics/organizations/my-org/suites/my-suite/commits":
			receivedDays = r.URL.Query().Get("days")
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte(""))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer svr.Close()

	cfg := getBackfillConfig(svr.URL)
	cfg.Days = 30

	err := BackfillCommitMetadata(context.Background(), cfg, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if receivedDays != "30" {
		t.Errorf("days param: got %q, want %q", receivedDays, "30")
	}
}

func TestBackfillCommitMetadata_Upload(t *testing.T) {
	var uploadReceived bool
	var uploadedFileContent string

	// S3 mock server (receives the actual upload)
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
				uploadedFileContent = string(data)
			}
			p.Close()
		}

		w.WriteHeader(http.StatusNoContent)
	}))
	defer s3Server.Close()

	// API server
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/access-token":
			writeTokenScopes(w, "read_suites", "write_suites")

		case "/v2/analytics/organizations/my-org/suites/my-suite/commits":
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte("abc123\n"))

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

	cfg := getBackfillConfig(apiServer.URL)
	// No Output set -- should upload to S3

	runner := newFakeGitRunner()

	err := BackfillCommitMetadata(context.Background(), cfg, runner)
	if err != nil {
		t.Fatalf("BackfillCommitMetadata error: %v", err)
	}

	if !uploadReceived {
		t.Error("expected upload to S3, but no upload was received")
	}
	if uploadedFileContent == "" {
		t.Error("uploaded file content was empty")
	}
}

func TestBackfillCommitMetadata_ScopeCheckFails(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/access-token":
			// Token has read_suites but missing write_suites
			writeTokenScopes(w, "read_suites")
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer svr.Close()

	cfg := getBackfillConfig(svr.URL)
	// No --output, so write_suites is required

	err := BackfillCommitMetadata(context.Background(), cfg, nil)
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

func TestBackfillCommitMetadata_ScopeCheckWarnsWithOutput(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/access-token":
			// Token only has read_suites (no write_suites)
			writeTokenScopes(w, "read_suites")
		case "/v2/analytics/organizations/my-org/suites/my-suite/commits":
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte("abc123\n"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer svr.Close()

	cfg := getBackfillConfig(svr.URL)
	cfg.Output = t.TempDir() + "/output.tar.gz"

	runner := newFakeGitRunner()

	// With --output set, missing write_suites should warn, not error
	err := BackfillCommitMetadata(context.Background(), cfg, runner)
	if err != nil {
		t.Fatalf("expected no error with --output (warn only), got: %v", err)
	}

	// Verify the output file was still created
	if _, err := os.Stat(cfg.Output); err != nil {
		t.Errorf("expected output file to exist: %v", err)
	}
}

func TestBackfillCommitMetadata_ScopeCheckFailsWithOutputMissingReadSuites(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/access-token":
			// Token has write_suites but NOT read_suites
			writeTokenScopes(w, "write_suites")
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer svr.Close()

	cfg := getBackfillConfig(svr.URL)
	cfg.Output = t.TempDir() + "/output.tar.gz"

	// Even with --output, missing read_suites is a hard error
	err := BackfillCommitMetadata(context.Background(), cfg, nil)
	if err == nil {
		t.Fatal("expected error for missing read_suites scope, got nil")
	}
	if !strings.Contains(err.Error(), "token scope check failed") {
		t.Errorf("expected scope check error, got: %v", err)
	}
}

// --- Upload-only mode tests (--upload flag) ---

func getUploadConfig(serverURL string, filePath string) *config.Config {
	cfg := config.New()
	cfg.AccessToken = "test-token"
	cfg.OrganizationSlug = "my-org"
	cfg.ServerBaseUrl = serverURL
	cfg.UploadFile = filePath
	return &cfg
}

func TestBackfillCommitMetadata_UploadOnly_HappyPath(t *testing.T) {
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

	err = BackfillCommitMetadata(context.Background(), cfg, nil)
	if err != nil {
		t.Fatalf("BackfillCommitMetadata --upload error: %v", err)
	}

	if !uploadReceived {
		t.Error("expected upload to S3, but no upload was received")
	}
	if uploadedContent != "fake tarball content" {
		t.Errorf("uploaded content: got %q, want %q", uploadedContent, "fake tarball content")
	}
}

func TestBackfillCommitMetadata_UploadOnly_FileNotFound(t *testing.T) {
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

	err := BackfillCommitMetadata(context.Background(), cfg, nil)
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
	if !strings.Contains(err.Error(), "file not found") {
		t.Errorf("expected 'file not found' in error, got: %v", err)
	}
}

func TestBackfillCommitMetadata_UploadOnly_ScopeCheckFails(t *testing.T) {
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

	err = BackfillCommitMetadata(context.Background(), cfg, nil)
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
