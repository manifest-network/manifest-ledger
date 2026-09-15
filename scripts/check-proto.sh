#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."
: "${PROTO_IMAGE:?Set PROTO_IMAGE to the pinned protobuf builder image}"
check_dir=$(mktemp -d "${TMPDIR:-/tmp}/manifest-proto-check.XXXXXX")
trap 'rm -rf "$check_dir"' EXIT
mkdir "$check_dir/before" "$check_dir/generated"

# Copy current source contents, including pending edits, without touching the checkout.
git ls-files -z --cached --others --exclude-standard -- proto api x scripts/protocgen.sh |
  tar --null -T - -cf - | tar -xf - -C "$check_dir/before"
cp -R "$check_dir/before/." "$check_dir/generated/"
# Removing old outputs also detects files whose source messages were deleted.
find "$check_dir/generated/x" -type f \( -name '*.pb.go' -o -name '*.pb.gw.go' \) -delete

docker run --rm --user "$(id -u):$(id -g)" \
  -e HOME=/tmp -e BUF_CACHE_DIR=/tmp/buf-cache \
  -v "$check_dir/generated:/workspace" --workdir /workspace "$PROTO_IMAGE" \
  sh -euo pipefail -c 'buf build proto; buf lint proto --error-format=json; sh scripts/protocgen.sh'

diff -ru "$check_dir/before/api" "$check_dir/generated/api"
diff -ru "$check_dir/before/x" "$check_dir/generated/x"
echo 'Protobuf compilation, lint and generated-source checks passed.'
