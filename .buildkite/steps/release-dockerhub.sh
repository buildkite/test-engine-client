#!/usr/bin/env bash

set -euo pipefail

version=$(buildkite-agent meta-data get "version")
image_tag="buildkite/test-splitter:${version}"

echo "--- :key: Login to Dockerhub"
echo ${DOCKERHUB_PASSWORD} | docker login --username=${DOCKERHUB_USER} --password-stdin

echo "--- :package: Downloading built binaries"
buildkite-agent artifact download "pkg/test-splitter-*" .

echo "--- :docker: Pushing Docker image"
docker buildx create --use
docker buildx build -t $image_tag --platform linux/amd64,linux/arm64 ./pkg
