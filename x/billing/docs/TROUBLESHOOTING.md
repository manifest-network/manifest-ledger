# Billing Module Troubleshooting Guide

This guide covers common errors and issues users may encounter when using the billing module.

## Credit Account Issues

### "credit account not found"

**Cause**: The tenant has never had their credit account funded.

**Solution**: Fund the credit account first:
```bash
manifestd tx billing fund-credit [tenant-address] [amount] --from [key]
```

The credit account is created automatically when first funded.

### "insufficient credit balance"

**Cause**: The credit account doesn't have enough balance in one or more denominations to cover the `min_lease_duration` requirement for the lease.

**Solution**: 
1. Check current balances:
   ```bash
   manifestd query billing credit-account [tenant-address]
   ```
2. Calculate minimum required for each denom used by SKUs in the lease:
   ```
   min_required[denom] = sum(sku_rate_per_second × quantity for that denom) × min_lease_duration
   ```
3. Fund additional credit for each denom:
   ```bash
   manifestd tx billing fund-credit [tenant-address] [amount_denom1] --from [key]
   manifestd tx billing fund-credit [tenant-address] [amount_denom2] --from [key]
   ```

**Example**: For a lease with SKU1 (10 upwr/second) and SKU2 (5 umfx/second) with `min_lease_duration` of 3600 seconds:
- Need at least 36,000 upwr
- Need at least 18,000 umfx

### "invalid denomination for credit account"

**Cause**: This error has been removed. Credit accounts now support any denomination.

**Note**: Credit accounts can hold multiple denominations. Simply fund the account with the tokens matching your target SKUs' `base_price` denoms.

## Lease Creation Issues

### "sku not found"

**Cause**: The specified SKU ID doesn't exist.

**Solution**: 
1. Query available SKUs:
   ```bash
   manifestd query sku skus
   ```
2. Use a valid SKU ID in your lease creation.

### "sku not active"

**Cause**: The SKU exists but has been deactivated.

**Solution**: 
1. Query the SKU to confirm:
   ```bash
   manifestd query sku sku [sku-id]
   ```
2. Contact the authority to reactivate the SKU, or use a different active SKU.

### "provider not found" or "provider not active"

**Cause**: The provider associated with the SKU doesn't exist or has been deactivated.

**Solution**: Contact the authority to create/reactivate the provider, or use SKUs from an active provider.

### "all SKUs in a lease must belong to the same provider"

**Cause**: Attempting to create a lease with SKUs from different providers.

**Solution**: Create separate leases for SKUs from different providers:
```bash
# Instead of this (fails):
manifestd tx billing create-lease 1:1 5:1 --from tenant  # SKUs from different providers

# Do this:
manifestd tx billing create-lease 1:1 --from tenant  # Provider 1 SKUs
manifestd tx billing create-lease 5:1 --from tenant  # Provider 2 SKUs
```

### "maximum leases per tenant reached"

**Cause**: The tenant has too many active leases.

**Solution**: 
1. Check current leases:
   ```bash
   manifestd query billing leases-by-tenant [tenant-address] --active-only
   ```
2. Close unused leases to free up slots:
   ```bash
   manifestd tx billing close-lease [lease-id] --from tenant
   ```
3. The default limit is 100 active leases per tenant.

### "too many items in lease"

**Cause**: Attempting to create a lease with too many SKU items.

**Solution**: Split the lease into multiple smaller leases. The default limit is 20 items per lease (hard limit is 100).

### "quantity must be greater than zero"

**Cause**: Creating a lease item with quantity 0.

**Solution**: Ensure all items have quantity ≥ 1:
```bash
manifestd tx billing create-lease 1:1 2:2 --from tenant
```

### "duplicate sku in lease items"

**Cause**: The same SKU ID appears multiple times in the lease items.

**Solution**: Combine quantities into a single item:
```bash
# Instead of this (fails):
manifestd tx billing create-lease 1:2 1:3 --from tenant

# Do this:
manifestd tx billing create-lease 1:5 --from tenant
```

### "lease must contain at least one item"

**Cause**: Trying to create a lease with no items.

**Solution**: Specify at least one SKU item:
```bash
manifestd tx billing create-lease 1:1 --from tenant
```

## Lease Closure Issues

### "lease not found"

**Cause**: The specified lease ID doesn't exist.

**Solution**: 
1. Query your leases:
   ```bash
   manifestd query billing leases-by-tenant [your-address]
   ```
2. Use a valid lease ID.

### "lease not active"

**Cause**: Attempting to close a lease that's already closed.

**Solution**: No action needed - the lease is already closed. Query it to see details:
```bash
manifestd query billing lease [lease-id]
```

### "unauthorized"

**Cause**: Attempting to close a lease you don't have permission to close.

**Solution**: Only the following can close a lease:
- The tenant who owns the lease
- The provider of the SKUs in the lease
- The module authority

## Withdrawal Issues

### "no withdrawable amount"

**Cause**: 
1. The lease has no accrued charges yet (just created or just settled), OR
2. The lease is not active (closed), OR
3. The provider already withdrew recently

**Solution**: 
1. Check the withdrawable amount:
   ```bash
   manifestd query billing withdrawable [lease-id]
   ```
2. For active leases, wait for more time to pass for charges to accrue.
3. For closed leases, you cannot withdraw (settlement happened during close).

### "lease not active"

**Cause**: Attempting to withdraw from a closed lease.

**Solution**: You can only withdraw from active leases. Settlement happens during closure, so there's nothing left to withdraw:
```bash
manifestd query billing lease [lease-id]
```

### "unauthorized"

**Cause**: Only the provider (or authority) can withdraw from a lease.

**Solution**: Use the provider's key to withdraw:
```bash
manifestd tx billing withdraw [lease-id] --from [provider-key]
```

### "provider not found"

**Cause**: The provider_id doesn't exist in the SKU module.

**Solution**: 
1. Query available providers:
   ```bash
   manifestd query sku providers
   ```
2. Use a valid provider ID.

## WithdrawAll Issues

### "provider_id is required for withdraw all"

**Cause**: Missing provider_id in the WithdrawAll command.

**Solution**: Specify the provider ID:
```bash
manifestd tx billing withdraw-all [provider-id] --from [key]
```

### Response shows "has_more: true"

**Cause**: There are more leases to process than the limit allows.

**Solution**: Continue calling withdraw-all until has_more is false:
```bash
# Process 50 leases at a time (default)
manifestd tx billing withdraw-all [provider-id] --from [key]

# Or specify a larger limit (max 100)
manifestd tx billing withdraw-all [provider-id] --limit 100 --from [key]
```

## Query Issues

### "lease_id cannot be zero"

**Cause**: Passing 0 as a lease ID.

**Solution**: Lease IDs start at 1. Query your leases to find valid IDs:
```bash
manifestd query billing leases
```

### "invalid tenant address"

**Cause**: The address format is invalid.

**Solution**: Use a valid bech32 address with the correct prefix:
```bash
manifestd query billing credit-account manifest1abc...
```

## Auto-Close Behavior

### Understanding Auto-Close

Auto-close is triggered when a lease's credit is exhausted (balance = 0). It happens during write operations only:
- `Withdraw` - If credit exhausted after settlement
- `CloseLease` - If credit was already exhausted

**Important:** Auto-close does NOT happen:
- During query operations (queries are read-only)
- At block boundaries (no EndBlocker processing)
- Automatically in the background

### Lease closed automatically

**What happened**:
1. During a Withdraw or CloseLease operation, the system detected zero credit
2. Final settlement was performed (any remaining balance transferred to provider)
3. The lease was closed with reason "credit_exhausted"

**How to verify**:
```bash
# Check lease state
manifestd query billing lease [lease-id]

# Look for events in the transaction
manifestd query tx [txhash] --output json | jq '.events'
```

Events to look for:
- `lease_auto_close` with `reason: credit_exhausted`

**Resolution**: 
1. Fund the credit account again
2. Create a new lease

### Why doesn't my query show accurate accrued amounts?

**Cause**: Lease queries (`Lease`, `Leases`, etc.) return stored state without performing settlement calculations. The `last_settled_at` field shows when settlement last occurred.

**Explanation**: 
- Lease queries return the stored `last_settled_at` timestamp
- They don't calculate time elapsed since then
- Use `WithdrawableAmount` or `ProviderWithdrawable` queries to get **real-time calculated amounts**:
  ```bash
  # Get real-time withdrawable for a specific lease
  manifestd query billing withdrawable [lease-id]
  
  # Get real-time total withdrawable for a provider
  manifestd query billing provider-withdrawable [provider-id]
  ```

## Parameter Issues

### "invalid params"

**Cause**: Invalid parameter values in UpdateParams.

**Common issues**:
- `min_lease_duration` must be > 0
- `max_leases_per_tenant` must be > 0
- `max_items_per_lease` must be > 0 and ≤ 100

**Solution**: Check current params and ensure new values are valid:
```bash
manifestd query billing params
```

### "unauthorized" on UpdateParams

**Cause**: Only the module authority can update parameters.

**Solution**: Use the authority account (POA admin group):
```bash
manifestd tx billing update-params ... --from authority
```

## Gas and Transaction Issues

### "out of gas"

**Cause**: The transaction requires more gas than allocated.

**Solution**: Increase gas limit:
```bash
manifestd tx billing create-lease 1:10 --from tenant --gas 500000
# Or use auto
manifestd tx billing create-lease 1:10 --from tenant --gas auto --gas-adjustment 1.5
```

### Transaction pending/not included

**Cause**: Gas price too low or network congestion.

**Solution**: 
1. Wait for inclusion, or
2. Resubmit with higher gas price

## Getting Help

If you encounter an issue not covered here:

1. **Check the full error message**: The error often contains specific details
2. **Query relevant state**:
   ```bash
   manifestd query billing params
   manifestd query billing lease [lease-id]
   manifestd query billing credit-account [tenant]
   manifestd query sku sku [sku-id]
   manifestd query sku provider [provider-id]
   ```
3. **Check events**: Query the transaction to see emitted events
   ```bash
   manifestd query tx [txhash] --output json | jq '.events'
   ```

## Related Documentation

- [Billing README](../README.md) - Complete billing module overview
- [Migration Guide](MIGRATION.md) - Migrating existing off-chain leases
- [API Reference](API.md) - Detailed API documentation
- [Architecture](ARCHITECTURE.md) - Technical architecture details
