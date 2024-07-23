#!/usr/bin/env sh

tag=$(buildkite-agent meta-data get "release-version")

git tag "${tag}"

goreleaser release --clean
