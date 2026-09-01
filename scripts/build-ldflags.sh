#!/bin/sh

set -eu

script_dir=$(CDPATH= cd "$(dirname "$0")" && pwd)
sh "$script_dir/validate-build-inputs.sh" metadata

ldflags="-X github.com/cosmos/cosmos-sdk/version.Name=manifest"
ldflags="$ldflags -X github.com/cosmos/cosmos-sdk/version.AppName=manifestd"
ldflags="$ldflags -X github.com/cosmos/cosmos-sdk/version.Version=$MANIFEST_BUILD_VERSION"
ldflags="$ldflags -X github.com/cosmos/cosmos-sdk/version.Commit=$MANIFEST_BUILD_COMMIT"
ldflags="$ldflags -X github.com/manifest-network/manifest-ledger/app.Bech32Prefix=manifest"
ldflags="$ldflags -X github.com/cosmos/cosmos-sdk/version.BuildTags=$MANIFEST_BUILD_TAGS"

if [ "${MANIFEST_WITH_CLEVELDB-}" = yes ]; then
	ldflags="$ldflags -X github.com/cosmos/cosmos-sdk/types.DBBackend=cleveldb"
fi
if [ "${MANIFEST_LINK_STATICALLY-}" = true ]; then
	ldflags="$ldflags -linkmode=external -extldflags \"-Wl,-z,muldefs -static\""
fi
if [ -n "${MANIFEST_EXTRA_LDFLAGS-}" ]; then
	ldflags="$ldflags $MANIFEST_EXTRA_LDFLAGS"
fi

printf '%s\n' "$ldflags"
