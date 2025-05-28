# Using bktec with Cucumber

To integrate bktec with Cucumber, set the `BUILDKITE_TEST_ENGINE_TEST_RUNNER` environment variable to `cucumber`. Then, specify the `BUILDKITE_TEST_ENGINE_RESULT_PATH` to define where the JSON result should be stored. bktec will instruct Cucumber to output the JSON result to this path, which is necessary for bktec to read the test results for retries and verification purposes.

```sh
export BUILDKITE_TEST_ENGINE_TEST_RUNNER=cucumber
export BUILDKITE_TEST_ENGINE_RESULT_PATH=tmp/cucumber-result.json
```

## Configure test command
By default, bktec runs Cucumber with the following command:

```sh
bundle exec cucumber --format pretty --format json --out {{resultPath}} {{testExamples}}
```

In this command:
- `{{testExamples}}` is replaced by bktec with the list of feature files or scenarios to run.
- `{{resultPath}}` is replaced with the value set in `BUILDKITE_TEST_ENGINE_RESULT_PATH`.

You can customise this command using the `BUILDKITE_TEST_ENGINE_TEST_CMD` environment variable.

```sh
export BUILDKITE_TEST_ENGINE_TEST_CMD="bundle exec cucumber --format json --out {{resultPath}} {{testExamples}}"
```

> **IMPORTANT** – Make sure your custom command includes `--format json --out {{resultPath}}` so that bktec can parse the results.

## Filter feature files
By default, bktec runs feature files that match the `features/**/*.feature` pattern. You can customise this pattern using the `BUILDKITE_TEST_ENGINE_TEST_FILE_PATTERN` environment variable.

```sh
export BUILDKITE_TEST_ENGINE_TEST_FILE_PATTERN=features/login/**/*.feature
```

You can also exclude certain directories or files with `BUILDKITE_TEST_ENGINE_TEST_FILE_EXCLUDE_PATTERN`:

```sh
export BUILDKITE_TEST_ENGINE_TEST_FILE_EXCLUDE_PATTERN=features/experimental
```

> **TIP** – The patterns use the same glob syntax as the [zzglob](https://github.com/DrJosh9000/zzglob#pattern-syntax) library.

## Automatically retry failed scenarios
Use `BUILDKITE_TEST_ENGINE_RETRY_COUNT` to automatically retry failed scenarios. When this variable is set and greater than `0`, failed scenarios will be re-run using the command from `BUILDKITE_TEST_ENGINE_RETRY_CMD` (or the main test command if not set).

```sh
export BUILDKITE_TEST_ENGINE_RETRY_COUNT=2
```

A typical retry command might look like:

```sh
export BUILDKITE_TEST_ENGINE_RETRY_CMD="bundle exec cucumber {{testExamples}} --format json --out {{resultPath}}"
```

## Limitation – Split by example
Splitting slow files by individual scenario is not currently supported for Cucumber. Test plans will be generated at the feature-file level.
