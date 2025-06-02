# Using bktec with pytest
To integrate bktec with pytest, you need to [install and configure Buildkite Test Collector for pytest](https://buildkite.com/docs/test-engine/python-collectors#pytest-collector) first. Then set the `BUILDKITE_TEST_ENGINE_TEST_RUNNER` environment variable to `pytest`.

```sh
export BUILDKITE_TEST_ENGINE_TEST_RUNNER=pytest
bktec
```

## Configure test command
By default, bktec runs pytest with the following command:

```sh
pytest {{testExamples}} --json={{resultPath}}
```

In this command, `{{testExamples}}` is replaced by bktec with the list of test files or tests to run, and `{{resultPath}}` is replaced with a unique temporary path created by bktec. `--json` option is a custom option added by Buildkite Test Collector to save the result into a JSON file at given path. You can customize this command using the `BUILDKITE_TEST_ENGINE_TEST_CMD` environment variable. 

To customize the test command, set the following environment variable:
```sh
export BUILDKITE_TEST_ENGINE_TEST_CMD="pytest --cache-clear --json={{resultPath}} {{testExamples}}"
```

> [!IMPORTANT]
> Make sure to include `--json={{resultPath}}` in your custom test command, as bktec requires this to read the test results for retries and verification purposes.

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

## Automatically retry failed tests
You can configure bktec to automatically retry failed tests using the `BUILDKITE_TEST_ENGINE_RETRY_COUNT` environment variable. When this variable is set to a number greater than `0`, bktec will retry each failed test up to the specified number of times, using either the default test command or the command specified in `BUILDKITE_TEST_ENGINE_TEST_CMD`.

To enable automatic retry, set the following environment variable:
```sh
export BUILDKITE_TEST_ENGINE_RETRY_COUNT=2
```
