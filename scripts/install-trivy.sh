#!/bin/sh

set -eu

TRIVY_VERSION=0.74.0
TRIVY_LINUX_AMD64_SHA256=2ae6fe3ee734b7fdf11335663e18c75ea12dccc76062f09f164a3b0f8be4371a

fail() {
	printf '%s\n' "$*" >&2
	exit 1
}

[ "$#" -eq 1 ] || fail "usage: $0 <install-directory>"
install_dir=$1
[ -d "$install_dir" ] || fail "install directory does not exist: $install_dir"

archive=$(mktemp)
trap 'rm -f "$archive"' EXIT HUP INT TERM

curl --fail --location --proto '=https' --tlsv1.2 \
	--output "$archive" \
	"https://github.com/aquasecurity/trivy/releases/download/v${TRIVY_VERSION}/trivy_${TRIVY_VERSION}_Linux-64bit.tar.gz"
printf '%s  %s\n' "$TRIVY_LINUX_AMD64_SHA256" "$archive" | sha256sum --check --status

tar --extract --gzip --file "$archive" --directory "$install_dir" trivy
chmod 0755 "$install_dir/trivy"
"$install_dir/trivy" --version
