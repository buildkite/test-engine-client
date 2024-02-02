#!/bin/bash

agent_version=$(cat version/VERSION)

# Skip release if version already exists
if git ls-remote --tags origin | grep "refs/tags/v${agent_version}" ; then
  echo "Version ${agent_version} already exists"
  exit 0
fi

echo "--- :package: Downloading built binaries from build ${ARTIFACTS_BUILD_ID}"
buildkite-agent artifact download --build "$ARTIFACTS_BUILD_ID" "pkg/test-splitter-*" .

echo "--- :octocat: :rocket: Creating GitHub release for v$agent_version"
gh release create "v$agent_version" ./pkg/* --generate-notes
