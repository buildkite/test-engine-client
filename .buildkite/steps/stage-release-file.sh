#!/usr/bin/env sh
set -eu

# Buildkite's Files registry requires {BASENAME}-{SEMVER}.{EXT} filenames,
# which differ from the GitHub release asset names (bktec_<version>_<os>_<arch>)
# that existing installers construct. GitHub naming is deliberately untouched;
# this helper renames staged copies for Buildkite Packages only.
artifact=$1
artifact_name=$2
version=$3
package_dir=dist/buildkite-packages

# bktec_3.0.0_linux_amd64       -> bktec-linux-amd64-3.0.0.bin
# bktec_3.0.0_windows_amd64.exe -> bktec-windows-amd64-3.0.0.exe
rename() {
  printf '%s\n' "$1" | sed -E \
    -e "s/^bktec_${version}_([a-z0-9]+)_([a-z0-9]+)\.exe$/bktec-\1-\2-${version}.exe/" \
    -e "s/^bktec_${version}_([a-z0-9]+)_([a-z0-9]+)$/bktec-\1-\2-${version}.bin/"
}

mkdir -p "${package_dir}"

case "${artifact_name}" in
  "bktec_${version}_checksums.txt")
    # Same hashes, renamed entries: the staged copies are byte-identical.
    # GoReleaser's checksum is global, so skip entries this path does not
    # publish (deb/rpm packages); their names pass through rename unchanged.
    while read -r hash name; do
      staged_name=$(rename "${name}")
      if [ "${staged_name}" != "${name}" ]; then
        printf '%s  %s\n' "${hash}" "${staged_name}"
      fi
    done < "${artifact}" > "${package_dir}/bktec-checksums-${version}.txt"
    ;;
  *)
    staged_name=$(rename "${artifact_name}")
    if [ "${staged_name}" = "${artifact_name}" ]; then
      echo "Unexpected release artifact name: ${artifact_name}" >&2
      exit 1
    fi
    cp "${artifact}" "${package_dir}/${staged_name}"
    ;;
esac
