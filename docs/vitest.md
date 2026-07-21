# Using bktec with Vitest
To integrate bktec with Vitest, set the `BUILDKITE_TEST_ENGINE_TEST_RUNNER` environment variable to `vitest`. Then, specify the `BUILDKITE_TEST_ENGINE_RESULT_PATH` to define where the JSON result should be stored. bktec will instruct Vitest to output the JSON result to this path, which is necessary for bktec to read the test results for retries and verification purposes.

```sh
export BUILDKITE_TEST_ENGINE_TEST_RUNNER=vitest
export BUILDKITE_TEST_ENGINE_RESULT_PATH=tmp/vitest-result.json
```

> [!NOTE]
> Vitest's JSON reporter is Jest-compatible, so bktec reuses the same result parsing as the Jest runner.

## Configure test command
By default, bktec runs Vitest with the following command:

```sh
npx vitest run {{testExamples}} --reporter=default --reporter=json --outputFile {{resultPath}}
```

In this command, `{{testExamples}}` is replaced by bktec with the list of test files to run, and `{{resultPath}}` is replaced with the value set in `BUILDKITE_TEST_ENGINE_RESULT_PATH`. You can customize this command using the `BUILDKITE_TEST_ENGINE_TEST_CMD` environment variable.

To customize the test command, set the following environment variable:
```sh
export BUILDKITE_TEST_ENGINE_TEST_CMD="yarn vitest run {{testExamples}} --reporter=default --reporter=json --outputFile {{resultPath}}"
```

> [!IMPORTANT]
> Make sure to append `--reporter=default --reporter=json --outputFile {{resultPath}}` in your custom test command, as bktec requires the JSON reporter to read the test results for retries and verification purposes. Also ensure you use `vitest run` (not watch mode) so the process exits after the run completes.

## Filter test files
By default, bktec runs test files that match the `**/{__tests__/**/*,*.spec,*.test}.{ts,js,tsx,jsx}` pattern. You can customize this pattern using the `BUILDKITE_TEST_ENGINE_TEST_FILE_PATTERN` environment variable. For instance, to configure bktec to only run Vitest test files inside the `src/components` directory, use:

```sh
export BUILDKITE_TEST_ENGINE_TEST_FILE_PATTERN=src/components/**/*.test.{ts,tsx}
```

Additionally, you can exclude specific files or directories that match a certain pattern using the `BUILDKITE_TEST_ENGINE_TEST_FILE_EXCLUDE_PATTERN` environment variable. For example, to exclude test files inside the `src/utilities` directory, use:

```sh
export BUILDKITE_TEST_ENGINE_TEST_FILE_EXCLUDE_PATTERN=src/utilities
```

> [!TIP]
> This option accepts the pattern syntax supported by the [zzglob](https://github.com/DrJosh9000/zzglob?tab=readme-ov-file#pattern-syntax) library.

## Location prefix
If you have configured the [Buildkite test collector](https://buildkite.com/docs/test-engine/test-collection) with a location prefix, you should set the same prefix for bktec so that test file paths match those reported by the collector. You can set this using the `--location-prefix` flag or the `BUILDKITE_TEST_ENGINE_LOCATION_PREFIX` environment variable.

```sh
export BUILDKITE_TEST_ENGINE_LOCATION_PREFIX=my/prefix/
```

## Selector-based test splitting

Vitest supports [selector-based test splitting](../README.md#selector-based-test-splitting). It doesn't change how your tests are split: the selector sent to Test Engine is the same test file path that file-based splitting already discovers using `BUILDKITE_TEST_ENGINE_TEST_FILE_PATTERN` / `_EXCLUDE_PATTERN`, so file discovery and the `{{testExamples}}` command placeholder work the same way.

> [!NOTE]
> If you upload results with the [JavaScript test collector](https://buildkite.com/docs/test-engine/test-collection/javascript-collectors), we recommend updating to `buildkite-test-collector` v1.10.0 or later so selectors are attributed directly. If you do not, nothing breaks: Test Engine [falls back to the file path](../README.md#how-selectors-are-matched) when a selector is not attributed, which keeps file-based matching working. Updating matters most if you use a [location prefix](#location-prefix), since the fallback only handles the prefix on a best-effort basis.

## Automatically retry failed tests
You can configure bktec to automatically retry failed tests using the `BUILDKITE_TEST_ENGINE_RETRY_COUNT` environment variable. When this variable is set to a number greater than `0`, bktec will retry each failed test up to the specified number of times, using the following command:

```sh
npx vitest run --testNamePattern '{{testNamePattern}}' --reporter=default --reporter=json --outputFile {{resultPath}}
```

In this command:
- `{{testNamePattern}}` is replaced by bktec with the pattern matching the failed test names
- `{{resultPath}}` is replaced with the value set in `BUILDKITE_TEST_ENGINE_RESULT_PATH`

You can customize this command using the `BUILDKITE_TEST_ENGINE_RETRY_CMD` environment variable.

```sh
export BUILDKITE_TEST_ENGINE_RETRY_CMD="yarn vitest run --testNamePattern '{{testNamePattern}}' --reporter=default --reporter=json --outputFile {{resultPath}}"
export BUILDKITE_TEST_ENGINE_RETRY_COUNT=2
```

> [!IMPORTANT]
> Make sure to append `--testNamePattern '{{testNamePattern}}' --reporter=default --reporter=json --outputFile {{resultPath}}` in your custom retry command.
