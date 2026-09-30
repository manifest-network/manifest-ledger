#!/usr/bin/env bash

set -euo pipefail

echo "Generating gogo proto code"
cd proto
proto_dirs=$(find . -name '*.proto' -print0 | xargs -0 -n1 dirname | sort | uniq)
for dir in $proto_dirs; do
  for file in $(find "${dir}" -maxdepth 1 -name '*.proto'); do
    # this regex checks if a proto file has its go_package set to github.com/manifest-network/manifest-ledger/...
    # gogo proto files SHOULD ONLY be generated if this is false
    # we don't want gogo proto to run for proto files which are natively built for google.golang.org/protobuf
    if grep -q "option go_package" "$file" && ! grep -q 'option go_package.*github.com/manifest-network/manifest-ledger/api' "$file"; then
      buf generate --template buf.gen.gogo.yaml "$file"
    fi
  done
done

echo "Generating pulsar proto code"
buf generate --template buf.gen.pulsar.yaml

cd ..

cp -r github.com/manifest-network/manifest-ledger/* ./
rm -rf github.com

# The managed Pulsar output mirrors proto/liftedinit under the repository root.
# Move only that generated tree; unrelated module directories are not outputs.
rm -rf api
mkdir api
mv liftedinit api/

# Use an explicit backup suffix supported by the builder's sed and BSD sed.
find api -type f -name '*.go' -exec sed -i.bak -e 's|types "github.com/cosmos/cosmos-sdk/types"|types "cosmossdk.io/api/cosmos/base/v1beta1"|g' {} +
find api -type f -name '*.go.bak' -delete
