# Buildkite Test Engine Client

Buildkite Test Engine Client (`bktec`) is a command-line tool for running test suites with Buildkite Test Engine.

Its main responsibilities are discovering runnable tests, requesting a test plan from Test Engine, running the tests assigned to the current parallel job, reading runner results, and uploading results when configured.

Use `bktec` to split test work across parallel Buildkite jobs using timing data from Test Engine. It can also retry failed tests and mute or skip known failures for runners that support those features, while leaving you in control of the command your test runner executes.

For runner-specific setup, see the [runner guides](#runner-guides).

## How bktec works

In a Buildkite parallel step, `bktec run` follows this general flow:

```text
Discover tests
→ request or reuse a test plan from Test Engine
→ run the tests assigned to this parallel job
→ read test runner results
→ upload results when configured
→ improve future timing data
```

`bktec` discovers tests using the configured runner, sends the discovered work and parallel job details to Test Engine, and receives a plan for the current job. It then runs only the assigned tests by expanding runner-specific placeholders such as `{{testExamples}}` and `{{resultPath}}` in the test command.

Splitting quality depends on the timing data available for the suite. First runs, newly added tests, or large changes to the test suite may be less evenly balanced until Test Engine has more recent results. Different runners support different features, and Buildkite Pipelines provides `BUILDKITE_PARALLEL_JOB` and `BUILDKITE_PARALLEL_JOB_COUNT` when `parallelism` is configured on the step.

## Supported runners and features

`bktec` supports multiple test runners. The table below shows the features supported by each runner:

<!-- DO NOT MANUALLY EDIT THE TABLE BELOW. The contents can be generate with `go run util/supported_features/main.go` -->

| Feature | RSpec | Jest | Vitest | Playwright | Cypress | pytest | gotest | Cucumber | Custom test runner |
| --- | :---: | :---: | :---: | :---: | :---: | :---: | :---: | :---: | :---: |
| Split tests by file[^1] | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ |
| [Split slow files by individual test example](https://github.com/buildkite/test-engine-client/blob/main/docs/rspec.md#split-slow-files-by-individual-test-example) | ✅ | ❌ | ❌ | ✅ | ❌ | ✅ | ❌ | ✅ | ❌ |
| Filter test files | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ |
| Filter tests by tag | ❌ | ❌ | ❌ | ❌ | ❌ | ✅ | ❌ | ❌ | ❌ |
| Automatically retry failed test | ✅ | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ |
| Mute tests (ignore test failures) | ✅ | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ✅ |
| Skip tests | ✅ | ❌ | ❌ | ✅ | ❌ | ✅ | ❌ | ✅ | ❌ |

## Installation

The latest version of bktec can be downloaded from https://github.com/buildkite/test-engine-client/releases

### Supported OS/Architecture

ARM and AMD architecture for linux, darwin, and windows

The available Go binaries

- bktec-darwin-amd64
- bktec-darwin-arm64
- bktec-linux-amd64
- bktec-linux-arm64
- bktec-windows-amd64.exe
- bktec-windows-arm64.exe

## Quickstart

Download `bktec` from the [latest release](https://github.com/buildkite/test-engine-client/releases), make it available in your test environment, and configure your Test Engine suite for authentication. From bktec 2.6.0, the default authentication path uses a Buildkite Agent OIDC token. See [Authentication](#authentication) for details.

This example runs RSpec across 10 parallel Buildkite jobs and uploads results using bktec's built-in upload support, available in bktec 2.7.0 or later:

```yaml
steps:
  - label: ":rspec: RSpec"
    command: bktec run
    parallelism: 10
    env:
      BUILDKITE_TEST_ENGINE_SUITE_SLUG: my-suite
      BUILDKITE_TEST_ENGINE_TEST_RUNNER: rspec
      BUILDKITE_TEST_ENGINE_RESULT_PATH: tmp/rspec-result.json
      BUILDKITE_TEST_ENGINE_UPLOAD_RESULTS: "true"
```

The RSpec runner defaults to:

```sh
bundle exec rspec --format progress --format json --out {{resultPath}} {{testExamples}}
```

`bktec` replaces `{{testExamples}}` with the tests assigned to the current parallel job and `{{resultPath}}` with the configured result path. Other runners may need different result output settings or commands; see the [runner guides](#runner-guides) before adapting this example.

For complete Buildkite pipeline examples across supported runners, see the [test-engine-client-examples repository](https://github.com/buildkite/test-engine-client-examples).

## Check that it worked

After the step runs, use the Buildkite job log and Test Engine suite page to check that `bktec`:

- authenticated successfully, either with OIDC or an API access token
- created or reused a test plan for the current build step
- received tests for the current parallel job
- ran the configured test command with assigned test files, examples, packages, or selectors
- uploaded results to Test Engine when `BUILDKITE_TEST_ENGINE_UPLOAD_RESULTS=true`

If the first run is uneven, check whether the suite has recent timing data. You can inspect the generated plan with `bktec plan --plan-out -`, or enable debug output with `BUILDKITE_TEST_ENGINE_DEBUG_ENABLED=true`.

## Using bktec

### Buildkite Pipeline environment variables

bktec uses the following Buildkite Pipeline provided environment variables.

| Environment Variable | Description|
| -------------------- | ----------- |
| `BUILDKITE_BUILD_ID` | The UUID of the Buildkite build. bktec uses this UUID along with `BUILDKITE_STEP_ID` to uniquely identify the test plan. |
| `BUILDKITE_JOB_ID` | The UUID of the job in Buildkite build. |
| `BUILDKITE_ORGANIZATION_SLUG` | The slug of your Buildkite organization. |
| `BUILDKITE_PARALLEL_JOB` | The index number of a parallel job created from a Buildkite parallel build step. <br>Make sure you configure `parallelism` in your pipeline definition.  You can read more about Buildkite parallel build step on this [page](https://buildkite.com/docs/pipelines/controlling-concurrency#concurrency-and-parallelism).|
| `BUILDKITE_PARALLEL_JOB_COUNT` | The total number of parallel jobs created from a Buildkite parallel build step. <br>Make sure you configure `parallelism` in your pipeline definition.  You can read more about Buildkite parallel build step on this [page](https://buildkite.com/docs/pipelines/controlling-concurrency#concurrency-and-parallelism). |
| `BUILDKITE_STEP_ID` | The UUID of the step group in Buildkite build. bktec uses this UUID along with `BUILDKITE_BUILD_ID` to uniquely identify the test plan. |

> [!IMPORTANT]
> Please make sure that the above environment variables are available in your testing environment, particularly if you use Docker or some other type of containerization to run your tests.

### Authentication

From bktec 2.6.0, bktec automatically requests a [Buildkite Agent OIDC token](https://buildkite.com/docs/agent/cli/reference/oidc) for authentication. You don't need to create or configure an API access token. You will need to [configure an OIDC policy for your Test Engine suite](https://buildkite.com/docs/pipelines/configure/tests/test-collection/oidc) to allow this.

If you're running bktec older than 2.6.0, or if you want to use an API access token instead, you can create a Buildkite API access token with `read_suites`, `read_test_plan`, and `write_test_plan` scopes from your [Personal Settings](https://buildkite.com/user/api-access-tokens) in Buildkite, then set:

```sh
export BUILDKITE_TEST_ENGINE_API_ACCESS_TOKEN=token
```

### Configure Test Engine suite slug

To use bktec, you need to configure the `BUILDKITE_TEST_ENGINE_SUITE_SLUG` environment variable with your Test Engine suite slug. You can find the suite slug in the URL of your suite. For example, in the URL `https://buildkite.com/organizations/my-organization/analytics/suites/my-suite`, the slug is `my-suite`.

```sh
export BUILDKITE_TEST_ENGINE_SUITE_SLUG=my-slug
```

### Upload test results to Test Engine

bktec needs to collect your test data to enable features like intelligent test splitting, retry, and muting. There are two ways to do this:

**Option 1: Use bktec's built-in upload (requires bktec 2.7.0 or later)**

Set `BUILDKITE_TEST_ENGINE_UPLOAD_RESULTS` to `true`:

```sh
export BUILDKITE_TEST_ENGINE_UPLOAD_RESULTS=true
```

You can attach key/value tags to each upload using `--tag` or `BUILDKITE_TEST_ENGINE_TAGS`. Tags are useful for filtering and grouping test results in Test Engine.

```sh
# As CLI flags (repeatable)
bktec run --tag env=production --tag region=us-east-1

# As an environment variable (comma-separated)
export BUILDKITE_TEST_ENGINE_TAGS="env=production,region=us-east-1"
```

**Option 2: Install a [Buildkite Test Collector](https://buildkite.com/docs/test-engine/test-collection)**

Test collectors are available for many languages and frameworks. Some collectors also provide richer data collection such as execution-level tagging and span tracing. See the [test collector docs](https://buildkite.com/docs/test-engine/test-collection) for details on what's available for your framework.

### Plan identifier

`--plan-identifier` (or `BUILDKITE_TEST_ENGINE_PLAN_IDENTIFIER`) sets the
identifier the `plan` command generates the plan under. The identifier is the
plan's server-side cache key: distinct values produce distinct plans, while
reusing a value returns the previously cached plan for that identifier.

Inside a Buildkite build you don't need to set this; the identifier defaults to
`${BUILDKITE_BUILD_ID}/${BUILDKITE_STEP_ID}`. Supply `--plan-identifier`
explicitly when generating a plan **off-agent** (for example, running it in your
local development environment or on your own machine). Doing so also removes the
need to set `BUILDKITE_BUILD_ID` and `BUILDKITE_STEP_ID`, which are otherwise
required.

> [!IMPORTANT]
> The identifier must be unique per **step**, not just per build. It is the
> cache key for the plan, so two steps sharing an identifier get the *same*
> cached plan, and the second step runs the first step's test split instead of
> its own. The default `${BUILDKITE_BUILD_ID}/${BUILDKITE_STEP_ID}` includes the
> step id for exactly this reason. When setting it yourself, use a unique value
> per request; a UUIDv7 works well, but any unique string is fine.

```sh
# UUIDv7 via Python (3.14+); use uuid4() on older versions
./bktec plan --json --plan-identifier "$(python3 -c 'import uuid; print(uuid.uuid7())')"

# UUIDv4 via uuidgen (preinstalled on macOS and most Linux)
./bktec plan --json --plan-identifier "$(uuidgen)"
```

`bktec run` accepts the same flag. It fetches the plan cached under the
identifier, or, on a cache miss, creates and caches one under it. A reused
identifier therefore returns the previously cached plan even if the inputs have
changed, so keep the value unique per distinct plan.

### Inspecting the full plan

`--plan-out` makes `bktec plan` write the full test plan without running any
tests. Unlike `--json`, which emits only the plan identifier and parallelism,
`--plan-out` writes the whole plan: the tasks, the per-node breakdown of tests,
the muted and skipped tests, and the timing metadata. It takes a destination:
`-` for stdout, or a file path.

```sh
./bktec plan --plan-out -            # stdout
./bktec plan --plan-out plan.json    # a file
```

The human-readable split summary and any warnings are written to stderr, so
stdout carries only the plan. `--json`, `--plan-out` and `--pipeline-upload`
are mutually exclusive; choose one.

`--plan-out` writes what the server returns, unmodified. If the server cannot
generate a plan it returns an empty plan, which is emitted as-is (a warning is
printed to stderr). Only when the server cannot be reached at all does `bktec`
fall back to a minimal locally-generated plan; this carries the identifier and
parallelism but no tasks (it is not a computed split), and is noted on stderr.

### Preview: Test Selection

You can pass test selection strategy configuration and additional change context to the test plan API request.
This preview is enabled only when `BKTEC_PREVIEW_SELECTION` is truthy (`1`, `true`, `yes`, or `on`).
This functionality is under development, and these flags currently have undefined behavior.

Environment variables:

```sh
export BKTEC_PREVIEW_SELECTION=true
export BUILDKITE_TEST_ENGINE_SELECTION_STRATEGY=percent
```

Command-line flags:

```sh
BKTEC_PREVIEW_SELECTION=true ./bktec plan --json --selection-strategy percent \
  --selection-param percent=40
```

#### Automatic git metadata collection

When `--selection-strategy` is set, the `plan` command automatically collects
git metadata from the current repository and sends it with the API request.
This includes commit information (SHA, author, committer, message), diff data
(files changed, numstat, full diff), and context fields (branch name, base
branch, pipeline slug, build UUID).

For pipelines that use `plan` without `--selection-strategy`, you can opt in
to metadata collection with the `--collect-git-metadata` flag (or
`BUILDKITE_TEST_ENGINE_COLLECT_GIT_METADATA=true`). This collects the same git
metadata without requiring selection to be configured:

```sh
BKTEC_PREVIEW_SELECTION=true ./bktec plan --json --collect-git-metadata
```

The base branch for diff computation is resolved using a fallback chain:

1. Explicit override via `--metadata base_branch=<branch>`
2. `BUILDKITE_PULL_REQUEST_BASE_BRANCH` (auto-set by Buildkite on PR builds)
3. Auto-detection via `<remote>/HEAD`, then `<remote>/main`, then `<remote>/master`

Most users don't need to configure anything. Override `base_branch` only if
your repository uses a non-standard default branch (for example, `develop` or `trunk`)
and `<remote>/HEAD` isn't configured.

The `--remote` flag (default `origin`) controls which git remote is used for
base branch detection. You can also set `BUILDKITE_TEST_ENGINE_REMOTE`.

Auto-collected values are merged with any explicit `--metadata` flags you
provide. Your explicit values always take precedence.

#### Manual metadata overrides

Use `--metadata key=value` to pass additional metadata or override
auto-collected values. Use `--selection-param key=value` to pass strategy
parameters. Both flags are repeatable. Values can be large and multiline.

```sh
BKTEC_PREVIEW_SELECTION=true ./bktec plan --json --selection-strategy percent \
  --selection-param percent=40 \
  --metadata base_branch=develop
```

`--selection-param` and `--metadata` are only supported as repeatable CLI flags.

### Preview: Commit Metadata Backfill

bktec can collect historical git commit metadata from your repository and upload it to Buildkite for training test selection models. This is useful for bootstrapping models with historical changeset data so that test selection can identify which tests are relevant to your code changes.

The `tools` subcommands are hidden from `bktec --help` by default. Setting `BKTEC_PREVIEW_SELECTION` to a truthy value (`1`, `true`, `yes`, or `on`) makes them visible in help output. The commands can always be invoked directly regardless of this setting.

Two commands are available under `bktec tools`:

**Collect and upload commit metadata:**

```sh
bktec tools backfill-commit-metadata \
  --access-token "bkua_..." \
  --organization-slug "my-org" \
  --suite-slug "my-suite"
```

**Generate the tarball locally for inspection before uploading:**

```sh
bktec tools backfill-commit-metadata --output commit-metadata.tar.gz

# Inspect the contents
tar tzf commit-metadata.tar.gz
# commit-metadata.jsonl
# metadata.json

# Upload when ready
bktec tools backfill-commit-metadata \
  --upload commit-metadata.tar.gz \
  --suite-slug "my-suite"
```

The API access token requires `read_suites` and `write_suites` scopes.

For detailed usage, flags, and configuration options, see the [Commit Metadata Backfill](./docs/commit-metadata-backfill.md) guide.

### Where to go next

- Configure the runner-specific command and result output for your test framework.
- Inspect a generated plan with `bktec plan --plan-out -` when you need to understand how work was split.
- Configure result uploads with `BUILDKITE_TEST_ENGINE_UPLOAD_RESULTS=true` or a Buildkite Test Collector so future runs have timing data.
- Configure retries, muting, or skipping where your runner supports them.
- Use `BUILDKITE_TEST_ENGINE_DEBUG_ENABLED=true` when troubleshooting authentication, uneven splits, or runner command issues.
- Run `bktec --help`, `bktec run --help`, or `bktec plan --help` for the current CLI options.

You can also find example configurations and usage instructions for each test runner in the [examples repository](https://github.com/buildkite/test-engine-client-examples).

### Runner guides

- [Jest](./docs/jest.md)
- [Vitest](./docs/vitest.md)
- [Playwright](./docs/playwright.md)
- [Cypress](./docs/cypress.md)
- [pytest](./docs/pytest.md)
- [go test](./docs/gotest.md)
- [RSpec](./docs/rspec.md)
- [Cucumber](./docs/cucumber.md)
- [Custom Test Runner](./docs/custom-test-runner.md)

### Debugging

To enable debug mode, set the `BUILDKITE_TEST_ENGINE_DEBUG_ENABLED` environment variable to `true`. This will print detailed output to assist in debugging bktec.

### Possible exit statuses

bktec may exit with a variety of exit statuses, outlined below:

- If there is a configuration error, bktec will exit with
  status 16.
- If the test runner (e.g. RSpec) exits cleanly, the exit status of
  the runner is returned. This will likely be 0 for successful test runs, 1 for
  failing test runs, but may be any other error status returned by the runner.
- If the test runner is terminated by an OS level signal, such as SIGSEGV or
  SIGABRT, the exit status returned will be equal to 128 plus the signal number.
  For example, if the runner raises a SIGSEGV, the exit status will be (128 +
  11) = 139.

## Development

Make sure you have Go, Ruby, and Node.js installed in your environment. You can follow the installation guides for each of these tools:

- [Go Installation Guide](https://golang.org/doc/install)
- [Ruby Installation Guide](https://www.ruby-lang.org/en/documentation/installation/)
- [Node.js Installation Guide](https://nodejs.org/en/download/package-manager/)

Once you have these dependencies installed, run `bin/setup` to install dependencies for the sample projects for testing purposes.

To test, run:
```sh
./bin/test
```

[^1]: NB: For go test, test splitting is by package and not by file.
