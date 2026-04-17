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
// Each candidate is probed against the repository using
// git rev-parse --verify. We try the candidate verbatim first, then fall
// back to "<remote>/<candidate>". This handles every common shape without
// heuristics: bare branch names ("main") resolve via the fallback to
// "origin/main"; refs from a different remote ("upstream/main"), fully-
// qualified refs ("refs/heads/release"), and values already including the
// configured remote ("origin/main") all resolve on the first probe.
// Without the verbatim probe, prefixing a qualified ref would rewrite it
// into an invalid value like "origin/upstream/main" and silently drop the
// explicit override.
// Returns the resolved ref (e.g. "origin/main") or an error.
func ResolveBaseBranch(ctx context.Context, runner GitRunner, explicit string, remote string) (string, error) {
	if remote == "" {
		return "", fmt.Errorf("remote must not be empty")
	}

	type candidate struct {
		value  string
		source string
	}
	candidates := []candidate{
		{value: explicit, source: "explicit --metadata base_branch"},
		{value: os.Getenv("BUILDKITE_PULL_REQUEST_BASE_BRANCH"), source: "BUILDKITE_PULL_REQUEST_BASE_BRANCH"},
	}

	for _, c := range candidates {
		if c.value == "" {
			continue
		}

		// Try the candidate verbatim first. This accepts qualified refs
		// from a non-default remote ("upstream/main"), fully-qualified
		// refs ("refs/heads/release"), and values already including the
		// configured remote ("origin/main") without rewriting them.
		if _, err := runner.Output(ctx, "rev-parse", "--verify", c.value); err == nil {
			debug.Printf("base branch resolved via %s: %s", c.source, c.value)
			return c.value, nil
		}

		// Fall back to "<remote>/<candidate>" for bare branch names
		// ("main" -> "origin/main") and release-style names with a "/"
		// that are still relative to the configured remote ("release/v2"
		// -> "origin/release/v2").
		prefixed := remote + "/" + c.value
		if _, err := runner.Output(ctx, "rev-parse", "--verify", prefixed); err == nil {
			debug.Printf("base branch resolved via %s: %q -> %s", c.source, c.value, prefixed)
			return prefixed, nil
		}
		debug.Printf("base branch candidate %q from %s not found (also tried %q), trying next", c.value, c.source, prefixed)
	}
	ref, err := DetectDefaultBranch(ctx, runner, remote)
	if err == nil {
		debug.Printf("base branch resolved via DetectDefaultBranch: %s", ref)
	}
	return ref, err
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

	// Real git log output ends in "\x1e\n" (git always appends a trailing
	// newline). TrimSpace must run first to strip the "\n", otherwise
	// TrimSuffix("\x1e") sees the "\n" at the end and no-ops, leaving the
	// record separator trapped inside the final field (the commit message).
	// TrimSpace does not strip "\x1e" because it is not Unicode whitespace.
	record := strings.TrimSuffix(strings.TrimSpace(output), recordSeparator)
	meta, ok := parseRecord(record)
	if !ok {
		debug.Printf("Warning: git log returned unparseable output; skipping commit metadata")
		return
	}

	mergeNonEmpty(metadata, meta.ToMap())
}

// collectDiffMetadata computes the merge-base between baseBranch and HEAD,
// then runs diff commands using two-arg form (merge-base, HEAD). This is
// equivalent to git diff baseBranch...HEAD but makes the fork-point
// resolution explicit and uses the same two-arg diff form as the backfill
// path.
func collectDiffMetadata(ctx context.Context, runner GitRunner, baseBranch string, metadata map[string]string) {
	forkPoint, err := runner.Output(ctx, "merge-base", baseBranch, "HEAD")
	if err != nil {
		debug.Printf("Warning: git merge-base failed: %v", err)
		return
	}
	forkPoint = strings.TrimSpace(forkPoint)

	diffs := runDiffCommands(ctx, runner, false, forkPoint, "HEAD")
	mergeNonEmpty(metadata, diffs.ToMap())
}

// MergeMetadata merges auto-collected metadata into existing user-provided
// metadata. User-provided keys take precedence: auto-collected values only
// fill in keys that are not already present. Empty auto-collected values
// are skipped. If existing is nil, the auto map is returned as-is.
func MergeMetadata(existing, auto map[string]string) map[string]string {
	if existing == nil {
		return auto
	}
	for k, v := range auto {
		if v == "" {
			continue
		}
		if _, exists := existing[k]; !exists {
			existing[k] = v
		}
	}
	return existing
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
	// branch: current branch name, falling back to BUILDKITE_BRANCH
	// for detached HEAD (common in CI where the agent checks out a commit SHA)
	if out, err := runner.Output(ctx, "branch", "--show-current"); err == nil {
		if branch := strings.TrimSpace(out); branch != "" {
			metadata["branch"] = branch
			debug.Printf("branch resolved via git branch --show-current: %s", branch)
		}
	} else {
		debug.Printf("Warning: git branch --show-current failed: %v", err)
	}
	if _, ok := metadata["branch"]; !ok {
		if branch := os.Getenv("BUILDKITE_BRANCH"); branch != "" {
			metadata["branch"] = branch
			debug.Printf("branch resolved via BUILDKITE_BRANCH env var: %s", branch)
		} else {
			debug.Printf("branch could not be determined (detached HEAD, no BUILDKITE_BRANCH)")
		}
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
