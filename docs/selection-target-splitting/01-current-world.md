# Current World

This page explains how the client works today.

## Commands

Run tests:

```sh
bktec run
```

Create a plan without running tests:

```sh
bktec plan --json
```

## Current split units

The client currently models split work as:

| Unit | Meaning | Example |
| --- | --- | --- |
| File | A test file/path the runner can execute | `spec/user_spec.rb`, `src/api.test.ts` |
| Example | One test inside a file | `spec/user_spec.rb[1:1]`, `test_api.py::test_create` |

There is no first-class concept for Go packages, Bazel labels, Pants targets, or generic runner targets.

## Default: file splitting

Example:

```sh
export BUILDKITE_TEST_ENGINE_TEST_RUNNER=jest
export BUILDKITE_TEST_ENGINE_SUITE_SLUG=my-suite
export BUILDKITE_TEST_ENGINE_RESULT_PATH=tmp/jest-results.json

bktec run
```

Conceptual request:

```json
{
  "tests": {
    "files": [
      { "path": "src/api.test.ts" },
      { "path": "src/auth.test.ts" }
    ]
  }
}
```

## File-list override

Users can provide a newline-delimited file list:

```sh
bktec run --files test-files.txt
```

Environment variable:

```sh
BUILDKITE_TEST_ENGINE_FILES=test-files.txt
```

Example:

```text
src/api.test.ts
src/auth.test.ts
src/billing.test.ts
```

## Split by example

Users opt in with:

```sh
bktec run --split-by-example
```

Environment variable:

```sh
BUILDKITE_TEST_ENGINE_SPLIT_BY_EXAMPLE=true
```

This does **not** mean every node gets one example. It also does **not** mean the plan contains only examples.

It means:

```text
Use example-level splitting where supported/useful; keep the rest as files.
```

Mixed request example:

```json
{
  "tests": {
    "files": [
      { "path": "spec/fast_spec.rb" }
    ],
    "examples": [
      {
        "path": "spec/slow_spec.rb[1:1]",
        "identifier": "spec/slow_spec.rb[1:1]",
        "name": "is slow",
        "scope": "SlowSpec"
      }
    ]
  }
}
```

Current split-by-example flow:

```text
1. Client discovers files.
2. Server tells the client which files should be split into examples.
3. Client asks the runner to enumerate examples for those files.
4. Client sends a mixed files + examples plan request.
```

Users do not currently have a first-class public CLI like `--examples-file` for directly supplying examples.

## Go today

Go usually runs packages:

```sh
go test ./internal/api
go test ./internal/auth
```

The `gotest` runner discovers packages with:

```sh
go list ./...
```

But the client still sends those packages as `tests.files`:

```json
{
  "tests": {
    "files": [
      { "path": "github.com/acme/repo/internal/api" },
      { "path": "github.com/acme/repo/internal/auth" }
    ]
  }
}
```

That works for execution because the Go runner treats `path` as a package. It is not a clean timing model because a package is not actually a file.

## Problem summary

The current API names one bucket `files`, but different runners put different things in that bucket:

```text
Jest/RSpec/Pytest: usually real files
Go: packages
Custom Bazel/Pants workaround: targets
```

That ambiguity is the reason to introduce a first-class selection-target model.
