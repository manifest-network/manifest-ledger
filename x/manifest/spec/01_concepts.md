<!--
order: 1
-->

# Concepts

## Manual Payout

**NOTE** This can only be run from the chain's PoA admin(s) addresses.

```bash
# address:coin_amount: A pair of destination address and amount of coin to mint
manifestd tx manifest payout [address:coin_amount,...]
```

## Manual Burning

**NOTE** This can only be run from the chain's PoA admin(s) addresses.

```bash
# coins: The amount of coins to burn from the POA admin account. 
manifestd tx manifest burn-coins [coins,...]
```
