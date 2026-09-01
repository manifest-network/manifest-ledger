#!/usr/bin/env bash

set -e

echo "Generating gogo proto code"
cd proto
# Generate only repository-owned, non-API messages. The sorted NUL-delimited
# file walk is deterministic and does not split valid paths on whitespace.
while IFS= read -r -d '' file; do
  if grep -q 'option go_package.*github.com/manifest-network/manifest-ledger/' "$file" &&
    ! grep -q 'option go_package.*github.com/manifest-network/manifest-ledger/api' "$file"; then
    buf generate --template buf.gen.gogo.yaml "$file"
  fi
done < <(find . -type f -name '*.proto' -print0 | sort -z)

echo "Generating pulsar proto code"
buf generate --template buf.gen.pulsar.yaml

cd ..

cp -r github.com/manifest-network/manifest-ledger/* ./
rm -rf github.com

# Copy files over for dep injection
rm -rf api && mkdir api
# Pulsar's source-relative output is created only under these generated module
# roots. Restricting the move to that known shape avoids scanning or moving
# unrelated workspace directories that happen to be named "module".
for module in liftedinit/*/module; do
  [ -d "$module" ] || continue
  dir_path=$(dirname "$module")
  mkdir -p "api/$dir_path"
  mv "$dir_path"/* "api/$dir_path/"
  rm -rf "$dir_path"
done
rmdir liftedinit 2>/dev/null || true
