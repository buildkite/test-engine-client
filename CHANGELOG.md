# Changelog

## 0.5.0
- ⚠️ **BREAKING** Rename `BUILDKITE_TEST_SPLITTER_CMD` to `BUILDKITE_SPLITTER_TEST_CMD`.
- ⚠️ **BREAKING** Change the authentication mechanism to use Buildkite API access token. See [Migrating to 0.5.0](https://github.com/buildkite/test-splitter/tree/main?tab=readme-ov-file#migrating-to-050).
- Add support for automatically retrying failed tests using `BUILDKITE_SPLITTER_RETRY_COUNT`.
- Add `--version` flag to aid in debugging.
