#!/bin/bash
version=$(cat version/VERSION)

cat <<YAML | buildkite-agent pipeline upload
  - name: ":rocket: Release"
    trigger: "test-splitter-client-release"
    build:
      message: "Release for ${version}, build ${BUILDKITE_BUILD_NUMBER}"
      commit: "${BUILDKITE_COMMIT}"
      branch: "${BUILDKITE_BRANCH}"
      env:
        ARTIFACTS_BUILD_ID: "${BUILDKITE_BUILD_ID}"
YAML