# Billing Module Migration Guide

This guide is for authority members responsible for migrating existing off-chain leases to the on-chain billing system.

## Overview

The migration process involves:
1. Setting up providers (see [Provider Setup Guide](../sku/PROVIDER_GUIDE.md))
2. Creating SKUs for billable items (see [SKU Setup Guide](../sku/SKU_GUIDE.md))
3. Ensuring PWR token is available
4. Funding tenant credit accounts
5. Creating leases on behalf of tenants using `MsgCreateLeaseForTenant`

## Prerequisites

- You must be an **authority member** (part of the POA admin group) OR
- Your address must be in the `allowed_list` parameter
- The **SKU module** must have the required providers and SKUs already created
- The **PWR token** must be available (factory denom)

## Step 0: Set Up Providers and SKUs

Before migrating any leases, you must have providers and SKUs configured. Follow these guides in order:

1. **[Provider Setup Guide](../sku/PROVIDER_GUIDE.md)** - Create providers for each service entity
2. **[SKU Setup Guide](../sku/SKU_GUIDE.md)** - Create SKUs (billable items) for each provider

## Step 1: Verify SKU Setup

Before migrating leases, ensure all necessary providers and SKUs exist:

```bash
# List all providers
manifestd query sku providers

# List all SKUs
manifestd query sku skus

# Query specific SKU to verify details
manifestd query sku sku [sku-id]
```

## Step 2: Fund Tenant Credit Accounts

Each tenant needs credit before a lease can be created for them.

```bash
# Fund a tenant's credit account
manifestd tx billing fund-credit [tenant-address] [amount] --from [authority-key]

# Example: Fund 1000 PWR
manifestd tx billing fund-credit manifest1abc... 1000000000factory/manifest1afk9zr2hn2jsac63h4hm60vl9z3e5u69gndzf7c99cqge3vzwjzsfmy9qj/upwr --from authority

# Verify credit was received
manifestd query billing credit-account [tenant-address]
```

**Note**: The minimum credit balance required to create a lease is 5 PWR (5000000upwr) by default.

## Step 3: Create Leases for Tenants

Use `MsgCreateLeaseForTenant` to create leases on behalf of users:

```bash
# Create a lease for a tenant
# Format: sku_id:quantity
manifestd tx billing create-lease-for-tenant [tenant-address] [sku_id:quantity...] --from [authority-key]

# Example: Create lease with 2 units of SKU 1 and 1 unit of SKU 2
manifestd tx billing create-lease-for-tenant manifest1abc... 1:2 2:1 --from authority
```

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
PWR_DENOM="factory/manifest1afk9zr2hn2jsac63h4hm60vl9z3e5u69gndzf7c99cqge3vzwjzsfmy9qj/upwr"

# Array of tenant migrations: "address|sku_items|credit_amount"
MIGRATIONS=(
  "manifest1abc...|1:2|100000000000"
  "manifest1def...|1:1 2:1|50000000000"
  "manifest1ghi...|3:5|200000000000"
)

for migration in "${MIGRATIONS[@]}"; do
  IFS='|' read -r tenant items credit <<< "$migration"
  
  echo "Processing tenant: $tenant"
  
  # Fund credit account
  echo "  Funding ${credit}${PWR_DENOM}..."
  manifestd tx billing fund-credit "$tenant" "${credit}${PWR_DENOM}" \
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

## Step 4: Verify Migration

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

When you create a lease, the current SKU prices are **locked in** for the duration of that lease. If SKU prices change later, existing leases continue at their locked prices.

### Credit Persistence

Credit that remains in a tenant's credit account stays there. There is no mechanism to withdraw unused credit - this mimics typical cloud provider behavior where credits must be spent.

### Events

Each lease creation emits a `lease_created` event with `created_by: authority` to distinguish from tenant-created leases. Use these events for auditing.

```bash
# Query events for a transaction
manifestd query tx [txhash] --output json | jq '.events'
```

### Authorization

The `MsgCreateLeaseForTenant` message can only be executed by:
- The module authority (POA admin group)
- Addresses explicitly listed in the `allowed_list` parameter

To add an address to the allowed list:

```bash
manifestd tx billing update-params \
  "factory/manifest1.../upwr" \
  100 \
  20 \
  3600 \
  --allowed-list "manifest1allowed1...,manifest1allowed2..." \
  --from authority
```

## Rollback Considerations

If a migration needs to be reversed:

1. **Close the lease**: The tenant, provider, or authority can close the lease
   ```bash
   manifestd tx billing close-lease [lease-id] --from authority
   ```

2. **Credit remains**: Any unspent credit stays in the tenant's credit account for future use

3. **Provider withdrawal**: Ensure the provider withdraws any accrued funds before closing
   ```bash
   manifestd tx billing withdraw [lease-id] --from provider
   ```

## Common Issues

### "insufficient credit balance"

The tenant doesn't have enough credit. Fund their account first:
```bash
manifestd tx billing fund-credit [tenant] [amount] --from authority
```

### "tenant has no credit account"

Create the credit account by funding it:
```bash
manifestd tx billing fund-credit [tenant] [amount] --from authority
```

### "SKU not found" or "SKU is not active"

Ensure the SKU exists and is active:
```bash
manifestd query sku sku [sku-id]
```

If the SKU doesn't exist, create it first. See [SKU Setup Guide](../sku/SKU_GUIDE.md).

### "all SKUs must belong to the same provider"

Multi-provider leases are not supported. Create separate leases for different providers.

### "unauthorized"

Your address is not the authority and not in the `allowed_list`. Check params:
```bash
manifestd query billing params
```

## Related Documentation

- [Provider Setup Guide](../sku/PROVIDER_GUIDE.md) - Creating and managing providers
- [SKU Setup Guide](../sku/SKU_GUIDE.md) - Creating and managing SKUs
- [Billing README](README.md) - Complete billing module overview
- [Billing API Reference](API.md) - Detailed API documentation
- [Troubleshooting Guide](TROUBLESHOOTING.md) - Common issues and solutions
