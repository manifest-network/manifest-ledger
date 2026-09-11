#!/bin/sh

set -eu

SYFT_VERSION=1.51.1
SYFT_LINUX_AMD64_SHA256=8fcb33017a0dc1058298c923c436d19dfa68ae93968e0b423248542e3afb9fc3

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
	"https://github.com/anchore/syft/releases/download/v${SYFT_VERSION}/syft_${SYFT_VERSION}_linux_amd64.tar.gz"
printf '%s  %s\n' "$SYFT_LINUX_AMD64_SHA256" "$archive" | sha256sum --check --status

tar --extract --gzip --file "$archive" --directory "$install_dir" syft
chmod 0755 "$install_dir/syft"
"$install_dir/syft" version
