#!/usr/bin/env bash
set -euo pipefail
# Takes a default genesis from manifestd and creates a new genesis file.

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
# shellcheck source=scripts/lib/node-home.sh
source "$script_dir/../../scripts/lib/node-home.sh"

require_explicit_reset_home "${HOME_DIR:-}"
HOME_DIR=$(resolve_node_home "$HOME_DIR")
export HOME_DIR
validate_node_home "$HOME_DIR"
export BINARY=${BINARY:-manifestd}

make -C "$script_dir/../.." install

rm -rf -- "$HOME_DIR"
printf 'Removed %s\n' "$HOME_DIR"

"$BINARY" --home "$HOME_DIR" init moniker --chain-id=manifest-1 --default-denom=umfx

update_genesis () {
    update_node_genesis "$@"
}

update_genesis '.consensus["params"]["block"]["max_gas"]="-1"'
update_genesis '.consensus["params"]["abci"]["vote_extensions_enable_height"]="1"'

# auth
update_genesis '.app_state["auth"]["params"]["max_memo_characters"]="512"'

update_genesis '.app_state["bank"]["denom_metadata"]=[
        {
            "base": "umfx",
            "denom_units": [
            {
                "aliases": [],
                "denom": "umfx",
                "exponent": 0
            },
            {
                "aliases": [],
                "denom": "MFX",
                "exponent": 6
            }
            ],
            "description": "Denom metadata for MFX (umfx)",
            "display": "MFX",
            "name": "MFX",
            "symbol": "MFX"
        }
]'

update_genesis '.app_state["crisis"]["constant_fee"]={"denom": "umfx","amount": "100000000"}'

update_genesis '.app_state["distribution"]["params"]["community_tax"]="0.000000000000000000"'

update_genesis '.app_state["gov"]["params"]["min_deposit"]=[{"denom":"umfx","amount":"100000000"}]'
update_genesis '.app_state["gov"]["params"]["max_deposit_period"]="259200s"'
update_genesis '.app_state["gov"]["params"]["voting_period"]="259200s"'
update_genesis '.app_state["gov"]["params"]["expedited_min_deposit"]=[{"denom":"umfx","amount":"250000000"}]'
update_genesis '.app_state["gov"]["params"]["min_deposit_ratio"]="0.100000000000000000"' # 10%
# update_genesis '.app_state["gov"]["params"]["constitution"]=""' # ?

# not used
update_genesis '.app_state["mint"]["minter"]["inflation"]="0.000000000000000000"'
update_genesis '.app_state["mint"]["minter"]["annual_provisions"]="0.000000000000000000"'
update_genesis '.app_state["mint"]["params"]["mint_denom"]="notused"'
update_genesis '.app_state["mint"]["params"]["inflation_rate_change"]="0.000000000000000000"'
update_genesis '.app_state["mint"]["params"]["inflation_max"]="0.000000000000000000"'
update_genesis '.app_state["mint"]["params"]["inflation_min"]="0.000000000000000000"'
update_genesis '.app_state["mint"]["params"]["blocks_per_year"]="6311520"' # default 6s blocks

update_genesis '.app_state["slashing"]["params"]["signed_blocks_window"]="10000"'
update_genesis '.app_state["slashing"]["params"]["min_signed_per_window"]="0.050000000000000000"'
update_genesis '.app_state["slashing"]["params"]["downtime_jail_duration"]="60s"'
update_genesis '.app_state["slashing"]["params"]["slash_fraction_double_sign"]="1.000000000000000000"'
update_genesis '.app_state["slashing"]["params"]["slash_fraction_downtime"]="0.000000000000000000"'


update_genesis '.app_state["staking"]["params"]["bond_denom"]="upoa"'

update_genesis '.app_state["tokenfactory"]["params"]["denom_creation_fee"]=[]'
update_genesis '.app_state["tokenfactory"]["params"]["denom_creation_gas_consume"]="250000"'

# Add CosmWasm configuration
update_genesis '.app_state["wasm"]["params"]["code_upload_access"]["permission"]="AnyOfAddresses"'
update_genesis '.app_state["wasm"]["params"]["code_upload_access"]["addresses"]=["manifest1afk9zr2hn2jsac63h4hm60vl9z3e5u69gndzf7c99cqge3vzwjzsfmy9qj"]' # The POA Admin
update_genesis '.app_state["wasm"]["params"]["instantiate_default_permission"]="AnyOfAddresses"'

# # add genesis accounts
# # TODO:
# manifestd genesis add-genesis-account manifest1hj5fveer5cjtn4wd6wstzugjfdxzl0xp8ws9ct 1umfx --append
