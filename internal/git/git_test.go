package git

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// feedback - the fakegitrunner is duplicated in another test file.
//
// FakeGitRunner returns canned responses based on the git arguments.
type FakeGitRunner struct {
	// Responses maps a key derived from args to the output string.
	Responses map[string]string
	// StdinResponses maps a key derived from args to a function that
	// takes stdin and returns the response. Used for OutputWithStdin.
	StdinResponses map[string]func(stdin string) string
}

func (f *FakeGitRunner) key(args []string) string {
	return strings.Join(args, " ")
}

func (f *FakeGitRunner) Output(ctx context.Context, args ...string) (string, error) {
	k := f.key(args)
	if resp, ok := f.Responses[k]; ok {
		return resp, nil
	}
	return "", fmt.Errorf("FakeGitRunner: no response for %q", k)
}

func (f *FakeGitRunner) OutputWithStdin(ctx context.Context, stdin string, args ...string) (string, error) {
	k := f.key(args)
	if fn, ok := f.StdinResponses[k]; ok {
		return fn(stdin), nil
	}
	if resp, ok := f.Responses[k]; ok {
		return resp, nil
	}
	return "", fmt.Errorf("FakeGitRunner: no response for %q (with stdin)", k)
}

func TestDetectDefaultBranch_SymbolicRef(t *testing.T) {
	runner := &FakeGitRunner{
		Responses: map[string]string{
			"symbolic-ref --short refs/remotes/origin/HEAD": "origin/main\n",
		},
	}
	branch, err := DetectDefaultBranch(context.Background(), runner, "origin")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if branch != "origin/main" {
		t.Errorf("got %q, want %q", branch, "origin/main")
	}
}

func TestDetectDefaultBranch_FallbackMain(t *testing.T) {
	runner := &FakeGitRunner{
		Responses: map[string]string{
			"rev-parse --verify origin/main": "abc123\n",
		},
	}
	branch, err := DetectDefaultBranch(context.Background(), runner, "origin")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if branch != "origin/main" {
		t.Errorf("got %q, want %q", branch, "origin/main")
	}
}

func TestDetectDefaultBranch_FallbackMaster(t *testing.T) {
	runner := &FakeGitRunner{
		Responses: map[string]string{
			"rev-parse --verify origin/master": "abc123\n",
		},
	}
	branch, err := DetectDefaultBranch(context.Background(), runner, "origin")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if branch != "origin/master" {
		t.Errorf("got %q, want %q", branch, "origin/master")
	}
}

func TestDetectDefaultBranch_CustomRemote(t *testing.T) {
	runner := &FakeGitRunner{
		Responses: map[string]string{
			"symbolic-ref --short refs/remotes/upstream/HEAD": "upstream/develop\n",
		},
	}
	branch, err := DetectDefaultBranch(context.Background(), runner, "upstream")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if branch != "upstream/develop" {
		t.Errorf("got %q, want %q", branch, "upstream/develop")
	}
}

func TestDetectDefaultBranch_NotFound(t *testing.T) {
	runner := &FakeGitRunner{
		Responses: map[string]string{},
	}
	_, err := DetectDefaultBranch(context.Background(), runner, "origin")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "could not detect default branch") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestFetchBulkMetadata(t *testing.T) {
	// Simulate git log output with unit/record separator delimiters
	record1 := "abc123\x1fdef456\x1fAlice\x1falice@example.com\x1f2026-03-15T10:00:00+00:00\x1fGitHub\x1fnoreply@github.com\x1f2026-03-15T10:00:00+00:00\x1fFix the thing"
	record2 := "def456\x1f\x1fBob\x1fbob@example.com\x1f2026-03-14T09:00:00+00:00\x1fBob\x1fbob@example.com\x1f2026-03-14T09:00:00+00:00\x1fInitial commit"
	gitOutput := record1 + "\x1e" + record2 + "\x1e"

	runner := &FakeGitRunner{
		StdinResponses: map[string]func(string) string{
			fmt.Sprintf("log --no-walk --stdin --format=%s", metadataFormat): func(_ string) string {
				return gitOutput
			},
		},
	}

	result, err := FetchBulkMetadata(context.Background(), runner, []string{"abc123", "def456"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result))
	}

	meta1 := result["abc123"]
	if meta1.CommitSHA != "abc123" {
		t.Errorf("CommitSHA: got %q, want %q", meta1.CommitSHA, "abc123")
	}
	if diff := cmp.Diff([]string{"def456"}, meta1.ParentSHAs); diff != "" {
		t.Errorf("ParentSHAs diff: %s", diff)
	}
	if meta1.AuthorName != "Alice" {
		t.Errorf("AuthorName: got %q, want %q", meta1.AuthorName, "Alice")
	}
	if meta1.Message != "Fix the thing" {
		t.Errorf("Message: got %q, want %q", meta1.Message, "Fix the thing")
	}

	meta2 := result["def456"]
	if meta2.ParentSHAs != nil {
		t.Errorf("ParentSHAs: got %v, want nil", meta2.ParentSHAs)
	}
}

func TestFetchBulkMetadata_MultilineMessage(t *testing.T) {
	record := "abc123\x1fdef456\x1fAlice\x1falice@example.com\x1f2026-03-15T10:00:00+00:00\x1fGitHub\x1fnoreply@github.com\x1f2026-03-15T10:00:00+00:00\x1fFix the thing\n\nThis is a longer description\nwith multiple lines."
	gitOutput := record + "\x1e"

	runner := &FakeGitRunner{
		StdinResponses: map[string]func(string) string{
			fmt.Sprintf("log --no-walk --stdin --format=%s", metadataFormat): func(_ string) string {
				return gitOutput
			},
		},
	}

	result, err := FetchBulkMetadata(context.Background(), runner, []string{"abc123"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	meta := result["abc123"]
	want := "Fix the thing\n\nThis is a longer description\nwith multiple lines."
	if meta.Message != want {
		t.Errorf("Message: got %q, want %q", meta.Message, want)
	}
}

func TestFetchBulkMetadata_Empty(t *testing.T) {
	result, err := FetchBulkMetadata(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty map, got %d entries", len(result))
	}
}

func TestFilterExistingCommits(t *testing.T) {
	runner := &FakeGitRunner{
		StdinResponses: map[string]func(string) string{
			"cat-file --batch-check": func(_ string) string {
				return "abc123 commit 271\ndef456 missing\nghi789 commit 350\n"
			},
		},
	}

	existing, missing, err := FilterExistingCommits(context.Background(), runner, []string{"abc123", "def456", "ghi789"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if diff := cmp.Diff([]string{"abc123", "ghi789"}, existing); diff != "" {
		t.Errorf("existing diff: %s", diff)
	}
	if diff := cmp.Diff([]string{"def456"}, missing); diff != "" {
		t.Errorf("missing diff: %s", diff)
	}
}

func TestFilterExistingCommits_AllMissing(t *testing.T) {
	runner := &FakeGitRunner{
		StdinResponses: map[string]func(string) string{
			"cat-file --batch-check": func(_ string) string {
				return "abc123 missing\ndef456 missing\n"
			},
		},
	}

	existing, missing, err := FilterExistingCommits(context.Background(), runner, []string{"abc123", "def456"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(existing) != 0 {
		t.Errorf("expected no existing commits, got %d", len(existing))
	}
	if diff := cmp.Diff([]string{"abc123", "def456"}, missing); diff != "" {
		t.Errorf("missing diff: %s", diff)
	}
}

func TestFilterExistingCommits_AllExist(t *testing.T) {
	runner := &FakeGitRunner{
		StdinResponses: map[string]func(string) string{
			"cat-file --batch-check": func(_ string) string {
				return "abc123 commit 271\ndef456 commit 350\n"
			},
		},
	}

	existing, missing, err := FilterExistingCommits(context.Background(), runner, []string{"abc123", "def456"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if diff := cmp.Diff([]string{"abc123", "def456"}, existing); diff != "" {
		t.Errorf("existing diff: %s", diff)
	}
	if len(missing) != 0 {
		t.Errorf("expected no missing commits, got %v", missing)
	}
}

func TestCollectDiffs_Basic(t *testing.T) {
	runner := &FakeGitRunner{
		Responses: map[string]string{
			// Fork-point: strategy 3 (plain merge-base)
			"merge-base origin/main abc123":                 "base111\n",
			"diff --no-ext-diff --name-only base111 abc123": "file1.go\nfile2.go\n",
			"diff --no-ext-diff --numstat base111 abc123":   "10\t5\tfile1.go\n3\t0\tfile2.go\n",
			"diff --no-ext-diff base111 abc123":             "diff --git a/file1.go...\n",
			"diff --no-ext-diff --raw base111 abc123":       ":100644 100644 aaa bbb M\tfile1.go\n",
		},
	}

	mc := &MainlineCache{
		onMainline: make(map[string]bool),
		parents:    make(map[string]string),
	}

	diffs, err := CollectDiffs(context.Background(), runner, []string{"abc123"}, "origin/main", mc, false, DefaultWorkerCount, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(diffs) != 1 {
		t.Fatalf("expected 1 result, got %d", len(diffs))
	}

	d := diffs[0]
	if d.FilesChanged != "file1.go\nfile2.go" {
		t.Errorf("FilesChanged: got %q", d.FilesChanged)
	}
	if d.DiffStat != "10\t5\tfile1.go\n3\t0\tfile2.go" {
		t.Errorf("DiffStat: got %q", d.DiffStat)
	}
	if d.GitDiff != "diff --git a/file1.go..." {
		t.Errorf("GitDiff: got %q", d.GitDiff)
	}
	if d.GitDiffRaw != ":100644 100644 aaa bbb M\tfile1.go" {
		t.Errorf("GitDiffRaw: got %q", d.GitDiffRaw)
	}
}

func TestCollectDiffs_SkipDiffs(t *testing.T) {
	runner := &FakeGitRunner{
		Responses: map[string]string{
			"merge-base origin/main abc123":                 "base111\n",
			"diff --no-ext-diff --name-only base111 abc123": "file1.go\n",
			"diff --no-ext-diff --numstat base111 abc123":   "10\t5\tfile1.go\n",
		},
	}

	mc := &MainlineCache{
		onMainline: make(map[string]bool),
		parents:    make(map[string]string),
	}

	diffs, err := CollectDiffs(context.Background(), runner, []string{"abc123"}, "origin/main", mc, true, DefaultWorkerCount, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	d := diffs[0]
	if d.GitDiff != "" {
		t.Errorf("expected empty GitDiff with skipDiffs, got %q", d.GitDiff)
	}
	if d.GitDiffRaw != "" {
		t.Errorf("expected empty GitDiffRaw with skipDiffs, got %q", d.GitDiffRaw)
	}
	if d.FilesChanged != "file1.go" {
		t.Errorf("FilesChanged should still be populated: got %q", d.FilesChanged)
	}
}

func TestCollectDiffs_OrderPreserved(t *testing.T) {
	runner := &FakeGitRunner{
		Responses: map[string]string{
			"merge-base origin/main aaa":               "base1\n",
			"merge-base origin/main bbb":               "base2\n",
			"merge-base origin/main ccc":               "base3\n",
			"diff --no-ext-diff --name-only base1 aaa": "a.go\n",
			"diff --no-ext-diff --name-only base2 bbb": "b.go\n",
			"diff --no-ext-diff --name-only base3 ccc": "c.go\n",
			"diff --no-ext-diff --numstat base1 aaa":   "",
			"diff --no-ext-diff --numstat base2 bbb":   "",
			"diff --no-ext-diff --numstat base3 ccc":   "",
			"diff --no-ext-diff base1 aaa":             "",
			"diff --no-ext-diff base2 bbb":             "",
			"diff --no-ext-diff base3 ccc":             "",
			"diff --no-ext-diff --raw base1 aaa":       "",
			"diff --no-ext-diff --raw base2 bbb":       "",
			"diff --no-ext-diff --raw base3 ccc":       "",
		},
	}

	mc := &MainlineCache{
		onMainline: make(map[string]bool),
		parents:    make(map[string]string),
	}

	diffs, err := CollectDiffs(context.Background(), runner, []string{"aaa", "bbb", "ccc"}, "origin/main", mc, false, DefaultWorkerCount, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{"a.go", "b.go", "c.go"}
	for i, d := range diffs {
		if d.FilesChanged != want[i] {
			t.Errorf("diffs[%d].FilesChanged: got %q, want %q", i, d.FilesChanged, want[i])
		}
	}
}

func TestCollectDiffs_ErrorSkipsCommit(t *testing.T) {
	runner := &FakeGitRunner{
		Responses: map[string]string{
			// aaa succeeds
			"merge-base origin/main aaa":               "base1\n",
			"diff --no-ext-diff --name-only base1 aaa": "a.go\n",
			"diff --no-ext-diff --numstat base1 aaa":   "",
			"diff --no-ext-diff base1 aaa":             "",
			"diff --no-ext-diff --raw base1 aaa":       "",
			// bbb fails at fork-point (no merge-base response)
			// ccc succeeds
			"merge-base origin/main ccc":               "base3\n",
			"diff --no-ext-diff --name-only base3 ccc": "c.go\n",
			"diff --no-ext-diff --numstat base3 ccc":   "",
			"diff --no-ext-diff base3 ccc":             "",
			"diff --no-ext-diff --raw base3 ccc":       "",
		},
	}

	mc := &MainlineCache{
		onMainline: make(map[string]bool),
		parents:    make(map[string]string),
	}

	diffs, err := CollectDiffs(context.Background(), runner, []string{"aaa", "bbb", "ccc"}, "origin/main", mc, false, DefaultWorkerCount, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if diffs[0].FilesChanged != "a.go" {
		t.Errorf("diffs[0].FilesChanged: got %q, want %q", diffs[0].FilesChanged, "a.go")
	}
	// bbb should be zero-value (error skipped)
	if diffs[1].FilesChanged != "" {
		t.Errorf("diffs[1].FilesChanged: expected empty for errored commit, got %q", diffs[1].FilesChanged)
	}
	if diffs[2].FilesChanged != "c.go" {
		t.Errorf("diffs[2].FilesChanged: got %q, want %q", diffs[2].FilesChanged, "c.go")
	}
}

func TestFetchMissingCommits_AllSucceed(t *testing.T) {
	runner := &FakeGitRunner{
		Responses: map[string]string{
			"fetch --no-tags --no-write-fetch-head origin aaa bbb ccc": "",
		},
	}

	unfetchable, err := FetchMissingCommits(context.Background(), runner, "origin", []string{"aaa", "bbb", "ccc"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if unfetchable != 0 {
		t.Errorf("unfetchable: got %d, want 0", unfetchable)
	}
}

func TestFetchMissingCommits_BisectOnError(t *testing.T) {
	// Batch of 3 fails (no response), bisects into [aaa] (ok) + [bbb, ccc].
	// [bbb, ccc] fails (no response), bisects into [bbb] (fails=unfetchable) + [ccc] (ok).
	runner := &FakeGitRunner{
		Responses: map[string]string{
			"fetch --no-tags --no-write-fetch-head origin aaa": "",
			"fetch --no-tags --no-write-fetch-head origin ccc": "",
		},
	}

	unfetchable, err := FetchMissingCommits(context.Background(), runner, "origin", []string{"aaa", "bbb", "ccc"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if unfetchable != 1 {
		t.Errorf("unfetchable: got %d, want 1", unfetchable)
	}
}

func TestFetchMissingCommits_AllUnfetchable(t *testing.T) {
	// No responses means everything fails
	runner := &FakeGitRunner{
		Responses: map[string]string{},
	}

	unfetchable, err := FetchMissingCommits(context.Background(), runner, "origin", []string{"aaa", "bbb"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if unfetchable != 2 {
		t.Errorf("unfetchable: got %d, want 2", unfetchable)
	}
}

func TestFetchMissingCommits_EmptyList(t *testing.T) {
	runner := &FakeGitRunner{
		Responses: map[string]string{},
	}

	unfetchable, err := FetchMissingCommits(context.Background(), runner, "origin", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if unfetchable != 0 {
		t.Errorf("unfetchable: got %d, want 0", unfetchable)
	}
}
