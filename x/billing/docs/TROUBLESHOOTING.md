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
manifestd tx billing create-lease <sku-uuid-provider1>:1 <sku-uuid-provider2>:1 --from tenant  # SKUs from different providers

# Do this:
manifestd tx billing create-lease <sku-uuid-provider1>:1 --from tenant  # Provider 1 SKUs only
manifestd tx billing create-lease <sku-uuid-provider2>:1 --from tenant  # Provider 2 SKUs only
```

### "maximum leases per tenant reached"

**Cause**: The tenant has too many active leases.

**Solution**: 
1. Check current leases:
   ```bash
   manifestd query billing leases-by-tenant [tenant-address] --state active
   ```
2. Close unused leases to free up slots:
   ```bash
   manifestd tx billing close-lease [lease-uuid] --from tenant
   ```
3. The default limit is 100 active leases per tenant.

### "maximum pending leases per tenant reached"

**Cause**: The tenant has too many pending leases awaiting acknowledgement.

**Solution**: 
1. Check current pending leases:
   ```bash
   manifestd query billing leases-by-tenant [tenant-address] --state pending
   ```
2. Wait for providers to acknowledge or reject pending leases, or cancel them:
   ```bash
   manifestd tx billing cancel-lease [lease-uuid] --from tenant
   ```
3. The default limit is 10 pending leases per tenant.

### "too many items in lease"

**Cause**: Attempting to create a lease with too many SKU items.

**Solution**: Split the lease into multiple smaller leases. The default limit is 20 items per lease (hard limit is 100).

### "quantity must be greater than zero"

**Cause**: Creating a lease item with quantity 0.

**Solution**: Ensure all items have quantity ≥ 1:
```bash
manifestd tx billing create-lease 01912345-6789-7abc-8def-0123456789ab:1 --from tenant
```

### "duplicate sku in lease items"

**Cause**: The same SKU UUID appears multiple times in the lease items without using service names.

**Solution**: Either combine quantities into a single item, or use service names for stack deployments:
```bash
# Option 1: Combine quantities into a single item
manifestd tx billing create-lease <sku-uuid>:5 --from tenant

# Option 2: Use service_names for stack deployments (same SKU, different services)
manifestd tx billing create-lease <sku-uuid>:2:web <sku-uuid>:3:db --from tenant
```

### "lease must contain at least one item"

**Cause**: Trying to create a lease with no items.

**Solution**: Specify at least one SKU item:
```bash
manifestd tx billing create-lease 01912345-6789-7abc-8def-0123456789ab:1 --from tenant
```

## Lease Acknowledgement Issues

### "lease not pending"

**Cause**: Attempting to acknowledge, reject, or cancel a lease that is not in PENDING state.

**Solution**: Check the lease state:
```bash
manifestd query billing lease [lease-uuid]
```

Only PENDING leases can be acknowledged/rejected by providers or cancelled by tenants.

### "unauthorized" (for acknowledge/reject)

**Cause**: The sender is not the provider who owns the SKUs in the lease.

**Solution**: Use the correct provider key:
```bash
manifestd tx billing acknowledge-lease [lease-uuid] --from [provider-key]
```

### "invalid rejection reason"

**Cause**: The rejection reason provided to `reject-lease` exceeds the maximum length of 256 characters.

**Solution**: Provide a shorter rejection reason:
```bash
# Maximum 256 characters
manifestd tx billing reject-lease [lease-uuid] --reason "Service unavailable" --from provider
```

### Provider not responding to pending lease

**What happens**:
1. Lease remains in PENDING state
2. After `pending_timeout` (default 30 minutes), the EndBlocker expires the lease
3. Lease state changes to EXPIRED
4. Credit is unlocked and remains in tenant's account

**What tenant can do**:
- Wait for automatic expiration
- Cancel the lease manually:
  ```bash
  manifestd tx billing cancel-lease [lease-uuid] --from tenant
  ```

## Lease Closure Issues

### "lease not found"

**Cause**: The specified lease UUID doesn't exist or is invalid.

**Solution**: 
1. Query your leases:
   ```bash
   manifestd query billing leases-by-tenant [your-address]
   ```
2. Use a valid lease UUID.

### "lease not active"

**Cause**: Attempting to close a lease that's not in ACTIVE state. Could be PENDING, CLOSED, REJECTED, or EXPIRED.

**Solution**: Check the lease state:
```bash
manifestd query billing lease [lease-uuid]
```

- **PENDING**: Use `cancel-lease` (tenant) or `reject-lease` (provider) instead
- **CLOSED/REJECTED/EXPIRED**: No action needed, lease is already terminated

### "unauthorized"

**Cause**: Attempting to close a lease you don't have permission to close.

**Solution**: Only the following can close an ACTIVE lease:
- The tenant who owns the lease
- The provider of the SKUs in the lease
- The module authority

## Withdrawal Issues

### "no withdrawable amount"

**Cause**: 
1. The lease is in PENDING state (billing hasn't started), OR
2. The lease has no accrued charges yet (just acknowledged or just settled), OR
3. The lease is not active (closed), OR
4. The provider already withdrew recently

**Solution**: 
1. Check the lease state and withdrawable amount:
   ```bash
   manifestd query billing lease [lease-uuid]
   manifestd query billing withdrawable [lease-uuid]
   ```
2. For PENDING leases, wait for acknowledgement or acknowledge it first
3. For ACTIVE leases, wait for more time to pass for charges to accrue
4. For closed leases, you cannot withdraw (settlement happened during close)

### "lease not active"

**Cause**: Attempting to withdraw from a lease that's not ACTIVE. PENDING leases don't accrue charges.

**Solution**: 
- For PENDING leases: Acknowledge the lease first
- For CLOSED leases: Settlement happened during closure, nothing left to withdraw
```bash
manifestd query billing lease [lease-uuid]
```

### "unauthorized"

**Cause**: Only the provider (or authority) can withdraw from a lease.

**Solution**: Use the provider's key to withdraw:
```bash
manifestd tx billing withdraw [lease-uuid] --from [provider-key]
```

### "provider not found"

**Cause**: The provider_uuid doesn't exist in the SKU module.

**Solution**: 
1. Query available providers:
   ```bash
   manifestd query sku providers
   ```
2. Use a valid provider UUID.

### Lease not included in provider-wide withdraw results

**Cause**: During provider-wide withdraw batch operations, individual lease settlements that encounter arithmetic overflow (extremely long-running leases with high rates) are silently skipped to prevent the entire batch from failing.

**What happens**:
1. The lease is skipped during the batch operation
2. Other leases in the batch are processed normally
3. No error is returned for the skipped lease
4. The skipped lease's funds remain available

**Solution**:
1. Use specific lease withdrawal for the problematic lease to see the actual error:
   ```bash
   manifestd tx billing withdraw [lease-uuid] --from provider
   ```
2. If overflow is the issue, close the lease and create a new one:
   ```bash
   manifestd tx billing close-lease [lease-uuid] --from provider
   ```

**Note**: This scenario only occurs with extremely long-running leases (years) with high per-second rates. Normal usage will not encounter this issue.

---

## Provider-Wide Withdraw Issues

### "cannot specify both lease_uuids and provider_uuid"

**Cause**: Both specific lease UUIDs and --provider flag were provided. These are mutually exclusive modes.

**Solution**: Use only one mode at a time:
```bash
# Mode 1: Specific leases (do NOT use --provider)
manifestd tx billing withdraw [lease-uuid] --from [key]

# Mode 2: Provider-wide (do NOT specify lease UUIDs)
manifestd tx billing withdraw --provider [provider-uuid] --from [key]
```

### "must specify either lease_uuids or provider_uuid"

**Cause**: Neither specific lease UUIDs nor --provider flag was provided.

**Solution**: Use one of the two modes:
```bash
# Mode 1: Specific leases
manifestd tx billing withdraw [lease-uuid] --from [key]

# Mode 2: Provider-wide
manifestd tx billing withdraw --provider [provider-uuid] --from [key]
```

### Response shows "has_more: true"

**Cause**: There are more leases to process than the limit allows.

**Solution**: Continue calling provider-wide withdraw until has_more is false:
```bash
# Process 50 leases at a time (default)
manifestd tx billing withdraw --provider [provider-uuid] --from [key]

# Or specify a larger limit (max 100)
manifestd tx billing withdraw --provider [provider-uuid] --limit 100 --from [key]
```

## Batch Operation Failures

### Batch operation failed entirely

**Cause**: All batch operations (acknowledge, reject, cancel, close, withdraw with specific UUIDs) are **atomic**. If any single lease in the batch fails validation, the entire batch fails and no changes are made.

**Common failure reasons:**
- One or more leases are in the wrong state (e.g., trying to acknowledge a CLOSED lease)
- Authorization failure on one lease (e.g., not the provider for that lease)
- One lease doesn't exist
- Leases belong to different providers (for operations requiring same-provider)

**Solution:**
1. Check each lease individually to identify the problem:
   ```bash
   for uuid in uuid1 uuid2 uuid3; do
     manifestd query billing lease $uuid
   done
   ```
2. Remove the problematic lease(s) from the batch
3. Retry the batch without the problematic lease(s)
4. Handle the problematic lease separately

**Example:**
```bash
# If batch acknowledge fails:
manifestd tx billing acknowledge-lease uuid1 uuid2 uuid3 --from provider  # FAILS

# Check each lease:
manifestd query billing lease uuid1  # PENDING - ok
manifestd query billing lease uuid2  # CLOSED - this is the problem!
manifestd query billing lease uuid3  # PENDING - ok

# Retry without uuid2:
manifestd tx billing acknowledge-lease uuid1 uuid3 --from provider  # SUCCESS
```

### "too many lease items in batch"

**Cause**: Exceeding `MaxBatchLeaseSize` (100 leases) in a single batch operation.

**Solution**: Split into multiple smaller batches:
```bash
# Instead of 150 leases in one call:
manifestd tx billing acknowledge-lease uuid1 uuid2 ... uuid150 --from provider  # FAILS

# Split into batches of 100:
manifestd tx billing acknowledge-lease uuid1 ... uuid100 --from provider
manifestd tx billing acknowledge-lease uuid101 ... uuid150 --from provider
```

### Batch event not showing all details

**Cause**: Batch operations emit summary events (`batch_acknowledged`, `batch_rejected`, etc.) alongside individual events. The batch event shows aggregate information (count, provider) while individual events show per-lease details.

**Solution**: To see all details, check both event types:
```bash
# Get transaction events
manifestd query tx [txhash] --output json | jq '.events[] | select(.type | startswith("batch_") or startswith("lease_"))'
```

**Event pattern:**
- Individual operations: `lease_acknowledged`, `lease_rejected`, etc. (one per lease)
- Batch summary: `batch_acknowledged`, `batch_rejected`, etc. (one per transaction)

---

## Query Issues

### "invalid lease_uuid format"

**Cause**: The lease UUID is not in valid UUIDv7 format.

**Solution**: Ensure you're using a valid UUID (format: `xxxxxxxx-xxxx-7xxx-xxxx-xxxxxxxxxxxx`):
```bash
manifestd query billing lease 01912345-6789-7abc-8def-0123456789ab
```

### "invalid tenant address"

**Cause**: The address format is invalid.

**Solution**: Use a valid bech32 address with the correct prefix:
```bash
manifestd query billing credit-account manifest1abc...
```

## Pending Lease Expiration

### Understanding Pending Expiration

Pending leases expire automatically if providers don't acknowledge them within `pending_timeout` (default 30 minutes).

**How it works**:
1. EndBlocker runs each block
2. Checks pending leases against `created_at + pending_timeout`
3. Expired leases transition to EXPIRED state
4. Credit is unlocked (was never billed since lease was never active)
5. `LeaseExpired` event is emitted

**Rate limiting**: Max 100 expirations per block to prevent DoS.

### Lease expired while waiting for provider

**What happened**:
1. Lease was created in PENDING state
2. Provider did not acknowledge within `pending_timeout`
3. EndBlocker expired the lease

**What to do**:
1. Credit remains in your account - no funds lost
2. Contact the provider if this was unexpected
3. Create a new lease if desired

## Auto-Close Behavior (for ACTIVE leases)

### Understanding Auto-Close

Auto-close is triggered when an ACTIVE lease's credit is exhausted (balance = 0). It happens during write operations only:
- `Withdraw` - If credit exhausted after settlement
- `CloseLease` - If credit was already exhausted

**Important:** Auto-close does NOT happen:
- For PENDING leases (they don't accrue charges)
- During query operations (queries are read-only)
- Automatically in the background (except for pending expiration)

### Lease closed automatically

**What happened**:
1. During a Withdraw or CloseLease operation, the system detected zero credit
2. Final settlement was performed (any remaining balance transferred to provider)
3. The lease was closed

**How to verify**:
```bash
# Check lease state
manifestd query billing lease [lease-uuid]

# Look for events in the transaction
manifestd query tx [txhash] --output json | jq '.events'
```

Events to look for:
- `lease_closed`

**Resolution**: 
1. Fund the credit account again
2. Create a new lease

### Why doesn't my query show accurate accrued amounts?

**Cause**: Lease queries (`Lease`, `Leases`, etc.) return stored state without performing settlement calculations. The `last_settled_at` field shows when settlement last occurred.

**Explanation**: 
- Lease queries return the stored `last_settled_at` timestamp
- They don't calculate time elapsed since then
- PENDING leases don't accrue charges (billing starts at acknowledgement)
- Use `WithdrawableAmount` or `ProviderWithdrawable` queries to get **real-time calculated amounts**:
  ```bash
  # Get real-time withdrawable for a specific lease
  manifestd query billing withdrawable [lease-uuid]
  
  # Get real-time total withdrawable for a provider
  manifestd query billing provider-withdrawable [provider-uuid]
  ```

## Parameter Issues

### "invalid params"

**Cause**: Invalid parameter values in UpdateParams.

**Common issues**:
- `min_lease_duration` must be > 0
- `max_leases_per_tenant` must be > 0
- `max_items_per_lease` must be > 0 and ≤ 100
- `max_pending_leases_per_tenant` must be > 0
- `pending_timeout` must be between 60 (1 min) and 86400 (24 hours)

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
