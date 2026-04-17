# Commit Metadata Backfill

> **Note:** This is a preview feature currently under active development.

bktec can collect historical git commit metadata from your repository and upload it to Buildkite for training test selection models. This data helps test selection identify which tests are relevant to your code changes.

The backfill commands are available under `bktec tools` and are hidden from `bktec --help` by default. Setting `BKTEC_PREVIEW_SELECTION` to a truthy value (`1`, `true`, `yes`, or `on`) makes them visible in help output. The commands can always be invoked directly regardless of this setting.

## Prerequisites

- A git repository checkout (full clone recommended for best results)
- A Buildkite API access token with `read_suites` and `write_suites` scopes
- Optional: `BKTEC_PREVIEW_SELECTION` set to a truthy value to see the commands in `bktec --help`

## Commands

### `bktec tools backfill-commit-metadata`

Collects historical git commit metadata and uploads it to Buildkite. This is the main command for the backfill workflow.

The command performs the following steps:

1. Verifies the API token has the required scopes (`read_suites` and `write_suites`)
2. Fetches the list of commit SHAs from the Buildkite API for your suite
3. Detects the default branch of your repository
4. Filters out commits that don't exist locally, and fetches missing commits from the remote
5. Collects commit metadata (author, committer, message, parent SHAs) in bulk
6. Collects diffs concurrently for each commit against its fork-point on the default branch
7. Packages everything as a compressed tarball (`commit-metadata.jsonl` + `metadata.json`)
8. Uploads the tarball to Buildkite via presigned S3

**Basic usage (flags):**

```sh
bktec tools backfill-commit-metadata \
  --access-token "bkua_..." \
  --organization-slug "my-org" \
  --suite-slug "my-suite"
```

**Or using environment variables:**

```sh
export BUILDKITE_TEST_ENGINE_API_ACCESS_TOKEN="bkua_..."
export BUILDKITE_ORGANIZATION_SLUG="my-org"
export BUILDKITE_TEST_ENGINE_SUITE_SLUG="my-suite"

bktec tools backfill-commit-metadata
```

**Write to a local file instead of uploading:**

```sh
bktec tools backfill-commit-metadata --output commit-metadata.tar.gz
```

**Skip full diffs:**

```sh
bktec tools backfill-commit-metadata --skip-diffs
```

**Customize the lookback window and concurrency:**

```sh
bktec tools backfill-commit-metadata --days 30 --concurrency 5
```

**Upload a previously generated tarball:**

```sh
bktec tools backfill-commit-metadata --upload commit-metadata.tar.gz
```

This is useful when you want to generate and upload in separate steps or when retrying a failed upload. If the command fails during upload, it retains the generated tarball locally and prints its path. You can then retry with `--upload` without re-running the entire metadata collection.

## Configuration

### Environment variables

| Environment Variable | Flag | Default | Description |
| --- | --- | --- | --- |
| `BUILDKITE_TEST_ENGINE_API_ACCESS_TOKEN` | `--access-token` | | Buildkite API access token (required) |
| `BUILDKITE_TEST_ENGINE_SUITE_SLUG` | `--suite-slug` | | Test Engine suite slug (required for backfill) |
| `BUILDKITE_ORGANIZATION_SLUG` | `--organization-slug` | | Buildkite organization slug (required) |
| `BUILDKITE_TEST_ENGINE_BASE_URL` | `--base-url` | `https://api.buildkite.com` | Buildkite API base URL |
| `BUILDKITE_TEST_ENGINE_SKIP_DIFFS` | `--skip-diffs` | `false` | Omit full git diffs from the export |
| `BUILDKITE_TEST_ENGINE_BACKFILL_DAYS` | `--days` | `90` | Number of days of commit history to export (1-90) |
| `BUILDKITE_TEST_ENGINE_REMOTE` or `BUILDKITE_TEST_ENGINE_BACKFILL_REMOTE` | `--remote` | `origin` | Git remote name for fetching and branch detection |
| `BUILDKITE_TEST_ENGINE_BACKFILL_CONCURRENCY` | `--concurrency` | `10` | Number of concurrent git operations for diff collection |
| `BUILDKITE_TEST_ENGINE_DEBUG_ENABLED` | `--debug` | `false` | Enable debug output |

When using `--upload`, only `--access-token` and `--organization-slug` are required. The `--suite-slug`, `--days`, and other backfill-specific flags are not needed because the upload endpoint is organization-scoped.

### API access token scopes

The `backfill-commit-metadata` command requires both `read_suites` (to fetch the commit list) and `write_suites` (to upload the tarball) scopes. If you use `--output` to write locally without uploading, only `read_suites` is required; a missing `write_suites` scope is downgraded to a warning.

When using `--upload`, only `write_suites` is required.

Token scopes are verified before any git work begins, so you get a fast failure if the token is misconfigured.

## Buildkite pipeline example

You can run the backfill as a Buildkite pipeline step:

```yaml
steps:
  - label: ":git: Backfill commit metadata"
    command: bktec tools backfill-commit-metadata
    env:
      BKTEC_PREVIEW_SELECTION: "true"
      BUILDKITE_TEST_ENGINE_SUITE_SLUG: "my-suite"
```

To run the backfill for multiple suites in the same repository:

```yaml
steps:
  - label: ":git: Backfill commit metadata ({{matrix}})"
    command: bktec tools backfill-commit-metadata
    matrix:
      - "my-rspec-suite"
      - "my-jest-suite"
    env:
      BKTEC_PREVIEW_SELECTION: "true"
      BUILDKITE_TEST_ENGINE_SUITE_SLUG: "{{matrix}}"
```

## How it works

### Commit list

The command fetches the list of commit SHAs from the Buildkite API. The server returns commits that appear in your suite's test execution history for the specified number of days. This means the backfill only processes commits that Buildkite has seen in test runs.

### Default branch detection

The default branch is detected using a fallback chain: `<remote>/HEAD`, then `<remote>/main`, then `<remote>/master`. The `--remote` flag controls which remote is used (default `origin`).

### Fork-point detection

For each commit, the command determines the appropriate base commit to diff against using three strategies:

1. `git merge-base --fork-point` (uses reflog data)
2. Mainline parent fallback (for commits directly on the default branch)
3. Plain `git merge-base` (for unmerged branches)

### Missing commits

Some commits from the API list may not exist in the local checkout (for example, from force-pushed branches or shallow clones). The command attempts to fetch missing commits from the remote. Commits that can't be fetched are skipped with a warning.

For best results, run from a full clone rather than a shallow clone.

### Output format

The tarball wraps its contents inside a directory named `backfill-<org>-<suite>-<timestamp>` (for example, `backfill-my-org-my-suite-20260402T100000.000Z/`). The directory contains two files:

- `commit-metadata.jsonl` -- one JSON object per line, with fields including `commit_sha`, `parent_shas`, `author_name`, `author_email`, `author_date`, `committer_name`, `committer_email`, `committer_date`, `message`, `files_changed`, `diff_stat`, `git_diff`, and `git_diff_raw`
- `metadata.json` -- archive metadata including the tool version, generation timestamp, commit count, configuration options used, and the date range of commits in the archive
