# Proposed Full Solution

This is the clean long-term API.

Original requirement:

```text
Support splitting by arbitrary selection target.
Build support for a group of tests via a selector tag, and a ClickHouse materialized view to support querying this data.
Goal: package-level splitting support.
```

The important architectural point: the CLI only names and submits targets. True smart splitting comes from result attribution plus a queryable ClickHouse timing materialized view keyed by selector tag/value.

## New concept: selection target

A **selection target** is a named group of tests the runner can execute.

| Tool | Selection target example |
| --- | --- |
| Go | `github.com/acme/repo/internal/api` |
| Bazel | `//src/api:test` |
| Pants | `src/python/api:tests` |

## New split mode

Add:

```sh
--split-by file
--split-by example
--split-by selection-target
```

Environment variable:

```sh
BUILDKITE_TEST_ENGINE_SPLIT_BY=file
BUILDKITE_TEST_ENGINE_SPLIT_BY=example
BUILDKITE_TEST_ENGINE_SPLIT_BY=selection-target
```

Semantics:

| Value | Meaning |
| --- | --- |
| `file` | Split discovered files. Current default for most runners. |
| `example` | Enable example-level splitting where supported. Final plans may contain both files and examples. |
| `selection-target` | Split arbitrary runner targets such as packages, Bazel labels, or Pants targets. |

In v1, `selection-target` mode should be exclusive. It should not intentionally mix files/examples with selection targets. Mixed target+example splitting can be a future feature.

Keep this compatibility flag:

```sh
--split-by-example
BUILDKITE_TEST_ENGINE_SPLIT_BY_EXAMPLE=true
```

Treat it as an alias for:

```sh
--split-by example
BUILDKITE_TEST_ENGINE_SPLIT_BY=example
```

## Target metadata

Selection targets need a selector tag so Test Engine knows which historical timing bucket to use.

```sh
--selection-target-tag package
```

Environment variable:

```sh
BUILDKITE_TEST_ENGINE_SELECTION_TARGET_TAG=package
```

Examples:

```text
package
bazel_label
pants_target
```

The selector tag is the timing namespace. Changing it creates separate history.

Recommended contract:

```text
selector.tag is scoped by suite + runner.
selector.tag is case-sensitive.
selector.value defaults to the target string supplied by the runner/client.
Use stable, canonical target strings to avoid fragmented timing history.
```

## Target list input

For tools like Bazel and Pants:

```sh
--selection-targets-file targets.txt
```

Environment variable:

```sh
BUILDKITE_TEST_ENGINE_SELECTION_TARGETS_FILE=targets.txt
```

Target file format:

```text
one target per line
empty lines ignored
lines starting with # ignored
no shell quoting
```

Example:

```text
//src/api:test
//src/auth:test
//src/billing:test
```

## Command placeholder

For custom selection-target runners, require:

```text
{{selectionTargets}}
```

Example:

```sh
export BUILDKITE_TEST_ENGINE_TEST_CMD='bazel test {{selectionTargets}}'
```

If this node receives two targets, bktec runs:

```sh
bazel test //src/api:test //src/auth:test
```

Recommendation: require the placeholder in selection-target mode. Do not silently append targets. Bazel/Pants command flags can be ordering-sensitive.

Placeholder expansion must be a public contract:

```text
{{selectionTargets}} expands to the assigned target list for this node.
Targets are passed as separate command arguments, not as one quoted string.
Users should not wrap {{selectionTargets}} in quotes.
The placeholder may appear once.
If no targets are assigned, bktec should skip TEST_CMD and exit successfully unless fail-on-no-tests is enabled.
```

If implementation requires string substitution before shell splitting, bktec must shell-escape each target before substitution. Do not substitute raw target-file contents into a shell command.

## Request shape

Add `selection_targets` beside existing `files` and `examples`:

```json
{
  "tests": {
    "files": [],
    "examples": [],
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

For v1:

```text
selector.value defaults to path
```

Avoid advanced mapping between executable path and selector value until there is a real need.

Naming note: `path` is reused from the existing `TestCase` shape for compatibility. In selection-target mode, it means “runner-executable target string”, not necessarily a filesystem path. If this becomes a public API independent of existing `TestCase`, prefer a clearer field name like `target`.

## Public plan output

`bktec plan --json` should not silently change shape when backend support rolls out.

Recommended contract:

```text
In selection-target mode, public client output should be target-oriented from day one.
Internal server transport may temporarily use tests.files during middle-ground rollout.
Do not expose backend transport details as the public plan JSON contract.
```

If we cannot keep the public output stable, add an explicit schema version or unit type.

## Go example

Eventually, Go should need no new config:

```yaml
steps:
  - label: ":golang: Go tests"
    command: bktec run
    parallelism: 4
    env:
      BUILDKITE_TEST_ENGINE_TEST_RUNNER: gotest
      BUILDKITE_TEST_ENGINE_SUITE_SLUG: my-suite
      BUILDKITE_TEST_ENGINE_RESULT_PATH: tmp/gotest-results.xml
```

The client discovers packages:

```sh
go list ./...
```

And sends:

```json
{
  "tests": {
    "selection_targets": [
      {
        "format": "selection_target",
        "path": "github.com/acme/repo/internal/api",
        "selector": {
          "tag": "package",
          "value": "github.com/acme/repo/internal/api"
        }
      }
    ]
  }
}
```

Rollout note: make this explicit opt-in first if backend support is not universal. Flip `gotest` to default selection-target mode only after server support is safely deployed.

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

## Validation rules

Recommended support matrix:

| Runner | `file` | `example` | `selection-target` |
| --- | --- | --- | --- |
| `gotest` | legacy/fallback | no | yes |
| `custom` | yes | no | yes, with target file |
| `rspec` | yes | yes | no initially |
| `pytest` | yes | yes | no initially |
| `playwright` | yes | yes | no initially |
| `jest` | yes | no | no initially |
| `cypress` | yes | no | no initially |
| `nunit` | yes | no | no initially |

Conflict rules:

```text
--split-by-example + --split-by=example
  OK

--split-by-example + --split-by=file
  Error

--split-by-example + --split-by=selection-target
  Error

--selection-targets-file without --split-by=selection-target
  Error

--files and --selection-targets-file together
  Error in selection-target mode
```

Custom runner rules:

```text
custom + split-by=file:
  requires TEST_CMD
  requires TEST_FILE_PATTERN

custom + split-by=selection-target:
  requires TEST_CMD containing {{selectionTargets}}
  requires SELECTION_TARGET_TAG
  requires SELECTION_TARGETS_FILE
  does not require TEST_FILE_PATTERN
```

Precedence rules:

```text
CLI flags override environment variables.
--split-by-example is a legacy alias for --split-by=example.
Conflicting explicit split modes are errors.
BUILDKITE_TEST_ENGINE_FILES and BUILDKITE_TEST_ENGINE_SELECTION_TARGETS_FILE conflict in selection-target mode.
--selection-targets-file implies target input, but should still require --split-by=selection-target for clarity.
```

Placeholder matrix:

| Runner/mode | Placeholder |
| --- | --- |
| `custom + file` | existing `{{testExamples}}` behavior |
| `custom + selection-target` | `{{selectionTargets}}` |
| `gotest` | existing `{{packages}}` remains supported |
| `gotest + selection-target backend` | user config should not need to change; `{{packages}}` still works |

## Retry behavior

Retries happen at the selected unit level unless the runner supports something more granular.

For selection targets:

```text
Go: retry the failed package
Bazel/Pants custom: retry the failed target
```

This should be called out in user docs because target-level retries are coarser than example-level retries.

If failures cannot be attributed to individual targets, choose and document one behavior before shipping:

```text
Option A: retry all targets assigned to the node
Option B: do not do target-level retry and explain attribution is missing
```

Do not silently claim target-level retry when the result data cannot identify the failed target.

## Result attribution and ClickHouse materialized-view requirement

Smart target splitting only works if Test Engine can associate historical results with the same selector tag/value.

This is the real unlock for the original requirement.

The ClickHouse materialized view should be queryable by something equivalent to:

```text
suite
runner
selector.tag
selector.value
```

and should expose enough timing data for planning, for example:

```text
median duration
sample count
last seen timestamp
fallback/default duration behavior
```

For Go, Test Engine may be able to derive package names from result data.

For custom Bazel/Pants usage, users may need collectors/results to include matching target metadata, for example:

```text
bazel_label=//src/api:test
```

This is especially important when a single command runs more than one target:

```sh
bazel test //src/api:test //src/auth:test
```

If the uploaded results do not identify which target produced which result/timing, the system cannot build accurate selector-level history. The planner can still distribute future targets using fallback timing, but it should not claim historical target balancing is active.

Do not silently approximate target timing by dividing node duration across assigned targets unless that approximation becomes an explicit product decision with a confidence marker. Incorrect attribution is worse than unknown timing because it teaches the ClickHouse materialized view bad data.

This is the most important server/client contract to clarify before shipping the full solution. The CLI can submit targets, but historical balancing improves only when future results can be attributed back to those targets.

If attribution is missing:

```text
tests should still run
the planner may fall back to default/unknown timing
docs must not promise smart target-level balancing
debug/summary output should say target timing was unavailable or unattributed
```

This requirement applies to every solution shape in these docs:

```text
lo-fi:
  may use tests.files transport, but must derive/configure selector.tag and use files[].path as selector.value

middle-ground:
  may use compatibility transport, but must carry selector.tag/value semantics and query the ClickHouse materialized view

full solution:
  sends selector tag/value explicitly in tests.selection_targets
```

Any solution that only distributes target strings without result attribution and ClickHouse materialized-view lookup is a dispatch workaround, not package-level smart splitting.
