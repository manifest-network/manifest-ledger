# x/manifest

The `manifest` module gives the Proof-of-Authority admin a controlled mint-and-distribute primitive plus the matching burn primitive. On a PoS chain these would normally come from `x/mint`'s automatic inflation; on a PoA chain inflation is policy, not protocol, so this module replaces that BeginBlocker with two explicit transaction types.

## Concepts

### Why this module exists

The standard Cosmos SDK `x/mint` module mints new supply every block based on a target inflation rate and bonded ratio. That model assumes a PoS economic loop. `manifest-1` runs Proof of Authority — there is no bonded ratio to target and no automatic inflation policy to enforce. Instead, the PoA admin (or admin group) decides when and how much to mint, and pays out stakeholders directly via `MsgPayout`.

Concretely:

```go
// app/app.go module manager registration:
app.ModuleManager.SetOrderBeginBlockers(
    // minttypes.ModuleName, // we override with the manifest module logic
    manifesttypes.ModuleName, // minter to stakeholders
    ...
)
```

The standard `x/mint` BeginBlocker is commented out. `x/manifest` occupies the same slot but **does not register a BeginBlocker** — minting only happens inside `MsgPayout` transactions. There is no time-based behaviour in this module.

### What the authority can do

| Operation | Effect on supply | Coin source / sink |
|---|---|---|
| `MsgPayout` | Increases | Mints fresh coins via the module's `Minter` permission, then sends to one or more recipient addresses |
| `MsgBurnHeldBalance` | Decreases | Pulls coins from the **authority's own bank balance** into the module account, then burns them |

`MsgBurnHeldBalance` does not let the authority burn arbitrary holders' balances — only coins already held in the authority address. To burn from somewhere else, fund the authority first.

## Authorisation

| Operation | Authority | Anyone else |
|---|---|---|
| `MsgPayout` | ✓ | ✗ |
| `MsgBurnHeldBalance` | ✓ | ✗ |

"Authority" means the address configured in `keeper.authority` at app wiring. On `manifest-1` this is the PoA admin (or, when wrapped in `x/group`, the group's policy address). The msg server compares `msg.authority` to `keeper.authority` byte-for-byte and rejects with a plain `fmt.Errorf("invalid authority; expected …, got …")` on mismatch.

> **Operational risk:** the authority has uncapped mint power. There is no on-chain policy limiting payout amounts, payout frequency, or burnable amounts. Compromise of the PoA admin key means uncapped supply changes. Treat the admin as a multi-sig / x/group policy in production.

## Messages

| Message | Description |
|---------|-------------|
| `MsgPayout` | Mint new coins and distribute to a list of `(address, coin)` pairs. Authority only. |
| `MsgBurnHeldBalance` | Burn coins held in the authority's own account. Authority only. |

### MsgPayout

```protobuf
message MsgPayout {
  string authority = 1;                     // must equal keeper.authority
  repeated PayoutPair payout_pairs = 2;     // 1..N pairs
}

message PayoutPair {
  string address = 1;                       // bech32 recipient
  cosmos.base.v1beta1.Coin coin = 2;        // single coin (one denom per pair)
}

message MsgPayoutResponse {}
```

**Validation (`MsgPayout.Validate`):**
- `authority` parses as bech32.
- `payout_pairs` is non-empty.
- Each pair's `address` parses as bech32.
- Each pair's `coin` is non-zero and passes `Coin.Validate()`.
- No duplicate `address` across pairs.

**Behaviour (`Keeper.Payout`):**
- For each pair, mint the coin into the `manifest` module account (`bankKeeper.MintCoins`).
- Send from the module account to the recipient (`bankKeeper.SendCoinsFromModuleToAccount`).
- Logs `Payout` per pair.

**CLI:**
```bash
manifestd tx manifest payout [address:coin_amount,...] --from <admin-key>
# Example (two recipients in one tx):
manifestd tx manifest payout \
  manifest1abc...:50_000umfx,manifest1xyz...:1_000_000umfx \
  --from authority
# Underscores in amounts are stripped client-side for readability.
```

### MsgBurnHeldBalance

```protobuf
message MsgBurnHeldBalance {
  string authority = 1;                     // must equal keeper.authority — the burn source
  repeated cosmos.base.v1beta1.Coin burn_coins = 2;  // 1..N coins (any denom mix)
}

message MsgBurnHeldBalanceResponse {}
```

> The proto field is named `authority` (and the proto comment mistakenly calls it `sender`). The semantics are unambiguous: this is the address the coins are debited from, and it must equal `keeper.authority`.

**Validation (`MsgBurnHeldBalance.Validate`):**
- `authority` parses as bech32.
- `burn_coins.Len() > 0`.
- `burn_coins.Validate()` (no zero, no negative, no duplicates, sorted denoms).

**Behaviour (`msgServer.BurnHeldBalance`):**
- Move `burn_coins` from the authority account into the `manifest` module account (`bankKeeper.SendCoinsFromAccountToModule`). Insufficient balance fails with `not enough balance to burn …`.
- Burn from the module account (`bankKeeper.BurnCoins`).

**CLI:**
```bash
manifestd tx manifest burn-coins [coins,...] --from <admin-key>
# Example (multi-denom burn):
manifestd tx manifest burn-coins 50000umfx,100othercoin --from authority
```

> Unlike `payout`, `burn-coins` does **not** strip underscores from amounts — it parses the argument with `sdk.ParseCoinsNormalized` directly. Pass plain digits (e.g. `50000umfx`); `50_000umfx` would fail with `invalid decimal coin expression`.

## State

`x/manifest` is **stateless**:

- `proto/liftedinit/manifest/v1/genesis.proto` defines `GenesisState {}` with no fields.
- `Keeper.InitGenesis` and `Keeper.ExportGenesis` are no-ops returning empty state.

There is one vestigial collection prefix in code:

```go
// x/manifest/types/keys.go
var ParamsKey = collections.NewPrefix(0)
```

It is reserved but never written or read. Future versions may either drop it or use it for an explicit params struct (e.g. payout caps).

## Queries

The module exposes **no queries**. `proto/liftedinit/manifest/v1/query.proto` is `service Query {}` with no methods. The `manifestd query manifest` parent command is still registered (it shows up in `manifestd query --help` for module-CLI wiring consistency), but it has no subcommands attached. To inspect minted/burned supply, query `x/bank` directly:

```bash
manifestd query bank total --denom umfx
manifestd query bank balances <authority-address>
```

## Events

`x/manifest` does not emit custom events. Standard SDK events from the underlying bank operations (`coin_received`, `coin_spent`, `mint`, `burn`, `transfer`) are emitted by `x/bank` as the keeper makes its calls. Indexers should key off those rather than expecting `manifest`-typed events.

## Errors

`x/manifest` does not register its own error codes (`errors.Register(ModuleName, …)` is not called anywhere in this module), so there are no `manifest`-typed numeric codes to match on. The handlers return errors in two layers:

1. **Outer wrapper** at the msg server (`x/manifest/keeper/msg_server.go`) — plain `fmt.Errorf` with stable prefix strings. The authority-mismatch check is a bare error; the validation and bank-keeper failures are wrapped with `%w` (preserving the underlying error chain). Substring-match the prefixes if you need to distinguish module-specific failure modes:

   | Source | Substring | When |
   |---|---|---|
   | `msg_server.go` | `invalid authority; expected …, got …` | `msg.authority` ≠ `keeper.authority` |
   | `msg_server.go` | `invalid payout message:` / `invalid burn held message:` | `Validate()` failed |
   | `msgs.go::Validate` | `payouts cannot be empty` / `burn coins cannot be empty` | empty list |
   | `msgs.go::Validate` | `duplicate address: …` | `MsgPayout` has the same recipient twice |
   | `msgs.go::Validate` | `coin cannot be zero for address: …` | zero-amount payout pair |
   | `msg_server.go::BurnHeldBalance` | `not enough balance to burn …` | authority balance < `burn_coins` |

2. **Wrapped underlying errors** carry structured codes from upstream — `cosmossdk.io/errors`-typed errors from `Msg*.Validate()` (e.g. wrapping `sdk.AccAddressFromBech32`, coin validation), and SDK-typed errors from the bank keeper (e.g. `sdkerrors.ErrInsufficientFunds` underneath `not enough balance to burn …`). Use `errors.Is` / `errors.As` after unwrapping if you need to react to a specific upstream code rather than the wrapper string.

## Module-account permissions

`x/manifest`'s module account holds `{Minter, Burner}` permissions, granted in `app/app.go`:

```go
manifesttypes.ModuleName: {authtypes.Minter, authtypes.Burner},
```

This is what makes the bank keeper accept `MintCoins(types.ModuleName, …)` and `BurnCoins(types.ModuleName, …)` calls from this keeper. No other module can drive these — the permission is scoped per module account.

> **Note:** the `mintkeeper.Keeper` is wired into `manifestkeeper.NewKeeper` and `manifestmodule.NewAppModule` but is never used by the public methods. This is dead-store, retained for potential future use. New callers should not assume the mint keeper does anything here.

## CLI reference

| Command | Purpose |
|---|---|
| `tx manifest payout [pairs]` | `MsgPayout` — see above |
| `tx manifest burn-coins [coins]` | `MsgBurnHeldBalance` — see above |

There are no `query manifest …` subcommands.

## Type URLs / amino names

| Type URL | Amino name |
|---|---|
| `/liftedinit.manifest.v1.MsgPayout` | `lifted/manifest/MsgPayout` |
| `/liftedinit.manifest.v1.MsgBurnHeldBalance` | `lifted/manifest/MsgBurnHeldBalance` |

`PayoutPair` (carried inside `MsgPayout`) uses the unusual amino name `lifted/manifest/payout-pair` (kebab-case). Both messages serialise their `Coin`s with amino encoding `legacy_coin` / `legacy_coins` for Ledger compatibility.

## Genesis

`GenesisState` is empty. There is no module-specific genesis tuning — adding the module to `app.ModuleManager` and configuring `keeper.authority` in `app.NewApp` is sufficient.

## Related modules

- [`x/poa`](https://github.com/strangelove-ventures/poa) — Provides the authority address used here. The PoA admin (or admin group) is the only valid `MsgPayout` / `MsgBurnHeldBalance` signer.
- `x/bank` — Where minted coins land and where burned coins are pulled from. All supply changes show up in standard bank events and `bank total`.
- [`x/sku`](../sku/README.md) and [`x/billing`](../billing/README.md) — These modules don't depend on `x/manifest`. They reuse the same authority pattern but maintain their own state.

## Spec

Earlier `spec/` notes ([01_concepts.md](./spec/01_concepts.md), [02_state.md](./spec/02_state.md)) covered subsets of this material; this README is the canonical reference and supersedes them.
