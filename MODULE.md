# Modules

## Table of Contents

- [Modules](#modules)
  - [Manifest Module](#manifest-module)
    - [Module Functionality](#module-functionality)
      - [Asset Issuance](#asset-issuance)
      - [Commands](#commands)
        - [Update Parameters (update-params)](#update-parameters-update-params)
        - [Stakeholder Payout (stakeholder-payout)](#stakeholder-payout-stakeholder-payout)
  - [Proof of Authority Module](#proof-of-authority-module)
    - [Module Functionality](#module-functionality-1)
      - [Validator Management](#validator-management)
      - [Staking Parameters Update](#staking-parameters-update)
      - [Administrative Rights](#administrative-rights)
      - [Commands](#commands-1)
        - [Update Staking Parameters (update-staking-params)](#update-staking-parameters-update-staking-params)
        - [Set Voting Power (set-power)](#set-voting-power-set-power)
        - [Remove Pending Validator (remove-pending)](#remove-pending-validator-remove-pending)
        - [Remove Validator (remove)](#remove-validator-remove)
  - [Token Factory Module](#token-factory-module)
    - [Module Functionality](#module-functionality-2)
      - [Token Minting](#token-minting)
      - [Token Burning](#token-burning)
      - [Token Administration](#token-administration)
      - [Metadata Management](#metadata-management)
      - [Commands](#commands-2)
        - [Burn (burn)](#burn-burn)
        - [Burn From (burn-from)](#burn-from-burn-from)
        - [Mint (mint)](#mint-mint)
        - [Change Admin (change-admin)](#change-admin-change-admin)
        - [Create Denom (create-denom)](#create-denom-create-denom)
        - [Force Transfer (force-transfer)](#force-transfer-force-transfer)
        - [Modify Metadata (modify-metadata)](#modify-metadata-modify-metadata)

## Manifest Module

The Manifest module is responsible for handling manual minting and coin burning events. Below is a structured breakdown of its components and functionalities:

### Module Functionality

- Manual Minting: The PoA admin can manually mint and disburse a specified amount of tokens.
- Manual Burning: The PoA admin can manually burn tokens from the PoA admin account.

#### Commands

##### Mint and disburse native tokens (payout):

- Syntax: `manifestd tx manifest payout [address:coin_amount,...]`

  - Parameters:

    - `address:coin_amount`: Pair of destination address and amount of coin to mint.

  **Example:** `manifestd tx manifest payout manifest1hj5fveer5cjtn4wd6wstzugjfdxzl0xp8ws9ct:777umfx,manifest1efd63aw40lxf3n4mhf7dzhjkr453axurm6rp3z:1000umfx`

##### Burn native tokens (burn-coins):

- Syntax: `manifestd tx manifest burn-coins [coins,...]`

  - Parameters:

    - `coins`: The amount of coins to burn from the POA admin account.

  **Example:** `manifestd tx manifest burn-coins 777umfx,1000othercoin`

## Proof of Authority Module

The PoA module is responsible for handling admin actions like adding and removing other administrators, setting the staking parameters of the chain, controlling voting power, and allowing/blocking validators. Below is a structured breakdown of its components and functionalities:

### Module Functionality

The PoA admin has several capabilities for managing the chain and its validators:

#### Validator Management:

- Remove validators from the active set.
- Remove validators pending addition to the active set.
- Specify the voting power for each validator.
- Approve the addition of new validators.

#### Administrative Rights:

- Assign or revoke administrative privileges.
- Determine if validators have the ability to self-revoke.

#### Commands

##### Update Staking Parameters (update-staking-params):

Updates the defaults of the staking module from the PoA admin. For most cases, this should never be touched when using PoA.

- Syntax: `manifestd tx poa update-staking-params [unbondingTime] [maxVals] [maxEntries] [historicalEntries] [bondDenom] [minCommissionRate]`

  - Parameters:

    - `unbondingTime`: The time period for tokens to move from a bonded to released state. Not applicable for Proof of Authority.
    - `maxVals`: The maximum number of validators in the active set who can sign blocks. Default is 100
    - `maxEntries`: The maximum number of unbonding entries a delegator can have during the unbonding time. Not applicable for Proof of Authority.
    - `historicalEntries`: The number of historical staking entries to account for. Not applicable for Proof of Authority.
    - `bondDenom`: The denomination for bonding and staking. Not applicable for Proof of Authority.
    - `minCommissionRate`: The minimum commission rate for validators to get a percent cut of fees generated. Not applicable for Proof of Authority.

  **Example:** `manifestd tx poa update-staking-params 1814400 100 7 1000 umfx 0.01`

##### Set Voting Power (set-power):

Update a validators vote power weighting in the network. A higher vote power results in more blocks being signed. This also accepts pending validators into the active set as an approval from the PoA admin.

- Syntax: `manifestd tx poa set-power [validator] [power] [--unsafe]`

  - Parameters:

    - `validator`: The validator's operator address.
    - `power`: The voting power to give the validator. This is relative to the total current power of all PoA validators on the network. Uses 10^6 exponent.

    **Example:** `manifestd tx poa set-power manifestvaloper1hj5fveer5cjtn4wd6wstzugjfdxzl0xp8ws9ct 1000000`

    **NOTE**: A network of 2 validators each with 1_000_000 power will have a total power of 2_000_000. So each have 50% of the network. If one validator increases, then the others network percentage decreases, but remains at the same fixed 1_000_000 power as before.

##### Remove Pending Validator (remove-pending):

In PoA networks, any user (validator) can submit to the chain a transaction to signal intent of becoming a chain validator. Since the PoA admin has the final say on who becomes a validator, they can remove any pending validators from the list who they wish not to add. This command is used to remove a pending validator from the list.

- Syntax: `manifestd tx poa remove-pending [validator]`

  - Parameters:

    - `validator`: The validator's operator address.

    **Example:** `manifestd tx poa remove-pending manifestvaloper1hj5fveer5cjtn4wd6wstzugjfdxzl0xp8ws9ct`

##### Remove Validator (remove):

If the PoA admin decides they no longer wish for a validator to be signing blocks on the network, they can forcably remove them from the active set for signing blocks. This command removes the validator from signing blocks.

- Syntax: `manifestd tx poa remove [validator]`

  - Parameters:

    - `validator`: The validator's operator address.

    **Example:** `manifestd tx poa remove manifestvaloper1hj5fveer5cjtn4wd6wstzugjfdxzl0xp8ws9ct`

## Token Factory Module

The Token Factory module as it is implemented on the Manifest Network, allows any user to have granular control over the creation and management of tokens on the Manifest Network. The creator can mint, burn, edit, and transfer tokens to other accounts from any account.

>_note:_ The module is designed to work with tokens created by the module itself.

### Module Functionality

#### Token Minting:

- Create a token with a specific denom
- Mint a token with a specific amount and denom to your account
- Mint a token with a specific amount and denom to another account

#### Token Burning:

- Burn a token with a specific amount and denom from your account
- Burn a token with a specific amount and denom from another account

#### Token Administration:

- Change the admin address for a factory-created denom
- Force transfer tokens from one address to another address

#### Metadata Management:

- Change the base metadata for a factory-created denom

#### Commands

##### Burn (burn):

- Syntax: `manifestd tx token-factory burn [amount]`

  - Parameters:
    - `amount`: The amount and denom of the token you would like to burn from your account.

  **Example:** `manifestd tx tokenfactory burn 1<denom>`

##### Burn From (burn-from):

- Syntax: `manifestd tx token-factory burn-from [address] [amount]`

  - Parameters:
    - `address`: The address of the account you would like to burn the tokens from.
    - `amount`: The amount and denom of the token you would like to burn.

  **Example:** `manifestd tx tokenfactory burn-from manifest1hj5fveer5cjtn4wd6wstzugjfdxzl0xp8ws9ct 1<denom>`

##### Mint (mint):

- Syntax: `manifestd tx token-factory mint [amount]`

  - Parameters:
    - `amount`: The amount and denom of the token you would like to mint to your account.

  **Example:** `manifestd tx tokenfactory mint 1<denom>`

##### Change Admin (change-admin):

- Syntax: `manifestd tx token-factory change-admin [denom] [new-admin-address]`

  - Parameters:
    - `denom`: The denom of the token that you would like to change the admin for.
    - `new-admin-address`: The new admin's wallet address.

  **Example:** `manifestd tx tokenfactory change-admin <denom> manifest1hj5fveer5cjtn4wd6wstzugjfdxzl0xp8ws9ct`

##### Create Denom (create-denom):

- Syntax: `manifestd tx token-factory create-denom [subdenom]`

> _note:_ the createor of the denom is the denoms admin.

- Parameters:
  - `subdenom`: The smallest denomination for your token e.g. udenom.

**Example:** `manifestd tx tokenfactory create-denom <subdenom>`

##### Force Transfer (force-transfer):

- Syntax: `manifestd tx token-factory force-transfer  [amount] [transfer-from-address] [transfer-to-address]`

  - Parameters:
    - `amount`: The amount and denom of the token you would like to transfer.
    - `transfer-from-address`: The address of the account you would like to transfer the tokens from.
    - `transfer-to-address`: The address of the account you would like to transfer the tokens to.

  **Example:** `manifestd tx tokenfactory force-transfer 1<denom> manifest1hj5fveer5cjtn4wd6wstzugjfdxzl0xp8ws9ct manifest1hj5fveer5cjtn4wd6wstzugjfdxzl0xp8ws9ct`

##### Modify Metadata (modify-metadata):

- Syntax: `manifestd tx token-factory modify-metadata [denom] [ticker-symbol] [description] [exponent]`

  - Parameters:
    - `denom`: The denom of the token you are modifying.
    - `ticker-symbol`: The ticker symbol for the token.
    - `description`: A description of the token.
    - `exponent`: The exponent for the token e.g. how many zeros.

  **Example:** `manifestd tx tokenfactory modify-metadata utoken TOKEN "A token" 6`
