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
> - Skip tests

To integrate bktec with pants, you need to [install and configure Buildkite Test Collector for pytest](https://buildkite.com/docs/test-engine/python-collectors#pytest-collector) first. Then set the `BUILDKITE_TEST_ENGINE_TEST_RUNNER` environment variable to `pytest-pants`.

Look at the example configuration files in the [pytest_pants testdata directory](../internal/runner/testdata/pytest_pants) for an example of how to add buildkite-test-collector to the pants resolve used by pytest. Specifically:

- [pants.toml](../internal/runner/testdata/pytest_pants/pants.toml) - pants configuration
- [3rdparty/python/BUILD](../internal/runner/testdata/pytest_pants/3rdparty/python/BUILD) - python_requirement targets
- [3rdparty/python/pytest-requirements.txt](../internal/runner/testdata/pytest_pants/3rdparty/python/pytest-requirements.txt) - Python requirements.txt

In the example in the repository, you would need to generate a lockfile next, i.e.

```sh
pants generate-lockfiles --resolve=pytest
```

Only running `pants test` with `python_test` targets is supported at this time.

```sh
export BUILDKITE_TEST_ENGINE_TEST_RUNNER=pytest-pants
export BUILDKITE_TEST_ENGINE_TEST_CMD="pants --filter-target-type=python_test --changed-since=HEAD~1 test -- --json={{resultPath}} --merge-json"
bktec run
```

## Configure test command

While pants support is experimental there is no default command. That means it is required to set `BUILDKITE_TEST_ENGINE_TEST_CMD`.
Below are a few recommendations for specific scenarios:

---

```sh
export BUILDKITE_TEST_ENGINE_TEST_CMD="pants --filter-target-type=python_test test //:: -- --json={{resultPath}} --merge-json""
```

This command is a good option if you want to run all python tests in your repository.

---

```sh
export BUILDKITE_TEST_ENGINE_TEST_CMD="pants --filter-target-type=python_test --changed-since=HEAD~1 test -- --json={{resultPath}} --merge-json"
```

This command is a good option if you want to only run the python tests that were
impacted by any changes made since `HEAD~1`. Checkout [pants Advanced target
selection doc][pants-advanced-target-selection] for more information on
`--changed-since`.

---

In both commands, `{{resultPath}}` is replaced with a unique temporary path created by bktec. `--json` option is a custom pytest option added by Buildkite Test Collector to save the result into a JSON file at given path. You can further customize the test command for your specific use case.

> [!IMPORTANT]
> Make sure to append `-- --json={{resultPath}} --merge-json` in your custom pants test command, as bktec requires these options to read the test results for retries and verification purposes.

## Filter test files

There is not support for filtering test files at this time.

## Automatically retry failed tests

You can configure bktec to automatically retry failed tests using the `BUILDKITE_TEST_ENGINE_RETRY_COUNT` environment variable. When this variable is set to a number greater than `0`, bktec will retry each failed test up to the specified number of times, using either the default test command or the command specified in `BUILDKITE_TEST_ENGINE_TEST_CMD`. Because pants caches test results, only failed tests will be retried.

To enable automatic retry, set the following environment variable:

```sh
export BUILDKITE_TEST_ENGINE_RETRY_COUNT=2
```

[pants-advanced-target-selection]: https://www.pantsbuild.org/stable/docs/using-pants/advanced-target-selection
