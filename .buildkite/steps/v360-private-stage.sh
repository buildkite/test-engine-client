#!/usr/bin/env bash
set -euo pipefail

tag=v3.6.0
canonical=cf34a8992ad430601ffbde7081258ffbc3906d5f
legacy=4f9bde98ac62d14cd6e4bf3fca47e68d6cdcdb4f
[[ "$BUILDKITE_ORGANIZATION_SLUG/$BUILDKITE_PIPELINE_SLUG" == buildkite/test-engine-client-release ]]
git merge-base --is-ancestor "$legacy" HEAD
git diff --exit-code "$legacy" HEAD -- . \
  ':(exclude).buildkite/pipeline.release.yml' ':(exclude).buildkite/steps/v360-private-stage.sh'
diff -u <(printf 'repository github.com/buildkite/bktest\ncommit %s\n' "$canonical") <(git show "$legacy:.bktest-source")

scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT
repo="$scratch/bktest"
git init -q "$repo"
GIT_TERMINAL_PROMPT=0 git -C "$repo" -c credential.helper= fetch --quiet --no-tags --depth=1 https://github.com/buildkite/bktest.git "$canonical"
git -C "$repo" switch --quiet --detach FETCH_HEAD
[[ "$(git -C "$repo" rev-parse HEAD)" == "$canonical" ]]
mkdir -p dist

case "$1" in
  stage)
    # This clone has no remotes. The fixture ref never leaves the worker.
    git -C "$repo" tag "$tag" "$canonical"
    image="bktest-private-stage:$BUILDKITE_JOB_ID"
    docker build -f "$repo/.buildkite/cli/Dockerfile.stage" -t "$image" "$repo/.buildkite"
    docker run --rm --user "$(id -u):$(id -g)" \
      --mount "type=bind,src=$repo,dst=/work,readonly" --workdir /work \
      --env HOME=/tmp --env GOPATH=/tmp/go --env "BUILDKITE_TAG=$tag" "$image" \
      bash -ec 'bash packaging/cli/stage-checkout.sh /tmp/cli-release >&2 && tar -C /tmp/cli-release -cf - .' \
      > dist/cli-release.tar.partial
    mv dist/cli-release.tar.partial dist/cli-release.tar
    sha256sum dist/cli-release.tar | tee dist/stage.sha256
    ;;
  verify)
    build=01a1f67c-d02c-48a0-9e8e-48eba8b5c32b
    job=01a1f680-9390-4e4c-9404-40bca7630349
    sha=e758f9e8d17b175a744c315c3f9daf281719f8f312e6baa84da51d5ee2feb0c8
    buildkite-agent artifact download 'dist/cli-release.tar' . --build "$build" --step "$job"
    printf '%s  dist/cli-release.tar\n' "$sha" | sha256sum --check --strict
    docker run --rm -i --user "$(id -u):$(id -g)" \
      --mount "type=bind,src=$repo,dst=/work,readonly" \
      --mount "type=bind,src=$PWD/dist/cli-release.tar,dst=/candidate.tar,readonly" \
      --env PYTHONPATH=/work/packaging/cli python:3.13-slim-bookworm \
      python3 -B - "$sha" "$tag" "$canonical" > dist/verified-files.tar <<'PY'
from pathlib import Path
import json
import sys
import tarfile

from publish import open_stage

sha, tag, source = sys.argv[1:]
with open_stage(Path("/candidate.tar"), sha, tag, source) as stage, \
        tarfile.open(fileobj=sys.stdout.buffer, mode="w|") as output:
    for name, asset in sorted(stage.assets.items()):
        if name.startswith("files/"):
            output.add(asset.path, arcname=f"buildkite-packages/{asset.path.name}")
            print(f"{asset.sha256}  buildkite-packages/{asset.path.name}", file=sys.stderr)
    manifest = stage.assets["manifest.json"].path
    output.add(manifest, arcname="verified-manifest.json")
    print(f"Verified {len(stage.assets)} staged files and aliases for {tag} at {source}", file=sys.stderr)
    print("Verified manifest: " + json.dumps(json.loads(manifest.read_bytes()), sort_keys=True), file=sys.stderr)
PY
    tar --extract --file dist/verified-files.tar --directory dist --no-same-owner --no-same-permissions
    sha256sum dist/cli-release.tar dist/verified-manifest.json | tee dist/verified-stage.sha256
    ;;
  *) exit 2 ;;
esac
