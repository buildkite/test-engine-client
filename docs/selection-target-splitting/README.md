# Selection Target Splitting: Client CLI API

This is the short overview. Detailed notes live in the ordered subdocs linked below.

## Original requirement

> Support splitting by arbitrary selection target.
>
> Our current options for file or example based test splitting don’t work for package-centric languages like Go, or tools like Bazel or Pants. Build test splitting support for a group of tests via a selector tag, and a ClickHouse materialized view to support querying this data.
>
> Goal: build package-level splitting support.

## Why this exists

Today `bktec` understands two split units:

| Unit | Example | Works well for |
| --- | --- | --- |
| File | `spec/user_spec.rb`, `src/api.test.ts` | RSpec, Jest, Pytest, Cypress, Playwright |
| Example | `spec/user_spec.rb[1:1]`, `test_api.py::test_create` | RSpec/Pytest/Playwright slow-file splitting |

But some tools are not file-centric:

| Tool | Natural split unit |
| --- | --- |
| Go | package, e.g. `github.com/acme/repo/internal/api` |
| Bazel | target label, e.g. `//src/api:test` |
| Pants | target address, e.g. `src/python/api:tests` |

Those are all **selection targets**: named groups of tests that a runner can execute.

The current client can run these by treating them as `files`, but that is leaky. The server cannot cleanly distinguish “a file path” from “a package/target that should use target-level timing history”.

The real unlock is not just the CLI flag. The real unlock is that uploaded results must be attributable to:

```text
suite + runner + selector.tag + selector.value
```

That attribution feeds the ClickHouse materialized view the planner queries for historical target timing.

## Ordered docs

1. [Current world](01-current-world.md)
2. [API contract](02-api-contract.md)
3. [Lo-fi solution](03-lo-fi-solution.md)
4. [Middle-ground solution](04-middle-ground-solution.md)
5. [Full solution](05-full-solution.md)
6. [Implementation estimates and recommendation](06-implementation-estimates.md)
7. [Evolution plan](07-evolution.md)

## Current client API

Current important behavior:

```sh
bktec run
bktec plan --json
bktec run --files test-files.txt
bktec run --split-by-example
```

Environment variables:

```sh
BUILDKITE_TEST_ENGINE_FILES=test-files.txt
BUILDKITE_TEST_ENGINE_SPLIT_BY_EXAMPLE=true
```

Important nuance: `--split-by-example` is not exclusive. It allows mixed plans containing both files and examples.

## Proposed full solution

Add a first-class split mode:

```sh
BUILDKITE_TEST_ENGINE_SPLIT_BY=selection-target
BUILDKITE_TEST_ENGINE_SELECTION_TARGET_TAG=bazel_label
BUILDKITE_TEST_ENGINE_SELECTION_TARGETS_FILE=bazel-targets.txt
BUILDKITE_TEST_ENGINE_TEST_CMD='bazel test {{selectionTargets}}'
```

Conceptual payload:

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

This is the clean long-term model, but it requires backend support for target timing, result attribution, and a selector-keyed ClickHouse materialized view.

## Lo-fi solution

Use today’s `--files` mechanism for target lists:

```yaml
commands:
  - bazel query 'kind(test, //...)' > bazel-targets.txt
  - bktec run --files bazel-targets.txt
env:
  BUILDKITE_TEST_ENGINE_TEST_RUNNER: custom
  BUILDKITE_TEST_ENGINE_TEST_FILE_PATTERN: unused-required-by-current-validation
  BUILDKITE_TEST_ENGINE_TEST_CMD: bazel test {{testExamples}}
```

This can work quickly, but it is only valid if the backend can still map each submitted target string to `selector.tag`/`selector.value` and feed the ClickHouse materialized view. Otherwise it is just target dispatch, not smart target splitting.

## Middle-ground solution

Add the clean target-oriented CLI now, but initially allow the client to send targets through the existing `tests.files` payload until the backend supports `tests.selection_targets`. Even in this compatibility mode, selector attribution and ClickHouse materialized-view lookup are required.

This is the recommended implementation path.

## Estimated path forward

Read: [implementation estimates and recommendation](06-implementation-estimates.md)

Summary:

| Option | Client work | Backend/ClickHouse work | Total shape | Recommendation |
| --- | ---: | ---: | --- | --- |
| Lo-fi | 1–3 days | 3–7 days if attribution/MV is simple | ~1–2 weeks | Fastest, awkward API |
| Middle-ground | 3–6 days | 1–3 weeks | ~2–4 weeks | Best practical path |
| Full solution | 1–2 weeks | 3–6+ weeks | ~4–8 weeks | Cleanest, highest coordination |

## Recommendation

Use the **middle-ground solution**.

It gives users a clear API now:

```sh
--split-by selection-target
--selection-target-tag bazel_label
--selection-targets-file bazel-targets.txt
```

while letting backend target timing support roll out independently.

Do not remove or break existing APIs:

```sh
--files
--split-by-example
BUILDKITE_TEST_ENGINE_FILES
BUILDKITE_TEST_ENGINE_SPLIT_BY_EXAMPLE
```

## Final viability check

All three approaches can work, but only under different constraints:

| Approach | Works as package/target splitting? | Biggest caveat |
| --- | --- | --- |
| Lo-fi | Yes, as a short-term workaround | Must still derive `selector.tag` and must not pollute normal file timing history. |
| Middle-ground | Yes, recommended | Requires a backend compatibility contract so target strings sent through `tests.files` are treated as selector values. |
| Full | Yes, best end state | Requires coordinated client, API, planner, result attribution, and ClickHouse MV support. |

The non-negotiable check for every approach is result attribution. If a node runs many Bazel/Pants targets but uploaded results cannot identify which target produced which timing, Test Engine cannot build reliable per-target history. In that case the command can still run, but smart target balancing must fall back to default timing until target metadata is available.
