#!/usr/bin/env bash
set -euo pipefail

checkout=$(mktemp -d)
trap 'rm -rf "$checkout"' EXIT

echo '+++ Checking out buildkite-rspec from bktest main'
git clone --depth 1 --branch main https://github.com/buildkite/bktest.git "$checkout/bktest"
runner_root="$checkout/bktest/test-collector-ruby"
bundle install --gemfile "$runner_root/Gemfile"

echo '+++ Building bktec and discovering integration packages'
go build \
  -ldflags "-X 'github.com/buildkite/test-engine-client/v3/internal/version.Version=${BUILDKITE_COMMIT:-dev}'" \
  -o /tmp/bktec .
go list -tags=integration -f '{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}{{end}}' \
  ./internal/integration/... > "$checkout/selectors.txt"

echo '+++ Running and uploading integration tests'
export BKTEC_RSPEC_RUNNER_ROOT="$runner_root"
export BUILDKITE_TEST_ENGINE_SUITE_SLUG=bktec
export BUILDKITE_TEST_ENGINE_TEST_RUNNER=gotest
export BUILDKITE_TEST_ENGINE_RESULT_PATH="gotest-integration-${BUILDKITE_JOB_ID}.jsonl"
export BUILDKITE_TEST_ENGINE_TEST_CMD='go test -json -count=1 -race -tags=integration {{packages}}'
export BUILDKITE_TEST_ENGINE_SELECTOR_FILE="$checkout/selectors.txt"
export BUILDKITE_TEST_ENGINE_UPLOAD_RESULTS=true
export BUILDKITE_TEST_ENGINE_TAGS=test.type=integration

/tmp/bktec run
