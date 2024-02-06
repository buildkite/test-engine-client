#!/usr/bin/env bash

set -euo pipefail

splitter_version="$(cat version/VERSION)"

cat <<YAML | buildkite-agent pipeline upload
steps:
  - name: ":rocket: Release"
    trigger: "test-splitter-client-release"
    build:
      message: "Release for ${splitter_version}, build ${BUILDKITE_BUILD_NUMBER}"
      commit: "${BUILDKITE_COMMIT}"
      branch: "${BUILDKITE_BRANCH}"
      env:
        ARTIFACTS_BUILD_ID: "${BUILDKITE_BUILD_ID}"
YAML