#!/bin/bash
#
# Manually testing the manifest module against the test_node.sh script
#

export CHAIN_ID=${CHAIN_ID:-"local-1"}
export KEYRING=${KEYRING:-"test"}

export KEY="user1" # PoA Admin
export KEY2="user2"

manifestd config set client chain-id $CHAIN_ID
manifestd config set client keyring-backend $KEYRING

# When automatic inflation is on, this address (by default) should get 100% of the coins
manifestd q manifest params
manifestd q bank balances manifest1hj5fveer5cjtn4wd6wstzugjfdxzl0xp8ws9ct

# toggle inflation to be off. Stakeholders should not get auto payments
manifestd tx manifest update-params manifest1hj5fveer5cjtn4wd6wstzugjfdxzl0xp8ws9ct:100_000_000 false 500000000umfx --yes --from $KEY
manifestd q manifest params
manifestd q bank balances manifest1hj5fveer5cjtn4wd6wstzugjfdxzl0xp8ws9ct

# Perform a 1 off manual mint with inflation off
manifestd tx manifest stakeholder-payout 777umfx --yes --from $KEY
manifestd q bank balances manifest1hj5fveer5cjtn4wd6wstzugjfdxzl0xp8ws9ct # should go up 777 tokens

# re-enable auto inflation
manifestd tx manifest update-params manifest1hj5fveer5cjtn4wd6wstzugjfdxzl0xp8ws9ct:100_000_000 true 500000000umfx --yes --from $KEY
manifestd q manifest params
manifestd q bank balances manifest1hj5fveer5cjtn4wd6wstzugjfdxzl0xp8ws9ct

# try to manual mint (fails due to auto inflation being on)
manifestd tx manifest stakeholder-payout 777umfx --yes --from $KEY
# query the Tx, raw log == failed to execute message; message index: 0: manual minting is disabled due to automatic inflation being on