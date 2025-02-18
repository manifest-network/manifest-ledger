#!/bin/bash

# Import environment variables that match test_node.sh
export KEY="user1"
export KEYRING="test"
export CHAIN_ID=${CHAIN_ID:-"local-1"}
export HOME_DIR=$(eval echo "${HOME_DIR:-"~/.manifest"}")
export BINARY=${BINARY:-manifestd}

# Set up the binary alias like in test_node.sh
alias BINARY="$BINARY --home=$HOME_DIR"

# Add keys if they don't exist (same seeds as test_node.sh)
echo "decorate bright ozone fork gallery riot bus exhaust worth way bone indoor calm squirrel merry zero scheme cotton until shop any excess stage laundry" | $BINARY keys add $KEY --keyring-backend $KEYRING --algo secp256k1 --recover 2>/dev/null || true

# Common flags using the environment variables
FLAGS="--gas=2500000 --from=$KEY --keyring-backend=$KEYRING --chain-id=$CHAIN_ID --output=json --yes --home=$HOME_DIR"

echo "Storing contract..."
$BINARY tx wasm store ./scripts/cw_template.wasm $FLAGS
sleep 2

echo "Instantiating contract..."
txhash=$($BINARY tx wasm instantiate 1 '{"count":0}' --label=cw_template --no-admin $FLAGS | jq -r .txhash) && echo "Transaction hash: $txhash"
sleep 10

echo "Getting contract address..."
addr=$($BINARY q tx $txhash --output=json | jq -r '.events[] | select(.type=="instantiate") | .attributes[] | select(.key=="_contract_address") | .value') && echo "Contract address: $addr"
sleep 2

echo "Querying contract info..."
$BINARY q wasm contract $addr