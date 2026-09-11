# Deterministic UUIDv7 Package

This package provides deterministic UUIDv7 generation for blockchain consensus, incorporating chain and entity inputs to reduce accidental collisions.

## Why a Custom Implementation?

Standard UUID libraries like `google/uuid` are **not suitable for blockchain use** because they rely on:

1. **Non-deterministic time sources**: `time.Now()` varies across validators
2. **Non-deterministic random sources**: `crypto/rand` produces different values on each machine

In a blockchain, **all validators must generate identical UUIDs** for the same transaction within the same block to achieve consensus. If validators generate different UUIDs, the state would diverge and consensus would fail.

## Design Decisions

### Deterministic Inputs

Our implementation uses only deterministic inputs available to all validators:

| Input | Source | Purpose |
|-------|--------|---------|
| Timestamp | `ctx.BlockTime()` | Time-ordering, same for all validators in a block |
| Header Hash | `ctx.HeaderHash()` | Distinguishes chains and blocks |
| Chain ID | `ctx.ChainID()` | Multi-chain deployment isolation |
| Module name | Hardcoded string | Intra-chain module isolation |
| Sequence | Module's internal counter | Uniqueness within module |

### Cross-Chain Collision Resistance

The **block header hash** and **chain ID** distinguish generation inputs when chains have identical timestamps and sequences. These inputs reduce accidental cross-chain collisions; they do not guarantee uniqueness or cryptographic collision resistance.

This is important because:
- You may deploy multiple chains (mainnet, testnet, devnet) simultaneously
- Chains might share the same genesis timestamp
- First entities created on each chain would otherwise have the same sequence (1)
- Without chain-specific entropy, these would collide

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
- **Bits 66-127**: Node ID derived from header hash + chain ID + module name + sequence hash

### Hash Function

The node ID uses the standard library’s `hash/fnv.New64a` implementation of FNV-1a (64-bit), preserving the original consensus encoding. The input is the concatenation of header hash bytes, chain ID bytes, entity namespace bytes, and the sequence as eight big-endian bytes, with no delimiters. The UUID retains the low 62 hash bits.

FNV is fast and deterministic, but it is not a cryptographic hash. Platform optimizations do not make cryptographic hashes such as SHA-256 nondeterministic; changing to one here would change generated identifiers and require a coordinated protocol change. Golden vectors protect the current algorithm, byte order, and framing.

### Sequence Management

The SKU module maintains separate provider and SKU counters; billing maintains a lease counter. Their namespaces are `sku-provider`, `sku-sku`, and `billing`, respectively. These strings participate in consensus-visible identifier generation and must match the keeper call sites:

```go
// Using a sequence allocated from the provider counter
id := uuid.GenerateUUIDv7(ctx, "sku-provider", sequence)
```

The sequence is incremented atomically and stored in module state. Keepers reject allocations once the uint64 counter is exhausted instead of wrapping it. The low 12 bits are stored directly in the UUID, and the full counter participates in the hash. Sequences beyond 4095 therefore still contribute distinct inputs, though the truncated hash cannot mathematically guarantee unique outputs.

## Usage

### Standard Usage (Recommended)

```go
import (
    "github.com/manifest-network/manifest-ledger/pkg/uuid"
    billingtypes "github.com/manifest-network/manifest-ledger/x/billing/types"
)

// Generate a deterministic UUIDv7 with full entropy
// Uses block time, header hash, and chain ID from context
id := uuid.GenerateUUIDv7(ctx, billingtypes.ModuleName, sequence)
```

### Testing / Migration

```go
// For testing when context is not available
// WARNING: Does not include header hash or chain ID
id := uuid.GenerateUUIDv7FromTime(time.Now(), "sku", 1)

// For full control over all inputs
id := uuid.GenerateUUIDv7WithEntropy(
    blockTime,
    headerHash,  // []byte, can be nil
    chainID,     // string
    moduleName,
    sequence,
)
```

### Validation

```go
// Validate a UUIDv7 string
if err := uuid.ValidateUUIDv7(id); err != nil {
    return err
}

// Check if valid without error
if uuid.IsValidUUIDv7(id) {
    // valid
}
```

## Security Considerations

### Predictability

- UUIDs are **deterministic** - anyone with access to block data can compute them
- This is by design for consensus; UUIDs should not be used as secrets
- The header hash adds unpredictability from the previous block, but once a block is produced, UUIDs are knowable

### Collision Resistance

- **Within a chain**: Separate entity namespaces and persistent counters reduce accidental collisions; the hash does not guarantee uniqueness after the 12-bit sequence field wraps.
- **Across chains**: Header hash and chain ID distinguish inputs, subject to hash collisions and the existing unframed concatenation.
- **Across time**: Distinct encoded millisecond timestamps produce distinct UUID prefixes.
- **Adversarial inputs**: FNV is non-cryptographic. These identifiers are not suitable as authentication tokens or cryptographic commitments.

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
go test ./x/billing/keeper -run '^TestCreateLeaseUUIDContract$'
go test ./x/sku/keeper -run '^TestCreate(Provider|SKU)UUIDContract$'
```

The tests verify:
- Format compliance with UUIDv7 specification
- Fixed golden vectors, captured from the manual implementation at `85d1602`, for all production namespaces, 12-bit sequence-field rollover at 4096, large counters, and time-only generation
- Keeper regressions that pin lease, provider, and SKU UUIDs from actual creation across sequences 4095 and 4096, including stored identities and sequence advancement
- Decoded timestamp, version, variant, and sequence bits
- Determinism (same inputs → same output)
- Uniqueness (different sequences → different UUIDs)
- Distinct UUIDs for representative cross-chain inputs
- Multi-chain deployment scenarios
- Validation of edge cases
