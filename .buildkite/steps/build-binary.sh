#!/usr/bin/env bash

set -euo pipefail

echo "--- :${1}: Building ${1}/${2}"

rm -rf pkg

export GOOS="$1"
export GOARCH="$2"

BUILD_NUMBER="${BUILDKITE_BUILD_NUMBER}"
NAME="test-splitter"
BUILD_PATH="pkg"
BINARY_FILENAME="${NAME}-${GOOS}-${GOARCH}"

if [[ "${GOOS}" = "dragonflybsd" ]]; then
  export GOOS="dragonfly"
fi

if [[ "${GOARCH}" = "armhf" ]]; then
  export GOARCH="arm"
  export GOARM="7"
fi

echo -e "Building ${NAME} with:\n"

echo "GOOS=${GOOS}"
echo "GOARCH=${GOARCH}"
if [[ -n "${GOARM:-}" ]]; then
  echo "GOARM=${GOARM}"
fi
echo "BUILD_NUMBER=${BUILD_NUMBER}"
echo ""

# Add .exe for Windows builds
if [[ "${GOOS}" == "windows" ]]; then
  BINARY_FILENAME="${BINARY_FILENAME}.exe"
fi

# Disable CGO completely
export CGO_ENABLED=0

mkdir -p "${BUILD_PATH}"
go build -v -o "${BUILD_PATH}/${BINARY_FILENAME}" .

chmod +x "${BUILD_PATH}/${BINARY_FILENAME}"

echo -e "\nDone: \033[33m${BUILD_PATH}/${BINARY_FILENAME}\033[0m 💪"
