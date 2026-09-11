#!/bin/sh

set -eu

fail() {
	printf '%s\n' "$*" >&2
	exit 1
}

validate_version_token() {
	name=$1
	value=$2
	case "$value" in
		"")
			fail "$name must not be empty"
			;;
		[!A-Za-z0-9]* | *[!A-Za-z0-9._+/-]*)
			fail "$name contains unsupported characters"
			;;
	esac
}

validate_commit() {
	value=$1
	case "$value" in
		unknown)
			return
			;;
		"" | *[!0-9A-Fa-f]*)
			fail "COMMIT must be unknown or a 7-64 character hexadecimal object ID"
			;;
	esac
	length=${#value}
	if [ "$length" -lt 7 ] || [ "$length" -gt 64 ]; then
		fail "COMMIT must be unknown or a 7-64 character hexadecimal object ID"
	fi
}

validate_build_metadata() {
	: "${MANIFEST_BUILD_VERSION:?MANIFEST_BUILD_VERSION is required}"
	: "${MANIFEST_BUILD_COMMIT:?MANIFEST_BUILD_COMMIT is required}"
	: "${MANIFEST_E2E_IMAGE_VERSION:?MANIFEST_E2E_IMAGE_VERSION is required}"
	: "${MANIFEST_BUILD_TAGS:?MANIFEST_BUILD_TAGS is required}"

	validate_version_token VERSION "$MANIFEST_BUILD_VERSION"
	validate_commit "$MANIFEST_BUILD_COMMIT"
	validate_version_token E2E_IMAGE_VERSION "$MANIFEST_E2E_IMAGE_VERSION"
	case "$MANIFEST_BUILD_TAGS" in
		[!A-Za-z0-9]* | *[!A-Za-z0-9_,.-]*)
			fail "BUILD_TAGS contains unsupported characters"
			;;
	esac
}

validate_release_tag() {
	tag=$1
	core='(0|[1-9][0-9]*)'
	identifier='(0|[1-9][0-9]*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*)'
	pattern="^v${core}\\.${core}\\.${core}(-${identifier}(\\.${identifier})*)?$"
	if ! LC_ALL=C printf '%s\n' "$tag" | grep -Eq "$pattern"; then
		fail "release tag is not canonical SemVer without build metadata: $tag"
	fi
}

case "${1-}" in
	metadata)
		[ "$#" -eq 1 ] || fail "usage: $0 metadata"
		validate_build_metadata
		;;
	build-command)
		[ "$#" -eq 2 ] || fail "usage: $0 build-command <command>"
		case "$2" in
			build | build-cover) ;;
			*) fail "BUILD_CMD must be build or build-cover" ;;
		esac
		;;
	release-tag)
		[ "$#" -eq 2 ] || fail "usage: $0 release-tag <tag>"
		validate_release_tag "$2"
		;;
	release-ref)
		[ "$#" -eq 1 ] || fail "usage: $0 release-ref"
		: "${RELEASE_REF_TYPE:?RELEASE_REF_TYPE is required}"
		: "${RELEASE_REF_NAME:?RELEASE_REF_NAME is required}"
		: "${RELEASE_REF:?RELEASE_REF is required}"
		[ "$RELEASE_REF_TYPE" = tag ] || fail "release workflow must run from a tag ref"
		[ "$RELEASE_REF" = "refs/tags/$RELEASE_REF_NAME" ] || fail "release ref and ref name do not match"
		validate_release_tag "$RELEASE_REF_NAME"
		;;
	*)
		fail "usage: $0 {metadata|build-command <command>|release-tag <tag>|release-ref}"
		;;
esac
