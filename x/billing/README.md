# x/billing

The `billing` module provides a credit-based billing system for leasing SKU resources. It enables tenants to fund credit accounts and create leases for SKU items, with automatic settlement and provider withdrawal capabilities.

## Concepts

### Credit Accounts

Each tenant has a credit account with a derived address. Credit accounts can hold any token denomination that matches the SKU's base_price denomination.

- **Credit Address**: Deterministically derived from the tenant's address
- **Balances**: Current credit balances (supports multiple denominations)
- **Top-up**: Anyone can fund a tenant's credit account with any token

### Credit Reservation System

The credit reservation system prevents overbooking by tracking a consumable
guarantee for each modern lease. When a lease is created, its initial
reservation is `rate_per_second × min_lease_duration` in each denomination.
That amount is stored in `Lease.reservation.remaining_amounts` and included in
the tenant's aggregate `CreditAccount.reserved_amounts`.

**Why Reservations Matter:**

Without reservations, a tenant could create multiple leases that exceed their credit balance:
```
Tenant balance: 100 credits
MinLeaseDuration: 1 hour

Lease A: 30/hour → Check: 100 >= 30 ✓ Created
Lease B: 30/hour → Check: 100 >= 30 ✓ Created
Lease C: 30/hour → Check: 100 >= 30 ✓ Created
Lease D: 30/hour → Check: 100 >= 30 ✓ Created

Result: 4 leases × 30/hour = 120/hour liability, only 100 credits
→ Overbooking! Providers don't get paid fairly.
```

With the reservation system:
```
Tenant balance: 100 credits, reserved: 0
Available: 100 - 0 = 100

Lease A: 30/hour → Reserve 30 → Available: 100 - 30 = 70 ✓
Lease B: 30/hour → Reserve 30 → Available: 70 - 30 = 40 ✓
Lease C: 30/hour → Reserve 30 → Available: 40 - 30 = 10 ✓
Lease D: 30/hour → Reserve 30 → Available: 10 < 30 ✗ Rejected

Result: 3 leases, properly collateralized.
```

**Available Credit Calculation:**
```
AvailableCredit = CreditBalance - ReservedAmounts
```

New leases can only be created if `AvailableCredit >= NewLeaseReservation` for all required denominations.

In consensus version 4, the aggregate is an exact accounting identity, not a
recalculation of every lease's original nominal reservation:

```
R = SUM(live modern Lease.reservation.remaining_amounts) + U
```

Here `R` is `CreditAccount.reserved_amounts` and `U` is
`CreditAccount.unattributed_reserved_amounts`, the explicit shared allocation
for live historical leases whose individual guarantees cannot be reconstructed.
Terminal leases and historical leases have an initialized but empty
`Lease.reservation`.

Settlement spends the target lease's own remaining tranche before unreserved
credit while preserving every other lease's guarantee. Per denomination, a
lease with allocation `A`, tenant balance `B`, and aggregate reservation `R`
may spend at most `B - (R - A)`. The amount paid from `A` is subtracted from
both `A` and `R`. A modern lease's terminal transition then releases exactly
its remaining `A`, never a nominal amount recomputed from current parameters.

**Reservation Lifecycle:**
- **Added**: When a lease is created (enters PENDING state)
- **Maintained**: When a lease is acknowledged (transitions to ACTIVE state)
- **Consumed**: Settlement reduces the lease's own remaining tranche and the account aggregate by the same amount
- **Released**: A terminal transition subtracts exactly the lease's remaining tranche; historical transitions decrement `unattributed_lease_count` in O(1), and the explicit cohort `U` is released when that count reaches zero

**Parameter Change Protection:**

New leases store `MinLeaseDurationAtCreation` to ensure consistent reservation calculation regardless of subsequent governance changes to the `MinLeaseDuration` parameter. Legacy leases created before this field was persisted have a zero value.

### Multi-Denomination Support

The billing module supports multiple token denominations:
- Each SKU defines its own `base_price` with a specific denomination
- Credit accounts can hold multiple denominations
- When creating a lease, the credit account must have sufficient balance in the denominations used by the leased SKUs
- Settlement transfers use the denomination specified in the SKU's locked price

### Leases

A lease represents an agreement between a tenant and a provider for one or more SKU items.

- **Tenant**: The address that created and pays for the lease
- **Provider UUID**: All SKUs in a lease must belong to the same provider
- **Items**: List of SKU items with locked-in prices and quantities
- **State**: PENDING, ACTIVE, CLOSED, REJECTED, or EXPIRED
- **Settlement**: Accrued charges are calculated based on time since last settlement

### Lease Lifecycle

Leases follow a two-phase commit pattern:

1. **PENDING**: Tenant creates lease, credit is locked, awaiting provider acknowledgement
2. **ACTIVE**: Provider acknowledges, billing starts from acknowledgement time
3. **CLOSED**: Lease terminated normally (by tenant, provider, or credit exhaustion)
4. **REJECTED**: Provider rejected the pending lease, or tenant cancelled it
5. **EXPIRED**: Pending lease timed out (exceeded `pending_timeout` parameter)

### Custom Domains (per-item)

Each `LeaseItem` can carry an optional `custom_domain` — an FQDN that the
provider routes to that item's container with a TLS cert provisioned via
HTTP-01. Domains are set or cleared after lease creation via
`MsgSetItemCustomDomain` and addressed by `service_name` (use `""` for a
1-item legacy lease).

**Authorisation.** The lease tenant, the module authority, and any address in
`params.allowed_list` may set or clear a domain. The lease must be in
`PENDING` or `ACTIVE` state; closed/rejected/expired leases are immutable.

**Validation rules** (`IsValidFQDN`):
- 1 to **253** bytes (`MaxCustomDomainLength`); lowercase only.
- ≥ 1 dot separator (at least 2 labels).
- Each label is RFC 1123 (1-63 alphanumerics + hyphens, no leading/trailing
  hyphen).
- The TLD label must contain at least one non-digit (rejects raw IPs).
- No scheme (`://`), no leading or trailing dot, and none of the literal characters `/`, space, `\t` (tab), `@`, `*`, `?`, `#`.

**Reserved suffixes** (`params.reserved_domain_suffixes`). Tenants cannot
claim a domain that matches any reserved suffix. Each suffix entry must begin
with `.` (e.g. `.barney0.manifest0.net`); a domain matches when it ends with
the suffix at a label boundary, or equals the suffix's apex
(`barney0.manifest0.net`). The check is case-insensitive and fail-closed on
malformed entries. Reserved suffixes are tunable via governance — providers
that bring up new wildcard zones can be added without a chain upgrade.

**Uniqueness.** A domain may be claimed by at most one PENDING/ACTIVE lease
item at a time. The keeper maintains a `CustomDomainIndex` reverse-lookup
(prefix `0x0C`, value `CustomDomainTarget{lease_uuid, service_name}`) so
`QueryLeaseByCustomDomain` returns the routing target in O(1) without
iterating items. Closing, rejecting, expiring, or auto-closing a lease frees
the index entry. Re-setting the same domain on the same item is idempotent
(no event, no state change).

**Multi-item legacy leases cannot use custom_domain.** When a multi-item
lease was created without `service_name`s (legacy mode), the addressing key
is empty for all items and the lookup is ambiguous. Recreate the lease in
service-name mode instead.

**Events**:
- `lease_custom_domain_set` — attributes: `lease_uuid`, `tenant`, `provider_uuid`, `service_name`, `custom_domain`, `set_by`
- `lease_custom_domain_cleared` — same attributes (`custom_domain` carries the previous value)

`set_by` ∈ {`tenant`, `authority`, `allowed`} indicates the role under which
the call was authorised.

### Price Locking

When a lease is created, the current prices of all SKUs are locked in for the duration of the lease. Price changes to SKUs only affect newly created leases.

### Settlement

Settlement calculates the accrued charges since the last settlement based on:
- Locked price per SKU (per second rate)
- Quantity of each SKU item
- Time elapsed since last settlement

Settlement is performed **lazily** (on-touch):
- When a provider withdraws from a lease
- When a lease is closed

This design keeps on-chain operations light and avoids per-block token transfers.
Charges use complete elapsed seconds. After a successful live settlement,
`last_settled_at` advances only through the seconds actually charged, leaving a
remainder of less than one second for the next settlement. Consequently, many
sub-second touches charge exactly the same total as one settlement over their
combined interval. A terminal close charges every complete remaining second,
then records the exact close time and discards the final fractional remainder.

### Overdraw and Auto-Close

If the credit this lease may safely spend is insufficient to cover accrued
charges, the billing module automatically closes it. Lease-spendable credit is
its own tranche plus genuinely unreserved balance; reservations belonging to
other leases remain protected. This happens through **lazy evaluation**
("check on touch") during write operations:

**When auto-close is triggered:**
- When withdrawing from a lease (`MsgWithdraw`)
- When attempting to close a lease (`MsgCloseLease`)

**How it works:**
1. When a lease is "touched" during a transaction, the system calculates accrued charges
2. If accrued amount >= lease-spendable credit `B - (R - A)`:
   - Performs final settlement (transfers that spendable amount to the provider)
   - Closes the lease automatically
   - Emits an event whose type depends on the trigger path (see below)

**Auto-close emits a different event on each path** — there is no single event that covers them all:

| Trigger | Event | Distinguishing attribute |
|---|---|---|
| `MsgCloseLease` | `lease_closed` | `closed_by = credit_exhaustion` on a genuine exhaustion (shortfall, accrual overflow, or a zero-balance close); a fully-paid close keeps the caller's `reason`/role |
| `MsgWithdraw` (specific lease UUIDs) | `provider_withdraw` | `auto_closed = true`; `amount` and `payout_address` report the final transfer |
| `MsgWithdraw` (provider-wide) | `lease_auto_closed` | `reason = credit_exhausted`; `amount` and `payout_address` report the final transfer |

A consumer that subscribes only to `lease_auto_closed` will miss credit-exhaustion closures triggered via `MsgCloseLease` or specific-lease `MsgWithdraw`.

**Design rationale:**
- **O(1) per lease check**: Instead of O(n) scanning all leases in EndBlock
- **Scalability**: Supports millions of leases without performance degradation
- **On-demand**: Only processes leases when they're actually used
- **No consensus overhead**: EndBlock remains lightweight
- **Transaction safety**: Auto-close only happens in transactions where state changes are committed

**Note**: Queries (`QueryLease`, `QueryLeases`, etc.) do NOT trigger auto-close. They return the stored state. Auto-close only happens during write operations (Withdraw, CloseLease) to ensure state changes are properly committed.

**Note**: During lazy settlement (withdrawal or manual close), the transfer is
capped at `B - (R - A)`, not the raw credit balance. This prevents one lease
from consuming another lease's reservation.

## State

Billing API, transaction, query, and genesis protobufs expose account addresses
as Bech32 strings. Consensus version 3 introduced separate disk-only value codecs:
all account identities inside Params, Lease, and CreditAccount values are
persisted as raw address bytes. Address-bearing keys and secondary indexes use
the SDK's `AccAddressKey` byte codec as well. The v2→v3 module migration rewrites
every legacy value in deterministic key order; it does not rebuild indexes.
A separate ascending credit-account pass uses the byte-addressed tenant/state
index in fixed PENDING→ACTIVE order to reconstruct cached counts and reconcile
fully verifiable reservations to the exact live floor without decoding
terminal lease history.
Equivalent allowed-list Bech32 spellings are collapsed by decoded address
identity while preserving first-seen list order.
For tenants with live legacy leases, it raises only deficient denominations to
the known non-legacy floor and preserves unknown excess. When no live legacy
lease remains, the reservation is fully reconstructible and is reconciled
exactly. Bank balances are not changed.

Consensus version 4 converts that aggregate-only state into consumable
allocations without minting, burning, or transferring tokens. For each tenant
and denomination, the allocation budget is bounded by both the pre-v4 aggregate
and the bank balance. The repaired aggregate must cover the tenant's complete
modern PENDING nominal sum; a short aggregate is corruption and halts the
migration. Bank underbacking is handled separately and tenant-wide. If every
denomination covers the PENDING sum, every modern PENDING lease keeps its exact
claim. If any denomination is short, all modern PENDING leases for that tenant
expire atomically at the upgrade block, receive empty tranches, and have their
counts and state-dependent indexes repaired. The cutover never creates a
partial pending guarantee, chooses arbitrary winners, or mints backing.

Modern ACTIVE nominal claims and one opaque live-legacy cohort share the
remaining bank-backed historical budget with Hamilton's largest-remainder
method, using lease UUID order and then the legacy cohort as the deterministic
tie-break. The resulting state satisfies `R = sum(A) + U` and records the exact
live historical cohort size in `unattributed_lease_count`. An expired request
remains as terminal history and transfers no funds; its client may submit a new
lease after the upgrade under current prices, parameters, limits, and available
credit.

A pre-v4 (v2/v3) genesis import applies the same policy at genesis block time
before its first billing-store write. During a live v2→v4 upgrade, v2→v3 can
stage writes before v3→v4 detects corrupt aggregate state. If the migration
halts, the upgrade handler returns an error and the upgrade block does not
commit, so none of the staged migration writes become chain state. A bank
shortfall alone expires the tenant's modern PENDING cohort and does not halt the
cutover.

### Storage Key Prefixes

| Prefix | Key Type | Description |
|--------|----------|-------------|
| `0x00` | Params | Module parameters |
| `0x01` | Lease | Primary lease storage (UUID → Lease) |
| `0x02` | LeaseSequence | Sequence counter for UUIDv7 generation |
| `0x03` | LeaseByTenant | Index: tenant → lease UUIDs |
| `0x04` | LeaseByProvider | Index: provider UUID → lease UUIDs |
| `0x05` | CreditAccount | Credit accounts (tenant → CreditAccount) |
| `0x06` | CreditAddressIndex | Reverse lookup: credit address → tenant |
| `0x07` | LeaseByState | Index: state → lease UUIDs (for pending expiration) |
| `0x08` | LeaseByProviderState | Compound index: provider+state → lease UUIDs |
| `0x09` | LeaseByTenantState | Compound index: tenant+state → lease UUIDs |
| `0x0A` | LeaseBySKU | Many-to-many index: SKU UUID → lease UUIDs |
| `0x0B` | LeaseByStateCreatedAt | Compound index: state+created_at → lease UUIDs |
| `0x0C` | CustomDomainIndex | Reverse index: custom_domain → CustomDomainTarget (lease_uuid + service_name) |

`SetLease` atomically reconciles both manually maintained lease projections:
the SKU membership set and the state-dependent custom-domain claims.
`SetCreditAccount` maintains the byte-keyed credit-address reverse index. The
module registers a bidirectional derived-index invariant that rejects missing,
stale, false, or mismatched entries in any of these three maps; the reservation
accounting/backing invariant remains a separate route.

### Params

Module parameters stored at key `0x00`:

| Field | Type | Description |
|-------|------|-------------|
| max_leases_per_tenant | uint64 | Maximum active leases per tenant; rechecked against each tenant's post-acknowledgement batch count (must be > 0) |
| max_items_per_lease | uint64 | Maximum items per lease (default: 20, hard limit: 100) |
| min_lease_duration | uint64 | Minimum lease duration in seconds (default: 3600 = 1 hour) |
| max_pending_leases_per_tenant | uint64 | Maximum pending leases per tenant (default: 10) |
| pending_timeout | uint64 | Hard acknowledgement window after `created_at`; exact cutoff is allowed, later block times are rejected (default: 1800 = 30 minutes, min: 60, max: 86400) |
| allowed_list | []string | List of addresses allowed to create leases on behalf of tenants and to set lease custom_domains |
| reserved_domain_suffixes | []string | DNS suffixes (each beginning with `.`) that tenants are forbidden from claiming via `set-item-custom-domain`. Used to gate provider wildcard zones. Tunable via governance. |

**Validation Constraints:**
- `max_leases_per_tenant`: Must be > 0 and ≤ 10,000
- `max_items_per_lease`: Must be > 0 and ≤ 100 (hard limit)
- `min_lease_duration`: Must be > 0 and ≤ 2,592,000 (30 days)
- `max_pending_leases_per_tenant`: Must be > 0 and ≤ 1,000
- `pending_timeout`: Must be between 60 seconds (1 minute) and 86400 seconds (24 hours)
- `reserved_domain_suffixes`: Each entry must begin with `.`, the substring after the dot must be a valid FQDN, no duplicates

**Note:** There is no global `denom` parameter. Each SKU defines its own denomination in its `base_price`, enabling multi-denom billing.

### Hard Limits (Constants)

These values are compile-time constants and cannot be changed via governance:

| Constant | Value | Description |
|----------|-------|-------------|
| `MaxItemsPerLeaseHardLimit` | 100 | Absolute maximum items per lease |
| `MaxReservedDenomsPerCreditAccount` | 1000 | Maximum aggregate reservation denom cardinality that a new lease may create; historical over-limit accounts remain releasable and cannot grow |
| `MaxQuantityPerItem` | 1,000,000,000 | Maximum quantity per lease item (1 billion). Defined in `types/errors.go`. |
| `MaxPendingLeaseExpirationsPerBlock` | 100 | Maximum pending lease expirations processed per block (DoS protection) |
| `DefaultProviderWithdrawLimit` | 50 | Default number of leases processed per provider-wide withdraw call (can be increased to MaxBatchLeaseSize) |
| `MaxBatchLeaseSize` | 100 | Hard limit for any batch operation. For provider-wide withdraw: configurable via `--limit` up to this value. For specific lease operations: maximum UUIDs per call. |
| `MaxRejectionReasonLength` | 256 | Maximum UTF-8 bytes for lease rejection reason |
| `MaxClosureReasonLength` | 256 | Maximum UTF-8 bytes for lease closure reason |
| `MaxCustomDomainLength` | 253 | Maximum bytes for `LeaseItem.custom_domain` (RFC 1035 max FQDN length) |
| `MaxWithdrawCursorLen` | 64 | Maximum bytes of the opaque `--key` cursor for provider-wide withdraw (a lease UUID is 36 bytes). Defined in `types/msgs.go`. |
| `CreditAccountAddressPrefix` | `billing/credit/` | Prefix used for deterministic credit address derivation |
| `DefaultCreditAccountBalanceQueryLimit` | 100 | Default bank-balance page size for `CreditAccount` |
| `MaxCreditAccountBalanceQueryLimit` | 1000 | Maximum bank-balance page size for `CreditAccount` |
| `DefaultProviderWithdrawableQueryLimit` | 50 | Default page size for the ProviderWithdrawable query, matching provider-wide `MsgWithdraw`. The request's old top-level `limit` field was removed; proto field 2 is reserved. |
| `MaxProviderWithdrawableQueryLimit` | 100 | Maximum page size for the ProviderWithdrawable query (`pagination.limit` is clamped to the transaction batch ceiling) |
| `MaxCreditEstimateLeaseItems` | 100,000 | Maximum active lease items aggregated by one unpaginated `CreditEstimate` request |

> **CreditEstimate iteration** follows the ACTIVE count stored on the tenant's credit account rather than the current governance limit. It enforces the conservative 11,000 ACTIVE bound, a 100,000-item work bound, and exact count/index agreement. `CreditAccount` does not scan leases: it returns all bank balances through bounded cursor pages (default 100, max 1000). Both queries reject rather than silently truncate work outside their documented contracts.

### Batch Operations

Several messages support batch processing of multiple leases in a single transaction:

| Message | Max Leases | Behavior |
|---------|------------|----------|
| `MsgAcknowledgeLease` | 100 | All leases must be PENDING, same provider, within the hard timeout, and within each tenant's post-batch active cap. Atomic. |
| `MsgRejectLease` | 100 | All leases must be PENDING, same provider. Atomic. |
| `MsgCancelLease` | 100 | All leases must be PENDING, same tenant. Atomic. |
| `MsgCloseLease` | 100 | All leases must be ACTIVE, authorized for sender. Atomic. |
| `MsgWithdraw` (specific) | 100 | All leases must be ACTIVE, same provider. Atomic. |
| `MsgWithdraw` (provider-wide) | 50 (default), 100 (max) | Ordered best effort per lease; pass `next_key` back as `--key` until `has_more` is false and retry every UUID reported in `failed_lease_uuids`. |

**Atomic Batch Operations:** When providing specific lease UUIDs, the operation is atomic—all leases succeed or all fail. If any lease fails validation (wrong state, unauthorized, etc.), the entire transaction is rejected.

**Provider-Wide Withdraw:** Unlike specific-lease operations, provider-wide withdraw is paginated and best effort per lease. It processes up to `--limit` leases (default 50, max 100) and returns `has_more: true` plus an opaque `next_key` cursor if more remain. Pass `next_key` back as `--key` on the next call and repeat until `has_more: false`; calling again without `--key` restarts from the first lease. Only ACTIVE leases are considered — CLOSED leases are already fully settled at close and are skipped, so every page is dense with ACTIVE leases. Leases are processed in ascending UUID order; `next_key` is the last lease UUID of the page and the next call resumes strictly after it. Failed lease caches are discarded and their UUIDs are returned in that same order in `failed_lease_uuids`; retain and retry those UUIDs because the next cursor resumes after them.

### Lease

Leases are stored at key prefix `0x01`. The table gives public protobuf types;
the disk-only codec persists account identities as raw address bytes.

| Field | Public/API Type | Description |
|-------|-----------------|-------------|
| uuid | string | UUIDv7 unique identifier |
| tenant | Bech32 string | Tenant address; raw SDK address bytes on disk |
| provider_uuid | string | Provider UUID (from SKU module) |
| items | []LeaseItem | List of SKU items |
| state | LeaseState | PENDING, ACTIVE, CLOSED, REJECTED, or EXPIRED |
| created_at | Timestamp | Creation time (credit locked) |
| acknowledged_at | Timestamp | Provider acknowledgement time (billing starts) |
| closed_at | Timestamp | Closure time |
| rejected_at | Timestamp | Rejection time |
| expired_at | Timestamp | Expiration time |
| last_settled_at | Timestamp | Accrual cursor through which complete seconds have settled; an ACTIVE lease retains any sub-second remainder here, while a CLOSED lease sets it to `closed_at` |
| rejection_reason | string | Provider's rejection reason (max 256 chars) |
| closure_reason | string | Closure reason (max 256 chars) |
| meta_hash | bytes | Hash/reference to off-chain deployment data (max 64 bytes, immutable) |
| min_lease_duration_at_creation | uint64 | Snapshot of `min_lease_duration` param at creation (for consistent reservation calculation) |
| reservation | LeaseReservation | Remaining consumable reservation for a modern lease; initialized empty for terminal and historical leases |

### LeaseItem

| Field | Type | Description |
|-------|------|-------------|
| sku_uuid | string | SKU UUID being leased |
| quantity | uint64 | Number of instances |
| locked_price | Coin | Price locked at creation (per second rate, includes denom) |
| service_name | string | Optional DNS-label for stack deployments (all-or-nothing per lease, unique within lease) |
| custom_domain | string | Optional FQDN (≤253 bytes) routed to this item by the provider. Mutable via `MsgSetItemCustomDomain`. Globally unique while the lease is PENDING/ACTIVE; the keeper enforces uniqueness through the `CustomDomainIndex` reverse-index. |

> **Note on `service_name` and `custom_domain`:** these two fields are compute-specific and live on `LeaseItem` today as a pragmatic shortcut. When a non-compute lease kind ships, both fields will be migrated to a future `x/deployment` module via a state migration (tracked: ENG-80).

### CreditAccount

Credit accounts are stored at key prefix `0x05`. Address strings below are the
public protobuf view; the disk-only codec persists their raw SDK address bytes.

| Field | Public/API Type | Description |
|-------|-----------------|-------------|
| tenant | Bech32 string | Tenant address; raw SDK address bytes on disk |
| credit_address | Bech32 string | Derived credit account address; raw SDK address bytes on disk |
| active_lease_count | uint64 | Number of ACTIVE leases |
| pending_lease_count | uint64 | Number of PENDING leases |
| reserved_amounts | []Coin | Exact aggregate `sum(live modern remaining reservations) + unattributed_reserved_amounts` |
| unattributed_reserved_amounts | []Coin | Explicit shared reservation for the live historical cohort whose per-lease guarantees cannot be reconstructed |
| unattributed_lease_count | uint64 | Exact number of live historical leases sharing `unattributed_reserved_amounts`; enables O(1) terminal release |

Note: The actual balance is tracked by the bank module at the `credit_address`.
Query the bank module or use cursor-paginated `QueryCreditAccount`, which
includes one ordered balance page (default 100, maximum 1000).
Reverse pages follow `x/bank` and return both coin lists in descending
denomination order; Go callers must call `Sort()` before using `sdk.Coins`
operations that require canonical ascending order.
New leases begin with a nominal `rate × min_lease_duration` tranche, but
settlement consumes it; therefore `reserved_amounts` tracks remaining
guarantees, not a fixed nominal sum.

## State Transitions

### Fund Credit

Transfers tokens from sender to tenant's credit account.

```
sender → credit_address
```

### Create Lease (PENDING)

1. Verify item count ≤ `max_items_per_lease`
2. Verify tenant has a credit account
3. Verify tenant hasn't exceeded max active or pending leases
4. Verify all SKUs exist, are active, and belong to the same provider (locking per-second prices)
5. Verify the provider exists and is active
6. Verify `AvailableCredit >= rate × min_lease_duration` per denom
7. Initialize the lease's remaining tranche `A` to that nominal amount and add the same coins to aggregate `R`
8. Create the lease in PENDING state and increment pending_lease_count

### Acknowledge Lease (PENDING → ACTIVE)

1. Provider verifies they own the SKUs in the lease
2. Revalidate every lease against the hard deadline: `now <= created_at + current pending_timeout`
3. Aggregate the entire batch per tenant and verify every post-batch active count is ≤ `max_leases_per_tenant`
4. After all gates pass, atomically set every lease to ACTIVE
5. Set acknowledged_at and last_settled_at to current block time (billing starts)
6. Decrement pending_lease_count and increment active_lease_count

An overdue lease can remain stored as PENDING until the rate-limited EndBlocker reaches it, but it
cannot be acknowledged. Providers may still reject it, and tenants may still cancel it.

### Reject Lease (PENDING → REJECTED)

1. Provider verifies they own the SKUs in the lease
2. Set lease state to REJECTED
3. Set rejected_at and rejection_reason
4. Decrement pending_lease_count
5. Release the lease's exact remaining reservation tranche

### Cancel Lease (Tenant cancels PENDING)

1. Verify sender is the lease tenant
2. Verify lease is in PENDING state
3. Set lease state to REJECTED
4. Decrement pending_lease_count
5. Release the lease's exact remaining reservation tranche

### Expire Lease (EndBlocker)

The EndBlocker automatically expires pending leases that exceed the `pending_timeout`:

1. Query all leases in PENDING state using the state index
2. For each lease where `now > created_at + pending_timeout`:
   - Set lease state to EXPIRED
   - Set expired_at timestamp
   - Decrement pending_lease_count
   - Release the lease's exact remaining reservation tranche

**Rate Limiting:** To prevent DoS attacks, the EndBlocker processes a maximum of **100 lease expirations per block** (`MaxPendingLeaseExpirationsPerBlock`). If more than 100 leases need to expire, the remaining leases are processed in subsequent blocks. This uses a two-pass approach to avoid iterator invalidation during state modification.

`pending_timeout` is a hard acknowledgement deadline independently of this cleanup schedule.
Acknowledgement succeeds exactly at the cutoff and fails strictly after it, including before
EndBlock in the same block and while an overdue lease is waiting behind the expiration rate limit.

### Close Lease (ACTIVE → CLOSED)

1. Calculate accrued charges since last settlement
2. Transfer `min(accrued, B - (R - A))` from credit to the provider, consuming
   this lease's tranche before genuinely unreserved credit and preserving every
   other lease's guarantee
3. Set lease state to CLOSED
4. Record closed_at timestamp
5. Decrement active_lease_count
6. Release the lease's exact remaining reservation tranche

### Withdraw

1. Calculate accrued charges since last settlement
2. Transfer at most this lease's spendable credit `B - (R - A)` to the provider
3. Auto-close on a shortfall/credit exhaustion; otherwise update
   `last_settled_at` through the complete seconds charged, retaining any
   sub-second remainder

## Messages

The billing module supports the following transaction messages:

| Message | Description |
|---------|-------------|
| `MsgFundCredit` | Fund a tenant's credit account |
| `MsgCreateLease` | Create a new lease (starts in PENDING state) |
| `MsgCreateLeaseForTenant` | Create a lease on behalf of a tenant (authority/allowed only) |
| `MsgAcknowledgeLease` | Provider acknowledges a pending lease (→ ACTIVE) |
| `MsgRejectLease` | Provider rejects a pending lease |
| `MsgCancelLease` | Tenant cancels their own pending lease |
| `MsgCloseLease` | Close an active lease |
| `MsgWithdraw` | Withdraw accrued funds (specific leases or provider-wide) |
| `MsgSetItemCustomDomain` | Set or clear `custom_domain` on a specific lease item (tenant / authority / `allowed_list`) |
| `MsgUpdateParams` | Update module parameters (authority only) |

For detailed message definitions, request/response formats, and CLI usage, see [API Reference](docs/API.md#cli-commands).

## Queries

| Query | Description |
|-------|-------------|
| Params | Get module parameters |
| Lease | Get a lease by ID |
| Leases | List all leases with pagination |
| LeasesByTenant | List leases for a tenant |
| LeasesByProvider | List leases for a provider (use `--state pending` filter for pending leases) |
| LeasesBySKU | List leases using a specific SKU |
| CreditAccount | Get a tenant's credit account plus one cursor-paginated page of all bank balances and page-aligned available balances |
| CreditAccounts | List all credit accounts |
| CreditEstimate | Report gross raw-bank-balance runway at the aggregate ACTIVE rate (not reservation-aware or an auto-close forecast) |
| CreditAddress | Derive credit address for a tenant |
| WithdrawableAmount | Get withdrawable amount for a lease |
| ProviderWithdrawable | Ordered best-effort dry-run of the current ACTIVE-lease page. Failed lease simulations are discarded and reported in `failed_lease_uuids`; successful virtual effects feed later leases, while no query state commits. The page is an execution estimate, not an additive snapshot. Every forward page is comparable to one provider-wide withdrawal because the query limit is capped at the transaction maximum of 100. After it commits, query the next segment with the prior query `pagination.next_key` and withdraw it with the prior transaction `next_key`; the two cursor contracts are not interchangeable. `lease_count` and `failed_lease_uuids` match the comparable transaction, including successful zero-transfer auto-closes in the count. |
| LeaseByCustomDomain | Look up the active or pending lease that has claimed a given custom_domain (and the `service_name` of the matching item) |

Generic list-query pages default to 100 rows and are capped at 1000 rows. An
oversized requested limit is clamped; callers continue with the opaque
`pagination.next_key` cursor. Standard offset and explicitly requested exact
total compatibility is available for the five collection/index list queries.
Unfiltered requests may inspect at most 20,000 physical rows; value-filtered
`LeasesBySKU` requests with a state filter retain a 1,000-row ceiling in every
pagination mode.
Requests that cannot produce an exact page or total within the applicable
ceiling fail. An omitted or zero limit does not implicitly request a total.
Larger histories must use cursors. `CreditAccount` and `ProviderWithdrawable`
remain cursor-only because their per-row work is more expensive.

**Events**: See [API Reference - Events](docs/API.md#events) for the complete list of events emitted by this module.

## Client

For complete CLI commands, gRPC endpoints, and REST API documentation, see [API Reference](docs/API.md).

**Quick examples:**
```bash
# Fund credit, create lease, query status
manifestd tx billing fund-credit [tenant] 1000000upwr --from [key]
manifestd tx billing create-lease [sku-uuid]:2 --from [key]
manifestd query billing leases-by-tenant [tenant] --state active
```

## Default Parameters

| Parameter | Default Value |
|-----------|---------------|
| max_leases_per_tenant | 100 |
| max_items_per_lease | 20 (hard limit: 100) |
| min_lease_duration | 3600 (1 hour) |
| max_pending_leases_per_tenant | 10 |
| pending_timeout | 1800 (30 minutes) |

## Authorization

For detailed authorization matrix, see [API Reference - Authorization](docs/API.md#authorization).

**Summary:**
- **Fund Credit**: Anyone can fund any tenant's credit account
- **Create Lease**: Tenants create their own leases; Authority/allow-list can create for others
- **Acknowledge/Reject Lease**: Provider or Authority
- **Cancel Lease**: Tenant (own pending leases only)
- **Close Lease**: Tenant, Provider, or Authority
- **Withdraw**: Provider or Authority
- **Set Item Custom Domain**: Tenant (own leases), Authority, or any address in `params.allowed_list`

## Integration with SKU Module

The billing module depends on the SKU module for:
- Validating SKU existence and active status
- Getting SKU prices for price locking (see [Price Locking](docs/DESIGN_DECISIONS.md#decision-4-price-locking-at-lease-creation))
- Getting provider information for authorization and payouts
- Per-second rate calculation (see [SKU Pricing](../sku/README.md#pricing-and-exact-divisibility))

The SKU module remains independent and does not know about the billing module.

**Key SKU Module Concepts for Billing:**
- [Provider and Payout Addresses](../sku/README.md#provider) - Where lease payments are sent
- [SKU Deactivation Impact](../sku/README.md#deactivation-impact-on-existing-leases) - How deactivated SKUs affect leases
- [Billing Units](../sku/README.md#billing-units) - Per-hour vs per-day pricing

## Known Limitations

### Credit Withdrawal Policy

There is no mechanism to withdraw unused credit from a credit account. Once tokens are funded, they can only be spent on leases. This mimics typical cloud providers (AWS credits, etc.) and prevents gaming of the system. Unused credit remains available for future leases.

### Provider/SKU Deactivation

When a provider or SKU is deactivated:
- Active leases continue with locked-in prices
- Providers can still withdraw accrued funds
- No new leases can be created with deactivated providers/SKUs

### Provider-Wide Withdraw Pagination

Provider-wide withdraw mode (`--provider` flag) processes up to 50 leases per call by default (max 100). Use `--limit` to increase, and pass the response's `next_key` back as `--key` to page through all leases until `has_more` is false:
```bash
manifestd tx billing withdraw --provider [provider-uuid] --limit 100 --from provider-key
# then, using next_key from the previous response:
manifestd tx billing withdraw --provider [provider-uuid] --limit 100 --key [next_key] --from provider-key
```

> `next_key` is a `bytes` value, so it appears base64-encoded in the JSON/CLI response. Pass that string verbatim to `--key` — it is not a raw UUID.
> This is the transaction response cursor only. Do not substitute a
> `ProviderWithdrawable` query's `pagination.next_key`; query pagination points
> at the first unread entry, while the transaction resumes strictly after its
> last processed lease.
>
> A provider-wide page can succeed while individual leases fail. Save every
> ordered UUID in `failed_lease_uuids` before advancing `next_key`, correct the
> underlying problem, then retry those leases explicitly. The `batch_withdraw`
> event carries the same `failed_lease_count` and comma-separated
> `failed_lease_uuids` for transaction-indexing clients.

For detailed scalability analysis, time manipulation considerations, and future improvement plans, see [Architecture](docs/ARCHITECTURE.md#scalability-considerations).

## Genesis Validation

The billing module performs comprehensive validation during genesis initialization to ensure state consistency.

### Reservation Invariant Validation

Version 4 genesis state enforces the exact consumable reservation invariant:

```
CreditAccount.ReservedAmounts
    == SUM(Lease.Reservation.RemainingAmounts for live modern leases)
       + CreditAccount.UnattributedReservedAmounts
```

A modern PENDING lease must retain its full nominal creation-time reservation;
a modern ACTIVE lease may retain any non-negative amount up to nominal after
settlement. Terminal and historical leases have empty lease-side tranches. `U`
is permitted only while a live historical lease exists, and
`unattributed_lease_count` must exactly equal the number of those live leases.
For a pre-v4 import, any modern PENDING lease that cannot receive that full
guarantee under the tenant-wide bank-backing policy is first transitioned to
EXPIRED; there is no valid partially reserved PENDING state.

The import path also accepts a complete pre-v4 aggregate-only (v2/v3) export (all
`Lease.reservation` messages absent). Static validation preserves the earlier
import-safe rules, then `InitGenesis` applies the same bank-backed,
tenant-wide PENDING-cohort/Hamilton conversion as the v3→v4 module migration
before its first billing-store write. Mixed absent/present reservation wrappers
are rejected. Import preparation mirrors v2→v3: it reconstructs the modern
reservation floor by decoded tenant identity, raises an account with a live
opaque legacy cohort to at least that floor while preserving unknown excess,
and otherwise reconciles the aggregate exactly to the floor. This bounded
repair handles historical legacy-release clamping without minting or moving
bank funds. `ValidateStrict()` remains non-repairing for newly authored state.
Static `validate-genesis` cannot inspect bank balances. A later bank shortfall
causes atomic expiration of every modern PENDING lease for the affected tenant,
followed by count recomputation, index construction, and allocation of the
remaining bank-backed budget to modern ACTIVE leases and the live historical
cohort. It does not fail the import, mint credit, partially reserve a pending
lease, or select a favored subset. That normalization applies only to complete
pre-v4 aggregate-only input. An already-v4 consumable import is not re-planned:
if its aggregate is not bank-backed, `InitGenesis` rejects it before the first
billing-store write.

**Validation Steps:**
1. Reject mixed pre-v4/v4 reservation representations
2. Prepare an import copy: canonicalize address aliases and rebuild cached
   counts; for a pre-v4 export, also reconcile the aggregate to the provable
   modern floor using the v2→v3 policy
3. Validate structural invariants and account presence; for v4 state, also
   verify every lease tranche, exact `R = sum(A) + U`, and the exact live
   historical cohort count
4. During `InitGenesis`, expire a pre-v4 under-backed tenant's entire modern
   PENDING cohort, compute the remaining bank-backed allocation, and revalidate
   the resulting v4 counts and accounting; reject an under-backed already-v4
   aggregate
5. Validate timestamps and SKU/provider references before the first billing-store write
6. Persist the normalized leases and build their state-dependent indexes

**Error Examples:**
```
# Mismatch between v4 aggregate and consumable allocations
billing reservation invariant violated: credit account for manifest1abc... has reserved_amounts 500upwr but consumable reservations sum to 600upwr

# Tenant with live reservation claims but missing credit account
billing reservation invariant violated: tenant manifest1def... has live reservation claims but no credit account
```

### Other Genesis Validations

- **Lease UUIDs**: All leases must have valid UUIDv7 format, no duplicates
- **Tenant/Provider addresses**: All addresses must be valid bech32 format
- **Credit address derivation**: Credit addresses must match deterministic derivation from tenant address
- **Lease counts**: Import preparation deterministically rebuilds each credit
  account's active and pending counts from primary lease state, grouped by
  decoded tenant address identity, before validation and persistence.
  `ValidateStrict()` performs no repair and requires newly authored counts to
  match exactly.
- **Lease state consistency**: CLOSED leases must have a non-nil, non-zero `closed_at`. At InitGenesis, `created_at`, `last_settled_at`, and `closed_at` must not be after the block time.
- **Custom domains**: Claims must remain valid and unambiguous. Imports do not replay the current reserved-suffix policy because a claim may predate a later suffix reservation; `ValidateStrict()` applies that policy to newly authored state.
- **Parameter validation**: All params must pass validation constraints

## Simulation Coverage

The state-machine simulator registers bounded operations for `FundCredit`,
tenant `CreateLease`, `AcknowledgeLease`, `RejectLease`, `CancelLease`,
`CloseLease`, `Withdraw`, and both set/replace and clear transitions of
`SetItemCustomDomain`; custom-domain operations exercise both tenant and current
`allowed_list` signers when available. Candidate lists retain
collection/simulation-account slice order; maps are used only for lookup and
are never ranged when building a request.

`CreateLeaseForTenant` is deliberately excluded from randomized transactions.
It is an administrative migration message whose governance authority has no
simulation private key. A delayed governance proposal is also unsound for this
stateful request because its SKU and tenant-credit preconditions can change
before proposal execution. Dedicated keeper, migration, and end-to-end tests
cover that path instead.

`UpdateParams` is also deliberately excluded. It requires the configured POA
authority, which has no simulation private key, while Cosmos SDK governance
proposal messages must instead have the governance module account as their sole
signer. Registering either form would only create guaranteed failures. Parameter
validation and update authorization remain covered by focused keeper/type tests;
randomized genesis supplies bounded valid parameter combinations to simulation.

## Additional Documentation

### User Guides
- [Provider Setup Guide](../sku/docs/PROVIDER_GUIDE.md) - Creating and managing providers
- [SKU Setup Guide](../sku/docs/SKU_GUIDE.md) - Creating and managing SKUs (billable items)
- [Migration Guide](docs/MIGRATION.md) - Guide for authority members migrating off-chain leases
- [Integration Guide](docs/INTEGRATION.md) - Tenant authentication to provider APIs (ADR-036)
- [Troubleshooting](docs/TROUBLESHOOTING.md) - Common errors and solutions
- [API Reference](docs/API.md) - Complete CLI and gRPC/REST API reference
- [Frontend / Integrator Cookbook](../../docs/FRONTEND.md) - Client-side signing recipes and the type-URL / amino-name reference table

### Developer Documentation
- [Architecture](docs/ARCHITECTURE.md) - Internal architecture, data models, and flow diagrams
- [Design Decisions](docs/DESIGN_DECISIONS.md) - Key design decisions and rationale
- [Comparison](docs/COMPARISON.md) - Comparison with Akash and architectural trade-offs
- [Capabilities](docs/CAPABILITIES.md) - Feature overview and future roadmap

### Related Modules
- [SKU Module README](../sku/README.md) - Provider and SKU management (prerequisite for billing)
- [SKU API Reference](../sku/docs/API.md) - SKU module CLI and API documentation
- [SKU Design Decisions](../sku/docs/DESIGN_DECISIONS.md) - SKU architecture rationale
