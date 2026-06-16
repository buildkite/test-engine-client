# Middle-ground Solution

Recommended path: prove selector timing first, then add the target-oriented CLI while allowing legacy transport during backend rollout.

This is the practical implementation path if the backend spike confirms we can store, query, or at least honestly report selector-level timing.

## User API

```sh
BUILDKITE_TEST_ENGINE_TEST_RUNNER=custom
BUILDKITE_TEST_ENGINE_SPLIT_BY=selection-target
BUILDKITE_TEST_ENGINE_SELECTION_TARGET_TAG=bazel_label
BUILDKITE_TEST_ENGINE_SELECTION_TARGETS_FILE=bazel-targets.txt
BUILDKITE_TEST_ENGINE_TEST_CMD='bazel test {{selectionTargets}}'
```

Bazel example:

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

Pants is the same shape with `pants_target` and `pants test {{selectionTargets}}`.

## Transport behavior

Native server support:

```json
{
  "tests": {
    "selection_targets": [
      {
        "target": "//src/api:test",
        "selector": { "tag": "bazel_label", "value": "//src/api:test" }
      }
    ]
  }
}
```

Compatibility mode:

```json
{
  "tests": { "files": [{ "path": "//src/api:test" }] },
  "selection_target": { "tag": "bazel_label", "value_from": "tests.files[].path" }
}
```

Compatibility transport must not mean file semantics. The server should still query selector timing and attribute results using `selector.tag/value`.

## Rollout rule

Use capability negotiation:

```text
server supports selection_targets => native transport
server lacks support => compatibility transport if enabled
native transport forced but unsupported => clear error
```

Do not silently switch payload shape without server support, and avoid fallback-after-error as the default strategy.

## Why this is the best path

```text
backend timing work is validated before public API work
users get the right API once we ship client support
backend can roll out native transport separately
same user config survives the migration
avoids documenting --files as the primary Bazel/Pants API
```

## Required client work

```text
config fields and CLI/env bindings
target-file parser
split-mode validation and conflict rules
custom runner mode that bypasses TEST_FILE_PATTERN
safe {{selectionTargets}} command expansion
target-aware split summary/debug output
retry behavior for attributed vs unattributed target failures
tests and docs
```

## Success criteria

```text
target-oriented public CLI
capability-controlled transport
selector tag/value semantics in both transports
ClickHouse selector timing lookup when data exists
clear fallback/default timing messaging
no claim of smart balancing when attribution is unavailable
```
