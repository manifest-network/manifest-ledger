# SKU Module Design Decisions

This document records key design decisions made during the development of the x/sku module, including the rationale and trade-offs considered.

## Decision 1: Separate Provider Entity

**Decision:** Create a separate Provider entity rather than embedding provider information in each SKU.

**Alternatives Considered:**
1. Embed provider name/address directly in SKU
2. Use a simple string provider identifier
3. Create a separate Provider entity with its own lifecycle (chosen)

**Rationale:**
- **Data Normalization:** Avoids duplicating provider information across SKUs
- **Payout Address Management:** Single source of truth for where payments go
- **Provider Lifecycle:** Can deactivate a provider without touching individual SKUs
- **Future Extensibility:** Can add provider-level attributes (reputation, limits, etc.)

**Trade-offs:**
- Additional storage overhead for provider entity
- Two-step creation process (provider then SKU)
- More complex queries to get full SKU details

## Decision 2: UUIDv7 Identifiers

**Decision:** Use deterministic UUIDv7 identifiers for both Providers and SKUs rather than auto-incrementing integers or user-provided identifiers.

**Alternatives Considered:**
1. User-provided string identifiers
2. Hash-based identifiers
3. Auto-incrementing integers
4. UUIDv7 with deterministic generation (chosen)

**Rationale:**
- **Uniqueness:** UUIDs are globally unique and collision-resistant
- **Debuggability:** Easier to trace and reference in logs and UIs
- **External Integration:** Standard format for external systems
- **Time-ordering:** UUIDv7 encodes timestamp for natural ordering
- **Consensus Safety:** Deterministic generation ensures all validators produce the same UUID

**Implementation:**
```go
// UUIDv7 is generated using:
// - Timestamp: block time in milliseconds
// - Random bits: SHA-256(block_height || sequence_counter)
uuid := uuidv7.NewDeterministic(ctx, sequence)
```

**Trade-offs:**
- Slightly larger storage than uint64 (36 chars vs 8 bytes)
- Requires custom deterministic generation for consensus
- Cannot be predicted (intentional for security)

## Decision 3: Soft Delete Pattern

**Decision:** Use an `active` boolean flag instead of deleting records.

**Alternatives Considered:**
1. Hard delete with orphan handling
2. Soft delete with active flag (chosen)
3. State machine with multiple states

**Rationale:**
- **Referential Integrity:** Billing module leases reference SKUs by ID
- **Audit Trail:** Historical records preserved
- **Simplicity:** Boolean is simpler than full state machine
- **Idempotency:** Deactivating twice is safe
- **Billing Continuity:** Existing leases continue with inactive SKUs/providers

**Trade-offs:**
- Storage grows indefinitely (no pruning)
- Queries must filter by active status
- No way to reclaim UUIDs (by design)

## Decision 4: Authority-Only Access with AllowedList

**Decision:** All write operations require either POA authority or user inclusion in the `allowed_list` parameter.

**Alternatives Considered:**
1. Provider self-registration
2. Permissioned provider accounts
3. Authority-only management with configurable `allowed_list` (chosen)

**Rationale:**
- **Security:** Prevents spam/malicious SKU creation
- **Quality Control:** Authority or explicitly permitted users vet all providers
- **Simplicity:** Single authorization check using POA or allowed_list
- **Trust Model:** Matches existing POA governance, with flexibility for additional users

**Trade-offs:**
- Higher operational overhead for adding providers
- Authority or allowed_list becomes bottleneck
- Less decentralized, but more flexible than POA-only

## Decision 5: Price Divisibility Validation

**Decision:** Require SKU prices to be evenly divisible by their unit's seconds.

**Alternatives Considered:**
1. Allow any price with rounding
2. Store per-second rate directly
3. Require exact divisibility (chosen)

**Rationale:**
- **Precision:** No rounding errors in billing calculations
- **Predictability:** Users know exact cost
- **Auditability:** Calculations are deterministic
- **Security:** Prevents exploitation of rounding

**Trade-offs:**
- Less pricing flexibility
- Must use specific price values (multiples of 3600 for hourly, 86400 for daily)
- Harder to express "nice" prices

**Implementation:** The billing module calculates per-second rate as:
```go
ratePerSecond = basePrice.Amount / unit.Seconds()
```

## Decision 6: Unit Enum vs Seconds Storage

**Decision:** Store unit as enum, convert to seconds at runtime.

**Alternatives Considered:**
1. Store seconds directly
2. Store enum with runtime conversion (chosen)
3. Store both enum and seconds

**Rationale:**
- **Readability:** "UNIT_PER_HOUR" clearer than "3600"
- **Validation:** Enum restricts to valid values
- **Display:** Easy to show human-readable unit
- **Flexibility:** Can add new units without migration

**Trade-offs:**
- Conversion overhead (minimal)
- Enum evolution requires careful handling

See [Billing Units](../README.md#billing-units) for the complete unit reference.

## Decision 7: Meta Hash for Off-Chain Data

**Decision:** Store optional hash of off-chain metadata rather than full metadata.

**Alternatives Considered:**
1. Store full metadata on-chain
2. Store IPFS CID
3. Store generic hash (chosen)
4. No off-chain reference

**Rationale:**
- **Storage Efficiency:** Hashes are fixed size
- **Flexibility:** No protocol dependency (IPFS, Arweave, etc.)
- **Verification:** Can verify off-chain data matches
- **Privacy:** Actual data stored off-chain

**Trade-offs:**
- No on-chain search of metadata
- Requires off-chain storage system
- Hash algorithm not enforced

## Decision 8: Provider-SKU Index

**Decision:** Maintain a secondary index for provider→SKU lookups.

**Alternatives Considered:**
1. Full table scan for provider queries
2. Secondary index (chosen)
3. Denormalize provider ID into SKU key

**Rationale:**
- **Query Performance:** O(n) where n = SKUs per provider
- **Common Access Pattern:** Provider dashboard needs their SKUs
- **Billing Integration:** Quick lookup for settlement

**Trade-offs:**
- Additional storage for index
- Index maintenance on create/update
- Slight write overhead

## Decision 9: Payout Address on Provider

**Decision:** Store payout address on Provider rather than per-SKU.

**Alternatives Considered:**
1. Per-SKU payout addresses
2. Per-provider payout address (chosen)
3. Both with override capability

**Rationale:**
- **Simplicity:** Single payout destination per provider
- **Operational:** Easier to manage
- **Accounting:** Clearer fund flow
- **Security:** Fewer addresses to manage

**Trade-offs:**
- No per-SKU payment splitting
- Provider must manage fund distribution
- Address changes affect all SKUs

## Decision 10: No SKU Versioning

**Decision:** Updates modify existing SKU in place, no version history.

**Alternatives Considered:**
1. Immutable SKUs with new versions
2. Mutable SKUs (chosen)
3. Append-only with version chain

**Rationale:**
- **Simplicity:** Simpler data model
- **Lease Locking:** Billing locks price at lease creation (in billing module)
- **Storage:** No version chain overhead
- **Queries:** Simpler without version handling

**Trade-offs:**
- No built-in version history
- Changes affect display immediately
- Must use events for audit trail

## Decision 11: Provider Address vs Payout Address

**Decision:** Separate management address (address) from payout address (payout_address) on Provider.

**Alternatives Considered:**
1. Single address for both
2. Separate addresses (chosen)

**Rationale:**
- **Security:** Operational keys separate from treasury
- **Flexibility:** Hot wallet for management, cold wallet for payouts
- **Delegation:** Allow different teams to manage vs receive funds

**Trade-offs:**
- Two addresses to manage per provider
- Potential confusion about which address does what

## Future Considerations

### Potential Enhancements for v2

1. **Provider Self-Registration:** Allow providers to register with approval workflow
2. **SKU Categories/Tags:** Hierarchical organization of SKUs
3. **Tiered Pricing:** Volume discounts or time-based pricing
4. **SKU Templates:** Pre-defined SKU configurations
5. **Provider Reputation:** On-chain reputation tracking
6. **Multi-Currency Pricing:** Support multiple denominations
7. **SKU Bundles:** Package multiple SKUs together
8. **Price History:** Track price changes over time

### Known Limitations

1. **No Price History:** Updates overwrite previous values
2. **No SKU Transfer:** Cannot transfer SKU ownership between providers
3. **No Bulk Operations:** Must create/update SKUs individually
4. **Limited Metadata:** Only hash stored on-chain

### Migration Considerations

When implementing breaking changes:
- Use store migrations for data structure changes
- Maintain backward compatibility for queries
- Version proto messages appropriately
- Document upgrade procedures
- Consider impact on existing leases in billing module
