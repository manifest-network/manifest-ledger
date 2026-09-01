#!/bin/sh

set -eu

if [ "$#" -ne 4 ]; then
	echo "usage: $0 <dist-dir> <expected-commit> <expected-version-or-empty> <expected-build-tags>" >&2
	exit 64
fi

dist_dir="$1"
expected_commit="$2"
expected_version="$3"
expected_build_tags="$4"

for command in file jq readelf sha256sum tar; do
	if ! command -v "$command" >/dev/null 2>&1; then
		echo "required release verifier command is unavailable: $command" >&2
		exit 69
	fi
done

set -- "$dist_dir"/*.tar.gz
if [ "$#" -ne 1 ] || [ ! -f "$1" ]; then
	echo "expected exactly one release archive in $dist_dir" >&2
	exit 1
fi
archive="$1"

set -- "$dist_dir"/*.tar.gz.sbom.json
if [ "$#" -ne 1 ] || [ ! -f "$1" ]; then
	echo "expected exactly one release SBOM in $dist_dir" >&2
	exit 1
fi
sbom="$1"

set -- "$dist_dir"/*_checksums.txt
if [ "$#" -ne 1 ] || [ ! -f "$1" ]; then
	echo "expected exactly one release checksum file in $dist_dir" >&2
	exit 1
fi
checksums="$1"

sh "$(dirname "$0")/verify-checksum-manifest.sh" "$checksums" "$archive" "$sbom"

(
	cd "$dist_dir"
	sha256sum --check "$(basename "$checksums")"
)

sh "$(dirname "$0")/verify-spdx-sbom.sh" "$sbom"

if ! archive_listing="$(tar -tzf "$archive")"; then
	echo "could not list release archive members" >&2
	exit 1
fi
archive_entries="$(printf '%s\n' "$archive_listing" | LC_ALL=C sort)"
expected_entries="$(printf '%s\n' LICENSE README.md manifestd)"
if [ "$archive_entries" != "$expected_entries" ]; then
	echo "release archive has unexpected members:" >&2
	printf '%s\n' "$archive_entries" >&2
	exit 1
fi

extract_dir="$(mktemp -d)"
trap 'rm -rf "$extract_dir"' EXIT HUP INT TERM
tar -xzf "$archive" -C "$extract_dir" --no-same-owner --no-same-permissions

binary="$extract_dir/manifestd"
if [ -L "$binary" ] || [ ! -f "$binary" ] || [ ! -x "$binary" ]; then
	echo "release archive manifestd is not a regular executable" >&2
	exit 1
fi

"$(dirname "$0")/verify-release-binary.sh" "$binary"

if ! program_headers="$(readelf -lW "$binary")"; then
	echo "could not inspect release binary program headers" >&2
	exit 1
fi
if printf '%s\n' "$program_headers" | grep -q 'INTERP'; then
	echo "release binary contains a dynamic program interpreter" >&2
	exit 1
fi
if ! dynamic_section="$(readelf -dW "$binary")"; then
	echo "could not inspect release binary dynamic section" >&2
	exit 1
fi
if printf '%s\n' "$dynamic_section" | grep -q '(NEEDED)'; then
	echo "release binary contains a dynamic library dependency" >&2
	exit 1
fi

version_json="$("$binary" version --long --output json)"
if ! printf '%s\n' "$version_json" | jq -e \
	--arg commit "$expected_commit" \
	--arg version "$expected_version" \
	--arg build_tags "$expected_build_tags" \
	'.name == "manifest"
	 and .server_name == "manifestd"
	 and .commit == $commit
	 and .build_tags == $build_tags
	 and (.go | endswith(" linux/amd64"))
	 and ($version == "" or .version == $version)' >/dev/null; then
	echo "release binary metadata does not match the requested build" >&2
	exit 1
fi
