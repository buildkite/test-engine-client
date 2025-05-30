# pytest_pants

This directory contains a working example of a
[Pants](https://www.pantsbuild.org/) project using test-engine-client. It
demonstrates how to integrate Pants with
[buildkite-test-collector][bk-test-collector] and test-engine-client.

## What is Pants?

[Pants](https://www.pantsbuild.org/) is a fast, scalable, user-friendly build
system for codebases of all sizes. It's particularly useful for:

- Managing Python dependencies and virtual environments
- Running tests at scale across large codebases
- Incremental builds and testing (only test what changed)
- Enforcing consistent tooling and standards

## Key Configuration Files

This example shows the essential files needed for Pants + pytest integration:

- **`pants.toml`** - Main Pants configuration file that defines:
  - Python version constraints
  - Backend plugins (enables Python support)
  - Resolve configuration for dependency management
- **`3rdparty/python/BUILD`** - Defines Python requirements as Pants targets
- **`3rdparty/python/pytest-requirements.txt`** - Standard pip requirements file
- **`3rdparty/python/pytest.lock`** - Generated lockfile ensuring reproducible builds
- **`BUILD`** - Tells Pants about Python tests in this directory

## Quick Start

1. **Install Pants** (if not already installed):
   ```sh
   curl --proto '=https' --tlsv1.2 -fsSL https://static.pantsbuild.org/setup/get-pants.sh | bash
   ```

2. **Generate lockfiles** (after adding new dependencies):
   ```sh
   pants generate-lockfiles --resolve=pytest
   ```

3. **Run tests**:
   ```sh
   pants test ::  # Run all tests
   pants test //path/to/specific:test  # Run specific test
   ```

## Integration with test-engine-client

When using with buildkite-test-collector and test-engine-client:

- Set `BUILDKITE_TEST_ENGINE_TEST_RUNNER=pytest-pants`
- The `buildkite-test-collector` package must be included in your pytest resolve
- Use pants-specific test commands that include the required `--json` and `--merge-json` flags

See the main [pytest-pants documentation](../../../docs/pytest-pants.md) for complete integration details.

## Updates to pytest pants resolve lock file

This lock file is what is used by tests. Updating this is particularly useful if the changes being made require a newer version of [buildkite-test-collector][bk-test-collector].

```sh
pants generate-lockfiles --resolve=pytest
```

[bk-test-collector]: https://pypi.org/project/buildkite-test-collector/
