#!/usr/bin/env bash

set -euo pipefail

version=$(buildkite-agent meta-data get "version")
image_tag="packages.buildkite.com/buildkite/test-splitter-docker/test-splitter:${version}"

echo "--- :key: Login to Buildkite Packages"
buildkite-agent oidc request-token \
  --audience "https://packages.buildkite.com/buildkite/test-splitter-docker" \
  --lifetime 300 \
  | docker login packages.buildkite.com/buildkite/test-splitter-docker --username=buildkite --password-stdin

echo "--- :package: Downloading built binaries"
buildkite-agent artifact download "pkg/test-splitter-*" .

echo "--- :test_tube: Building and testing Docker image"
docker build -t "$image_tag" ./pkg
docker run --rm "$image_tag" --version

echo "--- :docker: Pushing Docker image"
docker push $image_tag
