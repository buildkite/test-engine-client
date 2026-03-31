package command

import (
	"archive/tar"
	"compress/gzip"
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

// fakeGitRunner returns canned responses for the backfill test.
type fakeGitRunner struct {
	responses      map[string]string
	stdinResponses map[string]func(string) string
}

func (f *fakeGitRunner) key(args []string) string {
	return strings.Join(args, " ")
}

func (f *fakeGitRunner) Output(ctx context.Context, args ...string) (string, error) {
	k := f.key(args)
	if resp, ok := f.responses[k]; ok {
		return resp, nil
	}
	return "", fmt.Errorf("fakeGitRunner: no response for %q", k)
}

func (f *fakeGitRunner) OutputWithStdin(ctx context.Context, stdin string, args ...string) (string, error) {
	k := f.key(args)
	if fn, ok := f.stdinResponses[k]; ok {
		return fn(stdin), nil
	}
	if resp, ok := f.responses[k]; ok {
		return resp, nil
	}
	return "", fmt.Errorf("fakeGitRunner: no response for %q (with stdin)", k)
}

func setGitRunnerFactory(t *testing.T, runner git.GitRunner) {
	t.Helper()
	orig := gitRunnerFactory
	gitRunnerFactory = func() git.GitRunner { return runner }
	t.Cleanup(func() {
		gitRunnerFactory = orig
	})
}

func getBackfillConfig(serverURL string) *config.Config {
	cfg := config.New()
	cfg.AccessToken = "test-token"
	cfg.OrganizationSlug = "my-org"
	cfg.SuiteSlug = "my-suite"
	cfg.ServerBaseUrl = serverURL
	cfg.Days = 90
	return &cfg
}

// metadataFormat matches the constant in internal/git/metadata.go
const testMetadataFormat = "%H%x1f%P%x1f%an%x1f%ae%x1f%aI%x1f%cn%x1f%ce%x1f%cI%x1f%B%x1e"

func newFakeGitRunner() *fakeGitRunner {
	record := "abc123\x1fdef456\x1fAlice\x1falice@example.com\x1f2026-03-15T10:00:00+00:00\x1fGitHub\x1fnoreply@github.com\x1f2026-03-15T10:00:00+00:00\x1fFix the thing"
	gitLogOutput := record + "\x1e"

	return &fakeGitRunner{
		responses: map[string]string{
			// Default branch detection
			"symbolic-ref --short refs/remotes/origin/HEAD": "origin/main\n",
			// Mainline cache
			"log --first-parent --format=%H %P origin/main": "abc123 def456\ndef456\n",
			// Fork-point + diffs for abc123
			"merge-base origin/main abc123":                "def456\n",
			"diff --no-ext-diff --name-only def456 abc123": "file1.go\n",
			"diff --no-ext-diff --numstat def456 abc123":   "10\t5\tfile1.go\n",
			"diff --no-ext-diff def456 abc123":             "diff --git a/file1.go...\n",
			"diff --no-ext-diff --raw def456 abc123":       ":100644 100644 aaa bbb M\tfile1.go\n",
		},
		stdinResponses: map[string]func(string) string{
			// cat-file: abc123 exists
			"cat-file --batch-check": func(_ string) string {
				return "abc123 commit 271\n"
			},
			// Bulk metadata
			fmt.Sprintf("log --no-walk --stdin --format=%s", testMetadataFormat): func(_ string) string {
				return gitLogOutput
			},
		},
	}
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
	setGitRunnerFactory(t, runner)

	err := BackfillCommitMetadata(context.Background(), cfg)
	if err != nil {
		t.Fatalf("BackfillCommitMetadata error: %v", err)
	}

	// Verify the output file exists and is a valid tarball
	f, err := os.Open(cfg.Output)
	if err != nil {
		t.Fatalf("opening output: %v", err)
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	files := make(map[string]string)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("reading tar: %v", err)
		}
		data, _ := io.ReadAll(tr)
		files[hdr.Name] = string(data)
	}

	if _, ok := files["commit-metadata.jsonl"]; !ok {
		t.Error("tarball missing commit-metadata.jsonl")
	}
	if _, ok := files["metadata.json"]; !ok {
		t.Error("tarball missing metadata.json")
	}

	// Verify JSONL content
	jsonl := files["commit-metadata.jsonl"]
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
		if r.URL.Path == "/v2/analytics/organizations/my-org/suites/my-suite/commits" {
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte("abc123\n"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer svr.Close()

	cfg := getBackfillConfig(svr.URL)
	cfg.Output = t.TempDir() + "/output.tar.gz"
	cfg.SkipDiffs = true

	runner := newFakeGitRunner()
	setGitRunnerFactory(t, runner)

	err := BackfillCommitMetadata(context.Background(), cfg)
	if err != nil {
		t.Fatalf("BackfillCommitMetadata error: %v", err)
	}

	// Read and verify the output
	f, _ := os.Open(cfg.Output)
	defer f.Close()
	gzr, _ := gzip.NewReader(f)
	defer gzr.Close()
	tr := tar.NewReader(gzr)

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if hdr.Name == "commit-metadata.jsonl" {
			data, _ := io.ReadAll(tr)
			line := strings.TrimSpace(string(data))
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
	}
}

func TestBackfillCommitMetadata_NoCommits(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/analytics/organizations/my-org/suites/my-suite/commits" {
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte(""))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer svr.Close()

	cfg := getBackfillConfig(svr.URL)

	err := BackfillCommitMetadata(context.Background(), cfg)
	if err != nil {
		t.Fatalf("expected no error for empty commit list, got: %v", err)
	}
}

func TestBackfillCommitMetadata_AllCommitsMissing(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/analytics/organizations/my-org/suites/my-suite/commits" {
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte("missing111\n"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer svr.Close()

	cfg := getBackfillConfig(svr.URL)

	runner := &fakeGitRunner{
		responses: map[string]string{
			"symbolic-ref --short refs/remotes/origin/HEAD": "origin/main\n",
			"log --first-parent --format=%H %P origin/main": "abc123 def456\n",
		},
		stdinResponses: map[string]func(string) string{
			"cat-file --batch-check": func(_ string) string {
				return "missing111 missing\n"
			},
		},
	}
	setGitRunnerFactory(t, runner)

	err := BackfillCommitMetadata(context.Background(), cfg)
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

	err := BackfillCommitMetadata(context.Background(), cfg)
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
		if r.URL.Path == "/v2/analytics/organizations/my-org/suites/my-suite/commits" {
			receivedDays = r.URL.Query().Get("days")
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte(""))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer svr.Close()

	cfg := getBackfillConfig(svr.URL)
	cfg.Days = 30

	err := BackfillCommitMetadata(context.Background(), cfg)
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
	setGitRunnerFactory(t, runner)

	err := BackfillCommitMetadata(context.Background(), cfg)
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
