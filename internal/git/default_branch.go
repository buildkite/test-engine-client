package git

import (
	"context"
	"fmt"
	"strings"
)

// DetectDefaultBranch returns the remote default branch reference.
// Tries <remote>/HEAD, then falls back to <remote>/main, then <remote>/master.
func DetectDefaultBranch(ctx context.Context, runner GitRunner, remote string) (string, error) {
	// Try symbolic-ref (same as reporummage)
	output, err := runner.Output(ctx, "symbolic-ref", "--short", fmt.Sprintf("refs/remotes/%s/HEAD", remote))
	if err == nil {
		branch := strings.TrimSpace(output)
		if branch != "" {
			return branch, nil
		}
	}

	// Fallback: check if <remote>/main exists
	ref := fmt.Sprintf("%s/main", remote)
	if _, err := runner.Output(ctx, "rev-parse", "--verify", ref); err == nil {
		return ref, nil
	}

	// Fallback: check if <remote>/master exists
	ref = fmt.Sprintf("%s/master", remote)
	if _, err := runner.Output(ctx, "rev-parse", "--verify", ref); err == nil {
		return ref, nil
	}

	return "", fmt.Errorf("could not detect default branch: %s/HEAD not set and neither %s/main nor %s/master exist", remote, remote, remote)
}
