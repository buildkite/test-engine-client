# `bktec pool run`

`bktec pool run` runs tests from a Test Scheduler pool. It runs a worker's
leases in one of two ways:

- **In one persistent runner**, when you pass the runner command after `--`.
  The runner starts once and runs each lease in the same process, so startup
  costs such as loading the app or collecting tests are paid once instead of for
  each lease. RSpec and pytest support this.
- **With a new test command for each lease**, when you don't pass a command
  after `--`. bktec runs the test command configured like `bktec run` for each
  lease and each local retry round. Jest, Playwright and pytest support this.

Both ways share the same pool resolution, sizing with `bktec pool plan`,
authentication, local and pool retries, muting, lease prefetching and fallback
when Test Scheduler is unavailable.

> [!NOTE]
> `bktec pool run` replaces `bktec pool exec` and `bktec pool consume`, which
> still work but are deprecated. Replace `pool exec -- <runner>` with
> `pool run -- <runner>`, and `pool consume` with `pool run`.

## Run tests in one persistent runner

A persistent runner comes from your test runner's Buildkite test collector.
Each runner's guide describes its persistent runner and how to configure it:

- [RSpec](./rspec.md#run-tests-in-one-rspec-process-with-bktec-pool-run) runs
  with the [`buildkite-rspec`](https://github.com/buildkite/bktest/tree/main/test-collector-ruby)
  persistent runner from Test Collector Ruby.
- [pytest](./pytest.md#run-tests-in-one-pytest-process-with-bktec-pool-run)
  runs with the [`buildkite-pytest`](https://github.com/buildkite/bktest/tree/main/test-collector-python)
  persistent runner from `buildkite-test-collector`.

Create or reuse a pool automatically, passing the persistent runner command
after `--`. For example, an RSpec worker:

```shell
bktec pool run \
  --suite-slug my-suite \
  --test-runner rspec \
  -- bundle exec buildkite-rspec
```

Everything after `--` is passed directly to the persistent runner, not
interpreted as a shell command. `BUILDKITE_TEST_ENGINE_TEST_CMD` and
`BUILDKITE_TEST_ENGINE_RETRY_CMD` don't apply to how tests run.

Test result uploads are the runner's responsibility. Configure them through the
runner's test collector, described in its guide; bktec does not perform the
normal bktec result upload.

## Run a new test command for each lease

Configure your test runner like you would for `bktec run`, then run
`pool run` without a command after `--`:

```shell
export BUILDKITE_TEST_ENGINE_TEST_RUNNER=jest
export BUILDKITE_TEST_ENGINE_RESULT_PATH=tmp/jest-result.json

bktec pool run --suite-slug my-suite --local-retry-count 1
```

For each lease, bktec runs the test command with the lease's tests in place of
`{{testExamples}}`. Set `BUILDKITE_TEST_ENGINE_TEST_CMD` (`--test-command`) to
customize it. The command must write the runner's report to the result path,
because bktec reads that report to account for the lease. bktec removes any
earlier report before each command, so a command that doesn't write a new one
errors the lease instead of reusing an old report.

With `--local-retry-count`, bktec reruns a lease's failed tests with the retry
command. Set `BUILDKITE_TEST_ENGINE_RETRY_CMD` (`--retry-command`) to customize
it.

Each runner's default commands, requirements and limitations are described in
its guide:

- [Jest](./jest.md#run-tests-from-a-test-scheduler-pool)
- [Playwright](./playwright.md#run-tests-from-a-test-scheduler-pool)
- [pytest](./pytest.md#run-tests-from-a-test-scheduler-pool)

Test results are uploaded as they are with `bktec run`. A
[Buildkite test collector](https://buildkite.com/docs/test-engine/test-collection)
in your test runner's configuration uploads them from each command,
using the `BUILDKITE_ANALYTICS_TOKEN` that bktec provides. With
`--upload-results`, bktec uploads each command's report instead.

A lease's tests are reported as passed or failed from the runner's report. All
of a lease's tests are reported as errored when the command doesn't write a
usable report, or when it fails in a way the report can't attribute to
individual tests. See your runner's guide for the cases that error a lease.

When `bktec` receives SIGINT or SIGTERM, it interrupts the running command with
SIGINT, like Ctrl+C in a terminal, so the test runner can clean up, for example
Playwright stopping its `webServer`. This includes any processes it started,
such as Jest or Playwright under `npx`. bktec then releases its lease. If they
don't exit within 90 seconds, bktec kills them.

`BUILDKITE_TEST_ENGINE_RETRY_COUNT` and the muted-test retry options of
`bktec run` don't apply; muted failures are not retried.

## Pools

By default, workers in the same step use `BUILDKITE_STEP_ID` as the pool key.
Set `--pool-key` when different groups of workers in the same step need separate
pools.

To use a pool created with `bktec pool plan`, provide its ID:

```shell
bktec pool run \
  --pool-id "$BUILDKITE_TEST_ENGINE_POOL_ID" \
  --suite-slug my-suite \
  --test-runner rspec \
  -- bundle exec buildkite-rspec
```

### Size workers with `bktec pool plan`

Pass `--max-parallelism` (env: `BUILDKITE_TEST_ENGINE_MAX_PARALLELISM`), and
optionally `--target-time` (env: `BUILDKITE_TEST_ENGINE_TARGET_TIME`), to have
`bktec pool plan` request recommended parallelism. `--target-time` requires
`--max-parallelism`.

```shell
bktec pool plan \
  --suite-slug my-suite \
  --test-runner rspec \
  --target-time 2m \
  --max-parallelism 120 \
  --pipeline-upload pool-workers.yml
```

With these options, `pool plan` waits for Test Scheduler to finish planning,
for up to 130 seconds. It then exports `BUILDKITE_TEST_ENGINE_PARALLELISM` alongside
`BUILDKITE_TEST_ENGINE_POOL_ID`, either in `--json` output or in the
environment of `--pipeline-upload`. A completed empty plan exports `0`.

If Test Scheduler is unavailable, planning fails, or planning doesn't finish in
time, `pool plan` falls back like
`bktec plan`; see [Fallback when Test Scheduler is unavailable](#fallback-when-test-scheduler-is-unavailable).
If a ready pool does not
include recommended parallelism, `pool plan` prints a warning and exports the
`--max-parallelism` value instead, like the local fallback used by `bktec plan`.
Workers lease from the shared pool as they start, so the fallback can spread
tests across up to `--max-parallelism` jobs and use more agents than
recommended parallelism would. Workers that start after the pool is consumed
exit without running tests.

Without `--max-parallelism`, `pool plan` returns as soon as the pool is created
and exports only `BUILDKITE_TEST_ENGINE_POOL_ID`. `pool run` accepts the same
sizing options when it creates a pool, but it does not change the number of
running jobs.

When creating a pool, `pool run` supports these test discovery options:

- `--files` and `--selector-file`;
- `--test-file-pattern` and `--test-file-exclude-pattern`;
- `--location-prefix`.

## Options

| Option | Default | Description |
| --- | --- | --- |
| `--pool-id` | unset | Consume an existing pool instead of creating or reusing one. |
| `--pool-key` | `BUILDKITE_STEP_ID` | Identify the pool for workers in this build. |
| `--pool-lease-cost` | unset | Requested cost budget for each lease as `<dimension>=<budget>`; unset uses the Scheduler default. |
| `--pool-lease-max-attempts` | `0` | Requested maximum attempts per lease; `0` uses the Scheduler default. |
| `--pool-retry-count` | `0` | Retry failed or errored entries through the Scheduler, rerunning the whole entry. |
| `--local-retry-count` | `0` | Retry failed, unmuted tests locally before reporting the Scheduler attempt. |
| `--runner-startup-timeout` | `1m` | Persistent runner only: wait for the runner to become ready. |
| `--runner-batch-timeout` | `10m` | Persistent runner only: allow a batch to run and submit its report. |
| `--runner-shutdown-timeout` | `1m30s` | Persistent runner only: allow the runner to exit and flush the test collector. |

Without a persistent runner, `pool run` also accepts the test runner and upload
options of `bktec run`, such as `--test-command`, `--retry-command`,
`--result-path`, `--upload-results` and `--tag`.

`--pool-lease-cost` (env: `BUILDKITE_TEST_ENGINE_POOL_LEASE_COST`) sets the
pool's cost dimension and how much of it each lease may hold. Test Scheduler
supports `duration_p50_ms` and `duration_p90_ms`, the sum of the leased tests'
median or p90 durations in milliseconds:

```shell
bktec pool run --pool-lease-cost duration_p90_ms=120000 --suite-slug my-suite --test-runner rspec -- bundle exec buildkite-rspec
```

The deprecated `--pool-lease-duration-ms <ms>` (env:
`BUILDKITE_TEST_ENGINE_POOL_LEASE_DURATION_MS`) is the same as
`--pool-lease-cost duration_p50_ms=<ms>`. The lease options only apply when
bktec creates the pool; they're ignored with `--pool-id`.

## Authentication

OIDC authentication through `buildkite-agent` is enabled by default. To provide
a token instead, set `BUILDKITE_TEST_ENGINE_API_ACCESS_TOKEN` or use
`--access-token`. The token must include the job identity claims required by
Test Scheduler. With `--no-oidc`, it must remain valid for the entire command.

## Retries and results

Set `--pool-retry-count N` (env: `BUILDKITE_TEST_ENGINE_POOL_RETRY_COUNT`)
when creating a pool to allow up to N additional Scheduler attempts per entry.
It defaults to `0` (no pool retries) and is supported by `pool plan` and
`pool run`:

```shell
bktec pool plan --suite-slug my-suite --test-runner rspec --pool-retry-count 1 --json
```

Each pool retry reruns the whole entry (usually a file), including tests that
already passed. Attempts run one at a time per entry and stop after the first
passing attempt. Failures and errors share the same attempt budget. Any worker
can lease a retry, including the worker that ran the previous attempt; a
different worker is not guaranteed.

The retry count is fixed when the pool is created and is part of its planning
fingerprint. Use the same value when repeating planning for the same build and
pool key. It is ignored with `--pool-id` and does not apply to local fallback
execution. `--pool-lease-max-attempts` controls lease size, not retries.

`--local-retry-count` retries failed tests in the same worker. These retries
do not create additional Scheduler attempts. Muted failures are not retried.
Local retries reset for each Scheduler attempt: with `--pool-retry-count 2`
and `--local-retry-count 2`, an always-failing test can run up to nine times.

After running tests, `pool run` checks whether this job contributed a
failed or errored attempt to a terminal test failure. Original Buildkite jobs
(`BUILDKITE_RETRY_COUNT=0`) skip this check when no failed or errored attempts
remain after local retries. Retried Buildkite jobs still check for terminal
failures in retry ancestors, even when their current attempts all pass.
Unrelated pool failures and attempts that another worker successfully retried
do not fail the job. When a check is needed, a successful result is final only
once the pool is consumed. This requires the server's job failure API and
`BUILDKITE_JOB_ID` (or `--job-id`).

Retrying a Buildkite job while its pool is still running joins normal leasing.
Once the pool is consumed, a retried job does not start a runner or rerun tests.
It checks its own failure participation, not the pool's attempt totals: jobs
that contributed to a terminal failure exit with status 1, and jobs with no
terminal failures exit successfully without running tests. Rebuild to rerun
failed tests after the pool finishes.

In-job retries with `--local-retry-count` and Scheduler retries with
`--pool-retry-count` are unchanged.

If the job ID is missing or the job API returns 404, bktec keeps its previous
behavior: workers fail on their own failed or errored attempts, and retried
jobs that find a consumed pool fail if any pool attempt failed or errored.
Other job API errors fail the command.

Each Scheduler attempt is reported as passed, failed, or errored. A failed test
outcome exits with status 1. Runner process failures retain the runner's exit status
when available. Protocol, reporting, or accounting errors exit with status 16.
When `bktec` receives SIGINT or SIGTERM, it releases its active lease so another
worker can acquire those attempts without waiting for the lease to expire.

## Fallback when Test Scheduler is unavailable

Like `bktec run` and `bktec plan`, the pool commands fall back to a local split
when Test Scheduler can't plan the pool. This applies when its requests keep
failing with network errors, `429` or `5xx` responses until retries run out,
when the pool's planning fails, or when the pool isn't ready to lease within
130 seconds of creating or fetching it. Test Scheduler can take up to 15
minutes to give up on planning, so bktec doesn't wait that long. Rejected
requests (`4xx`) still fail the command.

- `pool plan` warns and exports an empty `BUILDKITE_TEST_ENGINE_POOL_ID` and
  `BUILDKITE_TEST_ENGINE_PARALLELISM` set to `--max-parallelism`, or to
  `BUILDKITE_PARALLEL_JOB_COUNT` (default 1) when that is unset. Workers given
  an empty pool ID create or reuse a pool themselves.
- `pool run` warns and runs its share of the locally discovered tests, split
  round-robin by `BUILDKITE_PARALLEL_JOB` across `BUILDKITE_PARALLEL_JOB_COUNT`
  jobs. Tests still run the same way and `--local-retry-count` still applies,
  but test selection and muting are not applied.

The fallback only happens before a worker starts leasing. If Test Scheduler
becomes unavailable once leasing has started, other workers may already have
run some tests, so the job fails instead. Because each worker decides on its
own, a worker that falls back can run tests that workers using the pool also
run, but no tests are skipped.

## Lease prefetching

While a lease runs, `bktec` requests the next lease shortly before the current
lease is expected to finish, so the runner can start it as soon as the current
lease is reported. The expected finish comes from the attempts' p50 or p90
duration costs, whichever the pool uses, adjusted by how quickly this worker
finished earlier leases. Both leases are heartbeated independently. If the
current lease is still running 30 seconds after the next lease arrives, or
`bktec` stops early, the unused lease is released so other workers can take it.
If the current lease finishes first, or the pool has no duration costs, `bktec`
requests the next lease while it reports the current one.

## Troubleshooting

The job log shows each lease as it is acquired, completed or released. Use
`bktec --debug pool run ...` to also show pool state, lease renewals and
prefetch timing, batches, and local retry rounds.

If the persistent runner does not start, check that:

- the suite and pool belong to the current Buildkite organization and build;
- OIDC is available, or a valid Scheduler OIDC token was supplied;
- the existing pool was created through Test Scheduler planning;
- the persistent runner is installed and the command after `--` starts it
  directly.
