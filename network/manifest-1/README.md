# Manifest-1 Network Guide

This guide will assist you in setting up a Manifest-1 network. There are three primary methods to join the network:

1. [Genesis](./GENESIS.md): For validators joining at the network's inception, please refer to the genesis document.
2. [Post Genesis](./POST_GENESIS.md): For validators joining after the initial network launch, consult the post-genesis document.
3. [Custom Genesis](#custom-genesis): To create your genesis file, follow the custom genesis guide.

## [Genesis](./GENESIS.md)

This section is tailored for validators intending to participate in the genesis ceremony. Prospective validators must download the genesis file and adhere to the provided steps to join the network. The genesis file originates from the [set-genesis-params.sh](./set-genesis-params.sh) script, which facilitates the generation of a bespoke genesis file. This file allows for the specification of network parameters, such as the Proof of Authority (PoA) token denomination and PoA administrators.

## [Post Genesis](./POST_GENESIS.md)

This section addresses validators aiming to join the network post-initial launch. Such validators need to execute the outlined steps to generate their validator file and integrate into the network by submitting a join request to the PoA administrators.

## Custom Genesis

This section is for those who wish to construct their genesis file for the network, enabling the modification of PoA administrator details and other parameters. Utilizing the [set-genesis-params.sh](./set-genesis-params.sh) script, you can alter the PoA administrator, the PoA denomination, among other settings.

In the script, you will encounter the following lines:

```bash
update_genesis '.app_state["poa"]["params"]["admins"]=["manifest1wxjfftrc0emj5f7ldcvtpj05lxtz3t2npghwsf"]'
update_genesis '.app_state["staking"]["params"]["bond_denom"]="upoa"'
```

Modifying the quoted values allows you to customize them as desired. After making the changes, execute the script, and your revised genesis file will be located at `$HOME/.manifest/config/genesis.json`.

Following these adjustments, proceed to the [Genesis](./GENESIS.md) document to finalize your node setup and participate in the genesis ceremony or launch your own.
