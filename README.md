# Buildkite Test Splitter
## Installation
The latest version of Buildkite Test Splitter can be downloaded from https://api.github.com/repos/buildkite/test-splitter/releases/latest

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
- BUILDKITE_BUILD_ID
- BUILDKITE_PARALLEL_JOB_COUNT
- BUILDKITE_PARALLEL_JOB

Additionally, we need the API token for the Test Suite that has the test data for the build. This will be available in the settings page for the Test Suite. We are expecting this key to be available as BUILDKITE_SUITE_TOKEN.
### Run the Test Splitter
Please download the executable and make it available in your testing environment. 

Once that's available, you'll need to modify permissions to make the binary executable, and then execute it. The test splitter will run the rspec specs for you, so you'll run the test splitter in lieu of the relevant rspec command. Under the hood, the test splitter will execute `bin/rspec YOUR_TEST_PLAN` so if your rspec installation is different or if you are using any custom flags, this may not work for your test set up. Please get in touch with one of the Test Splitting team and we'll see what we can do! 

Otherwise, your script for executing specs may look something like:
```  
chmod +x test-splitter
./test-splitter # fetches the test plan for this node, and then executes the rspec tests
```

## Release
To release a new version, open a PR and update the version number in `version/VERSION`. 
Once the PR is merged to `main`, a [release pipeline](https://buildkite.com/buildkite/test-splitter-client-release) will be triggered and the new version will be released.
Currently, we only release to [GitHub](https://github.com/buildkite/test-splitter/releases). The release pipeline will generate the release notes and attach the compiled binary to the GitHub release.

