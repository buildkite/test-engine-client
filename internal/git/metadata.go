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

// metadataFormat is the git log format string for bulk metadata extraction.
// Fields are separated by unit separator (%x1f), records by record separator (%x1e).
// These ASCII control characters are purpose-built for structured data and won't
// appear in normal git fields (names, emails, messages).
//
// Field order: hash, parents, author name, author email, author date (ISO),
// committer name, committer email, committer date (ISO), full message body.
const metadataFormat = "%H%x1f%P%x1f%an%x1f%ae%x1f%aI%x1f%cn%x1f%ce%x1f%cI%x1f%B%x1e"

const (
	fieldSeparator  = "\x1f"
	recordSeparator = "\x1e"
	metadataFields  = 9 // number of fields in metadataFormat
)

// FetchBulkMetadata fetches metadata for all given commits in a single git call.
// Uses --no-walk with --stdin to process only the specified commits (not ancestors).
// Returns a map from commit SHA to CommitMetadata for O(1) lookup.
func FetchBulkMetadata(ctx context.Context, runner GitRunner, commits []string) (map[string]CommitMetadata, error) {
	if len(commits) == 0 {
		return make(map[string]CommitMetadata), nil
	}

	stdin := strings.Join(commits, "\n")
	output, err := runner.OutputWithStdin(ctx, stdin,
		"log", "--no-walk", "--stdin", fmt.Sprintf("--format=%s", metadataFormat))
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
