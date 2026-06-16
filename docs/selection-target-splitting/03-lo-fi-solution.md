# Lo-fi Solution

Use today’s `--files` mechanism for target lists. This is a workaround, not the desired public API.

## User shape

Go already behaves like this internally: `gotest` discovers packages with `go list ./...` and sends them as `tests.files`.

Bazel/Pants can do the same with `custom`:

```yaml
commands:
  - bazel query 'kind(test, //...)' > bazel-targets.txt
  - bktec run --files bazel-targets.txt
env:
  BUILDKITE_TEST_ENGINE_TEST_RUNNER: custom
  BUILDKITE_TEST_ENGINE_TEST_FILE_PATTERN: unused-required-by-current-validation
  BUILDKITE_TEST_ENGINE_TEST_CMD: bazel test {{testExamples}}
```

Here `--files` means targets, and `{{testExamples}}` means assigned target strings.

## Required backend semantics

This only qualifies as smart target splitting if the backend interprets the file-shaped payload as selector timing input:

```text
tests.files[].path = selector.value
selector.tag = package / bazel_label / pants_target
```

The planner must query selector timing, not ordinary file timing. Target strings must not pollute file-history buckets.

If selector tag/value attribution is missing, this is only target dispatch.

## Pros

```text
smallest client change
usable immediately for experiments
can support early Go package-level timing if backend attribution exists
```

## Cons

```text
bad public names: --files and {{testExamples}}
custom still requires TEST_FILE_PATTERN
easy to ossify into the wrong API
still depends on backend selector attribution and ClickHouse lookup
```

## Use only when

```text
users need a short-term workaround
native or middle-ground CLI is not ready
backend can route target strings into selector timing, or docs clearly say timing is fallback/default only
```
