# API Contract

This contract must be true before `--split-by=selection-target` is a supported public API.

## Split modes

```sh
BUILDKITE_TEST_ENGINE_SPLIT_BY=file
BUILDKITE_TEST_ENGINE_SPLIT_BY=example
BUILDKITE_TEST_ENGINE_SPLIT_BY=selection-target
```

| Mode | Meaning |
| --- | --- |
| `file` | Plan filesystem paths. |
| `example` | Enable example-level splitting where supported; plans may contain files and examples. |
| `selection-target` | Plan runner-executable targets such as packages, Bazel labels, or Pants addresses. V1 should not intentionally mix files/examples with targets. |

Legacy alias:

```sh
BUILDKITE_TEST_ENGINE_SPLIT_BY_EXAMPLE=true # equivalent to split-by=example
```

## Precedence and validation

Rules:

```text
CLI flags override environment variables.
Conflicting explicit split modes are errors.
--split-by-example conflicts with explicit file or selection-target mode.
--selection-targets-file requires --split-by=selection-target.
--files and --selection-targets-file conflict in selection-target mode.
```

Custom runner validation:

| Mode | Required |
| --- | --- |
| `custom + file` | `TEST_CMD`, `TEST_FILE_PATTERN` |
| `custom + selection-target` | `TEST_CMD` containing `{{selectionTargets}}`, `SELECTION_TARGET_TAG`, `SELECTION_TARGETS_FILE`; no `TEST_FILE_PATTERN` |

## Target file format

`BUILDKITE_TEST_ENGINE_SELECTION_TARGETS_FILE` points to a UTF-8 text file.

Recommended parser behavior:

```text
one target per line
trim leading/trailing whitespace
ignore blank lines
ignore lines whose first non-whitespace character is #
do not support inline comments
do not shell-expand values
preserve first occurrence order
de-duplicate with a debug message
reject invalid UTF-8 or NUL bytes with line numbers
```

## Command placeholders

| Runner/mode | Placeholder |
| --- | --- |
| `custom + file` | existing `{{testExamples}}` behavior |
| `custom + selection-target` | `{{selectionTargets}}` |
| `gotest` | existing `{{packages}}` |

Selection-target placeholder contract:

```text
{{selectionTargets}} is required for custom selection-target mode.
It may appear once.
It expands to the assigned targets for this node.
Targets must become separate argv entries, not one quoted string.
Users should not quote {{selectionTargets}}.
Wrong or missing placeholders should produce clear validation errors.
```

Safety requirement: do not raw-substitute target-file contents into a shell command. Prefer argv-aware placeholder expansion; if string substitution remains, shell-escape each target before splitting.

Empty assignment behavior:

```text
no assigned targets + fail-on-no-tests=false => skip TEST_CMD and succeed
no assigned targets + fail-on-no-tests=true  => error
```

## Request transport

Preferred native shape:

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

Compatibility transport may temporarily send target values through `tests.files`, but must still carry or imply selector semantics:

```json
{
  "tests": { "files": [{ "path": "//src/api:test" }] },
  "selection_target": { "tag": "bazel_label", "value_from": "tests.files[].path" }
}
```

Use server capability negotiation to choose transport:

```text
server supports selection_targets => send native selection_targets
server lacks support => use compatibility transport if allowed
native transport forced but unsupported => fail clearly
```

Avoid “send native, then fallback after error” as the default. Ignored fields and partial acceptance are risky.

## Public plan output

`bktec plan --json` is public API, but today it only emits environment variables (`BUILDKITE_TEST_ENGINE_PLAN_IDENTIFIER`, `BUILDKITE_TEST_ENGINE_PARALLELISM`), not per-node tests.

If plan JSON is expanded to expose assigned units, selection-target mode should use a stable target-oriented shape from day one. Do not let internal transport (`tests.files` vs `tests.selection_targets`) leak into that public schema. If stability is uncertain, add `schema_version` and `unit_type`.

## Timing and result attribution

Smart target splitting requires timing keyed by:

```text
suite
runner
selector.tag
selector.value
```

Minimum ClickHouse materialized-view shape:

```text
suite_id
runner
selector_tag
selector_value
median_duration_ms
sample_count
last_seen_at
```

The planner must query this selector timing path for targets. Compatibility transport must not write target values into ordinary file timing history.

Uploaded results must be attributable to the same selector key. For Go this may be derivable from package-level result data. For Bazel/Pants/custom, result metadata or another mapping is needed when one node runs multiple targets.

If attribution is unavailable:

```text
run assigned targets
record/report selector timing unavailable
use default/unknown timing for planning
do not claim smart target-level balancing
```

Do not fake timing by evenly dividing node duration across assigned targets unless that becomes an explicit low-confidence product decision.

## Retry behavior

Retries must operate at a unit the runner can execute:

```text
attributed target failure => retry failed target(s)
unattributed target failure => choose and document either retry all assigned targets or skip target-level retry
gotest => retry failed package(s), preserving {{packages}}
```

Do not pass individual failed examples/testcases to a target-mode runner unless they are known executable targets.

## Go rollout guardrails

Do not flip `gotest` defaults based only on client version. Require server capability support, package attribution confidence, stable summary/debug output, and an opt-out such as:

```sh
BUILDKITE_TEST_ENGINE_SPLIT_BY=file
```
