# Using bktec with Go

To integrate bktec with Go tests, set the `BUILDKITE_TEST_ENGINE_TEST_RUNNER` environment variable to `go`. Then, specify the `BUILDKITE_TEST_ENGINE_RESULT_PATH` to define where the JSON result should be stored. bktec will instruct the Go test runner to output the JSON result to this path, which is necessary for bktec to read the test results for retries and verification purposes.
