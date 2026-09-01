# SKU Module Architecture

This document describes the internal architecture of the x/sku module for developers who need to understand, maintain, or extend the module.

## Overview

The SKU (Stock Keeping Unit) module provides on-chain management of service offerings and their providers. It serves as the catalog layer for the billing system, defining what services are available and at what prices.

## Module Dependencies

```mermaid
graph TD
    SKU[x/sku Module]
    Billing[x/billing Module]

    Billing -->|SKU lookups| SKU
    Billing -->|Provider lookups| SKU
```

The SKU module:
- **Authority**: holds the PoA admin address as its `authority` string (injected in `app.go` via `helpers.GetPoAAdmin()`); it makes no calls into `x/poa`. Authorization is a string compare against that address plus `Params.AllowedList`.
- **Depended on by**: `x/billing` for SKU and Provider information

## Data Model

### Entity Relationship Diagram

```mermaid
erDiagram
    PROVIDER ||--o{ SKU : "has many"
    PROVIDER {
        string uuid PK
        string address
        string payout_address
        string api_url
        bytes meta_hash
        bool active
    }
    SKU {
        string uuid PK
        string provider_uuid FK
        string name
        Unit unit
        Coin base_price
        bytes meta_hash
        bool active
    }
```

### Provider

Providers represent service vendors who offer SKUs:

| Field | Type | Description |
|-------|------|-------------|
| `uuid` | `string` | Unique UUIDv7 identifier (deterministically generated) |
| `address` | `string` | The provider's management address |
| `payout_address` | `string` | Address where billing payments are sent |
| `api_url` | `string` | HTTPS endpoint for provider's off-chain API (tenant authentication) |
| `meta_hash` | `bytes` | Optional hash of off-chain metadata (name, description, etc.) |
| `active` | `bool` | Whether provider can have new SKUs created |

### SKU

SKUs represent individual service offerings:

| Field | Type | Description |
|-------|------|-------------|
| `uuid` | `string` | Unique UUIDv7 identifier (deterministically generated) |
| `provider_uuid` | `string` | Reference to parent provider's UUID |
| `name` | `string` | Human-readable SKU name |
| `unit` | `Unit` | Billing unit (per hour, per day) |
| `base_price` | `Coin` | Price per unit (defines the payment denomination) |
| `meta_hash` | `bytes` | Optional hash of off-chain metadata |
| `active` | `bool` | Whether SKU can be used in new leases |

### Unit Enum

```
UNIT_UNSPECIFIED = 0  // Invalid
UNIT_PER_HOUR    = 1  // 3600 seconds
UNIT_PER_DAY     = 2  // 86400 seconds
```

## Module Parameters

The SKU module supports configurable parameters to control access and behavior:

| Parameter     | Type      | Description                                              |
|---------------|-----------|----------------------------------------------------------|
| `AllowedList` | `[]string`| List of user addresses permitted to perform write operations in addition to POA authority. |

### Parameter Validation

- All addresses in `AllowedList` must be valid Bech32 account addresses.
- No duplicate decoded address identities are allowed; equivalent Bech32
  spellings count as duplicates.

Parameters can be updated only via `MsgUpdateParams` from the POA authority (allow-listed addresses cannot modify params), and changes are emitted as `params_updated` events.

## Storage Layout

### Collections

```mermaid
graph LR
    subgraph "Primary Storage"
        Providers[Providers<br/>Map: string → Provider]
        SKUs[SKUs<br/>Map: string → SKU]
    end

    subgraph "Indexes"
        ProviderIdx[SKUsByProvider<br/>Map: provider_uuid, sku_uuid → empty]
        AddrIdx[ProvidersByAddress<br/>Map: address, provider_uuid → empty]
        ProviderActiveIdx[ProvidersByActive<br/>Map: active, provider_uuid → empty]
        SKUActiveIdx[SKUsByActive<br/>Map: active, sku_uuid → empty]
        SKUProviderActiveIdx[SKUsByProviderActive<br/>Map: provider_uuid+active, sku_uuid → empty]
    end

    subgraph "Sequences"
        ProviderSeq[ProviderSequence<br/>uint64]
        SKUSeq[SKUSequence<br/>uint64]
    end
```

| Collection | Key Type | Value Type | Purpose |
|------------|----------|------------|---------|
| `Params` | - | `Params` | Module parameters |
| `SKUs` | `string` (UUID) | `SKU` | Primary SKU storage |
| `SKUSequence` | - | `uint64` | Sequence counter for deterministic UUID generation |
| `SKUsByProvider` | `(string, string)` | `bool` | Index for provider → SKU lookups |
| `SKUsByActive` | `(bool, string)` | `bool` | Index for active status → SKU lookups |
| `SKUsByProviderActive` | `(string, bool, string)` | `bool` | Compound index for provider+active → SKU lookups |
| `Providers` | `string` (UUID) | `Provider` | Primary provider storage |
| `ProviderSequence` | - | `uint64` | Sequence counter for deterministic UUID generation |
| `ProvidersByAddress` | `(AccAddress, string)` | `bool` | Index for address → provider lookups |
| `ProvidersByActive` | `(bool, string)` | `bool` | Index for active status → provider lookups |

The registered `state` invariant verifies that provider and SKU collection keys
match their value UUIDs, then applies the full genesis validator to the live
export. This covers provider references, sequence monotonicity, pricing, URL,
address, metadata, and parameter constraints. It also checks both directions of
the provider address/active and SKU provider/active/provider-active indexes,
rejecting missing, stale, or mismatched rows. Collections owns secondary-index
updates and commits them atomically with each primary record; the explicit
invariant provides corruption detection beyond that normal-write guarantee.

### Public Models Versus Stored Values

The `Provider` and `Params` types in the data model are the public protobuf
contract. Transactions, queries, JSON genesis, and exports use Bech32 strings.
Collections expose those same Go types to keeper code, but custom value codecs
translate address identities at the storage boundary:

| Collection | Public value | Persistent value |
|------------|--------------|------------------|
| `Params` | `allowed_list: repeated string` | Disk-only `allowed_addresses: repeated bytes` |
| `Providers` | `address` and `payout_address` as strings | Disk-only raw address bytes; all non-address fields are preserved |
| `SKUs` | Public `SKU` protobuf | Public `SKU` protobuf; it contains no account address |

Params and Provider values carry the unambiguous storage tags
`\x00sku/params/v1` and `\x00sku/provider/v1`. The `v1` suffix names the
disk-message format; the module consensus version that introduced it is v2.
Unknown tag-zero formats fail closed. Legacy untagged values remain decodable so
the registered migration can read them, while every new write uses the tagged
raw-byte form. Decoding for keeper callers and exports returns canonical Bech32.

Address-bearing keys follow the same rule. `ProvidersByAddress` uses the SDK's
`AccAddressKey`; it never stores a Bech32 string in its key. The remaining keys
are UUIDs, booleans, or sequences and do not carry account identities.

### Module Consensus v1→v2

At an application upgrade, `RunMigrations` invokes `Migrate1to2` when the stored
SKU module version is 1. The migration:

1. Canonicalizes and deduplicates `Params.allowed_list` by decoded address bytes,
   preserving first-seen order.
2. Rewrites Params through the current value codec.
3. Reads Providers in ascending primary-key pages of 1,000, closes each iterator,
   and then rewrites that page through the current value codec.

Provider indexes were already byte-addressed and are left intact. SKU values,
sequences, and bank state are not changed. Because the codecs read both legacy
and current values but always write the current representation, rerunning the
migration produces the same bytes.

### Key Prefixes

```go
var (
    ParamsKey                    = collections.NewPrefix(0)
    SKUKey                       = collections.NewPrefix(1)
    SKUSequenceKey               = collections.NewPrefix(2)
    SKUByProviderIndexKey        = collections.NewPrefix(3)
    ProviderKey                  = collections.NewPrefix(4)
    ProviderSequenceKey          = collections.NewPrefix(5)
    ProviderByAddressIndexKey    = collections.NewPrefix(6)
    ProviderByActiveIndexKey     = collections.NewPrefix(7)
    SKUByActiveIndexKey          = collections.NewPrefix(8)
    SKUByProviderActiveIndexKey  = collections.NewPrefix(9)
)
```

### UUIDv7 Generation

The module uses deterministic UUIDv7 generation for consensus compatibility:

- **Timestamp**: Derived from block time (milliseconds)
- **Random bits**: Derived from FNV-1a hash of (header hash + chain ID + module name + sequence)
- **Format**: Standard UUIDv7 with version 7 and variant bits set correctly

This ensures all validators generate the same UUID for the same transaction.

## Message Flow

### CreateProvider

```mermaid
sequenceDiagram
    participant User
    participant MsgServer
    participant Keeper
    participant Store

    User->>MsgServer: MsgCreateProvider
    MsgServer->>MsgServer: isAuthorizedSender() (GetAuthority string compare + AllowedList params lookup)
    alt Not authority AND not in AllowedList
        MsgServer-->>User: Unauthorized
    else Authorized
        MsgServer->>MsgServer: Validate()
        Note over MsgServer: Unauthorized senders never reach validation
        MsgServer->>Keeper: CreateProvider()
        Keeper->>Store: Get Next ID
        Store-->>Keeper: ID
        Keeper->>Store: Save Provider
        Keeper->>MsgServer: Provider ID
        MsgServer->>MsgServer: Emit Event
        MsgServer-->>User: Success + ID
    end
```

### CreateSKU

```mermaid
sequenceDiagram
    participant User
    participant MsgServer
    participant Keeper
    participant Store

    User->>MsgServer: MsgCreateSKU
    MsgServer->>MsgServer: isAuthorizedSender() (GetAuthority string compare + AllowedList params lookup)
    alt Not authority AND not in AllowedList
        MsgServer-->>User: Unauthorized
    else Authorized
        MsgServer->>MsgServer: Validate()
        Note over MsgServer: Validates price divisibility; unauthorized senders never reach validation
        MsgServer->>Keeper: GetProvider()
        alt Provider Not Found or Inactive
            Keeper-->>MsgServer: Error
            MsgServer-->>User: Error
        else Provider OK
            Keeper-->>MsgServer: Provider
            MsgServer->>Keeper: CreateSKU()
            Keeper->>Store: Get Next ID
            Keeper->>Store: Save SKU
            Keeper->>Store: Update Index
            Keeper->>MsgServer: SKU ID
            MsgServer->>MsgServer: Emit Event
            MsgServer-->>User: Success + ID
        end
    end
```

### UpdateSKU

```mermaid
sequenceDiagram
    participant User
    participant MsgServer
    participant Keeper
    participant Store

    User->>MsgServer: MsgUpdateSKU
    MsgServer->>MsgServer: isAuthorizedSender() (GetAuthority string compare + AllowedList params lookup)
    alt Not authority AND not in AllowedList
        MsgServer-->>User: Unauthorized
    else Authorized
        MsgServer->>MsgServer: Validate()
        Note over MsgServer: Unauthorized senders never reach validation
        MsgServer->>Keeper: GetSKU()
        alt SKU Not Found
            Keeper-->>MsgServer: Error
            MsgServer-->>User: Error
        else SKU Found
            Keeper-->>MsgServer: Existing SKU
            alt provider_uuid != existingSKU.provider_uuid
                MsgServer-->>User: ErrInvalidSKU (provider_uuid mismatch)
            end
            MsgServer->>Keeper: GetProvider(existingSKU.provider_uuid)
            alt Provider Not Found
                Keeper-->>MsgServer: Error
                MsgServer-->>User: Error
            end
            alt active -> inactive
                MsgServer-->>User: ErrInvalidSKU (use DeactivateSKU instead)
            end
            alt reactivation while provider inactive
                MsgServer-->>User: ErrInvalidProvider
            end
            MsgServer->>Keeper: SetSKU()
            Keeper->>Store: Save SKU
            Note over MsgServer: IndexedMap maintains indexes implicitly
            alt inactive -> active
                MsgServer->>MsgServer: Emit sku_activated
            end
            MsgServer->>MsgServer: Emit sku_updated
            MsgServer-->>User: Success
        end
    end
```

## Validation Rules

### Price Divisibility

SKU prices must be evenly divisible by their unit's seconds to ensure exact per-second rate calculations. See [Pricing and Exact Divisibility](../README.md#pricing-and-exact-divisibility) for the user-facing explanation.

**Implementation** (`x/sku/types/unit.go`):
```go
func ValidatePriceAndUnit(basePrice sdk.Coin, unit Unit) error {
    divisor, ok := divisorForUnit(unit)
    if !ok {
        return fmt.Errorf("invalid unit: %s", unit)
    }

    perSecond := basePrice.Amount.Quo(divisor)

    // Check if per-second rate is zero (would result in free usage)
    if perSecond.IsZero() {
        return &PriceValidationError{
            BasePrice: basePrice,
            Unit:      unit,
            IsZero:    true,
        }
    }

    // Check if division is exact (no remainder)
    remainder := basePrice.Amount.Mod(divisor)
    if !remainder.IsZero() {
        return &PriceValidationError{
            BasePrice: basePrice,
            Unit:      unit,
            IsZero:    false,
            Remainder: remainder,
        }
    }

    return nil
}
```

The divisor comes from the unexported `divisorForUnit(unit)` (3600 for `UNIT_PER_HOUR`, 86400 for `UNIT_PER_DAY`). On failure the function returns a `*PriceValidationError` with two modes: `IsZero=true` ("results in zero per-second rate") when the price truncates to a zero rate, and `IsZero=false` ("not evenly divisible ... remainder: %s") when division leaves a remainder.

### Provider State Validation

- Cannot create SKU for non-existent provider
- Cannot create SKU for inactive provider
- A SKU cannot be re-parented — `MsgUpdateSKU.provider_uuid` must equal the SKU's existing `provider_uuid`, else `ErrInvalidSKU: provider_uuid mismatch`
- Deactivating provider cascades to deactivate all its SKUs (paginated for gas safety)

## Events and Error Codes

For the complete reference of events and error codes, see [API Reference](API.md#events).

## Security Considerations

### Authorization Model

Most write operations require either POA authority or user inclusion in the `AllowedList`:
- Only the POA admin group or users in the `AllowedList` can create/update providers
- Only the POA admin group or users in the `AllowedList` can create/update SKUs
- No other user-level SKU management is permitted

**Exception:** `MsgUpdateParams` is authority-only — allow-listed addresses cannot modify params (they cannot add themselves or others to the `AllowedList`). Only the six provider/SKU write handlers accept allow-listed senders.

The `AllowedList` is a configurable list of user addresses permitted to perform write operations alongside the POA authority.

### Soft Delete Pattern

Both providers and SKUs use soft delete (active flag):
- Maintains referential integrity with billing module
- Historical data preserved for auditing
- Inactive items excluded from new lease creation

### Input Validation

- SKU names: Max 256 UTF-8 bytes (`MaxSKUNameLength`)
- API URLs: Max 2048 UTF-8 bytes (`MaxAPIURLLength`), HTTPS required
- Provider/Payout addresses: Valid bech32 addresses
- Prices: Positive, divisible by unit seconds
- Meta hash: Optional, max 64 bytes (SHA-256/SHA-512)

## Performance Characteristics

| Operation | Complexity | Notes |
|-----------|------------|-------|
| GetProvider | O(1) | Direct key lookup by UUID |
| GetProvidersByAddress | O(k) | Address index scan, k = providers for address |
| GetActiveProviders | O(k) | Active index scan, k = active providers |
| GetSKU | O(1) | Direct key lookup by UUID |
| GetSKUsByProvider | O(n) | Index scan, n = SKUs per provider |
| GetSKUsByProvider (active only) | O(k) | Compound index scan, k = active SKUs for provider |
| GetActiveSKUs | O(k) | Active index scan, k = active SKUs |
| CreateProvider | O(1) | Single write + index writes + sequence increment |
| CreateSKU | O(1) | Three writes (SKU + indexes) + sequence increment |
| UpdateSKU | O(1) | Up to 5 writes if provider or active status changes |
| UpdateProvider | O(1) | Up to 3 writes if active status changes |

### Future Improvements

The following optimizations have been identified but deferred due to marginal benefit:

| Index | Current | Improvement | Notes |
|-------|---------|-------------|-------|
| `ProviderByAddressActive` | O(k) + post-filter | O(m) direct | Compound (address, active) index for `ProviderByAddress` with `active_only=true`. Deferred because provider counts per address are typically small (1-5), making post-filtering negligible. |

## Testing Strategy

### Unit Tests
- Message validation (`msgs_test.go`)
- Unit/pricing logic (`unit_test.go`)
- Params validation (`params_test.go`)
- Genesis validation (`genesis_test.go`)
- Keeper operations (`keeper_test.go`, `msg_server_test.go`, `querier_test.go`)

### Integration Tests
- Genesis import/export
- Query handlers
- Full message flows

### E2E Tests
- Authority permissions
- Provider lifecycle
- SKU lifecycle
- Pagination
- Error conditions

### Simulation
- Random provider creation
- Random SKU creation/updates
- Weight-based operation distribution

## Related Documentation

- [SKU Module README](../README.md) - Module overview
- [API Reference](API.md) - Messages, queries, events, and error codes
- [Design Decisions](DESIGN_DECISIONS.md) - Key design decisions and rationale
- [Provider Setup Guide](PROVIDER_GUIDE.md) - Step-by-step guide to creating providers
- [SKU Setup Guide](SKU_GUIDE.md) - Step-by-step guide to creating SKUs
- [Troubleshooting](TROUBLESHOOTING.md) - Common issues and resolutions
- [Billing Module](../../billing/docs/ARCHITECTURE.md) - Downstream consumer of SKU/Provider data
