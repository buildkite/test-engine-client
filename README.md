# Buildkite Test Splitter

Buildkite Test Splitter is an open source tool to orchestrate your test suites. It uses your Buildkite Test Analytic suite data to intelligently partition and parallelise your tests. 
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
In addition to above variables, you must set following environment variables.

| Environment Variable | Description |
| -------------------- | ----------- |
| `BUILDKITE_SPLITTER_API_ACCESS_TOKEN ` | Buildkite API access token with `read_suites`, `read_test_plan`, and `write_test_plan` scopes. You can create access token from [Personal Settings](https://buildkite.com/user/api-access-tokens) in Buildkite |
| `BUILDKITE_SPLITTER_SUITE_SLUG` | The slug of your Buildkite Test Analytics test suite. You can find the suite slug in the url for your suite. For example, the slug for the url: https://buildkite.com/organizations/my-organization/analytics/suites/my-suite is `my-suite` |


<br>
The following environment variables can be used optionally to configure your Test Splitter.

| Environment Variable | Default Value | Description |
| ---- | ---- | ----------- |
| `BUILDKITE_SPLITTER_DEBUG_ENABLED` | `false` | Flag to enable more verbose logging. |
| `BUILDKITE_SPLITTER_RETRY_COUNT` | `0` | The number of retries permitted. Test splitter runs the test command defined in `BUILDKITE_SPLITTER_TEST_CMD`, and retries only the failing tests for a maximum of `BUILDKITE_SPLITTER_RETRY_COUNT` times. For Rspec, the Test Splitter runs `BUILDKITE_SPLITTER_TEST_CMD` with `--only-failures` as the retry command. |
| `BUILDKITE_SPLITTER_SPLIT_BY_EXAMPLE` | `false` | Flag to enable split by example. When this option is `true`, the Test Splitter will split the execution of slow test files over multiple partitions. |
| `BUILDKITE_SPLITTER_TEST_CMD` | `bundle exec rspec {{testExamples}}` | Test command for running your tests. Test splitter will fill in the `{{testExamples}}` placeholder with the test splitting results |
| `BUILDKITE_SPLITTER_TEST_FILE_EXCLUDE_PATTERN` | - | Glob pattern to use for excluding test files or directory. </br> *It accepts pattern syntax supported by [zzglob](https://github.com/DrJosh9000/zzglob?tab=readme-ov-file#pattern-syntax) library.* |
| `BUILDKITE_SPLITTER_TEST_FILE_PATTERN` | `spec/**/*_spec.rb` | Glob pattern for discovering test files that need to be executed. </br> *It accepts pattern syntax supported by [zzglob](https://github.com/DrJosh9000/zzglob?tab=readme-ov-file#pattern-syntax) library*. |


### Run the Test Splitter
Please download the executable and make it available in your testing environment.
To parallelise your tests in your Buildkite build, you can amend your pipeline step configuration to:
```
steps:
  - name: "Rspec"
    command: ./test-splitter
    parallelism: 10
    env:
      BUILDKITE_SPLITTER_SUITE_SLUG: my-suite
      BUILDKITE_SPLITTER_API_ACCESS_TOKEN: your-secret-token
``` 

### Exit code
| Exit code | Description |
| ---- | ---- |
| 0 | Success (passed through from test runner) |
| 1 | Failure (passed through from test runner) |
| 16 | Test Splitter failure (eg. config error) |
| * | Other errors (passed through from the test runner) |
