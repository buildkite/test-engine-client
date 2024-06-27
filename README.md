# Buildkite Test Splitter

## Migrating from 0.4.0

0.5.0 introduces authentication mechanism changes. Previously, the test splitter used a suite token set in the `BUILDKITE_SPLITTER_SUITE_TOKEN` environment variable to authenticate.
From version 0.5.0 onwards, you will need to use a Buildkite API Access Token with the `read_suites`, `read_test_plan`, and `write_test_plan` scopes.
The API access token can be created from your [personal setting page](https://buildkite.com/user/api-access-tokens) in Buildkite, and needs to be configured for the test splitter using the `BUILDKITE_SPLITTER_API_ACCESS_TOKEN` environment variable.

Additionally, you will need to ensure that the organization and suite slugs are present in the environment. The organization slug is readily available in your Pipeline environment as `BUILDKITE_ORGANIZATION_SLUG` so you do not have to set it manually, however, you will need to pass this variable to the docker container if you are using docker-compose plugin.
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

| Environment Variable | Default Value | Description |
| ---- | ---- | ----------- |
| `BUILDKITE_ORGANIZATION_SLUG` | - | Required, the slug of your Buildkite organization. This is available in your pipeline environment, so you don't need to set it manually |
| `BUILDKITE_PARALLEL_JOB_COUNT` | - | Required, total number of parallelism. |
| `BUILDKITE_PARALLEL_JOB` | - | Required, test plan for specific node |
| `BUILDKITE_SPLITTER_API_ACCESS_TOKEN ` | - | Required, Buildkite API access token with `read_suites`, `read_test_plan`, and `write_test_plan` scopes. You can create access token from [Personal Settings](https://buildkite.com/user/api-access-tokens) in Buildkite |
| `BUILDKITE_SPLITTER_RETRY_COUNT` | 0 | Optional. Test splitter runs the test command defined in `BUILDKITE_SPLITTER_TEST_CMD`, and retries the failing tests maximum `BUILDKITE_SPLITTER_RETRY_COUNT` times. For Rspec, the test splitter runs `BUILDKITE_SPLITTER_TEST_CMD` with `--only-failures` as the retry command. |
| `BUILDKITE_SPLITTER_SPLIT_BY_EXAMPLE` | `false` | Enable or disable split by example. When this option is `true`, the Test Splitter will split slow test files that take longer than 3 minutes into individual test cases, so that the execution of file will be over multiple nodes. |
| `BUILDKITE_SPLITTER_SUITE_SLUG` | - | Required, the slug of your test suite. |
| `BUILDKITE_SPLITTER_TEST_CMD` | `bundle exec rspec {{testExamples}}` | Optional, test command for running your tests. Test splitter will fill in the `{{testExamples}}` placeholder with the test splitting results |
| `BUILDKITE_SPLITTER_TEST_FILE_PATTERN` | `spec/**/*_spec.rb` | Optional, glob pattern for discovering test files that need to be executed. </br> *It accepts pattern syntax supported by [zzglob](https://github.com/DrJosh9000/zzglob?tab=readme-ov-file#pattern-syntax) library*. |
| `BUILDKITE_SPLITTER_TEST_FILE_EXCLUDE_PATTERN` | - | Optional, glob pattern to use for excluding test files or directory. </br> *It accepts pattern syntax supported by [zzglob](https://github.com/DrJosh9000/zzglob?tab=readme-ov-file#pattern-syntax) library.* |
| `BUILDKITE_SPLITTER_DEBUG_ENABLED` | `false` | Optional, flag to enable more verbose logging. |

For most use cases, Test Splitter should work out of the box due to the default values available from your Buildkite environment.

However, you'll have to set `BUILDKITE_SPLITTER_API_ACCESS_TOKEN` and `BUILDKITE_SPLITTER_SUITE_SLUG`.

You can also set the `BUILDKITE_SPLITTER_TEST_FILE_PATTERN` or `BUILDKITE_SPLITTER_TEST_FILE_EXCLUDE_PATTERN` if you need to filter the tests selected for execution.

### Run the Test Splitter
Please download the executable and make it available in your testing environment.

Once that's available, you'll need to modify permissions to make the binary executable, and then execute it. The test splitter will run the rspec specs for you, so you'll run the test splitter in lieu of the relevant rspec command. 

Otherwise, your script for executing specs may look something like:
```
chmod +x test-splitter
./test-splitter # fetches the test plan for this node, and then executes the rspec tests
```
### Exit code
| Exit code | Description |
| ---- | ---- |
| 0 | Success (passed through from test runner) |
| 1 | Failure (passed through from test runner) |
| 16 | Test Splitter failure (eg. config error) |
| * | Other errors (passed through from the test runner) |
