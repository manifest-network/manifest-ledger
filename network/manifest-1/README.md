# Manifest-1 Network Guide

This directory holds the operator-facing docs and the canonical mainnet genesis file for `manifest-1`. Pick the path that matches what you're trying to do:

| If you want to… | Read |
|---|---|
| Join the **live** `manifest-1` chain as a validator or full node | [POST_GENESIS.md](./POST_GENESIS.md) |
| Track or pre-stage an **upgrade** | [UPGRADES.md](./UPGRADES.md) |
| Bootstrap a **new chain** from scratch (testnet, devnet, fork) | [GENESIS.md](./GENESIS.md) + [Custom Genesis](#custom-genesis) below |

The frozen mainnet genesis is checked in as [`manifest-1_genesis.json`](./manifest-1_genesis.json). The other JSON file (`genesis.json`) is a `manifestd` `init` artefact, **not** a usable network genesis — ignore it.

## Authoritative sources for live values

Some operator inputs change between releases and live outside this repo:

- **Seeds, persistent peers, RPC endpoints** — [`cosmos/chain-registry`](https://github.com/cosmos/chain-registry/tree/master/manifest), Manifest Network Discord `#validators`.
- **Snapshots, state-sync RPC servers** — Discord `#validators`, release notes.
- **Current binary version** — [GitHub releases](https://github.com/manifest-network/manifest-ledger/releases) (latest non-pre-release tag).
- **Scheduled upgrade height** — `manifestd query upgrade plan` against any live RPC.

## Custom Genesis

For anyone bootstrapping a new chain from this repo (testnet, devnet, fork): use [`set-genesis-params.sh`](./set-genesis-params.sh) to generate a fresh genesis with custom PoA admin(s), denomination, and other parameters.

Key knobs in the script:

```bash
# Staking denom (PoA bond denom). Distinct from the gas/fee denom.
update_genesis '.app_state["staking"]["params"]["bond_denom"]="upoa"'

# CosmWasm code-upload allowlist — set this to your chain's PoA admin(s):
update_genesis '.app_state["wasm"]["params"]["code_upload_access"]["addresses"]=["<your-admin-addr>"]'
```

Run the script, then pick up at [GENESIS.md](./GENESIS.md) to complete the genesis-ceremony flow.

> The script's `add-genesis-account` block is commented out — you'll need to uncomment and fill it in for any chain that should ship with seeded balances.
