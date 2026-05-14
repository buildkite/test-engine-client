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
	"sync/atomic"
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

// writePresignedUploadJSON writes the API response for the presigned-upload
// endpoint, pointing the form at s3URL.
func writePresignedUploadJSON(w http.ResponseWriter, s3URL string) {
	json.NewEncoder(w).Encode(map[string]interface{}{
		"uri": "s3://bucket/test.tar.gz",
		"form": map[string]interface{}{
			"method":     "POST",
			"url":        s3URL,
			"data":       map[string]string{"key": "test.tar.gz"},
			"file_input": "file",
		},
	})
}

func TestBackfillCommitMetadata_HappyPath(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
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
		case "/v2/analytics/organizations/my-org/suites/my-suite/commits":
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte("missing111\n"))
		case "/v2/analytics/organizations/my-org/suites/my-suite/commit-metadata-backfill/presigned-upload":
			// The preflight runs before the git filter step, so we still need
			// to mock it even though no commits will be processed locally.
			writePresignedUploadJSON(w, "http://unused.example/")
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
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal server error"))
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

	var presignHits int32

	// API server
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/analytics/organizations/my-org/suites/my-suite/commits":
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte("abc123\n"))

		case "/v2/analytics/organizations/my-org/suites/my-suite/commit-metadata-backfill/presigned-upload":
			atomic.AddInt32(&presignHits, 1)
			writePresignedUploadJSON(w, s3Server.URL)

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
	// On the happy path the held URL is reused; a second PresignUpload call
	// would mean the held-response optimisation has regressed.
	if got := atomic.LoadInt32(&presignHits); got != 1 {
		t.Errorf("expected 1 PresignUpload call (held URL reused on happy path), got %d", got)
	}
}

// TestBackfillCommitMetadata_WriteScopeMissing_FailsAtPreflight asserts that a
// 403 from PresignUpload surfaces before any git command runs, so a token
// missing write_suites fails fast rather than after the git work.
func TestBackfillCommitMetadata_WriteScopeMissing_FailsAtPreflight(t *testing.T) {
	var (
		commitsCalled  bool
		presignCalled  bool
		gitRunnerCalls int32
	)

	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/analytics/organizations/my-org/suites/my-suite/commits":
			commitsCalled = true
			// read_suites succeeds: return a commit so we get past this stage.
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte("abc123\n"))

		case "/v2/analytics/organizations/my-org/suites/my-suite/commit-metadata-backfill/presigned-upload":
			presignCalled = true
			http.Error(w, `{"message":"missing required scope: write_suites"}`, http.StatusForbidden)

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer svr.Close()

	cfg := getBackfillConfig(svr.URL)

	runner := &countingGitRunner{calls: &gitRunnerCalls}

	err := BackfillCommitMetadata(context.Background(), cfg, runner)
	if err == nil {
		t.Fatal("expected error for missing write_suites scope, got nil")
	}
	if !strings.Contains(err.Error(), "presigning upload") {
		t.Errorf("expected presigning-upload error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "write_suites") {
		t.Errorf("expected write_suites in error, got: %v", err)
	}
	if !commitsCalled {
		t.Error("expected FetchCommitList to run before the preflight")
	}
	if !presignCalled {
		t.Error("expected PresignUpload to run as the write-scope preflight")
	}
	if got := atomic.LoadInt32(&gitRunnerCalls); got != 0 {
		t.Errorf("expected 0 git commands before preflight failure, got %d", got)
	}
}

// TestBackfillCommitMetadata_ReadScopeMissing_FailsAtFetchCommitList asserts
// that a 403 from FetchCommitList surfaces before any git command runs, so a
// token missing read_suites fails fast.
func TestBackfillCommitMetadata_ReadScopeMissing_FailsAtFetchCommitList(t *testing.T) {
	var gitRunnerCalls int32

	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/analytics/organizations/my-org/suites/my-suite/commits":
			http.Error(w, `{"message":"missing required scope: read_suites"}`, http.StatusForbidden)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer svr.Close()

	cfg := getBackfillConfig(svr.URL)

	runner := &countingGitRunner{calls: &gitRunnerCalls}

	err := BackfillCommitMetadata(context.Background(), cfg, runner)
	if err == nil {
		t.Fatal("expected error for missing read_suites scope, got nil")
	}
	if !strings.Contains(err.Error(), "fetching commit list") {
		t.Errorf("expected fetching-commit-list error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "read_suites") {
		t.Errorf("expected read_suites in error, got: %v", err)
	}
	if got := atomic.LoadInt32(&gitRunnerCalls); got != 0 {
		t.Errorf("expected 0 git commands when FetchCommitList rejects, got %d", got)
	}
}

// TestBackfillCommitMetadata_OutputSkipsPresignPreflight asserts that
// --output (write-tarball-locally) does not call PresignUpload, so a user
// who opts out of the upload is not blocked by a missing write_suites scope.
func TestBackfillCommitMetadata_OutputSkipsPresignPreflight(t *testing.T) {
	var presignHits int32

	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/analytics/organizations/my-org/suites/my-suite/commits":
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte("abc123\n"))
		case "/v2/analytics/organizations/my-org/suites/my-suite/commit-metadata-backfill/presigned-upload":
			atomic.AddInt32(&presignHits, 1)
			// If this is called, the test should fail, but respond plausibly
			// so the test failure surfaces as a clear assertion below rather
			// than as a downstream error.
			writePresignedUploadJSON(w, "http://unused.example/")
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer svr.Close()

	cfg := getBackfillConfig(svr.URL)
	cfg.Output = t.TempDir() + "/output.tar.gz"

	if err := BackfillCommitMetadata(context.Background(), cfg, newFakeGitRunner()); err != nil {
		t.Fatalf("BackfillCommitMetadata error: %v", err)
	}

	if got := atomic.LoadInt32(&presignHits); got != 0 {
		t.Errorf("expected 0 PresignUpload calls when --output is set, got %d", got)
	}
	if _, err := os.Stat(cfg.Output); err != nil {
		t.Errorf("expected output file to exist: %v", err)
	}
}

// TestBackfillCommitMetadata_RetriesOnS3Forbidden asserts that a 403 from S3
// on the held presigned URL triggers a refresh and a single retry, and that
// the retry succeeds.
func TestBackfillCommitMetadata_RetriesOnS3Forbidden(t *testing.T) {
	var (
		s3Attempts      int32
		uploadSucceeded bool
	)

	// First attempt: real "Policy expired." 403 captured from S3.
	// Second attempt: succeeds.
	s3Server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt := atomic.AddInt32(&s3Attempts, 1)
		if attempt == 1 {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<Error><Code>AccessDenied</Code><Message>Invalid according to Policy: Policy expired.</Message><RequestId>P7XG7F93RENQV3B6</RequestId><HostId>lZUQ47E9D7XbgAiipMfWIbgJj4WVl1/34BihhSMiDaHGEAjE0FtKZ9QAh4psrJ8S1tFRvaGcoBeuEUnICTnXu9c6uOWe7ELO</HostId></Error>`))
			return
		}
		io.Copy(io.Discard, r.Body)
		uploadSucceeded = true
		w.WriteHeader(http.StatusNoContent)
	}))
	defer s3Server.Close()

	var presignHits int32

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/analytics/organizations/my-org/suites/my-suite/commits":
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte("abc123\n"))
		case "/v2/analytics/organizations/my-org/suites/my-suite/commit-metadata-backfill/presigned-upload":
			atomic.AddInt32(&presignHits, 1)
			writePresignedUploadJSON(w, s3Server.URL)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer apiServer.Close()

	cfg := getBackfillConfig(apiServer.URL)

	if err := BackfillCommitMetadata(context.Background(), cfg, newFakeGitRunner()); err != nil {
		t.Fatalf("BackfillCommitMetadata error: %v", err)
	}

	if got := atomic.LoadInt32(&s3Attempts); got != 2 {
		t.Errorf("expected 2 S3 attempts (first 403, second succeeds), got %d", got)
	}
	if got := atomic.LoadInt32(&presignHits); got != 2 {
		t.Errorf("expected 2 PresignUpload calls (initial preflight + refresh), got %d", got)
	}
	if !uploadSucceeded {
		t.Error("expected second S3 attempt to receive the upload body")
	}
}

// TestBackfillCommitMetadata_S3PolicyConditionMismatchDoesNotRefresh asserts
// that a 400 from S3 (e.g. a policy condition mismatch) surfaces immediately
// rather than triggering a refresh-and-retry, since refreshing the presigned
// URL would not fix a malformed upload.
func TestBackfillCommitMetadata_S3PolicyConditionMismatchDoesNotRefresh(t *testing.T) {
	var (
		s3Attempts  int32
		presignHits int32
	)

	s3Server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&s3Attempts, 1)
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<Error><Code>AccessDenied</Code><Message>Invalid according to Policy: Policy Condition failed: ["eq", "$key", "test-policy.txt"]</Message><RequestId>0000000000000000</RequestId><HostId>HOST</HostId></Error>`))
	}))
	defer s3Server.Close()

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/analytics/organizations/my-org/suites/my-suite/commits":
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte("abc123\n"))
		case "/v2/analytics/organizations/my-org/suites/my-suite/commit-metadata-backfill/presigned-upload":
			atomic.AddInt32(&presignHits, 1)
			writePresignedUploadJSON(w, s3Server.URL)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer apiServer.Close()

	cfg := getBackfillConfig(apiServer.URL)

	err := BackfillCommitMetadata(context.Background(), cfg, newFakeGitRunner())
	if err == nil {
		t.Fatal("expected error for generic S3 403, got nil")
	}
	if got := atomic.LoadInt32(&s3Attempts); got != 1 {
		t.Errorf("expected 1 S3 attempt (no refresh on generic 403), got %d", got)
	}
	if got := atomic.LoadInt32(&presignHits); got != 1 {
		t.Errorf("expected 1 PresignUpload call (no refresh on generic 403), got %d", got)
	}
}

// --- Upload-only mode tests (--upload flag) ---

func getUploadConfig(serverURL string, filePath string) *config.Config {
	cfg := config.New()
	cfg.AccessToken = "test-token"
	cfg.OrganizationSlug = "my-org"
	cfg.SuiteSlug = "my-suite"
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
		case "/v2/analytics/organizations/my-org/suites/my-suite/commit-metadata-backfill/presigned-upload":
			writePresignedUploadJSON(w, s3Server.URL)
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
		w.WriteHeader(http.StatusNotFound)
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

func TestBackfillCommitMetadata_UploadOnly_MissingSuiteSlugFailsBeforeNetwork(t *testing.T) {
	// Pins the contract that uploadOnly's defensive guard catches an empty
	// suite slug before any network round trip. Today this can only be hit if
	// a caller skips cfg.ValidateForBackfillCommitMetadata(); the guard makes
	// the layer boundary safe regardless.

	var requestCount int
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer apiServer.Close()

	tmpFile, err := os.CreateTemp(t.TempDir(), "test-tarball-*.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	tmpFile.WriteString("fake tarball content")
	tmpFile.Close()

	cfg := getUploadConfig(apiServer.URL, tmpFile.Name())
	cfg.SuiteSlug = "" // bypass validation gate, exercise the in-command guard

	err = BackfillCommitMetadata(context.Background(), cfg, nil)
	if err == nil {
		t.Fatal("expected error for empty suite slug, got nil")
	}
	if !strings.Contains(err.Error(), "suite slug must not be blank") {
		t.Errorf("expected suite-slug error, got: %v", err)
	}
	if requestCount != 0 {
		t.Errorf("expected no network requests, got %d", requestCount)
	}
}

// TestBackfillCommitMetadata_UploadOnly_WriteScopeMissing_FailsAtPresignUpload
// asserts that on the --upload path a 403 from PresignUpload surfaces before
// the S3 upload runs.
func TestBackfillCommitMetadata_UploadOnly_WriteScopeMissing_FailsAtPresignUpload(t *testing.T) {
	var s3Hit int32

	s3Server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&s3Hit, 1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer s3Server.Close()

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/analytics/organizations/my-org/suites/my-suite/commit-metadata-backfill/presigned-upload":
			http.Error(w, `{"message":"missing required scope: write_suites"}`, http.StatusForbidden)
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
	if !strings.Contains(err.Error(), "presigning upload") {
		t.Errorf("expected presigning-upload error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "write_suites") {
		t.Errorf("expected write_suites in error, got: %v", err)
	}
	if got := atomic.LoadInt32(&s3Hit); got != 0 {
		t.Errorf("expected 0 S3 requests when preflight fails, got %d", got)
	}
}

// countingGitRunner records every invocation, for tests that assert no git
// commands ran. Returns empty output; callers that reach git work will fail
// for unrelated reasons and the counter assertion will surface the regression.
type countingGitRunner struct {
	calls *int32
}

func (r *countingGitRunner) Output(ctx context.Context, args ...string) (string, error) {
	atomic.AddInt32(r.calls, 1)
	return "", nil
}

func (r *countingGitRunner) OutputWithStdin(ctx context.Context, stdin string, args ...string) (string, error) {
	atomic.AddInt32(r.calls, 1)
	return "", nil
}
