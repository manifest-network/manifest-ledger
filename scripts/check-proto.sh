#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."
: "${PROTO_IMAGE:?Set PROTO_IMAGE to the pinned protobuf builder image}"
check_dir=$(mktemp -d "${TMPDIR:-/tmp}/manifest-proto-check.XXXXXX")
trap 'rm -rf "$check_dir"' EXIT
mkdir "$check_dir/before" "$check_dir/generated"

# Copy current source contents, including pending edits and deletions, without
# touching the checkout. Deleted generated files will appear in the final diff.
git ls-files -z --cached --others --exclude-standard -- proto api x scripts/protocgen.sh |
  while IFS= read -r -d '' path; do
    if [[ -e "$path" || -L "$path" ]]; then
      printf '%s\0' "$path"
    fi
  done |
  tar --null -T - -cf - | tar -xf - -C "$check_dir/before"
cp -R "$check_dir/before/." "$check_dir/generated/"
# Removing old outputs also detects files whose source messages were deleted.
find "$check_dir/generated/x" -type f \( -name '*.pb.go' -o -name '*.pb.gw.go' \) -delete

docker run --rm -i --user "$(id -u):$(id -g)" \
  -e HOME=/tmp -e BUF_CACHE_DIR=/tmp/buf-cache \
  -v "$check_dir/generated:/workspace" --workdir /workspace "$PROTO_IMAGE" \
  sh -euo pipefail <<'PROTOCHECK'
buf build proto --exclude-imports -o /tmp/proto-check.json
# Check parsed options, including multiline declarations. The generator copies
# gogo outputs from this module path and manages Pulsar outputs under api/.
unsupported=$(jq -r '
  .file[]
  | (.options.goPackage // "" | split(";")[0] // "") as $package
  | select($package != "" and (
      ($package | startswith("github.com/manifest-network/manifest-ledger/x/")) or
      ($package | startswith("github.com/manifest-network/manifest-ledger/api/"))
    | not))
  | "\(.name): \($package)"
' /tmp/proto-check.json)
if [ -n "$unsupported" ]; then
  printf 'Unsupported go_package outside checked api/ and x/ paths:\n%s\n' "$unsupported" >&2
  exit 1
fi
buf lint proto --error-format=json
sh scripts/protocgen.sh
PROTOCHECK

# One comparison prints all drift, including both generated trees and any
# unexpected output outside them, before returning a nonzero status.
diff -ru "$check_dir/before" "$check_dir/generated"
echo 'Protobuf compilation, lint and generated-source checks passed.'
