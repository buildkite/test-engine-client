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

## Muting test results
If you have [Test state and quarantine](https://buildkite.com/docs/test-engine/test-suites/test-state-and-quarantine#lifecycle-states-mute-recommended) enabled in your Buildkite Test Suite, you can configure bktec to mute test results. When this is configured, failure from muted tests will not cause the build to fail.

To configure test muting, your test runner must output a json file containing the test results in the Test Engine [test result format](https://buildkite.com/docs/test-engine/test-collection/importing-json#json-test-results-data-reference). Then, set the `BUILDKITE_TEST_ENGINE_RESULT_PATH` environment variable to the path of the json file output by your test runner.

```sh
export BUILDKITE_TEST_ENGINE_TEST_RUNNER=custom
export BUILDKITE_TEST_ENGINE_TEST_CMD="bin/test {{testExamples}}"
export BUILDKITE_TEST_ENGINE_TEST_FILE_PATTERN="tests/**/test_*.js"
export BUILDKITE_TEST_ENGINE_RESULT_PATH="path/to/test-result.json"
bktec run
```


