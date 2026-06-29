#!/usr/bin/env bats

@test "failed" {
  run bash -c 'echo "something went wrong" >&2; exit 1'
  [ "$status" -eq 0 ]
  [[ "$output" == *"something went wrong"* ]]
}
