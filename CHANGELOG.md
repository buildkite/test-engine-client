# Changelog

## 0.6.0 - 2024-06-21

- ⚠️ **BREAKING** Remove support for the undocumented `--files` flag.
- ⚠️ **BREAKING** Rename the `BUILDKITE_API_ACCESS_TOKEN` environment variable to `BUILDKITE_SPLITTER_API_ACCESS_TOKEN`.
- Add support for split-by-example using the `BUILDKITE_SPLITTER_SPLIT_BY_EXAMPLE` environment variable.
- Add support for more verbose debug logging using the `BUILDKITE_SPLITTER_DEBUG_ENABLED` environment variable.

## 0.5.1
- Add a new line to each error log.

## 0.5.0
- ⚠️ **BREAKING** Rename `BUILDKITE_TEST_SPLITTER_CMD` to `BUILDKITE_SPLITTER_TEST_CMD`.
- ⚠️ **BREAKING** Change the authentication mechanism to use Buildkite API access token. See [Migrating to 0.5.0](https://github.com/buildkite/test-splitter/tree/main?tab=readme-ov-file#migrating-to-050).
- Add support for automatically retrying failed tests using `BUILDKITE_SPLITTER_RETRY_COUNT`.
- Add `--version` flag to aid in debugging.
