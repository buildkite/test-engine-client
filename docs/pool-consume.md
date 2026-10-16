# `bktec pool consume`

`bktec pool consume` runs tests from a Test Scheduler pool, starting a new test
runner command for each lease. Jest, Playwright and pytest are supported. Use
[`bktec pool exec`](./pool-exec.md) for runners that provide a persistent
runner, such as RSpec and pytest.

Apart from how tests run, `pool consume` works like `pool exec`. Both commands
share the same pool resolution, sizing with `bktec pool plan`, authentication,
local retries, muting, lease prefetching and fallback when Test Scheduler is
unavailable. See [`bktec pool exec`](./pool-exec.md) for details.

## Run a pool worker

Configure your test runner like you would for `bktec run`, then run
`pool consume` without a command after `--`:

```shell
export BUILDKITE_TEST_ENGINE_TEST_RUNNER=jest
export BUILDKITE_TEST_ENGINE_RESULT_PATH=tmp/jest-result.json

bktec pool consume --suite-slug my-suite --local-retry-count 1
```

For each lease, bktec runs the test command with the lease's tests in place of
`{{testExamples}}`. Set `BUILDKITE_TEST_ENGINE_TEST_CMD` (`--test-command`) to
customize it. The command must write the runner's report to the result path,
because bktec reads that report to account for the lease.

With `--local-retry-count`, bktec reruns a lease's failed tests with the retry
command. Set `BUILDKITE_TEST_ENGINE_RETRY_CMD` (`--retry-command`) to customize
it.

Each runner's default commands, requirements and limitations are described in
its guide:

- [Jest](./jest.md#run-tests-from-a-test-scheduler-pool)
- [Playwright](./playwright.md#run-tests-from-a-test-scheduler-pool)
- [pytest](./pytest.md#run-tests-from-a-test-scheduler-pool)

## Results

Test results are uploaded as they are with `bktec run`. A
[Buildkite test collector](https://buildkite.com/docs/test-engine/test-collection)
in your test runner's configuration uploads them from each command,
using the `BUILDKITE_ANALYTICS_TOKEN` that bktec provides. With
`--upload-results`, bktec uploads each command's report instead.

A lease's tests are reported as passed or failed from the runner's report. All
of a lease's tests are reported as errored when the command doesn't write a
usable report, or when it fails in a way the report can't attribute to
individual tests. See your runner's guide for the cases that error a lease.

bktec exits with status 16 if any tests errored, or with status 1 if tests
failed.

When `bktec` receives SIGINT or SIGTERM, it interrupts the running command with
SIGINT, like Ctrl+C in a terminal, so the test runner can clean up, for example
Playwright stopping its `webServer`. This includes any processes it started,
such as Jest or Playwright under `npx`. bktec then releases its lease. If they
don't exit within 90 seconds, bktec kills them.

## Options

`pool consume` accepts the `pool exec` [options](./pool-exec.md#options) except
the persistent runner timeouts. It also accepts the test runner and upload
options of `bktec run`, such as `--test-command`, `--retry-command`,
`--result-path`, `--upload-results` and `--tag`.

`BUILDKITE_TEST_ENGINE_RETRY_COUNT` and the muted-test retry options of
`bktec run` don't apply; muted failures are not retried.
