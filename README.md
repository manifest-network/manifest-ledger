<h1 align="center">Manifest Ledger</h1>

<p align="center">
  <a href="#overview"><img src="https://raw.githubusercontent.com/cosmos/chain-registry/00df6ff89abd382f9efe3d37306c353e2bd8d55c/manifest/images/manifest.png" alt="Lifted Initiative" width="100"/></a>
</p>

<p align="center">
 <a href="https://codecov.io/gh/manifest-network/manifest-ledger" >
     <img src="https://codecov.io/gh/manifest-network/manifest-ledger/graph/badge.svg?token=s7zzdGQ7Gh"/>
 </a>
  <a href="https://goreportcard.com/report/github.com/manifest-network/manifest-ledger">
    <img src="https://goreportcard.com/badge/github.com/manifest-network/manifest-ledger" alt="Go Report Card"/>
  </a>
  <a href="https://discord.gg/kQkaJzxvk9">
    <img src="https://badgen.net/badge/icon/discord?icon=discord&label" alt="Discord"/>
  </a>
</p>

## Overview

The Manifest Network, built on the Cosmos SDK, is a blockchain tailored for decentralized AI infrastructure access. Two on-chain primitives — `x/sku` and `x/billing` — let providers list billable resources (compute SKUs) and let tenants fund credit accounts and lease those SKUs at locked-in per-second prices. Settlement is lazy: charges accrue continuously but only move tokens when a provider withdraws or a lease is closed.

The chain runs Proof of Authority (`x/poa`) consensus, with plans to evolve toward Proof of Stake on CometBFT.

## Table of Contents

- [Quickstart](#quickstart) — by role
  - [I want to run a node / become a validator](#i-want-to-run-a-node--become-a-validator)
  - [I'm a tenant — buying compute](#im-a-tenant--buying-compute)
  - [I'm a provider — selling compute](#im-a-provider--selling-compute)
  - [I'm a wallet / frontend integrator](#im-a-wallet--frontend-integrator)
- [System Requirements](#system-requirements)
- [Installation](#install--run)
- [Testing](#testing)
- [Helper](#helper)
- [Modules](./MODULE.md)
- [Validators](./network/manifest-1/POST_GENESIS.md)
- [Contributing](./CONTRIBUTING.md)
- [Security/Bug Reporting](./SECURITY.md)
- [JavaScript / TypeScript SDK](#javascript--typescript-sdk)
- [Frontend / Integrator Cookbook](./docs/FRONTEND.md)

## Quickstart

The chain has four primary audiences. Pick the path that matches what you're trying to do.

### I want to run a node / become a validator

Follow [POST_GENESIS.md](./network/manifest-1/POST_GENESIS.md) end-to-end — install, peers, state-sync/snapshots, systemd, and the PoA `create-validator` flow. Once running, see [UPGRADES.md](./network/manifest-1/UPGRADES.md) for the upgrade runbook.

### I'm a tenant — buying compute

You'll fund a **credit account** with tokens, then create **leases** against published SKUs. Charges accrue per-second against your credit; the provider withdraws as the lease runs.

```bash
# 0. Point the CLI at manifest-1 and pick your key. <KEY> is anything in `manifestd keys list`.
manifestd config set client chain-id manifest-1
manifestd config set client node https://rpc.example.org:443     # see chain-registry / Discord

# 1. Discover providers and active SKUs.
manifestd query sku providers --active-only
manifestd query sku skus --active-only
# Or all SKUs from one provider:
manifestd query sku skus-by-provider <PROVIDER_UUID>

# 2. Fund your own credit account. Anyone can fund any tenant — most often you fund yourself.
#    Match the denom to whatever the target SKU prices in (`base_price.denom`).
manifestd tx billing fund-credit $(manifestd keys show <KEY> -a) 1000000upwr --from <KEY>

# 3. Confirm the credit is reserved-aware (available_balances = balances - reserved_amounts).
manifestd query billing credit-account $(manifestd keys show <KEY> -a)

# 4. Create a lease. Format is sku_uuid:quantity[:service_name]; multiple items must share one provider.
manifestd tx billing create-lease <SKU_UUID>:1 --from <KEY>
# Stack deployment with named services:
manifestd tx billing create-lease <SKU_UUID>:1:web <SKU_UUID>:1:db --from <KEY>

# 5. Wait for the provider to acknowledge. Until then the lease is PENDING and can be cancelled.
manifestd query billing leases-by-tenant $(manifestd keys show <KEY> -a) --state pending
manifestd query billing lease <LEASE_UUID>

# 6. (Optional, v2.1.0+) Attach a custom domain to a lease item once it's PENDING or ACTIVE.
#    The 2nd argument is the item's `service_name`. Use "" for a 1-item legacy lease
#    (item.service_name == ""); use the actual service name for stack-mode leases.
manifestd tx billing set-item-custom-domain <LEASE_UUID> "" app.example.com --from <KEY>          # 1-item legacy lease
manifestd tx billing set-item-custom-domain <LEASE_UUID> web app.example.com --from <KEY>         # stack-mode, target the "web" item

# 7. When you're done, close it. Final settlement transfers accrued charges to the provider.
manifestd tx billing close-lease <LEASE_UUID> --reason "no longer needed" --from <KEY>
```

If you're building a wallet or dashboard rather than driving the CLI, use [`@manifest-network/manifestjs`](#javascript--typescript-sdk) — every message and query above has a generated TypeScript counterpart. The end-to-end cookbook lives at [`docs/FRONTEND.md`](./docs/FRONTEND.md).

**Deeper reading:** [`x/billing/README.md`](./x/billing/README.md), [`x/billing/docs/API.md`](./x/billing/docs/API.md), [`x/billing/docs/INTEGRATION.md`](./x/billing/docs/INTEGRATION.md) (provider authentication / ADR-036 sign-in flow).

### I'm a provider — selling compute

Provider registration is **authority-controlled** on `manifest-1`: you cannot self-register. Coordinate with the authority (or a member of `params.allowed_list`) to provision your provider record and SKUs. Once you're set up, your day-to-day flow is acknowledging, withdrawing, and closing.

**One-time setup (run by the authority on your behalf):**

```bash
# 1. Authority creates your provider record with your operator address and a payout address.
manifestd tx sku create-provider <YOUR_OP_ADDR> <YOUR_PAYOUT_ADDR> \
  --api-url https://api.your-provider.example \
  --from authority

# 2. Authority creates a SKU under your provider. unit: 1=hour, 2=day. Price is per unit.
manifestd tx sku create-sku <PROVIDER_UUID> "GPU Instance — A100 40GB" 1 3600upwr --from authority
```

Note your `PROVIDER_UUID` and each `SKU_UUID` from the events / response — they're how tenants will address you.

**Steady-state (you run these):**

```bash
# 3. Watch for pending leases against your provider. Filter by state, or subscribe to the
#    chain's CometBFT websocket on `lease_created` events for push-style notifications.
manifestd query billing leases-by-provider <PROVIDER_UUID> --state pending

# 4. Acknowledge (transitions PENDING → ACTIVE; billing starts now). Reject if you can't fulfil.
manifestd tx billing acknowledge-lease <LEASE_UUID> --from <PROVIDER_KEY>
manifestd tx billing reject-lease    <LEASE_UUID> --reason "out of capacity" --from <PROVIDER_KEY>

# 5. Provision off-chain. Tenants authenticate to your API per docs/INTEGRATION.md (ADR-036).

# 6. Withdraw accrued credit. Two modes: specific lease(s), or paginated provider-wide.
manifestd tx billing withdraw <LEASE_UUID> --from <PROVIDER_KEY>
manifestd tx billing withdraw --provider <PROVIDER_UUID> --limit 100 --from <PROVIDER_KEY>
# Provider-wide mode pages — repeat while `has_more: true` in the response.

# 7. Check what's currently withdrawable without claiming it:
manifestd query billing provider-withdrawable <PROVIDER_UUID>
manifestd query billing withdrawable <LEASE_UUID>

# 8. Close a lease when service ends (provider, tenant, or authority can close ACTIVE leases):
manifestd tx billing close-lease <LEASE_UUID> --reason "service ended" --from <PROVIDER_KEY>
```

**Auto-close behaviour:** if a tenant's credit runs out, the next `withdraw` or `close-lease` against the lease performs the final settlement (against whatever balance remains) and auto-closes it. You don't need to poll.

**Deeper reading:** [`x/sku/docs/PROVIDER_GUIDE.md`](./x/sku/docs/PROVIDER_GUIDE.md), [`x/sku/docs/SKU_GUIDE.md`](./x/sku/docs/SKU_GUIDE.md), [`x/billing/docs/INTEGRATION.md`](./x/billing/docs/INTEGRATION.md).

### I'm a wallet / frontend integrator

Use [`@manifest-network/manifestjs`](#javascript--typescript-sdk) — a generated TypeScript client whose `liftedinit.{billing,sku,manifest}.v1` namespaces cover every message and query above (and stay in lock-step with this repo's protos). End-to-end recipes for Keplr/Leap signing, message composition, query patterns, websocket event subscriptions, and the type-URL / amino-name reference live in [`docs/FRONTEND.md`](./docs/FRONTEND.md).

## System Requirements

**Minimal**

- 4 GB RAM
- 100 GB SSD
- 3.2 GHz x4 CPU

**Recommended**

- 8 GB RAM
- 100 GB NVME SSD
- 4.2 GHz x6 CPU

**Software Dependencies**

1. The Go programming language - <https://go.dev/>
2. Git distributed version control - <https://git-scm.com/>
3. Docker - <https://www.docker.com/get-started/>
4. GNU Make - <https://www.gnu.org/software/make/>

**Operating System**

- Linux (x86_64 with SSSE3 support) or Linux (arm64 with NEON support)

> Note: CosmWasm requires x86-64 processors to support SSSE3 instructions (Intel Core 2 or newer) or ARM64 processors with NEON support.

**Arch Linux:**

```
pacman -S go git gcc make
```

**Ubuntu Linux:**

```
sudo snap install go --classic
sudo apt-get install git gcc make jq
```

## Install & Run

Clone the repository from GitHub and enter the directory:

```bash
git clone https://github.com/manifest-network/manifest-ledger.git
cd manifest-ledger
```

Then run:

```bash
# build the base binary for interaction
make install
mv $GOPATH/bin/manifestd /usr/local/bin
manifestd

# build docker image for e2e testing
make local-image
```

## Testing

There are various make commands to run tests for the modules with custom implementations

**To test the Proof of Authority implementation run:**

```bash
make ictest-poa
```

**To test the Token Factory implementation run:**

```bash
make ictest-tokenfactory
```

**To test the Manifest module run:**

```bash
make ictest-manifest
```

**To test the IBC implementation run:**

```bash
make ictest-ibc
```

**To test the Proof of Authority implementation where the administrator is a group run:**

```bash
make ictest-group-poa
```

**To test the chain upgrade run:**

```bash
make ictest-chain-upgrade
```

**To Test cosmwasm functionality run:**

```bash
make ictest-cosmwasm
```

**To test the SKU module run:**

```bash
make ictest-sku
```

**To test the Billing module run:**

```bash
make ictest-billing
```

## Simulation

**To execute the full application simulation run:**

```bash
make sim-full-app
```

**To execute the application simulation after state import run:**

```bash
make sim-after-import
```

**To test the application determinism run:**

```bash
make sim-app-determinism
```

Append `-random` to the end of the commands above to run the simulation with a random seed, e.g., `make sim-full-app-random`.

## Coverage

To generate a coverage report for the modules run:

```bash
make local-image
make coverage
```

## JavaScript / TypeScript SDK

For frontend, wallet, dashboard, and explorer integrations, use [`@manifest-network/manifestjs`](https://github.com/manifest-network/manifestjs) — a generated TypeScript client covering all chain modules (`x/manifest`, `x/sku`, `x/billing`, `x/poa`, `x/tokenfactory`, `x/wasm`, IBC) plus the standard Cosmos SDK modules.

```bash
npm install @manifest-network/manifestjs
```

The package is regenerated from this repo's `proto/liftedinit/**/*.proto` definitions, so message names, field types, and signatures stay in lock-step with on-chain types. See the `manifestjs` repo for usage examples and the published types under `liftedinit.{manifest,sku,billing}.v1`.

For end-to-end recipes — query client, signing client, Keplr/Leap integration, message composition, websocket subscriptions, and the type-URL / amino-name reference — read [`docs/FRONTEND.md`](./docs/FRONTEND.md).

## Helper

There are scripts for testing, installing, and initializing. Use this section to help you navigate the various scripts and their use cases.

#### Node Initialization script

`scripts/test_node.sh`

This is a script to assist with initializing and configuring a node. Ensure you properly configure the environment variables within the script.

Also in this script are examples of how you could run it

```bash
POA_ADMIN_ADDRESS=manifest1hj5fveer5cjtn4wd6wstzugjfdxzl0xp8ws9ct CHAIN_ID="local-1" HOME_DIR="~/.manifest" TIMEOUT_COMMIT="500ms" CLEAN=true sh scripts/test_node.sh
CHAIN_ID="local-2" HOME_DIR="~/.manifest2" CLEAN=true RPC=36657 REST=2317 PROFF=6061 P2P=36656 GRPC=8090 GRPC_WEB=8091 ROSETTA=8081 TIMEOUT_COMMIT="500ms" sh scripts/test_node.sh
```

The succesful executation of these commands will result in 2 ibc connected instances of manifestd running on your local machine.

#### Upload Contract script

`scripts/upload_contract.sh`

This script is used to upload a contract to the network. It is used to upload the cosmwasm template contract to the network.

`sh scripts/upload_contract.sh`

> Running this script with no arguments will utilize the same environment variables as the test_node.sh script.
