#!/usr/bin/env bats

@test "flaky" {
  run bash -c '
    if [ $((RANDOM % 2)) -eq 0 ]; then
      echo "fail"
      exit 1
    fi
    echo "pass"
    exit 0
  '
  [ "$status" -eq 0 ]
  [[ "$output" == "pass" ]]
}
