# Selection Target Splitting: Team Review Notes

This is the short version for folks to react to before we commit to an implementation path.

## What we need to decide

Should Test Engine add a first-class **selection target** split mode? If yes, should we validate the backend timing model first, then ship the clean CLI with compatibility transport if native support is not ready everywhere?

My vote: **yes — use the middle-ground path**.

But the first step should be backend validation, not CLI work. The original ask is about selector-tagged groups plus a materialized view. If we cannot attribute and query selector-level timing, the CLI can only do target dispatch.

## Why this is coming up

Today `bktec` thinks in files and examples. That works well for file-centric tools, but it gets weird for target-centric systems:

| Tool | Natural unit |
| --- | --- |
| Go | package |
| Bazel | target label |
| Pants | target address |

Go already exposes the problem: the client discovers packages but sends them through `tests.files`. Bazel/Pants can do the same with `--files`, but then the API cannot tell whether a value is a real file, package, label, or address.

The important bit: this is not just about running targets. Smart splitting only works if historical timing is keyed by:

```text
suite + runner + selector.tag + selector.value
```

If uploaded results cannot be attributed to those same selector keys, we can still run targets, but we have to use default/unknown timing.

## Suggested direction

Add the API we actually want users to keep:

```sh
BUILDKITE_TEST_ENGINE_SPLIT_BY=selection-target
BUILDKITE_TEST_ENGINE_SELECTION_TARGET_TAG=bazel_label
BUILDKITE_TEST_ENGINE_SELECTION_TARGETS_FILE=bazel-targets.txt
BUILDKITE_TEST_ENGINE_TEST_CMD='bazel test {{selectionTargets}}'
```

Then choose transport based on what the server supports:

```text
server supports selection_targets => send native tests.selection_targets
server lacks support => send compatibility tests.files with selector semantics
native forced but unsupported => fail clearly
```

Even in compatibility mode, target values need selector semantics. We should not write Bazel labels, Pants addresses, or Go packages into normal file timing history.

## Options

| Option | Summary | Pros | Cons |
| --- | --- | --- | --- |
| Lo-fi | Document `--files` for targets | fastest, minimal client work | bad API names, easy to ossify, still needs backend attribution |
| Middle-ground | Clean CLI now, compatibility transport if needed | best user API, safe rollout, backend can evolve | temporary internal mismatch, needs capability/compatibility contract |
| Full | Native target model everywhere now | cleanest architecture | highest coordination cost, blocks on backend readiness |

## Why I like the middle-ground path

It gives users the API we want to keep without blocking on native backend transport everywhere. It also avoids teaching Bazel/Pants users the `--files` workaround as the “real” solution.

```text
User API stays stable:
  --split-by selection-target
  --selection-target-tag
  --selection-targets-file
  {{selectionTargets}}

Transport can evolve:
  tests.files compatibility -> tests.selection_targets native
```

## Things we should not hand-wave

1. **Attribution is the real feature.** Without selector-tagged results, this is target dispatch with default timing.
2. **Command expansion needs to be safe.** `{{selectionTargets}}` should become separate argv entries. If we do string substitution, targets need shell escaping.
3. **Retries need an explicit rule.** In target mode, retry target units — not random failed examples — unless those examples are actually valid runner targets.
4. **`plan --json` is currently small.** It emits plan identifier and parallelism, not assigned tests. If we expose assigned units later, use a target-shaped/versioned schema.
5. **Do not flip Go by accident.** Make Go opt-in first, then default only after backend capability, package attribution, summaries, and opt-out are proven.

## A reasonable rollout

1. Spike backend attribution + ClickHouse lookup for Go, and figure out what Bazel/Pants/custom need.
2. Add the client middle-ground API: split mode, target-file parser, `{{selectionTargets}}`, validation, summaries, tests.
3. Add backend planner integration: selector timing lookup, fallback timing, capabilities, summary/debug metadata.
4. Ship Go behind opt-in: `selector.tag=package`, package targets from `go list ./...`.
5. Make Go default later, once capability and attribution are proven.

Decision tree:

```text
Can we attribute durations to selector.tag/value?
  yes => build selector MV + planner lookup, then ship selection-target mode
  no  => optionally ship target dispatch, but say timing is fallback/default
```

## What I want feedback on

The big questions:

1. Can we produce reliable package-level timing for Go?
2. Are we comfortable exposing the clean CLI before native `tests.selection_targets` exists everywhere?
3. How should capability negotiation work: endpoint, response field, API version, or explicit config gate?
4. What metadata can we realistically expect from Bazel/Pants/custom results?
5. If a target failure is unattributed, should retry rerun all assigned targets or skip target-level retry and explain why?
6. Should the native API field be `target`, or should we reuse `path` for compatibility?
7. Should this start with `custom`, `gotest`, or both behind opt-in?

## Open questions

| Question | Why it matters |
| --- | --- |
| How does the backend learn `selector.tag` during compatibility transport? | Required to query the right timing namespace. |
| Can Go result uploads produce package-level duration reliably? | Required before making Go target splitting smart/default. |
| Can Bazel/Pants result uploads include target labels for each result? | Required for multi-target command attribution. |
| Where should capability negotiation live? | Determines safe rollout and fallback behavior. |
| How should target timing availability appear in split summaries? | Users need to know whether balancing used history or defaults. |

## Not trying to solve in v1

```text
runner-native Bazel/Pants target discovery
mixing selection targets with example splitting
separate executable target from selector.value
deprecating files/examples
claiming smart target balancing without attribution
```

## My recommendation

Go with the middle-ground path, but start with the backend attribution/ClickHouse spike. If attribution is not ready, we can still ship the CLI as target dispatch — just be honest in summaries/docs that timing is default/fallback, not smart target balancing yet.
