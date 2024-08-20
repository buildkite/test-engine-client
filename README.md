# Buildkite Test Splitter

Buildkite Test Splitter is an open source tool to orchestrate your test suites. It uses your Buildkite Test Analytics suite data to intelligently partition and parallelise your tests. 
Test Splitter currently only supports RSpec but support for other frameworks is coming soon.

## Installation
The latest version of Buildkite Test Splitter can be downloaded from https://github.com/buildkite/test-splitter/releases

### Supported OS/Architecture
ARM and AMD architecture for linux and darwin

The available Go binaries
- test-splitter-darwin-amd64
- test-splitter-darwin-arm64
- test-splitter-linux-amd64
- test-splitter-linux-arm64

## Using the Test Splitter

### ENV variables
Test Splitter uses the following Buildkite provided environment variables. 
Please make sure that the following environment variables are available in your testing environment, particularly if you use Docker or some other type of containerization to run your tests.
| Environment Variable | Description|
| -------------------- | ----------- |
| `BUILDKITE_BUILD_ID` | The UUID of the Buildkite build. Test Splitter uses this UUID along with `BUILDKITE_STEP_ID` to uniquely identify the test plan. |
| `BUILDKITE_JOB_ID` | The UUID of the job in Buildkite build. |
| `BUILDKITE_ORGANIZATION_SLUG` | The slug of your Buildkite organization. |
| `BUILDKITE_PARALLEL_JOB` | The index number of a parallel job created from a Buildkite parallel build step. <br>Make sure you configure `parallelism` in your pipeline definition.  You can read more about Buildkite parallel build step on this [page](https://buildkite.com/docs/pipelines/controlling-concurrency#concurrency-and-parallelism).| 
| `BUILDKITE_PARALLEL_JOB_COUNT` | The total number of parallel jobs created from a Buildkite parallel build step. <br>Make sure you configure `parallelism` in your pipeline definition.  You can read more about Buildkite parallel build step on this [page](https://buildkite.com/docs/pipelines/controlling-concurrency#concurrency-and-parallelism). |
| `BUILDKITE_STEP_ID` | The UUID of the step group in Buildkite build. Test Splitter uses this UUID along with `BUILDKITE_BUILD_ID` to uniquely identify the test plan.

<br>
In addition to the above variables, you must set the following environment variables.

| Environment Variable | Description |
| -------------------- | ----------- |
| `BUILDKITE_SPLITTER_API_ACCESS_TOKEN ` | Buildkite API access token with `read_suites`, `read_test_plan`, and `write_test_plan` scopes. You can create an access token from [Personal Settings](https://buildkite.com/user/api-access-tokens) in Buildkite |
| `BUILDKITE_SPLITTER_SUITE_SLUG` | The slug of your Buildkite Test Analytics test suite. You can find the suite slug in the url for your suite. For example, the slug for the url: https://buildkite.com/organizations/my-organization/analytics/suites/my-suite is `my-suite` |


<br>
The following environment variables can be used optionally to configure your Test Splitter.

| Environment Variable | Default Value | Description |
| ---- | ---- | ----------- |
| `BUILDKITE_SPLITTER_DEBUG_ENABLED` | `false` | Flag to enable more verbose logging. |
| `BUILDKITE_SPLITTER_RETRY_CMD` | `BUILDKITE_SPLITTER_TEST_CMD` | The command to retry the failed tests. Test Splitter will fill in the `{{testExamples}}` placeholder with the failed tests. If not set, the Test Splitter will use the same command defined in `BUILDKITE_SPLITTER_TEST_CMD`. |
| `BUILDKITE_SPLITTER_RETRY_COUNT` | `0` | The number of retries. Test Splitter runs the test command defined in `BUILDKITE_SPLITTER_TEST_CMD` and retries only the failed tests up to `BUILDKITE_SPLITTER_RETRY_COUNT` times, using the retry command defined in `BUILDKITE_SPLITTER_RETRY_CMD`. |
| `BUILDKITE_SPLITTER_SPLIT_BY_EXAMPLE` | `false` | Flag to enable split by example. When this option is `true`, the Test Splitter will split the execution of slow test files over multiple partitions. |
| `BUILDKITE_SPLITTER_TEST_CMD` | `bundle exec rspec --format progress {{testExamples}}` | Test command to run your tests. Test Splitter will fill in the `{{testExamples}}` placeholder with the test splitting results. It is necessary to configure your Rspec `--format` when customizing the test command. |
| `BUILDKITE_SPLITTER_TEST_FILE_EXCLUDE_PATTERN` | - | Glob pattern to exclude certain test files or directories. The exclusion will be applied after discovering the test files using a pattern configured with `BUILDKITE_SPLITTER_TEST_FILE_PATTERN`. </br> *This option accepts the pattern syntax supported by the [zzglob](https://github.com/DrJosh9000/zzglob?tab=readme-ov-file#pattern-syntax) library.* |
| `BUILDKITE_SPLITTER_TEST_FILE_PATTERN` | `spec/**/*_spec.rb` | Glob pattern to discover test files. You can exclude certain test files or directories from the discovered test files using a pattern that can be configured with `BUILDKITE_SPLITTER_TEST_FILE_EXCLUDE_PATTERN`.</br> *This option accepts the pattern syntax supported by the [zzglob](https://github.com/DrJosh9000/zzglob?tab=readme-ov-file#pattern-syntax) library.* |
| `BUILDKITE_SPLITTER_TEST_RUNNER` | `rspec` | Test runner to use for running tests. Currently only `rspec` is supported.


### Run the Test Splitter
Please download the executable and make it available in your testing environment.
To parallelize your tests in your Buildkite build, you can amend your pipeline step configuration to:
```
steps:
  - name: "Rspec"
    command: ./test-splitter
    parallelism: 10
    env:
      BUILDKITE_SPLITTER_SUITE_SLUG: my-suite
      BUILDKITE_SPLITTER_API_ACCESS_TOKEN: your-secret-token
```

### Possible exit statuses

The test-splitter client may exit with a variety of exit statuses, outlined below:

- If there is a configuration error, the test-splitter client will exit with
  status 16.
- If the test-splitter runner (such as RSpec) exits cleanly, the exit status of
  the runner is returned. This will likely be 0 for successful test runs, 1 for
  failing test runs, but may be any other error status returned by the runner.
- If the test-splitter runner is terminated by an OS level signal, such as SIGSEGV or
  SIGABRT, the exit status returned will be equal to 128 plus the signal number.
  For example, if the runner raises a SIGSEGV, the exit status will be (128 +
  11) = 139.
