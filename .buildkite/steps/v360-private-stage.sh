#!/usr/bin/env bash
set -euo pipefail

tag=v3.6.0
canonical=cf34a8992ad430601ffbde7081258ffbc3906d5f
legacy=4f9bde98ac62d14cd6e4bf3fca47e68d6cdcdb4f
[[ "$BUILDKITE_ORGANIZATION_SLUG/$BUILDKITE_PIPELINE_SLUG" == buildkite/test-engine-client-release ]]
[[ "$(git rev-parse HEAD^)" == "$legacy" ]]
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
      --mount "type=bind,src=$repo,dst=/work" --workdir /work \
      --env HOME=/tmp --env GOPATH=/tmp/go --env "BUILDKITE_TAG=$tag" "$image" \
      bash -ec 'bash packaging/cli/stage-checkout.sh dist/cli-release && tar -C dist/cli-release -cf dist/cli-release.tar .'
    cp "$repo/dist/cli-release.tar" dist/cli-release.tar
    sha256sum dist/cli-release.tar | tee dist/stage.sha256
    ;;
  verify)
    buildkite-agent artifact download 'dist/cli-release.tar' . --step stage-v360
    buildkite-agent artifact download 'dist/stage.sha256' . --step stage-v360
    sha256sum --check --strict dist/stage.sha256
    read -r sha _ < dist/stage.sha256
    PYTHONPATH="$repo/packaging/cli" python3 -B - "$sha" "$tag" "$canonical" <<'PY'
from pathlib import Path
from shutil import copyfile
import sys

from publish import open_stage

sha, tag, source = sys.argv[1:]
dist = Path("dist")
with open_stage(dist / "cli-release.tar", sha, tag, source) as stage:
    files = dist / "buildkite-packages"
    files.mkdir()
    for name, asset in sorted(stage.assets.items()):
        if name.startswith("files/"):
            copyfile(asset.path, files / asset.path.name)
            print(f"{asset.sha256}  {files / asset.path.name}")
    copyfile(stage.assets["manifest.json"].path, dist / "verified-manifest.json")
    print(f"Verified {len(stage.assets)} staged files and aliases for {tag} at {source}")
PY
    sha256sum dist/cli-release.tar dist/verified-manifest.json | tee dist/verified-stage.sha256
    ;;
  *) exit 2 ;;
esac
