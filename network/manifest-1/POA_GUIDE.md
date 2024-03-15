# Proof of Authority Cheat Sheet

Use this document to help you better understanding creating PoA admins and overseeing admin operations of the chain.

## Table of Contents

- [Multi Sig Wallets](#multi-sig-wallets)
- [Creating a PoA Admin](#creating-a-poa-admin)
- [Network Controls](#network-controls)

## Multi Sig Wallets

A multi-sig wallet is a type of wallet that mandates multiple signatures to authorize a transaction, enhancing security by ensuring that no individual can transfer funds without approval from other designated signers.

This multi-sig wallet will serve as the administrator for the Proof of Authority (PoA) network. Consequently, only this multi-sig wallet will have the authority to perform PoA administrative functions, necessitating X/X signatures for each action.

First you and the other members of the multisig will need to create a wallet. This can be done pre-genesis via the CLI.

`manifestd keys add <key-name>` you can optionally add `--ledger` to add a ledger key.

Now that you have a key you must add the pubkey(s) of the other multi sig members. In order to do this they must give you their pubkey. This can be accomplished by running `manifestd keys show <key-name> -p` and sending the output to the other members.

Once you have all the pubkeys you can add them by to your keyring by running `manifestd keys add <multi-sig-member-name> --pubkey '{"@type":"/cosmos.crypto.secp256k1.PubKey","key":"A57Cxv5vgwE6pAJ9oYtnOdU4ehKixMj6gufF8jBRq4IC"}'`.

Now that you have all the pubkeys you can create the multi-sig wallet by running `manifestd keys add <multi-sig-name> --multisig <comma,separated,list,of,multisig,keys,including,your,own> --multisig-threshold 1`. The threshold is the number of signatures required to authorize a transaction.

## Signing & Broadcasting a Multi Sig Transaction

In order to sign and broadcast a multi sig transaction you must first generate the transaction, then distribute it to the other members of the multi sig wallet. Once all members have signed the transaction it can be broadcast to the network.

In order to generate a transaction you can follow this sample command for staking tokens to a validator.
`manifestd tx staking delegate manifestvaloper1hj5fveer5cjtn4wd6wstzugjfdxzl0xpmwd35e 1umfx --generate-only --chain-id manifest-1 --from <multi-sig-name>`

This will print out a json object that you can distribute to the other members of the multi sig wallet.

Each member can put the json output from the above command in a file `transaction.json` and sign it by running `manifestd tx sign staking.json --from <multi-sig-memberkey-name>`

Then each member will take the output of their sign command and put it in a file `<key-name>.json` and then distribute the files to the member who will apply the last signature then broadcast the member.

Once the the member who will be broadcasting the transaction receives each multisig members key, they can run

```bash

```
