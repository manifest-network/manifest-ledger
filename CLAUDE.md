# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Manifest Ledger is a Cosmos SDK-based blockchain for decentralized AI infrastructure access. It uses Proof of Authority (PoA) consensus with plans for future Proof of Stake transition.

**Binary**: `manifestd`
**Bech32 Prefix**: `manifest`
**Go Version**: 1.25.9

## Testing

### Unit Tests
```bash
# Run all unit tests
make test

# Run a specific test
go test -v ./x/billing/keeper -run TestAccrualCalculation
```

### Integration Tests (Interchaintest)
All e2e tests require the local Docker image first: `make local-image`. Same for `make coverage`.
The `ictest-*` and `sim-*` targets in the Makefile are the entry points.

## Architecture

### Custom Modules (in `x/`)

- **manifest**: Manual token minting/burning by PoA administrator. Replaces standard mint module's BeginBlocker for stakeholder distribution.

- **sku**: Provider and billing unit management. Providers represent service entities; SKUs represent billable items with per-hour or per-day pricing. Uses UUIDv7 identifiers.

- **billing**: Credit-based leasing system. Tenants fund credit accounts, create leases for SKUs with locked-in pricing. Features lazy settlement (on-touch) and automatic lease closure on credit exhaustion.

### Key Files

- `app/app.go`: Application wiring, keeper initialization, module registration
- `app/ante.go`: Transaction ante handlers including commission rate enforcement
- `app/upgrades.go`: Chain upgrade handlers
- `app/helpers/utils.go`: GetPoAAdmin() returns the authority address

### Module Initialization Order

BeginBlockers: manifest (minting) -> distr -> slashing -> evidence -> poa -> staking -> ...

EndBlockers: crisis -> gov -> poa -> staking -> ... -> billing (pending lease expiration) -> wasm

Genesis: capability -> tokenfactory -> auth -> bank -> ... -> poa -> manifest -> sku -> billing -> wasm

### Testing Patterns

- Keeper tests use `testutil.DefaultContextWithDB` and mock dependencies
- Interchaintest tests are in `interchaintest/` with shared setup in `setup.go`
- E2E tests require building Docker image first: `make local-image`

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
