# Buildkite Test Engine Client

Buildkite Test Engine Client (`bktec`) is a command-line tool for running test suites with Buildkite Test Engine.

Its main responsibilities are discovering runnable tests, requesting a test plan from Test Engine, running the tests assigned to the current parallel job, reading runner results, and uploading results when configured.

Use `bktec` to split test work across parallel Buildkite jobs using timing data from Test Engine. It can also retry failed tests and mute or skip known failures for runners that support those features, while leaving you in control of the command your test runner executes.

For runner-specific setup, see the [runner guides](#runner-guides). For the canonical setup guide, see [Installing and using the Test Engine Client](https://buildkite.com/docs/pipelines/configure/tests/bktec/installing-and-using-the-client).

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
| [Selector-based test splitting](https://github.com/buildkite/test-engine-client/blob/main/README.md#selector-based-test-splitting) | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| [Split slow files by individual test example](https://github.com/buildkite/test-engine-client/blob/main/docs/rspec.md#split-slow-files-by-individual-test-example) | ✅ | ❌ | ❌ | ✅ | ❌ | ✅ | ❌ | ✅ | ❌ |
| Filter test files | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ |
| Filter tests by tag | ❌ | ❌ | ❌ | ❌ | ❌ | ✅ | ❌ | ❌ | ❌ |
| Automatically retry failed test | ✅ | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ |
| Mute tests (ignore test failures) | ✅ | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ✅ |
| Skip tests | ✅ | ❌ | ❌ | ❌ | ❌ | ✅ | ❌ | ✅ | ❌ |

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

This setup does not require a Buildkite Test Collector. If your test suite already uses a collector, set `BUILDKITE_TEST_ENGINE_UPLOAD_RESULTS: false` to avoid double uploads. See Upload test results to Test Engine section below.

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
| `BUILDKITE_PARALLEL_JOB` | The index number of a parallel job created from a Buildkite parallel build step. Defaults to `0` when the command step does not set `parallelism`. |
| `BUILDKITE_PARALLEL_JOB_COUNT` | The total number of parallel jobs created from a Buildkite parallel build step. Defaults to `1`; configure `parallelism` in your pipeline definition to split tests across multiple jobs. You can read more about Buildkite parallel build steps on this [page](https://buildkite.com/docs/pipelines/controlling-concurrency#concurrency-and-parallelism). |
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

Test Engine needs test result data to provide timing data for intelligent test splitting and to support features such as retry and muting. Choose one method to upload results from each test run. A Buildkite Test Collector is not required to use bktec.

> [!IMPORTANT]
> Do not activate both result upload methods for the same test run. If bktec and a Buildkite Test Collector both upload the results, Test Engine records duplicate test executions.

**Option 1: Use bktec's built-in upload (requires bktec 2.7.0 or later)**

Built-in upload is off by default. If your test suite already uses a collector, remove or turn off its reporter, plugin, or test integration. Then, set `BUILDKITE_TEST_ENGINE_UPLOAD_RESULTS` to `true`:

```sh
export BUILDKITE_TEST_ENGINE_UPLOAD_RESULTS=true
```

bktec uses `BUILDKITE_ANALYTICS_TOKEN` as the upload token when it is set. Otherwise, bktec requests an OIDC token. See [Authentication](#authentication) for details.

You can attach key/value tags to each upload using `--tag` or `BUILDKITE_TEST_ENGINE_TAGS`. Tags are useful for filtering and grouping test results in Test Engine.

```sh
# As CLI flags (repeatable)
bktec run --tag env=production --tag region=us-east-1

# As an environment variable (comma-separated)
export BUILDKITE_TEST_ENGINE_TAGS="env=production,region=us-east-1"
```

**Option 2: Let a [Buildkite Test Collector](https://buildkite.com/docs/test-engine/test-collection) upload results**

Test collectors are available for many languages and frameworks. Some collectors also provide richer data collection such as execution-level tagging and span tracing. bktec can continue to discover, split, run, and retry tests while the collector uploads the results.

Leave `BUILDKITE_TEST_ENGINE_UPLOAD_RESULTS` unset, or set it to `false` if it is enabled elsewhere in your pipeline:

```sh
export BUILDKITE_TEST_ENGINE_UPLOAD_RESULTS=false
```

Some runner-specific features require an installed collector package. In those cases, let the collector upload the results and keep built-in upload off. See the [runner guides](#runner-guides) and [test collector docs](https://buildkite.com/docs/test-engine/test-collection) for details on your framework.

### Relay OpenTelemetry traces

For test collectors and OpenTelemetry SDKs that export OTLP/HTTP protobuf
traces, bktec can receive those traces on loopback and relay them to Buildkite.
Enable it on `bktec run` with:

```sh
export BUILDKITE_TESTS_OTLP_RELAY=true
```

The relay injects the standard `OTEL_EXPORTER_OTLP_TRACES_*` configuration into
the test process, acknowledges local exports immediately, and retries delivery
to Buildkite in the background. It obtains and refreshes the upstream credential
through `buildkite-agent oidc request-token`, reusing bktec's own OIDC-minted
collector upload token when one exists; `--no-oidc` cannot be used with the
relay. Exporters authenticate to the relay with a random, per-run local
credential; existing collector upload credentials are left unchanged.

A transient credential failure (endpoint outage, timeout) never fails the job:
the relay starts anyway, buffers traces, and keeps retrying in the background,
reporting any traces it had to drop when the run ends. The job only fails at
startup when the Buildkite API positively refuses to issue the token (agent
exit status 77, e.g. a disallowed audience), since that cannot succeed by
retrying.

After the final test attempt, bktec spends at most 10 seconds draining queued
requests and prints the number of forwarded and dropped requests. Requests over
900 KiB are rejected synchronously, and a full 64 MiB byte queue returns HTTP
429 so the SDK can apply backpressure.

The child resource also receives
`buildkite.otlp.endpoint=<loopback URL>` through
`OTEL_RESOURCE_ATTRIBUTES`. This is a Buildkite-specific attribute—OpenTelemetry
does not define a semantic convention for the relay route—and lets stored spans
show that their SDK exported through bktec without modifying the opaque OTLP
payload in the relay.

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

The human-readable planning summary and any warnings are written to stderr, so
stdout carries only the plan. `--json`, `--plan-out` and `--pipeline-upload`
are mutually exclusive; choose one.

Both `plan` and `run` distinguish this invocation's requested selection and
split settings (`Requested`) from the returned `Selection summary` and `Split
summary`. The returned plan may be cached: `Using existing plan` identifies a
successful fetch, while a create response makes no claim about cache status.
Returned selection parameters appear only when they differ from this invocation;
matching values do not establish that the invocation created the plan. Ordinary
values use `key = value`; whitespace/control characters are escaped, long values
are bounded, and unknown request payloads are omitted.
Returned strategy names are displayed as bounded, escaped text, including names
introduced by newer servers. Applied/skipped status comes from the returned
metadata, not the strategy name or this invocation's settings.

Selection counts use the backend's eligible denominator, labelled **test
selectors**, even when it includes a mixture of file and example formats.
`Estimated compute` compares selected and candidate cumulative mean durations
using the same candidate-pool median/default fallbacks. It requires supported
metadata and at least 50% candidate timing coverage. Its share is not the
requested duration cutoff or a wall-clock saving; a zero candidate total has no
defined share. It does not use the separate top-level duration estimates.

`Estimated nodes needed` is the pre-cap count and labels the returned sizing
basis (mean or P90 durations, including median/default fallbacks). `Node limit:
capped at N` appears only for an independently binding cap, not merely a reached
limit; tied constraints are not independently binding. The longest-node estimate
uses the actual P90-packed allocation. `P90 durations; target exceeded` compares
that estimate with the target used by sizing, not the mean decision estimate or
whole-build runtime. Sparse-history and unusable-timing sizing do not use the
configured target. Estimates are not runtime guarantees, and zero estimates do
not promise instant execution. Older plans and unknown estimators leave details
unavailable rather than inferring them from current flags. Historical timing
counts and missing-history median/default estimates remain visible separately.

`--plan-out` writes what the server returns, unmodified. If the server cannot
generate a plan it returns an empty plan, which is emitted as-is (a warning is
printed to stderr). Only when the server cannot be reached at all does `bktec`
fall back to a minimal locally-generated plan; this carries the identifier and
parallelism but no tasks (it is not a computed split), and is noted on stderr.

### Saving the plan used by a run

`bktec run --plan-out <path>` writes the full plan before running tests, so
verification tools can compare results against the actual split and selection
without fetching the plan again or managing another API token:

```sh
bktec run --plan-out tmp/test-engine/plan.json
# Or configure the output path through the environment:
BUILDKITE_TEST_ENGINE_PLAN_OUT=tmp/test-engine/plan.json bktec run
```

The flag takes precedence over `BUILDKITE_TEST_ENGINE_PLAN_OUT`. Parent
directories are created and existing files are overwritten. If the file cannot
be created, written, or closed, the run stops before executing tests (exit 16).
Otherwise test execution and exit statuses are unchanged. Unlike `bktec plan`,
`run --plan-out` accepts only file paths: `-` names a literal file, not stdout.

For cached and freshly created plans, the file is the API's full `test_plan`
JSON response, indented with a trailing newline. All server fields are retained,
including `identifier`, `parallelism`, all nodes in `tasks`, test `format`,
`path`, `identifier`, and `value`, and `selection`, `skipped_reason`, and timing
metadata when present. It is saved before node-specific location-prefix
adjustments or retries. Every parallel node writes the entire plan without any
additional network requests; consumers can upload the file from node 0.

When bktec falls back to local splitting (including an empty server error plan),
the file instead records the actual local tasks with an explicit `fallback: true`:

```json
{
  "identifier": "build-id/step-id",
  "parallelism": 2,
  "tasks": {
    "0": {"node_number": 0, "tests": [{"path": "spec/apple_spec.rb"}]},
    "1": {"node_number": 1, "tests": [{"path": "spec/banana_spec.rb"}]}
  },
  "fallback": true
}
```

This is a local full-suite split, not a server selection. Verification tools
should check `fallback` before interpreting the document as a server plan.

### Selector-based test splitting

By default, `bktec` discovers tests and requests a plan by sending runner-specific **selectors**, the values Test Engine looks up when computing a split for the current job. For every runner except gotest, the selector is the same file path that bktec discovers, so selector splitting doesn't change which tests run where, only how that path is reported and matched. gotest is the exception: its selector is a Go package import path from `go list`, and selector splitting replaces the legacy even-count package split with duration-aware splitting, so Go users may see a different (and better balanced) split once historical timing data is available.

> [!NOTE]
> bktec v3 always uses selector splitting for supported runners. Existing bktec v2 releases continue to use file-based splitting; remain on v2 if file-based requests are required.
> The deprecated `--selector-splitting` flag and `BUILDKITE_TEST_ENGINE_SELECTOR_SPLITTING` environment variable are accepted so existing pipeline configuration continues to work, but their values have no effect in v3.

See [Migrating from bktec v2 to v3](./docs/migrating-to-v3.md) for collector requirements, runner-specific changes, and upgrade verification steps.

This is supported for RSpec, Jest, Vitest, Cypress, Playwright, pytest, gotest, Cucumber, and the custom runner.

By default, `bktec` discovers selectors itself using file discovery for every runner except gotest (which uses `go list` output) and custom (which falls back to its configured file pattern). You can instead provide a fixed list of selectors with `--selector-file` (or `BUILDKITE_TEST_ENGINE_SELECTOR_FILE`), a path to a newline-delimited file of selector values:

```sh
export BUILDKITE_TEST_ENGINE_SELECTOR_FILE=selectors.txt
```

#### How selectors are matched

When Test Engine computes a plan, it matches the selectors bktec sends against historical executions tagged with `test.selector.primary`. That tag is attributed automatically when results are uploaded via bktec's built-in upload, or via the Ruby, JavaScript, or Python collector at the minimum version noted in the [runner guides](#runner-guides).

If an execution has no `test.selector.primary` (for example an older collector, or the custom runner), Test Engine falls back to the file name for matching. If no selector history can be matched, Test Engine uses default duration estimates rather than legacy file timings, and bktec prints a warning. Run the tests once to record selector timings, or update to the minimum collector version noted in the runner guides if historical executions should already be available. The file-name fallback strips a configured location prefix on a best-effort basis, so updating matters most if you use a location prefix.

The custom runner is the main case that needs attention, since its selector might not be a file path. See the [custom runner guide](./docs/custom-test-runner.md#selector-based-test-splitting).

See the [runner guides](#runner-guides) for runner-specific selector details and collector requirements.

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
- Choose one result upload method so future runs have timing data: enable built-in upload with `BUILDKITE_TEST_ENGINE_UPLOAD_RESULTS=true`, or let a Buildkite Test Collector upload the results and leave this environment variable unset or set to `false`.
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
