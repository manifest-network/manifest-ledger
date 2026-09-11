#!/bin/sh

set -eu

export LC_ALL=C

script_dir=$(CDPATH= cd "$(dirname "$0")" && pwd)
repo_root=$(dirname "$script_dir")
proto_root="$repo_root/proto"
gogo_output_root="$repo_root/github.com/manifest-network/manifest-ledger"
pulsar_output_root="$repo_root/liftedinit"

if [ ! -f "$repo_root/go.mod" ] || [ ! -d "$proto_root" ]; then
  echo "unable to locate the manifest-ledger repository" >&2
  exit 1
fi

cleanup_generated_roots() {
  rm -rf "$gogo_output_root" "$pulsar_output_root"
  rmdir "$repo_root/github.com/manifest-network" "$repo_root/github.com" 2>/dev/null || true
}

# Always generate from empty, repository-owned staging roots. This prevents a
# prior local run from masking a generator that silently produced no output.
cleanup_generated_roots
trap cleanup_generated_roots EXIT
trap 'exit 1' HUP INT TERM

echo "Generating gogo proto code"
cd "$proto_root"
# Generate only repository-owned, non-API messages. Repository proto paths are
# newline-free; the C-locale sort makes the invocation order reproducible.
find . -type f -name '*.proto' -print | sort | while IFS= read -r file; do
  if grep -q 'option go_package.*github.com/manifest-network/manifest-ledger/' "$file" &&
    ! grep -q 'option go_package.*github.com/manifest-network/manifest-ledger/api' "$file"; then
    buf generate --template buf.gen.gogo.yaml "$file"
  fi
done

if [ ! -d "$gogo_output_root" ]; then
  echo "gogo protobuf generation produced no repository output" >&2
  exit 1
fi

echo "Generating pulsar proto code"
buf generate --template buf.gen.pulsar.yaml

if [ ! -d "$pulsar_output_root" ]; then
  echo "pulsar protobuf generation produced no repository output" >&2
  exit 1
fi

cp -R "$gogo_output_root"/. "$repo_root"/

# Copy files over for dep injection
rm -rf "$repo_root/api"
mkdir "$repo_root/api"
cp -R "$pulsar_output_root" "$repo_root/api/"
