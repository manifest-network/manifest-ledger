# x/billing

The `billing` module provides a credit-based billing system for leasing SKU resources. It enables tenants to fund credit accounts and create leases for SKU items, with automatic settlement and provider withdrawal capabilities.

## Concepts

### Credit Accounts

Each tenant has a credit account with a derived address. The credit account holds the billing denomination (PWR tokens) that will be used to pay for leased resources.

- **Credit Address**: Deterministically derived from the tenant's address
- **Balance**: Current credit balance in the billing denomination
- **Top-up**: Anyone can fund a tenant's credit account

#### Send Restriction

A send restriction is enforced to prevent users from accidentally sending wrong tokens to credit accounts. Only the configured billing denomination (PWR tokens) can be sent to credit account addresses. Attempting to send any other denomination will result in an error.

This prevents loss of funds that would otherwise be unrecoverable since credit accounts are module-controlled addresses.

### Leases

A lease represents an agreement between a tenant and a provider for one or more SKU items.

- **Tenant**: The address that created and pays for the lease
- **Provider ID**: All SKUs in a lease must belong to the same provider
- **Items**: List of SKU items with locked-in prices and quantities
- **State**: ACTIVE or INACTIVE
- **Settlement**: Accrued charges are calculated based on time since last settlement

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

If a tenant's credit balance reaches zero, the billing module automatically closes their active leases. This happens through **lazy evaluation** ("check on touch"):

**When auto-close is triggered:**
- When a lease is queried individually (`QueryLease`)
- When withdrawing from a lease (`MsgWithdraw`)
- When checking withdrawable amounts (`QueryWithdrawableAmount`)
- When attempting to close a lease (`MsgCloseLease`)

**How it works:**
1. When a lease is "touched" (accessed directly), the system checks the tenant's credit balance
2. If balance is zero or negative:
   - Performs final settlement (transfers any remaining balance to provider)
   - Closes the lease automatically
   - Emits a `lease_auto_closed` event with `reason: credit_exhausted`

**Design rationale:**
- **O(1) per lease check**: Instead of O(n) scanning all leases in EndBlock
- **Scalability**: Supports millions of leases without performance degradation
- **On-demand**: Only processes leases when they're actually used
- **No consensus overhead**: EndBlock remains lightweight

**Note**: Bulk queries (`QueryLeases`, `QueryLeasesByTenant`, `QueryLeasesByProvider`) do NOT trigger auto-close checks to maintain query performance. The auto-close will happen when individual leases are accessed.

**Note**: During lazy settlement (withdrawal or manual close), if the credit balance is less than the accrued amount, only the available balance is transferred to the provider.

## State

### Params

Module parameters stored at key `0x00`:

| Field | Type | Description |
|-------|------|-------------|
| denom | string | Billing denomination (PWR token) |
| min_credit_balance | Int | Minimum credit required to create a lease |
| max_leases_per_tenant | uint64 | Maximum active leases per tenant (must be > 0) |
| max_items_per_lease | uint64 | Maximum items per lease (default: 20, hard limit: 100) |
| allowed_list | []string | List of addresses allowed to create leases on behalf of tenants |

### Lease

Leases stored at key prefix `0x01`:

| Field | Type | Description |
|-------|------|-------------|
| id | uint64 | Unique lease identifier |
| tenant | string | Tenant address |
| provider_id | uint64 | Provider ID (from SKU module) |
| items | []LeaseItem | List of SKU items |
| state | LeaseState | ACTIVE or INACTIVE |
| created_at | Timestamp | Creation time |
| closed_at | Timestamp | Closure time (if inactive) |
| last_settled_at | Timestamp | Last settlement time |

### LeaseItem

| Field | Type | Description |
|-------|------|-------------|
| sku_id | uint64 | SKU ID being leased |
| quantity | uint64 | Number of instances |
| locked_price | Int | Price locked at creation (per second) |

### CreditAccount

Credit accounts stored at key prefix `0x05`:

| Field | Type | Description |
|-------|------|-------------|
| tenant | string | Tenant address |
| credit_address | string | Derived credit account address |

Note: The actual balance is tracked by the bank module at the `credit_address`. Query the bank module or use `QueryCreditAccount` which includes the balance.

## State Transitions

### Fund Credit

Transfers tokens from sender to tenant's credit account.

```
sender → credit_address
```

### Create Lease

1. Verify tenant has sufficient credit balance
2. Verify all SKUs exist, are active, and belong to same provider
3. Verify tenant hasn't exceeded max leases
4. Lock current SKU prices
5. Create lease in ACTIVE state

### Close Lease

1. Calculate accrued charges since last settlement
2. Transfer accrued amount from credit to provider
3. Set lease state to INACTIVE
4. Record closed_at timestamp

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

Create a new lease.

```protobuf
message MsgCreateLease {
  string tenant = 1;
  repeated LeaseItemInput items = 2;
}
```

### MsgCreateLeaseForTenant

Create a lease on behalf of a tenant (authority or allowed addresses only). This is used for migrating off-chain leases to on-chain. The tenant's credit account must be pre-funded.

Addresses in the `allowed_list` params can use this message in addition to the module authority.

```protobuf
message MsgCreateLeaseForTenant {
  string authority = 1;
  string tenant = 2;
  repeated LeaseItemInput items = 3;
}
```

### MsgCloseLease

Close an active lease. Can be called by tenant, provider, or authority.

```protobuf
message MsgCloseLease {
  string sender = 1;
  uint64 lease_id = 2;
}
```

### MsgWithdraw

Withdraw accrued funds from a specific lease.

```protobuf
message MsgWithdraw {
  string sender = 1;
  uint64 lease_id = 2;
}
```

### MsgWithdrawAll

Withdraw accrued funds from all leases for a provider. The provider_id is required.

**Limits (DoS Protection):**
- **Default limit**: 50 leases per call (when limit=0 or not specified)
- **Maximum limit**: 100 leases per call
- Use the `has_more` response field to determine if additional calls are needed

```protobuf
message MsgWithdrawAll {
  string sender = 1;
  uint64 provider_id = 2;
  uint64 limit = 3;       // Max leases to process (0 = default 50, max 100)
}
```

Response includes `has_more` to indicate if additional calls are needed:

```protobuf
message MsgWithdrawAllResponse {
  cosmos.base.v1beta1.Coin total_amount = 1;
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
| lease_created | lease_id, tenant, provider_id, item_count, total_rate_per_second, active_lease_count, created_by | Lease created (created_by is "tenant" or "authority") |
| lease_closed | lease_id, tenant, provider_id, settled_amount, closed_by, duration_seconds, active_lease_count | Lease closed manually |
| lease_auto_closed | lease_id, tenant, provider_id, settled_amount, reason | Lease auto-closed due to credit exhaustion |
| provider_withdraw | lease_id, provider_id, amount, payout_address | Provider withdrawal |
| provider_withdraw_all | provider_id, amount, lease_count, payout_address | Provider withdrew from all leases |
| params_updated | | Module parameters updated |

## Client

### CLI

#### Transactions

```bash
# Fund a credit account
manifestd tx billing fund-credit [tenant] [amount] --from [key]

# Create a lease (format: sku_id:quantity)
manifestd tx billing create-lease 1:2 2:1 --from [key]

# Create a lease on behalf of a tenant (authority only)
manifestd tx billing create-lease-for-tenant [tenant] 1:2 2:1 --from [authority]

# Close a lease
manifestd tx billing close-lease [lease-id] --from [key]

# Withdraw from a lease
manifestd tx billing withdraw [lease-id] --from [key]

# Withdraw from all leases
manifestd tx billing withdraw-all [provider-id] --from [key]
```

#### Queries

```bash
# Query parameters
manifestd query billing params

# Query a lease
manifestd query billing lease [lease-id]

# Query all leases
manifestd query billing leases --active-only

# Query leases by tenant
manifestd query billing leases-by-tenant [tenant] --active-only

# Query leases by provider
manifestd query billing leases-by-provider [provider-id] --active-only

# Query credit account
manifestd query billing credit-account [tenant]

# Query credit address
manifestd query billing credit-address [tenant]

# Query withdrawable amount
manifestd query billing withdrawable [lease-id]

# Query provider total withdrawable
manifestd query billing provider-withdrawable [provider-id]
```

## Default Parameters

| Parameter | Default Value |
|-----------|---------------|
| denom | factory/manifest1afk9zr2hn2jsac63h4hm60vl9z3e5u69gndzf7c99cqge3vzwjzsfmy9qj/upwr |
| min_credit_balance | 5000000 (5 PWR) |
| max_leases_per_tenant | 100 |
| max_items_per_lease | 20 (hard limit: 100) |

## Authorization

| Action | Who Can Perform |
|--------|-----------------|
| Fund Credit | Anyone |
| Create Lease | Tenant (for themselves) |
| Create Lease for Tenant | Authority only (for migrating off-chain leases) |
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
manifestd tx billing withdraw-all 1 --from provider-key
# Or specify a custom limit (max 100)
manifestd tx billing withdraw-all 1 --limit 75 --from provider-key
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
