# Buildkite Test Splitter


## Release
To release a new version, opan a PR and update the version number in `version/VERSION`. 
Once the PR is merged to `main`, a release pipeline will be triggered and the new version will be released.
Currently, we only release to [GitHub](https://github.com/buildkite/test-splitter/releases). The release pipeline will generate the release notes and attach the compiled binary to the GitHub release.

We use [Semantic Version](https://semver.org) for the versioning.
The basic is:
```
x.y.z
```
1. x is MAJOR version, used for BREAKING CHANGES for example when the API is changed.
2. y is MINOR version, used for non breaking changes. This should be the default when releasing new features.
3. z is PATCH version, used for bug fixes for the MINOR version.

