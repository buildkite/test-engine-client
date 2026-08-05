# Using bktec with Playwright
To integrate bktec with Playwright, start by configuring Playwright to output the results to a JSON file. This is necessary for bktec to read the test results for retries and verification purposes.

```js
// playwright.config.js
import { defineConfig } from '@playwright/test';

export default defineConfig({
  reporter: [
    ['json', { outputFile: './tmp/test-results.json' }]
  ],
});
```

Next, set the `BUILDKITE_TEST_ENGINE_RESULT_PATH` environment variable to the path of your JSON file.

```sh
export BUILDKITE_TEST_ENGINE_TEST_RUNNER=playwright
export BUILDKITE_TEST_ENGINE_RESULT_PATH=./tmp/test-results.json
```

## Configure test command
By default, bktec runs Playwright with the following command:

```sh
npx playwright test {{testExamples}}
```

In this command, `{{testExamples}}` is replaced by bktec with the list of test files to run. You can customize this command using the `BUILDKITE_TEST_ENGINE_TEST_CMD` environment variable.

To customize the test command, set the following environment variable:
```sh
export BUILDKITE_TEST_ENGINE_TEST_CMD="yarn test {{testExamples}}"
```

## Filter test files
By default, bktec runs test files that match the `**/{*.spec,*.test}.{ts,js}` pattern. You can customize this pattern using the `BUILDKITE_TEST_ENGINE_TEST_FILE_PATTERN` environment variable. For instance, to configure bktec to only run Playwright test files inside the `tests` directory, use:

```sh
export BUILDKITE_TEST_ENGINE_TEST_FILE_PATTERN=tests/**/*.test.ts
```

Additionally, you can exclude specific files or directories that match a certain pattern using the `BUILDKITE_TEST_ENGINE_TEST_FILE_EXCLUDE_PATTERN` environment variable. For example, to exclude test files inside the `src/components` directory, use:

```sh
export BUILDKITE_TEST_ENGINE_TEST_FILE_EXCLUDE_PATTERN=src/components
```

You can also use both `BUILDKITE_TEST_ENGINE_TEST_FILE_PATTERN` and `BUILDKITE_TEST_ENGINE_TEST_FILE_EXCLUDE_PATTERN` simultaneously. For example, to run all Playwright test files with `.spec.ts` extension, except those in the `src/components` directory, use:

```sh
export BUILDKITE_TEST_ENGINE_TEST_FILE_PATTERN=**/*.spec.ts
export BUILDKITE_TEST_ENGINE_TEST_FILE_EXCLUDE_PATTERN=src/components
```

> [!TIP]
> This option accepts the pattern syntax supported by the [zzglob](https://github.com/DrJosh9000/zzglob?tab=readme-ov-file#pattern-syntax) library.

## Location prefix
If you have configured the [Buildkite test collector](https://buildkite.com/docs/test-engine/test-collection) with a location prefix, you should set the same prefix for bktec so that test file paths match those reported by the collector. You can set this using the `--location-prefix` flag or the `BUILDKITE_TEST_ENGINE_LOCATION_PREFIX` environment variable.

```sh
bktec run --location-prefix "my/prefix/"
```

Or using the environment variable:

```sh
export BUILDKITE_TEST_ENGINE_LOCATION_PREFIX=my/prefix/
```

## Selector-based test splitting

Playwright uses [selector-based test splitting](../README.md#selector-based-test-splitting) by default. Each test file path discovered with Playwright's default `**/{*.spec,*.test}.{ts,js}` pattern becomes a selector. You can customize which files are discovered with `BUILDKITE_TEST_ENGINE_TEST_FILE_PATTERN` and `BUILDKITE_TEST_ENGINE_TEST_FILE_EXCLUDE_PATTERN`; the `{{testExamples}}` command placeholder works as before.

> [!NOTE]
> If you upload results with the [JavaScript test collector](https://buildkite.com/docs/test-engine/test-collection/javascript-collectors), we recommend updating to `buildkite-test-collector` v1.10.0 or later so selectors are attributed directly. Older versions can still upload results, and Test Engine [attempts to match selectors using the file path](../README.md#how-selectors-are-matched). If no history matches, Test Engine uses default duration estimates and bktec prints a warning. Updating matters most if you use a [location prefix](#location-prefix), since the fallback only handles the prefix on a best-effort basis.

## Automatically retry failed tests
You can configure bktec to automatically retry failed tests using the `BUILDKITE_TEST_ENGINE_RETRY_COUNT` environment variable. When this variable is set to a number greater than `0`, bktec will retry each failed test up to the specified number of times, using either the default test command or the command specified in `BUILDKITE_TEST_ENGINE_TEST_CMD`.

To enable automatic retry, set the following environment variable:
```sh
export BUILDKITE_TEST_ENGINE_RETRY_COUNT=2
```
