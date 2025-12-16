# SKU Setup Guide

This guide walks you through creating and managing SKUs (Stock Keeping Units) in the x/sku module.

## Prerequisites

- A Provider already created (see [Provider Setup Guide](PROVIDER_GUIDE.md))
- Access to the Manifest Network chain (testnet or mainnet)
- A funded wallet for transaction fees
- Authorization: Either be the module authority or be added to the `allowed_list` parameter

## Overview

A SKU (Stock Keeping Unit) represents a billable item or service offered by a Provider. SKUs define the pricing structure and billing unit (per hour or per day) for services.

## Step 1: Understand Pricing Requirements

### Billing Units

| Unit Value | Name | Seconds | Description |
|------------|------|---------|-------------|
| 1 | `UNIT_PER_HOUR` | 3,600 | Hourly billing |
| 2 | `UNIT_PER_DAY` | 86,400 | Daily billing |

### Exact Divisibility Rule

**Important:** The base price MUST be exactly divisible by the billing unit's seconds. This ensures accurate per-second rate calculations with no rounding errors.

| Unit | Valid Prices | Invalid Prices |
|------|--------------|----------------|
| UNIT_PER_HOUR (3600s) | 3600, 7200, 10800, 36000 | 3601, 5000, 10000 |
| UNIT_PER_DAY (86400s) | 86400, 172800, 259200, 864000 | 86401, 100000, 150000 |

**Formula:** `price % seconds_in_unit == 0`

### Calculating Valid Prices

To calculate a price for a desired per-second rate:

```
price = rate_per_second × seconds_in_unit

# For $1/hour (1 token/second × 3600 seconds)
price_hourly = 1 × 3600 = 3600 tokens

# For $2/hour (2 tokens/second × 3600 seconds)  
price_hourly = 2 × 3600 = 7200 tokens

# For $1/day (1 token/second × 86400 seconds)
price_daily = 1 × 86400 = 86400 tokens
```

## Step 2: Prepare SKU Information

| Field | Description | Example |
|-------|-------------|---------|
| **Provider ID** | The ID of the provider offering this SKU | `1` |
| **Name** | Human-readable name for the SKU | `"Compute Small"` |
| **Unit** | Billing unit (1 = per hour, 2 = per day) | `1` |
| **Base Price** | Price per billing unit (must be exactly divisible) | `3600upwr` |
| **Meta Hash** (optional) | Hex-encoded hash of off-chain metadata | `deadbeef` |

### About Meta Hash

The `meta_hash` field stores a hash of off-chain metadata describing the SKU in detail:

```json
{
  "description": "Small compute instance",
  "specs": {
    "cpu": "2 vCPU",
    "memory": "4 GB RAM",
    "storage": "50 GB SSD"
  },
  "region": "us-east-1",
  "sla": "99.9% uptime"
}
```

## Step 3: Verify the Provider

Ensure your provider exists and is active:

```bash
manifestd query sku provider <provider_id> --output json
```

The provider must have `"active": true` to create SKUs for it.

## Step 4: Create the SKU

```bash
manifestd tx sku create-sku \
  <provider_id> \
  "<name>" \
  <unit> \
  <base_price> \
  --meta-hash <hex_encoded_hash> \
  --from <your_key> \
  --chain-id manifest-1 \
  --fees 5000upwr
```

### Example: Hourly Billing SKU

```bash
manifestd tx sku create-sku \
  1 \
  "Compute Small" \
  1 \
  3600upwr \
  --meta-hash a1b2c3d4 \
  --from mykey \
  --chain-id manifest-1 \
  --fees 5000upwr
```

### Example: Daily Billing SKU

```bash
manifestd tx sku create-sku \
  1 \
  "Storage Basic" \
  2 \
  86400upwr \
  --meta-hash e5f6g7h8 \
  --from mykey \
  --chain-id manifest-1 \
  --fees 5000upwr
```

### Successful Response

```json
{
  "code": 0,
  "txhash": "DEF456...",
  "events": [
    {
      "type": "sku_created",
      "attributes": [
        {"key": "sku_id", "value": "1"},
        {"key": "provider_id", "value": "1"},
        {"key": "name", "value": "Compute Small"}
      ]
    }
  ]
}
```

Note the `sku_id` from the response - tenants will use this when creating leases.

## Step 5: Verify the SKU

Query your newly created SKU:

```bash
manifestd query sku sku 1 --output json
```

Response:
```json
{
  "sku": {
    "id": "1",
    "provider_id": "1",
    "name": "Compute Small",
    "unit": "UNIT_PER_HOUR",
    "base_price": {
      "denom": "upwr",
      "amount": "3600"
    },
    "meta_hash": "oLLD1A==",
    "active": true
  }
}
```

List all SKUs:
```bash
manifestd query sku skus --output json
```

List SKUs by provider:
```bash
manifestd query sku skus-by-provider 1 --output json
```

## Step 6: Update SKU (Optional)

To update SKU details (e.g., change price, name):

```bash
manifestd tx sku update-sku \
  <sku_id> \
  <provider_id> \
  "<new_name>" \
  <new_unit> \
  <new_base_price> \
  <active> \
  --meta-hash <new_meta_hash> \
  --from <your_key> \
  --chain-id manifest-1
```

### Example: Update Price

```bash
manifestd tx sku update-sku \
  1 \
  1 \
  "Compute Small" \
  1 \
  7200upwr \
  true \
  --from mykey \
  --chain-id manifest-1
```

> **Important:** Price changes only affect NEW leases. Existing leases continue with their original locked-in prices.

## Step 7: Deactivate SKU (If Needed)

To deactivate a SKU (soft delete):

```bash
manifestd tx sku deactivate-sku 1 \
  --from mykey \
  --chain-id manifest-1
```

> **Important:** Deactivating a SKU:
> - Prevents the SKU from being used in new leases
> - Does NOT affect existing leases (they continue at locked prices)
> - The SKU remains queryable for reporting purposes
> - Can be reactivated later via `update-sku` with `active=true`

## Creating Multiple SKUs

For a typical service offering, you might create several SKUs:

```bash
# Small instance - $1/hour (3600 tokens/hour)
manifestd tx sku create-sku 1 "Compute Small" 1 3600upwr --from mykey --chain-id manifest-1

# Medium instance - $2/hour (7200 tokens/hour)
manifestd tx sku create-sku 1 "Compute Medium" 1 7200upwr --from mykey --chain-id manifest-1

# Large instance - $5/hour (18000 tokens/hour)
manifestd tx sku create-sku 1 "Compute Large" 1 18000upwr --from mykey --chain-id manifest-1

# Storage - $1/day (86400 tokens/day)
manifestd tx sku create-sku 1 "Storage 100GB" 2 86400upwr --from mykey --chain-id manifest-1
```

## Common Issues

### "invalid sku" (price not divisible)

**Cause:** The price doesn't divide evenly by the billing unit's seconds.

**Solution:** 
- For UNIT_PER_HOUR: Use prices divisible by 3600 (e.g., 3600, 7200, 10800)
- For UNIT_PER_DAY: Use prices divisible by 86400 (e.g., 86400, 172800, 259200)

### "provider not found"

**Cause:** The provider ID doesn't exist.

**Solution:**
- Verify provider exists: `manifestd query sku provider <id>`
- Create the provider first if needed (see Provider Setup Guide)

### "invalid provider" (provider not active)

**Cause:** The provider has been deactivated.

**Solution:**
- Reactivate the provider using `update-provider` with `active=true`
- Or use a different active provider

### "unauthorized"

**Cause:** Your address is not authorized to manage SKUs.

**Solution:**
- Ask the module authority to add your address to the allowed list
- Or submit through governance

## SKU Lifecycle

```
┌─────────────────────────────────────────────────────────────────┐
│                        SKU Lifecycle                            │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  ┌──────────┐    ┌──────────┐    ┌──────────────┐              │
│  │  Create  │───>│  Active  │───>│  Deactivated │              │
│  └──────────┘    └──────────┘    └──────────────┘              │
│                       │                  │                      │
│                       │                  │                      │
│                       v                  v                      │
│                  Can be used        Cannot be used              │
│                  in new leases      in new leases               │
│                       │                  │                      │
│                       v                  v                      │
│                  Existing leases   Existing leases              │
│                  continue          continue                     │
│                  (locked price)    (locked price)               │
│                                                                 │
│  Price updates ───> Only affect NEW leases                     │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

## Pricing Strategy Tips

1. **Start with simple rates:** Use round numbers like 1, 2, 5, 10 tokens per second
2. **Consider denomination:** PWR tokens have 6 decimal places (1 PWR = 1,000,000 upwr)
3. **Plan for granularity:** Hourly billing allows finer control than daily
4. **Document off-chain:** Store detailed pricing tiers and conditions in metadata

## Next Steps

Once SKUs are created:

1. **Tenants create leases** - Users can now lease your SKUs via the billing module
2. **Monitor usage** - Query leases by provider to see active usage
3. **Withdraw earnings** - Use billing module to withdraw accrued tokens

## Related Documentation

- [Provider Setup Guide](PROVIDER_GUIDE.md) - Creating providers
- [API Reference](API.md) - Complete API documentation
- [Billing Module](../../billing/README.md) - Understanding the billing system
- [Billing Migration Guide](../../billing/docs/MIGRATION.md) - Migrating existing off-chain leases
