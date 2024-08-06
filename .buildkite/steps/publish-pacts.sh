#!bash -eux

publish internal/api/pacts --consumer-app-version $BUILDKITE_BUILD_NUMBER
