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

// --- CollectPlanMetadata tests ---

func TestCollectPlanMetadata_HappyPath(t *testing.T) {
	t.Setenv("BUILDKITE_PIPELINE_SLUG", "my-pipeline")
	t.Setenv("BUILDKITE_BUILD_ID", "build-uuid-123")

	record := "abc123\x1fdef456 ghi789\x1fAlice\x1falice@example.com\x1f2026-03-15T10:00:00+00:00\x1fGitHub\x1fnoreply@github.com\x1f2026-03-15T10:00:00+00:00\x1fFix the thing"
	gitLogOutput := record + "\x1e"

	runner := &FakeGitRunner{
		Responses: map[string]string{
			fmt.Sprintf("log -1 --format=%s", MetadataFormat):   gitLogOutput,
			"diff --no-ext-diff --name-only origin/main...HEAD": "file1.go\nfile2.go\n",
			"diff --no-ext-diff --numstat origin/main...HEAD":   "10\t5\tfile1.go\n3\t0\tfile2.go\n",
			"diff --no-ext-diff origin/main...HEAD":             "diff --git a/file1.go b/file1.go\n",
			"diff --no-ext-diff --raw origin/main...HEAD":       ":100644 100644 aaa bbb M\tfile1.go\n",
			"branch --show-current":                             "feature/my-branch\n",
		},
	}

	metadata := CollectPlanMetadata(context.Background(), runner, "origin/main")

	// All 15 fields should be populated (parent_shas counted as 1 field)
	expectedKeys := []string{
		"commit_sha", "parent_shas", "author_name", "author_email", "author_date",
		"committer_name", "committer_email", "committer_date", "message",
		"files_changed", "diff_stat", "git_diff", "git_diff_raw",
		"branch", "base_branch", "pipeline_slug", "build_uuid",
	}
	for _, key := range expectedKeys {
		if _, ok := metadata[key]; !ok {
			t.Errorf("expected key %q in metadata, but not found", key)
		}
	}

	// Spot-check values
	if metadata["commit_sha"] != "abc123" {
		t.Errorf("commit_sha: got %q, want %q", metadata["commit_sha"], "abc123")
	}
	if metadata["parent_shas"] != "def456 ghi789" {
		t.Errorf("parent_shas: got %q, want %q", metadata["parent_shas"], "def456 ghi789")
	}
	if metadata["author_name"] != "Alice" {
		t.Errorf("author_name: got %q, want %q", metadata["author_name"], "Alice")
	}
	if metadata["author_email"] != "alice@example.com" {
		t.Errorf("author_email: got %q, want %q", metadata["author_email"], "alice@example.com")
	}
	if metadata["message"] != "Fix the thing" {
		t.Errorf("message: got %q, want %q", metadata["message"], "Fix the thing")
	}
	if metadata["files_changed"] != "file1.go\nfile2.go" {
		t.Errorf("files_changed: got %q, want %q", metadata["files_changed"], "file1.go\nfile2.go")
	}
	if metadata["diff_stat"] != "10\t5\tfile1.go\n3\t0\tfile2.go" {
		t.Errorf("diff_stat: got %q", metadata["diff_stat"])
	}
	if metadata["git_diff"] != "diff --git a/file1.go b/file1.go" {
		t.Errorf("git_diff: got %q", metadata["git_diff"])
	}
	if metadata["git_diff_raw"] != ":100644 100644 aaa bbb M\tfile1.go" {
		t.Errorf("git_diff_raw: got %q", metadata["git_diff_raw"])
	}
	if metadata["branch"] != "feature/my-branch" {
		t.Errorf("branch: got %q, want %q", metadata["branch"], "feature/my-branch")
	}
	if metadata["base_branch"] != "origin/main" {
		t.Errorf("base_branch: got %q, want %q", metadata["base_branch"], "origin/main")
	}
	if metadata["pipeline_slug"] != "my-pipeline" {
		t.Errorf("pipeline_slug: got %q, want %q", metadata["pipeline_slug"], "my-pipeline")
	}
	if metadata["build_uuid"] != "build-uuid-123" {
		t.Errorf("build_uuid: got %q, want %q", metadata["build_uuid"], "build-uuid-123")
	}
}

func TestCollectPlanMetadata_NoBaseBranch(t *testing.T) {
	t.Setenv("BUILDKITE_PIPELINE_SLUG", "")
	t.Setenv("BUILDKITE_BUILD_ID", "")

	record := "abc123\x1f\x1fAlice\x1falice@example.com\x1f2026-03-15T10:00:00+00:00\x1fAlice\x1falice@example.com\x1f2026-03-15T10:00:00+00:00\x1fInitial commit"
	gitLogOutput := record + "\x1e"

	runner := &FakeGitRunner{
		Responses: map[string]string{
			fmt.Sprintf("log -1 --format=%s", MetadataFormat): gitLogOutput,
			"branch --show-current":                           "main\n",
		},
	}

	metadata := CollectPlanMetadata(context.Background(), runner, "")

	// Commit metadata should still be present
	if metadata["commit_sha"] != "abc123" {
		t.Errorf("commit_sha: got %q, want %q", metadata["commit_sha"], "abc123")
	}
	if metadata["author_name"] != "Alice" {
		t.Errorf("author_name: got %q, want %q", metadata["author_name"], "Alice")
	}

	// Diff fields should be absent (no base branch)
	diffFields := []string{"files_changed", "diff_stat", "git_diff", "git_diff_raw"}
	for _, key := range diffFields {
		if _, ok := metadata[key]; ok {
			t.Errorf("expected key %q to be absent when no base branch, but found value %q", key, metadata[key])
		}
	}

	// base_branch should be absent
	if _, ok := metadata["base_branch"]; ok {
		t.Errorf("expected base_branch to be absent when empty")
	}

	// parent_shas should be absent (empty parents)
	if _, ok := metadata["parent_shas"]; ok {
		t.Errorf("expected parent_shas to be absent for root commit")
	}

	// Env var fields should be absent (unset)
	if _, ok := metadata["pipeline_slug"]; ok {
		t.Errorf("expected pipeline_slug to be absent when env var unset")
	}
	if _, ok := metadata["build_uuid"]; ok {
		t.Errorf("expected build_uuid to be absent when env var unset")
	}
}

func TestCollectPlanMetadata_DiffFails(t *testing.T) {
	t.Setenv("BUILDKITE_PIPELINE_SLUG", "")
	t.Setenv("BUILDKITE_BUILD_ID", "")

	record := "abc123\x1fdef456\x1fAlice\x1falice@example.com\x1f2026-03-15T10:00:00+00:00\x1fAlice\x1falice@example.com\x1f2026-03-15T10:00:00+00:00\x1fSome commit"
	gitLogOutput := record + "\x1e"

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
			"diff --no-ext-diff --name-only origin/main...HEAD": "file1.go\n",
			"diff --no-ext-diff --numstat origin/main...HEAD":   "10\t5\tfile1.go\n",
			"diff --no-ext-diff origin/main...HEAD":             "diff text\n",
			"diff --no-ext-diff --raw origin/main...HEAD":       ":100644 raw\n",
			"branch --show-current":                             "feature\n",
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

	record := "abc123\x1fdef456\x1fAlice\x1falice@example.com\x1f2026-03-15T10:00:00+00:00\x1fAlice\x1falice@example.com\x1f2026-03-15T10:00:00+00:00\x1fCommit msg"
	gitLogOutput := record + "\x1e"

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

	if _, ok := got["parent_shas"]; ok {
		t.Errorf("expected parent_shas to be absent for root commit, got %q", got["parent_shas"])
	}
	if got["commit_sha"] != "abc123" {
		t.Errorf("commit_sha: got %q, want %q", got["commit_sha"], "abc123")
	}
}

func TestCollectPlanMetadata_MultilineMessage(t *testing.T) {
	t.Setenv("BUILDKITE_PIPELINE_SLUG", "")
	t.Setenv("BUILDKITE_BUILD_ID", "")

	record := "abc123\x1fdef456\x1fAlice\x1falice@example.com\x1f2026-03-15T10:00:00+00:00\x1fAlice\x1falice@example.com\x1f2026-03-15T10:00:00+00:00\x1fFix the thing\n\nThis is a longer description\nwith multiple lines."
	gitLogOutput := record + "\x1e"

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

	record := "abc123\x1f\x1fAlice\x1falice@example.com\x1f2026-03-15T10:00:00+00:00\x1fAlice\x1falice@example.com\x1f2026-03-15T10:00:00+00:00\x1fMsg"
	gitLogOutput := record + "\x1e"

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

	record := "abc123\x1fdef456\x1fAlice\x1falice@example.com\x1f2026-03-15T10:00:00+00:00\x1fAlice\x1falice@example.com\x1f2026-03-15T10:00:00+00:00\x1fMerge commit"
	gitLogOutput := record + "\x1e"

	runner := &FakeGitRunner{
		Responses: map[string]string{
			fmt.Sprintf("log -1 --format=%s", MetadataFormat):   gitLogOutput,
			"diff --no-ext-diff --name-only origin/main...HEAD": "\n",
			"diff --no-ext-diff --numstat origin/main...HEAD":   "\n",
			"diff --no-ext-diff origin/main...HEAD":             "\n",
			"diff --no-ext-diff --raw origin/main...HEAD":       "\n",
			"branch --show-current":                             "main\n",
		},
	}

	metadata := CollectPlanMetadata(context.Background(), runner, "origin/main")

	// Empty diffs should be omitted (ToMap skips empty values)
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
