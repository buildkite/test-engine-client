#!/usr/bin/env bash

set -euo pipefail

echo -e "--- :wrench: Install Test Splitter"

# Download go binary from artifact

echo -e "Downloading test-splitter from the test-splitter-client build artifact"
buildkite-agent artifact download pkg/test-splitter-linux-amd64 .
mv pkg/test-splitter-linux-amd64 test-splitter

chmod +x test-splitter

# If RSpec exits with a non-zero code (e.g. due to test failure)
# we don't want this bash script to peform an early exit as we wish to
# retry the specs again.
#
set +e

echo -e "+++ :rspec: Running specs"

# Update buildkite build id when we run e2e tests for server return 
# error plan scenario

if [[ "${ERROR_PLAN:-}" == "true" ]] ; then
  BUILDKITE_BUILD_ID=error_plan_build_id
fi

./test-splitter

exit_code=$?
echo -e "--- :bangbang: RSpec exit code: $exit_code"
set -e

# Retry failures up to 3 times
retry_count=1
max_retries=3

while [ $exit_code -ne 0 ] && [ $retry_count -le $max_retries ]
do
  echo -e "+++ :recycle: Attempt $retry_count of $max_retries to retry failing tests"

  # Retry failures by calling RSpec directly
  set +e
  bin/rspec --options .rspec.ci --seed ${BUILDKITE_SEED:-${BUILDKITE_BUILD_NUMBER:-$RANDOM}} --only-failures
  exit_code=$?
  set -e

  if [ $exit_code -ne 0 ] && [ $retry_count -eq $max_retries ]; then
    echo -e "--- :red_circle: Tests failed"
    exit $exit_code
  fi

  ((retry_count++))
done