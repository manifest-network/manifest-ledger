# Testnet Genesis

## Cosmos Multisig (testnet)

```sh
CHAIN_ID='obvious-1'

# Add keys for multisig
manifestd keys add alice-ledger --pubkey <alice-pubkey-here>
manifestd keys add bob-ledger --pubkey <bob-pubkey-here>

# Create multisig with those keys and name it
manifestd keys add alice-bob-multisig --multisig reece-testnet,reece-other --multisig-threshold 1

# Generate a Tx
manifestd tx bank send $(manifestd keys show alice-bob-multisig -a) manifest12wfd44kmcetyg98e7mt7zlp0ul4wnmg9yuuv6l 10000000umfx --generate-only --chain-id=$CHAIN_ID | jq . > tx.json

# both sign
manifestd tx sign --from $(manifestd keys show -a reece-testnet) --multisig $(manifestd keys show -a alice-bob-multisig) tx.json --sign-mode amino-json --chain-id=$CHAIN_ID >> tx-signed-alice.json
# and for bob if required

# combine into a single Tx
manifestd tx multisign --from alice-bob-multisig tx.json alice-bob-multisig tx-signed-alice.json tx-signed-bob.json --chain-id=$CHAIN_ID > tx_ms.json

# Anyone can Broadcast tx
manifestd tx broadcast ms/tx_ms.json --chain-id=$CHAIN_ID
```


# Post Genesis Validators
If you are a validator joining the network after the initial genesis launch, follow the [post genesis document here](./POST_GENESIS.md).

## Hardware Requirements
**Minimal**
* 4 GB RAM
* 100 GB SSD
* 3.2 x4 GHz CPU

**Recommended**
* 8 GB RAM
* 100 GB NVME SSD
* 4.2 GHz x6 CPU

**Operating System**
* Linux (x86_64) or Linux (amd64) Recommended Arch Linux

### Dependencies
>Prerequisite: go1.21+, git, gcc, make, jq

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

manifestd config set client chain-id obvious-1
```

### Generate keys
* `manifestd keys add [key_name]`
* `manifestd keys add [key_name] --recover` to regenerate keys with your BIP39 mnemonic to add ledger key
* `manifestd keys add [key_name] --ledger` to add a ledger key

# Validator setup instructions
## Genesis Tx:
```bash
# Validator variables
KEYNAME='validator' # your keyname
MONIKER='pbcups'
SECURITY_CONTACT="email@domain.com"
WEBSITE="https://domain.com"
MAX_RATE='0.20'        # 20%
COMMISSION_RATE='0.00' # 0%
MAX_CHANGE='0.01'      # 1%
CHAIN_ID='obvious-1'
PROJECT_HOME="${HOME}/.manifest"
KEYNAME_ADDR=$(manifestd keys show $KEYNAME -a)

# Remove old files if they exist
manifestd tendermint unsafe-reset-all
rm $HOME/.manifest/config/genesis.json
rm $HOME/.manifest/config/gentx/*.json

# Give yourself 1POASTAKE for the genesis Tx signed
manifestd init "$MONIKER" --chain-id $CHAIN_ID --staking-bond-denom poastake
manifestd add-genesis-account $KEYNAME_ADDR 1000000poastake

# genesis transaction using all above variables
manifestd gentx $KEYNAME 1000000poastake \
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