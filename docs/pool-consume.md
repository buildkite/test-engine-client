# `bktec pool consume`

`bktec pool consume` runs tests from a Test Scheduler pool, starting a new test
runner command for each lease. Jest and Playwright are supported. Use
[`bktec pool exec`](./pool-exec.md) for runners that provide a persistent
runner, such as RSpec.

Apart from how tests run, `pool consume` works like `pool exec`. Both commands
share the same pool resolution, sizing with `bktec pool plan`, authentication,
local retries, muting, lease prefetching and fallback when Test Scheduler is
unavailable. See [`bktec pool exec`](./pool-exec.md) for details.

## Run a Jest pool worker

Configure Jest like you would for [`bktec run`](./jest.md), then run
`pool consume` without a command after `--`:

```shell
export BUILDKITE_TEST_ENGINE_TEST_RUNNER=jest
export BUILDKITE_TEST_ENGINE_RESULT_PATH=tmp/jest-result.json

bktec pool consume --suite-slug my-suite --local-retry-count 1
```

For each lease, bktec runs the test command with the lease's test files:

```sh
npx jest --runTestsByPath {{testExamples}} --json --testLocationInResults --outputFile {{resultPath}}
```

Set `BUILDKITE_TEST_ENGINE_TEST_CMD` (`--test-command`) to customize it. The
command must write Jest's JSON report to `{{resultPath}}`. bktec reads that
report to account for the lease.

With `--local-retry-count`, bktec reruns a lease's failed tests with the retry
command. Unless you set `BUILDKITE_TEST_ENGINE_RETRY_CMD` (`--retry-command`),
the retry command runs only the files of those tests:

```sh
npx jest --runTestsByPath {{testExamples}} --testNamePattern '{{testNamePattern}}' --json --testLocationInResults --outputFile {{resultPath}}
```

## Run a Playwright pool worker

Configure Playwright's JSON reporter to write to the result path, like you
would for [`bktec run`](./playwright.md), then run `pool consume`:

```shell
export BUILDKITE_TEST_ENGINE_TEST_RUNNER=playwright
export BUILDKITE_TEST_ENGINE_RESULT_PATH=tmp/test-results.json

bktec pool consume --suite-slug my-suite --local-retry-count 1
```

For each lease, bktec runs the test command with the lease's test files:

```sh
npx playwright test {{testExamples}}
```

Set `BUILDKITE_TEST_ENGINE_TEST_CMD` (`--test-command`) to customize it. Each
leased test file runs in every Playwright project, and its result counts once,
across all projects: it fails if the file's tests fail in any project. Muted
tests are muted in their own project only.

With `--local-retry-count`, bktec reruns a lease's failed tests by their
`file:line` with the retry command, which defaults to the test command. Each
retried test runs in every project again.

Pools plan Playwright tests by file. `--split-by-example`
(`BUILDKITE_TEST_ENGINE_SPLIT_BY_EXAMPLE`) is not supported for Playwright
pools.

`BUILDKITE_TEST_ENGINE_RETRY_COUNT` and the muted-test retry options of
`bktec run` don't apply; muted failures are not retried.

## Results

Test results are uploaded as they are with `bktec run`. A
[JavaScript test collector](https://buildkite.com/docs/test-engine/test-collection/javascript-collectors)
in your Jest or Playwright configuration uploads them from each command, using
the `BUILDKITE_ANALYTICS_TOKEN` that bktec provides. With `--upload-results`,
bktec uploads each command's JSON report instead.

A Jest lease's tests are reported as passed or failed from the Jest report. A
test file that fails outside its tests, for example because it doesn't load, is
reported as errored without affecting the lease's other files. All of a lease's
tests are reported as errored when the command doesn't write a usable report,
when a file with failed tests also fails outside them (for example, in an
`afterAll` hook), because the Jest report doesn't say which file, or when Jest
exits with an error but reports no failed tests, such as when a coverage
threshold isn't met.

A Playwright lease's tests are reported as passed or failed from the Playwright
report. All of a lease's tests are reported as errored when the command doesn't
write a usable report, when the report has errors outside tests (for example,
because a test file doesn't load, which stops Playwright from running any
tests), or when Playwright exits with an error but reports no failed tests.
Tests from files outside the lease, such as those of a
[setup project](https://playwright.dev/docs/test-global-setup-teardown#option-1-project-dependencies),
are ignored unless they fail. A failed one errors the lease's tests, since
Playwright skips the tests that depend on it.

bktec exits with status 16 if any tests errored, or with status 1 if tests
failed.

When `bktec` receives SIGINT or SIGTERM, it stops the running command, including
any processes it started such as Jest or Playwright under `npx`, and releases
its lease. If they don't exit within 90 seconds, bktec kills them.

## Options

`pool consume` accepts the `pool exec` [options](./pool-exec.md#options) except
the persistent runner timeouts. It also accepts the test runner and upload
options of `bktec run`, such as `--test-command`, `--retry-command`,
`--result-path`, `--upload-results` and `--tag`.
