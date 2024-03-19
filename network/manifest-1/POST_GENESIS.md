# Post-Genesis

## Become a validator

### Install the manifest binary

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

### Configure & start your node

- #### Intialize
  `manifestd init <moniker> --chain-id manifest-1 --default-denom poastake`
- #### Genesis
  `cp github.com/liftedinit/manifest-ledger/network/manifest-1/genesis.json ~/.manifestd/config/genesis.json`
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
Environment="POA_ADMIN_ADDRESS=manifest1nzpct7tq52rckgnvr55e2m0kmyr0asdrgayq9p"
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

Use this command to generate your validator file and change all the entries to your own information. `amount` should remain the same unless the team specifies otherwise. 1 POA power is 1000000poastake.

```bash
cat <<EOF > validator.json
{
  "pubkey": {"@type":"/cosmos.crypto.ed25519.PubKey","key":"oWg2ISpLF405Jcm2vXV+2v4fnjodh6aafuIdeoW+rUw="},
  "amount": "1000000poastake",
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

**Following these instructions, your validator will be put into a queue for the chain admins to accept or reject. Once accepted, you will be a validator on the network.
The chain admin's will set your amount if they accept.**
