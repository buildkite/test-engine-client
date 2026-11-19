#!/usr/bin/env bash
# Exercise the actual Go package path against a local Git repository, not a published version.
set -euo pipefail

if [[ $# != 6 ]]; then
  echo 'usage: bash verify-install.sh canonical|legacy REPO REF SHA BRAND NEW_SCRATCH_DIR' >&2
  exit 2
fi
case "$1" in
  canonical) repository=bktest; command=/cmd/bktest; binary=bktest ;;
  legacy) repository=test-engine-client; command=; binary=test-engine-client ;;
  *) exit 2 ;;
esac
repo=$(git -C "$2" rev-parse --absolute-git-dir)
ref=$3
sha=$4
brand=$5
mkdir "$6"
scratch=$(realpath "$6")
module="github.com/buildkite/$repository/v3"
package="$module$command"
version=v3.999.0-verification

# Only this module uses direct Git resolution, mapped to the isolated local repository.
# Public third-party dependencies may come from the public Go proxy as usual.
export GOWORK=off GOENV=off GOFLAGS= GOTOOLCHAIN=local
export GOPRIVATE="github.com/buildkite/$repository" GONOPROXY="github.com/buildkite/$repository"
export GONOSUMDB="github.com/buildkite/$repository" GOPROXY=https://proxy.golang.org GOSUMDB=sum.golang.org
export GOMODCACHE="$scratch/modcache" GOBIN="$scratch/bin" CGO_ENABLED=0
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1 GIT_CONFIG_COUNT=1
export GIT_CONFIG_KEY_0="url.file://$repo.insteadOf"
export GIT_CONFIG_VALUE_0="https://github.com/buildkite/$repository"

cd "$scratch"
resolved=$(go mod download -json "$module@$ref")
python3 -c 'import json,sys; metadata=json.load(sys.stdin); assert metadata["Origin"]["Hash"] == sys.argv[1], metadata' "$sha" <<< "$resolved"
go install -ldflags "-X $module/internal/version.Version=$version" "$package@$ref"

installed="$GOBIN/$binary"
go version -m "$installed" | awk -v package="$package" '$1 == "path" && $2 == package { found=1 } END { exit !found }'
test "$("$installed" --version)" = "$brand $version"
"$installed" --help > help.txt
awk -v brand="$brand" '$1 == brand && $2 == "-" { found=1 } END { exit !found }' help.txt
"$installed" pool --help > pool-help.txt
grep -q 'leases' pool-help.txt
printf 'verified %s@%s (%s): %s, %s %s, help and pool help\n' "$package" "$ref" "$sha" "$binary" "$brand" "$version"
