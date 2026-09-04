#!/bin/sh

set -eu

fail() {
	printf '%s\n' "$*" >&2
	exit 1
}

[ "$#" -ge 4 ] || fail "usage: $0 <builder-image> <syft-binary> <goreleaser-binary> <goreleaser-arguments>..."

builder_image=$1
syft_binary=$2
goreleaser_binary=$3
shift 3

case "$builder_image" in
	"" | *[!A-Za-z0-9._:/@-]*) fail "builder image contains unsupported characters" ;;
esac

for tool in "$syft_binary" "$goreleaser_binary"; do
	case "$tool" in
		/*) ;;
		*) fail "tool path must be absolute: $tool" ;;
	esac
	case "$tool" in
		*:*) fail "tool path contains unsupported characters: $tool" ;;
	esac
	[ -f "$tool" ] && [ ! -L "$tool" ] && [ -x "$tool" ] || fail "tool must be a regular executable: $tool"
done

command -v docker >/dev/null 2>&1 || fail "docker is required to run the pinned release toolchain"
command -v git >/dev/null 2>&1 || fail "git is required to locate the repository"

repository=$(git rev-parse --show-toplevel)
repository=$(cd "$repository" && pwd -P)
case "$repository" in
	*:*) fail "repository path contains unsupported characters: $repository" ;;
esac
[ -f "$repository/go.mod" ] && [ -d "$repository/.git" ] || fail "release toolchain must run from a Git checkout"

docker run --rm \
	--user "$(id -u):$(id -g)" \
	--env CGO_ENABLED=1 \
	--env GOCACHE=/tmp/manifest-go-build \
	--env GOPROXY=off \
	--env GOTOOLCHAIN=local \
	--env GOWORK=off \
	--env HOME=/tmp/manifest-release-home \
	--env SYFT_CHECK_FOR_APP_UPDATE=false \
	--volume "$repository:/workspace" \
	--volume "$syft_binary:/usr/local/bin/syft:ro" \
	--volume "$goreleaser_binary:/usr/local/bin/goreleaser:ro" \
	--workdir /workspace \
	"$builder_image" \
	/bin/sh -ec 'mkdir -p "$HOME" "$GOCACHE"; exec /usr/local/bin/goreleaser "$@"' \
	goreleaser "$@"
