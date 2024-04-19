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
|  `BUILDKITE_PARALLEL_JOB_COUNT` | - | Required, total number of parallelism |
|  `BUILDKITE_PARALLEL_JOB` | - | Required, test plan for specific node |
| `BUILDKITE_SPLITTER_SUITE_TOKEN` | `BUILDKITE_ANALYTICS_TOKEN` | Required, unique token for Test Suite that is being parallelised |
|  `BUILDKITE_SPLITTER_IDENTIFIER` | `BUILDKITE_BUILD_ID` | Required, use default unless you have a specific use case |
| `BUILDKITE_SPLITTER_TEST_FILE_PATTERN` | `spec/**/*_spec.rb` | Optional, glob pattern for discovering test files that need to be executed. </br> *It accepts pattern syntax supported by [zzglob](https://github.com/DrJosh9000/zzglob?tab=readme-ov-file#pattern-syntax) library*. |
| `BUILDKITE_SPLITTER_TEST_FILE_EXCLUDE_PATTERN` | - | Optional, glob pattern to use for excluding test files or directory. </br> *It accepts pattern syntax supported by [zzglob](https://github.com/DrJosh9000/zzglob?tab=readme-ov-file#pattern-syntax) library.* |

For most use cases, Test Splitter should work out of the box due to the default values available from your Buildkite environment.

However, you'll need to set `BUILDKITE_SPLITTER_SUITE_TOKEN` if your test collector doesn't use the `BUILDKITE_ANALYTICS_TOKEN` value, or your pipeline has multiple suites.

You can also set the `BUILDKITE_SPLITTER_TEST_FILE_PATTERN` or `BUILDKITE_SPLITTER_TEST_FILE_EXCLUDE_PATTERN` if you need to filter the tests selected for execution.

### Run the Test Splitter
Please download the executable and make it available in your testing environment.

Once that's available, you'll need to modify permissions to make the binary executable, and then execute it. The test splitter will run the rspec specs for you, so you'll run the test splitter in lieu of the relevant rspec command. Under the hood, the test splitter will execute `bin/rspec YOUR_TEST_PLAN` so if your rspec installation is different or if you are using any custom flags, this may not work for your test set up. Please get in touch with one of the Test Splitting team and we'll see what we can do!

Otherwise, your script for executing specs may look something like:
```
chmod +x test-splitter
./test-splitter # fetches the test plan for this node, and then executes the rspec tests
```


