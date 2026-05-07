# Post-Genesis: Joining manifest-1

This guide walks a validator (or full node) through joining the live `manifest-1` chain after launch. For the genesis-ceremony flow, see [GENESIS.md](./GENESIS.md). For the upgrade runbook, see [UPGRADES.md](./UPGRADES.md).

## Hardware Requirements

**Minimal**

- 4 GB RAM
- 100 GB SSD
- 3.2 GHz x4 CPU

**Recommended**

- 8 GB RAM
- 100 GB NVME SSD
- 4.2 GHz x6 CPU

**Operating System**

- Linux (x86_64 with SSSE3) or Linux (arm64 with NEON). CosmWasm requires SSSE3 / NEON — older CPUs will not run the binary.

### Dependencies

> Prerequisite: go1.25+, git, gcc, make, jq

**Arch Linux:**

```
pacman -S go git gcc make jq
```

**Ubuntu Linux:**

```
sudo snap install go --classic
sudo apt-get install git gcc make jq
```

## Install the manifestd binary

Pin to the version currently running on `manifest-1`. Mainnet's current version is published in the [GitHub releases](https://github.com/manifest-network/manifest-ledger/releases) — pick the latest non-pre-release tag.

```bash
git clone https://github.com/manifest-network/manifest-ledger.git
cd manifest-ledger
git checkout v2.1.0   # replace with the current mainnet tag

make install                       # go install ./...
# For ledger support:
# go install -tags ledger ./...

manifestd config set client chain-id manifest-1
```

OR download a published binary:

```bash
# Pull the matching binary from GitHub releases:
# https://github.com/manifest-network/manifest-ledger/releases/<tag>
chmod +x manifestd
sudo mv manifestd /usr/local/bin
```

## Generate keys

- `manifestd keys add [key_name]` — new key
- `manifestd keys add [key_name] --recover` — restore from BIP39 mnemonic
- `manifestd keys add [key_name] --ledger` — add a Ledger-backed key

## Initialise & configure your node

```bash
# --default-denom is the local fee/gas denom (umfx). The staking bond_denom
# (upoa) comes baked into mainnet's genesis.json — Cosmos SDK v0.50's `init`
# command doesn't expose a separate flag for it, so just `--default-denom`
# is what you pass here. The freshly-init'd genesis.json gets replaced by
# the canonical mainnet file in the next step anyway.
manifestd init <moniker> --chain-id manifest-1 --default-denom umfx
```

### Genesis

Replace the freshly-generated `genesis.json` with the canonical mainnet file:

```bash
wget https://raw.githubusercontent.com/manifest-network/manifest-ledger/main/network/manifest-1/manifest-1_genesis.json \
  -O ~/.manifest/config/genesis.json

manifestd genesis validate ~/.manifest/config/genesis.json
```

### Minimum gas prices

`manifest-1`'s gas/fee denom is `umfx`. Update `app.toml`:

```bash
sed -i 's/minimum-gas-prices = "0stake"/minimum-gas-prices = "0umfx"/g' \
  ~/.manifest/config/app.toml
```

### Peers

Add seeds and/or persistent peers in `~/.manifest/config/config.toml`. Authoritative sources for current peer values:

- The [`cosmos/chain-registry` entry for `manifest`](https://github.com/cosmos/chain-registry/tree/master/manifest) (`peers.seeds`, `peers.persistent_peers`).
- The Manifest Network Discord `#validators` channel.
- The latest [GitHub release notes](https://github.com/manifest-network/manifest-ledger/releases) when peers change at an upgrade boundary.

```bash
# Example shape — replace SEED1@... with values pulled from one of the sources above:
sed -i 's|seeds = ""|seeds = "SEED1@host1:26656,SEED2@host2:26656"|' \
  ~/.manifest/config/config.toml
```

### State-sync (optional, fastest path)

State-sync lets a fresh node skip block-by-block replay and snapshot directly to a recent height. The mainnet `app.toml` defaults disable this. Enable it like so (replace `RPC_SERVERS`, `TRUST_HEIGHT`, `TRUST_HASH` with values pulled from chain-registry / Discord / a trusted RPC operator):

```bash
SNAP_RPC1="https://rpc.example.org:443"
SNAP_RPC2="https://rpc.example.com:443"

# Trust height should be a recent block (typically `latest height - ~1000`).
# Trust hash is the block hash at that height — query from a trusted RPC:
#   curl -s "$SNAP_RPC1/block?height=<trust_height>" | jq -r '.result.block_id.hash'

cat <<EOF >> ~/.manifest/config/config.toml
# State-sync overrides
EOF

# Edit ~/.manifest/config/config.toml under [statesync]:
# enable = true
# rpc_servers = "$SNAP_RPC1,$SNAP_RPC2"
# trust_height = <TRUST_HEIGHT>
# trust_hash = "<TRUST_HASH>"
# trust_period = "168h0m0s"
```

> **Heads-up:** wasm contract state cannot be state-synced today (CosmWasm limitation as of wasmd 0.54). If contracts are critical for your node's role, prefer a snapshot or full replay.

### Snapshots (alternative to state-sync)

Snapshot tarballs let you bypass replay without state-sync's wasm caveat. Authoritative snapshot URLs are published by the Manifest Network team and partners — see the chain-registry entry, Discord `#validators`, or release notes for current locations. The shape is:

```bash
# Example shape — replace URL with a published mainnet snapshot:
wget -O - https://snapshots.example.org/manifest-1/manifest-1_latest.tar.lz4 \
  | lz4 -d \
  | tar -xv -C ~/.manifest
```

Always verify the publisher's checksum before extracting.

## Run as a systemd service

```bash
sudo tee /etc/systemd/system/manifestd.service > /dev/null <<EOF
[Unit]
Description=Manifest Node
After=network.target

[Service]
Type=simple
User=$(whoami)
WorkingDirectory=$HOME
Environment="POA_ADMIN_ADDRESS=manifest1wxjfftrc0emj5f7ldcvtpj05lxtz3t2npghwsf"
ExecStart=/usr/local/bin/manifestd start
Restart=on-failure
StartLimitInterval=0
RestartSec=3
LimitNOFILE=65535
LimitMEMLOCK=209715200

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl daemon-reload
sudo systemctl enable manifestd
sudo systemctl start manifestd
journalctl -u manifestd -f -o cat --no-hostname
```

> **Recommended:** run `manifestd` under [`cosmovisor`](https://docs.cosmos.network/main/build/tooling/cosmovisor) so chain upgrades happen automatically. See [UPGRADES.md](./UPGRADES.md) for the cosmovisor layout used on `manifest-1`.

## Become a validator

`manifest-1` runs **Proof of Authority** (`x/poa`). Submitting a `create-validator` puts you in the pending queue; the PoA admin (or admin group) must approve before you join the active set. Self-bonding is not required for PoA.

### Generate your validator file

`amount` should remain `1000000upoa` (= 1 POA power) unless the team specifies otherwise.

```bash
cat <<EOF > validator.json
{
  "pubkey": $(manifestd comet show-validator),
  "amount": "1000000upoa",
  "moniker": "<your validator name>",
  "identity": "<keybase identity>",
  "website": "<https://your.site>",
  "security": "<security contact email>",
  "details": "<short description>",
  "commission-rate": "0.1",
  "commission-max-rate": "0.2",
  "commission-max-change-rate": "0.01",
  "min-self-delegation": "1"
}
EOF
```

### Submit creation transaction

```bash
manifestd tx poa create-validator path/to/validator.json --from <keyname>
```

Your request enters a pending queue:

```bash
manifestd query poa pending-validators
```

If the PoA admin accepts, you'll appear in the active validator set with the power they assigned. If rejected (`manifestd tx poa remove-pending`), resubmit after addressing whatever they flagged.
