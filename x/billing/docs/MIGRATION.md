# Billing Module Migration Guide

This guide is for authority members responsible for migrating existing off-chain leases to the on-chain billing system.

## Overview

The migration process involves:
1. Setting up providers in the SKU module
2. Creating SKUs for billable items in the SKU module
3. Configuring billing parameters (limits, timeouts)
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
  10 \
  1800 \
  --from authority
```

**Parameters:**
| Parameter | Description | Default |
|-----------|-------------|---------|
| `max_leases_per_tenant` | Max active leases per tenant | 100 |
| `max_items_per_lease` | Max items in single lease | 20 |
| `min_lease_duration` | Min seconds credit must cover | 3600 |
| `max_pending_leases_per_tenant` | Max pending leases per tenant | 10 |
| `pending_timeout` | Seconds before pending lease expires | 1800 |

**Note:** There is no global `denom` parameter. Each SKU defines its own denomination in its `base_price`, enabling multi-denom billing.

### Adding Addresses to the Allow List

If you want non-authority addresses to create leases for tenants:

```bash
manifestd tx billing update-params \
  100 \
  20 \
  3600 \
  10 \
  1800 \
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
# Format: sku-uuid:quantity
manifestd tx billing create-lease-for-tenant [tenant-address] [sku-uuid:quantity...] --from [authority-key]

# Example: Create lease with 2 units of SKU 1 and 1 unit of SKU 2
manifestd tx billing create-lease-for-tenant manifest1abc... 01912345-6789-7abc-8def-0123456789ab:2 01912345-6789-7abc-8def-0123456789ac:1 --from authority
```

### Important: Leases Start in PENDING State

When you create a lease (via `MsgCreateLeaseForTenant` or `MsgCreateLease`), it starts in **PENDING** state. The provider must acknowledge the lease before billing begins:

```bash
# Provider acknowledges the lease (transitions PENDING → ACTIVE)
manifestd tx billing acknowledge-lease [lease-uuid] --from provider-key
```

For migrations, you may want to have the provider pre-acknowledge leases immediately after creation.

### Important Constraints

1. **All SKUs must be from the same provider** - Create separate leases for different providers
2. **All SKUs must be active** - Deactivated SKUs cannot be leased
3. **Provider must be active** - Deactivated providers cannot have new leases
4. **Credit must cover min_lease_duration** - Otherwise creation fails
5. **Pending timeout** - If provider doesn't acknowledge within `pending_timeout` (default 30 min), lease expires

### Multiple SKUs in One Lease

A single lease can include multiple SKUs, but they must all belong to the same provider:

```bash
# Multiple SKUs from the same provider
manifestd tx billing create-lease-for-tenant manifest1abc... <sku-uuid-1>:1 <sku-uuid-2>:2 <sku-uuid-3>:1 --from authority
```

### Batch Migration Script Example

For migrating many leases, consider a script that creates and acknowledges:

```bash
#!/bin/bash
# migration_script.sh

AUTHORITY_KEY="authority"
PROVIDER_KEY="provider"
DENOM="upwr"  # or your factory denom

# Array of tenant migrations: "address|sku_items|credit_amount"
MIGRATIONS=(
  "manifest1abc...|<sku-uuid>:2|100000000"
  "manifest1def...|<sku-uuid-1>:1 <sku-uuid-2>:1|50000000"
  "manifest1ghi...|<sku-uuid-3>:5|200000000"
)

for migration in "${MIGRATIONS[@]}"; do
  IFS='|' read -r tenant items credit <<< "$migration"
  
  echo "Processing tenant: $tenant"
  
  # Fund credit account
  echo "  Funding ${credit}${DENOM}..."
  manifestd tx billing fund-credit "$tenant" "${credit}${DENOM}" \
    --from "$AUTHORITY_KEY" -y --gas auto --gas-adjustment 1.5
  
  sleep 6  # Wait for block
  
  # Create lease (starts in PENDING state)
  echo "  Creating lease with items: $items..."
  RESULT=$(manifestd tx billing create-lease-for-tenant "$tenant" $items \
    --from "$AUTHORITY_KEY" -y --gas auto --gas-adjustment 1.5 --output json)
  
  LEASE_UUID=$(echo "$RESULT" | jq -r '.logs[0].events[] | select(.type=="lease_created") | .attributes[] | select(.key=="lease_uuid") | .value')
  
  sleep 6  # Wait for block
  
  # Acknowledge lease (transitions to ACTIVE, billing starts)
  echo "  Acknowledging lease $LEASE_UUID..."
  manifestd tx billing acknowledge-lease "$LEASE_UUID" \
    --from "$PROVIDER_KEY" -y --gas auto --gas-adjustment 1.5
  
  sleep 6  # Wait for block
  
  echo "  Done!"
done

echo "Migration complete!"
```

## Step 5: Verify Migration

After migration, verify the leases were created and acknowledged correctly:

```bash
# Query tenant's leases (should show ACTIVE state)
manifestd query billing leases-by-tenant [tenant-address] --state active

# Query tenant's credit account
manifestd query billing credit-account [tenant-address]

# Query specific lease details
manifestd query billing lease [lease-uuid]
```

## Important Notes

### Lease Lifecycle

1. **PENDING**: Lease created, credit locked, awaiting provider acknowledgement
2. **ACTIVE**: Provider acknowledged, billing has started
3. **CLOSED**: Lease terminated normally

For migrations, ensure the provider acknowledges leases promptly to avoid them expiring.

### Price Locking

When you create a lease, the current SKU prices are **locked in** as per-second rates for the duration of that lease. If SKU prices change later, existing leases continue at their locked prices.

### Credit Persistence

Credit that remains in a tenant's credit account stays there. There is no mechanism to withdraw unused credit - this mimics typical cloud provider behavior where credits must be spent.

### Events

Each lease creation and state transition emits events for auditing:

```bash
# Query events for a transaction
manifestd query tx [txhash] --output json | jq '.events'
```

Key events:
- `lease_created` - Contains `lease_uuid`, `tenant`, `provider_uuid`
- `lease_acknowledged` - Contains `lease_uuid`, `provider_uuid`, `acknowledged_at`
- `lease_rejected` - Contains `lease_uuid`, `provider_uuid`, `reason`
- `lease_closed` - Contains `lease_uuid`, `tenant`, `settled_amounts`
- `credit_funded` - Contains `tenant`, `amount`, `credit_address`

### Settlement

Leases created via `MsgCreateLeaseForTenant` work exactly like tenant-created leases:
- Billing only starts after provider acknowledgement (ACTIVE state)
- Settlement happens during `Withdraw` or `CloseLease` operations
- Auto-close triggers when credit is exhausted during write operations
- Tenants can close their own leases (even if created by authority)

## Rollback Considerations

If a migration needs to be reversed:

1. **For PENDING leases**: Cancel or reject them
   ```bash
   # Tenant cancels
   manifestd tx billing cancel-lease [lease-uuid] --from tenant
   # Or provider rejects
   manifestd tx billing reject-lease [lease-uuid] --from provider
   ```

2. **For ACTIVE leases**: Close the lease
   ```bash
   manifestd tx billing close-lease [lease-uuid] --from authority
   ```

3. **Settlement happens automatically**: Any accrued amount is transferred to the provider during closure

4. **Credit remains**: Any unspent credit stays in the tenant's credit account for future use

5. **Provider withdrawal**: Provider should withdraw any accrued funds before/after closing if needed
   ```bash
   manifestd tx billing withdraw [lease-uuid] --from provider
   ```

## Common Issues

### "insufficient credit balance"

The tenant doesn't have enough credit to cover `min_lease_duration` for one or more denominations. Calculate the minimum required for each denom:

```bash
# Check SKU rates and denoms
manifestd query sku sku [sku-uuid]

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
manifestd query sku sku [sku-uuid]
```

If the SKU doesn't exist, create it first via the SKU module.

### "all SKUs must belong to the same provider"

Multi-provider leases are not supported. Create separate leases for different providers:

```bash
# Provider 1 SKUs
manifestd tx billing create-lease-for-tenant manifest1abc... <provider1-sku-uuid>:1 --from authority

# Provider 2 SKUs (separate lease)
manifestd tx billing create-lease-for-tenant manifest1abc... <provider2-sku-uuid>:1 --from authority
```

### "unauthorized"

Your address is not the authority and not in the `allowed_list`. Check params:

```bash
manifestd query billing params
```

To add your address to the allow list, the authority must update params.

### "provider is not active"

The provider associated with the SKU has been deactivated. Contact the authority to reactivate, or use SKUs from an active provider.

### Lease expires before acknowledgement

If the lease expires (remains in PENDING past `pending_timeout`), it transitions to EXPIRED state. The credit is not consumed and remains available. To avoid this during migration:

1. Increase `pending_timeout` temporarily via `update-params`
2. Have provider ready to acknowledge immediately after creation
3. Script the create and acknowledge together (as shown above)

## Genesis Import Validation

When importing billing state via genesis (e.g., during chain upgrades or migrations), the module performs two-phase validation:

### Phase 1: Static Validation (`ValidateGenesis`)

Validates without blockchain context:
- Lease UUIDs are valid and unique
- Credit account addresses are correctly derived
- Required fields are present
- SKU-provider relationships are consistent

### Phase 2: Time-Based Validation (`ValidateWithBlockTime`)

Validates timestamps against block time during `InitGenesis`:

| Field | Validation |
|-------|------------|
| `last_settled_at` | Must not be in the future |
| `created_at` | Must not be in the future |
| `closed_at` | Must not be in the future (for CLOSED leases) |
| `rejected_at` | Must not be in the future (for REJECTED leases) |
| `expired_at` | Must not be in the future (for EXPIRED leases) |
| `acknowledged_at` | Must not be in the future (for ACTIVE leases) |

**Error Example:**
```
lease abc123 has last_settled_at (2025-01-08T00:00:00Z) in the future relative to block time (2025-01-07T12:00:00Z)
```

**Resolution:** Ensure all timestamps in genesis state are at or before the genesis block time.

## Related Documentation

- [Billing README](../README.md) - Complete billing module overview
- [API Reference](API.md) - Detailed API documentation
- [Troubleshooting Guide](TROUBLESHOOTING.md) - Common issues and solutions
- [Architecture](ARCHITECTURE.md) - Technical architecture details
