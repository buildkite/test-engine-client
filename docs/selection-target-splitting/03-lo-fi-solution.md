# Lo-fi Solution: Quick and Works

This is the fastest path. It mostly uses today’s `--files` mechanism for target lists.

This is a stopgap workaround, not the recommended long-term API. Use it only when target-list splitting is needed before first-class selection-target support exists.

Important: this is only a valid solution if it still produces selector attribution and feeds the ClickHouse materialized view. A lo-fi approach that merely dispatches target strings without selector timing attribution does **not** satisfy the original requirement.

## User API

Go already works this way implicitly:

```sh
export BUILDKITE_TEST_ENGINE_TEST_RUNNER=gotest
export BUILDKITE_TEST_ENGINE_RESULT_PATH=tmp/gotest-results.xml

bktec run
```

Bazel/Pants can use `custom` plus `--files`:

```yaml
steps:
  - label: ":bazel: Bazel tests"
    commands:
      - bazel query 'kind(test, //...)' > bazel-targets.txt
      - bktec run --files bazel-targets.txt
    parallelism: 4
    env:
      BUILDKITE_TEST_ENGINE_TEST_RUNNER: custom
      BUILDKITE_TEST_ENGINE_TEST_FILE_PATTERN: unused-required-by-current-validation
      BUILDKITE_TEST_ENGINE_TEST_CMD: bazel test {{testExamples}}
```

In this approach, `{{testExamples}}` really means “the assigned paths”, even though those paths are Bazel targets.

The `TEST_FILE_PATTERN` placeholder value is a sign this workaround is fighting the current file-oriented API.

## Attribution and ClickHouse materialized-view requirement

Even in the lo-fi solution, every target must map to the same timing key shape used by the ClickHouse materialized view:

```text
suite
runner
selector.tag
selector.value
```

Because the request is still file-shaped, attribution must come from one of these minimal mechanisms:

```text
gotest:
  selector.tag = package
  selector.value = discovered Go package path

custom Bazel/Pants:
  selector.tag comes from suite/server configuration or a minimal client setting
  selector.value = each line from the --files target list
```

If the backend cannot derive or receive `selector.tag`, the lo-fi solution should be treated as target dispatch only, not smart target splitting.

The ClickHouse materialized view can still work with file-shaped transport if the planner interprets each submitted `tests.files[].path` as `selector.value` under the configured `selector.tag`.

Conceptually:

```json
{
  "transport": "tests.files",
  "selector": {
    "tag": "bazel_label",
    "value_from": "tests.files[].path"
  }
}
```

## What this buys us

```text
Very small client change, maybe none.
Users can split target lists today.
Fallback splitting works.
Can feed the selector timing ClickHouse materialized view if selector.tag is derived or configured.
```

## What is bad about it

```text
The API says files, but users pass targets.
The placeholder says testExamples, but users pass targets.
The custom runner still requires TEST_FILE_PATTERN even though target mode does not need it.
Server transport still sees file-shaped data.
Docs have to explain an awkward workaround.
Requires a derivation/configuration path for selector.tag or it does not satisfy the requirement.
```

Do not let this write target timings into ordinary file-history buckets. The backend must either route these file-shaped target strings into selector timing history or treat them as unknown/default timing. Otherwise this workaround pollutes file timing data and makes a later migration harder.

## Final sanity check

This works only if all of these are true:

```text
each line in --files is a runner-executable target
the backend knows selector.tag for that target list
tests.files[].path is interpreted as selector.value
uploaded results can be attributed back to selector values, or timing is clearly marked unavailable
the planner queries the selector ClickHouse materialized view rather than normal file timing history
```

If any of those are false, the lo-fi path is still useful for dispatching targets across nodes, but it is not the original package-level smart-splitting feature.

## When to use it

Use this only as a stopgap when:

```text
users need something immediately
native selection_targets transport is not ready
the backend can still attribute file-shaped target strings to selector tag/value keys
```

Do not make this the long-term product API.
