package git

import (
	"context"
	"fmt"
	"strings"
)

// DetectDefaultBranch returns the remote default branch reference.
// Tries origin/HEAD, then falls back to origin/main, then origin/master.
func DetectDefaultBranch(ctx context.Context, runner GitRunner) (string, error) {
	// Try symbolic-ref (same as reporummage)
	output, err := runner.Output(ctx, "symbolic-ref", "--short", "refs/remotes/origin/HEAD")
	if err == nil {
		branch := strings.TrimSpace(output)
		if branch != "" {
			return branch, nil
		}
	}

	// Fallback: check if origin/main exists
	if _, err := runner.Output(ctx, "rev-parse", "--verify", "origin/main"); err == nil {
		return "origin/main", nil
	}

	// Fallback: check if origin/master exists
	if _, err := runner.Output(ctx, "rev-parse", "--verify", "origin/master"); err == nil {
		return "origin/master", nil
	}

	return "", fmt.Errorf("could not detect default branch: origin/HEAD not set and neither origin/main nor origin/master exist")
}
