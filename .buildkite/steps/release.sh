#!/usr/bin/env sh
set -euo pipefail

override_option_value="__override__"
version_number='(0|[1-9][0-9]*)'
prerelease_identifier='(0|[1-9][0-9]*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*)'
version_pattern="^v${version_number}\\.${version_number}\\.${version_number}(-${prerelease_identifier}(\\.${prerelease_identifier})*)?$"

selected_tag=$(buildkite-agent meta-data get "release-version")
override_tag=$(buildkite-agent meta-data get "release-version-override" 2>/dev/null || true)
override_tag=$(printf '%s' "${override_tag}" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')

if [[ "${selected_tag}" == "${override_option_value}" ]]; then
  if [[ -z "${override_tag}" ]]; then
    echo "Release version override selected, but no override was provided"
    exit 1
  fi
  tag="${override_tag}"
  echo "Using release version override: ${tag}"
else
  tag="${selected_tag}"
fi

if [[ "$(printf '%s' "${tag}" | tr -d '\n\r')" != "${tag}" ]]; then
  echo "Invalid release version: ${tag}"
  exit 1
fi

if ! printf '%s\n' "${tag}" | grep -Eq "${version_pattern}"; then
  echo "Invalid release version: ${tag}"
  exit 1
fi

if git rev-parse -q --verify "refs/tags/${tag}" >/dev/null; then
  echo "Release version already exists: ${tag}"
  exit 1
fi

git tag "${tag}"

# When releasing a stable version, we want the changelog
# in GitHub to be based on the previous stable version.
#
# For illustrations, consider the following tags:
# - v1.0.0
# - v1.0.1-rc.1
# - v1.0.1-rc.2
#
# When releasing v1.0.1, we want the changelog to be based on v1.0.0, not v1.0.1-rc.2.
# Hoever, when releasing v1.0.1-rc.3, we want the changelog to be based on v1.0.1-rc.2.
#
# To do this, we need to ignore all '-rc' tags when releasing a stable version.
if [[ ! "${tag}" =~ "-rc" ]]; then
  export GORELEASER_IGNORE_TAG="*-rc.*"
fi

echo "--- :key: :buildkite: Login to Buildkite Packages"
 buildkite-agent oidc request-token \
   --audience "https://packages.buildkite.com/buildkite/test-engine-client-docker" \
   --lifetime 300 \
   | docker login packages.buildkite.com/buildkite/test-engine-client-docker --username buildkite --password-stdin

echo "--- :key: :docker: Login to Docker"
echo "${DOCKERHUB_PASSWORD}" | docker login --username "${DOCKERHUB_USER}" --password-stdin

echo "--- :key: :aws: Login to AWS ECR Public"
(
  # The credentials is prefixed to avoid clashing with other AWS credentials
  export AWS_ACCESS_KEY_ID="${ECR_AWS_ACCESS_KEY_ID}"
  export AWS_SECRET_ACCESS_KEY="${ECR_AWS_SECRET_ACCESS_KEY}"
  export AWS_SESSION_TOKEN="${ECR_AWS_SESSION_TOKEN}"
  aws ecr-public get-login-password --region us-east-1 | docker login public.ecr.aws/buildkite/test-engine-client --username AWS --password-stdin 
)

echo "+++ :rocket: Creating Release"
goreleaser release --clean
