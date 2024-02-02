#!/bin/bash

agent_version=$(cat version/VERSION)

echo "--- :package: Downloading built binaries from build ${ARTIFACTS_BUILD_ID}"
buildkite-agent artifact download --build "$ARTIFACTS_BUILD_ID" "pkg/test-splitter-*" .

echo "--- :octocat: :rocket: Creating GitHub release for v$agent_version"
