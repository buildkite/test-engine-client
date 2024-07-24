#!/usr/bin/env sh

tag=$(buildkite-agent meta-data get "release-version")

git tag "${tag}"

echo "--- :key: Login to Buildkite Packages"
 buildkite-agent oidc request-token \
   --audience "https://packages.buildkite.com/buildkite/test-splitter-docker" \
   --lifetime 300 \
   | docker login packages.buildkite.com/buildkite/test-splitter-docker --username=buildkite --password-stdin

eco "--- :github: Creating Release"
goreleaser release --clean
