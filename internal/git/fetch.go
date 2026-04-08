package git

import (
	"context"
	"fmt"

	"github.com/buildkite/test-engine-client/internal/debug"
)

const fetchChunkSize = 1000

// FetchMissingCommits attempts to fetch the given commits from the remote.
// Uses chunked fetching (1000 commits per batch) with recursive bisection on
// error to isolate unfetchable SHAs (e.g. force-pushed/rebased commits that
// no longer exist on the remote).
//
// Returns the number of commits that could not be fetched.
func FetchMissingCommits(ctx context.Context, runner GitRunner, remote string, commits []string) (unfetchable int, err error) {
	if len(commits) == 0 {
		return 0, nil
	}

	// Process in chunks to avoid excessively long git fetch commands
	for i := 0; i < len(commits); i += fetchChunkSize {
		end := i + fetchChunkSize
		if end > len(commits) {
			end = len(commits)
		}
		chunk := commits[i:end]
		n, err := fetchChunkBisect(ctx, runner, remote, chunk)
		if err != nil {
			return unfetchable + n, err
		}
		unfetchable += n
	}

	return unfetchable, nil
}

// fetchChunkBisect attempts to fetch a chunk of commits. On failure, it
// recursively bisects the list to isolate unfetchable SHAs.
func fetchChunkBisect(ctx context.Context, runner GitRunner, remote string, commits []string) (unfetchable int, err error) {
	if len(commits) == 0 {
		return 0, nil
	}

	// Base case: single commit that fails -> it's unfetchable
	if len(commits) == 1 {
		if err := fetchCommits(ctx, runner, remote, commits); err != nil {
			debug.Printf("Commit %s is unfetchable: %v", commits[0], err)
			return 1, nil
		}
		return 0, nil
	}

	// Try fetching the whole chunk at once
	if err := fetchCommits(ctx, runner, remote, commits); err == nil {
		return 0, nil
	}

	debug.Printf("Fetch failed for batch of %d commits, bisecting...", len(commits))

	// Bisect: split in half and recurse
	mid := len(commits) / 2
	left, err := fetchChunkBisect(ctx, runner, remote, commits[:mid])
	if err != nil {
		return left, err
	}
	right, err := fetchChunkBisect(ctx, runner, remote, commits[mid:])
	if err != nil {
		return left + right, err
	}
	return left + right, nil
}

// fetchCommits runs git fetch for the given commits.
func fetchCommits(ctx context.Context, runner GitRunner, remote string, commits []string) error {
	args := []string{"fetch", "--no-tags", "--no-write-fetch-head", remote}
	args = append(args, commits...)

	debug.Printf("Fetching %d commits from %s", len(commits), remote)
	_, err := runner.Output(ctx, args...)
	if err != nil {
		return fmt.Errorf("git fetch: %w", err)
	}
	return nil
}
