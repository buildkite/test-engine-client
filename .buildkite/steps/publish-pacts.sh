#!/usr/bin/env sh

set -euo pipefail

echo -e "--- :download: Fetching pacts"
buildkite-agent artifact download internal/api/pacts/* .

export PACT_BROKER_BASE_URL=https://buildkite.pactflow.io

echo -e "--- :upload: Uploading pacts"
pact \
  publish \
  internal/api/pacts \
  --branch ${BUILDKITE_BRANCH} \
  --consumer-app-version ${BUILDKITE_COMMIT} \
  --tag-with-git-branch
