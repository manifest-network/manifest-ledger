# Billing Module Design Decisions

This document records key design decisions made during the development of the x/billing module, including the rationale and trade-offs considered.

## Decision 1: Pre-Funded Credit Account Model

**Decision:** Require tenants to pre-fund a credit account before creating leases, rather than billing post-usage.

**Alternatives Considered:**
1. Post-usage billing with invoices
2. Real-time deduction per block
3. Pre-funded credit account (chosen)

**Rationale:**
- **No Credit Risk:** Provider always paid from existing funds
- **Simple UX:** Mimics AWS credits model familiar to users
- **Off-Chain Top-Up:** Easy integration with fiat payment systems
- **No Collections:** No need for dispute resolution

**Trade-offs:**
- Users must pre-pay
- Unused credits locked in account (no withdrawal mechanism)
- Requires monitoring for low balance

## Decision 2: Lazy Settlement vs Per-Block Processing

**Decision:** Settle leases lazily during write operations (withdrawal, close) rather than every block.

**Alternatives Considered:**
1. EndBlocker settlement of all leases
2. Per-block micro-transfers
3. Lazy settlement on write operations (chosen)

**Rationale:**
- **Chain Performance:** No EndBlocker overhead regardless of lease count
- **Scalability:** Supports millions of leases without block time impact
- **Gas Efficiency:** Settlement cost paid by user who triggers it
- **Simplicity:** No background job management

**Trade-offs:**
- Lease state queries (`Lease`, `Leases`, `LeasesByTenant`, `LeasesByProvider`) return stored state
- Use `WithdrawableAmount` or `ProviderWithdrawable` queries for real-time accrued amounts
- Auto-close only happens during write operations (CloseLease, Withdraw)
- Provider withdrawal requires explicit action

**Implementation Note:** Queries do NOT trigger settlement or auto-close. This is intentional to ensure state changes are properly committed during transactions.

## Decision 3: Derived Credit Account Addresses

**Decision:** Derive credit account address deterministically from tenant address using module-specific derivation.

**Alternatives Considered:**
1. Separate account with stored mapping
2. Use tenant address directly
3. Derived address with reverse lookup (chosen)

**Rationale:**
- **Isolation:** Credit funds separate from spendable balance
- **O(1) Detection:** Can identify credit accounts without full scan via reverse lookup
- **Deterministic:** Same tenant always gets same credit address
- **No Key Management:** No private key needed for credit account

**Trade-offs:**
- Funds at derived address are controlled by module
- Requires reverse lookup for O(1) detection
- Migration complexity if derivation changes

**Derivation Formula:**
```go
creditAddr = sha256("billing" + tenantAddr)[:20]
```

## Decision 4: Price Locking at Lease Creation

**Decision:** Lock SKU prices when lease is created; price changes only affect new leases.

**Alternatives Considered:**
1. Dynamic pricing (current SKU price always used)
2. Price lock with term limits
3. Permanent price lock at creation (chosen)

**Rationale:**
- **Predictability:** Tenants know exact cost
- **Trust:** No surprise price increases
- **Simplicity:** No price update propagation
- **Business Model:** Matches reserved instances pattern

**Trade-offs:**
- Provider cannot increase prices for existing leases
- Long-running leases may have stale prices
- No volume discount adjustments

**Implementation Note:** The `locked_price` stored in `LeaseItem` is the pre-computed per-second rate (not the original SKU price), calculated at lease creation using `skutypes.CalculatePricePerSecond()`.

## Decision 5: Multi-Item Leases

**Decision:** Allow a single lease to contain multiple SKU items rather than one SKU per lease.

**Alternatives Considered:**
1. One SKU per lease
2. Multiple SKUs per lease (chosen)
3. SKU bundles as composite SKUs

**Rationale:**
- **User Experience:** Single lease for complete infrastructure
- **Atomic Operations:** All items start/stop together
- **Fewer Objects:** Less storage overhead
- **Cleaner Billing:** Single settlement per lease

**Trade-offs:**
- More complex lease structure
- Cannot close individual items
- Settlement must iterate all items
- All items must be from the same provider

## Decision 6: Single Provider Per Lease

**Decision:** All SKUs in a lease must belong to the same provider.

**Alternatives Considered:**
1. Allow mixed providers in one lease
2. Single provider per lease (chosen)

**Rationale:**
- **Simplified Settlement:** Only one payout destination per lease
- **Clear Ownership:** Provider can close their own leases
- **Authorization:** Provider permission applies to entire lease
- **Accounting:** No splitting of settlements

**Trade-offs:**
- Users need multiple leases for multi-provider setups
- More leases to manage for complex deployments

## Decision 7: Soft Delete for Leases

**Decision:** Keep closed leases with INACTIVE state rather than deleting them.

**Alternatives Considered:**
1. Hard delete closed leases
2. Soft delete with state flag (chosen)
3. Move to archive storage

**Rationale:**
- **Audit Trail:** Complete history preserved
- **Queries:** Can show past leases to tenant
- **Referential Integrity:** Provider queries still work
- **Dispute Resolution:** Evidence available if needed

**Trade-offs:**
- Storage grows indefinitely
- Must filter by state in queries
- No built-in pruning

## Decision 8: Provider Payout Address at Provider Level

**Decision:** Payout address defined on Provider (in SKU module), not per-lease or per-SKU.

**Alternatives Considered:**
1. Per-lease payout address
2. Per-SKU payout address
3. Per-provider payout address (chosen)

**Rationale:**
- **Simplicity:** Single payout destination per provider
- **Security:** Fewer addresses to secure
- **Operations:** Easy to change payout address
- **Module Independence:** Billing module doesn't duplicate provider data

**Trade-offs:**
- All provider revenue goes to one address
- Cannot split by SKU type
- Provider must distribute internally

## Decision 9: Multi-Denom Support

**Decision:** Allow SKUs to use different token denominations for their base_price, with credit accounts supporting multiple denoms.

**Alternatives Considered:**
1. Single global denom for all billing (rejected)
2. Per-provider denom restriction (rejected)
3. Full multi-denom support (chosen)

**Rationale:**
- **Flexibility:** Different SKUs can be priced in different tokens
- **Provider Choice:** Providers select appropriate payment token per SKU
- **Market Integration:** Can price in stablecoins, native tokens, or custom tokens
- **Simple Architecture:** Credit accounts leverage bank module's multi-denom support

**Implementation:**
- Each SKU's `base_price` is a `Coin` with its own denom
- Lease items store `locked_price` as `Coin` (preserving denom)
- Credit validation checks each denom separately
- Settlement transfers happen per-denom
- Credit accounts are regular bank accounts that can hold any denom
- No send restrictions on credit accounts (any token can be sent)

**Trade-offs:**
- Lease creation validation more complex (check each denom)
- Settlement must aggregate and transfer per-denom
- Users must fund credit with correct denoms for their desired SKUs
- No automatic denom conversion

**Example:**
```
# Lease with two SKUs using different denoms:
Item 1: SKU 1 (priced in 'upwr') - locked_price: 1000upwr/second
Item 2: SKU 2 (priced in 'umfx') - locked_price: 500umfx/second

# Credit account needs:
- Enough 'upwr' for Item 1 at min_lease_duration
- Enough 'umfx' for Item 2 at min_lease_duration

# Settlement transfers:
- Accrued upwr → provider payout address
- Accrued umfx → provider payout address
```

## Decision 10: Authority-Based Lease Creation for Migration

**Decision:** Allow authority and allow-listed addresses to create leases on behalf of tenants via `MsgCreateLeaseForTenant`.

**Alternatives Considered:**
1. Tenant-only lease creation
2. Delegation/authz pattern
3. Authority with allow-list (chosen)

**Rationale:**
- **Migration Support:** Essential for moving off-chain leases on-chain
- **No User Action:** Tenant doesn't need to sign
- **Controlled:** Authority or explicit allow-list
- **Transparent:** Events indicate who created lease

**Trade-offs:**
- Trust in authority/allow-list
- Potential for abuse
- Requires proper off-chain coordination

## Decision 11: Per-Second Rate Storage

**Decision:** Store pre-computed per-second rates (`locked_price`) rather than original unit-based prices.

**Alternatives Considered:**
1. Keep original units, convert at settlement
2. Store per-block rates
3. Pre-compute per-second rates at lease creation (chosen)

**Rationale:**
- **Precision:** Consistent calculation regardless of original unit
- **Performance:** No division at settlement time
- **Verification:** Easy to audit
- **Block Time Independence:** Not tied to block duration

**Trade-offs:**
- Requires price divisibility validation at SKU creation
- Integer math only (no fractions)
- Less intuitive stored values

## Decision 12: Exact Divisibility Requirement

**Decision:** Require SKU prices to be exactly divisible by unit seconds (no rounding).

**Alternatives Considered:**
1. Allow rounding with defined behavior
2. Use fixed-point decimals
3. Require exact divisibility (chosen)

**Rationale:**
- **Determinism:** Same calculation everywhere
- **Auditability:** Can verify exact amounts
- **No Rounding Disputes:** Eliminates edge cases
- **Security:** No rounding exploitation

**Trade-offs:**
- Less pricing flexibility
- Must use specific values (multiples of 3600 for hourly, 86400 for daily)
- May seem artificial to users

**Example:** For UNIT_PER_HOUR (3600 seconds), valid prices include 3600, 7200, 36000, etc.

## Decision 13: Maximum Limits as DoS Protection

**Decision:** Impose configurable limits on leases per tenant, items per lease, and withdrawal batch size.

**Alternatives Considered:**
1. No limits (rely on gas)
2. Hard-coded limits
3. Configurable parameter limits (chosen)

**Rationale:**
- **DoS Prevention:** Bounds computation
- **Flexibility:** Can adjust via governance
- **Predictability:** Known bounds for clients
- **Fair Resource Use:** Prevents single tenant monopolizing

**Trade-offs:**
- Must choose appropriate defaults
- Power users may hit limits
- Parameter updates affect existing users

**Default Limits:**
| Parameter | Default | Hard Limit |
|-----------|---------|------------|
| `max_leases_per_tenant` | 100 | None |
| `max_items_per_lease` | 20 | 100 |
| `min_lease_duration` | 3600s | None |
| WithdrawAll batch | 50 | 100 |

## Decision 14: Minimum Lease Duration

**Decision:** Require tenants to have enough credit to cover a minimum duration before creating a lease.

**Alternatives Considered:**
1. No minimum (allow immediate exhaustion)
2. Fixed minimum credit amount
3. Duration-based minimum (chosen)

**Rationale:**
- **Prevents Spam:** Can't create leases that immediately close
- **Rate-Based:** Adapts to lease cost automatically
- **User Protection:** Ensures meaningful lease duration
- **Provider Protection:** Guarantees minimum revenue per lease

**Trade-offs:**
- More complex lease creation validation
- May prevent low-balance users from creating expensive leases
- Requires calculation at creation time

**Implementation:**
```go
minRequired = totalRatePerSecond * minLeaseDuration
if creditBalance < minRequired {
    return ErrInsufficientCredit
}
```

## Future Considerations

### Potential Enhancements for v2

1. **Lease Pruning:** Archive old leases to reduce state size
2. **Tiered Pricing:** Volume discounts based on usage
3. **Grace Period:** Short overdraw allowance
4. **Credit Withdrawal:** Allow extracting unused credits
5. **Batch Operations:** Bulk lease creation
6. **Scheduled Closure:** Lease with end date
7. **Usage Reports:** Periodic settlement with receipts
8. **Multi-Provider Leases:** Items from different providers
9. **Credit Transfers:** Move credits between accounts
10. **Provider Disputes:** On-chain dispute resolution

### Known Limitations

1. **No Partial Credit Withdrawal:** Credits locked until spent
2. **No Dispute Mechanism:** Trust-based provider relationship  
3. **No Grace Period:** Immediate closure on exhaustion
4. **No Lease Modification:** Cannot add/remove items after creation
5. **Linear WithdrawAll:** O(n) for many leases (capped at 100)
6. **Single Provider Per Lease:** Cannot mix providers in one lease
7. **Lease Queries Return Stored State:** `Lease`, `Leases`, etc. return stored `last_settled_at` (use `WithdrawableAmount` for real-time)
8. **No Denom Conversion:** Must fund credit with exact denoms required by target SKUs

### Migration Considerations

When implementing breaking changes:
- Settle all active leases before migration
- Preserve credit account balances
- Maintain lease history
- Update indexes atomically
- Version proto messages appropriately
