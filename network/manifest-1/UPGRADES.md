# manifest-1 Upgrade Runbook

This document covers how chain upgrades work on `manifest-1` and the running list of released versions. For first-time install, see [POST_GENESIS.md](./POST_GENESIS.md).

## How upgrades work on manifest-1

Upgrades are coordinated through the Cosmos SDK `x/upgrade` module. The flow:

1. The PoA authority (or PoA admin group) submits a `MsgSoftwareUpgrade` proposal naming the target version (e.g. `v2.1.0`) and a target block height.
2. At the target height, every validator's `manifestd` panics with `UPGRADE NEEDED`.
3. Each operator stops the old binary, swaps in the new one (cosmovisor automates this), and starts again.
4. The new binary detects the registered upgrade name, runs the matching upgrade handler (typically `RunMigrations`), and resumes block production.

### The "next" handler pattern

This repo uses a single generic upgrade handler — [`app/upgrades/next/upgrades.go`](../../app/upgrades/next/upgrades.go) — re-named per release at app boot:

```go
// app/upgrades.go
Upgrades = append(Upgrades, next.NewUpgrade(app.Version()))
```

The handler contains no release-specific state logic; it delegates to
`module.Manager.RunMigrations`. That is not necessarily a no-op. Whenever a
module raises its consensus version, the module manager runs each registered
migration from the stored version to the binary's current version. For the
ENG-859 upgrade from the live v2 baseline, billing runs v2→v3 and then v3→v4,
while SKU runs v1→v2. Those migrations perform non-trivial, deterministic state
rewrites even though the generic application handler remains unchanged.

The upgrade name on `manifest-1` is always the binary's version string (the
`git describe` output baked in at build time, e.g. `v2.1.0`). When voting on an
upgrade proposal, confirm `plan.name` equals the exact tag of the release you
intend to run. A release needs custom handler logic only for application-level
work outside registered module migrations. Store additions, deletions, or
renames also require explicit `StoreUpgrades`; ENG-859 changes existing values
and leaves `StoreUpgrades` empty.

`RunMigrations` executes inside the upgrade block's cached state. An earlier
migration can stage writes before a later migration returns an error. If the
handler fails, the upgrade block is not committed and those staged writes do
not become chain state. This atomic commit boundary—not an assumption that no
write was attempted—is what prevents a partial on-chain migration.

## cosmovisor (recommended)

Run `manifestd` under [cosmovisor](https://docs.cosmos.network/main/build/tooling/cosmovisor) so the binary swap at upgrade height is automatic.

### Layout

```
$DAEMON_HOME/                       # ~/.manifest by default
├── cosmovisor/
│   ├── current  -> genesis | upgrades/<name>
│   ├── genesis/
│   │   └── bin/
│   │       └── manifestd           # the launch-version binary
│   └── upgrades/
│       └── <upgrade_name>/         # e.g. v2.1.0
│           └── bin/
│               └── manifestd       # the post-upgrade binary
```

### Environment

```bash
# /etc/systemd/system/manifestd.service (or your shell rc)
Environment="DAEMON_NAME=manifestd"
Environment="DAEMON_HOME=/home/<user>/.manifest"
Environment="DAEMON_RESTART_AFTER_UPGRADE=true"
Environment="DAEMON_ALLOW_DOWNLOAD_BINARIES=false"   # security — pre-stage manually
Environment="UNSAFE_SKIP_BACKUP=false"               # keep snapshot backups
```

### Pre-staging the next binary

For each upcoming `<upgrade_name>` (= the next release tag):

```bash
# Build the target version locally
cd manifest-ledger
git fetch --tags
git checkout <upgrade_name>          # e.g. v2.2.0
make build

# Place it where cosmovisor expects it
mkdir -p $DAEMON_HOME/cosmovisor/upgrades/<upgrade_name>/bin
cp ./build/manifestd $DAEMON_HOME/cosmovisor/upgrades/<upgrade_name>/bin/

# Verify
$DAEMON_HOME/cosmovisor/upgrades/<upgrade_name>/bin/manifestd version
```

When the chain hits the upgrade height, cosmovisor stops the current process, switches `cosmovisor/current` to the new directory, and restarts.

## Released versions

Mainnet's `genesis_time` is **2024-04-10**, predating the formal `v1.x.x` tag series — the chain ran on `v0.0.1-rc.*` builds through early 2025. Each row below summarises the upgrade-impact pieces only; the full release notes (the `Description` field of each tag) live at <https://github.com/manifest-network/manifest-ledger/releases>. Mainnet runs the latest non-pre-release tag.

| Version | Released | Major changes | Operator action |
|---------|----------|---------------|-----------------|
| `v1.0.0` | 2025-02-21 | First formal `v1.x.x` tag. Security fix for ASA-2025-003. Cosmos SDK 0.50.12. **No change to chain handler logic** — the chain-code update is a libwasmvm requirement bump. Requires `libwasmvm` [v2.2.2](https://github.com/CosmWasm/wasmvm/releases/v2.2.2). | Coordinated upgrade from the prior RC binary. Update `libwasmvm` on the host alongside the binary swap. |
| `v1.0.1` | 2025-03-03 | Security fix for ASA-2025-004. Requires `libwasmvm` v2.2.2. | Coordinated upgrade; bump `libwasmvm`. |
| `v1.0.2` – `v1.0.3` | 2025-03 | Patch fixes / pin updates around `cosmossdk.io/x/tx`. | Standard binary swap. |
| `v1.0.4` | 2025-04-02 | **Behavioural change — IBC Transfers disabled** at this release. (No store migration; transfer messages are rejected at the application layer.) | Coordinated upgrade. After the swap, IBC `MsgTransfer` will fail until a future release re-enables it. |
| `v1.0.5` – `v1.0.6` | 2025-06 → 2025-08 | Dependency bumps. | Standard binary swap. |
| `v1.0.7` | 2025-08-18 | GitHub-org rename (`liftedinit` → `manifest-network`), Go and dependency bumps. Requires `libwasmvm` v2.2.4. | Coordinated upgrade; **bump `libwasmvm` to v2.2.4** (older v2.2.2 will fail to load). |
| `v1.0.8` – `v1.0.14` | 2025-08 → 2026-01 | Periodic dependency bumps (CometBFT, ibc-go, wasmd). Each release continues to require `libwasmvm` v2.2.4. No state migrations. | Coordinated binary swap per release. |
| `v2.0.0` | 2026-02-27 | **Breaking — billing v2** (#144). Adds `x/sku` and `x/billing` modules with new keepers wired into the upgrade handler (`app.AppKeepers`); bumps Cosmos SDK 0.50.12 → 0.50.14, wasmd 0.54.0 → 0.54.2, CometBFT 0.38.17 → 0.38.21, ibc-go 8.4.0 → 8.7.0; switches the `cosmos-sdk` replace directive from `liftedinit/cosmos-sdk` to `manifest-network/cosmos-sdk`. | **Required upgrade.** Pre-stage `v2.0.0`. Backup state before running. Plan downtime — the new module stores need to initialise. |
| `v2.0.1` | 2026-03-25 | Go 1.25.7 → 1.25.8 bump (#147). No state changes. | Standard binary swap. |
| `v2.0.2` | 2026-04-08 | wasmd 0.54.2 → 0.54.3 (#148). No state changes. | Standard binary swap. |
| `v2.0.3` | 2026-04-16 | Determinism-under-load fix and genesis-validation hardening (#150). No state migration but addresses a class of consensus divergence — **prioritise this upgrade**. | Standard binary swap; recommended ASAP after release. |
| `v2.1.0` | 2026-04-30 | **Feature — per-item custom domains** (#152). Adds `MsgSetItemCustomDomain`, `LeaseItem.custom_domain`, `Params.reserved_domain_suffixes`, `Query/LeaseByCustomDomain`, and the `CustomDomainIndex` reverse-lookup. New error codes 29–33. Backward-compatible: existing leases keep `custom_domain == ""`. | Standard binary swap. After upgrade, authority should consider seeding `Params.reserved_domain_suffixes` for any provider wildcard zones via `update-params --reserved-domain-suffixes "..."`. |
| `v2.2.0` | 2026-05-29 | Adds the provider UUID to billing custom-domain events (#155). No store migration. | Coordinated binary swap using plan name `v2.2.0`. |
| `v2.3.0` | 2026-07-08 | **Breaking query/API change — paginated provider-wide withdrawal** (ENG-475, #161). Adds opaque cursor pagination to bound provider-wide withdrawal work. | Required coordinated binary swap using plan name `v2.3.0`; update clients that consume the provider-withdrawable response. |
| `v2.3.1` | 2026-07-14 | Query-only credit-estimate fix: derives lease caps from current billing params (ENG-527, #168). No consensus-state migration. This is the live pre-ENG-859 baseline. | Coordinated binary swap using plan name `v2.3.1`. |

> **Upgrade-name discipline.** Every release's GitHub note records `Upgrade Handler Name: <tag>` exactly — `v1.0.0`, `v2.0.0`, `v2.1.0`, etc. When voting on an upgrade proposal, confirm `plan.name` (or `manifestd query upgrade plan`) matches the published `Upgrade Handler Name` byte-for-byte before staging the binary at `$DAEMON_HOME/cosmovisor/upgrades/<name>/bin/`.

### Where to learn about upcoming upgrades

- [GitHub releases](https://github.com/manifest-network/manifest-ledger/releases) — release notes, breaking-change call-outs, build artifacts.
- The Manifest Network Discord `#validators` channel — proposal heights, scheduled coordination calls, snapshot updates.
- `manifestd query upgrade plan` — the currently scheduled upgrade (if any) on the live chain.

## Standard upgrade procedure (TL;DR)

```bash
# 1. Read the release notes for breaking changes / migration impact:
#    https://github.com/manifest-network/manifest-ledger/releases/tag/<NEW_TAG>

# 2. Build & pre-stage the new binary under cosmovisor:
git fetch --tags
git checkout <NEW_TAG>
make build
mkdir -p $DAEMON_HOME/cosmovisor/upgrades/<NEW_TAG>/bin
cp ./build/manifestd $DAEMON_HOME/cosmovisor/upgrades/<NEW_TAG>/bin/

# 3. Confirm the proposal name on-chain matches <NEW_TAG>:
manifestd query gov proposal <PROPOSAL_ID>
manifestd query upgrade plan      # after the proposal passes

# 4. (Optional) take a backup before the upgrade height:
tar czf manifest-pre-<NEW_TAG>.tar.gz -C $HOME .manifest/data

# 5. Wait. cosmovisor handles the swap at the upgrade height.

# 6. Verify:
manifestd version                  # should print <NEW_TAG>
manifestd status | jq '.sync_info.latest_block_height'   # should be advancing
```

### ENG-859 Billing v2→v4 and SKU v1→v2 Checklist

Use this checklist in addition to the release notes. Rehearse it against a copy
of current testnet or mainnet state before scheduling the production height.

Before the upgrade:

- Confirm every validator is on the announced source release and the stored
  module versions are `billing=2` and `sku=1`:

  ```bash
  manifestd query upgrade module-versions billing
  manifestd query upgrade module-versions sku
  ```

- Confirm the proposal name, candidate binary version, artifact checksum, and
  cosmovisor directory name match byte-for-byte.
- Confirm the live binary reports `2.2.4` and the candidate reports `2.2.8`
  from `manifestd query wasm libwasmvm-version`. The published archive and
  container embed the v2.2.8 static library. Operators compiling a dynamic
  binary from source must install the exact v2.2.8 shared library before the
  swap; an older host library is not an acceptable fallback.
- Confirm `manifestd version --long --output json | jq -r .go` reports a Go
  1.26.7 linux/amd64 build for the published archive. Go 1.25 is no longer a
  supported release line and must not be used for an operator-local rebuild.
- Take and verify a recoverable, height-labelled snapshot before the halt. Record
  the committed height and app hash from more than one validator.
- Run the candidate binary's deterministic offline
  [billing migration preflight](../../x/billing/docs/MIGRATION.md), then archive
  the height-labelled export and report together:

  ```bash
  manifestd genesis preflight-billing-v4 exported-genesis.json \
    > billing-v4-preflight.json
  jq '.tenants[] | select(.has_planned_reservation_change)' \
    billing-v4-preflight.json
  jq '.tenants[] | select(.expiring_modern_pending_lease_uuids | length > 0)' \
    billing-v4-preflight.json
  ```

  The command uses the same planner as the production migration. Verify its
  `source_chain_id` and `source_initial_height` against the archived export and
  recorded snapshot: the initial height is normally the committed export height
  plus one (or zero for a zero-height export). `input_genesis_time` is the
  original chain genesis timestamp preserved by SDK export, not the future
  upgrade time. For every tenant and denomination the report includes source,
  repaired pre-cutover, and planned post-cutover aggregates; pre/post opaque
  legacy-cohort allocations; and every modern ACTIVE lease's sorted
  nominal/planned amounts. Review every tenant with
  `has_planned_reservation_change`, including ACTIVE-only or legacy haircuts
  even when no PENDING lease expires.

  The report also compares the complete modern PENDING nominal sum with both
  the post-v2→v3 aggregate and the bank balance at the derived credit address.
  An aggregate shortfall is corruption and fails the preflight; investigate
  indexes and primary leases rather than manufacturing reservation metadata. A
  bank-only shortfall is reachable under v2 settlement and does not halt: if any
  denomination is short, all modern PENDING leases for that tenant expire
  atomically at the upgrade block. Record the exact sorted lease UUID cohorts so
  clients can be notified. `validate-genesis` cannot perform the bank
  comparison. Conversely, this reservation preflight does not run block-time or
  cross-module SKU-reference validation and is not a full InitGenesis check.
- Rerun the preflight against the final pre-upgrade export. Funding, new leases,
  and settlement after an earlier snapshot can change the result.
- Rehearse the exact source binary, candidate binary, and state snapshot on dev
  or testnet. Include upgrade, module-version checks, representative billing/SKU
  lifecycle tests, load tests, export, `validate-genesis`, and import/re-export.
  Store, instantiate, and execute a contract before the halt, then query and
  execute that same contract after the upgrade before uploading fresh code.
  This exercises wasmvm v2.2.4→v2.2.8 compiled-cache replacement and state
  continuity as well as post-upgrade validation.
  Verify that a tenant with one underbacked PENDING denomination loses its entire
  modern PENDING cohort, never an arbitrary subset, while other tenants are
  unaffected.
- Build and deploy compatible `manifest-admin`, `manifest-mcp-mono`, and Fred
  clients to dev before scheduling the production height. Exercise all billing
  and SKU list screens/resources with both cursor pagination and the bounded
  SDK-compatible offset/`count_total` path. Offset/exact-total scans fail closed
  above 20,000 rows, so bulk consumers must always support `next_key`.
  `CreditAccount` and `ProviderWithdrawable` remain cursor-only.
- Verify those clients preserve chain-returned identifiers as canonical
  lowercase UUIDv7 values when calling `LeasesByProvider`, `LeasesBySKU`, and
  `SKUsByProvider`. Empty fields keep their distinct `<field> cannot be empty`
  error; non-empty malformed, uppercase, or non-v7 values now fail with gRPC
  `InvalidArgument` instead of being treated as unknown. An unknown canonical
  UUIDv7 remains valid input and returns an empty page.
- Verify Fred does not sum separately queried `ProviderWithdrawable` pages:
  shared tenant balances make those estimates non-additive. It must query one
  forward transaction-sized page, submit the matching withdrawal, wait for the
  commit, and then re-query current state.
- Regenerate downstream protobuf clients before using
  `MsgUpdateProvider.clear_api_url`. The field is additive; older clients
  continue to preserve the URL because an absent field decodes as false, but
  they cannot request an explicit clear.
- Run the `manifest-loadtest` K6 and edge-case suites against dev, including
  concurrent 100-lease `ProviderWithdrawable` queries, bounded
  offset/`count_total` queries near their ceilings, upgrade-height traffic, and
  removal/recovery of one backend node. Record latency, memory, error rates,
  app hashes, and post-recovery catch-up before approving the mainnet release.

After the upgrade:

- Confirm all validators run the expected binary, advance beyond the upgrade
  height, and report the same app hash at the same committed height.
- Confirm stored module versions are exactly `billing=4` and `sku=2` with the
  two `module-versions` queries above.
- Confirm `manifestd query wasm libwasmvm-version` reports exactly `2.2.8`, and
  verify the pre-upgrade contract retains its state and can execute before
  accepting fresh contract uploads.
- Query every page of representative Provider, SKU, Lease, CreditAccount, and
  balance data. Public addresses must remain canonical Bech32 and records must
  be unchanged except for documented migration fields.
- Compare the observed expiration set with the preflight report. Every affected
  tenant's modern PENDING leases must be EXPIRED at the upgrade block with empty
  tranches; no unaffected tenant or partially preserved affected cohort is
  acceptable. The migration does not emit normal per-lease `lease_expired`
  events, so query the recorded UUIDs rather than relying on an event indexer.
  Notify clients that they may submit replacement leases under the post-upgrade
  prices, limits, and available credit.
- Repeat the rehearsal's raw-state checks. SKU Params/Provider and billing
  address-bearing values must use their tagged raw-byte formats; Bech32 account
  strings must not remain in those persisted values. Address indexes must still
  resolve the same records.
- Verify billing conservation for every account and denomination:
  `reserved_amounts = sum(live modern lease remaining_amounts) +
  unattributed_reserved_amounts`; live lease counts, historical cohort counts,
  bank backing, and secondary indexes must agree with primary state.
- From a stopped copy of the post-upgrade state, export genesis, run
  `manifestd validate-genesis`, import it into an isolated home, and confirm the
  imported node starts and re-exports successfully.

If migration fails, no upgrade block has committed. Keep the network halted and
follow the aborted-upgrade recovery procedure below; do not let individual
validators improvise a rollback or restart the old binary at the due plan. A
bank-only PENDING shortfall is not such a failure; a repaired aggregate shortfall
or another rejected invariant is.

## Rolling back

Rolling back a finalised upgrade is **not** safe — the chain has already produced blocks under the new binary. If you need to roll back:

- For an aborted upgrade (the new binary crashes before the upgrade block commits): **do not restart the old binary at the due height**. The scheduled plan is still present, so the old binary will halt on the same plan again; it cannot clear or replace that plan. Keep the network halted and coordinate either (a) a corrected binary that registers the exact same plan name and can complete the migration, or (b) restoration by every validator from the agreed pre-upgrade snapshot before governance schedules a replacement plan. Never let only a subset of validators restore or change binaries.
- For a post-finalisation rollback (catastrophic bug): you are in chain-halt territory. Coordinate on Discord — this needs a multi-validator restore from a pre-upgrade snapshot under a new upgrade proposal.

Always test upgrades on a local chain (`make ictest-chain-upgrade-local`) or
testnet first. The local target rebuilds and verifies the candidate image from
the working tree; `make ictest-chain-upgrade` is reserved for CI's
content-identified image artifact.
