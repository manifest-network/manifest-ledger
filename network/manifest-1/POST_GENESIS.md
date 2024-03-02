# Post-Genesis

### Become a validator
* Install the manifest binary
* manifestd init <moniker> --chain-id manifest-1 --default-denom poastake
* Replace your genesis with the public one found in this repo
* Find peers and seeds here.
* Update your minimum-gas-prices in the app.toml
* Start the node and sync up
* Once completed, `manifestd tx poa create-validator path/to/validator.json --from keyname`. This command shows the JSON needed.
```json
{
        "pubkey": {"@type":"/cosmos.crypto.ed25519.PubKey","key":"oWg2ISpLF405Jcm2vXV+2v4fnjodh6aafuIdeoW+rUw="},
        "amount": "1poastake", # ignored
        "moniker": "myvalidator",
        "identity": "keybase-identity",
        "website": "validator's (optional) website",
        "security": "validator's (optional) security contact email",
        "details": "validator's (optional) details",
        "commission-rate": "0.1",
        "commission-max-rate": "0.2",
        "commission-max-change-rate": "0.01",
        "min-self-delegation": "1" # ignored
}
```

Following these instructions, your validator will be put into a queue for the chain admins to accept or reject. Once accepted, you will be a validator on the network.
The chain admin's will set your amount if they accept.