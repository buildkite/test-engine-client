# Using bktec with any test runner
bktec supports splitting by test files for any test runner by setting the `BUILDKITE_TEST_ENGINE_TEST_RUNNER` environment variable to `custom`.
To use bktec with a custom test runner, you must set the file patterns and test command that bktec will use to discover and run tests.

```sh
export BUILDKITE_TEST_ENGINE_TEST_RUNNER=custom
export BUILDKITE_TEST_ENGINE_TEST_CMD="bin/test {{testExamples}}"
export BUILDKITE_TEST_ENGINE_TEST_FILE_PATTERN="tests/**/test_*.js"
bktec run
```

`{{testExamples}}` in the `BUILDKITE_TEST_ENGINE_TEST_CMD` variable will be replaced by bktec with space-separated list of test files matching the `tests/**/test_*.js` pattern.
In the above example, bktec will run `bin/test` followed by list of test files that need to be run on each node.
The actual command that bktec will run on each node will look like this:

```sh
bin/test tests/test_a.js tests/test_b.js
```

> [!TIP]
> The test file pattern uses the pattern syntax supported by the [zzglob](https://github.com/DrJosh9000/zzglob?tab=readme-ov-file#pattern-syntax) library.

## Filter test files
You can exclude specific files or directories that match a certain pattern using the `BUILDKITE_TEST_ENGINE_TEST_FILE_EXCLUDE_PATTERN` environment variable. For example, to exclude test files inside the `tests/api` directory, use:

```sh
export BUILDKITE_TEST_ENGINE_TEST_FILE_EXCLUDE_PATTERN=tests/api
```

## Selector-based test splitting

The custom runner uses [selector-based test splitting](../README.md#selector-based-test-splitting) by default. Unlike the other runners, it has no built-in way to discover selectors on its own, since it doesn't know your test framework's file conventions. Provide a list of selectors with `--selector-file` (or `BUILDKITE_TEST_ENGINE_SELECTOR_FILE`), a path to a newline-delimited file of selector values:

```sh
export BUILDKITE_TEST_ENGINE_TEST_RUNNER=custom
export BUILDKITE_TEST_ENGINE_TEST_CMD="bin/test {{testExamples}}"
export BUILDKITE_TEST_ENGINE_SELECTOR_FILE=selectors.txt
bktec run
```

The values in `selectors.txt` are passed straight through to `{{testExamples}}`, one per assigned test case, the same way discovered file paths are.

If `--selector-file` isn't set, the custom runner discovers selectors using `BUILDKITE_TEST_ENGINE_TEST_FILE_PATTERN` and, when set, `BUILDKITE_TEST_ENGINE_TEST_FILE_EXCLUDE_PATTERN`. Unlike the built-in runners, the custom runner has no default file pattern, so `BUILDKITE_TEST_ENGINE_TEST_FILE_PATTERN` is required in this mode.

> [!IMPORTANT]
> The custom runner is a special case for [selector matching](../README.md#how-selectors-are-matched): `bktec` has no way to know what your test command considers a "selector", so it doesn't automatically attribute `test.selector.primary` for custom runner executions. When that tag is missing, Test Engine attempts to match selectors using the file name. If no history matches, Test Engine uses default duration estimates and bktec prints a warning. If your selectors aren't file paths and you want accurate selector history, set `test.selector.primary` yourself as an execution-level [custom tag](https://buildkite.com/docs/pipelines/configure/tests/test-suites/tags#custom-tags) (for example, in the [Test Engine JSON](#test-engine-json-example) result your runner outputs).

## Muting test results
If you have [Test state and quarantine](https://buildkite.com/docs/test-engine/test-suites/test-state-and-quarantine#lifecycle-states-mute-recommended) enabled in your Buildkite Test Suite, you can configure bktec to mute test results. When this is configured, failure from muted tests will not cause the build to fail.

To configure test muting, your test runner must output a file containing the test results, and you must set the `BUILDKITE_TEST_ENGINE_RESULT_PATH` environment variable to the path of that file.

Two result file formats are supported:

- **Test Engine JSON** (default): a JSON file in the Test Engine [test result format](https://buildkite.com/docs/test-engine/test-collection/importing-json#json-test-results-data-reference). Used when the result path does not end in `.xml`.
- **JUnit XML**: a standard JUnit XML file. Used when the result path ends in `.xml`.

### Test Engine JSON example

```sh
export BUILDKITE_TEST_ENGINE_TEST_RUNNER=custom
export BUILDKITE_TEST_ENGINE_TEST_CMD="bin/test {{testExamples}}"
export BUILDKITE_TEST_ENGINE_TEST_FILE_PATTERN="tests/**/test_*.js"
export BUILDKITE_TEST_ENGINE_RESULT_PATH="path/to/test-result.json"
bktec run
```

### JUnit XML example

```sh
export BUILDKITE_TEST_ENGINE_TEST_RUNNER=custom
export BUILDKITE_TEST_ENGINE_TEST_CMD="bin/test {{testExamples}}"
export BUILDKITE_TEST_ENGINE_TEST_FILE_PATTERN="tests/**/test_*.js"
export BUILDKITE_TEST_ENGINE_RESULT_PATH="path/to/test-result.xml"
bktec run
```
