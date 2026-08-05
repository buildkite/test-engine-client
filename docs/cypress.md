# Using bktec with Cypress
To integrate bktec with Cypress, set the `BUILDKITE_TEST_ENGINE_TEST_RUNNER` environment variable to `cypress`.

```sh
export BUILDKITE_TEST_ENGINE_TEST_RUNNER=cypress
```

## Configure test command
By default, bktec runs Cypress with the following command:

```sh
npx cypress run --spec {{testExamples}}
```

In this command, `{{testExamples}}` is replaced by bktec with the list of test files to run. You can customize this command using the `BUILDKITE_TEST_ENGINE_TEST_CMD` environment variable.

To customize the test command, set the following environment variable:
```sh
export BUILDKITE_TEST_ENGINE_TEST_CMD="yarn cypress:run --spec {{testExamples}}"
```

## Filter test files
By default, bktec runs test files that match the `**/*.cy.{js,jsx,ts,tsx}` pattern. You can customize this pattern using the `BUILDKITE_TEST_ENGINE_TEST_FILE_PATTERN` environment variable. For instance, to configure bktec to only run Cypress test files inside a `cypress/e2e` directory, use:
```sh
export BUILDKITE_TEST_ENGINE_TEST_FILE_PATTERN=cypress/e2e/**/*.cy.js
```

Additionally, you can exclude specific files or directories that match a certain pattern using the `BUILDKITE_TEST_ENGINE_TEST_FILE_EXCLUDE_PATTERN` environment variable. For example, to exclude test files inside the `cypress/component` directory, use:

```sh
export BUILDKITE_TEST_ENGINE_TEST_FILE_EXCLUDE_PATTERN=cypress/component
```

You can also use both `BUILDKITE_TEST_ENGINE_TEST_FILE_PATTERN` and `BUILDKITE_TEST_ENGINE_TEST_FILE_EXCLUDE_PATTERN` simultaneously. For example, to run all Cypress test files with `cy.js`, except those in the `cypress/e2e` directory, use:

```sh
export BUILDKITE_TEST_ENGINE_TEST_FILE_PATTERN=**/*.cy.js
export BUILDKITE_TEST_ENGINE_TEST_FILE_EXCLUDE_PATTERN=cypress/e2e
```

> [!TIP]
> This option accepts the pattern syntax supported by the [zzglob](https://github.com/DrJosh9000/zzglob?tab=readme-ov-file#pattern-syntax) library.

## Selector-based test splitting

Cypress uses [selector-based test splitting](../README.md#selector-based-test-splitting) by default. Each test file path discovered with Cypress's default `**/*.cy.{js,jsx,ts,tsx}` pattern becomes a selector. You can customize which files are discovered with `BUILDKITE_TEST_ENGINE_TEST_FILE_PATTERN` and `BUILDKITE_TEST_ENGINE_TEST_FILE_EXCLUDE_PATTERN`; the `{{testExamples}}` command placeholder works as before.

> [!NOTE]
> If you upload results with the [JavaScript test collector](https://buildkite.com/docs/test-engine/test-collection/javascript-collectors), we recommend updating to `buildkite-test-collector` v1.10.0 or later so selectors are attributed directly. Older versions can still upload results, and Test Engine [attempts to match selectors using the file path](../README.md#how-selectors-are-matched). If no history matches, Test Engine uses default duration estimates and bktec prints a warning. Updating matters most if you use a location prefix, since the fallback only handles the prefix on a best-effort basis.
