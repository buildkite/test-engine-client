# Buildkite Test Splitter
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
| `BUILDKITE_API_ACCESS_TOKEN ` | - | Required, Buildkite API access token with `read_suites`, `read_test_plan`, and `write_test_plan` scopes. You can create access token from [Personal Settings](https://buildkite.com/user/api-access-tokens) in Buildkite |
| `BUILDKITE_ORGANIZATION_SLUG` | - | Required, the slug of your Buildkite organization. This is available in your pipeline environment, so you don't need to set it manually |
| `BUILDKITE_PARALLEL_JOB_COUNT` | - | Required, total number of parallelism. |
| `BUILDKITE_PARALLEL_JOB` | - | Required, test plan for specific node |
| `BUILDKITE_SPLITTER_IDENTIFIER` | `BUILDKITE_BUILD_ID/BUILDKITE_STEP_ID` | Optional. Test Splitter uses the identifier to store and fetch the test plan and must be unique for each build and steps group. By default it will use a composite of `BUILDKITE_BUILD_ID` and `BUILDKITE_STEP_ID`, but it can be overridden by specifying the `BUILDKITE_SPLITTER_IDENTIFIER`. `BUILDKITE_BUILD_ID` and `BUILDKITE_STEP_ID` must be accessible by the client when using the default. |
| `BUILDKITE_SPLITTER_TEST_FILE_PATTERN` | `spec/**/*_spec.rb` | Optional, glob pattern for discovering test files that need to be executed. </br> *It accepts pattern syntax supported by [zzglob](https://github.com/DrJosh9000/zzglob?tab=readme-ov-file#pattern-syntax) library*. |
| `BUILDKITE_SPLITTER_TEST_FILE_EXCLUDE_PATTERN` | - | Optional, glob pattern to use for excluding test files or directory. </br> *It accepts pattern syntax supported by [zzglob](https://github.com/DrJosh9000/zzglob?tab=readme-ov-file#pattern-syntax) library.* |
| `BUILDKITE_SPLITTER_SUITE_SLUG` | - | Required, the slug of your test suite. |
| `BUILDKITE_TEST_SPLITTER_CMD` | `bundle exec rspec {{testExamples}}` | Optional, test command for running your tests. Test splitter will fill in the `{{testExamples}}` placeholder with the test splitting results |

For most use cases, Test Splitter should work out of the box due to the default values available from your Buildkite environment.

However, you'll have to set `BUILDKITE_API_ACCESS_TOKEN` and `BUILDKITE_SPLITTER_SUITE_SLUG`.

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
