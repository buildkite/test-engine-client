package git

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/buildkite/test-engine-client/internal/debug"
)

// ResolveBaseBranch determines the base branch ref to diff against.
//
// Resolution order:
//  1. explicit (from --metadata base_branch=...) -- for repos with
//     non-standard default branches (not main/master), or PRs targeting
//     non-default branches outside Buildkite CI.
//  2. $BUILDKITE_PULL_REQUEST_BASE_BRANCH -- auto-set by the Buildkite
//     agent on PR builds.
//  3. DetectDefaultBranch() -- tries remote/HEAD, remote/main, remote/master.
//
// Most users should NOT need to set base_branch explicitly. Override is
// only needed when:
//   - The repo uses a non-standard default branch (e.g. "develop", "trunk")
//     AND remote/HEAD is not configured
//   - The build targets a non-default branch (e.g. a PR into "release/v2")
//     AND $BUILDKITE_PULL_REQUEST_BASE_BRANCH is not set (non-Buildkite CI
//     or manual trigger)
//
// If a candidate is a bare branch name (e.g. "main"), it's prefixed with
// "<remote>/" to form the remote ref. Each candidate is verified with
// git rev-parse --verify before being accepted.
// Returns the resolved ref (e.g. "origin/main") or an error.
func ResolveBaseBranch(ctx context.Context, runner GitRunner, explicit string, remote string) (string, error) {
	for _, candidate := range []string{explicit, os.Getenv("BUILDKITE_PULL_REQUEST_BASE_BRANCH")} {
		if candidate == "" {
			continue
		}
		ref := candidate
		if !strings.HasPrefix(ref, remote+"/") {
			ref = remote + "/" + ref
		}
		if _, err := runner.Output(ctx, "rev-parse", "--verify", ref); err == nil {
			return ref, nil
		}
		debug.Printf("base branch candidate %q (resolved to %q) not found, trying next", candidate, ref)
	}
	return DetectDefaultBranch(ctx, runner, remote)
}

// CollectPlanMetadata collects git metadata for the current HEAD commit.
// Returns a map of metadata keys to values. Skips keys that cannot be
// collected (e.g. if not in a git repo). Does not error on git failures;
// returns partial results with warnings logged via debug.Printf.
func CollectPlanMetadata(ctx context.Context, runner GitRunner, baseBranch string) map[string]string {
	metadata := make(map[string]string)

	// Phase 1: Commit metadata via git log -1 --format=...
	// Reuses MetadataFormat from metadata.go for consistency with backfill.
	collectCommitMetadata(ctx, runner, metadata)

	// Phase 2: Diff fields against base branch (only if base branch is resolved)
	if baseBranch != "" {
		collectDiffMetadata(ctx, runner, baseBranch, metadata)
	}

	// Phase 3: Context fields
	collectContextFields(ctx, runner, baseBranch, metadata)

	return metadata
}

// collectCommitMetadata extracts commit metadata for HEAD using a single
// git log call with the same format as FetchBulkMetadata, parses it into
// a CommitMetadata struct, and flattens it into the metadata map via ToMap.
func collectCommitMetadata(ctx context.Context, runner GitRunner, metadata map[string]string) {
	output, err := runner.Output(ctx, "log", "-1", fmt.Sprintf("--format=%s", MetadataFormat))
	if err != nil {
		debug.Printf("Warning: git log failed, skipping commit metadata: %v", err)
		return
	}

	meta, ok := parseCommitRecord(output)
	if !ok {
		return
	}

	mergeNonEmpty(metadata, meta.ToMap())
}

// parseCommitRecord parses a single git log record (using MetadataFormat
// separators) into a CommitMetadata struct. Returns false if the record
// is empty or has too few fields. Reuses the same parsing logic as
// FetchBulkMetadata.
func parseCommitRecord(output string) (CommitMetadata, bool) {
	record := strings.TrimRight(output, recordSeparator+"\n ")
	if record == "" {
		return CommitMetadata{}, false
	}

	fields := strings.SplitN(record, fieldSeparator, metadataFields)
	if len(fields) < metadataFields {
		debug.Printf("Warning: git log returned %d fields, expected %d; skipping commit metadata", len(fields), metadataFields)
		return CommitMetadata{}, false
	}

	sha := strings.TrimSpace(fields[0])
	if sha == "" {
		return CommitMetadata{}, false
	}

	var parentSHAs []string
	if parents := strings.TrimSpace(fields[1]); parents != "" {
		parentSHAs = strings.Fields(parents)
	}

	return CommitMetadata{
		CommitSHA:      sha,
		ParentSHAs:     parentSHAs,
		AuthorName:     strings.TrimSpace(fields[2]),
		AuthorEmail:    strings.TrimSpace(fields[3]),
		AuthorDate:     strings.TrimSpace(fields[4]),
		CommitterName:  strings.TrimSpace(fields[5]),
		CommitterEmail: strings.TrimSpace(fields[6]),
		CommitterDate:  strings.TrimSpace(fields[7]),
		Message:        strings.TrimSpace(fields[8]),
	}, true
}

// collectDiffMetadata runs diff commands against the resolved base branch
// using triple-dot syntax (<base>...HEAD) and merges the results into the
// metadata map.
func collectDiffMetadata(ctx context.Context, runner GitRunner, baseBranch string, metadata map[string]string) {
	diffs := runDiffCommands(ctx, runner, false, baseBranch+"...HEAD")
	mergeNonEmpty(metadata, diffs.ToMap())
}

// mergeNonEmpty copies entries from src into dst, skipping empty values.
// This avoids sending meaningless keys (e.g. "git_diff":"") in the API
// request, since json.Marshal does not omit empty strings within a map.
func mergeNonEmpty(dst, src map[string]string) {
	for k, v := range src {
		if v != "" {
			dst[k] = v
		}
	}
}

// collectContextFields adds branch, base_branch, and Buildkite env var fields.
func collectContextFields(ctx context.Context, runner GitRunner, baseBranch string, metadata map[string]string) {
	// branch: current branch name (empty on detached HEAD, omitted)
	if out, err := runner.Output(ctx, "branch", "--show-current"); err == nil {
		branch := strings.TrimSpace(out)
		if branch != "" {
			metadata["branch"] = branch
		}
	} else {
		debug.Printf("Warning: git branch --show-current failed: %v", err)
	}

	// base_branch: the resolved base ref (not a git command)
	if baseBranch != "" {
		metadata["base_branch"] = baseBranch
	}

	// pipeline_slug from Buildkite env (omitted if not set)
	if slug := os.Getenv("BUILDKITE_PIPELINE_SLUG"); slug != "" {
		metadata["pipeline_slug"] = slug
	}

	// build_uuid from Buildkite env (omitted if not set)
	if buildID := os.Getenv("BUILDKITE_BUILD_ID"); buildID != "" {
		metadata["build_uuid"] = buildID
	}
}
