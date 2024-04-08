#!/usr/bin/env bash

set -euo pipefail

echo -e "--- :wrench: Install Test Splitter"

# if the build is triggered by Test Splitter Client pipeline,
# download the test-splitter binary from the triggering build,
# otherwise install the test-splitter from the GitHub.

echo -e "Downloading test-splitter from the test-splitter-client build artifact"
buildkite-agent artifact download --build "${BUILDKITE_BUILD_ID}" pkg/test-splitter-linux-amd64 .
mv pkg/test-splitter-linux-amd64 test-splitter

chmod +x test-splitter

# If RSpec exits with a non-zero code (e.g. due to test failure)
# we don't want this bash script to peform an early exit as we wish to
# retry the specs again.
#
set +e

echo -e "+++ :rspec: Running specs"

./test-splitter

exit_code=$?
echo -e "--- :bangbang: RSpec exit code: $exit_code"
set -e

# Move the jsons file to a splitter directory so it doesn't conflicts with the jsons from the main rspec step (Knapsack).
#
# We are hardcoding the rspec command and options in the test splitter client, so we can't control where to store the output files,
# unless we change the test splitter.
mkdir -p tmp/splitter
mv tmp/rspec-*.json tmp/splitter/
