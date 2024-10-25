# Using bktec with Jest
To integrate bktec with Jest, set the `BUILDKITE_TEST_ENGINE_TEST_RUNNER` environment variable to `jest`. Then, specify the `BUILDKITE_TEST_ENGINE_RESULT_PATH` to define where the JSON result should be stored. bktec will instruct Jest to output the JSON result to this path, which is necessary for bktec to read the test results for retries and verification purposes.

```sh
export BUILDKITE_TEST_ENGINE_TEST_RUNNER=jest
export BUILDKITE_TEST_ENGINE_RESULT_PATH=tmp/jest-result.json
```

## Configure test command
By default, bktec runs Jest with the following command:

```sh
npx jest {{testExamples}} --json --testLocationInResults --outputFile {{resultPath}}
```

In this command, `{{testExamples}}` is replaced by bktec with the list of test files or tests to run, and `{{resultPath}}` is replaced with the value set in `BUILDKITE_TEST_ENGINE_RESULT_PATH`. You can customize this command using the `BUILDKITE_TEST_ENGINE_TEST_CMD` environment variable.

To customize the test command, set the following environment variable:
```sh
export BUILDKITE_TEST_ENGINE_TEST_CMD="yarn test {{testExamples}} --json --testLocationInResults --outputFile {{resultPath}}"
```

> [!IMPORTANT]
> Make sure to append `--json --testLocationInResults --outputFile {{resultPath}}` in your custom test command, as bktec requires this to read the test results for retries and verification purposes.

## Filter test files
By default, bktec runs test files that match the `**/{__tests__/**/*,*.spec,*.test}.{ts,js,tsx,jsx}` pattern. You can customize this pattern using the `BUILDKITE_TEST_ENGINE_TEST_FILE_PATTERN` environment variable. For instance, to configure bktec to only run Jest test files inside the `src/components` directory, use:

```sh
export BUILDKITE_TEST_ENGINE_TEST_FILE_PATTERN=src/components/**/*.test.{ts,tsx}
```

Additionally, you can exclude specific files or directories that match a certain pattern using the `BUILDKITE_TEST_ENGINE_TEST_FILE_EXCLUDE_PATTERN` environment variable. For example, to exclude test files inside the `src/utilities` directory, use:

```sh
export BUILDKITE_TEST_ENGINE_TEST_FILE_EXCLUDE_PATTERN=src/utilities
```

You can also use both `BUILDKITE_TEST_ENGINE_TEST_FILE_PATTERN` and `BUILDKITE_TEST_ENGINE_TEST_FILE_EXCLUDE_PATTERN` simultaneously. For example, to run all Jest test files with `spec.ts`, except those in the `src/components` directory, use:

```sh
export BUILDKITE_TEST_ENGINE_TEST_FILE_PATTERN=**/*.spec.ts
export BUILDKITE_TEST_ENGINE_TEST_FILE_EXCLUDE_PATTERN=src/components
```

> [!TIP]
> This option accepts the pattern syntax supported by the [zzglob](https://github.com/DrJosh9000/zzglob?tab=readme-ov-file#pattern-syntax) library.

## Automatically retry failed tests
You can configure bktec to automatically retry failed tests using the `BUILDKITE_TEST_ENGINE_RETRY_COUNT` environment variable. When this variable is set to a number greater than `0`, bktec will retry each failed test up to the specified number of times, using the following command:

```sh
npx yarn --testNamePattern '{{testNamePattern}}' --json --testLocationInResults --outputFile {{resultPath}}
```

In this command, `{{testNamePattern}}` is replaced by bktec with the list of failed tests to run, and `{{resultPath}}` is replaced with the value set in `BUILDKITE_TEST_ENGINE_RESULT_PATH`. You can customize this command using the `BUILDKITE_TEST_ENGINE_RETRY_CMD` environment variable.

To enable automatic retry and customize the retry command, set the following environment variable:
```sh
export BUILDKITE_TEST_ENGINE_RETRY_CMD="yarn test --testNamePattern '{{testNamePattern}}' --json --testLocationInResults --outputFile {{resultPath}}"
export BUILDKITE_TEST_ENGINE_RETRY_COUNT=2
```

> [!IMPORTANT]
> Make sure to append `--testNamePattern '{{testNamePattern}}' --json --testLocationInResults --outputFile {{resultPath}}` in your custom retry command.
