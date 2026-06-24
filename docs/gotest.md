# Using bktec with go test

`bktec` works with Go tests in two result modes:

- **JUnit XML output** (default): `bktec` runs [`gotestsum`](https://github.com/gotestyourself/gotestsum) with `--junitfile={{resultPath}}` and reads the generated JUnit XML report.
- **Go JSONL output**: `bktec` detects `gotestsum --jsonfile` or `go test -json`, reads the Go JSON event stream, and uploads it using the `go-jsonl` ingestion format when result uploads are enabled.

If you use the default command, install [`gotestsum`](https://github.com/gotestyourself/gotestsum) before running `bktec`.

Set the following environment variables to configure `bktec` for your Go project:

```sh
# Tell bktec to use the Go test runner integration
export BUILDKITE_TEST_ENGINE_TEST_RUNNER=gotest

# Specify where gotestsum should write the JUnit XML report in the default mode
# A unique file name per build is recommended, especially when running in parallel
export BUILDKITE_TEST_ENGINE_RESULT_PATH=tmp/gotest-result.xml
export BUILDKITE_TEST_ENGINE_SUITE_SLUG=your-suite-slug
export BUILDKITE_TEST_ENGINE_API_ACCESS_TOKEN=your-token

# Run the test engine client
bktec run
```

> [!IMPORTANT]
> Due to Go's package-oriented design, file-level or example-level test splitting (like that available for RSpec or Pytest) is not supported. This means test splitting is less granular, and automatic retries operate on the entire package rather than individual tests.

## Configure test command

By default, `bktec` runs Go tests through `gotestsum` and parses JUnit XML output:

```sh
gotestsum --junitfile={{resultPath}} {{packages}}
```

In this command, `{{packages}}` is replaced by bktec with the list of packages to run, and `{{resultPath}}` is replaced with the `BUILDKITE_TEST_ENGINE_RESULT_PATH` environment variable.

You can customize this command using the `BUILDKITE_TEST_ENGINE_TEST_CMD` environment variable. For example:
```sh
export BUILDKITE_TEST_ENGINE_TEST_CMD="gotestsum --format="testname" --junitfile={{resultPath}} {{packages}}"
```

## Use Go JSONL output

To use Go JSONL output instead of JUnit XML, configure your test command to produce Go JSON events. When `BUILDKITE_TEST_ENGINE_UPLOAD_RESULTS=true`, `bktec` uploads the JSONL result file using the `go-jsonl` ingestion format.

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
> Go JSONL output contains both test-level events and package-level events. `bktec` still runs and retries Go tests by package, but reports individual tests when Go includes a test name in the JSON event stream.

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
When this variable is set to a number greater than `0`, `bktec` will retry each failed packages up to the specified number of times.

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
    BUILDKITE_TEST_ENGINE_RESULT_PATH: tmp/gotest-result.xml
    BUILDKITE_TEST_ENGINE_RETRY_COUNT: 1
  parallelism: 2
```
