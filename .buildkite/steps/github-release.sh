#!/usr/bin/env sh

set -euo pipefail

splitter_version="$(cat version/VERSION)" 

# Skip release if version already exists
if git ls-remote --tags origin | grep "refs/tags/v${splitter_version}" ; then
  echo "Version ${splitter_version} already exists"
  exit 0
fi

echo "--- :package: Downloading built binaries from build ${ARTIFACTS_BUILD_ID}"
buildkite-agent artifact download --build "${ARTIFACTS_BUILD_ID}" "pkg/test-splitter-*" .

echo "--- :octocat: :rocket: Creating GitHub release for v${splitter_version}"
gh release create "v${splitter_version}" ./pkg/* --generate-notes
