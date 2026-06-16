# API Contract

This is the stricter contract that should be true before `--split-by=selection-target` is treated as a supported public API.

## Scope

This is a client/API contract, not an implementation plan.

It covers:

```text
CLI flags and env vars
target-file parsing
command placeholder behavior
plan JSON behavior
server capability behavior
result attribution requirements
retry behavior
```

## Split modes

```sh
BUILDKITE_TEST_ENGINE_SPLIT_BY=file
BUILDKITE_TEST_ENGINE_SPLIT_BY=example
BUILDKITE_TEST_ENGINE_SPLIT_BY=selection-target
```

Semantics:

| Mode | Contract |
| --- | --- |
| `file` | Use file paths as the planning unit. |
| `example` | Enable example-level splitting where supported. Plans may contain both files and examples. |
| `selection-target` | Use runner targets as the planning unit. V1 should not intentionally mix files/examples with selection targets. |

Legacy alias:

```sh
BUILDKITE_TEST_ENGINE_SPLIT_BY_EXAMPLE=true
```

means:

```sh
BUILDKITE_TEST_ENGINE_SPLIT_BY=example
```

## Precedence and conflicts

Rules:

```text
CLI flags override environment variables.
Conflicting explicit split modes are errors.
Legacy split-by-example conflicts with explicit file or selection-target mode.
```

Examples:

```text
--split-by-example + --split-by=example
  OK

--split-by-example + --split-by=file
  Error

--split-by-example + --split-by=selection-target
  Error

BUILDKITE_TEST_ENGINE_SPLIT_BY_EXAMPLE=true + BUILDKITE_TEST_ENGINE_SPLIT_BY=selection-target
  Error unless an explicit CLI flag resolves the conflict
```

Input conflicts:

```text
--selection-targets-file without --split-by=selection-target
  Error

--files and --selection-targets-file together in selection-target mode
  Error
```

## Target file format

`BUILDKITE_TEST_ENGINE_SELECTION_TARGETS_FILE` points to a UTF-8 text file.

Rules:

```text
one target per line
leading/trailing whitespace is trimmed
blank lines are ignored
lines whose first non-whitespace character is # are ignored
inline comments are not supported
values are not shell-expanded
order is preserved
duplicate handling must be chosen before implementation; prefer de-duplicate with a debug message
invalid lines should report line numbers
```

Example:

```text
# API tests
//src/api:test

# Auth tests
//src/auth:test
```

## Command placeholders

Placeholder support:

| Runner/mode | Placeholder |
| --- | --- |
| `custom + file` | existing `{{testExamples}}` behavior |
| `custom + selection-target` | `{{selectionTargets}}` |
| `gotest` | existing `{{packages}}` |

Selection-target placeholder contract:

```text
{{selectionTargets}} is required for custom selection-target mode.
It expands to the assigned targets for this node.
Targets must be passed as separate command arguments.
Users should not quote {{selectionTargets}}.
The placeholder may appear once.
Wrong placeholders should produce clear validation errors.
```

Empty assignment behavior:

```text
If no targets are assigned, skip TEST_CMD and exit successfully.
If fail-on-no-tests is enabled, exit with an error.
```

Safety requirement:

```text
Do not substitute raw target-file contents into a shell command.
If implementation uses string substitution, shell-escape each target before substitution.
Prefer argv construction where each target is a separate argument.
```

## Public plan output

`bktec plan --json` is public API.

Contract:

```text
Selection-target mode should expose target-oriented plan data from day one.
Internal server transport may use tests.files during middle-ground rollout.
Do not let backend transport changes silently alter public JSON shape.
```

If a stable target-oriented shape is not possible immediately, add:

```text
schema_version
unit_type
```

or clearly document that the JSON shape is experimental.

## Server capability behavior

Use server capability negotiation as the primary rollout mechanism.

Recommended behavior:

```text
If server supports selection_targets:
  send tests.selection_targets

If server does not support selection_targets:
  middle-ground mode may send target strings through tests.files

If user explicitly forces native selection_targets transport and server lacks support:
  fail clearly
```

Avoid “send and fallback after error” as the default strategy. Ignored fields or partial acceptance are risky.

## Result attribution and ClickHouse materialized view

The original requirement is not satisfied by the CLI alone.

Smart target splitting requires a timing query path keyed by:

```text
suite
runner
selector.tag
selector.value
```

Uploaded results must be attributable to the same selector tag/value. That attribution feeds the ClickHouse materialized view the planner queries.

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

If attribution is missing:

```text
tests still run
target timing is unknown/defaulted
target-level balancing is not guaranteed
debug/summary output should explain this
```

For Go, package attribution may be derived from result data.

For Bazel/Pants/custom, either document required result metadata or clearly mark smart target timing as not yet supported.

This requirement applies to all solution levels:

| Solution | Transport may be | Attribution/ClickHouse MV requirement |
| --- | --- | --- |
| Lo-fi | `tests.files` | Backend must derive/configure `selector.tag`; `tests.files[].path` is `selector.value`. |
| Middle-ground | `tests.files` or `tests.selection_targets` | Client/server must preserve selector tag/value semantics and query the ClickHouse MV. |
| Full | `tests.selection_targets` | Each target explicitly carries selector tag/value. |

If a proposed solution cannot produce selector attribution keys, it is only target dispatch and should not be considered an implementation of package-level smart splitting.

### Attribution granularity

The system must be able to attribute duration to the same unit it plans.

For Go, that likely means package-level result attribution:

```text
selector.tag = package
selector.value = github.com/acme/repo/internal/api
duration = duration for that package test run
```

For Bazel/Pants/custom, this is the riskier part. If one node runs multiple targets:

```sh
bazel test //src/api:test //src/auth:test
```

then uploaded results need target metadata such as:

```text
selector.tag = bazel_label
selector.value = //src/api:test
```

for each target's result/timing. If the result upload only says “this node took 120 seconds” and does not identify target-level timing, the ClickHouse MV cannot learn reliable per-target durations.

Acceptable v1 behavior if target-level attribution is missing:

```text
run the assigned targets
record that selector timing was unavailable
use default/unknown timing for future planning
do not claim smart target-level balancing for that runner/configuration
```

Do not fake selector timing by evenly dividing node duration across assigned targets unless that approximation is explicitly designed, documented, and marked as low confidence.

## Retry behavior

Selection-target retry should retry at target granularity only when failures can be attributed to targets.

Contract to choose before shipping:

```text
custom + selection-target + attributed failure:
  retry failed target(s) using {{selectionTargets}}

custom + selection-target + unattributed failure:
  either retry all targets assigned to the node, or skip target-level retry with a clear explanation

gotest:
  retry failed package(s), preserving existing {{packages}} command behavior
```

## Go rollout guardrails

Do not flip `gotest` defaults based only on client version.

Require:

```text
server capability support
result attribution confidence
stable plan output
observability in split summary/debug logs
an opt-out path, e.g. BUILDKITE_TEST_ENGINE_SPLIT_BY=file
```
