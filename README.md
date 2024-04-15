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

### Required ENV variables
- BUILDKITE_BUILD_ID
- BUILDKITE_SUITE_TOKEN

`BUILDKITE_SUITE_TOKEN` can be found in the Test Analytics suite setting page under API token section https://buildkite.com/organizations/buildkite/analytics/suites/{#SUITE_SLUG}/edit

- BUILDKITE_PARALLEL_JOB_COUNT
- BUILDKITE_PARALLEL_JOB

`BUILDKITE_PARALLEL_JOB_COUNT` and `BUILDKITE_PARALLEL_JOB` will be accessable when build step is configured with `parallelism`.
### Run the Test Splitter
Assuming the Go binary is downloaded and renamed to `test-splitter`

We need to make it executable 

`chmod +x test-splitter`

Run the test splitter

`./test-splitter`

## Release
To release a new version, open a PR and update the version number in `version/VERSION`. 
Once the PR is merged to `main`, a [release pipeline](https://buildkite.com/buildkite/test-splitter-client-release) will be triggered and the new version will be released.
Currently, we only release to [GitHub](https://github.com/buildkite/test-splitter/releases). The release pipeline will generate the release notes and attach the compiled binary to the GitHub release.

