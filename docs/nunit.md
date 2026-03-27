# Using bktec with NUnit

To integrate `bktec` with NUnit (.NET), you need the [JUnit XML test logger](https://github.com/spekt/junit.testlogger) NuGet package installed in your test project so that `dotnet test` can produce JUnit XML results.

```sh
dotnet add package JUnitXml.TestLogger
```

## How it works

`bktec` discovers `.cs` test files using a glob pattern, then maps each file to a class name (the filename without the `.cs` extension). It builds a `dotnet test --filter` expression using `FullyQualifiedName~.ClassName` predicates joined with `|` (OR), so only the test classes assigned to this node are executed.

> [!IMPORTANT]
> Because test splitting is based on file/class name mapping, each `.cs` test file should contain a single test class whose name matches the filename (e.g. `CalculatorTests.cs` contains class `CalculatorTests`). This is the standard NUnit convention.

## Quick start

Build your solution first (so each split can use `--no-build`), then run `bktec`:

```sh
# Build once upfront
dotnet build

# Configure bktec
export BUILDKITE_TEST_ENGINE_TEST_RUNNER=nunit
export BUILDKITE_TEST_ENGINE_RESULT_PATH=test-results/results.xml
export BUILDKITE_TEST_ENGINE_TEST_FILE_PATTERN="tests/**/*Tests.cs"
export BUILDKITE_TEST_ENGINE_SUITE_SLUG=your-suite-slug
export BUILDKITE_TEST_ENGINE_API_ACCESS_TOKEN=your-token

# Run
bktec run
```

## Configure test command

By default, `bktec` runs tests with the following command:

```sh
dotnet test --no-build --filter {{testFilter}} --logger junit;LogFilePath={{resultPath}}
```

In this command:
- `{{testFilter}}` is replaced by `bktec` with the filter expression (e.g. `FullyQualifiedName~.CalculatorTests|FullyQualifiedName~.StringUtilsTests`)
- `{{resultPath}}` is replaced with the `BUILDKITE_TEST_ENGINE_RESULT_PATH` environment variable

You can customize this command using the `BUILDKITE_TEST_ENGINE_TEST_CMD` environment variable. For example, to target a specific project:

```sh
export BUILDKITE_TEST_ENGINE_TEST_CMD="dotnet test MyProject.Tests --no-build --filter {{testFilter}} --logger junit;LogFilePath={{resultPath}}"
```

## Configure test file pattern

By default, `bktec` discovers test files matching `**/*Tests.cs`. You can customize this with:

```sh
export BUILDKITE_TEST_ENGINE_TEST_FILE_PATTERN="tests/**/*Tests.cs"
```

You can also exclude files:

```sh
export BUILDKITE_TEST_ENGINE_TEST_FILE_EXCLUDE_PATTERN="tests/**/IntegrationTests.cs"
```

## Automatically retry failed tests

You can configure `bktec` to automatically retry failed tests using the `BUILDKITE_TEST_ENGINE_RETRY_COUNT` environment variable:

```sh
export BUILDKITE_TEST_ENGINE_RETRY_COUNT=1
```

## Full Buildkite pipeline example

```yaml
steps:
  - name: "NUnit :dotnet:"
    commands:
      - dotnet build
      - bktec run
    env:
      BUILDKITE_TEST_ENGINE_SUITE_SLUG: your-suite-slug
      BUILDKITE_TEST_ENGINE_API_ACCESS_TOKEN: your-api-token
      BUILDKITE_TEST_ENGINE_TEST_RUNNER: nunit
      BUILDKITE_TEST_ENGINE_RESULT_PATH: test-results/results.xml
      BUILDKITE_TEST_ENGINE_TEST_FILE_PATTERN: "tests/**/*Tests.cs"
      BUILDKITE_TEST_ENGINE_RETRY_COUNT: 1
    parallelism: 4
    plugins:
      - test-collector#v1.11.0:
          files: "test-results/results.xml"
          format: "junit"
```
