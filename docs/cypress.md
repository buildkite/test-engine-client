# Using bktec with Cypress
To integrate bktec with Cypress, set the `BUILDKITE_TEST_ENGINE_TEST_RUNNER` environment variable to `cypress`.

```sh
export BUILDKITE_TEST_ENGINE_TEST_RUNNER=cypress
```

## Test Command
By default, bktec runs Cypress with the following command:

```sh
npx cypress run --spec {{testExamples}}
```

In this command, `{{testExamples}}` is replaced by bktec with the list of test files to run. You can customize this command using the `BUILDKITE_TEST_ENGINE_TEST_CMD` environment variable.

To customize the test command, set the following environment variable:
```sh
export BUILDKITE_TEST_ENGINE_TEST_CMD="yarn cypress:run --spec {{testExamples}}"
```

## Test Discovery and Filtering
bktec discovers the test files using a glob pattern. By default, it identifies the files matching the `**/*.cy.{js,jsx,ts,tsx}` pattern. This means it will recursively find all JavaScript or TypeScript files with a `.cy` suffix, such as `/cypress/e2e/login.cy.ts`. You can customize this pattern using the `BUILDKITE_TEST_ENGINE_TEST_FILE_PATTERN` environment variable.

Additionally, you can exclude certain files or directories that match a specific pattern using the `BUILDKITE_TEST_ENGINE_TEST_FILE_EXCLUDE_PATTERN` environment variable.

To customize the discovery pattern and exclude certain files, set the following environment variable:
```sh
export BUILDKITE_TEST_ENGINE_TEST_PATTERN=**/*.cy.{ts,tsx}
export BUILDKITE_TEST_ENGINE_TEST_EXCLUDE_PATTERN=cypress/component
```

With the above configurations, bktec will discover all files matching the `**/*.cy.{ts,tsx}` pattern and exclude any files inside the `cypress/component` directory.

> [!TIP]
> This option accepts the pattern syntax supported by the [zzglob](https://github.com/DrJosh9000/zzglob?tab=readme-ov-file#pattern-syntax) library.
