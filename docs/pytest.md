# Using bktec with pytest
Set the `BUILDKITE_TEST_ENGINE_TEST_RUNNER` environment variable to `pytest` to use bktec with pytest.

```sh
export BUILDKITE_TEST_ENGINE_TEST_RUNNER=pytest
bktec run
```

bktec works with pytest in two modes depending on whether [Buildkite Test Collector for pytest](https://buildkite.com/docs/test-engine/python-collectors#pytest-collector) is installed:

- **With `buildkite-test-collector`** (recommended): bktec uses the collector's JSON output for richer test result data and supports `--tag-filters`.
- **Without `buildkite-test-collector`**: bktec falls back to JUnit XML output. This is sufficient for basic parallelisation and retry, but `--tag-filters` is not available.

bktec logs which mode it is using on startup.

## Configure test command
By default, bktec runs pytest with one of the following commands depending on which mode is active:

**With `buildkite-test-collector`:**
```sh
pytest {{testExamples}} --json={{resultPath}}
```

**Without `buildkite-test-collector` (JUnit fallback):**
```sh
pytest {{testExamples}} --junit-xml={{resultPath}}
```

In both commands, `{{testExamples}}` is replaced by bktec with the list of test files or tests to run, and `{{resultPath}}` is replaced with a unique temporary path created by bktec. You can customize this command using the `BUILDKITE_TEST_ENGINE_TEST_CMD` environment variable.

To customize the test command, set the following environment variable:
```sh
export BUILDKITE_TEST_ENGINE_TEST_CMD="pytest --cache-clear --json={{resultPath}} {{testExamples}}"
```

> [!IMPORTANT]
> Make sure to include the appropriate result flag (`--json={{resultPath}}` or `--junit-xml={{resultPath}}`) in your custom test command, as bktec requires this to read the test results for retries and verification purposes.

## Filter test files
By default, bktec runs test files that match the `**/{*_test,test_*}.py` pattern. You can customize this pattern using the `BUILDKITE_TEST_ENGINE_TEST_FILE_PATTERN` environment variable. For instance, to configure bktec to only run test files inside the `tests` directory, use:

```sh
export BUILDKITE_TEST_ENGINE_TEST_FILE_PATTERN="tests/**/{*_test,test_*}.py"
```

Additionally, you can exclude specific files or directories that match a certain pattern using the `BUILDKITE_TEST_ENGINE_TEST_FILE_EXCLUDE_PATTERN` environment variable. For example, to exclude test files inside the `tests/api` directory, use:

```sh
export BUILDKITE_TEST_ENGINE_TEST_FILE_EXCLUDE_PATTERN=tests/api
```

You can also use both `BUILDKITE_TEST_ENGINE_TEST_FILE_PATTERN` and `BUILDKITE_TEST_ENGINE_TEST_FILE_EXCLUDE_PATTERN` simultaneously. For example, to run all test files inside the `tests/` directory, except those inside `tests/api`, use:

```sh
export BUILDKITE_TEST_ENGINE_TEST_FILE_PATTERN="**/{*_test,test_*}.py"
export BUILDKITE_TEST_ENGINE_TEST_FILE_EXCLUDE_PATTERN=tests/api
```

> [!TIP]
> This option accepts the pattern syntax supported by the [zzglob](https://github.com/DrJosh9000/zzglob?tab=readme-ov-file#pattern-syntax) library.

## Filter test by tags

> [!NOTE]
> Tag filtering requires `buildkite-test-collector` to be installed. bktec will exit with an error if `--tag-filters` is used without it.

You can filter tests to run based on `execution_tag` markers using the `BUILDKITE_TEST_ENGINE_TAG_FILTERS` environment variable or `--tag-filters` CLI option. 

```py
import pytest

@pytest.mark.execution_tag('gpu_allocation', '2')
def test_my_test():
    ...
```

To run tests tagged with `gpu_allocation` of `2`:

```sh
export BUILDKITE_TEST_ENGINE_TEST_RUNNER=pytest
export BUILDKITE_TEST_ENGINE_TAG_FILTERS="gpu_allocation:2"
bktec run
```

> [!NOTE]
> `execution_tag` is a custom marker added by Buildkite Test Collector for pytest. You can add multiple tags to a test, however the tag filters only support single `key:value` pairs.

## Automatically retry failed tests
You can configure bktec to automatically retry failed tests using the `BUILDKITE_TEST_ENGINE_RETRY_COUNT` environment variable. When this variable is set to a number greater than `0`, bktec will retry each failed test up to the specified number of times, using either the default test command or the command specified in `BUILDKITE_TEST_ENGINE_TEST_CMD`.

To enable automatic retry, set the following environment variable:
```sh
export BUILDKITE_TEST_ENGINE_RETRY_COUNT=2
```
