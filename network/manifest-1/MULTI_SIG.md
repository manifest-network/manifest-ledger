# Manifest Network Cheat Sheet

Use this document to help you better understanding creating PoA admins and overseeing admin operations of the chain.

## Table of Contents

- [Multi Sig Wallets](#multi-sig-wallets)
- [PoA Admin](#creating-a-poa-admin)
- [Inflation Controls](#inflation-controls)

## Multi Sig Wallets

A multi-sig wallet is a type of wallet that mandates multiple signatures to authorize a transaction, enhancing security by ensuring that no individual can transfer funds without approval from other designated signers.

This multi-sig wallet will serve as the administrator for the Proof of Authority (PoA) network. Consequently, only this multi-sig wallet will have the authority to perform PoA administrative functions, necessitating X/X signatures for each action.

### Setting Up a Multi-Sig Wallet

1. **Wallet Creation:** Each member of the multi-sig group must first create a wallet. This step can be executed pre-genesis using the CLI:

   ```bash
   manifestd keys add <key-name> [--ledger]
   ```

   The `--ledger` flag is optional for adding a ledger key.

2. **Sharing Public Keys:** Members need to share their public keys with the group. Obtain your public key using:

   ```bash
   manifestd keys show <key-name> -p
   ```

   Share the output with other members.

3. **Adding Public Keys:** Once you've collected all public keys, add them to your keyring:

   ```bash
   manifestd keys add <multi-sig-member-name> --pubkey '{"@type":"/cosmos.crypto.secp256k1.PubKey","key":"A57Cxv5vgwE6pAJ9oYtnOdU4ehKixMj6gufF8jBRq4IC"}'
   ```

4. **Creating the Multi-Sig Wallet:** With all public keys added, create the multi-sig wallet:

   ```bash
   manifestd keys add <multi-sig-name> --multisig <comma-separated-list-of-keys> --multisig-threshold <THRESHOLD>
   ```

   The threshold indicates the number of signatures required to authorize a transaction.

### Signing & Broadcasting a Multi-Sig Transaction

1. **Generate the Transaction:** Start by generating the transaction. For example, to stake tokens:

   ```bash
   manifestd tx manifest update-params manifest1aucdev30u9505dx9t6q5fkcm70sjg4rh7rn5nf:100_000_000 true 6000000000000umfx --from=obvious-1-multisig --chain-id obvious-1 --generate-only > tx.json
   ```

   This command creates a `tx.json` file to distribute to other wallet members.

2. **Signing the Transaction:** Each member signs the `tx.json`:

   ```bash
   manifestd tx sign tx.json --from=reece-testnet --chain-id=obvious-1 --multisig=obvious-1-multisig >> reece.json
   ```

   After signing, members pass their signed JSON files back to the transaction coordinator.

3. **Combining Signatures:** The coordinating member aggregates all signatures:

   ```bash
   manifestd tx multisign --from obvious-1-multisig tx.json obvious-1-multisig reece.json --chain-id obvious-1 > tx_ms.json
   ```

4. **Broadcasting the Transaction:** With all required signatures, the final transaction can be broadcast to the network:

   ```bash
   manifestd tx broadcast tx_ms.json --chain-id obvious-1
   ```

   You can utilize this example to build any other transaction type, just be sure to replace or add any flags as necessary.

## PoA Admin

Please refer to the [Module Documentation](../../MODULE.md) for more information on the PoA module and its operations.

Any of the transactions listed in the module documentation can be executed using the multi-sig wallet. The multi-sig wallet will be the only entity capable of executing these transactions. You must follow the process of creating, signing, and broadcasting a transaction as outlined in the previous section just be sure to replace the transaction type and flags as necessary.

## Inflation Controls

Please refer to the [Manifest Module Documentation](../../MODULE.md) for more information on controlling inflation via the Manifest module.

Any of the transactions listed in the module documentation can be executed using the multi-sig wallet. The multi-sig wallet will be the only entity capable of executing these transactions. You must follow the process of creating, signing, and broadcasting a transaction as outlined in the previous section just be sure to replace the transaction type and flags as necessary.
