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
Please ensure that these default Buildkite ENV variables are available in the environment you are running your tests in. We will detect these env vars automatically, and use them to orchestrate the test splitting
- `BUILDKITE_BUILD_ID`
- `BUILDKITE_PARALLEL_JOB_COUNT`
- `BUILDKITE_PARALLEL_JOB`

We also need the API token for the Test Suite that has the test data for the build. This will be available in the settings page for the Test Suite. We are expecting this key to be available as `BUILDKITE_SUITE_TOKEN`.

Additionally, you can configure the Test Splitter using the following optional environment variables:
| Environment Variable | Description |
| ---- | ----------- |
| `BUILDKITE_SPLITTER_TEST_FILE_PATTERN` | Glob pattern for discovering test files that need to be executed. The default value for Rspec is `spec/**/*_spec.rb`. </br> *It accepts pattern syntax supported by [zzglob](https://github.com/DrJosh9000/zzglob?tab=readme-ov-file#pattern-syntax) library*. |
| `BUILDKITE_SPLITTER_TEST_FILE_EXCLUDE_PATTERN` | Glob pattern to use for excluding test files or directory. </br> *It accepts pattern syntax supported by [zzglob](https://github.com/DrJosh9000/zzglob?tab=readme-ov-file#pattern-syntax) library.* |


### Run the Test Splitter
Please download the executable and make it available in your testing environment. 

Once that's available, you'll need to modify permissions to make the binary executable, and then execute it. The test splitter will run the rspec specs for you, so you'll run the test splitter in lieu of the relevant rspec command. Under the hood, the test splitter will execute `bin/rspec YOUR_TEST_PLAN` so if your rspec installation is different or if you are using any custom flags, this may not work for your test set up. Please get in touch with one of the Test Splitting team and we'll see what we can do! 

Otherwise, your script for executing specs may look something like:
```  
chmod +x test-splitter
./test-splitter # fetches the test plan for this node, and then executes the rspec tests
```


