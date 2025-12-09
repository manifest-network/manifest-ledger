# x/billing

The `billing` module provides a credit-based billing system for leasing SKU resources. It enables tenants to fund credit accounts and create leases for SKU items, with automatic settlement and provider withdrawal capabilities.

## Concepts

### Credit Accounts

Each tenant has a credit account with a derived address. The credit account holds the billing denomination (PWR tokens) that will be used to pay for leased resources.

- **Credit Address**: Deterministically derived from the tenant's address
- **Balance**: Current credit balance in the billing denomination
- **Top-up**: Anyone can fund a tenant's credit account

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

### Overdraw

If a tenant's credit balance is insufficient during settlement, the lease is automatically closed. The remaining balance stays in the credit account.

## State

### Params

Module parameters stored at key `0x00`:

| Field | Type | Description |
|-------|------|-------------|
| denom | string | Billing denomination (PWR token) |
| min_credit_balance | Int | Minimum credit required to create a lease |
| max_leases_per_tenant | uint64 | Maximum active leases per tenant (must be > 0) |

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

```protobuf
message MsgWithdrawAll {
  string sender = 1;
  uint64 provider_id = 2;
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
| credit_funded | tenant, credit_address, amount | Credit account funded |
| lease_created | lease_id, tenant, provider_id | Lease created |
| lease_closed | lease_id | Lease closed |
| lease_settled | lease_id, amount | Lease settlement occurred |
| provider_withdraw | lease_id, amount, payout_address | Provider withdrawal |
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

## Authorization

| Action | Who Can Perform |
|--------|-----------------|
| Fund Credit | Anyone |
| Create Lease | Tenant (for themselves) |
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
