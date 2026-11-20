#!/usr/bin/env bash
set +x
set -euo pipefail

[[ "$BUILDKITE_ORGANIZATION_SLUG/$BUILDKITE_PIPELINE_SLUG" == buildkite/test-engine-client-release ]]

scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT
# This artifact is a rehearsal. It proves the route, never candidate identity.
build=01a1f3b9-efa8-40b5-b46d-5e3d29bc9fd3
job=01a1f3ba-28d9-4700-ad1b-a8b91a30c5ff
artifact=dist/cli-release.tar
expected=$(buildkite-agent artifact shasum --sha256 --build "$build" --step "$job" "$artifact")
[[ "$expected" =~ ^[a-f0-9]{64}$ ]]
buildkite-agent artifact download --build "$build" --step "$job" "$artifact" "$scratch"
printf '%s  %s\n' "$expected" "$scratch/$artifact" | sha256sum --check --strict
tar -tf "$scratch/$artifact" > "$scratch/paths"
grep -q 'bktec_3\.0\.0-rehearsal_linux_amd64\.deb$' "$scratch/paths"
printf 'Cross-build artifact download and SHA-256 verified (rehearsal only): %s\n' "$expected"

for registry in bktec test-engine-client-deb test-engine-client-rpm test-engine-client-docker; do
  audience="https://packages.buildkite.com/buildkite/$registry"
  buildkite-agent oidc request-token --audience "$audience" --lifetime 180 |
    python3 -c '
import base64, json, sys

payload = sys.stdin.read().strip().split(".")[1]
claims = json.loads(base64.urlsafe_b64decode(payload + "=" * (-len(payload) % 4)))
assert claims["iss"] == "https://agent.buildkite.com"
assert claims["aud"] == sys.argv[1] or sys.argv[1] in claims["aud"]
assert claims["organization_slug"] == "buildkite"
assert claims["pipeline_slug"] == "test-engine-client-release"
' "$audience"
  printf 'OIDC issued with expected claims: %s (not proof of registry grant)\n' "$registry"
done

printf 'SSM identity: '
aws sts get-caller-identity --query Account --output text
aws ssm describe-parameters --region us-east-1 \
  --parameter-filters 'Key=Name,Option=BeginsWith,Values=/pipelines/buildkite/test-engine-client-release/dockerhub-' \
  --query 'Parameters[].Name' --output text

export AWS_ACCESS_KEY_ID="$ECR_AWS_ACCESS_KEY_ID"
export AWS_SECRET_ACCESS_KEY="$ECR_AWS_SECRET_ACCESS_KEY"
export AWS_SESSION_TOKEN="$ECR_AWS_SESSION_TOKEN"
printf 'ECR identity: '
aws sts get-caller-identity --query Account --output text
aws ecr-public describe-images --region us-east-1 --repository-name test-engine-client \
  --image-ids imageTag=v3.5.0 --query 'imageDetails[0].imageDigest' --output text
