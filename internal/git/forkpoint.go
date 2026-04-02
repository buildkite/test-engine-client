package git

import (
	"context"
	"fmt"
	"strings"
)

// ForkPointResult holds the base commit for diffing and the strategy used to find it.
type ForkPointResult struct {
	Base     string
	Strategy string // "fork-point", "parent-fallback", "merge-base"
}

// MainlineCache precomputes the first-parent topology of the default branch.
// This is used by the parent-fallback strategy to detect commits that are
// directly on the main branch (e.g. direct pushes or merge commits).
type MainlineCache struct {
	onMainline map[string]bool
	parent     map[string]string // commit -> first parent
}

// Size returns the number of commits in the mainline cache.
func (mc *MainlineCache) Size() int {
	return len(mc.onMainline)
}

// BuildMainlineCache builds a cache of the first-parent chain from the given branch.
// Uses `git log --first-parent --format=%H %P` to enumerate all commits on the
// mainline and their first parents.
func BuildMainlineCache(ctx context.Context, runner GitRunner, mainBranch string) (*MainlineCache, error) {
	output, err := runner.Output(ctx, "log", "--first-parent", "--format=%H %P", mainBranch)
	if err != nil {
		return nil, fmt.Errorf("building mainline cache: %w", err)
	}
	mc := &MainlineCache{
		onMainline: make(map[string]bool),
		parent:     make(map[string]string),
	}
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		mc.onMainline[fields[0]] = true
		if len(fields) >= 2 {
			mc.parent[fields[0]] = fields[1]
		}
	}
	return mc, nil
}

// FindForkPoint determines the base commit to diff against using 3 strategies:
//
//  1. git merge-base --fork-point (uses reflog, best for recent branches)
//  2. Mainline parent fallback (commit is on the first-parent chain of main)
//  3. Plain git merge-base (fallback for unmerged branches)
func FindForkPoint(ctx context.Context, runner GitRunner, mainBranch, commit string, mc *MainlineCache) (ForkPointResult, error) {
	// Strategy 1: git merge-base --fork-point (uses reflog)
	if base, err := runner.Output(ctx, "merge-base", "--fork-point", mainBranch, commit); err == nil {
		base = strings.TrimSpace(base)
		if base != "" && base != commit {
			return ForkPointResult{Base: base, Strategy: "fork-point"}, nil
		}
	}

	// Strategy 2: Mainline parent fallback -- commit is on the first-parent
	// chain of main (direct push or merge commit on main itself).
	if mc != nil && mc.onMainline[commit] {
		if parent, ok := mc.parent[commit]; ok {
			return ForkPointResult{Base: parent, Strategy: "parent-fallback"}, nil
		}
	}

	// Strategy 3: Plain merge-base (unmerged branch)
	base, err := runner.Output(ctx, "merge-base", mainBranch, commit)
	if err != nil {
		return ForkPointResult{}, fmt.Errorf("merge-base %s %s: %w", mainBranch, commit, err)
	}
	base = strings.TrimSpace(base)
	if base == "" {
		return ForkPointResult{}, fmt.Errorf("empty merge-base for %s vs %s", commit, mainBranch)
	}
	return ForkPointResult{Base: base, Strategy: "merge-base"}, nil
}
