# Using bktec with pants (Experimental)

> [!WARNING]
> Pants support is currently experimental and has limited feature support. Only the following features are supported:
>
> - Intelligent test splitting with `{{testExamples}}` and `--files`
> - Automatically retry failed tests
> - Mute tests (ignore test failures)
>
> The following features are not supported:
>
> - Filter test files (via glob patterns)
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
export BUILDKITE_TEST_ENGINE_TEST_CMD="pants --filter-target-type=python_test test //:: -- --json={{resultPath}} --merge-json"
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

## Intelligent test splitting with `{{testExamples}}`

You can use `{{testExamples}}` in your test command to have bktec inject intelligently-sharded test file paths into the pants command. This enables bktec to distribute tests across parallel nodes using historical timing data for balanced shards, while pants handles the actual test execution (building pex files, caching, etc.).

When `{{testExamples}}` is included, bktec replaces it with the subset of test files assigned to the current node. For example:

```sh
pants test {{testExamples}} -- --json={{resultPath}} --merge-json
```

becomes (for a given node):

```sh
pants test tests/test_a.py tests/test_b.py tests/test_c.py -- --json=/tmp/bktec-xxx/result.json --merge-json
```

### Using `--files` with `{{testExamples}}`

Because the `pytest-pants` runner does not support bktec's glob-based file discovery, use the `--files` flag (or `BUILDKITE_TEST_ENGINE_FILES` env var) to provide an explicit list of test files. This is especially useful when pants determines which tests need to run based on changed files and their transitive dependencies.

Create a file with one test target per line:

```
tests/test_auth.py
tests/test_api.py
tests/models/test_user.py
```

### Example: Replacing `--test-shard` with intelligent splitting

A common pants CI pattern is to use `pants filter` with `--changed-since` and `--changed-dependents=transitive` to resolve the affected test targets for a PR, then shard them across parallel agents using pants' built-in `--test-shard`. While this works, the native sharding distributes tests without considering execution time, which often leads to significantly unbalanced shards — for example, some agents finishing in 10 minutes while others run for 25 minutes.

By replacing `--test-shard` with bktec's test splitting, you get shards balanced by historical timing data. The general workflow is:

1. **Resolve affected targets** — use `pants filter` to determine which tests need to run:

   ```sh
   pants \
       --changed-since="origin/main" \
       --changed-dependents=transitive \
       --filter-target-type="+python_test" \
       filter > affected_tests.txt
   ```

2. **Create a balanced plan** — pass the target list to bktec:

   ```sh
   bktec plan --json --files affected_tests.txt
   ```

   This sends the file list to the Buildkite Test Engine API, which uses historical timing data to distribute tests across nodes. The output includes a `BUILDKITE_TEST_ENGINE_PLAN_IDENTIFIER` that subsequent steps use to fetch the plan.

3. **Run shards in parallel** — each node downloads the target list and runs its assigned shard:

   ```sh
   bktec run --files affected_tests.txt
   ```

   With `BUILDKITE_TEST_ENGINE_TEST_CMD` set to:

   ```sh
   pants test {{testExamples}} -- --json={{resultPath}} --merge-json
   ```

   bktec fetches the plan, determines which tests belong to this node, and substitutes `{{testExamples}}` with those test paths before invoking pants.

This replaces pants' `--test-shard=i/N` with bktec's timing-aware distribution. Pants still handles everything it's good at — resolving dependencies, building PEX files, caching — while bktec ensures each node gets a balanced share of the work.

> [!NOTE]
> When `{{testExamples}}` is **not** present in the test command, bktec runs the command as-is without injecting test file paths. This preserves the original behavior where pants handles test selection (e.g., via `--changed-since` or `//::` target specs).

## Filter test files

There is no support for filtering test files via glob patterns at this time. Use the `--files` flag to provide an explicit list of test files, or use pants' own filtering mechanisms in the test command.

## Automatically retry failed tests

You can configure bktec to automatically retry failed tests using the `BUILDKITE_TEST_ENGINE_RETRY_COUNT` environment variable. When this variable is set to a number greater than `0`, bktec will retry each failed test up to the specified number of times, using either the default test command or the command specified in `BUILDKITE_TEST_ENGINE_TEST_CMD`. Because pants caches test results, only failed tests will be retried.

To enable automatic retry, set the following environment variable:

```sh
export BUILDKITE_TEST_ENGINE_RETRY_COUNT=2
```

[pants-advanced-target-selection]: https://www.pantsbuild.org/stable/docs/using-pants/advanced-target-selection
