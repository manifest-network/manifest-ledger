# x/billing

The `billing` module provides a credit-based billing system for leasing SKU resources. It enables tenants to fund credit accounts and create leases for SKU items, with automatic settlement and provider withdrawal capabilities.

## Concepts

### Credit Accounts

Each tenant has a credit account with a derived address. Credit accounts can hold any token denomination that matches the SKU's base_price denomination.

- **Credit Address**: Deterministically derived from the tenant's address
- **Balances**: Current credit balances (supports multiple denominations)
- **Top-up**: Anyone can fund a tenant's credit account with any token

### Credit Reservation System

The credit reservation system prevents overbooking by tracking reserved amounts per tenant. When a lease is created, credits are reserved to guarantee that sufficient funds exist for at least `min_lease_duration` seconds of operation.

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

**Reservation Lifecycle:**
- **Added**: When a lease is created (enters PENDING state)
- **Maintained**: When a lease is acknowledged (transitions to ACTIVE state)
- **Released**: When a lease transitions to CLOSED, REJECTED, or EXPIRED

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

### Overdraw and Auto-Close

If a tenant's credit balance is insufficient to cover accrued charges, the billing module automatically closes their active leases. This happens through **lazy evaluation** ("check on touch") during write operations:

**When auto-close is triggered:**
- When withdrawing from a lease (`MsgWithdraw`)
- When attempting to close a lease (`MsgCloseLease`)

**How it works:**
1. When a lease is "touched" during a transaction, the system calculates accrued charges
2. If accrued amount >= credit balance:
   - Performs final settlement (transfers available balance to provider)
   - Closes the lease automatically
   - Emits an event whose type depends on the trigger path (see below)

**Auto-close emits a different event on each path** — there is no single event that covers them all:

| Trigger | Event | Distinguishing attribute |
|---|---|---|
| `MsgCloseLease` | `lease_closed` | `closed_by = credit_exhaustion` on a genuine exhaustion (shortfall, accrual overflow, or a zero-balance close); a fully-paid close keeps the caller's `reason`/role |
| `MsgWithdraw` (specific lease UUIDs) | `provider_withdraw` | `auto_closed = true` (with `amount = 0`) |
| `MsgWithdraw` (provider-wide) | `lease_auto_closed` | `reason = credit_exhausted` |

A consumer that subscribes only to `lease_auto_closed` will miss credit-exhaustion closures triggered via `MsgCloseLease` or specific-lease `MsgWithdraw`.

**Design rationale:**
- **O(1) per lease check**: Instead of O(n) scanning all leases in EndBlock
- **Scalability**: Supports millions of leases without performance degradation
- **On-demand**: Only processes leases when they're actually used
- **No consensus overhead**: EndBlock remains lightweight
- **Transaction safety**: Auto-close only happens in transactions where state changes are committed

**Note**: Queries (`QueryLease`, `QueryLeases`, etc.) do NOT trigger auto-close. They return the stored state. Auto-close only happens during write operations (Withdraw, CloseLease) to ensure state changes are properly committed.

**Note**: During lazy settlement (withdrawal or manual close), if the credit balance is less than the accrued amount, only the available balance is transferred to the provider.

## State

Billing API, transaction, query, and genesis protobufs expose account addresses
as Bech32 strings. Consensus version 3 uses separate disk-only value codecs:
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
| `MaxQuantityPerItem` | 1,000,000,000 | Maximum quantity per lease item (1 billion). Defined in `types/errors.go`. |
| `MaxPendingLeaseExpirationsPerBlock` | 100 | Maximum pending lease expirations processed per block (DoS protection) |
| `DefaultProviderWithdrawLimit` | 50 | Default number of leases processed per provider-wide withdraw call (can be increased to MaxBatchLeaseSize) |
| `MaxBatchLeaseSize` | 100 | Hard limit for any batch operation. For provider-wide withdraw: configurable via `--limit` up to this value. For specific lease operations: maximum UUIDs per call. |
| `MaxRejectionReasonLength` | 256 | Maximum characters for lease rejection reason |
| `MaxClosureReasonLength` | 256 | Maximum characters for lease closure reason |
| `MaxCustomDomainLength` | 253 | Maximum bytes for `LeaseItem.custom_domain` (RFC 1035 max FQDN length) |
| `MaxWithdrawCursorLen` | 64 | Maximum bytes of the opaque `--key` cursor for provider-wide withdraw (a lease UUID is 36 bytes). Defined in `types/msgs.go`. |
| `CreditAccountAddressPrefix` | `billing/credit/` | Prefix used for deterministic credit address derivation |
| `DefaultProviderWithdrawableQueryLimit` | 100 | Default page size for the ProviderWithdrawable query (`pagination.limit`). The request's old top-level `limit` field was removed; proto field 2 is reserved. |
| `MaxProviderWithdrawableQueryLimit` | 1000 | Maximum page size for the ProviderWithdrawable query (`pagination.limit` is clamped to this) |

> **CreditEstimate / CreditAccount query iteration** follows the lease counts stored on the tenant's credit account rather than the current governance limits. This keeps pre-existing leases visible after limits are lowered and covers historical acknowledgement overshoot state. The stored counts are still clamped to fixed DoS ceilings of 11,000 active iterations and 1,000 pending iterations; corrupt or adversarial imported state above those ceilings is truncated.

### Batch Operations

Several messages support batch processing of multiple leases in a single transaction:

| Message | Max Leases | Behavior |
|---------|------------|----------|
| `MsgAcknowledgeLease` | 100 | All leases must be PENDING, same provider, within the hard timeout, and within each tenant's post-batch active cap. Atomic. |
| `MsgRejectLease` | 100 | All leases must be PENDING, same provider. Atomic. |
| `MsgCancelLease` | 100 | All leases must be PENDING, same tenant. Atomic. |
| `MsgCloseLease` | 100 | All leases must be ACTIVE, authorized for sender. Atomic. |
| `MsgWithdraw` (specific) | 100 | All leases must be ACTIVE, same provider. Atomic. |
| `MsgWithdraw` (provider-wide) | 50 (default), 100 (max) | Paginated; pass `next_key` back as `--key` until `has_more` is false. |

**Atomic Batch Operations:** When providing specific lease UUIDs, the operation is atomic—all leases succeed or all fail. If any lease fails validation (wrong state, unauthorized, etc.), the entire transaction is rejected.

**Provider-Wide Withdraw:** Unlike specific-lease operations, provider-wide withdraw is paginated. It processes up to `--limit` leases (default 50, max 100) and returns `has_more: true` plus an opaque `next_key` cursor if more remain. Pass `next_key` back as `--key` on the next call and repeat until `has_more: false`; calling again without `--key` restarts from the first lease. Only ACTIVE leases are considered — CLOSED leases are already fully settled at close and are skipped, so every page is dense with ACTIVE leases. Leases are processed in ascending UUID order; `next_key` is the last lease UUID of the page and the next call resumes strictly after it.

### Lease

Leases stored at key prefix `0x01`:

| Field | Type | Description |
|-------|------|-------------|
| uuid | string | UUIDv7 unique identifier |
| tenant | string | Tenant address |
| provider_uuid | string | Provider UUID (from SKU module) |
| items | []LeaseItem | List of SKU items |
| state | LeaseState | PENDING, ACTIVE, CLOSED, REJECTED, or EXPIRED |
| created_at | Timestamp | Creation time (credit locked) |
| acknowledged_at | Timestamp | Provider acknowledgement time (billing starts) |
| closed_at | Timestamp | Closure time |
| rejected_at | Timestamp | Rejection time |
| expired_at | Timestamp | Expiration time |
| last_settled_at | Timestamp | Last settlement time |
| rejection_reason | string | Provider's rejection reason (max 256 chars) |
| closure_reason | string | Closure reason (max 256 chars) |
| meta_hash | bytes | Hash/reference to off-chain deployment data (max 64 bytes, immutable) |
| min_lease_duration_at_creation | uint64 | Snapshot of `min_lease_duration` param at creation (for consistent reservation calculation) |

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

Credit accounts stored at key prefix `0x05`:

| Field | Type | Description |
|-------|------|-------------|
| tenant | string | Tenant address |
| credit_address | string | Derived credit account address |
| active_lease_count | uint64 | Number of ACTIVE leases |
| pending_lease_count | uint64 | Number of PENDING leases |
| reserved_amounts | []Coin | Sum of all credit reservations for active and pending leases |

Note: The actual balance is tracked by the bank module at the `credit_address`. Query the bank module or use `QueryCreditAccount` which includes the balance. The `reserved_amounts` field tracks how much credit is reserved by existing leases (rate × min_lease_duration per denom), preventing overbooking.

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
6. Verify `AvailableCredit >= rate × min_lease_duration` per denom and add the reservation
7. Create lease in PENDING state and increment pending_lease_count

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
5. Release credit reservation (rate × min_lease_duration)

### Cancel Lease (Tenant cancels PENDING)

1. Verify sender is the lease tenant
2. Verify lease is in PENDING state
3. Set lease state to REJECTED
4. Decrement pending_lease_count
5. Release credit reservation (rate × min_lease_duration)

### Expire Lease (EndBlocker)

The EndBlocker automatically expires pending leases that exceed the `pending_timeout`:

1. Query all leases in PENDING state using the state index
2. For each lease where `now > created_at + pending_timeout`:
   - Set lease state to EXPIRED
   - Set expired_at timestamp
   - Decrement pending_lease_count
   - Release credit reservation (rate × min_lease_duration)

**Rate Limiting:** To prevent DoS attacks, the EndBlocker processes a maximum of **100 lease expirations per block** (`MaxPendingLeaseExpirationsPerBlock`). If more than 100 leases need to expire, the remaining leases are processed in subsequent blocks. This uses a two-pass approach to avoid iterator invalidation during state modification.

`pending_timeout` is a hard acknowledgement deadline independently of this cleanup schedule.
Acknowledgement succeeds exactly at the cutoff and fails strictly after it, including before
EndBlock in the same block and while an overdue lease is waiting behind the expiration rate limit.

### Close Lease (ACTIVE → CLOSED)

1. Calculate accrued charges since last settlement
2. Transfer accrued amount from credit to provider's payout address
3. Set lease state to CLOSED
4. Record closed_at timestamp
5. Decrement active_lease_count
6. Release credit reservation (rate × min_lease_duration)

### Withdraw

1. Calculate accrued charges since last settlement
2. Transfer accrued amount from credit to provider's payout address
3. Update last_settled_at timestamp

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
| CreditAccount | Get a tenant's credit account |
| CreditAccounts | List all credit accounts |
| CreditEstimate | Estimate remaining credit duration |
| CreditAddress | Derive credit address for a tenant |
| WithdrawableAmount | Get withdrawable amount for a lease |
| ProviderWithdrawable | Withdrawable amount across the provider's ACTIVE leases, one page at a time — sum `amounts` across pages until `pagination.next_key` is empty. `lease_count` counts only non-zero-withdrawable leases in the current page. |
| LeaseByCustomDomain | Look up the active or pending lease that has claimed a given custom_domain (and the `service_name` of the matching item) |

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

For detailed scalability analysis, time manipulation considerations, and future improvement plans, see [Architecture](docs/ARCHITECTURE.md#scalability-considerations).

## Genesis Validation

The billing module performs comprehensive validation during genesis initialization to ensure state consistency.

### Reservation Invariant Validation

For tenants whose PENDING and ACTIVE leases all store
`MinLeaseDurationAtCreation`, genesis validation enforces the exact credit
reservation invariant:

```
CreditAccount.ReservedAmounts == SUM(GetLeaseReservationAmount(lease, params.MinLeaseDuration))
                                 for all PENDING and ACTIVE leases of the tenant
```

An imported tenant may also have a legacy lease whose
`MinLeaseDurationAtCreation` is zero. Its historical minimum duration cannot be
reconstructed after governance changes, so exact equality is not provable.
This remains true after the legacy lease becomes terminal: older release logic
used the then-current parameter and could leave a residual from the original
reservation. For any tenant with such legacy history, import validation
requires `ReservedAmounts` to be valid and at least cover the complete sum for
all non-legacy PENDING and ACTIVE leases. Current lifecycle handling never
guesses a legacy release from present-day params: it preserves the shared
unknown aggregate while another live legacy lease remains, then reconciles to
the exact non-legacy floor when the last one terminates. Exceeding the fixed
per-tenant scan ceiling instead preserves the aggregate rather than failing the
lifecycle transition or performing an unbounded scan; no later retry is
assumed.
`ValidateStrict()` is
available for newly authored state and instead applies the current minimum
duration to live legacy leases before requiring exact equality.

**Validation Steps:**
1. Compute expected reservations by iterating all leases and summing reservation amounts for PENDING/ACTIVE leases per tenant
2. Require exact equality for fully verifiable tenants; for tenants with legacy leases, require the stored amount to cover the known non-legacy portion
3. Verify that every tenant with active reservations has a corresponding credit account

**Error Examples:**
```
# Mismatch between stored and calculated reservations
invalid credit operation: credit account for manifest1abc... has reserved_amounts 500upwr but lease reservations sum to 600upwr

# Tenant with leases but missing credit account
invalid credit operation: tenant manifest1def... has lease reservations totaling 1000upwr but no credit account
```

### Other Genesis Validations

- **Lease UUIDs**: All leases must have valid UUIDv7 format, no duplicates
- **Tenant/Provider addresses**: All addresses must be valid bech32 format
- **Credit address derivation**: Credit addresses must match deterministic derivation from tenant address
- **Lease counts**: Each credit account's stored active and pending counts must exactly match the imported lease set
- **Lease state consistency**: CLOSED leases must have a non-nil, non-zero `closed_at`. At InitGenesis, `created_at`, `last_settled_at`, and `closed_at` must not be after the block time.
- **Custom domains**: Claims must remain valid and unambiguous. Imports do not replay the current reserved-suffix policy because a claim may predate a later suffix reservation; `ValidateStrict()` applies that policy to newly authored state.
- **Parameter validation**: All params must pass validation constraints

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
