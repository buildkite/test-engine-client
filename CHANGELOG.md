# Changelog

## 0.7.2 - 2024-07-03
- Fix log statement newline issue.

## 0.7.1 - 2024-07-02
- Fix issue where `--version` would fail if no environment configured.
- Prefix log statements with 'Buildkite Test Splitter'.

## 0.7.0 - 2024-06-27
- Remove the ability to override the test plan identifier via `BUILDKITE_SPLITTER_IDENTIFIER`.
- Add support for orchestration page in Buildkite, by sending metadata after tests execution.

## 0.6.2 - 2024-06-24
- Fix issue where the client version is not set in the user agent.

## 0.6.1 - 2024-06-21
- Ignore request body when it is empty or when the request is a GET request.

## 0.6.0 - 2024-06-21

- ⚠️ **BREAKING** Remove support for the undocumented `--files` flag.
- ⚠️ **BREAKING** Rename the `BUILDKITE_API_ACCESS_TOKEN` environment variable to `BUILDKITE_SPLITTER_API_ACCESS_TOKEN`.
- Add support for split-by-example using the `BUILDKITE_SPLITTER_SPLIT_BY_EXAMPLE` environment variable.
- Add support for more verbose debug logging using the `BUILDKITE_SPLITTER_DEBUG_ENABLED` environment variable.

## 0.5.1
- Add a new line to each error log.

## 0.5.0
- ⚠️ **BREAKING** Rename `BUILDKITE_TEST_SPLITTER_CMD` to `BUILDKITE_SPLITTER_TEST_CMD`.
- ⚠️ **BREAKING** Change the authentication mechanism to use Buildkite API access token. See [Migrating to 0.5.0](https://github.com/buildkite/test-splitter/tree/cdbbe348a0eb10bb6ca3211f2c5cd870f0dadfdd?tab=readme-ov-file#migrating-from-040).
- Add support for automatically retrying failed tests using `BUILDKITE_SPLITTER_RETRY_COUNT`.
- Add `--version` flag to aid in debugging.
