# Mainnet Genesis

TODO:

- Update PoA Admin(s) from manifest10d07y265gmmuvt4z0w9aw880jnsr700jmq3jzm
- Remove manifest1hj5fveer5cjtn4wd6wstzugjfdxzl0xp8ws9ct once others are given genesis allocations

# Post Genesis Validators

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

- Linux (x86_64) or Linux (amd64)

### Dependencies

> Prerequisite: go1.23+, git, gcc, make, jq

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
git clone https://github.com/liftedinit/manifest-ledger.git
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
manifestd tendermint unsafe-reset-all
rm $HOME/.manifest/config/genesis.json
rm $HOME/.manifest/config/gentx/*.json
wget https://raw.githubusercontent.com/liftedinit/manifest-ledger/main/network/manifest-1/genesis.json -O $HOME/.manifest/config/genesis.json

# Give yourself 1POASTAKE for the genesis Tx signed
manifestd init "$MONIKER" --chain-id $CHAIN_ID --staking-bond-denom upoa
manifestd add-genesis-account $KEYNAME_ADDR 1000000upoa

# genesis transaction using all above variables
manifestd gentx $KEYNAME 1000000upoa \
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
echo $(manifestd tendermint show-node-id)@$(curl -s ifconfig.me):26656`
```

> Update minimum gas prices

```bash
# nano ${HOME}/.manifest/config/app.toml # minimum-gas-prices -> "0umfx"
sed -i 's/minimum-gas-prices = "0stake"/minimum-gas-prices = "0umfx"/g' ${HOME}/.manifest/config/app.toml
```

## Collect Gentx

After you create your gentx, you will need to submit it to the network. You can do this by creating a PR to the network repository with your gentx file, or by collecting all gentx files in the `~/.manifest/config/gentx` then running `manifestd genesis collect-gentxs` to collect all gentx files and create a new genesis file.

## Start your node

Start your node with the new genesis file `manifestd start`.
