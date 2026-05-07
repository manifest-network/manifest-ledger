# Mainnet Genesis

This document describes the genesis-ceremony flow for validators participating in the **initial** chain launch — i.e., signing a gentx that goes into the canonical `manifest-1_genesis.json` before the chain produces its first block. Mainnet has been live since 2024-04-10, so unless you are bootstrapping a new chain or testnet from this repository's `set-genesis-params.sh`, follow [POST_GENESIS.md](./POST_GENESIS.md) instead.

> The frozen mainnet genesis file is checked in at [`manifest-1_genesis.json`](./manifest-1_genesis.json). Post-genesis joiners download that file directly — they do **not** run the gentx flow below.

## Post Genesis Validators

If you are a validator joining the network after the initial genesis launch, follow the [post genesis document here](./POST_GENESIS.md).

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
pacman -S go git gcc make
```

**Ubuntu Linux:**

```
sudo snap install go --classic
sudo apt-get install git gcc make jq
```

## manifestd Installation Steps

```bash
# Clone git repository
git clone https://github.com/manifest-network/manifest-ledger.git
cd manifest-ledger
git checkout VERSION

make install # go install ./...
# For ledger support `go install -tags ledger ./...`
manifestd config set client chain-id manifest-1
```

OR

```bash
wget <link to manifest precompile>
chmod +x manifestd
mv manifestd /usr/local/bin
```

### Generate keys

- `manifestd keys add [key_name]`
- `manifestd keys add [key_name] --recover` to regenerate keys with your BIP39 mnemonic to add ledger key
- `manifestd keys add [key_name] --ledger` to add a ledger key

# Validator setup instructions

## Genesis Tx:

```bash
# Validator variables
KEYNAME='' # your keyname
MONIKER='' # your validator moniker
SECURITY_CONTACT="email@domain.com"
WEBSITE="https://domain.com"
MAX_RATE='0.20'        # 20%
COMMISSION_RATE='0.00' # 0%
MAX_CHANGE='0.01'      # 1%
CHAIN_ID='manifest-1'
PROJECT_HOME="${HOME}/.manifest"
KEYNAME_ADDR=$(manifestd keys show $KEYNAME -a)

# Remove old files if they exist and replace genesis.json
manifestd comet unsafe-reset-all
rm $HOME/.manifest/config/genesis.json
rm $HOME/.manifest/config/gentx/*.json
wget https://raw.githubusercontent.com/manifest-network/manifest-ledger/main/network/manifest-1/manifest-1_genesis.json -O $HOME/.manifest/config/genesis.json

# Give yourself 1POASTAKE for the genesis Tx signed.
# --default-denom (umfx) controls the local fee/gas denom; --staking-bond-denom (upoa)
# is the staking denom that must match the genesis app_state.staking.params.bond_denom.
manifestd init "$MONIKER" --chain-id $CHAIN_ID --default-denom umfx --staking-bond-denom upoa
manifestd genesis add-genesis-account $KEYNAME_ADDR 1000000upoa

# genesis transaction using all above variables
manifestd genesis gentx $KEYNAME 1000000upoa \
    --home=$PROJECT_HOME \
    --chain-id=$CHAIN_ID \
    --moniker="$MONIKER" \
    --commission-max-change-rate=$MAX_CHANGE \
    --commission-max-rate=$MAX_RATE \
    --commission-rate=$COMMISSION_RATE \
    --security-contact=$SECURITY_CONTACT \
    --website=$WEBSITE \
    --details=""

# Get that gentx data easily -> your home directory
cat ${PROJECT_HOME}/config/gentx/gentx-*.json

# get your peer
echo $(manifestd comet show-node-id)@$(curl -s ifconfig.me):26656
```

> Update minimum gas prices

```bash
# nano ${HOME}/.manifest/config/app.toml # minimum-gas-prices -> "0umfx"
sed -i 's/minimum-gas-prices = "0stake"/minimum-gas-prices = "0umfx"/g' ${HOME}/.manifest/config/app.toml
```

## Collect Gentx

After you create your gentx, you will need to submit it to the network. You can do this by creating a PR to the network repository with your gentx file, or by collecting all gentx files in `${HOME}/.manifest/config/gentx` then running `manifestd genesis collect-gentxs` to collect all gentx files and create a new genesis file.

## Start your node

Start your node with the new genesis file `manifestd start`.
