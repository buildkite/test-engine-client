#!/usr/bin/env sh

set -euo pipefail

splitter_version="$(buildkite-agent meta-data get "version")" 

echo "--- :package: Downloading built binaries"
buildkite-agent artifact download "pkg/test-splitter-*" .

echo "--- :octocat: :rocket: Creating GitHub release for v${splitter_version}"
gh release create "v${splitter_version}" ./pkg/* --generate-notes
