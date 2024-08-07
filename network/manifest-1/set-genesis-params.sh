# Takes a default genesis from manifestd and creates a new genesis file.

make install

export HOME_DIR=$(eval echo "${HOME_DIR:-"~/.manifest"}")

rm -rf $HOME_DIR && echo "Removed $HOME_DIR"

manifestd init moniker --chain-id=manifest-1 --default-denom=umfx

update_genesis () {
    cat $HOME_DIR/config/genesis.json | jq "$1" > $HOME_DIR/config/tmp_genesis.json && mv $HOME_DIR/config/tmp_genesis.json $HOME_DIR/config/genesis.json
}

# Consensus
update_genesis '.consensus["params"]["block"]["max_gas"]="-1"'
update_genesis '.consensus["params"]["abci"]["vote_extensions_enable_height"]="1"'

# Auth
update_genesis '.app_state["auth"]["params"]["max_memo_characters"]="512"'

# Bank
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

# Crisis
update_genesis '.app_state["crisis"]["constant_fee"]={"denom": "umfx","amount": "100000000"}'

# Distribution
update_genesis '.app_state["distribution"]["params"]["community_tax"]="0.000000000000000000"'

# Gov
update_genesis '.app_state["gov"]["params"]["min_deposit"]=[{"denom":"umfx","amount":"100000000"}]'
update_genesis '.app_state["gov"]["params"]["max_deposit_period"]="259200s"'
update_genesis '.app_state["gov"]["params"]["voting_period"]="259200s"'
update_genesis '.app_state["gov"]["params"]["expedited_min_deposit"]=[{"denom":"umfx","amount":"250000000"}]'
update_genesis '.app_state["gov"]["params"]["min_deposit_ratio"]="0.100000000000000000"' # 10%
# update_genesis '.app_state["gov"]["params"]["constitution"]=""' # ?

# Mint
update_genesis '.app_state["mint"]["minter"]["inflation"]="0.000000000000000000"'
update_genesis '.app_state["mint"]["minter"]["annual_provisions"]="0.000000000000000000"'
update_genesis '.app_state["mint"]["params"]["mint_denom"]="notused"'
update_genesis '.app_state["mint"]["params"]["inflation_rate_change"]="0.000000000000000000"'
update_genesis '.app_state["mint"]["params"]["inflation_max"]="0.000000000000000000"'
update_genesis '.app_state["mint"]["params"]["inflation_min"]="0.000000000000000000"'
update_genesis '.app_state["mint"]["params"]["blocks_per_year"]="6311520"' # default 6s blocks

# Slashing
update_genesis '.app_state["slashing"]["params"]["signed_blocks_window"]="10000"'
update_genesis '.app_state["slashing"]["params"]["min_signed_per_window"]="0.050000000000000000"'
update_genesis '.app_state["slashing"]["params"]["downtime_jail_duration"]="60s"'
update_genesis '.app_state["slashing"]["params"]["slash_fraction_double_sign"]="0.000000000000000000"'
update_genesis '.app_state["slashing"]["params"]["slash_fraction_downtime"]="0.000000000000000000"'

# Group
update_genesis '.app_state["group"]["group_seq"]="1"'
update_genesis '.app_state["group"]["groups"]=[{"id": "1", "admin": "manifest1afk9zr2hn2jsac63h4hm60vl9z3e5u69gndzf7c99cqge3vzwjzsfmy9qj", "metadata": "AQ==", "version": "2", "total_weight": "2", "created_at": "2024-05-16T15:10:54.372190727Z"}]'
update_genesis '.app_state["group"]["group_members"]=[{"group_id": "1", "member": {"address": "manifest1hj5fveer5cjtn4wd6wstzugjfdxzl0xp8ws9ct", "weight": "1", "metadata": "user1", "added_at": "2024-05-16T15:10:54.372190727Z"}}, {"group_id": "1", "member": {"address": "manifest1efd63aw40lxf3n4mhf7dzhjkr453axurm6rp3z", "weight": "1", "metadata": "user2", "added_at": "2024-05-16T15:10:54.372190727Z"}}]'
update_genesis '.app_state["group"]["group_policy_seq"]="1"'
update_genesis '.app_state["group"]["group_policies"]=[{"address": "manifest1afk9zr2hn2jsac63h4hm60vl9z3e5u69gndzf7c99cqge3vzwjzsfmy9qj", "group_id": "1", "admin": "manifest1afk9zr2hn2jsac63h4hm60vl9z3e5u69gndzf7c99cqge3vzwjzsfmy9qj", "metadata": "AQ==", "version": "2", "decision_policy": { "@type": "/cosmos.group.v1.ThresholdDecisionPolicy", "threshold": "1", "windows": {"voting_period": "30s", "min_execution_period": "0s"}}, "created_at": "2024-05-16T15:10:54.372190727Z"}]'

# Staking
update_genesis '.app_state["staking"]["params"]["bond_denom"]="upoa"'

# Token Factory
update_genesis '.app_state["tokenfactory"]["params"]["denom_creation_fee"]=[]'
update_genesis '.app_state["tokenfactory"]["params"]["denom_creation_gas_consume"]="250000"'
# SPDT Token
update_test_genesis '.app_state["tokenfactory"]["factory_denoms"]=[{"denom": "factory/manifest1afk9zr2hn2jsac63h4hm60vl9z3e5u69gndzf7c99cqge3vzwjzsfmy9qj/uspdt", "authority_metadata": {"admin": "manifest1afk9zr2hn2jsac63h4hm60vl9z3e5u69gndzf7c99cqge3vzwjzsfmy9qj"}}]'
update_test_genesis '.app_state["bank"]["denom_metadata"]=[{"description": "SpaceData", "denom_units": [{"denom": "factory/manifest1afk9zr2hn2jsac63h4hm60vl9z3e5u69gndzf7c99cqge3vzwjzsfmy9qj/uspdt", "exponent": 0, "aliases": ["SPDT"]}, {"denom": "SPDT", "exponent": 6, "aliases": ["factory/manifest1afk9zr2hn2jsac63h4hm60vl9z3e5u69gndzf7c99cqge3vzwjzsfmy9qj/uspdt"]}], "base": "factory/manifest1afk9zr2hn2jsac63h4hm60vl9z3e5u69gndzf7c99cqge3vzwjzsfmy9qj/uspdt", "display": "SPDT", "name": "SpaceData", "symbol": "SPDT", "uri": "", "uri_hash": ""}]'
# ABUS Token
update_test_genesis '.app_state["tokenfactory"]["factory_denoms"] |= . + [{"denom": "factory/manifest1afk9zr2hn2jsac63h4hm60vl9z3e5u69gndzf7c99cqge3vzwjzsfmy9qj/uabus", "authority_metadata": {"admin": "manifest1afk9zr2hn2jsac63h4hm60vl9z3e5u69gndzf7c99cqge3vzwjzsfmy9qj"}}]'
update_test_genesis '.app_state["bank"]["denom_metadata"] |= . + [{"description": "Arebus Gas Token", "denom_units": [{"denom": "factory/manifest1afk9zr2hn2jsac63h4hm60vl9z3e5u69gndzf7c99cqge3vzwjzsfmy9qj/uabus", "exponent": 0, "aliases": ["ABUS"]}, {"denom": "ABUS", "exponent": 6, "aliases": ["factory/manifest1afk9zr2hn2jsac63h4hm60vl9z3e5u69gndzf7c99cqge3vzwjzsfmy9qj/uabus"]}], "base": "factory/manifest1afk9zr2hn2jsac63h4hm60vl9z3e5u69gndzf7c99cqge3vzwjzsfmy9qj/uabus", "display": "ABUS", "name": "Arebus Gas Token", "symbol": "ABUS", "uri": "", "uri_hash": ""}]'
# ... Add other MANY tokens here

# FeeGrant
# TODO: Add feegrant gas station here

# # add genesis accounts
# # TODO:
# manifestd genesis add-genesis-account $KEY 1000000upoa,10000000umfx,1000000000000000000000factory/manifest1afk9zr2hn2jsac63h4hm60vl9z3e5u69gndzf7c99cqge3vzwjzsfmy9qj/uspdt,100000000000000000000000factory/manifest1afk9zr2hn2jsac63h4hm60vl9z3e5u69gndzf7c99cqge3vzwjzsfmy9qj/uabus
# manifestd genesis add-genesis-account $KEY2 100000umfx