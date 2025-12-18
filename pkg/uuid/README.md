# Deterministic UUIDv7 Package

This package provides deterministic UUIDv7 generation for blockchain consensus.

## Why a Custom Implementation?

Standard UUID libraries like `google/uuid` are **not suitable for blockchain use** because they rely on:

1. **Non-deterministic time sources**: `time.Now()` varies across validators
2. **Non-deterministic random sources**: `crypto/rand` produces different values on each machine

In a blockchain, **all validators must generate identical UUIDs** for the same transaction within the same block to achieve consensus. If validators generate different UUIDs, the state would diverge and consensus would fail.

## Design Decisions

### Deterministic Inputs

Our implementation uses only deterministic inputs:

| Input | Source | Determinism |
|-------|--------|-------------|
| Timestamp | `ctx.BlockTime()` | Same for all validators in a block |
| Module name | Hardcoded string | Constant across validators |
| Sequence | Module's internal counter stored in state | Consensus-agreed value |

### UUIDv7 Structure

We follow the [RFC 9562](https://www.rfc-editor.org/rfc/rfc9562) UUIDv7 specification:

```
 0                   1                   2                   3
 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                          unix_ts_ms (32 bits)                 |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|           unix_ts_ms (16 bits)        |  ver  | seq (12 bits) |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|var|                     node (62 bits)                        |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                          node (continued)                     |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
```

- **Bits 0-47**: Unix timestamp in milliseconds (from block time)
- **Bits 48-51**: Version (7)
- **Bits 52-63**: 12-bit sequence counter
- **Bits 64-65**: Variant (RFC 4122, value `10`)
- **Bits 66-127**: Node ID derived from module name + sequence hash

### Hash Function

We use FNV-1a (64-bit) for the node ID derivation because:

1. **Deterministic**: Same inputs always produce same output
2. **Fast**: Simple operations, no external dependencies
3. **Well-distributed**: Good avalanche effect for varied inputs
4. **No crypto dependency**: Avoids `crypto/sha256` which may have platform-specific optimizations

### Sequence Management

Each module (SKU, Billing) maintains its own sequence counter in state:

```go
// In module's keeper
sequence := k.GetNextSequence(ctx)
uuid := uuid.GenerateUUIDv7(ctx, "sku", sequence)
```

The sequence is incremented atomically and stored in the module's state, ensuring:
- Uniqueness within the same block (different sequences)
- Uniqueness across blocks (different timestamps)
- Determinism (sequence comes from consensus state)

## Usage

```go
import "github.com/manifest-network/manifest-ledger/pkg/uuid"

// Generate a deterministic UUIDv7
id := uuid.GenerateUUIDv7(ctx, "billing", sequence)

// Validate a UUIDv7 string
if err := uuid.ValidateUUIDv7(id); err != nil {
    return err
}

// Check if valid without error
if uuid.IsValidUUIDv7(id) {
    // valid
}
```

## Alternatives Considered

| Option | Why Not Used |
|--------|--------------|
| `google/uuid` | Uses `time.Now()` and `crypto/rand` - non-deterministic |
| Sequential uint64 | Works but UUIDs are more debuggable and standard |
| Hash of inputs | Would work but UUIDv7 provides time-ordering benefits |
| UUIDv4 | Requires random source, not deterministic |
| UUIDv5 (name-based) | Could work but UUIDv7 is more modern and time-sortable |

## Testing

```bash
go test -v ./pkg/uuid/...
```

The tests verify:
- Format compliance with UUIDv7 specification
- Determinism (same inputs → same output)
- Uniqueness (different sequences → different UUIDs)
- Validation of edge cases
