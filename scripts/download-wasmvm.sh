#!/bin/sh
set -eu

if [ "$#" -ne 3 ]; then
    echo "Usage: $0 VERSION ARCH OUTPUT_DIRECTORY" >&2
    exit 1
fi

version=$1
arch=$2
output_dir=$3
script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
checksum=$(awk -v version="$version" -v arch="$arch" \
    '$1 == version && $2 == arch { print $3 }' "$script_dir/wasmvm-checksums.txt")
if [ -z "$checksum" ]; then
    echo "No pinned WasmVM checksum for version $version, architecture $arch" >&2
    exit 1
fi

# Verify a temporary download before replacing the archive used by the linker.
archive=$(mktemp "$output_dir/.wasmvm.XXXXXX")
trap 'rm -f "$archive"' EXIT HUP INT TERM
curl --fail --location --retry 5 --retry-delay 3 --retry-max-time 180 \
    --connect-timeout 15 --max-time 120 --output "$archive" \
    "https://github.com/CosmWasm/wasmvm/releases/download/$version/libwasmvm_muslc.$arch.a"
printf '%s  %s\n' "$checksum" "$archive" | sha256sum -c -
mv "$archive" "$output_dir/libwasmvm_muslc.a"
ln -sf libwasmvm_muslc.a "$output_dir/libwasmvm_muslc.$arch.a"
