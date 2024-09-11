# Buildkite Test Engine Client

Buildkite Test Engine Client (bktec) is an open source tool to orchestrate your test suites. It uses your Buildkite Test Engine suite data to intelligently partition and parallelise your tests.

bktec supports RSpec and Jest.

## Migrating to 0.9.0
Version 0.9.0 introduces a new feature that requires Test Splitter to read test results from the runner for retries and verification purposes. To enable this feature, it is necessary to configure the `BUILDKITE_SPLITTER_RESULT_PATH` environment variable. This variable specifies the location of where the runner should store test results.

Furthermore, we have updated the default test command for RSpec to `bundle exec rspec --format progress --format json --out {{resultPath}} {{testExamples}}`. Test Splitter will automatically replace `{{resultPath}}` with the value specified in `BUILDKITE_SPLITTER_RESULT_PATH`. If you want to customize the RSpec command, make sure to include `--format json --out {{resultPath}}` in the command. 

## Installation
The latest version of bktec can be downloaded from https://github.com/buildkite/test-splitter/releases

### Supported OS/Architecture
ARM and AMD architecture for linux and darwin

The available Go binaries
- test-splitter-darwin-amd64
- test-splitter-darwin-arm64
- test-splitter-linux-amd64
- test-splitter-linux-arm64

## Using bktec

### ENV variables
bktec uses the following Buildkite provided environment variables.
Please make sure that the following environment variables are available in your testing environment, particularly if you use Docker or some other type of containerization to run your tests.
| Environment Variable | Description|
| -------------------- | ----------- |
| `BUILDKITE_BUILD_ID` | The UUID of the Buildkite build. bktec uses this UUID along with `BUILDKITE_STEP_ID` to uniquely identify the test plan. |
| `BUILDKITE_JOB_ID` | The UUID of the job in Buildkite build. |
| `BUILDKITE_ORGANIZATION_SLUG` | The slug of your Buildkite organization. |
| `BUILDKITE_PARALLEL_JOB` | The index number of a parallel job created from a Buildkite parallel build step. <br>Make sure you configure `parallelism` in your pipeline definition.  You can read more about Buildkite parallel build step on this [page](https://buildkite.com/docs/pipelines/controlling-concurrency#concurrency-and-parallelism).|
| `BUILDKITE_PARALLEL_JOB_COUNT` | The total number of parallel jobs created from a Buildkite parallel build step. <br>Make sure you configure `parallelism` in your pipeline definition.  You can read more about Buildkite parallel build step on this [page](https://buildkite.com/docs/pipelines/controlling-concurrency#concurrency-and-parallelism). |
| `BUILDKITE_STEP_ID` | The UUID of the step group in Buildkite build. bktec uses this UUID along with `BUILDKITE_BUILD_ID` to uniquely identify the test plan.

<br>
In addition to the above variables, you must set the following environment variables.

| Environment Variable | Description |
| -------------------- | ----------- |
| `BUILDKITE_TEST_ENGINE_API_ACCESS_TOKEN ` | Buildkite API access token with `read_suites`, `read_test_plan`, and `write_test_plan` scopes. You can create an access token from [Personal Settings](https://buildkite.com/user/api-access-tokens) in Buildkite |
| `BUILDKITE_TEST_ENGINE_SUITE_SLUG` | The slug of your Buildkite Test Engine test suite. You can find the suite slug in the url for your suite. For example, the slug for the url: https://buildkite.com/organizations/my-organization/analytics/suites/my-suite is `my-suite` |
| `BUILDKITE_TEST_ENGINE_RESULT_PATH` | bktec uses this environment variable to tell the runner where to store the test result. Test Splitter reads the test result after each test run for retries and verification. For RSpec, the result is generated using the `--format json` and `--out` CLI options, while for Jest, it is generated using the `--json` and `--outputFile` options. We have included these options in the default test command for RSpec and Jest. If you need to customize your test command, make sure to append the CLI options to save the result to a file. Please refer to the `BUILDKITE_SPLITTER_TEST_CMD` environment variable for more details. <br> *Note: Test Splitter will not delete the file after running the test, however it will be deleted by Buildkite Agent as part of build lifecycle. *|

<br>
The following environment variables can be used optionally to configure bktec.

| Environment Variable | Default Value | Description |
| ---- | ---- | ----------- |
| `BUILDKITE_TEST_ENGINE_DEBUG_ENABLED` | `false` | Flag to enable more verbose logging. |
| `BUILDKITE_TEST_ENGINE_RETRY_CMD` | `BUILDKITE_TEST_ENGINE_TEST_CMD` | The command to retry the failed tests. bktec will fill in the `{{testExamples}}` placeholder with the failed tests. If not set, bktec will use the same command defined in `BUILDKITE_TEST_ENGINE_TEST_CMD`. |
| `BUILDKITE_TEST_ENGINE_RETRY_COUNT` | `0` | The number of retries. bktec runs the test command defined in `BUILDKITE_TEST_ENGINE_TEST_CMD` and retries only the failed tests up to `BUILDKITE_TEST_ENGINE_RETRY_COUNT` times, using the retry command defined in `BUILDKITE_TEST_ENGINE_RETRY_CMD`. |
| `BUILDKITE_TEST_ENGINE_SPLIT_BY_EXAMPLE` | `false` | Flag to enable split by example. When this option is `true`, bktec will split the execution of slow test files over multiple partitions. |
| `BUILDKITE_TEST_ENGINE_TEST_CMD` | `bundle exec rspec --format progress --format json --out {{resultPath}} {{testExamples}}` | Test command to run your tests. bktec will replace the `{{testExamples}}` placeholder with the test plan, and replace `{{resultPath}}` with the value set in `BUILDKITE_TEST_ENGINE_RESULT_PATH`. It is necessary to configure your Rspec with `--format json --out {{resultPath}}` when customizing the test command, because bktec needs to read the result after each test run. |
| `BUILDKITE_TEST_ENGINE_TEST_FILE_EXCLUDE_PATTERN` | - | Glob pattern to exclude certain test files or directories. The exclusion will be applied after discovering the test files using a pattern configured with `BUILDKITE_TEST_ENGINE_TEST_FILE_PATTERN`. </br> *This option accepts the pattern syntax supported by the [zzglob](https://github.com/DrJosh9000/zzglob?tab=readme-ov-file#pattern-syntax) library.* |
| `BUILDKITE_TEST_ENGINE_TEST_FILE_PATTERN` | `spec/**/*_spec.rb` | Glob pattern to discover test files. You can exclude certain test files or directories from the discovered test files using a pattern that can be configured with `BUILDKITE_TEST_ENGINE_TEST_FILE_EXCLUDE_PATTERN`.</br> *This option accepts the pattern syntax supported by the [zzglob](https://github.com/DrJosh9000/zzglob?tab=readme-ov-file#pattern-syntax) library.* |
| `BUILDKITE_TEST_ENGINE_TEST_RUNNER` | `rspec` | Test runner to use for running tests. Currently only `rspec` is supported.


### Running bktec
Please download the executable and make it available in your testing environment.
To parallelize your tests in your Buildkite build, you can amend your pipeline step configuration to:
```
steps:
  - name: "Rspec"
    command: ./bktec
    parallelism: 10
    env:
      BUILDKITE_TEST_ENGINE_SUITE_SLUG: my-suite
      BUILDKITE_TEST_ENGINE_API_ACCESS_TOKEN: your-secret-token
```

### Possible exit statuses

bktec may exit with a variety of exit statuses, outlined below:

- If there is a configuration error, bktec will exit with
  status 16.
- If the test runner (e.g. RSpec) exits cleanly, the exit status of
  the runner is returned. This will likely be 0 for successful test runs, 1 for
  failing test runs, but may be any other error status returned by the runner.
- If the test runner is terminated by an OS level signal, such as SIGSEGV or
  SIGABRT, the exit status returned will be equal to 128 plus the signal number.
  For example, if the runner raises a SIGSEGV, the exit status will be (128 +
  11) = 139.
