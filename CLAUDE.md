# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Manifest Ledger is a Cosmos SDK-based blockchain for decentralized AI infrastructure access. It uses Proof of Authority (PoA) consensus with plans for future Proof of Stake transition.

**Binary**: `manifestd`
**Bech32 Prefix**: `manifest`
**Go Version**: 1.25.5

## Build Commands

```bash
# Build and install the binary
make install

# Build without installing (outputs to ./build/)
make build

# Build local Docker image for e2e testing
make local-image
```

## Testing

### Unit Tests
```bash
# Run all unit tests
make test

# Run a specific test
go test -v ./x/billing/keeper -run TestAccrualCalculation
```

### Integration Tests (Interchaintest)
All e2e tests require the local Docker image: `make local-image`

```bash
# Individual module tests
make ictest-poa           # Proof of Authority
make ictest-manifest      # Manifest module
make ictest-tokenfactory  # Token Factory
make ictest-ibc           # IBC functionality
make ictest-cosmwasm      # CosmWasm
make ictest-group-poa     # Group-based PoA
make ictest-sku           # SKU module
make ictest-billing       # Billing module (45m timeout)
make ictest-chain-upgrade # Chain upgrade
```

### Simulation Tests
```bash
make sim-full-app          # Full app simulation
make sim-after-import      # Simulation after state import
make sim-app-determinism   # Determinism test

# With random seed
make sim-full-app-random
```

### Coverage
```bash
make local-image && make coverage
```

## Linting and Formatting

```bash
make lint          # Run golangci-lint
make lint-fix      # Run golangci-lint with auto-fix
make format        # Run goimports
make vet           # Run go vet
make govulncheck   # Run vulnerability check
```

Import order (enforced by gci): standard -> default -> cometbft -> cosmos -> cosmossdk.io -> cosmos-sdk -> poa -> tokenfactory -> wasmd -> wasmvm -> manifest-ledger

## Protobuf

```bash
make proto-gen     # Generate Go code from proto files
make proto-format  # Format proto files
make proto-lint    # Lint proto files
```

## Architecture

### Custom Modules (in `x/`)

- **manifest**: Manual token minting/burning by PoA administrator. Replaces standard mint module's BeginBlocker for stakeholder distribution.

- **sku**: Provider and billing unit management. Providers represent service entities; SKUs represent billable items with per-hour or per-day pricing. Uses UUIDv7 identifiers.

- **billing**: Credit-based leasing system. Tenants fund credit accounts, create leases for SKUs with locked-in pricing. Features lazy settlement (on-touch) and automatic lease closure on credit exhaustion.

### External Modules

- **strangelove-ventures/poa**: Proof of Authority consensus
- **strangelove-ventures/tokenfactory**: Token creation and management
- **CosmWasm/wasmd**: Smart contract support
- **cosmos/ibc-go**: Inter-Blockchain Communication

### Key Files

- `app/app.go`: Application wiring, keeper initialization, module registration
- `app/ante.go`: Transaction ante handlers including commission rate enforcement
- `app/upgrades.go`: Chain upgrade handlers
- `app/helpers/helpers.go`: GetPoAAdmin() returns the authority address

### Module Initialization Order

BeginBlockers: manifest (minting) -> distr -> slashing -> evidence -> poa -> staking -> ...

EndBlockers: crisis -> gov -> poa -> staking -> ... -> billing (batch settlement) -> wasm

Genesis: capability -> tokenfactory -> auth -> bank -> ... -> poa -> manifest -> sku -> billing -> wasm

### Testing Patterns

- Keeper tests use `testutil.DefaultContextWithDB` and mock dependencies
- Interchaintest tests are in `interchaintest/` with shared setup in `setup.go`
- E2E tests require building Docker image first: `make local-image`

## Proto Structure

Proto files are in `proto/liftedinit/{module}/v1/`:
- `tx.proto`: Transaction messages
- `query.proto`: Query service
- `genesis.proto`: Genesis state
- `types.proto`: Shared types
- `module/v1/module.proto`: Module configuration

## Go Conventions

### Go 1.25+ Features
- Use `any` instead of `interface{}`
- Use `sync.WaitGroup.Go(fn)` instead of manual `Add`/`Done` pattern

### Style
- Use `%w` verb in `fmt.Errorf` for error wrapping
- Error messages: lowercase, no punctuation
- Keep error variable name as `err`
- Accept interfaces, return concrete types
- Table-driven tests with `t.Run` subtests

### Cosmos SDK Patterns
- Keeper methods take `context.Context` (unwrap with `sdk.UnwrapSDKContext` if needed)
- Use `cosmossdk.io/errors` for error types
- Use `cosmossdk.io/math.Int` and `math.LegacyDec` for numeric operations
- Collections API (`cosmossdk.io/collections`) for state management
