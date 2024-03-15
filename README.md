<h1 align="center">Manifest Ledger</h1>

<p align="center">
  <a href="#overview"><img src="https://avatars.githubusercontent.com/u/90303796?s=200&v=4" alt="Lifted Initiative" width="100"/></a>
</p>

<p align="center">
  <a href="https://codecov.io/gh/liftedinit/manifest-ledger">
    <img src="https://codecov.io/gh/liftedinit/manifest-ledger/branch/reece/codecov/graph/badge.svg" alt="codecov"/>
  </a>
  <a href="https://goreportcard.com/report/github.com/liftedinit/manifest-ledger">
    <img src="https://goreportcard.com/badge/github.com/liftedinit/manifest-ledger" alt="Go Report Card"/>
  </a>
  <a href="https://discord.gg/kQkaJzxvk9">
    <img src="https://badgen.net/badge/icon/discord?icon=discord&label" alt="Discord"/>
  </a>
</p>

## Overview

The Manifest Network, built on the Cosmos SDK, is a blockchain tailored for decentralized AI infrastructure access. Initially employing a Proof of Authority (PoA) model it ensures a secure and efficient launch phase, with a trusted validator set managing consensus.

While PoA offers immediate stability and control, the Manifest Network aspires for greater decentralization. The future roadmap includes evolving towards a Proof of Stake (PoS) mechanism, utilizing the underlying CometBft algorithm inherent in the Cosmos SDK.

## Table of Contents

- [System Requirements](#system-requirements)
- [Installation](#install--run)
- [Testing](#testing)
- [Helper](#helper)
- [Modules](#modules)
- [Contributing](CONTRIBUTING.md)
- [Security/Bug Reporting](SECURITY.md)
- [Project Documentation]() #add docs link

## System Requirements

**Minimal**

- 4 GB RAM
- 100 GB SSD
- 3.2 x4 GHz CPU

**Recommended**

- 8 GB RAM
- 100 GB NVME SSD
- 4.2 GHz x6 CPU

**Software Dependencies**

1. The Go programming language - <https://go.dev/>
2. Git distributed version control - <https://git-scm.com/>
3. Docker - <https://www.docker.com/get-started/>
4. GNU Make - <https://www.gnu.org/software/make/>

**Operating System**

- Linux (x86_64) or Linux (amd64) Recommended Arch Linux

**Arch Linux:**

```
pacman -S go git gcc make
```

**Ubuntu Linux:**

```
sudo snap install go --classic
sudo apt-get install git gcc make jq
```

## Install & Run

Clone the repository from GitHub and enter the directory:

```bash
    git clone https://github.com/liftedinit/manifest-ledger.git
    cd manifest-ledger
```

Then run:

```bash
    make install
    mv $GOPATH/bin/manifestd /usr/local/bin
    manifestd
```

## Testing

There are various make commands to run tests for the modules with custom implementations

**To test the Proof of Authority implementation run:**

```bash
make ictest-poa
```

**To test the Token Factory implementation run:**

```bash
make ictest-tokenfactory
```

**To test the Manifest module which includes inflation changes run:**

```bash
make ictest-manifest
```

**To test the IBC implementation run:**

```bash
make ictest-ibc
```

## Modules

### Manifest Module

The Manifest module is responsible for handling inflation and manual minting events. Below is a structured breakdown of its components and functionalities:

#### Module Functionality

Stakeholder Management: Allows the PoA admin to designate stakeholders, who can be one or multiple manifest wallet addresses. These stakeholders are eligible to receive assets issued by the PoA admin.

**Asset Issuance:**

- Manual Issuance: The PoA admin can manually mint and disburse a specified amount of tokens to the stakeholders.

- Automatic Inflation: When enabled, tokens are minted every block, aiming for a predetermined total over a year.

**Commands**

Update Parameters (update-params):

- Syntax: `manifestd tx manifest update-params [address:percent_share] [inflation_on_off] [annual_total_mint]`

  - Parameters:
    - `address:percent_share`: Specifies the wallet address and its share of the total rewards (to the ninth exponent).
    - `inflation_on_off`: A boolean value (true or false) to toggle automatic inflation.
    - `annual_total_mint`: The total amount of tokens to be minted annually.

  **Example:** `manifestd tx manifest update-params manifest1hj5fveer5cjtn4wd6wstzugjfdxzl0xp8ws9ct:100_000_000 false 500000000umfx`

Stakeholder Payout (stakeholder-payout):

- Syntax: `manifestd tx manifest stakeholder-payout [amount]`

  - Parameters:

    - `amount`: The amount of tokens to mint from the yearly allocated inflation.

    This command will fail if automatic inflation is enabled.

  **Example:** `manifestd tx manifest stakeholder-payout 777umfx`

### Proof of Authority Module

The PoA module is responsible for handling admin actions like adding and removing other administrators, setting the staking parameters of the chain, controlling voting power, and allowing/blocking validators. Below is a structured breakdown of its components and functionalities:

#### Module Functionality

The PoA admin has several capabilities for managing the chain and its validators:

**Validator Management:**

- Remove validators from the active set.
- Remove validators pending addition to the active set.
- Specify the voting power for each validator.
- Approve the addition of new validators.

**Staking Parameters Update:**

- The PoA admin can update various staking parameters, including:
  - `unbondingTime`: The time period for unbonding tokens.
  - `maxVals`: The maximum number of validators.
  - `maxEntries`: The maximum number of entries.
  - `historicalEntries`: The number of historical entries to store.
  - `bondDenom`: The denomination for bonding and staking.
  - `minCommissionRate`: The minimum commission rate for validators.

**Administrative Rights:**

- Assign or revoke administrative privileges.
- Determine if validators have the ability to self-revoke.

**Commands**

Update Parameters (update-params):

- Syntax: `manifestd tx poa update-params [admin1,admin2,admin3,...] [allow-validator-self-exit-bool]`

  - Parameters:
    - `admin1,admin2,admin3,...`: A list of admin addresses which can be multisig addresses.
    - `allow-validator-self-exit-bool`: A boolean value (true or false) to allow validators to self-exit.

  **Example:** `manifestd tx poa update-params manifest1hj5fveer5cjtn4wd6wstzugjfdxzl0xp8ws9ct,manifest1hj5fveer5cjtn4wd6wstzugjfdxzl0xp8ws9ct false`

Update Staking Parameters (update-staking-params):

- Syntax: `manifestd tx poa update-staking-params [unbondingTime] [maxVals] [maxEntries] [historicalEntries] [bondDenom] [minCommissionRate]`

  - Parameters:

    - `unbondingTime`: The amount of time it takes to unbond tokens.
    - `maxVals`: The maximum number of validators.
    - `maxEntries`: The maximum number of entries.
    - `historicalEntries`: The number of historical entries to store.
    - `bondDenom`: The denomination for bonding and staking.
    - `minCommissionRate`: The minimum commission rate for validators.

  **Example:** `manifestd tx poa update-staking-params 1814400 100 7 1000 umfx 0.01`

Set Voting Power (set-power):

- Syntax: `manifestd tx poa set-power [validator] [power] [--unsafe]`

  - Parameters:

    - `validator`: The validator's operator address.
    - `power`: The voting power to give the validator.
    - `--unsafe`: A flag to allow the PoA admin to set the voting power of a validator that is not in the active set.

    **Example:** `manifestd tx poa set-power manifestvaloper1hj5fveer5cjtn4wd6wstzugjfdxzl0xp8ws9ct 1000000000 --unsafe`

Remove Pending Validator (remove-pending):

- Syntax: `manifestd tx poa remove-pending [validator]`

  - Parameters:

    - `validator`: The validator's operator address.

    **Example:** `manifestd tx poa remove-pending manifestvaloper1hj5fveer5cjtn4wd6wstzugjfdxzl0xp8ws9ct`

Remove Validator (remove):

- Syntax: `manifestd tx poa remove [validator]`

  - Parameters:

    - `validator`: The validator's operator address.

    **Example:** `manifestd tx poa remove manifestvaloper1hj5fveer5cjtn4wd6wstzugjfdxzl0xp8ws9ct`

### Token Factory Module

The Token Factory module as it is implemented on the Manifest Network, allows the PoA admin to have granular control over the creation and management of tokens on the Manifest Network. The admin can mint, burn, edit, and transfer tokens to other accounts from any account.

#### Module Functionality

**Token Minting:**

- Create a token with a specific denom
- Mint a token with a specific amount and denom to your account
- Mint a token with a specific amount and denom to another account

**Token Burning:**

- Burn a token with a specific amount and denom from your account
- Burn a token with a specific amount and denom from another account

**Token Administration:**

- Change the admin address for a factory-created denom
- Force transfer tokens from one address to another address

**Metadata Management:**

- Change the base metadata for a factory-created denom

**Commands**

Burn (burn):

- Syntax: `manifestd tx token-factory burn [amount]`

  - Parameters:
    - `amount`: The amount and denom of the token you would like to burn from your account.

  **Example:** `manifestd tx tokenfactory burn 1<denom>`

Burn From (burn-from):

- Syntax: `manifestd tx token-factory burn-from [address] [amount]`

  - Parameters:
    - `address`: The address of the account you would like to burn the tokens from.
    - `amount`: The amount and denom of the token you would like to burn.

  **Example:** `manifestd tx tokenfactory burn-from manifest1hj5fveer5cjtn4wd6wstzugjfdxzl0xp8ws9ct 1<denom>`

Mint (mint):

- Syntax: `manifestd tx token-factory mint [amount]`

  - Parameters:
    - `amount`: The amount and denom of the token you would like to mint to your account.

  **Example:** `manifestd tx tokenfactory mint 1<denom>`

Change Admin (change-admin):

- Syntax: `manifestd tx token-factory change-admin [denom] [new-admin-address]`

  - Parameters:
    - `denom`: The denom of the token that you would like to change the admin for.
    - `new-admin-address`: The new admin's wallet address.

  **Example:** `manifestd tx tokenfactory change-admin <denom> manifest1hj5fveer5cjtn4wd6wstzugjfdxzl0xp8ws9ct`

Create Denom (create-denom):

- Syntax: `manifestd tx token-factory create-denom [subdenom]`

  - Parameters:
    - `subdenom`: The smallest denomination for your token e.g. udenom.

  **Example:** `manifestd tx tokenfactory create-denom <subdenom>`

Force Transfer (force-transfer):

- Syntax: `manifestd tx token-factory force-transfer  [amount] [transfer-from-address] [transfer-to-address]`

  - Parameters:
    - `amount`: The amount and denom of the token you would like to transfer.
    - `transfer-from-address`: The address of the account you would like to transfer the tokens from.
    - `transfer-to-address`: The address of the account you would like to transfer the tokens to.

  **Example:** `manifestd tx tokenfactory force-transfer 1<denom> manifest1hj5fveer5cjtn4wd6wstzugjfdxzl0xp8ws9ct manifest1hj5fveer5cjtn4wd6wstzugjfdxzl0xp8ws9ct`

Modify Metadata (modify-metadata):

- Syntax: `manifestd tx token-factory modify-metadata [denom] [ticker-symbol] [description] [exponent]`

  - Parameters:
    - `denom`: The denom of the token you are modifying.
    - `ticker-symbol`: The ticker symbol for the token.
    - `description`: A description of the token.
    - `exponent`: The exponent for the token e.g. how many zeros.

  **Example:** `manifestd tx tokenfactory modify-metadata utoken TOKEN "A token" 6`

## Helper

There are scripts for testing, installing, and initializing. Use this section to help you navigate the various scripts and their use cases.

#### Manifest Module script

`scripts/manifest_testing.sh`

This is a script to assist with configuring and testing the inflation and stakeholders. To better understand the script and what exactly it is testing please refer to the [Manifest Module](#manifest-module) section.

#### Node Initialization script

`scripts/test_node.sh`

This is a script to assist with intializing and configuring a node. Ensure you properly congigure the environment variables within the script.
