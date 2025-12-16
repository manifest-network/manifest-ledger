# Billing Module Migration Guide

This guide is for authority members responsible for migrating existing off-chain leases to the on-chain billing system.

## Overview

The migration process involves:
1. Setting up providers in the SKU module
2. Creating SKUs for billable items in the SKU module
3. Configuring billing parameters (denom, limits)
4. Funding tenant credit accounts
5. Creating leases on behalf of tenants using `MsgCreateLeaseForTenant`

## Prerequisites

- You must be the **module authority** (POA admin group address) OR
- Your address must be in the `allowed_list` billing parameter
- The **SKU module** must have the required providers and SKUs already created
- You must have sufficient tokens (matching SKU denominations) to fund tenant credit accounts

## Step 1: Configure Billing Parameters

Before migrating, ensure billing parameters are properly set:

```bash
# Check current parameters
manifestd query billing params
```

If parameters need updating:

```bash
# Update params via authority
manifestd tx billing update-params \
  100 \
  20 \
  3600 \
  --from authority
```

**Parameters:**
| Parameter | Description | Default |
|-----------|-------------|---------|
| `max_leases_per_tenant` | Max active leases per tenant | 100 |
| `max_items_per_lease` | Max items in single lease | 20 |
| `min_lease_duration` | Min seconds credit must cover | 3600 |

**Note:** There is no global `denom` parameter. Each SKU defines its own denomination in its `base_price`, enabling multi-denom billing.

### Adding Addresses to the Allow List

If you want non-authority addresses to create leases for tenants:

```bash
manifestd tx billing update-params \
  "factory/manifest1.../upwr" \
  100 \
  20 \
  3600 \
  --allowed-list "manifest1allowed1...,manifest1allowed2..." \
  --from authority
```

## Step 2: Verify SKU Setup

Before migrating leases, ensure all necessary providers and SKUs exist:

```bash
# List all providers
manifestd query sku providers

# List all SKUs
manifestd query sku skus

# Query specific SKU to verify details
manifestd query sku sku [sku-id]
```

**Important:** Note the SKU IDs and their per-second rates. You'll need these to calculate minimum credit requirements.

## Step 3: Fund Tenant Credit Accounts

Each tenant needs credit before a lease can be created for them. The credit must cover at least `min_lease_duration` seconds of the lease.

### Calculate Minimum Credit Required

For each denomination used by the SKUs in the lease:

```
min_credit[denom] = sum(sku_rate_per_second × quantity for SKUs with that denom) × min_lease_duration
```

**Example (single denom):**
- SKU 1: 1 upwr/second, quantity 2 → 2 upwr/second
- SKU 2: 5 upwr/second, quantity 1 → 5 upwr/second
- Total rate: 7 upwr/second
- Min duration: 3600 seconds
- Min credit: 7 × 3600 = 25,200 upwr

**Example (multi-denom):**
- SKU 1: 1 upwr/second, quantity 2 → 2 upwr/second
- SKU 2: 3 umfx/second, quantity 1 → 3 umfx/second
- Min duration: 3600 seconds
- Min credit needed: 7,200 upwr AND 10,800 umfx

### Fund the Credit Account

```bash
# Fund a tenant's credit account
manifestd tx billing fund-credit [tenant-address] [amount] --from [authority-key]

# Example: Fund with 100,000,000 upwr (100 PWR)
manifestd tx billing fund-credit manifest1abc... 100000000upwr --from authority

# For multi-denom, fund each denom separately
manifestd tx billing fund-credit manifest1abc... 100000000upwr --from authority
manifestd tx billing fund-credit manifest1abc... 50000000umfx --from authority

# Verify credit was received
manifestd query billing credit-account [tenant-address]
```

**Note:** Anyone can fund any tenant's credit account - this is not restricted to authority. Credit accounts support multiple denominations.

## Step 4: Create Leases for Tenants

Use `MsgCreateLeaseForTenant` to create leases on behalf of users:

```bash
# Create a lease for a tenant
# Format: sku_id:quantity
manifestd tx billing create-lease-for-tenant [tenant-address] [sku_id:quantity...] --from [authority-key]

# Example: Create lease with 2 units of SKU 1 and 1 unit of SKU 2
manifestd tx billing create-lease-for-tenant manifest1abc... 1:2 2:1 --from authority
```

### Important Constraints

1. **All SKUs must be from the same provider** - Create separate leases for different providers
2. **All SKUs must be active** - Deactivated SKUs cannot be leased
3. **Provider must be active** - Deactivated providers cannot have new leases
4. **Credit must cover min_lease_duration** - Otherwise creation fails

### Multiple SKUs in One Lease

A single lease can include multiple SKUs, but they must all belong to the same provider:

```bash
# Multiple SKUs from the same provider
manifestd tx billing create-lease-for-tenant manifest1abc... 1:1 2:2 3:1 --from authority
```

### Batch Migration Script Example

For migrating many leases, consider a script:

```bash
#!/bin/bash
# migration_script.sh

AUTHORITY_KEY="authority"
DENOM="upwr"  # or your factory denom

# Array of tenant migrations: "address|sku_items|credit_amount"
MIGRATIONS=(
  "manifest1abc...|1:2|100000000"
  "manifest1def...|1:1 2:1|50000000"
  "manifest1ghi...|3:5|200000000"
)

for migration in "${MIGRATIONS[@]}"; do
  IFS='|' read -r tenant items credit <<< "$migration"
  
  echo "Processing tenant: $tenant"
  
  # Fund credit account
  echo "  Funding ${credit}${DENOM}..."
  manifestd tx billing fund-credit "$tenant" "${credit}${DENOM}" \
    --from "$AUTHORITY_KEY" -y --gas auto --gas-adjustment 1.5
  
  sleep 6  # Wait for block
  
  # Create lease
  echo "  Creating lease with items: $items..."
  manifestd tx billing create-lease-for-tenant "$tenant" $items \
    --from "$AUTHORITY_KEY" -y --gas auto --gas-adjustment 1.5
  
  sleep 6  # Wait for block
  
  echo "  Done!"
done

echo "Migration complete!"
```

## Step 5: Verify Migration

After migration, verify the leases were created correctly:

```bash
# Query tenant's leases
manifestd query billing leases-by-tenant [tenant-address]

# Query tenant's credit account
manifestd query billing credit-account [tenant-address]

# Query specific lease details
manifestd query billing lease [lease-id]
```

## Important Notes

### Price Locking

When you create a lease, the current SKU prices are **locked in** as per-second rates for the duration of that lease. If SKU prices change later, existing leases continue at their locked prices.

### Credit Persistence

Credit that remains in a tenant's credit account stays there. There is no mechanism to withdraw unused credit - this mimics typical cloud provider behavior where credits must be spent.

### Events

Each lease creation emits events for auditing:

```bash
# Query events for a transaction
manifestd query tx [txhash] --output json | jq '.events'
```

Key events:
- `lease_created` - Contains `lease_id`, `tenant`, `provider_id`, `created_by`
- `credit_funded` - Contains `tenant`, `amount`, `credit_address`

### Settlement

Leases created via `MsgCreateLeaseForTenant` work exactly like tenant-created leases:
- Settlement happens during `Withdraw` or `CloseLease` operations
- Auto-close triggers when credit is exhausted during write operations
- Tenants can close their own leases (even if created by authority)

## Rollback Considerations

If a migration needs to be reversed:

1. **Close the lease**: The tenant, provider, or authority can close the lease
   ```bash
   manifestd tx billing close-lease [lease-id] --from authority
   ```

2. **Settlement happens automatically**: Any accrued amount is transferred to the provider during closure

3. **Credit remains**: Any unspent credit stays in the tenant's credit account for future use

4. **Provider withdrawal**: Provider should withdraw any accrued funds before/after closing if needed
   ```bash
   manifestd tx billing withdraw [lease-id] --from provider
   ```

## Common Issues

### "insufficient credit balance"

The tenant doesn't have enough credit to cover `min_lease_duration` for one or more denominations. Calculate the minimum required for each denom:

```bash
# Check SKU rates and denoms
manifestd query sku sku [sku-id]

# For each denom: sum(rate × quantity) × min_lease_duration
# Fund accordingly
manifestd tx billing fund-credit [tenant] [amount_denom1] --from authority
manifestd tx billing fund-credit [tenant] [amount_denom2] --from authority
```

### "credit account not found"

This happens if you try to create a lease before funding. The credit account is created automatically when first funded:

```bash
manifestd tx billing fund-credit [tenant] [amount] --from authority
```

### "SKU not found" or "SKU is not active"

Ensure the SKU exists and is active:

```bash
manifestd query sku sku [sku-id]
```

If the SKU doesn't exist, create it first via the SKU module.

### "all SKUs must belong to the same provider"

Multi-provider leases are not supported. Create separate leases for different providers:

```bash
# Provider 1 SKUs
manifestd tx billing create-lease-for-tenant manifest1abc... 1:1 2:1 --from authority

# Provider 2 SKUs (separate lease)
manifestd tx billing create-lease-for-tenant manifest1abc... 5:1 6:1 --from authority
```

### "unauthorized"

Your address is not the authority and not in the `allowed_list`. Check params:

```bash
manifestd query billing params
```

To add your address to the allow list, the authority must update params.

### "provider is not active"

The provider associated with the SKU has been deactivated. Contact the authority to reactivate, or use SKUs from an active provider.

## Related Documentation

- [Billing README](../README.md) - Complete billing module overview
- [API Reference](API.md) - Detailed API documentation
- [Troubleshooting Guide](TROUBLESHOOTING.md) - Common issues and solutions
- [Architecture](ARCHITECTURE.md) - Technical architecture details
