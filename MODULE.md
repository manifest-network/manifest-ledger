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
        - [Update Parameters (update-params)](#update-parameters-update-params-1)
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

The Manifest module is responsible for handling inflation and manual minting events. Below is a structured breakdown of its components and functionalities:

### Module Functionality

Stakeholder Management: Allows the PoA admin to designate stakeholders, who can be one or multiple manifest wallet addresses. These stakeholders are eligible to receive assets issued by the PoA admin.

#### Asset Issuance:

- Manual Issuance: The PoA admin can manually mint and disburse a specified amount of tokens to the stakeholders.

- Automatic Inflation: When enabled, tokens are minted every block, aiming for a predetermined total over a year.

#### Commands

##### Update Parameters (update-params):

- Syntax: `manifestd tx manifest update-params [address:percent_share] [inflation_on_off] [annual_total_mint]`

  - Parameters:
    - `address:percent_share`: Specifies the wallet address and its share of the total rewards (to the ninth exponent).
    - `inflation_on_off`: A boolean value (true or false) to toggle automatic inflation.
    - `annual_total_mint`: The total amount of tokens to be minted annually.

  **Example:** `manifestd tx manifest update-params manifest1hj5fveer5cjtn4wd6wstzugjfdxzl0xp8ws9ct:100_000_000 false 500000000umfx`

##### Stakeholder Payout (stakeholder-payout):

- Syntax: `manifestd tx manifest stakeholder-payout [amount]`

  - Parameters:

    - `amount`: The amount of tokens to mint from the yearly allocated inflation.

    This command will fail if automatic inflation is enabled.

  **Example:** `manifestd tx manifest stakeholder-payout 777umfx`

## Proof of Authority Module

The PoA module is responsible for handling admin actions like adding and removing other administrators, setting the staking parameters of the chain, controlling voting power, and allowing/blocking validators. Below is a structured breakdown of its components and functionalities:

### Module Functionality

The PoA admin has several capabilities for managing the chain and its validators:

#### Validator Management:

- Remove validators from the active set.
- Remove validators pending addition to the active set.
- Specify the voting power for each validator.
- Approve the addition of new validators.

#### Staking Parameters Update:

- The PoA admin can update various staking parameters, including:
  - `unbondingTime`: The time period for unbonding tokens.
  - `maxVals`: The maximum number of validators.
  - `maxEntries`: The maximum number of entries.
  - `historicalEntries`: The number of historical entries to store.
  - `bondDenom`: The denomination for bonding and staking.
  - `minCommissionRate`: The minimum commission rate for validators.

#### Administrative Rights:

- Assign or revoke administrative privileges.
- Determine if validators have the ability to self-revoke.

#### Commands

##### Update Parameters (update-params):

- Syntax: `manifestd tx poa update-params [admin1,admin2,admin3,...] [allow-validator-self-exit-bool]`

  - Parameters:
    - `admin1,admin2,admin3,...`: A list of admin addresses which can be multisig addresses.
    - `allow-validator-self-exit-bool`: A boolean value (true or false) to allow validators to self-exit.

  **Example:** `manifestd tx poa update-params manifest1hj5fveer5cjtn4wd6wstzugjfdxzl0xp8ws9ct,manifest1hj5fveer5cjtn4wd6wstzugjfdxzl0xp8ws9ct false`

##### Update Staking Parameters (update-staking-params):

- Syntax: `manifestd tx poa update-staking-params [unbondingTime] [maxVals] [maxEntries] [historicalEntries] [bondDenom] [minCommissionRate]`

  - Parameters:

    - `unbondingTime`: The amount of time it takes to unbond tokens.
    - `maxVals`: The maximum number of validators.
    - `maxEntries`: The maximum number of entries.
    - `historicalEntries`: The number of historical entries to store.
    - `bondDenom`: The denomination for bonding and staking.
    - `minCommissionRate`: The minimum commission rate for validators.

  **Example:** `manifestd tx poa update-staking-params 1814400 100 7 1000 umfx 0.01`

##### Set Voting Power (set-power):

- Syntax: `manifestd tx poa set-power [validator] [power] [--unsafe]`

  - Parameters:

    - `validator`: The validator's operator address.
    - `power`: The voting power to give the validator.
    - `--unsafe`: A flag to allow the PoA admin to set the voting power of a validator that is not in the active set.

    **Example:** `manifestd tx poa set-power manifestvaloper1hj5fveer5cjtn4wd6wstzugjfdxzl0xp8ws9ct 1000000000 --unsafe`

##### Remove Pending Validator (remove-pending):

- Syntax: `manifestd tx poa remove-pending [validator]`

  - Parameters:

    - `validator`: The validator's operator address.

    **Example:** `manifestd tx poa remove-pending manifestvaloper1hj5fveer5cjtn4wd6wstzugjfdxzl0xp8ws9ct`

##### Remove Validator (remove):

- Syntax: `manifestd tx poa remove [validator]`

  - Parameters:

    - `validator`: The validator's operator address.

    **Example:** `manifestd tx poa remove manifestvaloper1hj5fveer5cjtn4wd6wstzugjfdxzl0xp8ws9ct`

## Token Factory Module

The Token Factory module as it is implemented on the Manifest Network, allows the PoA admin to have granular control over the creation and management of tokens on the Manifest Network. The admin can mint, burn, edit, and transfer tokens to other accounts from any account.

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
