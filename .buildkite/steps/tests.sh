#!/usr/bin/env bash
set -euo pipefail

go version
echo arch is "$(uname -m)"

go install gotest.tools/gotestsum@v1.8.0

# Install dependencies for js runner tests
cd internal/runner/testdata
yarn install
cd playwright
yarn playwright install
cd ../../../..

# On feature branches, build bktec from the current source so the pipeline
# splits its own tests using the code under test. This way a change that breaks
# bktec's test splitting fails the pipeline before it can be merged to main.
# On main we use the released binary installed in the image (see Dockerfile).
bktec_bin="bktec"
if [[ "${BUILDKITE_BRANCH:-}" != "main" ]]; then
  echo '+++ Building bktec from source'
  go build \
    -ldflags "-X 'github.com/buildkite/test-engine-client/v2/internal/version.Version=${BUILDKITE_COMMIT:-dev}'" \
    -o /tmp/bktec .
  bktec_bin="/tmp/bktec"
fi

echo '+++ Running tests'

export BUILDKITE_TEST_ENGINE_SUITE_SLUG=bktec
export BUILDKITE_TEST_ENGINE_TEST_RUNNER=gotest
export BUILDKITE_TEST_ENGINE_RESULT_PATH="junit-${BUILDKITE_JOB_ID}.xml"
export BUILDKITE_TEST_ENGINE_RETRY_COUNT=1
export BUILDKITE_TEST_ENGINE_TEST_CMD='gotestsum --junitfile={{resultPath}} -- -count=1 -coverprofile=cover.out -failfast -race {{packages}}'

"${bktec_bin}"

echo 'Producing coverage report'
go tool cover -html cover.out -o cover.html
