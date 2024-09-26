#!/usr/bin/env bash

set -Eeufo pipefail

echo --- :go: Generating code
go generate ./...
if ! git diff --no-ext-diff --exit-code; then
  echo ^^^ +++
  echo :x: Generated code was not commited.
  echo "Run"
  echo "  go generate ./..."
  echo "and make a commit."

  exit 1
fi

echo +++ Everything is clean and tidy! 🎉
