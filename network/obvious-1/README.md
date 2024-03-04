# Testnet Genesis

## Cosmos Multisig (testnet)

```sh
CHAIN_ID='obvious-1'

# Add keys for multisig
manifestd keys add chandrastation --pubkey '{"@type":"/cosmos.crypto.secp256k1.PubKey","key":"A9hZjm7++QBixsH4QTQadXPrnhVBDk+MPLE74U0/GoJp"}' # manifest1wxjfftrc0emj5f7ldcvtpj05lxtz3t2npghwsf
manifestd keys add reece-testnet --pubkey '{"@type":"/cosmos.crypto.secp256k1.PubKey","key":"A57Cxv5vgwE6pAJ9oYtnOdU4ehKixMj6gufF8jBRq4IC"}'  # manifest1aucdev30u9505dx9t6q5fkcm70sjg4rh7rn5nf

# Create multisig with those keys and name it
manifestd keys add obvious-1-multisig --multisig reece-testnet,chandrastation --multisig-threshold 1

# - address: manifest1nzpct7tq52rckgnvr55e2m0kmyr0asdrgayq9p
#   name: obvious-1-multisig
#   pubkey: '{"@type":"/cosmos.crypto.multisig.LegacyAminoPubKey","threshold":1,"public_keys":[{"@type":"/cosmos.crypto.secp256k1.PubKey","key":"A9hZjm7++QBixsH4QTQadXPrnhVBDk+MPLE74U0/GoJp"},{"@type":"/cosmos.crypto.secp256k1.PubKey","key":"A57Cxv5vgwE6pAJ9oYtnOdU4ehKixMj6gufF8jBRq4IC"}]}'
#   type: multi

# Generate a Tx
manifestd tx bank send $(manifestd keys show obvious-1-multisig -a) manifest1aucdev30u9505dx9t6q5fkcm70sjg4rh7rn5nf 10000000umfx --generate-only --chain-id=$CHAIN_ID | jq . > tx.json

# both sign
manifestd tx sign --from $(manifestd keys show -a reece-testnet) --multisig $(manifestd keys show -a obvious-1-multisig) tx.json --sign-mode amino-json --chain-id=$CHAIN_ID >> tx-signed-reece.json
# and for chandra station

# combine into a single Tx
manifestd tx multisign --from obvious-1-multisig tx.json obvious-1-multisig tx-signed-reece.json tx-signed-chandra.json --chain-id=$CHAIN_ID > tx_ms.json

# Anyone can Broadcast tx
manifestd tx broadcast tx_ms.json --chain-id=$CHAIN_ID
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
git checkout v0.0.1-alpha.1

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
manifestd init "$MONIKER" --chain-id $CHAIN_ID --default-denom poastake
manifestd genesis add-genesis-account $KEYNAME_ADDR 1000000poastake --append

# genesis transaction using all above variables
manifestd genesis gentx $KEYNAME 1000000poastake \
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
echo $(manifestd tendermint show-node-id)@$(curl -s ifconfig.me):26656
```