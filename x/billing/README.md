# x/billing

The `billing` module provides a credit-based billing system for leasing SKU resources. It enables tenants to fund credit accounts and create leases for SKU items, with automatic settlement and provider withdrawal capabilities.

## Concepts

### Credit Accounts

Each tenant has a credit account with a derived address. Credit accounts can hold any token denomination that matches the SKU's base_price denomination.

- **Credit Address**: Deterministically derived from the tenant's address
- **Balances**: Current credit balances (supports multiple denominations)
- **Top-up**: Anyone can fund a tenant's credit account with any token

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
4. **REJECTED**: Provider rejected the pending lease
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

### Params

Module parameters stored at key `0x00`:

| Field | Type | Description |
|-------|------|-------------|
| max_leases_per_tenant | uint64 | Maximum active leases per tenant (must be > 0) |
| max_items_per_lease | uint64 | Maximum items per lease (default: 20, hard limit: 100) |
| min_lease_duration | uint64 | Minimum lease duration in seconds (default: 3600 = 1 hour) |
| max_pending_leases_per_tenant | uint64 | Maximum pending leases per tenant (default: 10) |
| pending_timeout | uint64 | Seconds before pending lease expires (default: 1800 = 30 minutes) |
| allowed_list | []string | List of addresses allowed to create leases on behalf of tenants |

**Note:** There is no global `denom` parameter. Each SKU defines its own denomination in its `base_price`, enabling multi-denom billing.

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

Note: The actual balance is tracked by the bank module at the `credit_address`. Query the bank module or use `QueryCreditAccount` which includes the balance.

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

1. EndBlocker checks pending leases older than pending_timeout
2. Set lease state to EXPIRED
3. Set expired_at timestamp
4. Decrement pending_lease_count

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

### MsgFundCredit

Fund a tenant's credit account.

```protobuf
message MsgFundCredit {
  string sender = 1;
  string tenant = 2;
  cosmos.base.v1beta1.Coin amount = 3;
}
```

### MsgCreateLease

Create a new lease in PENDING state. Provider must acknowledge before billing starts.

```protobuf
message MsgCreateLease {
  string tenant = 1;
  repeated LeaseItemInput items = 2;  // items use sku_uuid
}
```

### MsgCreateLeaseForTenant

Create a lease on behalf of a tenant (authority or allowed addresses only). This is used for migrating off-chain leases to on-chain. The tenant's credit account must be pre-funded.

Addresses in the `allowed_list` params can use this message in addition to the module authority.

```protobuf
message MsgCreateLeaseForTenant {
  string authority = 1;
  string tenant = 2;
  repeated LeaseItemInput items = 3;  // items use sku_uuid
}
```

### MsgAcknowledgeLease

Provider acknowledges a PENDING lease, transitioning it to ACTIVE state and starting billing.

```protobuf
message MsgAcknowledgeLease {
  string sender = 1;       // Provider address or authority
  string lease_uuid = 2;
}
```

### MsgRejectLease

Provider rejects a PENDING lease with an optional reason.

```protobuf
message MsgRejectLease {
  string sender = 1;       // Provider address or authority
  string lease_uuid = 2;
  string reason = 3;       // Optional, max 256 characters
}
```

### MsgCancelLease

Tenant cancels their own PENDING lease before provider acknowledgement.

```protobuf
message MsgCancelLease {
  string tenant = 1;
  string lease_uuid = 2;
}
```

### MsgCloseLease

Close an ACTIVE lease. Can be called by tenant, provider, or authority.

```protobuf
message MsgCloseLease {
  string sender = 1;
  string lease_uuid = 2;
}
```

### MsgWithdraw

Withdraw accrued funds from a specific lease.

```protobuf
message MsgWithdraw {
  string sender = 1;
  string lease_uuid = 2;
}
```

### MsgWithdrawAll

Withdraw accrued funds from all leases for a provider. The provider_uuid is required.

**Limits (DoS Protection):**
- **Default limit**: 50 leases per call (when limit=0 or not specified)
- **Maximum limit**: 100 leases per call
- Use the `has_more` response field to determine if additional calls are needed

```protobuf
message MsgWithdrawAll {
  string sender = 1;
  string provider_uuid = 2;
  uint64 limit = 3;       // Max leases to process (0 = default 50, max 100)
}
```

Response includes `has_more` to indicate if additional calls are needed:

```protobuf
message MsgWithdrawAllResponse {
  repeated cosmos.base.v1beta1.Coin total_amounts = 1; // One per denom
  uint64 lease_count = 2;
  string payout_address = 3;
  bool has_more = 4;      // True if more leases remain
}
```

### MsgUpdateParams

Update module parameters (authority only).

```protobuf
message MsgUpdateParams {
  string authority = 1;
  Params params = 2;
}
```

## Queries

| Query | Description |
|-------|-------------|
| Params | Get module parameters |
| Lease | Get a lease by ID |
| Leases | List all leases with pagination |
| LeasesByTenant | List leases for a tenant |
| LeasesByProvider | List leases for a provider |
| CreditAccount | Get a tenant's credit account |
| CreditAddress | Derive credit address for a tenant |
| WithdrawableAmount | Get withdrawable amount for a lease |
| ProviderWithdrawable | Get total withdrawable for a provider |

## Events

| Event | Attributes | Description |
|-------|------------|-------------|
| credit_funded | tenant, credit_address, sender, amount, new_balance | Credit account funded |
| lease_created | lease_uuid, tenant, provider_uuid, item_count, total_rate_per_second, pending_lease_count, created_by | Lease created in PENDING state |
| lease_acknowledged | lease_uuid, tenant, provider_uuid, acknowledged_by | Provider acknowledged lease (→ ACTIVE) |
| lease_rejected | lease_uuid, tenant, provider_uuid, reason, rejected_by | Provider rejected lease |
| lease_cancelled | lease_uuid, tenant | Tenant cancelled pending lease |
| lease_expired | lease_uuid, tenant, provider_uuid | Pending lease expired |
| lease_closed | lease_uuid, tenant, provider_uuid, settled_amounts, closed_by, duration_seconds, active_lease_count | Lease closed manually |
| lease_auto_closed | lease_uuid, tenant, provider_uuid, settled_amounts, reason | Lease auto-closed due to credit exhaustion |
| provider_withdraw | lease_uuid, provider_uuid, amounts, payout_address | Provider withdrawal |
| provider_withdraw_all | provider_uuid, total_amounts, lease_count, payout_address | Provider withdrew from all leases |
| params_updated | | Module parameters updated |

## Client

### CLI

#### Transactions

```bash
# Fund a credit account
manifestd tx billing fund-credit [tenant] [amount] --from [key]

# Create a lease in PENDING state (format: sku_uuid:quantity)
manifestd tx billing create-lease 01912345-6789-7abc-8def-0123456789ab:2 --from [key]

# Create a lease on behalf of a tenant (authority only)
manifestd tx billing create-lease-for-tenant [tenant] 01912345-6789-7abc-8def-0123456789ab:2 --from [authority]

# Acknowledge a pending lease (provider only)
manifestd tx billing acknowledge-lease [lease-uuid] --from [provider-key]

# Reject a pending lease (provider only)
manifestd tx billing reject-lease [lease-uuid] --reason "Resource unavailable" --from [provider-key]

# Cancel a pending lease (tenant only)
manifestd tx billing cancel-lease [lease-uuid] --from [tenant-key]

# Close an active lease
manifestd tx billing close-lease [lease-uuid] --from [key]

# Withdraw from a lease
manifestd tx billing withdraw [lease-uuid] --from [key]

# Withdraw from all leases
manifestd tx billing withdraw-all [provider-uuid] --from [key]
```

#### Queries

```bash
# Query parameters
manifestd query billing params

# Query a lease
manifestd query billing lease [lease-uuid]

# Query all leases
manifestd query billing leases --active-only

# Query leases by tenant
manifestd query billing leases-by-tenant [tenant] --active-only

# Query leases by provider
manifestd query billing leases-by-provider [provider-uuid] --active-only

# Query credit account
manifestd query billing credit-account [tenant]

# Query credit address
manifestd query billing credit-address [tenant]

# Query withdrawable amount
manifestd query billing withdrawable [lease-uuid]

# Query provider total withdrawable
manifestd query billing provider-withdrawable [provider-uuid]
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

| Action | Who Can Perform |
|--------|-----------------|
| Fund Credit | Anyone |
| Create Lease | Tenant (for themselves) |
| Create Lease for Tenant | Authority or Allow-Listed addresses |
| Acknowledge Lease | Provider or Authority |
| Reject Lease | Provider or Authority |
| Cancel Lease | Tenant (own pending leases only) |
| Close Lease | Tenant, Provider, or Authority |
| Withdraw | Provider or Authority |
| Withdraw All | Provider or Authority |
| Update Params | Authority only |

## Integration with SKU Module

The billing module depends on the SKU module for:
- Validating SKU existence and active status
- Getting SKU prices for price locking
- Getting provider information for authorization and payouts

The SKU module remains independent and does not know about the billing module.

## Known Limitations & Future Improvements (v2)

### Scalability Considerations

The following limitations exist in the current implementation and are tracked for future improvement:

#### 1. WithdrawAll Performance

**Issue**: `MsgWithdrawAll` iterates over all leases for a provider, which can become expensive with many leases.

**Current Mitigation**:
- **Hard limits enforced**: Default limit of 50 leases per call, maximum of 100 leases per call to prevent DoS attacks
- **Pagination support**: Use the `limit` parameter to process leases in batches. When `has_more` is true in the response, make additional calls to process remaining leases
- Settlement is O(1) per lease

**Example**:
```bash
# Process default 50 leases at a time
manifestd tx billing withdraw-all [provider-uuid] --from provider-key
# Or specify a custom limit (max 100)
manifestd tx billing withdraw-all [provider-uuid] --limit 75 --from provider-key
# If has_more is true, repeat until all leases are processed
```

**Future Improvement**: Consider adding:
- A secondary index mapping `provider_id -> active lease IDs` for O(1) lookup
- Background batch processing for providers with very large lease counts

#### 2. LeasesByProvider Query

**Issue**: `QueryLeasesByProvider` uses `CollectionFilteredPaginate` which may scan non-matching leases.

**Current Mitigation**: A secondary index (`LeasesByProvider` with key prefix `0x03`) exists for efficient lookup.

**Future Improvement**: Ensure the index is used optimally and consider caching for read-heavy workloads.

#### 3. Active Lease Counting

**Issue**: `CountActiveLeasesByTenant` iterates over all tenant leases to count active ones.

**Current Mitigation**: Added `active_lease_count` field to `CreditAccount` for O(1) lookup, updated on lease create/close.

**Future Improvement**: If count accuracy issues arise, consider periodic reconciliation or event-sourcing.

#### 4. Provider Rate Limiting

**Issue**: Providers with many active leases could create expensive on-chain operations during bulk withdrawals.

**Recommendation for Integrators**:
- Use the `limit` parameter for `MsgWithdrawAll` to batch operations
- Implement off-chain rate limiting for withdrawal operations
- Consider gas limits appropriate for expected lease volumes
- Monitor transaction costs during high-throughput periods

### Arithmetic Precision

**Issue**: Very long-running leases (years) could theoretically overflow during accrual calculation.

**Current Mitigation**: Overflow checking is implemented in `CalculateAccrual` using `math.Int` safe operations with explicit checks before multiplication.

### Time Manipulation Considerations

**Context**: The billing module uses block time (from consensus) for all time-based calculations including:
- Lease creation timestamps
- Settlement calculations
- Duration-based accrual

**Security Model**:
- Block time is determined by validator consensus and cannot be arbitrarily manipulated by a single malicious validator
- Time manipulation attacks would require >2/3 of voting power to collude
- Even with collusion, CometBFT enforces that block times must be monotonically increasing

**Genesis Import Validation**:
- When importing genesis state, `ValidateWithBlockTime` ensures that `LastSettledAt` timestamps are not in the future relative to the new chain's block time
- This prevents issues when restarting a chain with a different genesis time

**Recommendation**:
- Monitor for abnormal block time patterns in production
- The billing module is safe from single-validator time manipulation attacks

### Provider/SKU Deactivation Impact

**Provider Deactivation**:
- Providers can be deactivated via the SKU module at any time
- Active leases continue to work with locked-in prices
- Providers can still withdraw accrued funds from existing leases
- No new leases can be created with SKUs from the deactivated provider

**SKU Deactivation**:
- SKUs can be deactivated via the SKU module at any time
- Active leases continue to work with locked-in prices
- SKU details remain queryable for reporting purposes
- No new leases can be created using the deactivated SKU

### Credit Withdrawal Policy

**Important**: There is no mechanism to withdraw unused credit from a credit account. Once tokens are funded to a credit account, they can only be spent on leases. This design choice:

- Mimics typical web2 cloud providers (AWS credits, Google Cloud credits, etc.)
- Simplifies the economic model
- Prevents gaming of the system

Credit that remains after a lease is closed stays in the credit account and can be used for future leases.

### Future Feature Candidates

1. **Lease Pruning**: Implement automatic pruning of old inactive leases to reduce state size
2. **Credit Account Expiry**: Allow credit accounts to be cleaned up if empty and unused
3. **Multi-Provider Leases**: Allow a single lease to span multiple providers
4. **Delegation**: Allow tenants to delegate lease management to other addresses
5. **Provider Reputation**: Track provider uptime and reliability for tenant decision-making
6. **Provider Shutdown Handling**: Automated lease closure and refund mechanism when providers go offline
7. **Dispute Resolution**: Mechanism for tenants to dispute charges or service quality

## Additional Documentation

### User Guides
- [Provider Setup Guide](../sku/docs/PROVIDER_GUIDE.md) - Creating and managing providers
- [SKU Setup Guide](../sku/docs/SKU_GUIDE.md) - Creating and managing SKUs (billable items)
- [Migration Guide](docs/MIGRATION.md) - Guide for authority members migrating off-chain leases
- [Troubleshooting](docs/TROUBLESHOOTING.md) - Common errors and solutions
- [API Reference](docs/API.md) - Complete CLI and gRPC/REST API reference

### Developer Documentation
- [Architecture](docs/ARCHITECTURE.md) - Internal architecture, data models, and flow diagrams
- [Design Decisions](docs/DESIGN_DECISIONS.md) - Key design decisions and rationale
