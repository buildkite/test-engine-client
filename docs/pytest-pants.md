# Using bktec with pants (Experimental)

> [!WARNING]
> Pants support is currently experimental and has limited feature support. Only the following features are supported:
>
> - Automatically retry failed tests
> - Mute tests (ignore test failures)
>
> The following features are not supported:
>
> - Filter test files
> - Split slow files by individual test example
> - Filter test by tags
> - Skip tests

Set the `BUILDKITE_TEST_ENGINE_TEST_RUNNER` environment variable to `pytest-pants` to use bktec with pants.

```sh
export BUILDKITE_TEST_ENGINE_TEST_RUNNER=pytest-pants
bktec run
```

bktec works with pytest-pants in two modes depending on which result flag you include in your test command:

- **With `--junit-xml={{resultPath}}`** (default): bktec uses JUnit XML output. No additional dependencies are required.
- **With `--json={{resultPath}}`**: bktec uses the collector's JSON output for richer test result data. This requires [Buildkite Test Collector for pytest](https://buildkite.com/docs/test-engine/python-collectors#pytest-collector) to be added to the pants resolve used by pytest.

## Configure test command

There is no default command for pants. You must set `BUILDKITE_TEST_ENGINE_TEST_CMD`.

bktec determines the output format by detecting `--junit-xml` or `--json=` in your test command. Below are a few recommendations for specific scenarios.

### JUnit XML output (default)

```sh
export BUILDKITE_TEST_ENGINE_TEST_CMD="pants --filter-target-type=python_test test //:: -- --junit-xml={{resultPath}}"
```

This command is a good option if you want to run all python tests in your repository.

```sh
export BUILDKITE_TEST_ENGINE_TEST_CMD="pants --filter-target-type=python_test --changed-since=HEAD~1 test -- --junit-xml={{resultPath}}"
```

This command is a good option if you want to only run the python tests that were impacted by any changes made since `HEAD~1`. Checkout [pants Advanced target selection doc][pants-advanced-target-selection] for more information on `--changed-since`.

> [!IMPORTANT]
> Make sure to include `-- --junit-xml={{resultPath}}` in your custom pants test command, as bktec requires this option to read the test results for retries and verification purposes.

### JSON output (with buildkite-test-collector)

To use JSON output, add `buildkite-test-collector` to the pants resolve used by pytest. Look at the example configuration files in the [pytest_pants testdata directory](../internal/runner/testdata/pytest_pants) for reference:

- [pants.toml](../internal/runner/testdata/pytest_pants/pants.toml) - pants configuration
- [3rdparty/python/BUILD](../internal/runner/testdata/pytest_pants/3rdparty/python/BUILD) - python_requirement targets
- [3rdparty/python/pytest-requirements.txt](../internal/runner/testdata/pytest_pants/3rdparty/python/pytest-requirements.txt) - Python requirements.txt

After updating the configuration, generate a lockfile:

```sh
pants generate-lockfiles --resolve=pytest
```

Then set your test command to use `--json={{resultPath}} --merge-json`:

```sh
export BUILDKITE_TEST_ENGINE_TEST_CMD="pants --filter-target-type=python_test test //:: -- --json={{resultPath}} --merge-json"
```

> [!IMPORTANT]
> Make sure to include `-- --json={{resultPath}} --merge-json` in your custom pants test command, as bktec requires these options to read the test results for retries and verification purposes.

## Filter test files

There is no support for filtering test files at this time.

## Automatically retry failed tests

You can configure bktec to automatically retry failed tests using the `BUILDKITE_TEST_ENGINE_RETRY_COUNT` environment variable. When this variable is set to a number greater than `0`, bktec will retry each failed test up to the specified number of times, using either the default test command or the command specified in `BUILDKITE_TEST_ENGINE_TEST_CMD`. Because pants caches test results, only failed tests will be retried.

To enable automatic retry, set the following environment variable:

```sh
export BUILDKITE_TEST_ENGINE_RETRY_COUNT=2
```

[pants-advanced-target-selection]: https://www.pantsbuild.org/stable/docs/using-pants/advanced-target-selection
