# Implementation Estimates and Recommendation

These are rough planning estimates for the three solution shapes. They assume one experienced engineer or a small pair, and that existing backend/Test Engine patterns are reasonably easy to extend.

The estimates intentionally separate client work from backend/ClickHouse work because the hard part is not the CLI. The hard part is selector attribution plus the ClickHouse materialized view queried by the planner.

## Summary

| Option | Client work | Backend/ClickHouse work | Total shape | Recommendation |
| --- | ---: | ---: | --- | --- |
| Lo-fi | 1–3 days | 3–7 days if attribution/MV is simple | ~1–2 weeks | Fastest, awkward API |
| Middle-ground | 3–6 days | 1–3 weeks | ~2–4 weeks | Best practical path |
| Full solution | 1–2 weeks | 3–6+ weeks | ~4–8 weeks | Cleanest, highest coordination |

All options must preserve selector attribution and feed the ClickHouse materialized view. Without that, the work is only target dispatch and does not satisfy the original package-level splitting requirement.

## Lo-fi estimate

The lo-fi path uses today’s `--files` mechanism for target lists, while the backend interprets each submitted path as a selector value under a configured or derived selector tag.

Example:

```sh
bktec run --files bazel-targets.txt
```

Conceptually:

```text
tests.files[].path = selector.value
selector.tag = package / bazel_label / pants_target
```

### Estimated effort

Client:

```text
1–3 days
```

Mostly docs, validation tweaks, and maybe runner-specific polish.

Backend / ClickHouse:

```text
3–7 days minimum
```

Only this small if there is already a straightforward way to:

```text
derive or configure selector.tag
map tests.files[].path to selector.value
write/query the ClickHouse materialized view
apply unknown-target fallback timing
```

If attribution is not already clear, this becomes larger.

### Pros

```text
Fastest path
Smallest client change
Good enough for early Go package-level experiments
```

### Cons

```text
Bad names: --files means targets
Bad placeholder: {{testExamples}} means targets
Harder to explain
Easy to ossify into a bad API
Still needs selector attribution and ClickHouse MV support
```

### Use only if

```text
We need a proof of concept immediately.
We can keep it behind internal docs/config.
We do not present it as the final public API.
The backend can still feed/query selector timing in ClickHouse.
```

## Middle-ground estimate

The middle-ground path adds the clean CLI now:

```sh
--split-by selection-target
--selection-target-tag bazel_label
--selection-targets-file bazel-targets.txt
{{selectionTargets}}
```

But it allows backend compatibility transport initially:

```json
{
  "tests": {
    "files": [
      { "path": "//src/api:test" }
    ]
  },
  "selection_target": {
    "tag": "bazel_label",
    "value_from": "tests.files[].path"
  }
}
```

Later, the same CLI can move to native `selection_targets` transport:

```json
{
  "tests": {
    "selection_targets": [
      {
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

### Estimated effort

Client:

```text
3–6 days
```

Includes:

```text
config fields
CLI flags/env vars
target file parsing
{{selectionTargets}}
validation/conflict rules
split summary wording
tests
docs
```

Backend / ClickHouse:

```text
1–3 weeks
```

Includes:

```text
selector tag/value attribution
ClickHouse MV write/query path
planner query integration
default/fallback timing for unknown targets
server capability or compatibility behavior
API tests
debug/summary metadata
```

### Pros

```text
Good user-facing API now
Can ship before native selection_targets transport is complete
Avoids teaching users the --files workaround
Clean path to the full solution
Can still feed the ClickHouse MV during compatibility transport
```

### Cons

```text
Slightly more client work
Temporary internal mismatch between CLI and transport
Needs clear debug output so users know if native target timing is active
```

### Recommendation

Choose this path.

It balances:

```text
good API
reasonable implementation size
safe rollout
backend flexibility
future compatibility
```

## Full solution estimate

The full solution implements the clean end-state immediately:

```sh
BUILDKITE_TEST_ENGINE_SPLIT_BY=selection-target
BUILDKITE_TEST_ENGINE_SELECTION_TARGET_TAG=bazel_label
BUILDKITE_TEST_ENGINE_SELECTION_TARGETS_FILE=bazel-targets.txt
BUILDKITE_TEST_ENGINE_TEST_CMD='bazel test {{selectionTargets}}'
```

Native API payload:

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

### Estimated effort

Client:

```text
1–2 weeks
```

More than middle-ground because there is no compatibility shortcut and integration testing needs to be broader.

Backend / ClickHouse:

```text
3–6+ weeks
```

Potentially more if result attribution is hard.

Includes:

```text
new request schema
new planner input model
ClickHouse MV creation/query path
MV refresh/backfill behavior
attribution from uploaded results
capability handling
response shape
retries
observability
rollout and migration
tests
```

### Pros

```text
Cleanest architecture
Least long-term ambiguity
Best API story
```

### Cons

```text
Largest coordination cost
Higher risk
May block client work on backend readiness
Harder to incrementally validate
```

### Use only if

```text
Backend team is ready now.
Attribution design is already clear.
We can afford a longer coordinated release.
```

## Best path forward

Use the **middle-ground solution**.

The real hard part is:

```text
selector attribution + ClickHouse MV + planner query
```

The highest schedule risk is target-level result attribution, especially for Bazel/Pants/custom workflows that run multiple targets in one command. If result uploads cannot identify the target that produced each timing, the client API can still ship, but smart target balancing must stay in default/unknown-timing mode for those workflows.

The middle-ground path lets us:

1. Give users the right CLI shape now.
2. Avoid baking in the awkward `--files` workaround.
3. Keep existing APIs working.
4. Roll out backend support safely.
5. Flip Go to package-level default later, when attribution and the ClickHouse MV are proven.

## Suggested implementation plan

### Phase 1: backend attribution and ClickHouse MV spike

Estimate:

```text
2–5 days
```

Goal:

```text
Can we reliably produce selector.tag/value for Go package results?
Can we do the same for Bazel/Pants/custom?
What is the ClickHouse MV schema?
How will the planner query it?
What happens for unknown targets?
```

Deliverable:

```text
A concrete backend contract for selector timing.
```

Do this first because it determines whether the CLI is truly smart splitting or just target dispatch.

### Phase 2: client middle-ground API

Estimate:

```text
3–6 days
```

Add:

```sh
--split-by selection-target
--selection-target-tag
--selection-targets-file
{{selectionTargets}}
```

With validation, tests, docs, and compatibility transport if needed.

### Phase 3: backend planner integration

Estimate:

```text
1–3 weeks
```

Add:

```text
selector timing lookup
unknown target fallback
split summary metadata
result attribution
ClickHouse MV refresh/query path
capability handling
```

### Phase 4: Go package rollout

Estimate:

```text
2–5 days after backend support is ready
```

Use:

```text
selector.tag = package
selector.value = package path
```

Start explicit opt-in first:

```sh
BUILDKITE_TEST_ENGINE_SPLIT_BY=selection-target
```

Then later make it the `gotest` default once attribution, ClickHouse MV timing, and observability are proven.

### Phase 5: Bazel/Pants docs and polish

Estimate:

```text
2–4 days
```

Add examples, caveats, and required result attribution instructions.

## Concrete recommendation

Do this:

```text
1. Start with a backend attribution/ClickHouse MV spike.
2. Implement the middle-ground CLI.
3. Use compatibility transport only if needed, but preserve selector tag/value semantics.
4. Ship Go package splitting behind explicit opt-in.
5. Make Go default later.
```

Do not make `--files` the public recommended API for Bazel/Pants.

Do not flip Go defaults until the ClickHouse MV and attribution path are proven.

This gives the shortest safe path to package-level splitting without locking in a bad user API.
