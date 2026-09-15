#!/bin/sh

set -eu

fail() {
	printf '%s\n' "$*" >&2
	exit 1
}

: "${GITHUB_REPOSITORY:?GITHUB_REPOSITORY is required}"
: "${GITHUB_REPOSITORY_OWNER:?GITHUB_REPOSITORY_OWNER is required}"
: "${GITHUB_REF_NAME:?GITHUB_REF_NAME is required}"
: "${IMAGE_NAME:?IMAGE_NAME is required}"

sh "$(dirname "$0")/validate-build-inputs.sh" release-tag "$GITHUB_REF_NAME"

package_name=${IMAGE_NAME#*/}
[ "$IMAGE_NAME" = "$GITHUB_REPOSITORY_OWNER/$package_name" ] || \
	fail "IMAGE_NAME must belong to GITHUB_REPOSITORY_OWNER"
case "$package_name" in
	"" | */*) fail "IMAGE_NAME must contain exactly one owner separator" ;;
esac

# Capture each complete API response before testing it. With `set -e`, any
# authentication, permission, pagination, or availability error aborts the
# release instead of being mistaken for an absent tag.
release_tags=$(gh api --paginate \
	"repos/$GITHUB_REPOSITORY/releases?per_page=100" \
	--jq '.[].tag_name')
package_tags=$(gh api --paginate \
	"orgs/$GITHUB_REPOSITORY_OWNER/packages/container/$package_name/versions?per_page=100" \
	--jq '.[].metadata.container.tags[]?')

normalized_version=${GITHUB_REF_NAME#v}

contains_line() {
	lines=$1
	wanted=$2
	printf '%s\n' "$lines" | grep --fixed-strings --line-regexp --quiet -- "$wanted"
}

for candidate in "$GITHUB_REF_NAME" "$normalized_version"; do
	if contains_line "$release_tags" "$candidate"; then
		fail "GitHub release already exists and will not be overwritten: $candidate"
	fi
	if contains_line "$package_tags" "$candidate"; then
		fail "container tag already exists and will not be overwritten: $candidate"
	fi
done
