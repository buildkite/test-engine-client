# Selection Target Splitting

## Problem

`bktec` currently plans two units:

| Unit | Example | Good for |
| --- | --- | --- |
| File | `spec/user_spec.rb` | RSpec, Jest, Pytest, Cypress, Playwright |
| Example | `spec/user_spec.rb[1:1]` | slow-file splitting for supported runners |

Some runners are target-centric instead:

| Tool | Natural planning unit |
| --- | --- |
| Go | package, e.g. `github.com/acme/repo/internal/api` |
| Bazel | label, e.g. `//src/api:test` |
| Pants | address, e.g. `src/python/api:tests` |

Today those values can be squeezed through `tests.files`, but that hides their meaning and makes timing history ambiguous.

## Required outcome

Support splitting by arbitrary **selection targets**: named groups of tests a runner can execute. Smart target splitting requires historical timing keyed by:

```text
suite + runner + selector.tag + selector.value
```

The CLI can dispatch targets without this, but it cannot provide reliable target-level balancing unless uploaded results are attributable to the same selector keys and the planner can query a selector-keyed ClickHouse materialized view.

## Recommendation

Use the **middle-ground solution**:

1. First prove the backend can attribute and query selector-level timing:

   ```text
   suite + runner + selector.tag + selector.value
   ```

2. Then add the clean user API:

   ```sh
   BUILDKITE_TEST_ENGINE_SPLIT_BY=selection-target
   BUILDKITE_TEST_ENGINE_SELECTION_TARGET_TAG=bazel_label
   BUILDKITE_TEST_ENGINE_SELECTION_TARGETS_FILE=bazel-targets.txt
   BUILDKITE_TEST_ENGINE_TEST_CMD='bazel test {{selectionTargets}}'
   ```

3. Initially send target values through legacy `tests.files` only when the server does not support native `tests.selection_targets`.
4. Preserve selector tag/value semantics in both transports.
5. Move to native `tests.selection_targets` through server capability negotiation.

Do **not** make `--files` the recommended long-term Bazel/Pants API, and do **not** flip Go defaults until backend attribution, timing lookup, and rollout observability are proven.

If the backend spike shows selector attribution is not ready, the client can still dispatch targets, but that should be described as target dispatch with fallback/default timing — not smart target splitting.

## Docs

1. [Current world](01-current-world.md)
2. [API contract](02-api-contract.md)
3. [Lo-fi solution](03-lo-fi-solution.md)
4. [Middle-ground solution](04-middle-ground-solution.md)
5. [Full solution](05-full-solution.md)
6. [Implementation estimates](06-implementation-estimates.md)
7. [Evolution plan](07-evolution.md)
8. [Team review notes](08-developer-review-rfc.md)

## Option summary

| Option | Shape | Use when | Caveat |
| --- | --- | --- | --- |
| Lo-fi | Use `--files` for targets | immediate workaround | awkward names; must not pollute file timing history |
| Middle-ground | Clean CLI, compatibility transport if needed | recommended path | requires selector semantics during legacy transport |
| Full | Native `tests.selection_targets` from day one | backend is ready now | largest coordinated change |

If selector attribution is missing, call the feature target dispatch, not smart target splitting.
