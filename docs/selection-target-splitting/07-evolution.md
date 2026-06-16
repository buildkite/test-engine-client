# Evolution Plan

## Phase 0: backend attribution and timing spike

Before building the public client API, prove the backend model:

```text
selector.tag/value can be stored with result timing
the planner can query selector timing from a materialized view
unknown selector values have explicit fallback timing
Go package-level attribution is reliable enough for opt-in rollout
Bazel/Pants/custom attribution requirements are understood
```

If this is not true yet, any client work should be described as target dispatch, not smart target splitting.

## Phase 1: compatibility workaround

Allow target-style workflows through existing `--files` only as a short-term workaround.

```text
Use selector attribution if backend can derive/configure selector.tag.
Do not call it smart target splitting unless selector timing is written and queried.
Do not write target strings into ordinary file timing history.
```

## Phase 2: clean client API

Add:

```sh
--split-by selection-target
--selection-target-tag
--selection-targets-file
{{selectionTargets}}
```

Keep existing APIs:

```sh
--files
--split-by-example
BUILDKITE_TEST_ENGINE_FILES
BUILDKITE_TEST_ENGINE_SPLIT_BY_EXAMPLE
```

Initial implementation may use compatibility transport, but it must preserve selector semantics.

## Phase 3: backend target timing

Add native API and planner support for:

```json
{ "tests": { "selection_targets": [] } }
```

Add ClickHouse timing lookup by:

```text
suite + runner + selector.tag + selector.value
```

Unknown targets should use explicit fallback/default timing.

## Phase 4: result attribution

Associate uploaded results with the targets the planner assigns.

Possible mechanisms:

```text
derive package from Go results
require custom/Bazel/Pants result metadata
use assignment metadata when a node runs exactly one target
```

Do not claim reliable target-level timing for Bazel/Pants until multi-target commands can attribute results per target.

## Phase 5: Go default

After backend support is proven, make `gotest` default to:

```text
split mode: selection-target
selector tag: package
target discovery: go list ./...
```

Guardrails:

```text
server capability support
package-level result attribution confidence
target-aware summaries/debug output
opt-out: BUILDKITE_TEST_ENGINE_SPLIT_BY=file
```

## Observability

Split summaries/debug logs should state:

```text
12 selection targets across 4 nodes
selection-target transport: native selection_targets | files compatibility
target timing: selector history used | unavailable, using defaults
result attribution: available | unavailable
```

## Later, only if needed

Consider these only after real demand appears:

```text
separate executable target from selector.value
runner-native Bazel/Pants target discovery
direct examples-file input
target-level result attribution helpers
```

Do not deprecate `tests.files` or `tests.examples`; they remain correct for file/example-centric runners.
