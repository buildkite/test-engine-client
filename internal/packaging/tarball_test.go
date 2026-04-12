package packaging

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func sampleRecords() []CommitRecord {
	return []CommitRecord{
		{
			SchemaVersion:  1,
			CommitSHA:      "abc123",
			ParentSHAs:     []string{"def456"},
			AuthorName:     "Alice",
			AuthorEmail:    "alice@example.com",
			AuthorDate:     "2026-03-15T10:00:00Z",
			CommitterName:  "GitHub",
			CommitterEmail: "noreply@github.com",
			CommitterDate:  "2026-03-15T10:00:00Z",
			Message:        "Fix the thing",
			FilesChanged:   "file1.go\nfile2.go",
			DiffStat:       "10\t5\tfile1.go",
			GitDiff:        "diff --git a/file1.go...",
			GitDiffRaw:     ":100644 100644 aaa bbb M\tfile1.go",
		},
		{
			SchemaVersion:  1,
			CommitSHA:      "def456",
			ParentSHAs:     nil,
			AuthorName:     "Bob",
			AuthorEmail:    "bob@example.com",
			AuthorDate:     "2026-03-14T09:00:00Z",
			CommitterName:  "Bob",
			CommitterEmail: "bob@example.com",
			CommitterDate:  "2026-03-14T09:00:00Z",
			Message:        "Initial commit",
			FilesChanged:   "README.md",
			DiffStat:       "1\t0\tREADME.md",
		},
	}
}

func sampleMetadata() ArchiveMetadata {
	return ArchiveMetadata{
		SchemaVersion:    1,
		Tool:             "bktec",
		ToolVersion:      "2.3.0",
		GeneratedAt:      "2026-03-30T12:00:00Z",
		OrganizationSlug: "my-org",
		SuiteSlug:        "my-suite",
		CommitCount:      2,
		SkippedCommits:   1,
		Days:             90,
		Remote:           "origin",
		SkippedDiffs:     false,
		MinCommitDate:    "2026-03-14T09:00:00Z",
		MaxCommitDate:    "2026-03-15T10:00:00Z",
	}
}

func TestCreateTarball_BasicStructure(t *testing.T) {
	path, err := CreateTarball(sampleRecords(), sampleMetadata())
	if err != nil {
		t.Fatalf("CreateTarball error: %v", err)
	}
	defer os.Remove(path)

	files := ReadTarball(t, path)
	if !HasTarEntry(files, "/commit-metadata.jsonl") {
		t.Error("tarball missing commit-metadata.jsonl")
	}
	if !HasTarEntry(files, "/metadata.json") {
		t.Error("tarball missing metadata.json")
	}
	// 3 entries: directory + 2 files
	if len(files) != 3 {
		t.Errorf("expected 3 entries in tarball (dir + 2 files), got %d", len(files))
	}
}

func TestCreateTarball_JSONLContent(t *testing.T) {
	records := sampleRecords()
	path, err := CreateTarball(records, sampleMetadata())
	if err != nil {
		t.Fatalf("CreateTarball error: %v", err)
	}
	defer os.Remove(path)

	files := ReadTarball(t, path)
	jsonl := FindTarEntry(t, files, "/commit-metadata.jsonl")
	lines := strings.Split(strings.TrimSpace(jsonl), "\n")
	if len(lines) != len(records) {
		t.Fatalf("expected %d JSONL lines, got %d", len(records), len(lines))
	}

	for i, line := range lines {
		var got CommitRecord
		if err := json.Unmarshal([]byte(line), &got); err != nil {
			t.Errorf("line %d: invalid JSON: %v", i, err)
			continue
		}
		if diff := cmp.Diff(records[i], got); diff != "" {
			t.Errorf("line %d diff: %s", i, diff)
		}
	}
}

func TestCreateTarball_MetadataContent(t *testing.T) {
	meta := sampleMetadata()
	path, err := CreateTarball(sampleRecords(), meta)
	if err != nil {
		t.Fatalf("CreateTarball error: %v", err)
	}
	defer os.Remove(path)

	files := ReadTarball(t, path)
	var got ArchiveMetadata
	if err := json.Unmarshal([]byte(FindTarEntry(t, files, "/metadata.json")), &got); err != nil {
		t.Fatalf("parsing metadata.json: %v", err)
	}
	if diff := cmp.Diff(meta, got); diff != "" {
		t.Errorf("metadata diff: %s", diff)
	}
}

func TestCreateTarball_EmptyRecords(t *testing.T) {
	path, err := CreateTarball(nil, sampleMetadata())
	if err != nil {
		t.Fatalf("CreateTarball error: %v", err)
	}
	defer os.Remove(path)

	files := ReadTarball(t, path)
	jsonl := FindTarEntry(t, files, "/commit-metadata.jsonl")
	if jsonl != "" {
		t.Errorf("expected empty JSONL for nil records, got %q", jsonl)
	}
}

func TestCreateTarball_OmitsEmptyDiffs(t *testing.T) {
	records := []CommitRecord{
		{
			SchemaVersion: 1,
			CommitSHA:     "abc123",
			FilesChanged:  "file1.go",
			// GitDiff and GitDiffRaw are empty -- should be omitted
		},
	}
	path, err := CreateTarball(records, sampleMetadata())
	if err != nil {
		t.Fatalf("CreateTarball error: %v", err)
	}
	defer os.Remove(path)

	files := ReadTarball(t, path)
	lines := strings.Split(strings.TrimSpace(FindTarEntry(t, files, "/commit-metadata.jsonl")), "\n")

	// The JSON line should not contain git_diff or git_diff_raw keys
	if strings.Contains(lines[0], "git_diff") {
		t.Error("expected git_diff to be omitted when empty")
	}
}

func TestCreateTarball_SchemaVersion(t *testing.T) {
	records := []CommitRecord{
		{SchemaVersion: 1, CommitSHA: "abc123"},
	}
	path, err := CreateTarball(records, sampleMetadata())
	if err != nil {
		t.Fatalf("CreateTarball error: %v", err)
	}
	defer os.Remove(path)

	files := ReadTarball(t, path)
	lines := strings.Split(strings.TrimSpace(FindTarEntry(t, files, "/commit-metadata.jsonl")), "\n")

	var got map[string]interface{}
	if err := json.Unmarshal([]byte(lines[0]), &got); err != nil {
		t.Fatalf("parsing JSONL line: %v", err)
	}
	if got["schema_version"] != float64(1) {
		t.Errorf("schema_version: got %v, want 1", got["schema_version"])
	}
}

func TestCreateTarball_DirectoryStructure(t *testing.T) {
	path, err := CreateTarball(sampleRecords(), sampleMetadata())
	if err != nil {
		t.Fatalf("CreateTarball error: %v", err)
	}
	defer os.Remove(path)

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("opening tarball: %v", err)
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("creating gzip reader: %v", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)

	// First entry should be the directory
	hdr, err := tr.Next()
	if err != nil {
		t.Fatalf("reading first tar entry: %v", err)
	}
	if hdr.Typeflag != tar.TypeDir {
		t.Errorf("first entry should be a directory, got typeflag %d", hdr.Typeflag)
	}
	if !strings.HasPrefix(hdr.Name, "backfill-my-org-my-suite-") {
		t.Errorf("directory name should start with 'backfill-my-org-my-suite-', got %q", hdr.Name)
	}
	if !strings.HasSuffix(hdr.Name, "/") {
		t.Errorf("directory name should end with '/', got %q", hdr.Name)
	}
	dirName := hdr.Name

	// Second entry should be commit-metadata.jsonl inside the directory
	hdr, err = tr.Next()
	if err != nil {
		t.Fatalf("reading second tar entry: %v", err)
	}
	if hdr.Name != dirName+"commit-metadata.jsonl" {
		t.Errorf("expected %q, got %q", dirName+"commit-metadata.jsonl", hdr.Name)
	}

	// Third entry should be metadata.json inside the directory
	hdr, err = tr.Next()
	if err != nil {
		t.Fatalf("reading third tar entry: %v", err)
	}
	if hdr.Name != dirName+"metadata.json" {
		t.Errorf("expected %q, got %q", dirName+"metadata.json", hdr.Name)
	}
}
