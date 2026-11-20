#!/usr/bin/env bash
set -euo pipefail

tag=v3.6.0
canonical=cf34a8992ad430601ffbde7081258ffbc3906d5f
stage_sha=e758f9e8d17b175a744c315c3f9daf281719f8f312e6baa84da51d5ee2feb0c8
manifest_sha=05d2a81b853a3be630f830fdd96f2dff517850a7bd776b643b6331a2d97f0f58
[[ "$BUILDKITE_ORGANIZATION_SLUG/$BUILDKITE_PIPELINE_SLUG" == buildkite/test-engine-client-release ]]
scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT
mkdir "$scratch/checkout" "$scratch/contexts" "$scratch/docker"
export DOCKER_CONFIG="$scratch/docker"
repo="$scratch/checkout"
git init -q "$repo"
GIT_TERMINAL_PROMPT=0 git -C "$repo" -c credential.helper= fetch --quiet --no-tags --depth=1 https://github.com/buildkite/bktest.git "$canonical"
git -C "$repo" switch --quiet --detach FETCH_HEAD
[[ "$(git -C "$repo" rev-parse HEAD)" == "$canonical" ]]
buildkite-agent artifact download 'dist/cli-release.tar' "$scratch" \
  --build 01a1f67c-d02c-48a0-9e8e-48eba8b5c32b --step 01a1f680-9390-4e4c-9404-40bca7630349
printf '%s  %s\n' "$stage_sha" "$scratch/dist/cli-release.tar" | sha256sum --check --strict
docker run --rm -i --user "$(id -u):$(id -g)" \
  --mount "type=bind,src=$repo,dst=/work,readonly" \
  --mount "type=bind,src=$scratch/dist/cli-release.tar,dst=/candidate.tar,readonly" \
  --env PYTHONPATH=/work/packaging/cli python:3.13-slim-bookworm \
  python3 -B - "$stage_sha" "$tag" "$canonical" "$manifest_sha" > "$scratch/contexts.tar" <<'PY'
from pathlib import Path
import sys
import tarfile

from publish import open_stage

sha, tag, source, manifest_sha = sys.argv[1:]
with open_stage(Path('/candidate.tar'), sha, tag, source) as stage, \
        tarfile.open(fileobj=sys.stdout.buffer, mode='w|') as output:
    assert stage.assets['manifest.json'].sha256 == manifest_sha
    for arch in ('amd64', 'arm64'):
        prefix = f'image/linux_{arch}'
        for name in ('Dockerfile', 'bktec', 'bktest'):
            path = stage.assets[f'{prefix}/{name}'].path
            # open_stage verifies bytes and deliberately does not restore file modes.
            path.chmod(0o644 if name == 'Dockerfile' else 0o755)
            output.add(path, arcname=f'linux_{arch}/{name}')
PY
tar --extract --file "$scratch/contexts.tar" --directory "$scratch/contexts" --no-same-owner --no-same-permissions

mkdir -p dist/oci
builder="v360-$BUILDKITE_JOB_ID"
docker buildx create --name "$builder" --driver docker-container
trap 'docker buildx rm "$builder"; rm -rf "$scratch"' EXIT
for arch in amd64 arm64; do
  docker buildx build --builder "$builder" --platform "linux/$arch" \
    --progress plain --provenance=false --sbom=false \
    --output "type=oci,dest=dist/oci/linux_$arch.tar" "$scratch/contexts/linux_$arch"
done
docker run --rm -i --user "$(id -u):$(id -g)" \
  --mount "type=bind,src=$PWD/dist/oci,dst=/oci,readonly" python:3.13-slim-bookworm \
  python3 -B - "$canonical" "$stage_sha" "$manifest_sha" <<'PY' | tee dist/oci/receipt.json
import hashlib
import json
from pathlib import Path
import sys
import tarfile

canonical, stage_sha, manifest_sha = sys.argv[1:]
receipt = dict(source=canonical, stage_sha256=stage_sha, stage_manifest_sha256=manifest_sha, images={})
for arch in ('amd64', 'arm64'):
    path = Path('/oci') / f'linux_{arch}.tar'
    with path.open('rb') as stream:
        archive_sha = hashlib.file_digest(stream, 'sha256').hexdigest()
    with tarfile.open(path) as archive:
        members = {member.name.removeprefix('./'): member for member in archive.getmembers()}

        def verified_blob(descriptor):
            algorithm, sha = descriptor['digest'].split(':', 1)
            assert algorithm == 'sha256' and len(sha) == 64
            member = members[f'blobs/sha256/{sha}']
            assert member.isfile() and member.size == descriptor['size']
            with archive.extractfile(member) as blob:
                assert hashlib.file_digest(blob, 'sha256').hexdigest() == sha
                blob.seek(0)
                if descriptor['mediaType'].endswith('+json'):
                    return json.load(blob)

        index = json.load(archive.extractfile(members['index.json']))
        descriptor, = index['manifests']
        manifest = verified_blob(descriptor)
        if 'manifests' in manifest:
            descriptor, = manifest['manifests']
            manifest = verified_blob(descriptor)
        config = verified_blob(manifest['config'])
        assert (config['os'], config['architecture']) == ('linux', arch)
        assert config['config']['Entrypoint'] == ['/usr/local/bin/bktec']
        for layer in manifest['layers']:
            verified_blob(layer)
        receipt['images'][arch] = dict(archive_sha256=archive_sha, manifest=descriptor['digest'],
                                        config=manifest['config']['digest'])
print(json.dumps(receipt, indent=2))
PY
