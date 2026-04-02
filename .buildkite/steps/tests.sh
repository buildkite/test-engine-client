#!/usr/bin/env bash
set -euo pipefail

go version
echo arch is "$(uname -m)"

go install gotest.tools/gotestsum@v1.8.0

if [[ "$(go env GOOS)" != "windows" ]]; then
  # Install pact-go (not supported on Windows)
  go install github.com/pact-foundation/pact-go/v2@latest
  pact-go -l DEBUG install

  # Install dependencies for js runner tests
  cd internal/runner/testdata
  yarn install
  cd playwright
  yarn playwright install
  cd ../../../..
fi

echo '+++ Running tests'

export BUILDKITE_TEST_ENGINE_SUITE_SLUG=bktec
export BUILDKITE_TEST_ENGINE_TEST_RUNNER=gotest
export BUILDKITE_TEST_ENGINE_RESULT_PATH="junit-${BUILDKITE_JOB_ID}.xml"
export BUILDKITE_TEST_ENGINE_RETRY_COUNT=1

if [[ "$(go env GOOS)" == "windows" ]]; then
  export BUILDKITE_TEST_ENGINE_TEST_CMD='gotestsum --junitfile={{resultPath}} -- -count=1 {{packages}}'
else
  export BUILDKITE_TEST_ENGINE_TEST_CMD='gotestsum --junitfile={{resultPath}} -- -count=1 -coverprofile=cover.out -failfast -race {{packages}}'
fi

bktec

if [[ "$(go env GOOS)" != "windows" ]]; then
  echo 'Producing coverage report'
  go tool cover -html cover.out -o cover.html
fi
