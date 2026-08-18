#!/usr/bin/env sh
set -eu

# GoReleaser has already selected and named the uploadable artifacts. Keep this
# helper intentionally limited to making them available as Buildkite artifacts.
artifact=$1
artifact_name=$2
package_dir=dist/buildkite-packages

mkdir -p "${package_dir}"
cp "${artifact}" "${package_dir}/${artifact_name}"
