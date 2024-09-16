#!/usr/bin/env sh

file=${1}
extension="${file##*.}"
registry="test-engine-client-${extension}"
audience="https://packages.buildkite.com/buildkite/${registry}"

echo "--- :key: :buildkite: Fetching OIDC token for ${audience}"
token=$(buildkite-agent oidc request-token --audience "${audience}" --lifetime 180)

echo "--- :linux: Uploading ${file}"
curl -s -X POST "https://api.buildkite.com/v2/packages/organizations/buildkite/registries/${registry}/packages" \
      -H "Authorization: Bearer ${token}" \
      -F "file=@${file}" \
    --fail-with-body
