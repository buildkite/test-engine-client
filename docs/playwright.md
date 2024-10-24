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

## Discover and filter test files
bktec discovers the test files using a glob pattern. By default, it identifies the files matching the `**/{*.spec,*.test}.{ts,js}` pattern. This means it will recursively find all JavaScript or TypeScript files with a `.test` or `.spec` suffix, such as `/src/e2e/login.spec.ts`. You can customize this pattern using the `BUILDKITE_TEST_ENGINE_TEST_FILE_PATTERN` environment variable.

Additionally, you can exclude certain files or directories that match a specific pattern using the `BUILDKITE_TEST_ENGINE_TEST_FILE_EXCLUDE_PATTERN` environment variable.

To customize the discovery pattern and exclude certain files, set the following environment variable:
```sh
export BUILDKITE_TEST_ENGINE_TEST_PATTERN=**/*.test.ts
export BUILDKITE_TEST_ENGINE_TEST_EXCLUDE_PATTERN=**/component
```

With the above configurations, bktec will discover all files matching the `**/*.test.ts` pattern and exclude any files inside a `component` directory.

> [!TIP]
> This option accepts the pattern syntax supported by the [zzglob](https://github.com/DrJosh9000/zzglob?tab=readme-ov-file#pattern-syntax) library.

## Automatically retry failed tests
You can configure bktec to automatically retry failed tests using the `BUILDKITE_TEST_ENGINE_RETRY_COUNT` environment variable. When this variable is set to a number greater than `0`, bktec will retry each failed test up to the specified number of times, using either the default test command or the command specified in `BUILDKITE_TEST_ENGINE_TEST_CMD`.

To enable automatic retry, set the following environment variable:
```sh
export BUILDKITE_TEST_ENGINE_RETRY_COUNT=2
```
