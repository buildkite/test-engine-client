#!/usr/bin/env bash

set -euo pipefail

version="$(cat version/VERSION)"

echo "--- :label: Checking git tag"

# If the tag is already exists, we assume the release is for nightly build,
# otherwise we assume it's a stable release
if git ls-remote --tags origin | grep "refs/tags/v${version}" ; then
  echo "v${version} already exists, running nightly release"
  buildkite-agent meta-data set "release-phase" "nightly"
  buildkite-agent meta-data set "version" "$version-$(git rev-parse --short HEAD)"
  buildkite-agent pipeline upload .buildkite/pipeline.release.nightly.yml
else
  echo "v${version} does not exist, running stable release"
  buildkite-agent meta-data set "release-phase" "stable"
  buildkite-agent meta-data set "version" "$version"
  buildkite-agent pipeline upload .buildkite/pipeline.release.stable.yml
fi
