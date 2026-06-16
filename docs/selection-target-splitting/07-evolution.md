# Evolution Plan

## Phase 1: documented compatibility path

Support target-style workflows using existing mechanics, but document them honestly as a workaround.

```text
Use --files for target lists if needed today.
Use selector attribution if the backend can derive/configure selector.tag.
Do not call it smart target splitting unless it feeds the ClickHouse materialized view.
```

## Phase 2: clean client API

Add:

```sh
--split-by
--selection-target-tag
--selection-targets-file
{{selectionTargets}}
```

Keep existing APIs working:

```sh
--files
--split-by-example
BUILDKITE_TEST_ENGINE_FILES
BUILDKITE_TEST_ENGINE_SPLIT_BY_EXAMPLE
```

Initial implementation can still send `tests.files` if backend support for `tests.selection_targets` is not ready, but it must still preserve selector tag/value semantics for ClickHouse materialized-view lookup.

## Phase 3: backend target timing

Add server support for:

```json
{
  "tests": {
    "selection_targets": []
  }
}
```

Add historical timing lookup by:

```text
suite
runner
selector.tag
selector.value
```

This is where target splitting becomes smart instead of only syntactically cleaner.

The ClickHouse materialized view is the core backend artifact for the original requirement. It should let the planner quickly answer:

```text
For this suite/runner/selector tag, what historical duration do we have for each selector value?
```

## Phase 4: result attribution

Make sure uploaded results can be associated with selection targets.

Possible approaches:

```text
derive package from Go result data
require custom collectors to emit target tags
use plan assignment metadata to associate node results with assigned targets
```

This must be solved before claiming target-level timing is reliable for Bazel/Pants.

## Phase 5: make Go automatic

After backend support is safe, change `gotest` defaults:

```text
split mode: selection-target
selector tag: package
target discovery: go list ./...
```

Existing Go users should not need to change pipeline config.

Guardrails:

```text
do not flip based only on client version
require server capability support
require package-level result attribution confidence
preserve existing {{packages}} commands
provide an opt-out, e.g. BUILDKITE_TEST_ENGINE_SPLIT_BY=file
```

## Observability

During rollout, users need to see which behavior they are getting.

Split summary/debug output should say things like:

```text
12 selection targets across 4 nodes
selection-target transport: native selection_targets
selection-target transport: legacy files compatibility mode
target timing: available from selector ClickHouse materialized view
target timing: unavailable; using defaults
```

## Phase 6: advanced cases only when needed

Avoid adding advanced options too early. Later, if real users need them, consider:

```text
separate selector.value from executable path
runner-native target discovery for Bazel/Pants
direct examples file input
target-level result attribution helpers
```

## Deprecation guidance

Do not remove anything in the first release.

Keep:

```sh
--files
--split-by-example
```

If `--split-by` is adopted broadly, later soft-deprecate `--split-by-example` in docs as an alias for:

```sh
--split-by example
```

Do not deprecate `tests.files` or `tests.examples`; they remain correct concepts for many runners.
