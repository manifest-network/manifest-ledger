# manifest-1 Upgrade Runbook

This document covers how chain upgrades work on `manifest-1` and the running list of released versions. For first-time install, see [POST_GENESIS.md](./POST_GENESIS.md).

## How upgrades work on manifest-1

Upgrades are coordinated through the Cosmos SDK `x/upgrade` module. The flow:

1. The configured upgrade authority submits a `MsgSoftwareUpgrade` transaction naming the target handler and block height. If the authority is a group policy, its members use that policy's proposal/execution flow.
2. With no matching handler in the running binary, every validator stops before committing the target height with `UPGRADE NEEDED`. The process and RPC server may remain alive while consensus is halted.
3. Each operator stops the old binary, swaps in the new one (cosmovisor automates this), and starts again.
4. The new binary detects the registered upgrade name, runs the matching upgrade handler (typically `RunMigrations`), and resumes block production.

### The "next" handler pattern

This repo uses a single generic upgrade handler — [`app/upgrades/next/upgrades.go`](../../app/upgrades/next/upgrades.go) — re-named per release at app boot:

```go
// app/upgrades.go
Upgrades = append(Upgrades, next.NewUpgrade(app.Version()))
```

The handler runs the registered module migrations. Its name is the binary's version string. The Makefile currently defaults `VERSION` to `v2.3.1`; it uses `git describe` only when `VERSION` is explicitly empty. Build each release with `make build VERSION=<upgrade-name>` and verify `manifestd version`; checking out a tag alone does not override the Makefile default. Confirm `plan.name` equals the published upgrade handler name byte-for-byte.

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
make build VERSION=<upgrade_name>

# Place it where cosmovisor expects it
mkdir -p $DAEMON_HOME/cosmovisor/upgrades/<upgrade_name>/bin
cp ./build/manifestd $DAEMON_HOME/cosmovisor/upgrades/<upgrade_name>/bin/

# Verify
$DAEMON_HOME/cosmovisor/upgrades/<upgrade_name>/bin/manifestd version
```

When the chain hits the upgrade height, cosmovisor stops the current process, switches `cosmovisor/current` to the new directory, and restarts.

## Selected release history

Mainnet's `genesis_time` is **2024-04-10**, predating the formal `v1.x.x` tag series — the chain ran on `v0.0.1-rc.*` builds through early 2025. This is a selected historical table, not a record of every release or of which binary is currently deployed. Full notes live at <https://github.com/manifest-network/manifest-ledger/releases>. A published release does not establish that its upgrade has been applied on-chain.

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
| [`v2.3.1`](https://github.com/manifest-network/manifest-ledger/releases/tag/v2.3.1) | 2026-07-14 | Derives credit-estimate lease caps from parameters (ENG-527); the release describes this as query-only. | Coordinated upgrade with handler `v2.3.1`. This is the source baseline observed for ENG-879, not a claim about the current deployment. |

Before an upgrade or rehearsal, record the running `manifestd version`, the last applied upgrade (`query upgrade applied <name>`), and any pending `query upgrade plan`. These are separate from release history. ENG-879 observed `v2.3.1` applied at height 7,554,200 on 2026-09-03; recheck those facts on the selected source node.

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
make build VERSION=<NEW_TAG>
mkdir -p $DAEMON_HOME/cosmovisor/upgrades/<NEW_TAG>/bin
cp ./build/manifestd $DAEMON_HOME/cosmovisor/upgrades/<NEW_TAG>/bin/

# 3. Confirm the committed upgrade plan name and height:
manifestd query upgrade plan

# 4. Before the upgrade height, stop the node and back up its complete home.
#    BACKUP_DIR must be outside DAEMON_HOME and accessible only to operators.
: "${DAEMON_HOME:?set the actual daemon home}"
: "${BACKUP_DIR:?set an external backup directory}"
sudo systemctl stop manifestd
# Confirm the service is inactive and no other process uses this home.
if systemctl is-active --quiet manifestd; then
  echo "manifestd is still running; refusing backup" >&2
  exit 1
fi
umask 077
mkdir -p "$BACKUP_DIR"
tar -czf "$BACKUP_DIR/manifest-pre-<NEW_TAG>.tar.gz" -C "$DAEMON_HOME" .
# This includes data/, wasm/, config/, signer state, and cosmovisor metadata.
tar -tzf "$BACKUP_DIR/manifest-pre-<NEW_TAG>.tar.gz" > /dev/null
sudo systemctl start manifestd

# 5. Wait. cosmovisor handles the swap at the upgrade height.

# 6. Verify:
manifestd version                  # should print <NEW_TAG>
manifestd status | jq '.sync_info.latest_block_height'   # should be advancing
```

## Recovering from a failed upgrade

Stop the affected node and preserve its failed home and logs. Repointing
`cosmovisor/current` to the old binary does not undo an upgrade: the pending plan
remains, so the old binary normally halts again at the same height.

- If the upgrade height has not committed, coordinate a corrected binary with a
  handler for the **same pending plan name**. Pre-stage and verify it before
  restarting. A failed handler or store loader may already have changed local
  files; determine whether a coordinated state restore is also necessary.
- If blocks have committed under the new binary, coordinate a forward fix or a
  validator-wide halt and restore of the same agreed pre-upgrade state. Include
  the application and Comet databases, Wasm bytecode, and required configuration;
  restoring one validator or changing a binary symlink is not a network rollback.

Never reset or roll back production signing state just to make an old snapshot
start. Recovery must account for every height already signed, including external
signers, to avoid conflicting signatures. The retained backup is recovery input,
not permission to restart a historical signing identity without coordination.

Always test upgrades on a local chain (`make local-image` + `make ictest-chain-upgrade`) or testnet first.

## Rehearsing against a copy of chain state

`manifestd in-place-testnet <new-chain-id> <operator-account-address>` converts a
disposable copy of a stopped node into a single-validator testnet and starts it.
It preserves application stores directly, so migration tests can exercise legacy
encodings and orphaned application records that a genesis export may normalize.
This is the ledger wiring for [ENG-879](https://linear.app/liftedinit/issue/ENG-879)
and [ENG-886](https://linear.app/liftedinit/issue/ENG-886). Four-validator promotion
and the deployment playbooks are separate follow-ups.

This branch pins [SDK `v0.50.14-liftedinit.2`](https://github.com/manifest-network/cosmos-sdk/releases/tag/v0.50.14-liftedinit.2),
which includes the [ENG-885 fix](https://github.com/manifest-network/cosmos-sdk/pull/4)
for extended commits and the cached genesis chain ID. The older
`v0.50.14-liftedinit.1` lacks these fixes and cannot start a fork with vote
extensions enabled. Keep extensions enabled and follow the
[SDK operator preparation instructions](https://github.com/manifest-network/cosmos-sdk/blob/v0.50.14-liftedinit.2/docs/docs/user/run-node/05-run-testnet.md),
including redirecting configured absolute paths into the disposable copy and
choosing a separate, writable address-book path. The fixed SDK does not establish
the fork binary's state compatibility with the source chain; verify that separately.

The ledger command checks isolation before conversion: the chain ID must differ
from the copied Comet state, and the local key must not appear in the source's
current, next, or last validator set. It also checks the allowed consensus key
type, local file signing, isolated peer settings, and the configured mutable
paths. Targets must stay inside the copied home and cannot alias other files
that the conversion reads, overwrites, or removes. Correct copied configuration
before retrying a rejected preflight; do not point it back at production files.

Prepare the copied home before running the command:

1. Use a fork base whose state transitions match the source binary. Changes on
   `main` are not automatically compatible with a released source chain. Choose
   the build version for the rehearsal mode below and verify
   `build/manifestd version`. The source baseline for this work is `v2.3.1`;
   recheck the source's last completed upgrade and any pending plan before each
   rehearsal. Merely setting the version does not establish state compatibility.
   A bare `go build` omits the required version ldflags.
2. Copy a stopped node's state, or state-sync a disposable node and allow it to
   block-sync several more blocks before stopping. The SDK needs a local full
   block and seen commit; a snapshot alone is insufficient. Keep a backup of the
   copied home so a failed conversion can be retried from the original copy.
   Include the source's `wasm/` bytecode: this application does not currently
   register the Wasm snapshot extension.
3. Install a complete, freshly generated local consensus key. Never use a
   production `priv_validator_key.json`. Keep the signing-state file present and
   replace its contents with `{"height":"0","round":0,"step":0}`. Use a fresh node
   key as well. Do this only in the stopped, disposable fork home. After preflight,
   the SDK removes the configured consensus WAL and its numbered rotations and
   clears pending source-chain evidence, preserving committed evidence history.
4. Give the fork a distinct chain ID, clear `persistent_peers` and `seeds`, disable
   peer exchange and state sync, and isolate its P2P network from production.
   The SDK clears the address book but does not clear configured peers or seeds.
5. Set `POA_ADMIN_ADDRESS` to the local operator's canonical lowercase **account**
   address before startup. Use an account with a local signing key; module
   accounts cannot serve as the funded operator. Retain that byte-identical value
   for **every ordinary restart, service/container launch and cosmovisor binary
   swap**, on every fork node.
   The command records the fork identity and expected authority in
   `<copied-fork-home>/in-place-testnet.json`. Startup rejects a missing or
   different authority for that marked home before opening the application.
   The marker must accompany the fork through every copy and binary swap.
   The simulation-only `POA_BYPASS_ADMIN_CHECK_FOR_SIMULATION_TESTING_ONLY`
   environment switch is rejected by the daemon.

`POA_ADMIN_ADDRESS` supplies keeper authority for POA, upgrades, consensus
parameters, auth/bank, staking/mint/distribution/slashing, governance/crisis/circuit,
IBC core/transfers/interchain accounts, Wasm/tokenfactory, and Manifest/SKU/billing.
It changes their authority-gated administration; it does not transfer the PWR
tokenfactory denom's stored group-policy authority or rewrite group membership.

For an **ordinary fork replay**, build the compatible source-state binary with
the source's last completed upgrade name and omit `--trigger-testnet-upgrade`:

```bash
make build VERSION=<last-completed-upgrade-name>
build/manifestd version
export POA_ADMIN_ADDRESS=<local-manifest-account-address>
build/manifestd in-place-testnet manifest-ledger-fork-1 "$POA_ADMIN_ADDRESS" \
  --home <copied-fork-home> --skip-confirmation --minimum-gas-prices 0umfx
```

In a normal daemon process, this application's sole upgrade handler is named
after its build version. Unless an upgrade is due on the first fork block,
`x/upgrade` requires a handler for the last completed upgrade during its startup
check. An ordinary replay retains any pending source plan; account for its halt
height when choosing the copied state and planning the rehearsal.

The initializer replaces all source validators, including jailed/unbonded records,
consensus and power indices, delegation records and unbonding/redelegation queues.
It clears POA pending validators and update caches, and removes source validators'
slashing records, missed-block bitmaps and public-key mappings while preserving
slashing parameters. It then installs the local key with fresh distribution/slashing
records and a synthetic self-delegation. Staking pools and bank supply are adjusted
through the bank keeper. Removed validators' rewards
are reassigned to the community pool and their distribution histories are cleared.
The operator receives
`1000000000000umfx` through a transfer that also creates its x/auth account.
Other application state is retained; this is intentionally a modified staking
environment, not a reproduction of the source validator/delegator economics.

Cleanup copies at most 256 keys at a time, but the application cache still holds
the full rewrite until it succeeds. Memory requirements therefore depend on the
source's live delegation/reward/slashing records. No peak-RSS claim for an actual
mainnet snapshot has been established by the synthetic tests; measure it on the
selected stopped copy before sizing a production-state rehearsal host.

Initial voting power is `900000000000000`, matching the SDK's CometBFT replacement
set; staking tokens include the chain's power reduction factor. The initializer
disables PoA validator creation, power changes, and removal through the on-chain
circuit breaker, including messages dispatched by authz or group execution. It
also disables circuit-reset transactions so these guards cannot be removed by a
transaction. Other existing circuit restrictions remain in effect. This fork
supports a fixed single validator throughout its lifetime. The pinned POA module's
unsafe power-reduction path leaves a stale power index and surplus bonded-pool
tokens, and removal narrows its `9e20` tokens to an overflowing `int64`, so it
cannot safely normalize or rotate this validator. Multi-validator promotion
requires the accounting repair and transition checks tracked in
[ENG-945](https://linear.app/liftedinit/issue/ENG-945).

Wait for committed height to advance at least two blocks beyond the copied
height. Check the new network ID, a single expected validator, an existing
`query auth account <operator>` result and funded bank balance. Verify that
`query staking pool` reports bonded tokens equal to the validator's tokens and
zero unbonded tokens, and that the operator's sole self-delegation has matching
shares and balance. After a clean stop, restart with the same binary and authority:

```bash
export POA_ADMIN_ADDRESS=<same-local-manifest-account-address>
build/manifestd start --home <copied-fork-home> --minimum-gas-prices 0umfx
```

In another terminal, verify further block progression and run
`build/manifestd query upgrade authority --node <fork-rpc-url>`; the returned
address must still equal the fork operator. Put `POA_ADMIN_ADDRESS` in the
service's persistent environment as well, rather than relying on an earlier
interactive shell export. Repeat the authority query after every restart or
binary swap. Run `in-place-testnet` only once per copied home; the first fork
block persists the application rewrite.

Conversion is not a transaction across all files and databases. The marker is
written as incomplete before conversion and becomes complete only after the
first successful fork application commit. A restart also checks persisted
Comet/application agreement. If initialization, commit, or the process fails and
the marker remains incomplete or the stored identities disagree, discard that
working copy and prepare a fresh one from the stopped source backup. Do not
delete/edit the marker or repair signing state to retry an interrupted conversion.

For a **forced first-block migration**, start from a fresh prepared copy and
build the target migration code under an upgrade name that has **never completed
in that copied state**. For example, after confirming the following rehearsal
name is unused:

```bash
TARGET_UPGRADE=eng-879-migration-rehearsal-1
make build VERSION="$TARGET_UPGRADE"
build/manifestd version
export POA_ADMIN_ADDRESS=<local-manifest-account-address>
build/manifestd in-place-testnet manifest-ledger-fork-1 "$POA_ADMIN_ADDRESS" \
  --home <fresh-copied-fork-home> --skip-confirmation --minimum-gas-prices 0umfx \
  --trigger-testnet-upgrade "$TARGET_UPGRADE"
```

The flag and binary version must match. Reusing the last completed upgrade name
fails with `upgrade with name ... has already been completed`; unknown handlers
and skipped trigger heights are rejected before conversion. The initializer schedules the new plan
at copied application height + 1, replacing any pending source plan. Because it
is due on the first fork block, `x/upgrade` skips the old completed-handler check
and executes the target handler. Do not mark that height in
`--unsafe-skip-upgrades`. Verify `query upgrade applied "$TARGET_UPGRADE"` at the
fork RPC reports the expected height, and retain the target-version binary for
ordinary restarts after the migration.

This executes the target code's module migrations against the copied module
version map; changing the version label alone does not add a migration. The
target application must load the source stores before the initializer can
schedule anything. The flag cannot repair incompatible state encodings or
substitute for startup store-loader work needed by added, deleted or renamed
stores. Rehearse those changes with their required store loader and binary swap.

For a **real halt and binary swap**, first establish the ordinary fork replay
above under the compatible source version, without the trigger flag. Build the
target code in a separate checkout with
`make build VERSION=<unused-target-upgrade-name>` and pre-stage that binary in
the fork's `cosmovisor/upgrades/<unused-target-upgrade-name>/bin/` directory, or
keep it separately for a manual swap.

The pinned SDK's `tx upgrade software-upgrade` helper creates a governance
`MsgSubmitProposal`, including with `--generate-only`; it does not submit a direct
`MsgSoftwareUpgrade`. The direct AutoCLI command is disabled. With the fork
operator as upgrade authority, use the complete transaction recipe below while
the source binary is still running. It requires Bash and `jq`.

Set these values to the running fork and its existing operator key. The `test`
keyring backend below is for the disposable test key; select the backend that
actually holds that key. Zero fees match this runbook's `0umfx` minimum gas price;
set a sufficient nonzero fee if the fork requires one. Fees and gas are encoded
in the unsigned transaction.

```bash
MANIFESTD="$(pwd)/build/manifestd"
FORK_HOME="/absolute/path/to/copied-fork-home"
FORK_CHAIN_ID="manifest-ledger-fork-1"
FORK_RPC="tcp://127.0.0.1:26657"
OPERATOR_KEY="fork-operator"
KEYRING_BACKEND="test"
UPGRADE_NAME="eng-879-swap-rehearsal-1" # unused name matching the staged target binary
FEE_DENOM="umfx"
FEE_AMOUNT="0"
GAS_LIMIT="200000"
export POA_ADMIN_ADDRESS="<same-local-manifest-account-address>"
```

The following signs online, fetching the account number and sequence from the
fork RPC. Do not send another transaction from this key between signing and
broadcasting. The example schedules 100 blocks ahead; pre-stage the target
first and increase that interval if needed. `--output-document` keeps the signed
JSON separate from console diagnostics.

```bash
set -euo pipefail
OPERATOR_ADDRESS=$("$MANIFESTD" keys show "$OPERATOR_KEY" -a \
  --home "$FORK_HOME" --keyring-backend "$KEYRING_BACKEND")
test "$OPERATOR_ADDRESS" = "$POA_ADMIN_ADDRESS"
"$MANIFESTD" query upgrade authority --home "$FORK_HOME" --node "$FORK_RPC" --output json \
  | jq -e --arg authority "$POA_ADMIN_ADDRESS" '.address == $authority'
"$MANIFESTD" query upgrade applied "$UPGRADE_NAME" --home "$FORK_HOME" --node "$FORK_RPC" --output json \
  | jq -e '((.height // "0") | tonumber) == 0'
CURRENT_HEIGHT=$("$MANIFESTD" status --home "$FORK_HOME" --node "$FORK_RPC" \
  | jq -r '.sync_info.latest_block_height // .SyncInfo.latest_block_height')
UPGRADE_HEIGHT=$((CURRENT_HEIGHT + 100))
TX_DIR=$(mktemp -d "$FORK_HOME/upgrade-tx.XXXXXX")

jq -n --arg authority "$POA_ADMIN_ADDRESS" --arg name "$UPGRADE_NAME" \
  --arg height "$UPGRADE_HEIGHT" --arg denom "$FEE_DENOM" \
  --arg amount "$FEE_AMOUNT" --arg gas "$GAS_LIMIT" '{
    body: {
      messages: [{
        "@type": "/cosmos.upgrade.v1beta1.MsgSoftwareUpgrade",
        authority: $authority,
        plan: {name: $name, height: $height, info: "fork upgrade rehearsal"}
      }],
      memo: "", timeout_height: "0",
      extension_options: [], non_critical_extension_options: []
    },
    auth_info: {
      signer_infos: [],
      fee: {
        amount: (if $amount == "0" then [] else [{denom: $denom, amount: $amount}] end),
        gas_limit: $gas, payer: "", granter: ""
      }
    },
    signatures: []
  }' > "$TX_DIR/unsigned.json"

"$MANIFESTD" tx sign "$TX_DIR/unsigned.json" \
  --home "$FORK_HOME" --from "$OPERATOR_KEY" --keyring-backend "$KEYRING_BACKEND" \
  --chain-id "$FORK_CHAIN_ID" --node "$FORK_RPC" --sign-mode direct \
  --output-document "$TX_DIR/signed.json"
"$MANIFESTD" tx broadcast "$TX_DIR/signed.json" \
  --home "$FORK_HOME" --chain-id "$FORK_CHAIN_ID" --node "$FORK_RPC" \
  --broadcast-mode sync --output json > "$TX_DIR/broadcast.json"
jq -e '.code == 0' "$TX_DIR/broadcast.json"
TX_HASH=$(jq -r '.txhash' "$TX_DIR/broadcast.json")

# Sync broadcast only checks admission; verify the committed execution result.
for attempt in {1..30}; do
  if "$MANIFESTD" query tx "$TX_HASH" --home "$FORK_HOME" --node "$FORK_RPC" \
    --output json > "$TX_DIR/committed.json" 2> "$TX_DIR/query-tx.err"; then
    break
  fi
  sleep 1
done
test -s "$TX_DIR/committed.json"
jq -e --arg hash "$TX_HASH" \
  '.txhash == $hash and .code == 0 and ((.height | tonumber) > 0)' "$TX_DIR/committed.json"
"$MANIFESTD" query upgrade plan --home "$FORK_HOME" --node "$FORK_RPC" --output json \
  > "$TX_DIR/plan.json"
jq -e --arg name "$UPGRADE_NAME" --arg height "$UPGRADE_HEIGHT" \
  '.plan.name == $name and (.plan.height | tostring) == $height' "$TX_DIR/plan.json"
```

The source binary must not register the target handler: it halts at that height and writes
`data/upgrade-info.json`. Start the pre-staged target binary only at this halt;
its version must equal the plan name. Starting it early fails the upgrade
checks. After the swap, verify the applied height, block progression and upgrade
authority, keeping the same `POA_ADMIN_ADDRESS` throughout. Ensure
`manifestd status --home <copied-fork-home>` reaches the fork RPC through
`config/client.toml`: cosmovisor uses that command without an explicit `--node`.

Regression entry points are `go test ./cmd/manifestd/cmd`, `make ictest-unit`, and
`make ictest-poa-unjail-dup`. Build both local images before the fork scenarios:

```bash
make local-image-coverage local-image-testnet-upgrade
make ictest-in-place-testnet
```

The fork tests exercise vote extensions, rejected isolation inputs, committed
state, and ordinary restart. The released-source scenario seeds synthetic state
with the published v2.3.1 image, includes Wasm code, schedules an upgrade, observes
the halt, and swaps to a distinct target image. That image alone enables the
`testnet_upgrade_fixture` build tag: its handler performs a measurable test-only
parameter migration, checked again after restart to catch repeat execution.
Normal production builds do not include that fixture. This verifies the release
and binary-swap mechanics; it does not replace rehearsing the actual target
migration against a real mainnet copy and measuring its resource requirements.
