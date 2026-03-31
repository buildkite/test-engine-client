package git

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/buildkite/test-engine-client/internal/debug"
)

// CommitDiffs holds the diff information for a single commit relative to its fork-point.
type CommitDiffs struct {
	FilesChanged string `json:"files_changed"`
	DiffStat     string `json:"diff_stat"`
	GitDiff      string `json:"git_diff,omitempty"`
	GitDiffRaw   string `json:"git_diff_raw,omitempty"`
}

const workerCount = 10

type indexedResult struct {
	idx  int
	diff CommitDiffs
	err  error
}

// CollectDiffs collects diff information for each commit concurrently using a
// 10-goroutine worker pool. Results are returned in the same order as the input
// commits slice.
//
// For each commit it finds the fork-point and runs:
//   - git diff --no-ext-diff --name-only <base> <commit>  -> FilesChanged
//   - git diff --no-ext-diff --numstat <base> <commit>    -> DiffStat
//   - git diff --no-ext-diff <base> <commit>              -> GitDiff (unless skipDiffs)
//   - git diff --no-ext-diff --raw <base> <commit>        -> GitDiffRaw (unless skipDiffs)
//
// The onProgress callback is called after each commit is processed with the
// running count and total. It may be nil.
func CollectDiffs(
	ctx context.Context,
	runner GitRunner,
	commits []string,
	mainBranch string,
	mc *MainlineCache,
	skipDiffs bool,
	onProgress func(done, total int),
) ([]CommitDiffs, error) {
	if len(commits) == 0 {
		return nil, nil
	}

	results := make([]CommitDiffs, len(commits))
	jobs := make(chan int, len(commits))
	resultCh := make(chan indexedResult, len(commits))

	// Enqueue all jobs
	for i := range commits {
		jobs <- i
	}
	close(jobs)

	// Start workers
	var wg sync.WaitGroup
	for w := 0; w < workerCount && w < len(commits); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobs {
				diff, err := extractCommitDiffs(ctx, runner, commits[idx], mainBranch, mc, skipDiffs)
				resultCh <- indexedResult{idx: idx, diff: diff, err: err}
			}
		}()
	}

	// Close result channel when all workers finish
	go func() {
		wg.Wait()
		close(resultCh)
	}()

	// Collect results
	var processed atomic.Int32
	for res := range resultCh {
		if res.err != nil {
			debug.Printf("Warning: skipping commit %s: %v", commits[res.idx], res.err)
			// Leave the zero-value CommitDiffs for this index
		} else {
			results[res.idx] = res.diff
		}
		count := int(processed.Add(1))
		if onProgress != nil {
			onProgress(count, len(commits))
		}
	}

	return results, nil
}

// extractCommitDiffs extracts diff information for a single commit.
func extractCommitDiffs(
	ctx context.Context,
	runner GitRunner,
	commit, mainBranch string,
	mc *MainlineCache,
	skipDiffs bool,
) (CommitDiffs, error) {
	fp, err := FindForkPoint(ctx, runner, mainBranch, commit, mc)
	if err != nil {
		return CommitDiffs{}, fmt.Errorf("finding fork-point for %s: %w", commit, err)
	}
	debug.Printf("commit %s fork-point %s (strategy: %s)", commit, fp.Base, fp.Strategy)

	var diffs CommitDiffs

	// files_changed: --name-only
	if out, err := runner.Output(ctx, "diff", "--no-ext-diff", "--name-only", fp.Base, commit); err == nil {
		diffs.FilesChanged = strings.TrimRight(out, "\n")
	} else {
		debug.Printf("Warning: diff --name-only failed for %s: %v", commit, err)
	}

	// diff_stat: --numstat
	if out, err := runner.Output(ctx, "diff", "--no-ext-diff", "--numstat", fp.Base, commit); err == nil {
		diffs.DiffStat = strings.TrimRight(out, "\n")
	} else {
		debug.Printf("Warning: diff --numstat failed for %s: %v", commit, err)
	}

	if !skipDiffs {
		// git_diff: full diff
		if out, err := runner.Output(ctx, "diff", "--no-ext-diff", fp.Base, commit); err == nil {
			diffs.GitDiff = strings.TrimRight(out, "\n")
		} else {
			debug.Printf("Warning: diff failed for %s: %v", commit, err)
		}

		// git_diff_raw: --raw
		if out, err := runner.Output(ctx, "diff", "--no-ext-diff", "--raw", fp.Base, commit); err == nil {
			diffs.GitDiffRaw = strings.TrimRight(out, "\n")
		} else {
			debug.Printf("Warning: diff --raw failed for %s: %v", commit, err)
		}
	}

	return diffs, nil
}
