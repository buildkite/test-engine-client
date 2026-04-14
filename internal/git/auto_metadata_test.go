package git

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// --- ResolveBaseBranch tests ---

func TestResolveBaseBranch_ExplicitFromMetadata(t *testing.T) {
	runner := &FakeGitRunner{
		Responses: map[string]string{
			"rev-parse --verify origin/develop": "abc123\n",
		},
	}

	ref, err := ResolveBaseBranch(context.Background(), runner, "develop", "origin")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ref != "origin/develop" {
		t.Errorf("got %q, want %q", ref, "origin/develop")
	}
}

func TestResolveBaseBranch_ExplicitWithRemotePrefix(t *testing.T) {
	runner := &FakeGitRunner{
		Responses: map[string]string{
			"rev-parse --verify origin/main": "abc123\n",
		},
	}

	ref, err := ResolveBaseBranch(context.Background(), runner, "origin/main", "origin")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ref != "origin/main" {
		t.Errorf("got %q, want %q", ref, "origin/main")
	}
}

func TestResolveBaseBranch_EnvVar(t *testing.T) {
	t.Setenv("BUILDKITE_PULL_REQUEST_BASE_BRANCH", "main")

	runner := &FakeGitRunner{
		Responses: map[string]string{
			"rev-parse --verify origin/main": "abc123\n",
		},
	}

	ref, err := ResolveBaseBranch(context.Background(), runner, "", "origin")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ref != "origin/main" {
		t.Errorf("got %q, want %q", ref, "origin/main")
	}
}

func TestResolveBaseBranch_DetectDefault(t *testing.T) {
	t.Setenv("BUILDKITE_PULL_REQUEST_BASE_BRANCH", "")

	runner := &FakeGitRunner{
		Responses: map[string]string{
			"symbolic-ref --short refs/remotes/origin/HEAD": "origin/main\n",
		},
	}

	ref, err := ResolveBaseBranch(context.Background(), runner, "", "origin")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ref != "origin/main" {
		t.Errorf("got %q, want %q", ref, "origin/main")
	}
}

func TestResolveBaseBranch_AllFail(t *testing.T) {
	t.Setenv("BUILDKITE_PULL_REQUEST_BASE_BRANCH", "")

	runner := &FakeGitRunner{
		Responses: map[string]string{},
	}

	_, err := ResolveBaseBranch(context.Background(), runner, "", "origin")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "could not detect default branch") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestResolveBaseBranch_ExplicitRefNotFound(t *testing.T) {
	// Explicit ref doesn't exist, falls through to DetectDefaultBranch
	runner := &FakeGitRunner{
		Responses: map[string]string{
			// rev-parse --verify origin/nonexistent is missing -> error
			"symbolic-ref --short refs/remotes/origin/HEAD": "origin/main\n",
		},
	}

	ref, err := ResolveBaseBranch(context.Background(), runner, "nonexistent", "origin")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ref != "origin/main" {
		t.Errorf("got %q, want %q", ref, "origin/main")
	}
}

func TestResolveBaseBranch_ExplicitFailsEnvVarSucceeds(t *testing.T) {
	t.Setenv("BUILDKITE_PULL_REQUEST_BASE_BRANCH", "release/v2")

	runner := &FakeGitRunner{
		Responses: map[string]string{
			// explicit "nonexistent" fails (no response)
			"rev-parse --verify origin/release/v2": "abc123\n",
		},
	}

	ref, err := ResolveBaseBranch(context.Background(), runner, "nonexistent", "origin")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ref != "origin/release/v2" {
		t.Errorf("got %q, want %q", ref, "origin/release/v2")
	}
}

func TestResolveBaseBranch_EmptyRemote(t *testing.T) {
	runner := &FakeGitRunner{}

	_, err := ResolveBaseBranch(context.Background(), runner, "main", "")
	if err == nil {
		t.Fatal("expected error for empty remote, got nil")
	}
}

// --- Helpers ---

// buildRecord joins fields with fieldSeparator and appends recordSeparator,
// matching the output format of git log --format=MetadataFormat.
func buildRecord(fields ...string) string {
	return strings.Join(fields, fieldSeparator) + recordSeparator
}

// --- CollectPlanMetadata tests ---

func TestCollectPlanMetadata_HappyPath(t *testing.T) {
	t.Setenv("BUILDKITE_PIPELINE_SLUG", "my-pipeline")
	t.Setenv("BUILDKITE_BUILD_ID", "build-uuid-123")

	gitLogOutput := buildRecord("abc123", "def456 ghi789", "Alice", "alice@example.com", "2026-03-15T10:00:00+00:00", "GitHub", "noreply@github.com", "2026-03-15T10:00:00+00:00", "Fix the thing")

	runner := &FakeGitRunner{
		Responses: map[string]string{
			fmt.Sprintf("log -1 --format=%s", MetadataFormat): gitLogOutput,
			"merge-base origin/main HEAD":                     "aaa000\n",
			"diff --no-ext-diff --name-only aaa000 HEAD":      "file1.go\nfile2.go\n",
			"diff --no-ext-diff --numstat aaa000 HEAD":        "10\t5\tfile1.go\n3\t0\tfile2.go\n",
			"diff --no-ext-diff aaa000 HEAD":                  "diff --git a/file1.go b/file1.go\n",
			"diff --no-ext-diff --raw aaa000 HEAD":            ":100644 100644 aaa bbb M\tfile1.go\n",
			"branch --show-current":                           "feature/my-branch\n",
		},
	}

	got := CollectPlanMetadata(context.Background(), runner, "origin/main")

	want := map[string]string{
		"commit_sha":      "abc123",
		"parent_shas":     "def456 ghi789",
		"author_name":     "Alice",
		"author_email":    "alice@example.com",
		"author_date":     "2026-03-15T10:00:00+00:00",
		"committer_name":  "GitHub",
		"committer_email": "noreply@github.com",
		"committer_date":  "2026-03-15T10:00:00+00:00",
		"message":         "Fix the thing",
		"files_changed":   "file1.go\nfile2.go",
		"diff_stat":       "10\t5\tfile1.go\n3\t0\tfile2.go",
		"git_diff":        "diff --git a/file1.go b/file1.go",
		"git_diff_raw":    ":100644 100644 aaa bbb M\tfile1.go",
		"branch":          "feature/my-branch",
		"base_branch":     "origin/main",
		"pipeline_slug":   "my-pipeline",
		"build_uuid":      "build-uuid-123",
	}

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("CollectPlanMetadata mismatch (-want +got):\n%s", diff)
	}
}

func TestCollectPlanMetadata_NoBaseBranch(t *testing.T) {
	t.Setenv("BUILDKITE_PIPELINE_SLUG", "")
	t.Setenv("BUILDKITE_BUILD_ID", "")

	gitLogOutput := buildRecord("abc123", "", "Alice", "alice@example.com", "2026-03-15T10:00:00+00:00", "Alice", "alice@example.com", "2026-03-15T10:00:00+00:00", "Initial commit")

	runner := &FakeGitRunner{
		Responses: map[string]string{
			fmt.Sprintf("log -1 --format=%s", MetadataFormat): gitLogOutput,
			"branch --show-current":                           "main\n",
		},
	}

	got := CollectPlanMetadata(context.Background(), runner, "")

	// No base branch: diff fields, base_branch, parent_shas (root commit),
	// pipeline_slug, and build_uuid should all be absent.
	want := map[string]string{
		"commit_sha":      "abc123",
		"author_name":     "Alice",
		"author_email":    "alice@example.com",
		"author_date":     "2026-03-15T10:00:00+00:00",
		"committer_name":  "Alice",
		"committer_email": "alice@example.com",
		"committer_date":  "2026-03-15T10:00:00+00:00",
		"message":         "Initial commit",
		"branch":          "main",
	}

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("CollectPlanMetadata mismatch (-want +got):\n%s", diff)
	}
}

func TestCollectPlanMetadata_DiffFails(t *testing.T) {
	t.Setenv("BUILDKITE_PIPELINE_SLUG", "")
	t.Setenv("BUILDKITE_BUILD_ID", "")

	gitLogOutput := buildRecord("abc123", "def456", "Alice", "alice@example.com", "2026-03-15T10:00:00+00:00", "Alice", "alice@example.com", "2026-03-15T10:00:00+00:00", "Some commit")

	runner := &FakeGitRunner{
		Responses: map[string]string{
			fmt.Sprintf("log -1 --format=%s", MetadataFormat): gitLogOutput,
			// All diff commands missing -> will error
			"branch --show-current": "feature\n",
		},
	}

	metadata := CollectPlanMetadata(context.Background(), runner, "origin/main")

	// Commit metadata should be present
	if metadata["commit_sha"] != "abc123" {
		t.Errorf("commit_sha: got %q, want %q", metadata["commit_sha"], "abc123")
	}

	// Diff fields should be absent (empty values filtered at merge)
	diffFields := []string{"files_changed", "diff_stat", "git_diff", "git_diff_raw"}
	for _, key := range diffFields {
		if _, ok := metadata[key]; ok {
			t.Errorf("expected key %q to be absent when diff fails, but found value %q", key, metadata[key])
		}
	}

	// base_branch should still be present (set from input, not from git)
	if metadata["base_branch"] != "origin/main" {
		t.Errorf("base_branch: got %q, want %q", metadata["base_branch"], "origin/main")
	}
}

func TestCollectPlanMetadata_LogFails(t *testing.T) {
	t.Setenv("BUILDKITE_PIPELINE_SLUG", "")
	t.Setenv("BUILDKITE_BUILD_ID", "")

	runner := &FakeGitRunner{
		Responses: map[string]string{
			// git log missing -> will error
			"merge-base origin/main HEAD":                "aaa000\n",
			"diff --no-ext-diff --name-only aaa000 HEAD": "file1.go\n",
			"diff --no-ext-diff --numstat aaa000 HEAD":   "10\t5\tfile1.go\n",
			"diff --no-ext-diff aaa000 HEAD":             "diff text\n",
			"diff --no-ext-diff --raw aaa000 HEAD":       ":100644 raw\n",
			"branch --show-current":                      "feature\n",
		},
	}

	metadata := CollectPlanMetadata(context.Background(), runner, "origin/main")

	// Commit metadata should be absent
	commitFields := []string{"commit_sha", "author_name", "author_email", "author_date",
		"committer_name", "committer_email", "committer_date", "message"}
	for _, key := range commitFields {
		if _, ok := metadata[key]; ok {
			t.Errorf("expected key %q to be absent when git log fails, but found value %q", key, metadata[key])
		}
	}

	// Diff fields should still be present
	if metadata["files_changed"] != "file1.go" {
		t.Errorf("files_changed: got %q, want %q", metadata["files_changed"], "file1.go")
	}

	// Context fields should still be present
	if metadata["branch"] != "feature" {
		t.Errorf("branch: got %q, want %q", metadata["branch"], "feature")
	}
	if metadata["base_branch"] != "origin/main" {
		t.Errorf("base_branch: got %q, want %q", metadata["base_branch"], "origin/main")
	}
}

func TestCollectPlanMetadata_DetachedHead(t *testing.T) {
	t.Setenv("BUILDKITE_PIPELINE_SLUG", "")
	t.Setenv("BUILDKITE_BUILD_ID", "")

	gitLogOutput := buildRecord("abc123", "def456", "Alice", "alice@example.com", "2026-03-15T10:00:00+00:00", "Alice", "alice@example.com", "2026-03-15T10:00:00+00:00", "Commit msg")

	runner := &FakeGitRunner{
		Responses: map[string]string{
			fmt.Sprintf("log -1 --format=%s", MetadataFormat): gitLogOutput,
			"branch --show-current":                           "\n", // empty on detached HEAD
		},
	}

	metadata := CollectPlanMetadata(context.Background(), runner, "")

	// branch key should be omitted when empty
	if _, ok := metadata["branch"]; ok {
		t.Errorf("expected branch key to be absent on detached HEAD, but found %q", metadata["branch"])
	}

	// Commit metadata should still work
	if metadata["commit_sha"] != "abc123" {
		t.Errorf("commit_sha: got %q, want %q", metadata["commit_sha"], "abc123")
	}
}

func TestCommitMetadata_ToMap(t *testing.T) {
	meta := CommitMetadata{
		CommitSHA:      "abc123",
		ParentSHAs:     []string{"def456", "ghi789"},
		AuthorName:     "Alice",
		AuthorEmail:    "alice@example.com",
		AuthorDate:     "2026-03-15T10:00:00+00:00",
		CommitterName:  "GitHub",
		CommitterEmail: "noreply@github.com",
		CommitterDate:  "2026-03-15T10:00:00+00:00",
		Message:        "Fix bug",
	}

	got := meta.ToMap()

	want := map[string]string{
		"commit_sha":      "abc123",
		"parent_shas":     "def456 ghi789",
		"author_name":     "Alice",
		"author_email":    "alice@example.com",
		"author_date":     "2026-03-15T10:00:00+00:00",
		"committer_name":  "GitHub",
		"committer_email": "noreply@github.com",
		"committer_date":  "2026-03-15T10:00:00+00:00",
		"message":         "Fix bug",
	}

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("ToMap() mismatch (-want +got):\n%s", diff)
	}
}

func TestCommitMetadata_ToMap_NoParents(t *testing.T) {
	meta := CommitMetadata{
		CommitSHA:      "abc123",
		ParentSHAs:     nil,
		AuthorName:     "Alice",
		AuthorEmail:    "alice@example.com",
		AuthorDate:     "2026-03-15T10:00:00+00:00",
		CommitterName:  "Alice",
		CommitterEmail: "alice@example.com",
		CommitterDate:  "2026-03-15T10:00:00+00:00",
		Message:        "Initial commit",
	}

	got := meta.ToMap()

	if got["parent_shas"] != "" {
		t.Errorf("parent_shas: got %q, want empty string for root commit", got["parent_shas"])
	}
	if got["commit_sha"] != "abc123" {
		t.Errorf("commit_sha: got %q, want %q", got["commit_sha"], "abc123")
	}
}

func TestCollectPlanMetadata_MultilineMessage(t *testing.T) {
	t.Setenv("BUILDKITE_PIPELINE_SLUG", "")
	t.Setenv("BUILDKITE_BUILD_ID", "")

	gitLogOutput := buildRecord("abc123", "def456", "Alice", "alice@example.com", "2026-03-15T10:00:00+00:00", "Alice", "alice@example.com", "2026-03-15T10:00:00+00:00", "Fix the thing\n\nThis is a longer description\nwith multiple lines.")

	runner := &FakeGitRunner{
		Responses: map[string]string{
			fmt.Sprintf("log -1 --format=%s", MetadataFormat): gitLogOutput,
			"branch --show-current":                           "main\n",
		},
	}

	metadata := CollectPlanMetadata(context.Background(), runner, "")

	want := "Fix the thing\n\nThis is a longer description\nwith multiple lines."
	if diff := cmp.Diff(want, metadata["message"]); diff != "" {
		t.Errorf("message mismatch (-want +got):\n%s", diff)
	}
}

func TestCollectPlanMetadata_EnvVarsPopulated(t *testing.T) {
	t.Setenv("BUILDKITE_PIPELINE_SLUG", "my-org/my-pipeline")
	t.Setenv("BUILDKITE_BUILD_ID", "abc-def-123")

	gitLogOutput := buildRecord("abc123", "", "Alice", "alice@example.com", "2026-03-15T10:00:00+00:00", "Alice", "alice@example.com", "2026-03-15T10:00:00+00:00", "Msg")

	runner := &FakeGitRunner{
		Responses: map[string]string{
			fmt.Sprintf("log -1 --format=%s", MetadataFormat): gitLogOutput,
			"branch --show-current":                           "main\n",
		},
	}

	metadata := CollectPlanMetadata(context.Background(), runner, "")

	if metadata["pipeline_slug"] != "my-org/my-pipeline" {
		t.Errorf("pipeline_slug: got %q, want %q", metadata["pipeline_slug"], "my-org/my-pipeline")
	}
	if metadata["build_uuid"] != "abc-def-123" {
		t.Errorf("build_uuid: got %q, want %q", metadata["build_uuid"], "abc-def-123")
	}
}

func TestCollectPlanMetadata_EmptyDiffOutput(t *testing.T) {
	// Simulates builds on the default branch where diff is empty
	t.Setenv("BUILDKITE_PIPELINE_SLUG", "")
	t.Setenv("BUILDKITE_BUILD_ID", "")

	gitLogOutput := buildRecord("abc123", "def456", "Alice", "alice@example.com", "2026-03-15T10:00:00+00:00", "Alice", "alice@example.com", "2026-03-15T10:00:00+00:00", "Merge commit")

	runner := &FakeGitRunner{
		Responses: map[string]string{
			fmt.Sprintf("log -1 --format=%s", MetadataFormat): gitLogOutput,
			"merge-base origin/main HEAD":                     "aaa000\n",
			"diff --no-ext-diff --name-only aaa000 HEAD":      "\n",
			"diff --no-ext-diff --numstat aaa000 HEAD":        "\n",
			"diff --no-ext-diff aaa000 HEAD":                  "\n",
			"diff --no-ext-diff --raw aaa000 HEAD":            "\n",
			"branch --show-current":                           "main\n",
		},
	}

	metadata := CollectPlanMetadata(context.Background(), runner, "origin/main")

	// Empty diffs should be omitted (mergeNonEmpty skips empty values)
	diffFields := []string{"files_changed", "diff_stat", "git_diff", "git_diff_raw"}
	for _, key := range diffFields {
		if _, ok := metadata[key]; ok {
			t.Errorf("expected key %q to be absent for empty diff, but found value %q", key, metadata[key])
		}
	}

	// Commit metadata should still be fully populated
	if metadata["commit_sha"] != "abc123" {
		t.Errorf("commit_sha: got %q, want %q", metadata["commit_sha"], "abc123")
	}
}

// --- MergeMetadata tests ---

func TestMergeMetadata_UserPrecedence(t *testing.T) {
	existing := map[string]string{
		"commit_sha": "user-provided-sha",
		"branch":     "user-branch",
	}
	auto := map[string]string{
		"commit_sha":  "auto-sha",
		"branch":      "auto-branch",
		"author_name": "Alice",
	}

	result := MergeMetadata(existing, auto)

	// User-provided values should win
	if result["commit_sha"] != "user-provided-sha" {
		t.Errorf("commit_sha: got %q, want %q", result["commit_sha"], "user-provided-sha")
	}
	if result["branch"] != "user-branch" {
		t.Errorf("branch: got %q, want %q", result["branch"], "user-branch")
	}
	// Auto-collected values should fill gaps
	if result["author_name"] != "Alice" {
		t.Errorf("author_name: got %q, want %q", result["author_name"], "Alice")
	}
}

func TestMergeMetadata_NilExisting(t *testing.T) {
	auto := map[string]string{
		"commit_sha":  "abc123",
		"author_name": "Alice",
	}

	result := MergeMetadata(nil, auto)

	if result["commit_sha"] != "abc123" {
		t.Errorf("commit_sha: got %q, want %q", result["commit_sha"], "abc123")
	}
	if result["author_name"] != "Alice" {
		t.Errorf("author_name: got %q, want %q", result["author_name"], "Alice")
	}
}

func TestMergeMetadata_SkipsEmptyAutoValues(t *testing.T) {
	existing := map[string]string{
		"branch": "main",
	}
	auto := map[string]string{
		"commit_sha": "abc123",
		"git_diff":   "",
	}

	result := MergeMetadata(existing, auto)

	if result["commit_sha"] != "abc123" {
		t.Errorf("commit_sha: got %q, want %q", result["commit_sha"], "abc123")
	}
	if _, ok := result["git_diff"]; ok {
		t.Errorf("expected git_diff to be absent for empty auto value, got %q", result["git_diff"])
	}
}
