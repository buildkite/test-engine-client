#!/usr/bin/env sh

tag=$(buildkite-agent meta-data get "release-version")

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
   --audience "https://packages.buildkite.com/buildkite/test-splitter-docker" \
   --lifetime 300 \
   | docker login packages.buildkite.com/buildkite/test-splitter-docker --username=buildkite --password-stdin

echo "--- :key: :docker: Login to Docker"
echo "${DOCKERHUB_PASSWORD}" | docker login --username "${DOCKERHUB_USER}" --password-stdin

echo "+++ :rocket: Creating Release"
goreleaser release --clean
