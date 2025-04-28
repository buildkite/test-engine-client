# Using bktec with go test

To integrate `bktec` with Go's testing framework, you first need to install [`gotestsum`](https://github.com/gotestyourself/gotestsum), which `bktec` uses to generate JUnit XML reports.

Set the following environment variables to configure `bktec` for your Go project:

```sh
# Tell bktec to use the Go test runner integration
export BUILDKITE_TEST_ENGINE_TEST_RUNNER=gotest

# Specify where gotestsum should write the JUnit XML report
# A unique file name per build is recommended, especially when running in parallel
export BUILDKITE_TEST_ENGINE_RESULT_PATH=tmp/gotest-result.xml
export BUILDKITE_TEST_ENGINE_SUITE_SLUG=your-suite-slug
export BUILDKITE_TEST_ENGINE_API_ACCESS_TOKEN=your-token

# Run the test engine client
bktec
```

> [!IMPORTANT]
> Due to Go's package-oriented design, file-level or example-level test splitting (like that available for RSpec or Pytest) is not supported. This means test splitting is less granular, and automatic retries operate on the entire package rather than individual tests.

## Configure test command

By default, `bktec` runs go test with the following command:

```sh
gotestsum --junitfile={{resultPath}} {{packages}}
```

In this command, `{{packages}}` is replaced by bktec with the list of packages to run, and `{{resultPath}}` is replaced with the `BUILDKITE_TEST_ENGINE_RESULT_PATH` environment variable.

You can customize this command using the `BUILDKITE_TEST_ENGINE_TEST_CMD` environment variable. For example:
```sh
export BUILDKITE_TEST_ENGINE_TEST_CMD="gotestsum --format="testname" --junitfile={{resultPath}} {{packages}}"
```

## Filter packages

Support for filtering specific packages is planned for a future release. Please let us know if this is a feature you need sooner.

## Test state management

Using `bktec` allows you to manage test states, such as muting flaky tests, directly through the Buildkite Test Engine platform. This helps in managing test suites more effectively.

## Test splitting by package

`bktec` supports package-level test splitting for Go tests.

```yaml
  - name: "Go test :golang:"
    commands:
      - bktec
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
    - bktec
  env:
    BUILDKITE_ANALYTICS_TOKEN: your-suite-token # For test collector
    BUILDKITE_TEST_ENGINE_SUITE_SLUG: your-suite-slug
    BUILDKITE_TEST_ENGINE_API_ACCESS_TOKEN: your-api-token # For state management
    BUILDKITE_TEST_ENGINE_TEST_RUNNER: gotest
    BUILDKITE_TEST_ENGINE_RESULT_PATH: tmp/gotest-result.xml
    BUILDKITE_TEST_ENGINE_RETRY_COUNT: 1
  parallelism: 2
  plugins:
    # This will make sure test result are sent to buildkite.
    - test-collector#v1.11.0:
        files: "tmp/gotest-result.xml"
        format: "junit"
```
