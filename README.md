# Buildkite Test Splitter


## Release
To release a new version, open a PR and update the version number in `version/VERSION`. 
Once the PR is merged to `main`, a [release pipeline](https://buildkite.com/buildkite/test-splitter-client-release) will be triggered and the new version will be released.
Currently, we only release to [GitHub](https://github.com/buildkite/test-splitter/releases). The release pipeline will generate the release notes and attach the compiled binary to the GitHub release.

