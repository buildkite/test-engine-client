# Using bktec with go test

`bktec` works with Go tests in two result modes:

- **Go JSONL output** (default): `bktec` runs [`gotestsum`](https://github.com/gotestyourself/gotestsum) with `--jsonfile={{resultPath}}`, reads the Go JSON event stream, and uploads it using the `go-jsonl` ingestion format when result uploads are enabled.
- **JUnit XML output**: `bktec` detects an explicitly configured `gotestsum --junitfile` command and reads the generated JUnit XML report.

If you use the default command, install [`gotestsum`](https://github.com/gotestyourself/gotestsum) before running `bktec`.

Set the following environment variables to configure `bktec` for your Go project:

```sh
# Tell bktec to use the Go test runner integration
export BUILDKITE_TEST_ENGINE_TEST_RUNNER=gotest

# Specify where gotestsum should write the Go JSONL report
# A unique file name per build is recommended, especially when running in parallel
export BUILDKITE_TEST_ENGINE_RESULT_PATH=tmp/gotest-result.jsonl
export BUILDKITE_TEST_ENGINE_SUITE_SLUG=your-suite-slug
export BUILDKITE_TEST_ENGINE_API_ACCESS_TOKEN=your-token

# Run the test engine client
bktec run
```

> [!IMPORTANT]
> Go splitting uses packages by default. Individual-test splitting supports top-level tests, runnable examples, and fuzz seed tests, not arbitrary dynamically generated subtests. See [individual-test splitting](#individual-test-splitting) for the supported scope.

## Configure test command

By default, `bktec` runs Go tests through `gotestsum` and parses Go JSONL output:

```sh
gotestsum --jsonfile={{resultPath}} {{packages}}
```

In this command, `{{packages}}` is replaced by bktec with the list of packages to run, and `{{resultPath}}` is replaced with the `BUILDKITE_TEST_ENGINE_RESULT_PATH` environment variable.

You can customize this command using the `BUILDKITE_TEST_ENGINE_TEST_CMD` environment variable. For example, to pass additional arguments to `go test` through `gotestsum`:

```sh
export BUILDKITE_TEST_ENGINE_TEST_CMD="gotestsum --jsonfile={{resultPath}} -- -race {{packages}}"
```

## Use Go JSONL output

Go JSONL is the default output mode. You can also configure your own test command to produce Go JSON events. When `BUILDKITE_TEST_ENGINE_UPLOAD_RESULTS=true`, `bktec` uploads the JSONL result file using the `go-jsonl` ingestion format.

### With gotestsum

Include `--jsonfile={{resultPath}}` in the command:

```sh
export BUILDKITE_TEST_ENGINE_RESULT_PATH=tmp/gotest-result.jsonl
export BUILDKITE_TEST_ENGINE_TEST_CMD="gotestsum --jsonfile={{resultPath}} {{packages}}"
```

### With go test -json

You can also run `go test -json` directly. In this mode, `bktec` captures stdout to `{{resultPath}}` while still streaming it to the build log:

```sh
export BUILDKITE_TEST_ENGINE_RESULT_PATH=tmp/gotest-result.jsonl
export BUILDKITE_TEST_ENGINE_TEST_CMD="go test -json {{packages}}"
```

> [!IMPORTANT]
> Go JSONL output contains both test-level events and package-level events. `bktec` preserves individual test and subtest results, including the parent test's inclusive duration. Parent and child durations should not be summed to estimate wall-clock time.

## Use JUnit XML output

To retain the bktec v2 JUnit XML mode, configure `gotestsum --junitfile` explicitly:

```sh
export BUILDKITE_TEST_ENGINE_RESULT_PATH=tmp/gotest-result.xml
export BUILDKITE_TEST_ENGINE_TEST_CMD="gotestsum --junitfile={{resultPath}} {{packages}}"
```

## Selector-based test splitting

go test supports [selector-based test splitting](../README.md#selector-based-test-splitting). It is enabled by default. The selector is the Go package import path, and Test Engine uses historical package duration data to balance packages across nodes instead of splitting them evenly by count.

Go suites must use [Go JSONL output](#use-go-jsonl-output) so the package import path is reported for selector attribution. Configure `gotestsum --jsonfile={{resultPath}}` or `go test -json` as described above.

If `--selector-file` isn't set, `bktec` discovers packages itself using `go list`. You can instead provide a fixed list of package import paths with `--selector-file` (or `BUILDKITE_TEST_ENGINE_SELECTOR_FILE`).

## Individual-test splitting

Set `BUILDKITE_TEST_ENGINE_SPLIT_BY_EXAMPLE=true` to distribute individual top-level tests across nodes instead of whole packages:

```sh
export BUILDKITE_TEST_ENGINE_SPLIT_BY_EXAMPLE=true
bktec run
```

Unlike the slow-file expansion used by other runners, this opt-in expands **all discovered Go packages** (or every package in `--selector-file`), not just slow ones, before server-side test selection. The server's slow-file filter uses file-name timings, while native Go JSONL reports package selectors. No server change is required: `bktec` sends discovered tests through the existing example-planning API. Without the opt-in, package-level splitting and retries are unchanged.

Listing every package compiles its test binary and runs package initialization and `TestMain` before planning. This adds discovery time for the full suite, even for packages that are already fast. Packages with no listable tests stay as package assignments. If Test Engine is unavailable, the normal fallback still splits whole packages.

Individual-test assignments use the package import path as both `scope` and `path`, and the exact Go test name as `name`. This matches Go JSONL's `(Package, Test)` result identity, so existing top-level test timings can be used without renaming tests.

Discovery runs the configured `go test` or `gotestsum` command with `-list=.`. Build flags such as `-tags` and `GOFLAGS` apply during discovery too. Test bodies are not executed, but package initialization and `TestMain` still run and must permit listing tests.

Supported units are top-level `Test…` functions, runnable `Example…` functions, and `Fuzz…` seed tests. Benchmarks and active fuzzing are not supported. Go's `-list` cannot enumerate `t.Run` children, including table-driven, nested, or duplicate-name subtests: they execute together with their top-level parent. A single slow top-level test cannot be divided further.

`bktec` runs each assigned package separately with an anchored, escaped `-run` expression. This avoids both partial-name matches and running identically named tests in another package. JSONL or JUnit reports from those commands are combined before upload. Each invocation repeats package initialization and `TestMain`, so suites with expensive package setup may benefit less from splitting.

Use the documented `go test` or `gotestsum` command forms with `{{packages}}`; do not add fixed packages, `-run`, benchmark, or active-fuzzing flags. Shell wrappers are not supported for individual-test discovery or execution. Custom retry commands must use the same result format as the main command.

## Filter packages

Support for filtering specific packages is planned for a future release. Please let us know if this is a feature you need sooner.

## Test state management

Using `bktec` allows you to manage test states, such as muting flaky tests, directly through the Buildkite Test Engine platform. This helps in managing test suites more effectively.

## Test splitting by package

`bktec` supports package-level test splitting for Go tests.

```yaml
  - name: "Go test :golang:"
    commands:
      - bktec run
    env:
      ...
    parallelism: 2 # This activate test splitting!
```



## Automatically retry failed tests

You can configure `bktec` to automatically retry failed tests using the `BUILDKITE_TEST_ENGINE_RETRY_COUNT` environment variable.
When this variable is set to a number greater than `0`, `bktec` retries failed packages up to the specified number of times by default.

With `BUILDKITE_TEST_ENGINE_SPLIT_BY_EXAMPLE=true`, it retries failed top-level tests instead. A failed subtest retries its entire top-level parent, including sibling subtests, rather than trying to rediscover or isolate dynamic children. Passing unrelated top-level tests are not retried. Package-level failures may still require a whole-package retry; compilation failures remain terminal errors.

To enable automatic retry, set the following environment variable:

```sh
export BUILDKITE_TEST_ENGINE_RETRY_COUNT=1
```

## Full Buildkite pipeline example

```yaml
- name: "Go test :golang:"
  commands:
    - bktec run
  env:
    BUILDKITE_ANALYTICS_TOKEN: your-suite-token # For uploading test data to Test Engine
    BUILDKITE_TEST_ENGINE_UPLOAD_RESULTS: "true" # This will upload test results to Test Engine using the BUILDKITE_ANALYTICS_TOKEN
    BUILDKITE_TEST_ENGINE_SUITE_SLUG: your-suite-slug
    BUILDKITE_TEST_ENGINE_API_ACCESS_TOKEN: your-api-token
    BUILDKITE_TEST_ENGINE_TEST_RUNNER: gotest
    BUILDKITE_TEST_ENGINE_RESULT_PATH: tmp/gotest-result.jsonl
    BUILDKITE_TEST_ENGINE_RETRY_COUNT: 1
  parallelism: 2
```
