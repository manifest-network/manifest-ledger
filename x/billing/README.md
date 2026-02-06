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

Each lease stores `MinLeaseDurationAtCreation` to ensure consistent reservation calculation regardless of subsequent governance changes to the `MinLeaseDuration` parameter. This prevents existing reservations from becoming inconsistent when parameters change.

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
   - Emits a `lease_auto_closed` event with `reason: credit_exhausted`

**Design rationale:**
- **O(1) per lease check**: Instead of O(n) scanning all leases in EndBlock
- **Scalability**: Supports millions of leases without performance degradation
- **On-demand**: Only processes leases when they're actually used
- **No consensus overhead**: EndBlock remains lightweight
- **Transaction safety**: Auto-close only happens in transactions where state changes are committed

**Note**: Queries (`QueryLease`, `QueryLeases`, etc.) do NOT trigger auto-close. They return the stored state. Auto-close only happens during write operations (Withdraw, CloseLease) to ensure state changes are properly committed.

**Note**: During lazy settlement (withdrawal or manual close), if the credit balance is less than the accrued amount, only the available balance is transferred to the provider.

## State

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

### Params

Module parameters stored at key `0x00`:

| Field | Type | Description |
|-------|------|-------------|
| max_leases_per_tenant | uint64 | Maximum active leases per tenant (must be > 0) |
| max_items_per_lease | uint64 | Maximum items per lease (default: 20, hard limit: 100) |
| min_lease_duration | uint64 | Minimum lease duration in seconds (default: 3600 = 1 hour) |
| max_pending_leases_per_tenant | uint64 | Maximum pending leases per tenant (default: 10) |
| pending_timeout | uint64 | Seconds before pending lease expires (default: 1800 = 30 minutes, min: 60, max: 86400) |
| allowed_list | []string | List of addresses allowed to create leases on behalf of tenants |

**Validation Constraints:**
- `max_leases_per_tenant`: Must be > 0
- `max_items_per_lease`: Must be > 0 and ≤ 100 (hard limit)
- `min_lease_duration`: Must be > 0
- `max_pending_leases_per_tenant`: Must be > 0
- `pending_timeout`: Must be between 60 seconds (1 minute) and 86400 seconds (24 hours)

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
| `MaxDurationSeconds` | 3,153,600,000 (100 years) | Maximum lease duration for accrual calculations (overflow protection). Defined in `keeper/accrual.go`. |
| `CreditAccountAddressPrefix` | `billing/credit/` | Prefix used for deterministic credit address derivation |
| `DefaultProviderWithdrawableQueryLimit` | 100 | Default limit for ProviderWithdrawable query |
| `MaxProviderWithdrawableQueryLimit` | 1000 | Maximum limit for ProviderWithdrawable query |
| `MaxCreditEstimateLeases` | 100 | Maximum active leases processed in CreditEstimate query |

### Batch Operations

Several messages support batch processing of multiple leases in a single transaction:

| Message | Max Leases | Behavior |
|---------|------------|----------|
| `MsgAcknowledgeLease` | 100 | All leases must be PENDING, same provider. Atomic. |
| `MsgRejectLease` | 100 | All leases must be PENDING, same provider. Atomic. |
| `MsgCancelLease` | 100 | All leases must be PENDING, same tenant. Atomic. |
| `MsgCloseLease` | 100 | All leases must be ACTIVE, authorized for sender. Atomic. |
| `MsgWithdraw` (specific) | 100 | All leases must be ACTIVE, same provider. Atomic. |
| `MsgWithdraw` (provider-wide) | 50 (default), 100 (max) | Paginated, use `has_more` to continue. |

**Atomic Batch Operations:** When providing specific lease UUIDs, the operation is atomic—all leases succeed or all fail. If any lease fails validation (wrong state, unauthorized, etc.), the entire transaction is rejected.

**Provider-Wide Withdraw:** Unlike specific-lease operations, provider-wide withdraw is paginated. It processes up to `--limit` leases (default 50, max 100) and returns `has_more: true` if more remain. Call repeatedly until `has_more: false`.

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

1. Verify tenant has a credit account
2. Verify tenant has sufficient credit to cover minimum lease duration (credit >= rate * min_lease_duration)
3. Verify all SKUs exist, are active, and belong to same provider
4. Verify tenant hasn't exceeded max active or pending leases
5. Lock current SKU prices
6. Create lease in PENDING state
7. Increment pending_lease_count

### Acknowledge Lease (PENDING → ACTIVE)

1. Provider verifies they own the SKUs in the lease
2. Set lease state to ACTIVE
3. Set acknowledged_at to current block time (billing starts)
4. Decrement pending_lease_count, increment active_lease_count

### Reject Lease (PENDING → REJECTED)

1. Provider verifies they own the SKUs in the lease
2. Set lease state to REJECTED
3. Set rejected_at and rejection_reason
4. Decrement pending_lease_count

### Cancel Lease (Tenant cancels PENDING)

1. Verify sender is the lease tenant
2. Verify lease is in PENDING state
3. Set lease state to REJECTED
4. Decrement pending_lease_count

### Expire Lease (EndBlocker)

The EndBlocker automatically expires pending leases that exceed the `pending_timeout`:

1. Query all leases in PENDING state using the state index
2. For each lease where `now > created_at + pending_timeout`:
   - Set lease state to EXPIRED
   - Set expired_at timestamp
   - Decrement pending_lease_count

**Rate Limiting:** To prevent DoS attacks, the EndBlocker processes a maximum of **100 lease expirations per block** (`MaxPendingLeaseExpirationsPerBlock`). If more than 100 leases need to expire, the remaining leases are processed in subsequent blocks. This uses a two-pass approach to avoid iterator invalidation during state modification.

### Close Lease (ACTIVE → CLOSED)

1. Calculate accrued charges since last settlement
2. Transfer accrued amount from credit to provider's payout address
3. Set lease state to CLOSED
4. Record closed_at timestamp
5. Decrement active_lease_count

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
| ProviderWithdrawable | Get total withdrawable for a provider |

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

Provider-wide withdraw mode (`--provider` flag) processes up to 50 leases per call by default (max 100). Use the `--limit` parameter to increase and check `has_more` in the response to process all leases:
```bash
manifestd tx billing withdraw --provider [provider-uuid] --limit 100 --from provider-key
```

For detailed scalability analysis, time manipulation considerations, and future improvement plans, see [Architecture](docs/ARCHITECTURE.md#scalability-considerations).

## Genesis Validation

The billing module performs comprehensive validation during genesis initialization to ensure state consistency.

### Reservation Invariant Validation

Genesis validation enforces the credit reservation invariant:

```
CreditAccount.ReservedAmounts == SUM(GetLeaseReservationAmount(lease, params.MinLeaseDuration))
                                 for all PENDING and ACTIVE leases of the tenant
```

**Validation Steps:**
1. Compute expected reservations by iterating all leases and summing reservation amounts for PENDING/ACTIVE leases per tenant
2. Compare each credit account's `reserved_amounts` against the computed expected value
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
- **Lease state consistency**: Timestamps must be consistent with lease state (e.g., `acknowledged_at` only for ACTIVE leases)
- **Parameter validation**: All params must pass validation constraints

## Additional Documentation

### User Guides
- [Provider Setup Guide](../sku/docs/PROVIDER_GUIDE.md) - Creating and managing providers
- [SKU Setup Guide](../sku/docs/SKU_GUIDE.md) - Creating and managing SKUs (billable items)
- [Migration Guide](docs/MIGRATION.md) - Guide for authority members migrating off-chain leases
- [Integration Guide](docs/INTEGRATION.md) - Tenant authentication to provider APIs (ADR-036)
- [Troubleshooting](docs/TROUBLESHOOTING.md) - Common errors and solutions
- [API Reference](docs/API.md) - Complete CLI and gRPC/REST API reference

### Developer Documentation
- [Architecture](docs/ARCHITECTURE.md) - Internal architecture, data models, and flow diagrams
- [Design Decisions](docs/DESIGN_DECISIONS.md) - Key design decisions and rationale
- [Comparison](docs/COMPARISON.md) - Comparison with Akash and architectural trade-offs
- [Capabilities](docs/CAPABILITIES.md) - Feature overview and future roadmap

### Related Modules
- [SKU Module README](../sku/README.md) - Provider and SKU management (prerequisite for billing)
- [SKU API Reference](../sku/docs/API.md) - SKU module CLI and API documentation
- [SKU Design Decisions](../sku/docs/DESIGN_DECISIONS.md) - SKU architecture rationale
