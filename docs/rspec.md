# Using bktec with RSpec
To integrate bktec with RSpec, set the `BUILDKITE_TEST_ENGINE_TEST_RUNNER` environment variable to `rspec`. Then, specify the `BUILDKITE_TEST_ENGINE_RESULT_PATH` to define where the JSON result should be stored. bktec will instruct RSpec to output the JSON result to this path, which is necessary for bktec to read the test results for retries and verification purposes.

```sh
export BUILDKITE_TEST_ENGINE_TEST_RUNNER=rspec
export BUILDKITE_TEST_ENGINE_RESULT_PATH=tmp/result.json
```

## Configure test command
By default, bktec runs RSpec with the following command:

```sh
bundle exec rspec --format progress --format json --out {{resultPath}} {{testExamples}}
```

In this command, `{{testExamples}}` is replaced by bktec with the list of test files or tests to run, and `{{resultPath}}` is replaced with the value set in `BUILDKITE_TEST_ENGINE_RESULT_PATH`. You can customize this command using the `BUILDKITE_TEST_ENGINE_TEST_CMD` environment variable.

To customize the test command, set the following environment variable:
```sh
export BUILDKITE_TEST_ENGINE_TEST_CMD="bin/rspec --format json --out {{resultPath}} {{testExamples}}"
```

> [!IMPORTANT]
> Make sure to append `--format json --out {{resultPath}}` in your custom test command, as bktec requires this to read the test results for retries and verification purposes.

> [!IMPORTANT]
> If you have another formatter configured in an [RSpec configuration file](https://rspec.info/features/3-13/rspec-core/configuration/read-options-from-file/), the default test command will override it. To avoid this, use a custom test command and add a JSON formatter in your RSpec configuration file.

```sh
export BUILDKITE_TEST_ENGINE_RESULT_PATH=tmp/rspec-result.json
export BUILDKITE_TEST_ENGINE_TEST_CMD="bundle exec rspec {{testExamples}}"
```

Then, in your RSpec configuration file:

```sh
#.rspec
--format junit
--out rspec.xml
--format json
--out tmp/rspec-result.json
```

## Filter test files
By default, bktec runs test files that match the `spec/**/*_spec.rb` pattern. You can customize this pattern using the `BUILDKITE_TEST_ENGINE_TEST_FILE_PATTERN` environment variable. For instance, to configure bktec to only run test files inside the `spec/features` directory, use:

```sh
export BUILDKITE_TEST_ENGINE_TEST_FILE_PATTERN=spec/features/**/*_spec.rb
```

Additionally, you can exclude specific files or directories that match a certain pattern using the `BUILDKITE_TEST_ENGINE_TEST_FILE_EXCLUDE_PATTERN` environment variable. For example, to exclude test files inside the `spec/features` directory, use:

```sh
export BUILDKITE_TEST_ENGINE_TEST_FILE_EXCLUDE_PATTERN=spec/features
```

You can also use both `BUILDKITE_TEST_ENGINE_TEST_FILE_PATTERN` and `BUILDKITE_TEST_ENGINE_TEST_FILE_EXCLUDE_PATTERN` simultaneously. For example, to run all test files inside the `spec/models` directory, except those inside `spec/models/user`, use:

```sh
export BUILDKITE_TEST_ENGINE_TEST_FILE_PATTERN=spec/models/**/*_spec.rb
export BUILDKITE_TEST_ENGINE_TEST_FILE_EXCLUDE_PATTERN=spec/models/user
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

## Automatically retry failed tests
You can configure bktec to automatically retry failed tests using the `BUILDKITE_TEST_ENGINE_RETRY_COUNT` environment variable. When this variable is set to a number greater than `0`, bktec will retry each failed test up to the specified number of times, using the command set in `BUILDKITE_TEST_ENGINE_RETRY_CMD` environment variable. If this variable is not set, bktec will use either the default test command or the command specified in `BUILDKITE_TEST_ENGINE_TEST_CMD` to retry the tests.

To enable automatic retry, set the following environment variable:
```sh
export BUILDKITE_TEST_ENGINE_RETRY_COUNT=2
```

## Split slow files by individual test example
By default, bktec splits your test suite into batches of test files. In some scenarios, e.g. if your test suite has a few test files that take a very long time to run, you may want to split slow test files into individual test examples for execution. To enable this, you can set the `BUILDKITE_TEST_ENGINE_SPLIT_BY_EXAMPLE` environment variable to `true`. This setting enables bktec to dynamically split slow test files across multiple partitions based on their duration and the number of parallelism.

To enable split by example, set the following environment variable:
```sh
export BUILDKITE_TEST_ENGINE_SPLIT_BY_EXAMPLE=true
```

## Preview: Test Engine queue

`bktec queue` is an experimental Test Engine queue workflow for builds where one job discovers RSpec files and many workers lease work until the queue is drained. Enable it with:

```sh
export BKTEC_PREVIEW_TEST_QUEUE=true
export BUILDKITE_TEST_ENGINE_TEST_RUNNER=rspec
export BUILDKITE_TEST_ENGINE_RESULT_PATH=tmp/rspec-result.json
```

By default, queue commands connect to `http://127.0.0.1:9998`. Set `BUILDKITE_TEST_ENGINE_QUEUE_SERVER_URL` when `bkgo test-queue` is running somewhere else.

Generate one queue env file in the discovery step and pass that same file to every worker step. The example uses a filename-safe queue name so the env filename and metadata key can use the queue name directly.

```yaml
steps:
  - label: "Discover RSpec files"
    command: |
      queue_name=rspec
      queue_env_file="test-engine-queue-${queue_name}.env"
      queue_metadata_key="test-engine-queue-${queue_name}-env"
      bktec queue uuid --queue-name "$queue_name" > "$queue_env_file"
      buildkite-agent meta-data set "$queue_metadata_key" "$(cat "$queue_env_file")"
      source "$queue_env_file"
      bktec queue push
    env:
      BKTEC_PREVIEW_TEST_QUEUE: "true"
      BUILDKITE_TEST_ENGINE_SUITE_SLUG: "my-suite"
      BUILDKITE_TEST_ENGINE_TEST_RUNNER: "rspec"
      BUILDKITE_TEST_ENGINE_RESULT_PATH: "tmp/rspec-result.json"
      BUILDKITE_TEST_ENGINE_QUEUE_SERVER_URL: "http://127.0.0.1:9998"

  - wait

  - label: "Run queued RSpec files"
    command: |
      queue_name=rspec
      queue_env_file="test-engine-queue-${queue_name}.env"
      queue_metadata_key="test-engine-queue-${queue_name}-env"
      buildkite-agent meta-data get "$queue_metadata_key" > "$queue_env_file"
      source "$queue_env_file"
      bktec queue worker
    parallelism: 100
    env:
      BKTEC_PREVIEW_TEST_QUEUE: "true"
      BUILDKITE_TEST_ENGINE_SUITE_SLUG: "my-suite"
      BUILDKITE_TEST_ENGINE_TEST_RUNNER: "rspec"
      BUILDKITE_TEST_ENGINE_RESULT_PATH: "tmp/rspec-result.json"
      BUILDKITE_TEST_ENGINE_QUEUE_SERVER_URL: "http://127.0.0.1:9998"
```

`bktec queue uuid` writes `BUILDKITE_TEST_ENGINE_QUEUE_UUID`, `BUILDKITE_TEST_ENGINE_QUEUE_NAME`, and `BUILDKITE_TEST_ENGINE_QUEUE_ENV_FILE`. Keep those values together and include a filename-safe queue name in the env filename and metadata key; the UUID is the shared queue identity, and the name is used for display, deterministic entry IDs, and human-readable file names.

In the current preview, `bktec queue push` uses raw local file discovery. It does not call the Test Engine test-plan API and does not add Test Engine timing-based planning metadata, muted-test, skipped-test, or split-by-example enrichment. To populate custom entries, pass a JSON Lines file:

```sh
bktec queue push --file queue-entries.jsonl
```

Each line should be a queue entry. If `uuid` is omitted, bktec generates a deterministic UUID from the queue identity and test payload:

```json
{"test":{"format":"file","path":"spec/models/user_spec.rb"},"metadata":{}}
```

Workers lease batches with `BUILDKITE_TEST_ENGINE_QUEUE_BATCH_SIZE`, heartbeat leases while RSpec runs, complete successful or normally failing test executions, and can enqueue retry entries atomically when `BUILDKITE_TEST_ENGINE_RETRY_COUNT` is set. `BUILDKITE_TEST_ENGINE_QUEUE_RETRY_POSITION` controls where retry entries are placed: `front`, `back`, or `inline`.
