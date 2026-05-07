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

The handler is a no-op that runs module migrations. Implication for operators: **the upgrade name on `manifest-1` is always the binary's version string** (the `git describe` output baked in at build time, e.g. `v2.1.0`). When voting on an upgrade proposal, confirm `plan.name` equals the tag of the release you intend to run.

> If a future release needs a non-trivial migration (state surgery, store renames, etc.), the team will replace the noop body in `app/upgrades/next/upgrades.go` for that release. Read the release notes carefully — a non-noop release may set `StoreUpgrades` and require operators to recompute disk-resident state.

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

## Rolling back

Rolling back a finalised upgrade is **not** safe — the chain has already produced blocks under the new binary. If you need to roll back:

- For an aborted upgrade (binary crashes before the chain advances past the upgrade height): point `cosmovisor/current` back at `genesis/` (or the previous upgrade dir), restart, and coordinate a re-proposal.
- For a post-finalisation rollback (catastrophic bug): you are in chain-halt territory. Coordinate on Discord — this needs a multi-validator restore from a pre-upgrade snapshot under a new upgrade proposal.

Always test upgrades on a local chain (`make local-image` + `make ictest-chain-upgrade`) or testnet first.
