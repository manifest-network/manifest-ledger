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

Mainnet runs the latest non-pre-release tag from this repo. Each row below summarises the upgrade-impact pieces only — see the linked release notes for the full changelog.

| Version | Released | Major changes | Operator action |
|---------|----------|---------------|-----------------|
| `v1.0.0` | 2025-02-21 | First stable mainnet release. Cosmos SDK 0.50.12. | Initial install. |
| `v1.0.1` – `v1.0.14` | 2025-03 → 2026-01 | Patch releases: dependency bumps, pin fixes (e.g. `cosmossdk.io/x/tx`), CometBFT bumps. No state migrations. | Pre-stage tag, vote on upgrade proposal, swap binary. |
| `v2.0.0` | 2026-02-27 | **Breaking — billing v2.** Adds `x/sku` and `x/billing` modules; introduces `x/wasm`-permissioned providers; price/lease/credit semantics. New keepers wired into the upgrade handler (see `app.AppKeepers`). Reads PoA admin from `manifest1afk9zr2hn2jsac63h4hm60vl9z3e5u69gndzf7c99cqge3vzwjzsfmy9qj` per the seeded params. | **Required upgrade.** Pre-stage `v2.0.0`. Backup state before running. |
| `v2.0.1` | 2026-03-24 | Go 1.25.8 bump. No state changes. | Standard binary swap. |
| `v2.0.2` | 2026-04-08 | wasmd 0.54.2 → 0.54.3. No state changes. | Standard binary swap. |
| `v2.0.3` | 2026-04-16 | Determinism-under-load fixes; genesis-validation fixes (#150). No state migration but fixes a class of consensus-divergence under load — **prioritise this upgrade**. | Standard binary swap; recommended ASAP after release. |
| `v2.1.0` | 2026-04-30 | **Feature — per-item custom domains** (#152). Adds `MsgSetItemCustomDomain`, `LeaseItem.custom_domain`, `Params.reserved_domain_suffixes`, `Query/LeaseByCustomDomain`, and the `CustomDomainIndex` reverse-lookup. New error codes 29–33. Backward-compatible: existing leases keep `custom_domain == ""`. | Standard binary swap. After upgrade, authority should consider seeding `Params.reserved_domain_suffixes` for any provider wildcard zones via `update-params --reserved-domain-suffixes "..."`. |

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
