# Changelog

## 1.0.1
- Experimental support for Cypress. See [Cypress usage guide](./docs/cypress.md).
- Experimental support for Playwright. See [Playwright usage guide](./docs/playwright.md).
- Update `BUILDKITE_TEST_ENGINE_TEST_CMD` and `BUILDKITE_TEST_ENGINE_RETRY_CMD` for Jest. See [Jest usage guide](./docs/jest.md).
- Remove `**/node_modules` from default value of `BUILDKITE_TEST_ENGINE_TEST_FILE_EXCLUDE_PATTERN`. Files inside `node_modules` will be ignore regardless the value of this environment variable.

## 1.0.0 - 2024-09-23
- ⚠️ **BREAKING** Rename all environment variables from `BUILDKITE_SPLITTER_*` to `BUILDKITE_TEST_ENGINE_*`. See [Migrating to 1.0.0](https://github.com/buildkite/test-splitter/tree/90b699918b11500336f8a0fce306da917fba7408?tab=readme-ov-file#migrating-to-100)
- ⚠️ **BREAKING** Add the `BUILDKITE_TEST_ENGINE_TEST_RUNNER` as required environment variable.

## 0.9.1 - 2024-09-16
- Fix issue with split by example when shared examples are used in RSpec

## 0.9.0 - 2024-09-11
- ⚠️ **BREAKING** Add the `BUILDKITE_SPLITTER_RESULT_PATH` required environment variable. See [Migrating to 0.9.0](https://github.com/buildkite/test-splitter/tree/db4cab8cd6c82392553cd80481cf75e3888c2f4c?tab=readme-ov-file#migrating-to-090).
- Experimental support for Jest by setting `BUILDKITE_SPLITTER_TEST_RUNNER` to `jest`.
- Update the retry behavior to only retry failed tests.
- Update split-by-example behavior to perform more work server-side.
- Improve configuration error message.
- Fix issue printing dry-run errors.
- Fix issue with `BUILDKITE_STEP_ID` presence validation.

## 0.8.1 - 2024-08-06
- Add `BUILDKITE_BRANCH` env var for test plan experiments
- Fix to zzglob library issue where files not matching the include pattern are in the test plan

## 0.8.0 - 2024-07-26
- Add support to customize the rspec retry command.
- Fix issue with file globbing during the file discovery.

## 0.7.3 - 2024-07-19
- Improve handling when the runner terminates due to an OS-level signal.

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
