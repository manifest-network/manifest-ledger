#!/bin/sh

set -eu

fail() {
	printf '%s\n' "$*" >&2
	exit 1
}

validate_object_id() {
	case "$1" in
	???????????????????????????????????????? | \
	????????????????????????????????????????????????????????????????) ;;
	*) fail "GitHub returned an invalid release object ID" ;;
	esac
	case "$1" in
	*[!0-9A-Fa-f]*) fail "GitHub returned an invalid release object ID" ;;
	esac
}

resolve_object() {
	response=$(gh api "$1" --jq '.object.type + ":" + .object.sha')
	case "$response" in
	*:* ) ;;
	*) fail "GitHub returned an invalid release object" ;;
	esac

	remote_type=${response%%:*}
	remote_object_id=${response#*:}
	case "$remote_type" in
	commit | tag) ;;
	*) fail "GitHub returned an unsupported release object type" ;;
	esac
	validate_object_id "$remote_object_id"
}

: "${GITHUB_REPOSITORY:?GITHUB_REPOSITORY is required}"
: "${GITHUB_REF_NAME:?GITHUB_REF_NAME is required}"
: "${GITHUB_SHA:?GITHUB_SHA is required}"

sh "$(dirname "$0")/validate-build-inputs.sh" release-tag "$GITHUB_REF_NAME"

# Resolve refs/tags explicitly so a same-named branch can never satisfy this
# publication gate. Dereference annotated tags with a small fail-closed depth
# bound; lightweight tags already point directly at a commit.
resolve_object "repos/$GITHUB_REPOSITORY/git/ref/tags/$GITHUB_REF_NAME"
depth=0
while [ "$remote_type" = tag ]; do
	depth=$((depth + 1))
	[ "$depth" -le 16 ] || fail "release tag annotation depth exceeds the safety limit"
	resolve_object "repos/$GITHUB_REPOSITORY/git/tags/$remote_object_id"
done

[ "$remote_object_id" = "$GITHUB_SHA" ] || \
	fail "release tag no longer resolves to the workflow commit"
