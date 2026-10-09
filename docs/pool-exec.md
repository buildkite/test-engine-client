# `bktec pool exec`

`bktec pool exec` runs tests from a Test Scheduler pool using one persistent
runner process.

RSpec support requires Test Collector Ruby, which provides the
[`buildkite-rspec`](https://github.com/bktest/test-collector-ruby) persistent
runner. Linux and macOS are supported.

## Run a pool worker

Create or reuse a pool automatically:

```shell
bktec pool exec \
  --suite-slug my-suite \
  --test-runner rspec \
  -- bundle exec buildkite-rspec
```

Everything after `--` is passed directly to the persistent runner, not
interpreted as a shell command.

By default, workers in the same step use `BUILDKITE_STEP_ID` as the pool key.
Set `--pool-key` when different groups of workers in the same step need separate
pools.

To use a pool created with `bktec pool plan`, provide its ID:

```shell
bktec pool exec \
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

With these options, `pool plan` waits for Test Scheduler to finish planning. It
then exports `BUILDKITE_TEST_ENGINE_PARALLELISM` alongside
`BUILDKITE_TEST_ENGINE_POOL_ID`, either in `--json` output or in the
environment of `--pipeline-upload`. A completed empty plan exports `0`.

If Test Scheduler is unavailable or planning fails, `pool plan` falls back like
`bktec plan`; see [Fallback when Test Scheduler is unavailable](#fallback-when-test-scheduler-is-unavailable).
If a ready pool does not
include recommended parallelism, `pool plan` prints a warning and exports the
`--max-parallelism` value instead, like the local fallback used by `bktec plan`.
Workers lease from the shared pool as they start, so the fallback can spread
tests across up to `--max-parallelism` jobs and use more agents than
recommended parallelism would. Workers that start after the pool is consumed
exit without running tests.

Without `--max-parallelism`, `pool plan` returns as soon as the pool is created
and exports only `BUILDKITE_TEST_ENGINE_POOL_ID`. `pool exec` accepts the same
sizing options when it creates a pool, but it does not change the number of
running jobs.

When creating a pool, `pool exec` supports these test discovery options:

- `--files` and `--selector-file`;
- `--test-file-pattern` and `--test-file-exclude-pattern`;
- `--location-prefix`.

## Options

| Option | Default | Description |
| --- | --- | --- |
| `--pool-id` | unset | Consume an existing pool instead of creating or reusing one. |
| `--pool-key` | `BUILDKITE_STEP_ID` | Identify the pool for workers in this build. |
| `--pool-lease-duration-ms` | `0` | Requested duration budget for each lease; `0` uses the Scheduler default. |
| `--pool-lease-max-attempts` | `0` | Requested maximum attempts per lease; `0` uses the Scheduler default. |
| `--local-retry-count` | `0` | Retry failed, unmuted examples locally before reporting the Scheduler attempt. |
| `--runner-startup-timeout` | `5m` | Wait for the persistent runner to become ready. |
| `--runner-batch-timeout` | `10m` | Allow a batch to run and submit its report. |
| `--runner-shutdown-timeout` | `1m30s` | Allow the runner to exit and flush the test collector. |

## Authentication

OIDC authentication through `buildkite-agent` is enabled by default. To provide
a token instead, set `BUILDKITE_TEST_ENGINE_API_ACCESS_TOKEN` or use
`--access-token`. The token must include the job identity claims required by
Test Scheduler. With `--no-oidc`, it must remain valid for the entire command.

## Retries and results

`--local-retry-count` retries failed examples in the same worker. These retries
do not create additional Scheduler attempts. Muted failures are not retried.

Retrying a Buildkite job doesn't re-run its tests. A retried job that finds its
pool already consumed exits 1 if the pool has failed or errored attempts, so the
retry doesn't hide those failures, and exits 0 otherwise, for example after an
automatic retry for a lost agent. To re-run failed tests, rebuild instead.

Each Scheduler attempt is reported as passed, failed, or errored. A failed test
run exits with status 1. Runner process failures retain the runner's exit status
when available. Protocol, reporting, or accounting errors exit with status 16.
When `bktec` receives SIGINT or SIGTERM, it releases its active lease so another
worker can acquire those attempts without waiting for the lease to expire.

Test result uploads are the runner's responsibility. Configure them through
Test Collector Ruby; `bktec pool exec` does not perform the normal bktec result
upload.

## Fallback when Test Scheduler is unavailable

Like `bktec run` and `bktec plan`, the pool commands fall back to a local split
when Test Scheduler can't plan the pool. This applies when its requests keep
failing with network errors, `429` or `5xx` responses until retries run out,
or when the pool's planning fails. Rejected requests (`4xx`) still fail the
command.

- `pool plan` warns and exports an empty `BUILDKITE_TEST_ENGINE_POOL_ID` and
  `BUILDKITE_TEST_ENGINE_PARALLELISM` set to `--max-parallelism`, or to
  `BUILDKITE_PARALLEL_JOB_COUNT` (default 1) when that is unset. Workers given
  an empty pool ID create or reuse a pool themselves.
- `pool exec` warns and runs its share of the locally discovered tests, split
  round-robin by `BUILDKITE_PARALLEL_JOB` across `BUILDKITE_PARALLEL_JOB_COUNT`
  jobs. Tests still run in the persistent runner and `--local-retry-count`
  still applies, but test selection and muting are not applied.

The fallback only happens before a worker starts leasing. If Test Scheduler
becomes unavailable once leasing has started, other workers may already have
run some tests, so the job fails instead. Because each worker decides on its
own, a worker that falls back can run tests that workers using the pool also
run, but no tests are skipped.

## Lease prefetching

While a lease runs, `bktec` requests the next lease shortly before the current
lease is expected to finish, so the runner can start it as soon as the current
lease is reported. The expected finish comes from the attempts' p90 duration
costs, adjusted by how quickly this worker finished earlier leases. Both leases
are heartbeated independently. If the current lease is still running 30 seconds
after the next lease arrives, or `bktec` stops early, the unused lease is
released so other workers can take it. If the current lease finishes first, or
the pool has no duration costs, `bktec` requests the next lease while it reports
the current one.

## Troubleshooting

The job log shows each lease as it is acquired, completed or released. Use
`bktec --debug pool exec ...` to also show pool state, lease renewals and
prefetch timing, batches, and local retry rounds.

If the runner does not start, check that:

- the suite and pool belong to the current Buildkite organization and build;
- OIDC is available, or a valid Scheduler OIDC token was supplied;
- the existing pool was created through Test Scheduler planning;
- `buildkite-rspec` is installed and the command after `--` starts it directly.
