# Post-Genesis

## Become a validator

### Hardware Requirements

**Minimal**

- 4 GB RAM
- 100 GB SSD
- 3.2 x4 GHz CPU

**Recommended**

- 8 GB RAM
- 100 GB NVME SSD
- 4.2 GHz x6 CPU

**Operating System**

- Linux (x86_64) or Linux (amd64) Recommended Arch Linux

### Dependencies

> Prerequisite: go1.24+, git, gcc, make, jq

**Arch Linux:**

```
pacman -S go git gcc make
```

**Ubuntu Linux:**

```
sudo snap install go --classic
sudo apt-get install git gcc make jq
```

### Install the manifest binary

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

### Configure & start your node

- #### Intialize
  `manifestd init <moniker> --chain-id manifest-1 --default-denom upoa`
- #### Genesis
  `cp github.com/manifest-network/manifest-ledger/network/manifest-1/manifest-1_genesis.json ~/.manifestd/config/genesis.json`
- #### Peers
  `sed -i 's/seeds = ""/seeds = "SEED_ADDRESS"/g' ${HOME}/.manifest/config/config.toml`
- #### Minimum Gas
  `sed -i 's/minimum-gas-prices = "0stake"/minimum-gas-prices = "0umfx"/g' ${HOME}/.manifest/config/app.toml`
- #### Start
  **Create a systemd service file**

```bash
cat <<EOF | sudo tee /etc/systemd/system/manifestd.service
[Unit]
Description=Manifest Node
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=${HOME}
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
```

**Start your service**

```bash
sudo systemctl enable manifestd
sudo systemctl daemon-reload
sudo sytemctl start manifestd && journalctl -u manifestd -f -o cat --no-hostname
```

### Join the network

- #### Generate your validator file

Use this command to generate your validator file and change all the entries to your own information. `amount` should remain the same unless the team specifies otherwise. 1 POA power is 1000000upoa.

```bash
cat <<EOF > validator.json
{
  "pubkey": {"@type":"/cosmos.crypto.ed25519.PubKey","key":"oWg2ISpLF405Jcm2vXV+2v4fnjodh6aafuIdeoW+rUw="},
  "amount": "1000000upoa",
  "moniker": "validator's name",
  "identity": "keybase-identity",
  "website": "validator's (optional) website",
  "security": "validator's (optional) security contact email",
  "details": "validator's (optional) details",
  "commission-rate": "0.1",
  "commission-max-rate": "0.2",
  "commission-max-change-rate": "0.01",
  "min-self-delegation": "1"
}
EOF

```

You can find your pubkey information by running `manifestd tendermint show-validator`

- #### Submit creation transaction
  `manifestd tx poa create-validator path/to/validator.json --from keyname`

**Following these instructions, your validator will be put into a queue for the chain admins to accept or reject.**. You can view this queue by running `manifestd q poa pending-validators`.

If accepted, you will become a validator on the network with the PoA admin's desired power for you.
