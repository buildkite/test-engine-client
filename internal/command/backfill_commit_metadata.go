package command

import (
	"context"
	"errors"
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

// BackfillCommitMetadata collects historical git commit metadata from the local
// repo and uploads it to Buildkite via presigned S3.
//
// When cfg.UploadFile is set (--upload flag), it skips all git work and uploads
// the specified tarball directly. This supports workflows where generation and
// upload happen in separate steps (e.g. air-gapped environments or retrying a
// failed upload).
//
// Auth model (TE-5834): bktec does not preflight token scopes via the
// user-token introspection endpoint. That endpoint (GET /v2/access-token) does
// not accept OIDC JWTs, so preflighting there breaks the agent-OIDC auth path.
// Instead, this command relies on the natural error path of the suite-scoped
// endpoints it has to call anyway: FetchCommitList fast-fails for read_suites
// + suite policy, and PresignUpload (run before the git work) fast-fails for
// write_suites + suite policy. The PresignUpload response is held and reused
// at the actual upload site; if S3 rejects the upload because the presigned
// URL has expired, a fresh presigned URL is fetched and the upload is retried
// once.
func BackfillCommitMetadata(ctx context.Context, cfg *config.Config, runner git.GitRunner) error {
	fmt.Fprintf(os.Stderr, "+++ Buildkite Test Engine Client: bktec %s\n\n", version.Version)

	if cfg.UploadFile != "" {
		return uploadOnly(ctx, cfg)
	}

	// 1. Create API client
	apiClient := api.NewClient(api.ClientConfig{
		AccessToken:      cfg.AccessToken,
		OrganizationSlug: cfg.OrganizationSlug,
		ServerBaseUrl:    cfg.ServerBaseUrl,
	})

	// 2. Fetch commit list from server.
	// This is the first auth-validating call. Missing read_suites, an
	// un-admitted OIDC pipeline, or a bad suite slug all surface here as a
	// typed API error before any git work runs. This preserves the fast-fail
	// UX that the dropped /v2/access-token preflight used to provide for the
	// read-scope half of the check.
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

	// 3. Preflight the upload by requesting a presigned URL up front.
	// This is the write-scope half of what the dropped /v2/access-token
	// preflight used to do: PresignUpload runs verify_write_scope server-side,
	// so missing write_suites or a wrong OIDC policy fails here, before the
	// git work. The response is held and reused at the actual upload site
	// below; if it has expired by then, the upload site re-fetches a fresh
	// URL.
	//
	// Skipped when --output is set, because there's no upload to authorise.
	// In that case missing write_suites is not an error -- the user has asked
	// for a local tarball, and write_suites is only needed if they later
	// re-run with --upload.
	var presigned api.PresignedUploadResponse
	if cfg.Output == "" {
		fmt.Fprintln(os.Stderr, "Requesting presigned upload URL...")
		presigned, err = apiClient.PresignUpload(ctx, cfg.SuiteSlug)
		if err != nil {
			return fmt.Errorf("presigning upload: %w", err)
		}
		debug.Println("Held presigned upload URL for use after git work")
	}

	// 4. Detect default branch
	defaultBranch, err := git.DetectDefaultBranch(ctx, runner, cfg.Remote)
	if err != nil {
		return fmt.Errorf("detecting default branch: %w", err)
	}
	debug.Printf("Default branch: %s", defaultBranch)

	// 5. Filter commits that exist locally
	existingCommits, missingCommits, err := git.FilterExistingCommits(ctx, runner, commits)
	if err != nil {
		return fmt.Errorf("filtering commits: %w", err)
	}

	// 6. Fetch missing commits from remote
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

	// 7. Build mainline cache
	fmt.Fprintln(os.Stderr, "Building mainline cache...")
	mc, err := git.BuildMainlineCache(ctx, runner, defaultBranch, cfg.Days)
	if err != nil {
		return fmt.Errorf("building mainline cache: %w", err)
	}
	debug.Printf("Mainline cache: %d commits", mc.Size())

	// 8. Bulk-fetch commit metadata
	fmt.Fprintln(os.Stderr, "Fetching commit metadata...")
	metadataMap, err := git.FetchBulkMetadata(ctx, runner, existingCommits)
	if err != nil {
		return fmt.Errorf("fetching metadata: %w", err)
	}

	// 9. Collect diffs (concurrent worker pool)
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

	// 10. Assemble records and compute commit date range
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

	// 11. Package as tar.gz
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
	tarInfo, err := os.Stat(tarPath)
	if err != nil {
		return fmt.Errorf("stat tarball: %w", err)
	}
	switch size := tarInfo.Size(); {
	case size >= 1024*1024:
		fmt.Fprintf(os.Stderr, "%.2f MiB\n", float64(size)/(1024*1024))
	case size >= 1024:
		fmt.Fprintf(os.Stderr, "%.2f KiB\n", float64(size)/1024)
	default:
		fmt.Fprintf(os.Stderr, "%d bytes\n", size)
	}
	removeTarball := true
	defer func() {
		if removeTarball {
			os.Remove(tarPath)
		}
	}()

	// 12. Upload or write locally
	if cfg.Output != "" {
		if err := copyFile(tarPath, cfg.Output); err != nil {
			return fmt.Errorf("writing output file: %w", err)
		}
		fmt.Fprintf(os.Stderr, "Wrote %s\n", cfg.Output)
	} else {
		fmt.Fprintln(os.Stderr, "Uploading to S3...")
		if err := uploadWithRetryOn403(ctx, apiClient, cfg.SuiteSlug, tarPath, presigned); err != nil {
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

// uploadOnly uploads a previously generated commit metadata tarball to Buildkite
// via presigned S3 POST. This is the upload-only path for --upload, intended for
// cases where generation and upload happen in separate steps.
//
// Auth model: PresignUpload is the first network call. It runs verify_write_scope
// server-side, so missing write_suites or a wrong OIDC policy surfaces here as
// a typed API error before the S3 upload runs. There is no held-response window
// to worry about in this path (no git work between PresignUpload and S3 upload),
// but we still route through uploadWithRetryOn403 to keep the retry path
// centralised and exercise the same code in both call sites.
func uploadOnly(ctx context.Context, cfg *config.Config) error {
	// 1. Defensive contract check. Callers (today: main.go) are expected to call
	// cfg.ValidateForBackfillCommitMetadata() first; this guard makes the layer
	// boundary safe if that ever stops being true. Empty suite slug would otherwise
	// produce a malformed URL with a `//` segment that 404s after a network round
	// trip.
	if cfg.SuiteSlug == "" {
		return fmt.Errorf("suite slug must not be blank (set --suite-slug or BUILDKITE_TEST_ENGINE_SUITE_SLUG)")
	}

	// 2. Verify file exists
	if _, err := os.Stat(cfg.UploadFile); err != nil {
		return fmt.Errorf("file not found: %w", err)
	}

	// 3. Create API client
	apiClient := api.NewClient(api.ClientConfig{
		AccessToken:      cfg.AccessToken,
		OrganizationSlug: cfg.OrganizationSlug,
		ServerBaseUrl:    cfg.ServerBaseUrl,
	})

	// 4. Request presigned upload URL.
	// This is the first auth-validating call; missing write_suites or a wrong
	// OIDC policy surfaces here as a typed API error.
	fmt.Fprintln(os.Stderr, "Requesting presigned upload URL...")
	presigned, err := apiClient.PresignUpload(ctx, cfg.SuiteSlug)
	if err != nil {
		return fmt.Errorf("presigning upload: %w", err)
	}

	// 5. Upload to S3
	fmt.Fprintln(os.Stderr, "Uploading to S3...")
	if err := uploadWithRetryOn403(ctx, apiClient, cfg.SuiteSlug, cfg.UploadFile, presigned); err != nil {
		return fmt.Errorf("uploading to S3: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Uploaded %s to %s\n", cfg.UploadFile, presigned.URI)
	return nil
}

// uploadWithRetryOn403 uploads filePath to S3 using the provided presigned
// form. If S3 rejects the upload with 403 Forbidden, it requests a fresh
// presigned URL and retries the upload once. Other S3 errors surface to the
// caller as-is.
//
// The common 403 case is "the presigned POST policy has expired" (recoverable
// by refreshing); less common permanent 403s (bucket-policy denial, etc.) cost
// one wasted retry against a fresh URL, which is a fair price to pay for not
// pattern-matching on AWS error message text.
//
// The "preflight + held response + retry" pattern is isolated in this helper
// so a future migration to a side-effect-free auth check is a small surface
// change rather than a refactor of the whole BackfillCommitMetadata flow.
func uploadWithRetryOn403(
	ctx context.Context,
	apiClient *api.Client,
	suiteSlug, filePath string,
	presigned api.PresignedUploadResponse,
) error {
	err := upload.UploadToS3(ctx, filePath, presigned.Form)
	if err == nil {
		return nil
	}

	var forbidden *upload.S3ForbiddenError
	if !errors.As(err, &forbidden) {
		return err
	}

	// Two-line emit: a clean operator-facing line on stderr, and the raw S3
	// XML body under debug for diagnosis if the retry itself goes wrong. The
	// raw body is too verbose to print unconditionally.
	fmt.Fprintln(os.Stderr, "S3 rejected the upload (403); requesting a fresh presigned URL and retrying...")
	debug.Printf("S3 403 response body: %s", forbidden.Body)

	fresh, err := apiClient.PresignUpload(ctx, suiteSlug)
	if err != nil {
		return fmt.Errorf("refreshing presigned upload: %w", err)
	}

	if err := upload.UploadToS3(ctx, filePath, fresh.Form); err != nil {
		return fmt.Errorf("retrying upload with fresh URL: %w", err)
	}
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

	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
