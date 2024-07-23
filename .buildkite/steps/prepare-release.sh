#!/usr/bin/env sh

current=$(svu current)
minor=$(svu minor)
patch=$(svu patch)

# If the current version is a prerelease, we want to bump the prerelease version
# otherwise we want to bump the minor version and add a prerelease tag.
if [[ "$current" =~ "-rc*" ]]; then
  prerelease=$(svu prerelease --pre-release rc)
else 
  prerelease=$(svu minor --pre-release rc)
fi


cat <<YAML | buildkite-agent pipeline upload
- block: "Create release?"
  fields:
    - select: "Version"
      key: "release-version"
      hint: "Select the version to release. Current version is $current"
      options:
        - label: "Minor ($minor)"
          value: $minor
        - label: "Patch ($patch)"
          value: $patch
        - label: "Prerelease ($prerelease)"
          value: $prerelease
YAML
