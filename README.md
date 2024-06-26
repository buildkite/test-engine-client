# Buildkite Test Splitter

Buildkite Test Splitter is an open source tool to orchestrate your test suites. It uses your Buildkite Test Analytic suite data to intelligently partition and parallelise your tests. 
Test Splitter currently only support RSpec but support for other frameworks is coming soon.

## Migrating from 0.4.0

0.5.0 introduces authentication mechanism changes. Previously, the test splitter used a suite token set in the `BUILDKITE_SPLITTER_SUITE_TOKEN` environment variable to authenticate.
From version 0.5.0 onwards, you will need to use a Buildkite API Access Token with the `read_suites`, `read_test_plan`, and `write_test_plan` scopes.
The API access token can be created from your [personal setting page](https://buildkite.com/user/api-access-tokens) in Buildkite, and needs to be configured for the test splitter using the `BUILDKITE_SPLITTER_API_ACCESS_TOKEN` environment variable.

Additionally, you will need to ensure that the organization and suite slugs are present in the environment. The organization slug is readily available in your Buildkite build environment as `BUILDKITE_ORGANIZATION_SLUG` so you do not have to set it manually, however, you will need to pass this variable to the docker container if you are using docker-compose plugin.
The suite slug needs to be manually configured using the `BUILDKITE_SPLITTER_SUITE_SLUG` environment variable. You can find the suite slug in the url for your suite, for example, the slug for the url: https://buildkite.com/organizations/my-organization/analytics/suites/my-suite?branch=main is `my-suite`. 

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
Test Splitter uses the following environment variables provided in your Buildkite build environment. 
Please make sure that the following environment variables are available in your testing environment, particularly if you use Docker or some other type of containerization to run your tests.
| Environment Variable | Description|
| -------------------- | ----------- |
| `BUILDKITE_BUILD_ID` | The UUID of the Buildkite build. Test Splitter uses this UUID along with `BUILDKITE_STEP_ID` to uniquely identify the test plan. |
| `BUILDKITE_JOB_ID` | The UUID of the job in Buildkite build. |
| `BUILDKITE_ORGANIZATION_SLUG` | The slug of your Buildkite organization. |
| `BUILDKITE_PARALLEL_JOB` | The index of parallel job created from a Buildkite parallel build step. Test Splitter uses this value to get specific test plan for each parallel job. <br>Make sure you configure `parallelism` for your Buildkite build step.  You can read more on Buildkite parallel build step on this [page](https://buildkite.com/docs/pipelines/controlling-concurrency#concurrency-and-parallelism).| 
| `BUILDKITE_PARALLEL_JOB_COUNT` | The total number of parallel jobs created from a Buildkite parallel build step. Test Splitter uses this value to split the tests and create the test plan. <br>Make sure you configure `parallelism` for your Buildkite build step.  You can read more on Buildkite parallel build step on this [page](https://buildkite.com/docs/pipelines/controlling-concurrency#concurrency-and-parallelism) |
| `BUILDKITE_STEP_ID` | The UUID of the step group in Buildkite build. Test Splitter uses this UUID along with `BUILDKITE_BUILD_ID` to uniquely identify the test plan.

<br>
In addition to above variables, you must set following environment variables.

| Environment Variable | Description |
| -------------------- | ----------- |
| `BUILDKITE_SPLITTER_API_ACCESS_TOKEN ` | Buildkite API access token with `read_suites`, `read_test_plan`, and `write_test_plan` scopes. You can create access token from [Personal Settings](https://buildkite.com/user/api-access-tokens) in Buildkite |
| `BUILDKITE_SPLITTER_SUITE_SLUG` | The slug of your Buildkite Test Analytics test suite. You can find the suite slug in the url for your suite, for example, the slug for the url: https://buildkite.com/organizations/my-organization/analytics/suites/my-suite is `my-suite` |


<br>
Following environment variables can be used optionally to configure your test splitter.

| Environment Variable | Default Value | Description |
| ---- | ---- | ----------- |
| `BUILDKITE_SPLITTER_DEBUG_ENABLED` | `false` | Flag to enable more verbose logging. |
| `BUILDKITE_SPLITTER_RETRY_COUNT` | `0` | The number of retry. Test splitter runs the test command defined in `BUILDKITE_SPLITTER_TEST_CMD`, and retries the failing tests maximum `BUILDKITE_SPLITTER_RETRY_COUNT` times. For Rspec, the test splitter runs `BUILDKITE_SPLITTER_TEST_CMD` with `--only-failures` as the retry command. |
| `BUILDKITE_SPLITTER_SPLIT_BY_EXAMPLE` | `false` | Flag to enable or disable split by example. When this option is `true`, the Test Splitter will split slow test files that take longer than 3 minutes into individual test cases, so that the execution of file will be over multiple nodes. |
| `BUILDKITE_SPLITTER_TEST_CMD` | `bundle exec rspec {{testExamples}}` | Test command for running your tests. Test splitter will fill in the `{{testExamples}}` placeholder with the test splitting results |
| `BUILDKITE_SPLITTER_TEST_FILE_EXCLUDE_PATTERN` | - | Glob pattern to use for excluding test files or directory. </br> *It accepts pattern syntax supported by [zzglob](https://github.com/DrJosh9000/zzglob?tab=readme-ov-file#pattern-syntax) library.* |
| `BUILDKITE_SPLITTER_TEST_FILE_PATTERN` | `spec/**/*_spec.rb` | Glob pattern for discovering test files that need to be executed. </br> *It accepts pattern syntax supported by [zzglob](https://github.com/DrJosh9000/zzglob?tab=readme-ov-file#pattern-syntax) library*. |


### Run the Test Splitter
Please download the executable and make it available in your testing environment.

Once that's available, you'll need to modify permissions to make the binary executable, and then execute it. The test splitter will run the rspec specs for you, so you'll run the test splitter in lieu of the relevant rspec command. 

Otherwise, your script for executing specs may look something like:
```
chmod +x test-splitter
./test-splitter # fetches the test plan for this node, and then executes the rspec tests
```

To parallelise your tests in your Buildkite build, you can amend your pipeline step configuration to:
```
steps:
  - name: "Rspec"
    command: ./test-splitter
    parallelism: 10
    env:
      BUILDKITE_SPLITTER_SUITE_SLUG: my-suite
      BUILDKITE_SPLITTER_API_ACCESS_TOKEN: your-secret-tokens
```

### Exit code
| Exit code | Description |
| ---- | ---- |
| 0 | Success (passed through from test runner) |
| 1 | Failure (passed through from test runner) |
| 16 | Test Splitter failure (eg. config error) |
| * | Other errors (passed through from the test runner) |
