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

When a test file fails to load, Playwright runs no tests at all, so bktec runs the other test files again without it. The broken file fails the job, but failed tests in the other files are still retried. Errors that Playwright doesn't locate in one of the test files bktec ran, such as a failed `globalSetup` or an error in a helper file that a test file imports, error the whole job and stop retries.

## Run tests from a Test Scheduler pool
To run Playwright tests from a Test Scheduler pool, use [`bktec pool consume`](./pool-consume.md). It uses the test command and result path configured above, and runs Playwright once for each lease.

```sh
export BUILDKITE_TEST_ENGINE_TEST_RUNNER=playwright
export BUILDKITE_TEST_ENGINE_RESULT_PATH=tmp/test-results.json

bktec pool consume --suite-slug my-suite --local-retry-count 1
```

For each lease, bktec runs the test command with the lease's test files. Each leased test file runs in every Playwright project, and its result counts once, across all projects: it fails if the file's tests fail in any project. Muted tests are muted in their own project only.

With `--local-retry-count`, bktec reruns a lease's failed tests by their `file:line` with the retry command, which defaults to the test command. Each retried test runs in every project again, and its result in every project counts toward the file's attempt.

Pools plan Playwright tests by file. `--split-by-example` (`BUILDKITE_TEST_ENGINE_SPLIT_BY_EXAMPLE`) is not supported for Playwright pools.

Playwright also starts and stops its [`webServer`](https://playwright.dev/docs/test-webserver) with every command, so it restarts for each lease and each local retry round.

### Setup projects

Playwright runs a project's dependencies, such as a [setup project](https://playwright.dev/docs/test-global-setup-teardown#option-1-project-dependencies), with every command, whichever test files it runs. In a pool, setup runs for each lease and each local retry round, on every worker, and its tests are uploaded with each command's results. bktec doesn't count setup tests toward the lease unless one fails, which errors the lease's tests, because Playwright skips the tests that depend on it.

To run setup once per job instead, run it before bktec and add `--no-deps` to the test command. The retry command defaults to the test command, so retries skip setup too:

```sh
npx playwright test --project=setup
export BUILDKITE_TEST_ENGINE_TEST_CMD="npx playwright test --no-deps {{testExamples}}"

bktec pool consume --suite-slug my-suite --local-retry-count 1
```

Before you use `--no-deps`, check that:

- Setup saves its results outside Playwright, such as a `storageState` file or a seeded database, and they stay valid for the whole job. For example, a login session must not expire while the worker runs leases.
- Setup doesn't have a `teardown` project, or you skip it when running setup. `--project=setup` also runs the setup's teardown straight away, which undoes the setup before any tests run. For example, set `teardown: process.env.SKIP_TEARDOWN ? undefined : 'cleanup'` in the setup project, run setup with `SKIP_TEARDOWN=1`, then run `npx playwright test --project=cleanup` after bktec, even if it fails.
- None of your projects depend on projects of other tests. `--no-deps` skips every dependency of the leased tests' projects, not only setup.

If setup didn't run, or its results are missing, the leased tests fail like any other failed test rather than erroring.

### Pool results

A lease's tests are reported as passed or failed from the Playwright report.

A leased test file that doesn't load is reported as errored. Playwright doesn't run any tests when a file fails to load, so bktec runs the lease's other files again without it, and they keep their local retries.

All of a lease's tests are reported as errored when the command doesn't write a usable report, when the report has errors that Playwright doesn't locate in a leased file, such as a failed `globalSetup` or an error in a helper file that a test file imports, or when Playwright exits with an error but reports no failed tests.

A file's tests are reported as errored when Playwright skips one of them because something else failed, such as an earlier test in a [serial group](https://playwright.dev/docs/api/class-test#test-describe-serial), since that test didn't run. Tests skipped on purpose, such as with `test.skip`, are fine.

Tests from files outside the lease, such as those of a [setup project](#setup-projects), are ignored unless they fail. A failed one errors the lease's tests.
