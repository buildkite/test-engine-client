package command

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/buildkite/test-engine-client/internal/api"
	"github.com/buildkite/test-engine-client/internal/config"
	"github.com/buildkite/test-engine-client/internal/debug"
	"github.com/buildkite/test-engine-client/internal/git"
	"github.com/buildkite/test-engine-client/internal/packaging"
	"github.com/buildkite/test-engine-client/internal/upload"
	"github.com/buildkite/test-engine-client/internal/version"
)

// gitRunnerFactory creates a GitRunner. Tests override this to inject fakes.
var gitRunnerFactory func() git.GitRunner = func() git.GitRunner {
	return &git.ExecGitRunner{}
}

// BackfillCommitMetadata collects historical git commit metadata from the local
// repo and uploads it to Buildkite via presigned S3.
func BackfillCommitMetadata(ctx context.Context, cfg *config.Config) error {
	fmt.Fprintf(os.Stderr, "+++ Buildkite Test Engine Client: bktec %s\n\n", version.Version)

	// 1. Create API client
	apiClient := api.NewClient(api.ClientConfig{
		AccessToken:      cfg.AccessToken,
		OrganizationSlug: cfg.OrganizationSlug,
		ServerBaseUrl:    cfg.ServerBaseUrl,
	})

	// 2. Verify token scopes (fast-fail before expensive git work)
	// Always check both scopes so we can warn about missing write_suites
	// even when --output is set (the user may intend to upload later).
	tokenInfo, scopeErr := apiClient.VerifyTokenScopes(ctx, []string{"read_suites", "write_suites"})
	if scopeErr != nil {
		// Check if read_suites is present -- it's always required
		hasReadSuites := false
		if tokenInfo != nil {
			for _, s := range tokenInfo.Scopes {
				if s == "read_suites" {
					hasReadSuites = true
					break
				}
			}
		}

		if cfg.Output != "" && hasReadSuites {
			// write_suites not needed for local output, warn only
			fmt.Fprintf(os.Stderr, "Warning: %v\n", scopeErr)
		} else {
			fmt.Fprintln(os.Stderr, "The token needs read_suites and write_suites scopes.")
			return fmt.Errorf("token scope check failed: %w", scopeErr)
		}
	}
	debug.Println("Token scopes verified")

	// 3. Fetch commit list from server
	fmt.Fprintf(os.Stderr, "Fetching commit list for suite %q (last %d days)...\n", cfg.SuiteSlug, cfg.Days)
	commits, err := apiClient.FetchCommitList(ctx, cfg.SuiteSlug, cfg.Days)
	if err != nil {
		return fmt.Errorf("fetching commit list: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Server returned %d commits\n", len(commits))

	if len(commits) == 0 {
		fmt.Fprintln(os.Stderr, "No commits to process.")
		return nil
	}

	// 4. Set up git runner
	runner := gitRunnerFactory()

	// 5. Detect default branch
	defaultBranch, err := git.DetectDefaultBranch(ctx, runner, cfg.Remote)
	if err != nil {
		return fmt.Errorf("detecting default branch: %w", err)
	}
	debug.Printf("Default branch: %s", defaultBranch)

	// 6. Filter commits that exist locally
	existingCommits, missingCommits, err := git.FilterExistingCommits(ctx, runner, commits)
	if err != nil {
		return fmt.Errorf("filtering commits: %w", err)
	}

	// 7. Fetch missing commits from remote
	if len(missingCommits) > 0 {
		fmt.Fprintf(os.Stderr, "Fetching %d missing commits from %s...\n", len(missingCommits), cfg.Remote)
		unfetchable, err := git.FetchMissingCommits(ctx, runner, cfg.Remote, missingCommits)
		if err != nil {
			return fmt.Errorf("fetching missing commits: %w", err)
		}
		if unfetchable > 0 {
			fmt.Fprintf(os.Stderr, "Warning: %d commits could not be fetched (skipped)\n", unfetchable)
		}

		// Re-filter: some previously missing commits may now be available
		existingCommits, missingCommits, err = git.FilterExistingCommits(ctx, runner, commits)
		if err != nil {
			return fmt.Errorf("re-filtering commits: %w", err)
		}
	}

	if len(missingCommits) > 0 {
		fmt.Fprintf(os.Stderr, "Warning: %d commits not available locally (skipped)\n", len(missingCommits))
	}
	fmt.Fprintf(os.Stderr, "Processing %d commits\n", len(existingCommits))

	if len(existingCommits) == 0 {
		fmt.Fprintln(os.Stderr, "No commits available locally. Nothing to export.")
		return nil
	}

	// 8. Build mainline cache
	fmt.Fprintln(os.Stderr, "Building mainline cache...")
	mc, err := git.BuildMainlineCache(ctx, runner, defaultBranch)
	if err != nil {
		return fmt.Errorf("building mainline cache: %w", err)
	}
	debug.Printf("Mainline cache: %d commits", mc.Size())

	// 9. Bulk-fetch commit metadata
	fmt.Fprintln(os.Stderr, "Fetching commit metadata...")
	metadataMap, err := git.FetchBulkMetadata(ctx, runner, existingCommits)
	if err != nil {
		return fmt.Errorf("fetching metadata: %w", err)
	}

	// 10. Collect diffs (concurrent worker pool)
	fmt.Fprintln(os.Stderr, "Collecting diffs...")
	diffs, err := git.CollectDiffs(ctx, runner, existingCommits, defaultBranch, mc, cfg.SkipDiffs,
		cfg.Concurrency, func(done, total int) {
			if done%100 == 0 || done == total {
				fmt.Fprintf(os.Stderr, "\rProcessed %d/%d commits", done, total)
			}
		})
	if err != nil {
		return fmt.Errorf("collecting diffs: %w", err)
	}
	fmt.Fprintln(os.Stderr) // newline after progress

	// 11. Assemble records and compute commit date range
	var records []packaging.CommitRecord
	var minDate, maxDate string
	for i, commit := range existingCommits {
		meta, ok := metadataMap[commit]
		if !ok {
			debug.Printf("Warning: no metadata for commit %s (skipping)", commit)
			continue
		}
		record := packaging.CommitRecord{
			SchemaVersion:  1,
			CommitSHA:      meta.CommitSHA,
			ParentSHAs:     meta.ParentSHAs,
			AuthorName:     meta.AuthorName,
			AuthorEmail:    meta.AuthorEmail,
			AuthorDate:     meta.AuthorDate,
			CommitterName:  meta.CommitterName,
			CommitterEmail: meta.CommitterEmail,
			CommitterDate:  meta.CommitterDate,
			Message:        meta.Message,
			FilesChanged:   diffs[i].FilesChanged,
			DiffStat:       diffs[i].DiffStat,
			GitDiff:        diffs[i].GitDiff,
			GitDiffRaw:     diffs[i].GitDiffRaw,
		}
		records = append(records, record)

		// ISO 8601 strings are lexicographically sortable
		if minDate == "" || meta.CommitterDate < minDate {
			minDate = meta.CommitterDate
		}
		if maxDate == "" || meta.CommitterDate > maxDate {
			maxDate = meta.CommitterDate
		}
	}

	// 12. Package as tar.gz
	fmt.Fprintln(os.Stderr, "Packaging tarball...")
	archiveMeta := packaging.ArchiveMetadata{
		SchemaVersion:    1,
		Tool:             "bktec",
		ToolVersion:      version.Version,
		GeneratedAt:      time.Now().UTC().Format(time.RFC3339),
		OrganizationSlug: cfg.OrganizationSlug,
		SuiteSlug:        cfg.SuiteSlug,
		CommitCount:      len(records),
		SkippedCommits:   len(missingCommits),
		Days:             cfg.Days,
		Remote:           cfg.Remote,
		SkippedDiffs:     cfg.SkipDiffs,
		MinCommitDate:    minDate,
		MaxCommitDate:    maxDate,
	}
	tarPath, err := packaging.CreateTarball(records, archiveMeta)
	if err != nil {
		return fmt.Errorf("creating tarball: %w", err)
	}
	removeTarball := true
	defer func() {
		if removeTarball {
			os.Remove(tarPath)
		}
	}()

	// 13. Upload or write locally
	if cfg.Output != "" {
		if err := copyFile(tarPath, cfg.Output); err != nil {
			return fmt.Errorf("writing output file: %w", err)
		}
		fmt.Fprintf(os.Stderr, "Wrote %s\n", cfg.Output)
	} else {
		fmt.Fprintln(os.Stderr, "Requesting presigned upload URL...")
		presigned, err := apiClient.PresignUpload(ctx)
		if err != nil {
			removeTarball = false
			fmt.Fprintf(os.Stderr, "Tarball retained at %s\n", tarPath)
			return fmt.Errorf("presigning upload: %w", err)
		}
		fmt.Fprintln(os.Stderr, "Uploading to S3...")
		if err := upload.UploadToS3(tarPath, presigned.Form); err != nil {
			removeTarball = false
			fmt.Fprintf(os.Stderr, "Tarball retained at %s\n", tarPath)
			return fmt.Errorf("uploading to S3: %w", err)
		}
		fmt.Fprintf(os.Stderr, "Uploaded to %s\n", presigned.URI)
	}

	fmt.Fprintf(os.Stderr, "Done. %d commits exported", len(records))
	if len(missingCommits) > 0 {
		fmt.Fprintf(os.Stderr, ", %d skipped", len(missingCommits))
	}
	fmt.Fprintln(os.Stderr, ".")

	return nil
}

// copyFile copies src to dst as a fallback when os.Rename fails (cross-filesystem).
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}
