#!/usr/bin/env bash
set -euo pipefail

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
# shellcheck source=scripts/lib/node-home.sh
source "$script_dir/lib/node-home.sh"

export KEY="user1"
export KEYRING="test"
export CHAIN_ID=${CHAIN_ID:-"local-1"}
HOME_DIR=$(resolve_node_home "${HOME_DIR:-}")
export HOME_DIR
validate_node_home "$HOME_DIR"
export BINARY=${BINARY:-manifestd}

run_manifestd() {
  "$BINARY" --home "$HOME_DIR" "$@"
}

# Add the development key if it does not exist (same seed as test_node.sh).
printf '%s\n' 'decorate bright ozone fork gallery riot bus exhaust worth way bone indoor calm squirrel merry zero scheme cotton until shop any excess stage laundry' |
  run_manifestd keys add "$KEY" --keyring-backend "$KEYRING" --algo secp256k1 --recover 2>/dev/null || true

flags=(--gas=2500000 --from="$KEY" --keyring-backend="$KEYRING" --chain-id="$CHAIN_ID" --output=json --yes)

echo "Storing contract..."
run_manifestd tx wasm store "$script_dir/cw_template.wasm" "${flags[@]}"
sleep 2

echo "Instantiating contract..."
txhash=$(run_manifestd tx wasm instantiate 1 '{"count":0}' --label=cw_template --no-admin "${flags[@]}" | jq -er '.txhash | select(type == "string" and length > 0)')
printf 'Transaction hash: %s\n' "$txhash"
sleep 10

echo "Getting contract address..."
addr=$(run_manifestd q tx "$txhash" --output=json | jq -er '.events[] | select(.type=="instantiate") | .attributes[] | select(.key=="_contract_address") | .value | select(type == "string" and length > 0)')
printf 'Contract address: %s\n' "$addr"
sleep 2

echo "Querying contract info..."
run_manifestd q wasm contract "$addr"
