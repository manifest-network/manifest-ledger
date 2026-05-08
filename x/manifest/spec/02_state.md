<!--
order: 2
-->

# State

The `x/manifest` module has no genesis state and writes nothing on transactions: `GenesisState {}` is empty, and both `MsgPayout` and `MsgBurnHeldBalance` only mutate `x/bank` state via the module account's `Minter` / `Burner` permissions.

A reserved key prefix exists at `types.ParamsKey = collections.NewPrefix(0)` but is currently unused. Treat the module as stateless until that prefix becomes populated by a future release.

For the canonical, up-to-date description see [`x/manifest/README.md`](../README.md).
