# Full Solution

Clean end state: selection targets are first-class in the client, API, planner, result attribution, and timing storage.

## Concept

A selection target is a named group of tests the runner can execute.

| Tool | Target example | Selector tag |
| --- | --- | --- |
| Go | `github.com/acme/repo/internal/api` | `package` |
| Bazel | `//src/api:test` | `bazel_label` |
| Pants | `src/python/api:tests` | `pants_target` |

## User API

```sh
--split-by file
--split-by example
--split-by selection-target
```

Environment variable:

```sh
BUILDKITE_TEST_ENGINE_SPLIT_BY=selection-target
```

`--split-by-example` / `BUILDKITE_TEST_ENGINE_SPLIT_BY_EXAMPLE=true` remain supported as legacy aliases for `split-by=example`.

Target metadata:

```sh
--selection-target-tag bazel_label
--selection-targets-file bazel-targets.txt
```

```sh
BUILDKITE_TEST_ENGINE_SELECTION_TARGET_TAG=bazel_label
BUILDKITE_TEST_ENGINE_SELECTION_TARGETS_FILE=bazel-targets.txt
```

For custom selection-target mode, require:

```sh
BUILDKITE_TEST_ENGINE_TEST_CMD='bazel test {{selectionTargets}}'
```

The placeholder expands to this node’s assigned targets as separate argv entries. If no targets are assigned, skip the command unless `fail-on-no-tests` is enabled.

## Native request shape

Prefer a target-specific type instead of further overloading `TestCase`:

```json
{
  "tests": {
    "selection_targets": [
      {
        "target": "//src/api:test",
        "selector": {
          "tag": "bazel_label",
          "value": "//src/api:test"
        }
      }
    ]
  }
}
```

`selector.value` should default to the executable target string. Avoid separate executable target vs selector value mapping until real users need it.

If compatibility with the existing `TestCase` shape is required, the transport can temporarily use `path`, but the internal client model should still distinguish files, examples, and selection targets.

## Go rollout

Initial opt-in:

```sh
BUILDKITE_TEST_ENGINE_TEST_RUNNER=gotest
BUILDKITE_TEST_ENGINE_SPLIT_BY=selection-target
```

The client discovers packages with `go list ./...` and sends:

```json
{
  "tests": {
    "selection_targets": [
      {
        "target": "github.com/acme/repo/internal/api",
        "selector": {
          "tag": "package",
          "value": "github.com/acme/repo/internal/api"
        }
      }
    ]
  }
}
```

Make this the `gotest` default only after server capability support, package attribution, timing summaries, and an opt-out are proven.

## Bazel/Pants examples

Bazel:

```yaml
commands:
  - bazel query 'kind(test, //...)' > bazel-targets.txt
  - bktec run
parallelism: 4
env:
  BUILDKITE_TEST_ENGINE_TEST_RUNNER: custom
  BUILDKITE_TEST_ENGINE_SPLIT_BY: selection-target
  BUILDKITE_TEST_ENGINE_SELECTION_TARGET_TAG: bazel_label
  BUILDKITE_TEST_ENGINE_SELECTION_TARGETS_FILE: bazel-targets.txt
  BUILDKITE_TEST_ENGINE_TEST_CMD: bazel test {{selectionTargets}}
```

Pants uses the same pattern with `pants_target` and `pants test {{selectionTargets}}`.

## Validation matrix

| Runner | `file` | `example` | `selection-target` |
| --- | --- | --- | --- |
| `gotest` | legacy/fallback | no | yes |
| `custom` | yes | no | yes, with target file |
| `rspec` | yes | yes | no initially |
| `pytest` | yes | yes | no initially |
| `playwright` | yes | yes | no initially |
| `jest`, `cypress`, `nunit` | yes | no | no initially |

Conflict rules:

```text
--split-by-example + --split-by=example => OK
--split-by-example + --split-by=file|selection-target => error
--selection-targets-file without --split-by=selection-target => error
--files with --selection-targets-file in selection-target mode => error
```

## Implementation impact in this client

Client areas to change:

```text
config.Config: SplitBy, SelectionTargetTag, SelectionTargetsFile
cli.go: flags/env vars and conflict handling
command/files.go: new target-file parser, not reused --files parsing
api/create_test_plan.go: SelectionTargets request field or compatibility metadata
plan/type.go: selection-target format or separate target type
plan/summary.go: target-aware nouns and timing metadata
runner/custom.go: target mode bypasses TEST_FILE_PATTERN and uses {{selectionTargets}}
runner/gotest.go: package targets become selection targets when opted in
runner command construction: argv-safe placeholder expansion or shell escaping
run retry loop: retry executable target units, not arbitrary failed examples
```

## Backend requirements

The full solution requires:

```text
native selection_targets API schema
planner input model for selector targets
ClickHouse materialized view keyed by suite/runner/selector tag/value
unknown-target fallback durations
result attribution to selector values
capability negotiation
summary/debug metadata
tests for request, planning, attribution, retry, and fallback behavior
```

For Bazel/Pants/custom, attribution is the riskiest part. If one command runs multiple targets, uploaded results must identify the target that produced each timing. Otherwise planning can still dispatch targets but must use unknown/default timing.

## Retry contract

Target mode retries target units:

```text
Go => failed package(s)
Bazel/Pants attributed failures => failed target(s)
Bazel/Pants unattributed failures => retry all assigned targets or skip target retry, but document the choice
```

Do not silently retry failed test examples through `{{selectionTargets}}` unless those examples are valid runner targets.

## Non-negotiable

Any solution that distributes target strings without selector attribution and selector timing lookup is target dispatch, not smart package/target splitting.
