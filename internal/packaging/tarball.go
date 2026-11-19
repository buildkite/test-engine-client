package packaging

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// CommitRecord is the per-commit record written to the JSONL file.
type CommitRecord struct {
	SchemaVersion  int      `json:"schema_version"`
	CommitSHA      string   `json:"commit_sha"`
	ParentSHAs     []string `json:"parent_shas"`
	AuthorName     string   `json:"author_name"`
	AuthorEmail    string   `json:"author_email"`
	AuthorDate     string   `json:"author_date"`
	CommitterName  string   `json:"committer_name"`
	CommitterEmail string   `json:"committer_email"`
	CommitterDate  string   `json:"committer_date"`
	Message        string   `json:"message"`
	FilesChanged   string   `json:"files_changed"`
	DiffStat       string   `json:"diff_stat"`
	GitDiff        string   `json:"git_diff,omitempty"`
	GitDiffRaw     string   `json:"git_diff_raw,omitempty"`
}

// ArchiveMetadata is the metadata written to metadata.json in the tarball.
type ArchiveMetadata struct {
	SchemaVersion    int    `json:"schema_version"`
	Tool             string `json:"tool"`
	ToolVersion      string `json:"tool_version"`
	GeneratedAt      string `json:"generated_at"`
	OrganizationSlug string `json:"organization_slug"`
	SuiteSlug        string `json:"suite_slug"`
	OrganizationUUID string `json:"organization_uuid"`
	SuiteUUID        string `json:"suite_uuid"`
	CommitCount      int    `json:"commit_count"`
	SkippedCommits   int    `json:"skipped_commits"`
	// Config options used for this export
	Days         int    `json:"days"`
	Remote       string `json:"remote"`
	SkippedDiffs bool   `json:"skipped_diffs"`
	// Date range of commits in the archive (ISO 8601, from CommitterDate)
	MinCommitDate string `json:"min_commit_date,omitempty"`
	MaxCommitDate string `json:"max_commit_date,omitempty"`
}

// CreateTarball writes a tar.gz to a temp file containing:
//   - <dir>/commit-metadata.jsonl (one JSON object per line)
//   - <dir>/metadata.json (archive metadata)
//
// Files are nested inside a directory named
// backfill-<org>-<suite>-<timestamp> to follow tar best practices
// (no top-level files).
//
// Returns the archive path and a cleanup function. The private directory keeps
// paths unique without changing the timestamped basename used in uploads.
func CreateTarball(records []CommitRecord, meta ArchiveMetadata) (string, func(), error) {
	dir, err := os.MkdirTemp("", "bktec-backfill-*")
	if err != nil {
		return "", nil, fmt.Errorf("creating temp directory: %w", err)
	}
	cleanup := func() { os.RemoveAll(dir) }

	success := false
	defer func() {
		if !success {
			cleanup()
		}
	}()

	ts := time.Now().UTC().Format("20060102T150405.000Z")
	tmpPath := filepath.Join(dir, fmt.Sprintf("bktec-commit-metadata-%s.tar.gz", ts))
	tmpFile, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", nil, fmt.Errorf("creating temp file: %w", err)
	}

	gzWriter := gzip.NewWriter(tmpFile)
	tarWriter := tar.NewWriter(gzWriter)

	// Write commit-metadata.jsonl
	var jsonlBuf bytes.Buffer
	for _, record := range records {
		line, err := json.Marshal(record)
		if err != nil {
			tmpFile.Close()
			return "", nil, fmt.Errorf("marshalling record for %s: %w", record.CommitSHA, err)
		}
		jsonlBuf.Write(line)
		jsonlBuf.WriteByte('\n')
	}

	now := time.Now()

	// Build directory name: backfill-<org>-<suite>-<timestamp>
	dirName := fmt.Sprintf("backfill-%s-%s-%s",
		meta.OrganizationSlug, meta.SuiteSlug,
		now.UTC().Format("20060102T150405.000Z"))

	// Write directory entry
	if err := tarWriter.WriteHeader(&tar.Header{
		Name:     dirName + "/",
		Typeflag: tar.TypeDir,
		Mode:     0o755,
		ModTime:  now,
	}); err != nil {
		tmpFile.Close()
		return "", nil, fmt.Errorf("writing directory tar header: %w", err)
	}

	if err := tarWriter.WriteHeader(&tar.Header{
		Name:    dirName + "/commit-metadata.jsonl",
		Size:    int64(jsonlBuf.Len()),
		Mode:    0o644,
		ModTime: now,
	}); err != nil {
		tmpFile.Close()
		return "", nil, fmt.Errorf("writing jsonl tar header: %w", err)
	}
	if _, err := tarWriter.Write(jsonlBuf.Bytes()); err != nil {
		tmpFile.Close()
		return "", nil, fmt.Errorf("writing jsonl tar content: %w", err)
	}

	// Write metadata.json
	metaBytes, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		tmpFile.Close()
		return "", nil, fmt.Errorf("marshalling metadata: %w", err)
	}
	metaBytes = append(metaBytes, '\n')

	if err := tarWriter.WriteHeader(&tar.Header{
		Name:    dirName + "/metadata.json",
		Size:    int64(len(metaBytes)),
		Mode:    0o644,
		ModTime: now,
	}); err != nil {
		tmpFile.Close()
		return "", nil, fmt.Errorf("writing metadata tar header: %w", err)
	}
	if _, err := tarWriter.Write(metaBytes); err != nil {
		tmpFile.Close()
		return "", nil, fmt.Errorf("writing metadata tar content: %w", err)
	}

	// Close in order: tar -> gzip -> file
	if err := tarWriter.Close(); err != nil {
		tmpFile.Close()
		return "", nil, fmt.Errorf("closing tar writer: %w", err)
	}
	if err := gzWriter.Close(); err != nil {
		tmpFile.Close()
		return "", nil, fmt.Errorf("closing gzip writer: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return "", nil, fmt.Errorf("closing temp file: %w", err)
	}

	success = true
	return tmpPath, cleanup, nil
}
