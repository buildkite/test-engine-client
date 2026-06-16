# Implementation Estimates

Rough estimates assume one experienced engineer or a small pair. Backend timing and attribution are the schedule risk, not the CLI.

## Summary

| Option | Client | Backend/ClickHouse | When to choose |
| --- | ---: | ---: | --- |
| Lo-fi | 1–3 days | 3–7 days minimum | short-lived workaround or spike |
| Middle-ground | 3–6 days | 1–3 weeks | recommended path |
| Full | 1–2 weeks | 3–6+ weeks | backend is ready and coordinated now |

All options require selector attribution and selector timing lookup to qualify as smart target splitting.

## Lo-fi

Shape:

```text
CLI: bktec run --files targets.txt
transport: tests.files[].path = selector.value
selector.tag: derived/configured elsewhere
```

Best case client work is mostly docs and validation polish. Backend work is only small if selector tag derivation, ClickHouse materialized view writes/queries, and unknown-target fallback already fit existing patterns.

Risk: bad public names (`--files`, `{{testExamples}}`) become sticky and target values pollute file timing history.

## Middle-ground

Shape:

```text
CLI: --split-by selection-target --selection-target-tag --selection-targets-file
command: {{selectionTargets}}
transport: native selection_targets when supported; compatibility files transport otherwise
```

Client work includes:

```text
config fields
CLI flags/env vars
target parser
placeholder expansion
validation/conflict rules
custom runner target mode
summary/debug wording
retry behavior
tests and docs
```

Backend work includes:

```text
selector tag/value attribution
ClickHouse MV write/query path
planner integration
unknown-target fallback
capability/compatibility behavior
summary/debug metadata
API tests
```

This is recommended because the backend timing model is validated first, then users get the right API while backend transport can evolve.

## Full

Shape:

```text
CLI and transport are target-native from day one
tests.selection_targets is the only supported target transport
```

Client work is higher than middle-ground because native schemas, capability behavior, public JSON stability, summaries, retries, and integration tests all need to land together.

Backend work is the largest because it includes new API schema, planner model, ClickHouse MV, result attribution, capabilities, rollout, and migration/backfill decisions.

## Recommended sequence

1. **Backend attribution/MV spike** (2–5 days): prove selector tag/value attribution for Go and determine what is possible for Bazel/Pants/custom multi-target commands.
2. **Client middle-ground API** (3–6 days): add `--split-by selection-target`, target-file parsing, `{{selectionTargets}}`, validation, tests, docs, and compatibility transport if needed.
3. **Backend planner integration** (1–3 weeks): selector timing lookup, unknown fallback, ClickHouse MV write/query path, capability behavior, and observability.
4. **Go opt-in rollout** (2–5 days after backend support): `selector.tag=package`, target discovery via `go list ./...`, explicit opt-in first.
5. **Bazel/Pants docs and polish** (2–4 days): document required result attribution and limitations.

## First spike questions

Before building the public CLI, answer these:

```text
Can the backend store timing for selector.tag/value?
Can the planner query selector timing by suite + runner + selector tag?
Can Go results produce package-level durations reliably?
What happens when one node runs multiple packages or targets?
What metadata would Bazel/Pants/custom results need for target attribution?
What fallback duration is used for unknown selector values?
Where does server capability negotiation live?
```

Decision tree:

```text
Can we attribute durations to selector.tag/value?
  yes => build selector MV, planner lookup, then client selection-target mode
  no  => client can only ship target dispatch with fallback/default timing
```

## Caveats that affect estimates

The middle-ground estimate assumes compatibility transport and no large expansion of `bktec plan --json`. If public JSON starts exposing assigned units, add schema-versioning and target-shaped output work.

Command construction is a real client task: current runners join paths/packages into a command string before shell splitting. Selection targets need argv-safe placeholder expansion or shell escaping.

Retry work depends on attribution. If a failed result cannot be mapped to an executable target, choose and document either retry-all-assigned-targets or skip target retries.

## Final recommendation

Do the middle-ground path, but start with the backend attribution/ClickHouse spike. Without that spike, the CLI can ship only as target dispatch with fallback/default timing.
