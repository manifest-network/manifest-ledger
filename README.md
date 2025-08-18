<h1 align="center">Manifest Ledger</h1>

<p align="center">
  <a href="#overview"><img src="https://avatars.githubusercontent.com/u/90303796?s=200&v=4" alt="Lifted Initiative" width="100"/></a>
</p>

<p align="center">
 <a href="https://codecov.io/gh/manifest-network/manifest-ledger" >
     <img src="https://codecov.io/gh/manifest-network/manifest-ledger/graph/badge.svg?token=s7zzdGQ7Gh"/>
 </a>
  <a href="https://goreportcard.com/report/github.com/manifest-network/manifest-ledger">
    <img src="https://goreportcard.com/badge/github.com/manifest-network/manifest-ledger" alt="Go Report Card"/>
  </a>
  <a href="https://discord.gg/kQkaJzxvk9">
    <img src="https://badgen.net/badge/icon/discord?icon=discord&label" alt="Discord"/>
  </a>
</p>

## Overview

The Manifest Network, built on the Cosmos SDK, is a blockchain tailored for decentralized AI infrastructure access. Initially employing a Proof of Authority (PoA) model it ensures a secure and efficient launch phase, with a trusted validator set managing consensus.

While PoA offers immediate stability and control, the Manifest Network aspires for greater decentralization. The future roadmap includes evolving towards a Proof of Stake (PoS) network, utilizing the underlying CometBft consensus mechanism inherent in the Cosmos SDK.

## Table of Contents

- [System Requirements](#system-requirements)
- [Installation](#install--run)
- [Testing](#testing)
- [Helper](#helper)
- [Modules](./MODULE.md)
- [Validators](./network/manifest-1/POST_GENESIS.md)
- [Contributing](./CONTRIBUTING.md)
- [Security/Bug Reporting](./SECURITY.md)

## System Requirements

**Minimal**

- 4 GB RAM
- 100 GB SSD
- 3.2 GHz x4 CPU

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

- Linux (x86_64 with SSSE3 support) or Linux (arm64 with NEON support)

> Note: CosmWasm requires x86-64 processors to support SSSE3 instructions (Intel Core 2 or newer) or ARM64 processors with NEON support.

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
git clone https://github.com/manifest-network/manifest-ledger.git
cd manifest-ledger
```

Then run:

```bash
# build the base binary for interaction
make install
mv $GOPATH/bin/manifestd /usr/local/bin
manifestd

# build docker image for e2e testing
make local-image
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

**To test the Manifest module run:**

```bash
make ictest-manifest
```

**To test the IBC implementation run:**

```bash
make ictest-ibc
```

**To test the Proof of Authority implementation where the administrator is a group run:**

```bash
make ictest-group-poa
```

**To test the chain upgrade run:**

```bash
make ictest-chain-upgrade
```

**To Test cosmwasm functionality run:**

```bash
make ictest-cosmwasm
```

## Simulation

**To execute the full application simulation run:**

```bash
make sim-full-app
```

**To execute the application simulation after state import run:**

```bash
make sim-after-import
```

**To test the application determinism run:**

```bash
make sim-app-determinism
```

Append `-random` to the end of the commands above to run the simulation with a random seed, e.g., `make sim-full-app-random`.

## Coverage

To generate a coverage report for the modules run:

```bash
make local-image
make coverage
```

## Helper

There are scripts for testing, installing, and initializing. Use this section to help you navigate the various scripts and their use cases.

#### Node Initialization script

`scripts/test_node.sh`

This is a script to assist with initializing and configuring a node. Ensure you properly configure the environment variables within the script.

Also in this script are examples of how you could run it

```bash
POA_ADMIN_ADDRESS=manifest1hj5fveer5cjtn4wd6wstzugjfdxzl0xp8ws9ct CHAIN_ID="local-1" HOME_DIR="~/.manifest" TIMEOUT_COMMIT="500ms" CLEAN=true sh scripts/test_node.sh
CHAIN_ID="local-2" HOME_DIR="~/.manifest2" CLEAN=true RPC=36657 REST=2317 PROFF=6061 P2P=36656 GRPC=8090 GRPC_WEB=8091 ROSETTA=8081 TIMEOUT_COMMIT="500ms" sh scripts/test_node.sh
```

The succesful executation of these commands will result in 2 ibc connected instances of manifestd running on your local machine.

#### Upload Contract script

`scripts/upload_contract.sh`

This script is used to upload a contract to the network. It is used to upload the cosmwasm template contract to the network.

`sh scripts/upload_contract.sh`

> Running this script with no arguments will utilize the same environment variables as the test_node.sh script.
