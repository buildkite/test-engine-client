package git

import (
	"context"
	"fmt"
	"strings"
)

// FilterExistingCommits checks which commits exist in the local repo.
// Returns the list of existing commits (preserving input order), the list of
// missing commit SHAs, and any error.
//
// Uses `git cat-file --batch-check` with stdin for efficiency (single process
// for all commits, rather than one git call per commit).
func FilterExistingCommits(ctx context.Context, runner GitRunner, commits []string) (existing []string, missing []string, err error) {
	if len(commits) == 0 {
		return nil, nil, nil
	}

	stdin := strings.Join(commits, "\n")
	output, err := runner.OutputWithStdin(ctx, stdin, "cat-file", "--batch-check")
	if err != nil {
		return nil, nil, fmt.Errorf("checking commits: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) != len(commits) {
		return nil, nil, fmt.Errorf("cat-file returned %d lines, expected %d", len(lines), len(commits))
	}

	for i, line := range lines {
		if strings.Contains(line, " missing") {
			missing = append(missing, commits[i])
		} else if strings.Contains(line, " commit ") {
			existing = append(existing, commits[i])
		} else {
			// Unexpected output (e.g. tree, blob, tag) -- skip it
			missing = append(missing, commits[i])
		}
	}

	return existing, missing, nil
}
