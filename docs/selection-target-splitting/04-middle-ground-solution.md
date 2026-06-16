# Middle-ground Solution

This is the recommended practical path.

Add the clean target-oriented CLI now, but initially send targets through the existing `tests.files` payload until backend `selection_targets` support is ready.

This is only acceptable if the client/server still carry selector attribution and feed the ClickHouse materialized view. The middle-ground solution is not “dispatch targets now, solve timing later”; it is “use legacy transport temporarily while preserving selector tag/value semantics.”

## User API

```sh
BUILDKITE_TEST_ENGINE_TEST_RUNNER=custom
BUILDKITE_TEST_ENGINE_SPLIT_BY=selection-target
BUILDKITE_TEST_ENGINE_SELECTION_TARGET_TAG=bazel_label
BUILDKITE_TEST_ENGINE_SELECTION_TARGETS_FILE=bazel-targets.txt
BUILDKITE_TEST_ENGINE_TEST_CMD='bazel test {{selectionTargets}}'
```

## Bazel example

```yaml
steps:
  - label: ":bazel: Bazel tests"
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

## Pants example

```yaml
steps:
  - label: ":pants: Pants tests"
    commands:
      - pants list :: --filter-target-type=python_tests > pants-targets.txt
      - bktec run
    parallelism: 4
    env:
      BUILDKITE_TEST_ENGINE_TEST_RUNNER: custom
      BUILDKITE_TEST_ENGINE_SPLIT_BY: selection-target
      BUILDKITE_TEST_ENGINE_SELECTION_TARGET_TAG: pants_target
      BUILDKITE_TEST_ENGINE_SELECTION_TARGETS_FILE: pants-targets.txt
      BUILDKITE_TEST_ENGINE_TEST_CMD: pants test {{selectionTargets}}
```

## Client behavior, phase 1

The user-facing API is target-based, but the client sends file-shaped payloads for compatibility:

```json
{
  "tests": {
    "files": [
      { "path": "//src/api:test" },
      { "path": "//src/auth:test" }
    ]
  }
}
```

This preserves server compatibility while giving users a clean CLI.

However, the request must still carry or imply the selector tag:

```json
{
  "tests": {
    "files": [
      { "path": "//src/api:test" },
      { "path": "//src/auth:test" }
    ]
  },
  "selection_target": {
    "tag": "bazel_label",
    "value_from": "tests.files[].path"
  }
}
```

The exact wire shape can differ, but the planner must be able to query the ClickHouse materialized view using:

```text
suite + runner + selector.tag + selector.value
```

## Client behavior, phase 2

Once the server supports it, the same CLI starts sending:

```json
{
  "tests": {
    "selection_targets": [
      {
        "format": "selection_target",
        "path": "//src/api:test",
        "selector": {
          "tag": "bazel_label",
          "value": "//src/api:test"
        }
      }
    ]
  }
}
```

## Why this is attractive

```text
Users get the right CLI now.
We avoid teaching new users the --files workaround.
Backend rollout can happen independently.
The same user config can evolve to true selection_targets later.
Selector timing can still use the ClickHouse materialized view during compatibility transport.
```

## Critical requirement

Do not silently switch payload shape unless the server can handle it.

Use server capability negotiation as the primary rollout mechanism.

```text
server supports selection_targets:
  send tests.selection_targets

server does not support selection_targets:
  send target strings through tests.files during middle-ground rollout

user explicitly forces native selection_targets transport on an unsupported server:
  fail clearly
```

The client should not break older server environments by sending `selection_targets` before support is available. Avoid relying on “try request, then fallback after error” as the default path; ignored fields or partial acceptance are risky.

## Phase 1 limitation

Phase 1 uses cleaner CLI with compatibility transport. It must still support selector attribution and ClickHouse materialized-view lookup. If it cannot, it should not be described as satisfying the original requirement.

```text
Targets are accepted via --selection-targets-file.
{{selectionTargets}} is used for command construction.
Selector tag/value keys are available to the planner.
ClickHouse materialized-view lookup is used when data exists.
Unknown targets use default/fallback timing.
```

Compatibility transport must not mean compatibility semantics. Even if the wire payload uses `tests.files`, the server should treat those values as selection targets for timing purposes. Target history should go through the selector-keyed ClickHouse materialized view, not ordinary file timing history.

## Final sanity check

This approach works if all of these are true:

```text
the public CLI is target-oriented from day one
the server capability check decides native versus compatibility transport
compatibility transport still carries selector.tag/value semantics
the planner queries the selector ClickHouse materialized view when timing exists
debug output says whether selector timing was used or defaulted
result upload can attribute duration to selector values, or docs clearly mark smart timing unavailable
```

This is the best practical path because the user-facing API does not change when the backend moves from compatibility transport to native `tests.selection_targets`.

## Recommendation

This is the best path unless backend support lands at the same time as the client API.

It gives us a good public API now and an honest path to a better backend model later.
