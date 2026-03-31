package packaging

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
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
	CommitCount      int    `json:"commit_count"`
	SkippedCommits   int    `json:"skipped_commits"`
	SkippedDiffs     bool   `json:"skipped_diffs"`
}

// CreateTarball writes a tar.gz to a temp file containing:
//   - commit-metadata.jsonl (one JSON object per line)
//   - metadata.json (archive metadata)
//
// Returns the path to the temp file. Caller is responsible for cleanup.
func CreateTarball(records []CommitRecord, meta ArchiveMetadata) (string, error) {
	// Create temp file
	tmpFile, err := os.CreateTemp("", "bktec-commit-metadata-*.tar.gz")
	if err != nil {
		return "", fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	// If anything fails, clean up the temp file
	success := false
	defer func() {
		if !success {
			os.Remove(tmpPath)
		}
	}()

	gzWriter := gzip.NewWriter(tmpFile)
	tarWriter := tar.NewWriter(gzWriter)

	// Write commit-metadata.jsonl
	var jsonlBuf bytes.Buffer
	for _, record := range records {
		line, err := json.Marshal(record)
		if err != nil {
			tmpFile.Close()
			return "", fmt.Errorf("marshalling record for %s: %w", record.CommitSHA, err)
		}
		jsonlBuf.Write(line)
		jsonlBuf.WriteByte('\n')
	}

	now := time.Now()
	if err := tarWriter.WriteHeader(&tar.Header{
		Name:    "commit-metadata.jsonl",
		Size:    int64(jsonlBuf.Len()),
		Mode:    0644,
		ModTime: now,
	}); err != nil {
		tmpFile.Close()
		return "", fmt.Errorf("writing jsonl tar header: %w", err)
	}
	if _, err := tarWriter.Write(jsonlBuf.Bytes()); err != nil {
		tmpFile.Close()
		return "", fmt.Errorf("writing jsonl tar content: %w", err)
	}

	// Write metadata.json
	metaBytes, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		tmpFile.Close()
		return "", fmt.Errorf("marshalling metadata: %w", err)
	}
	metaBytes = append(metaBytes, '\n')

	if err := tarWriter.WriteHeader(&tar.Header{
		Name:    "metadata.json",
		Size:    int64(len(metaBytes)),
		Mode:    0644,
		ModTime: now,
	}); err != nil {
		tmpFile.Close()
		return "", fmt.Errorf("writing metadata tar header: %w", err)
	}
	if _, err := tarWriter.Write(metaBytes); err != nil {
		tmpFile.Close()
		return "", fmt.Errorf("writing metadata tar content: %w", err)
	}

	// Close in order: tar -> gzip -> file
	if err := tarWriter.Close(); err != nil {
		tmpFile.Close()
		return "", fmt.Errorf("closing tar writer: %w", err)
	}
	if err := gzWriter.Close(); err != nil {
		tmpFile.Close()
		return "", fmt.Errorf("closing gzip writer: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return "", fmt.Errorf("closing temp file: %w", err)
	}

	success = true
	return tmpPath, nil
}
