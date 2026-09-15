#!/bin/sh

set -eu

BUILDX_VERSION=0.36.1
BUILDX_LINUX_AMD64_SHA256=48af8a397ebd60178778bf63611dbcebe5f5e7a9be90eb9147b24b9587455778

fail() {
	printf '%s\n' "$*" >&2
	exit 1
}

[ "$#" -eq 1 ] || fail "usage: $0 <docker-cli-plugin-directory>"
plugin_dir=$1
[ -d "$plugin_dir" ] || fail "Docker CLI plugin directory does not exist: $plugin_dir"
[ "$(uname -s)" = Linux ] && [ "$(uname -m)" = x86_64 ] || \
	fail "Buildx installer supports only linux/amd64"

binary=$(mktemp)
trap 'rm -f "$binary"' EXIT HUP INT TERM

curl --fail --location --proto '=https' --tlsv1.2 \
	--output "$binary" \
	"https://github.com/docker/buildx/releases/download/v${BUILDX_VERSION}/buildx-v${BUILDX_VERSION}.linux-amd64"
printf '%s  %s\n' "$BUILDX_LINUX_AMD64_SHA256" "$binary" | sha256sum --check --status

cp "$binary" "$plugin_dir/docker-buildx"
chmod 0755 "$plugin_dir/docker-buildx"
"$plugin_dir/docker-buildx" version
