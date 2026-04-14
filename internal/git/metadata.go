package git

import (
	"context"
	"fmt"
	"strings"
)

// CommitMetadata holds the metadata for a single git commit.
type CommitMetadata struct {
	CommitSHA      string   `json:"commit_sha"`
	ParentSHAs     []string `json:"parent_shas"`
	AuthorName     string   `json:"author_name"`
	AuthorEmail    string   `json:"author_email"`
	AuthorDate     string   `json:"author_date"`
	CommitterName  string   `json:"committer_name"`
	CommitterEmail string   `json:"committer_email"`
	CommitterDate  string   `json:"committer_date"`
	Message        string   `json:"message"`
}

const (
	fieldSeparator  = "\x1f"
	recordSeparator = "\x1e"

	// Git format placeholders for each metadata field.
	fmtFieldSep  = "%x1f" // ASCII unit separator between fields
	fmtRecordSep = "%x1e" // ASCII record separator between commits
	fmtHash      = "%H"   // full commit hash
	fmtParents   = "%P"   // parent hashes (space-separated)
	fmtAuthorN   = "%an"  // author name
	fmtAuthorE   = "%ae"  // author email
	fmtAuthorD   = "%aI"  // author date (ISO 8601)
	fmtCommitN   = "%cn"  // committer name
	fmtCommitE   = "%ce"  // committer email
	fmtCommitD   = "%cI"  // committer date (ISO 8601)
	fmtBody      = "%B"   // full commit message

	metadataFields = 9 // number of fields in metadataFormat
)

// MetadataFormat is the git log format string for bulk metadata extraction.
// Fields are separated by ASCII unit separator (%x1f), records by ASCII
// record separator (%x1e). These control characters are purpose-built for
// structured data and won't appear in normal git fields.
var MetadataFormat = strings.Join([]string{
	fmtHash, fmtParents,
	fmtAuthorN, fmtAuthorE, fmtAuthorD,
	fmtCommitN, fmtCommitE, fmtCommitD,
	fmtBody,
}, fmtFieldSep) + fmtRecordSep

// ToMap returns the commit metadata as a flat string map using the same key
// names as the JSON tags. ParentSHAs are stored as a space-separated string;
// the key is omitted if there are no parents (root commit).
func (m CommitMetadata) ToMap() map[string]string {
	result := map[string]string{
		"commit_sha":      m.CommitSHA,
		"author_name":     m.AuthorName,
		"author_email":    m.AuthorEmail,
		"author_date":     m.AuthorDate,
		"committer_name":  m.CommitterName,
		"committer_email": m.CommitterEmail,
		"committer_date":  m.CommitterDate,
		"message":         m.Message,
	}
	if len(m.ParentSHAs) > 0 {
		result["parent_shas"] = strings.Join(m.ParentSHAs, " ")
	}
	return result
}

// FetchBulkMetadata fetches metadata for all given commits in a single git call.
// Uses --no-walk with --stdin to process only the specified commits (not ancestors).
// Returns a map from commit SHA to CommitMetadata for O(1) lookup.
func FetchBulkMetadata(ctx context.Context, runner GitRunner, commits []string) (map[string]CommitMetadata, error) {
	if len(commits) == 0 {
		return make(map[string]CommitMetadata), nil
	}

	stdin := strings.Join(commits, "\n")
	output, err := runner.OutputWithStdin(ctx, stdin,
		"log", "--no-walk", "--stdin", fmt.Sprintf("--format=%s", MetadataFormat))
	if err != nil {
		return nil, fmt.Errorf("fetching bulk metadata: %w", err)
	}

	result := make(map[string]CommitMetadata, len(commits))
	records := strings.Split(output, recordSeparator)
	for _, record := range records {
		record = strings.TrimSpace(record)
		if record == "" {
			continue
		}
		fields := strings.SplitN(record, fieldSeparator, metadataFields)
		if len(fields) < metadataFields {
			continue
		}

		sha := strings.TrimSpace(fields[0])
		if sha == "" {
			continue
		}

		var parentSHAs []string
		if parents := strings.TrimSpace(fields[1]); parents != "" {
			parentSHAs = strings.Fields(parents)
		}

		meta := CommitMetadata{
			CommitSHA:      sha,
			ParentSHAs:     parentSHAs,
			AuthorName:     strings.TrimSpace(fields[2]),
			AuthorEmail:    strings.TrimSpace(fields[3]),
			AuthorDate:     strings.TrimSpace(fields[4]),
			CommitterName:  strings.TrimSpace(fields[5]),
			CommitterEmail: strings.TrimSpace(fields[6]),
			CommitterDate:  strings.TrimSpace(fields[7]),
			Message:        strings.TrimSpace(fields[8]),
		}
		result[sha] = meta
	}

	return result, nil
}
