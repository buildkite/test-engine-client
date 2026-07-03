#!/usr/bin/env bats

@test "happy" {
  run bash -c 'echo "ok"; exit 0'
  [ "$status" -eq 0 ]
  [ "$output" = "ok" ]
}
