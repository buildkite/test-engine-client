#!/usr/bin/env sh
set -eu

# This publisher requires a public Files registry with the slug
# "test-engine-client-files" and the following OIDC policy:
#
# - iss: https://agent.buildkite.com
#   scopes:
#     - write_packages
#   claims:
#     organization_slug: buildkite
#     pipeline_slug: test-engine-client-release

version=${1#v}
dist_dir=${2:-dist}
registry="test-engine-client-files"
audience="https://packages.buildkite.com/buildkite/${registry}"
upload_url="https://api.buildkite.com/v2/packages/organizations/buildkite/registries/${registry}/packages"
package_dir="${dist_dir}/buildkite-packages"
artifacts_file="${dist_dir}/artifacts.json"
checksums_file="${package_dir}/bktec-checksums-${version}.txt"

version_number='(0|[1-9][0-9]*)'
prerelease_identifier='(0|[1-9][0-9]*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*)'
version_pattern="^${version_number}\\.${version_number}\\.${version_number}(-${prerelease_identifier}(\\.${prerelease_identifier})*)?$"

if ! printf '%s\n' "${version}" | grep -Eq "${version_pattern}"; then
  echo "Invalid release version: ${version}" >&2
  exit 1
fi

if [ ! -f "${artifacts_file}" ]; then
  echo "GoReleaser artifact manifest not found: ${artifacts_file}" >&2
  exit 1
fi

rm -rf "${package_dir}"
mkdir -p "${package_dir}"

binary_count=$(jq '[.[] | select(.type == "Binary")] | length' "${artifacts_file}")
if [ "${binary_count}" -eq 0 ]; then
  echo "No release binaries found in ${artifacts_file}" >&2
  exit 1
fi

jq -r '.[] | select(.type == "Binary") | [.path, .goos, .goarch] | @tsv' "${artifacts_file}" |
  while IFS="$(printf '\t')" read -r source_file goos goarch; do
    if [ ! -f "${source_file}" ]; then
      echo "Release binary not found: ${source_file}" >&2
      exit 1
    fi

    extension="bin"
    case "${source_file}" in
      *.exe) extension="exe" ;;
    esac

    destination="${package_dir}/bktec-${goos}-${goarch}-${version}.${extension}"
    if [ -e "${destination}" ]; then
      echo "Duplicate release platform: ${goos}/${goarch}" >&2
      exit 1
    fi

    cp "${source_file}" "${destination}"
  done

prepared_count=$(find "${package_dir}" -type f | wc -l | tr -d ' ')
if [ "${prepared_count}" -ne "${binary_count}" ]; then
  echo "Prepared ${prepared_count} of ${binary_count} release binaries" >&2
  exit 1
fi

(
  cd "${package_dir}"
  sha256sum ./* > "${checksums_file##*/}"
)

echo "--- :key: :buildkite: Fetching OIDC token for ${audience}"
token=$(buildkite-agent oidc request-token --audience "${audience}" --lifetime 300)

upload() {
  file=$1
  filename=${file##*/}
  existing_file="${file}.existing"
  download_url="https://packages.buildkite.com/buildkite/${registry}/files/${filename}"

  status=$(curl -sS -L -o "${existing_file}" -w '%{http_code}' "${download_url}")
  case "${status}" in
    200)
      if [ "$(sha256sum "${file}" | cut -d ' ' -f 1)" = "$(sha256sum "${existing_file}" | cut -d ' ' -f 1)" ]; then
        echo "--- :white_check_mark: ${filename} is already published"
        rm "${existing_file}"
        return
      fi
      echo "Published file differs from ${file}" >&2
      rm "${existing_file}"
      exit 1
      ;;
    404)
      rm "${existing_file}"
      ;;
    *)
      echo "Could not check ${download_url}: HTTP ${status}" >&2
      rm "${existing_file}"
      exit 1
      ;;
  esac

  echo "--- :package: Uploading ${file}"
  curl -sS -X POST "${upload_url}" \
    -H "Authorization: Bearer ${token}" \
    -F "file=@${file}" \
    --fail-with-body
}

# The checksum is uploaded last so consumers can treat it as the completion
# marker for the version.
for file in "${package_dir}"/*; do
  if [ "${file}" = "${checksums_file}" ]; then
    continue
  fi
  upload "${file}"
done
upload "${checksums_file}"
