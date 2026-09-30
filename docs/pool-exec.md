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

If planning fails, `pool plan` exits with an error. If a ready pool does not
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

Each Scheduler attempt is reported as passed, failed, or errored. A failed test
run exits with status 1. Runner process failures retain the runner's exit status
when available. Protocol, reporting, or accounting errors exit with status 16.

Test result uploads are the runner's responsibility. Configure them through
Test Collector Ruby; `bktec pool exec` does not perform the normal bktec result
upload.

## Troubleshooting

Use `bktec --debug pool exec ...` to show pool state, lease activity, local
retry rounds, and final attempt counts in the job log.

If the runner does not start, check that:

- the suite and pool belong to the current Buildkite organization and build;
- OIDC is available, or a valid Scheduler OIDC token was supplied;
- the existing pool was created through Test Scheduler planning;
- `buildkite-rspec` is installed and the command after `--` starts it directly.
