<!--
order: 1
-->

# Concepts

## Manifest

The Manifest module allows the chain admin(s) to set stakeholders and their percent of network rewards. These rewards can be made automatically on a set fixed amount per block, or manually if auto inflation is toggled off.

## Update Stakeholders and Inflation

**NOTE** This can only be run from the chain's PoA admin(s) addresses.

```bash
# address_pairs is a comma delimited list of address and percent pairs in the micro format
# - `address:1_000_000,address2:99_000_000` = 2 addresses at 100%.
#
# inflation_per_year_coin is the amount of coins to give per year for automatic inflation
# - `5000000umfx`
#
manifestd tx manifest update-params [address_pairs] [automatic_inflation_enabled] [inflation_per_year_coin]
```

## Payout Stakeholders Manually

**NOTE** This can only be run from the chain's PoA admin(s) addresses.
**NOTE** This may only be run when automatic inflation is off. If it is on, this transaction will always error out on execution.

```bash
# coin_amount is the amount of coins to distribute to all stakeholders. This is then split up based off their split of the network distribution.
# - `5000000umfx`
#
# If you wish to payout to a different group than what current stakeholders are set, use a multi-message transaction to update-params, perform the payout, and update-params back to the original state. This can be done in 1 transaction.
# -https://docs.junonetwork.io/developer-guides/miscellaneous/multi-message-transaction
manifestd tx manifest stakeholder-payout [coin_amount]
```
