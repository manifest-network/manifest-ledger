#!/bin/sh

set -eu

readonly wasmvm_module="github.com/CosmWasm/wasmvm/v2"
readonly supported_version="v2.2.8"

wasmvm_version="$(go list -m -f '{{.Version}}' "$wasmvm_module")"
if [ "$wasmvm_version" != "$supported_version" ]; then
	echo "unsupported wasmvm release: $wasmvm_version" >&2
	exit 1
fi

goarch="${GOARCH:-$(go env GOARCH)}"
case "$goarch" in
	amd64)
		asset="libwasmvm_muslc.x86_64.a"
		sha256="4ebe53c15a4282c27d5fb2f3f853588d9a901877e26e0cd4f4a605d3b271d041"
		;;
	arm64)
		asset="libwasmvm_muslc.aarch64.a"
		sha256="1f7a5a8c6f17f30324ed4ae279ef59cf624c18fc70889d7e3bbb8e0e91a785a5"
		;;
	*)
		echo "unsupported wasmvm architecture: $goarch" >&2
		exit 1
		;;
esac

download_dir="$(mktemp -d)"
trap 'rm -rf "$download_dir"' EXIT HUP INT TERM
download_path="$download_dir/$asset"
url="https://github.com/CosmWasm/wasmvm/releases/download/$wasmvm_version/$asset"
# The muslc build tag searches the system library path. Keep this outside the
# extracted Go module so the later `go mod verify` remains meaningful.
install_dir="${WASMVM_LIB_DIR:-/lib}"

if command -v curl >/dev/null 2>&1; then
	curl --fail --location --silent --show-error --output "$download_path" "$url"
elif command -v wget >/dev/null 2>&1; then
	wget -q -O "$download_path" "$url"
else
	echo "curl or wget is required to download $asset" >&2
	exit 1
fi

echo "$sha256  $download_path" | sha256sum -c -
mkdir -p "$install_dir"
cp "$download_path" "$install_dir/$asset"
chmod 0644 "$install_dir/$asset"
