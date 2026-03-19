# Using bktec with pants (Experimental)

> [!WARNING]
> Pants support is currently experimental and has limited feature support. Only the following features are supported:
>
> - Intelligent test splitting with `{{testExamples}}` and `--files`
> - Automatically retry failed tests
> - Mute tests (ignore test failures)
>
> The following features are not supported:
>
> - Filter test files (via glob patterns)
> - Split slow files by individual test example
> - Skip tests

## Prerequisites

1. [Install and configure Buildkite Test Collector for pytest](https://buildkite.com/docs/test-engine/python-collectors#pytest-collector). The `buildkite-test-collector` package must be added to the pants resolve used by pytest.

   See the example configuration files in the [pytest_pants testdata directory](../internal/runner/testdata/pytest_pants):
   - [pants.toml](../internal/runner/testdata/pytest_pants/pants.toml) - pants configuration
   - [3rdparty/python/BUILD](../internal/runner/testdata/pytest_pants/3rdparty/python/BUILD) - python_requirement targets
   - [3rdparty/python/pytest-requirements.txt](../internal/runner/testdata/pytest_pants/3rdparty/python/pytest-requirements.txt) - Python requirements.txt

   After adding the dependency, regenerate lockfiles:
   ```sh
   pants generate-lockfiles --resolve=pytest
   ```

2. Set the test runner:
   ```sh
   export BUILDKITE_TEST_ENGINE_TEST_RUNNER=pytest-pants
   ```

3. There is no default test command for the pants runner. You must set `BUILDKITE_TEST_ENGINE_TEST_CMD` explicitly.

## Usage modes

There are two ways to use bktec with pants:

- **Without test splitting** — pants handles test selection (e.g. `--changed-since`, `//::`) and bktec adds retries and muting on top.
- **With intelligent test splitting** — bktec distributes tests across parallel nodes using historical timing data, and pants handles execution. This is the primary use case described below.

## Intelligent test splitting

This is the recommended approach for parallel CI. Instead of relying on pants' built-in `--test-shard` (which distributes tests without considering execution time), bktec uses historical timing data from Buildkite Test Engine to create balanced shards.

The key idea: separate **target resolution** (`pants filter`) from **test execution** (`pants test`). This gives bktec an explicit list of targets to plan against.

### How it works

The flow has three steps, typically split across two CI pipeline steps:

#### Step 1: Resolve targets and create a plan

In a setup/planning CI step, determine which tests need to run and ask bktec to create a balanced plan:

```sh
# Resolve affected test targets into an explicit list.
# This replaces using --changed-since directly on pants test.
pants \
    --changed-since="origin/main" \
    --changed-dependents=transitive \
    --filter-target-type="+python_test" \
    filter > affected_tests.txt

# Create a balanced test plan.
# --max-parallelism tells bktec the maximum number of shards to create.
# --files provides the explicit target list (bypasses bktec's glob-based discovery,
# which is not supported for the pants runner).
bktec plan --json --files affected_tests.txt --max-parallelism 4
```

`bktec plan --json` outputs JSON with two values:
```json
{"BUILDKITE_TEST_ENGINE_PLAN_IDENTIFIER":"<identifier>","BUILDKITE_TEST_ENGINE_PARALLELISM":"4"}
```

- `BUILDKITE_TEST_ENGINE_PLAN_IDENTIFIER` — a unique identifier for the plan, cached server-side. Pass this to `bktec run` so each parallel node fetches the same plan.
- `BUILDKITE_TEST_ENGINE_PARALLELISM` — the number of shards the plan was created for.

You need to make both the plan identifier and the `affected_tests.txt` file available to the parallel run steps. How you do this depends on your CI setup (e.g. Buildkite meta-data + artifacts, environment variables, shared filesystem).

#### Step 2: Run shards in parallel

Each parallel node fetches the plan and runs its assigned subset of tests:

```sh
# BUILDKITE_TEST_ENGINE_PLAN_IDENTIFIER must be set (from step 1).
# BUILDKITE_PARALLEL_JOB and BUILDKITE_PARALLEL_JOB_COUNT are set automatically
# by Buildkite when using the parallelism setting on a step.

bktec run --files affected_tests.txt
```

bktec fetches the cached plan by identifier, extracts this node's assigned tests, and substitutes `{{testExamples}}` in the test command with those test paths.

#### The test command

The test command tells bktec how to invoke pants. `{{testExamples}}` is where bktec injects the test targets assigned to the current node:

```sh
export BUILDKITE_TEST_ENGINE_TEST_CMD="pants test {{testExamples}} -- --json={{resultPath}} --merge-json"
```

For a node assigned 3 targets, this becomes:

```sh
pants test src/python/myapp/tests/test_auth.py:tests src/python/myapp/tests/test_api.py:tests src/python/myapp/tests/test_models.py:tests -- --json=/tmp/bktec-xxx/result.json --merge-json
```

- `{{testExamples}}` — replaced by bktec with this node's test targets (space-separated, shell-quoted)
- `{{resultPath}}` — replaced by bktec with a temporary file path for test results
- `--json` and `--merge-json` — flags from `buildkite-test-collector` that write pytest results to a JSON file. bktec reads this file for retry logic and test reporting.

> [!IMPORTANT]
> The test command must include `-- --json={{resultPath}} --merge-json` after the pants arguments. bktec requires these to read test results for retries and verification.

### Required environment variables

| Variable | Description |
|---|---|
| `BUILDKITE_TEST_ENGINE_TEST_RUNNER` | Must be `pytest-pants` |
| `BUILDKITE_TEST_ENGINE_TEST_CMD` | The pants test command with `{{testExamples}}` and `{{resultPath}}` placeholders |
| `BUILDKITE_TEST_ENGINE_API_ACCESS_TOKEN` | Buildkite API token with `read_suites`, `read_test_plan`, `write_test_plan` scopes |
| `BUILDKITE_TEST_ENGINE_SUITE_SLUG` | Your Buildkite Test Engine suite slug |
| `BUILDKITE_TEST_ENGINE_PLAN_IDENTIFIER` | The plan identifier from `bktec plan` (only needed for `bktec run`) |
| `BUILDKITE_PARALLEL_JOB` | Current node index (0-based). Set automatically by Buildkite with `parallelism` |
| `BUILDKITE_PARALLEL_JOB_COUNT` | Total number of parallel nodes. Set automatically by Buildkite with `parallelism` |

### Buildkite pipeline example

This is a complete example showing how the pieces fit together in a Buildkite pipeline:

```yaml
steps:
  # Step 1: Resolve targets and create a balanced plan
  - label: "Plan test shards"
    key: plan
    command: |
      # Resolve affected test targets
      pants \
          --changed-since="origin/$${BUILDKITE_PULL_REQUEST_BASE_BRANCH:-main}" \
          --changed-dependents=transitive \
          --filter-target-type="+python_test" \
          filter > affected_tests.txt

      # Create a balanced test plan
      PLAN_JSON=$$(bktec plan --json --files affected_tests.txt --max-parallelism 10)

      # Pass the plan identifier to the run steps
      PLAN_ID=$$(echo "$$PLAN_JSON" | jq -r '.BUILDKITE_TEST_ENGINE_PLAN_IDENTIFIER')
      buildkite-agent meta-data set BKTEC_PLAN_IDENTIFIER "$$PLAN_ID"

      # Upload the target list for parallel nodes
      buildkite-agent artifact upload affected_tests.txt
    env:
      BUILDKITE_TEST_ENGINE_TEST_RUNNER: pytest-pants
      BUILDKITE_TEST_ENGINE_SUITE_SLUG: my-suite
      BUILDKITE_TEST_ENGINE_TEST_CMD: "pants test {{testExamples}} -- --json={{resultPath}} --merge-json"

  # Step 2: Run shards in parallel
  - label: "Test"
    depends_on: plan
    parallelism: 10
    command: |
      buildkite-agent artifact download affected_tests.txt .
      export BUILDKITE_TEST_ENGINE_PLAN_IDENTIFIER=$$(buildkite-agent meta-data get BKTEC_PLAN_IDENTIFIER)
      bktec run --files affected_tests.txt
    env:
      BUILDKITE_TEST_ENGINE_TEST_RUNNER: pytest-pants
      BUILDKITE_TEST_ENGINE_SUITE_SLUG: my-suite
      BUILDKITE_TEST_ENGINE_TEST_CMD: "pants test {{testExamples}} -- --json={{resultPath}} --merge-json"
```

### How this compares to `--test-shard`

| | `pants --test-shard` | bktec + `{{testExamples}}` |
|---|---|---|
| **Target resolution** | `pants test --changed-since` handles it | `pants filter` produces an explicit list |
| **Shard balancing** | Round-robin by target count | Historical timing data from Buildkite Test Engine |
| **Test execution** | `pants test --test-shard=i/N` | `pants test {{testExamples}}` via bktec |
| **Result reporting** | No JSON result file | `--json={{resultPath}} --merge-json` for retries |
| **Retry support** | Manual retry of entire shard | bktec retries only the failed tests |

## Without test splitting

If you don't need bktec to manage test splitting — for example, if you want pants to handle test selection and you only want bktec for retries or muting — you can omit `{{testExamples}}` from the test command. bktec will run the command as-is.

```sh
export BUILDKITE_TEST_ENGINE_TEST_RUNNER=pytest-pants
export BUILDKITE_TEST_ENGINE_TEST_CMD="pants --filter-target-type=python_test --changed-since=HEAD~1 test -- --json={{resultPath}} --merge-json"
bktec run
```

See the [pants Advanced target selection docs][pants-advanced-target-selection] for more on `--changed-since`.

## Automatically retry failed tests

You can configure bktec to automatically retry failed tests using the `BUILDKITE_TEST_ENGINE_RETRY_COUNT` environment variable. When this variable is set to a number greater than `0`, bktec will retry each failed test up to the specified number of times. Because pants caches test results, only failed tests will be retried.

```sh
export BUILDKITE_TEST_ENGINE_RETRY_COUNT=2
```

[pants-advanced-target-selection]: https://www.pantsbuild.org/stable/docs/using-pants/advanced-target-selection
