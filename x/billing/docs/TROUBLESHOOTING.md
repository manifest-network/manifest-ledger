# Billing Module Troubleshooting Guide

This guide covers common errors and issues users may encounter when using the billing module.

## Credit Account Issues

### "credit account not found"

**Cause**: The tenant has never had their credit account funded.

**Solution**: Fund the credit account first:
```bash
manifestd tx billing fund-credit [tenant-address] [amount] --from [key]
```

### "insufficient credit balance"

**Cause**: The credit account doesn't have enough balance to meet the minimum requirement or cover the lease.

**Solution**: 
1. Check current balance:
   ```bash
   manifestd query billing credit-account [tenant-address]
   ```
2. Fund additional credit:
   ```bash
   manifestd tx billing fund-credit [tenant-address] [amount] --from [key]
   ```

The default minimum credit balance is 5 PWR (5000000 upwr).

### "expected [denom], got [wrong-denom]"

**Cause**: Attempting to fund a credit account with the wrong token denomination.

**Solution**: Use the correct billing denomination (PWR token):
```bash
# Check the correct denom
manifestd query billing params

# Use the correct denom
manifestd tx billing fund-credit [tenant] 1000000000factory/manifest1afk9zr2hn2jsac63h4hm60vl9z3e5u69gndzf7c99cqge3vzwjzsfmy9qj/upwr --from [key]
```

### "only billing denomination can be sent to credit accounts"

**Cause**: Attempting to send non-PWR tokens to a credit account address via bank send.

**Solution**: Only the configured billing denomination (PWR) can be sent to credit accounts. This is a protective measure to prevent fund loss.

## Lease Creation Issues

### "SKU not found"

**Cause**: The specified SKU ID doesn't exist.

**Solution**: 
1. Query available SKUs:
   ```bash
   manifestd query sku skus
   ```
2. Use a valid SKU ID in your lease creation.

### "SKU is not active"

**Cause**: The SKU exists but has been deactivated.

**Solution**: 
1. Query the SKU to confirm:
   ```bash
   manifestd query sku sku [sku-id]
   ```
2. Contact the authority to reactivate the SKU, or use a different active SKU.

### "provider is not active"

**Cause**: The provider associated with the SKU has been deactivated.

**Solution**: Contact the authority to reactivate the provider, or use SKUs from an active provider.

### "all SKUs must belong to the same provider"

**Cause**: Attempting to create a lease with SKUs from different providers.

**Solution**: Create separate leases for SKUs from different providers:
```bash
# Instead of this (fails):
manifestd tx billing create-lease 1:1 5:1  # SKUs from different providers

# Do this:
manifestd tx billing create-lease 1:1 --from tenant  # Provider 1 SKUs
manifestd tx billing create-lease 5:1 --from tenant  # Provider 2 SKUs
```

### "tenant has reached maximum lease limit"

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

### "lease exceeds maximum items"

**Cause**: Attempting to create a lease with too many SKU items.

**Solution**: Split the lease into multiple smaller leases. The default limit is 20 items per lease.

### "quantity must be greater than zero"

**Cause**: Creating a lease item with quantity 0.

**Solution**: Ensure all items have quantity ≥ 1:
```bash
manifestd tx billing create-lease 1:1 2:2 --from tenant
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

### "lease is already inactive"

**Cause**: Attempting to close a lease that's already closed.

**Solution**: No action needed - the lease is already closed. Query it to see details:
```bash
manifestd query billing lease [lease-id]
```

### "unauthorized: sender is not tenant, provider, or authority"

**Cause**: Attempting to close a lease you don't have permission to close.

**Solution**: Only the following can close a lease:
- The tenant who created the lease
- The provider of the SKUs in the lease
- The module authority

## Withdrawal Issues

### "no withdrawable amount"

**Cause**: 
1. The lease has no accrued charges yet (just created), OR
2. The provider already withdrew recently

**Solution**: 
1. Check the withdrawable amount:
   ```bash
   manifestd query billing withdrawable [lease-id]
   ```
2. Wait for more time to pass for charges to accrue.

### "lease is not active"

**Cause**: Attempting to withdraw from an inactive (closed) lease.

**Solution**: You can only withdraw from active leases. Check the lease state:
```bash
manifestd query billing lease [lease-id]
```

### "unauthorized to withdraw"

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

### Lease closed automatically

**Cause**: The tenant's credit balance reached zero, triggering automatic lease closure.

**What happened**:
1. The system detected zero credit during a lease operation
2. Final settlement was performed (remaining balance transferred to provider)
3. The lease was closed automatically

**How to verify**:
```bash
# Check lease state
manifestd query billing lease [lease-id]

# Look for "lease_auto_closed" event with reason "credit_exhausted"
```

**Resolution**: 
1. Fund the credit account again
2. Create a new lease

### Why wasn't my lease auto-closed?

**Cause**: Auto-close uses lazy evaluation - it only triggers when the lease is "touched".

**Auto-close triggers on**:
- `QueryLease` (individual lease query)
- `MsgWithdraw`
- `QueryWithdrawableAmount`
- `MsgCloseLease`

**Auto-close does NOT trigger on**:
- Bulk queries (`QueryLeases`, `QueryLeasesByTenant`, `QueryLeasesByProvider`)

**Solution**: Query the individual lease to trigger the check:
```bash
manifestd query billing lease [lease-id]
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

1. **Check the logs**: Look at the full error message for details
2. **Query state**: Use query commands to understand current state
3. **Check events**: Query the transaction to see emitted events
4. **Review parameters**: 
   ```bash
   manifestd query billing params
   manifestd query sku params
   ```

## Related Documentation

- [Provider Setup Guide](../sku/PROVIDER_GUIDE.md) - Creating and managing providers
- [SKU Setup Guide](../sku/SKU_GUIDE.md) - Creating and managing SKUs
- [Billing README](README.md) - Complete billing module overview
- [Migration Guide](MIGRATION.md) - Migrating existing off-chain leases
- [API Reference](API.md) - Detailed API documentation
