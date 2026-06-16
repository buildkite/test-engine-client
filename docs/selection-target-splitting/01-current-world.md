# Current World

## Client model

`bktec` currently plans and runs two public units:

| Unit | Example | Notes |
| --- | --- | --- |
| File | `spec/user_spec.rb`, `src/api.test.ts` | default model for most runners |
| Example | `spec/user_spec.rb[1:1]`, `test_api.py::test_create` | enabled by `--split-by-example`; plans may still include files |

Important APIs:

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

## Current request shape

File-only plans send:

```json
{ "tests": { "files": [{ "path": "src/api.test.ts" }] } }
```

Example-capable runners can send mixed plans:

```json
{
  "tests": {
    "files": [{ "path": "spec/fast_spec.rb" }],
    "examples": [{ "path": "spec/slow_spec.rb[1:1]", "format": "example" }]
  }
}
```

## Existing target-shaped behavior

Go already returns packages from `GetFiles()` and sends them as `tests.files`:

```json
{ "tests": { "files": [{ "path": "github.com/acme/repo/internal/api" }] } }
```

This works for execution because the Go runner treats `path` as a package. It is not a clean timing model because a package is not a file.

The same workaround can dispatch Bazel/Pants targets via `--files`, but the server cannot tell whether `tests.files[].path` is a real file, Go package, Bazel label, or Pants address unless additional selector semantics are provided.

## Gap

The current `files` bucket mixes different concepts:

```text
RSpec/Jest/Pytest: filesystem paths
Go: packages
Bazel/Pants workaround: runner targets
```

That ambiguity is why selection targets need a first-class model.
