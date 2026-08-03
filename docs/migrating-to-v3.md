# Migrating from bktec v2 to v3

bktec v3 uses selector-based test splitting by default for every supported runner. Most suites can upgrade by replacing the bktec binary, but check the requirements below before upgrading.

## What changed

In v2, bktec sent file-based test plan requests by default. In v3, it sends runner-specific selectors instead:

- RSpec, Jest, Vitest, Cypress, Playwright, pytest, and Cucumber continue to use discovered file paths as selectors.
- Go uses package import paths discovered by `go list`.
- The custom runner uses values from `--selector-file` when provided, or falls back to its configured file patterns.

The deprecated `--selector-splitting` flag and `BUILDKITE_TEST_ENGINE_SELECTOR_SPLITTING` environment variable are still accepted so existing pipeline configuration does not fail, but their values no longer affect behavior. You can remove them after upgrading.

If you need bktec to continue sending file-based test plan requests, remain on v2.

## Before upgrading

### Check result attribution

Test Engine balances selectors using historical executions attributed with the `test.selector.primary` tag. bktec's built-in result upload attributes this tag automatically.

If you upload results with a Buildkite test collector, update to a version that attributes selectors directly:

- Ruby: `buildkite-test_collector` v2.14.0 or later
- JavaScript: `buildkite-test-collector` v1.10.0 or later
- Python: `buildkite-test-collector` v1.6.0 or later

Older collector versions continue to work because Test Engine falls back to matching the execution's file path. Updating is most important when you use a location prefix, because prefix handling in the fallback is best effort.

When no selector history can be matched, Test Engine uses default duration estimates instead of legacy file timings and bktec prints a warning. Run the suite once to record selector timings. If you expected existing history to match, check the collector version and location-prefix configuration.

### Update Go result output

Go suites must produce Go JSONL output so bktec can attribute package import paths. Use one of the following test command forms:

```sh
export BUILDKITE_TEST_ENGINE_TEST_CMD="gotestsum --jsonfile={{resultPath}} {{packages}}"
```

```sh
export BUILDKITE_TEST_ENGINE_TEST_CMD="go test -json {{packages}}"
```

See the [Go runner guide](./gotest.md#use-go-jsonl-output) for result-path configuration and more examples.

### Check custom runner selectors

The custom runner needs extra configuration when its runnable selectors are not file paths. Provide a newline-delimited selector file using `--selector-file` or `BUILDKITE_TEST_ENGINE_SELECTOR_FILE`, and attribute `test.selector.primary` in uploaded executions so Test Engine can match their history.

If the custom runner's selectors are file paths, its existing file-pattern configuration continues to work through the file-path fallback.

See the [custom runner guide](./custom-test-runner.md#selector-based-test-splitting) for configuration examples.

### Update Go module references

If you build bktec from source or reference its Go module, update the module path from `github.com/buildkite/test-engine-client/v2` to `github.com/buildkite/test-engine-client/v3`.

## Verify the upgrade

After upgrading, run the suite and check the Buildkite job log:

1. Confirm bktec creates a plan and runs the expected tests, packages, or selectors on each parallel job.
2. Look for the warning that no selector history was found. A warning on the first run is expected when no attributed history exists; later runs should use the timings recorded by that run.
3. Compare parallel job durations after timing data is available. Go suites may have a noticeably different split because v3 balances package durations instead of distributing packages evenly by count.

For more detail about selector discovery and matching, see [Selector-based test splitting](../README.md#selector-based-test-splitting).
