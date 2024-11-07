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
