#!/usr/bin/env sh

current=$(svu current)
overrideOptionValue="__override__"

options() {
  if [[ "$current" =~ "-rc" ]]; then
    # if current version is 0.7.5-rc.1
    # the options should be
    # - 0.7.5
    # - 0.7.5-rc.2
    cat <<YAML
        - label: $(svu patch)
          value: $(svu patch)
        - label: $(svu prerelease)
          value: $(svu prerelease)
YAML
  else
    # if current version is 0.7.5
    # the options should be
    # - 0.7.6
    # - 0.7.6-rc.1
    # - 0.8.0
    # - 0.8.0-rc.1
    # - 1.0.0
    # - 1.0.0-rc.1
    cat <<YAML
        - label: $(svu patch)
          value: $(svu patch)
        - label: $(svu patch --pre-release rc).1
          value: $(svu patch --pre-release rc).1
        - label: $(svu minor)
          value: $(svu minor)
        - label: $(svu minor --pre-release rc).1
          value: $(svu minor --pre-release rc).1
        - label: $(svu major)
          value: $(svu major)
        - label: $(svu major --pre-release rc).1
          value: $(svu major --pre-release rc).1
YAML
  fi
}

cat <<YAML | buildkite-agent pipeline upload
- block: "Create release?"
  fields:
    - select: "Version"
      key: "release-version"
      hint: "Select the version to release, or choose Version override to use the exact version below. Current version is $current"
      options:
$(options)
        - label: "Version override"
          value: "$overrideOptionValue"
    - text: "Version override"
      key: "release-version-override"
      hint: "Exact version to release when Version is set to Version override, e.g. v2.9.0-rc.3"
      required: false
YAML
