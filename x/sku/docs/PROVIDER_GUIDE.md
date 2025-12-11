# Provider Setup Guide

This guide walks you through setting up a Provider in the x/sku module.

## Prerequisites

- Access to the Manifest Network chain (testnet or mainnet)
- A funded wallet for transaction fees
- Authorization: Either be the module authority or be added to the `allowed_list` parameter

## Overview

A Provider represents an entity (organization, business, or individual) that offers services through the Manifest Network billing system. Before you can create SKUs (billable items), you must first create a Provider.

## Step 1: Check Your Authorization

First, verify you have permission to create providers:

```bash
# Check the current allowed list
manifestd query sku params --output json
```

The response will show:
```json
{
  "params": {
    "allowed_list": ["manifest1abc...", "manifest1def..."]
  }
}
```

You can create providers if:
1. Your address is in the `allowed_list`, OR
2. You are the module authority (governance)

### Adding Addresses to the Allowed List

Only the module authority can add addresses to the allowed list:

```bash
manifestd tx sku update-params \
  --allowed-list "manifest1existing...,manifest1newaddress..." \
  --from authority \
  --chain-id manifest-1
```

> **Note:** The allowed list is replaced entirely, so include all addresses you want authorized.

## Step 2: Prepare Provider Information

Before creating a provider, gather the following information:

| Field | Description | Example |
|-------|-------------|---------|
| **Address** | The provider's operational address (can be used for management) | `manifest1provider...` |
| **Payout Address** | Where earned tokens will be sent during withdrawals | `manifest1payout...` |
| **Meta Hash** (optional) | Hex-encoded hash of off-chain metadata (e.g., business info, contact details) | `deadbeef` |

### About Meta Hash

The `meta_hash` field stores a hash (e.g., SHA256) of off-chain metadata. This allows you to:
- Reference additional provider information stored off-chain (IPFS, database, etc.)
- Verify the integrity of off-chain data
- Keep on-chain data minimal

Example off-chain metadata:
```json
{
  "name": "Acme Cloud Services",
  "website": "https://acme.example.com",
  "support_email": "support@acme.example.com",
  "description": "Enterprise cloud computing solutions"
}
```

Hash this JSON and store the hex-encoded hash on-chain.

## Step 3: Create the Provider

```bash
manifestd tx sku create-provider \
  <address> \
  <payout_address> \
  --meta-hash <hex_encoded_hash> \
  --from <your_key> \
  --chain-id manifest-1 \
  --fees 5000umfx
```

### Example

```bash
manifestd tx sku create-provider \
  manifest1provideraddr123456789abcdef \
  manifest1payoutaddr987654321fedcba \
  --meta-hash a1b2c3d4e5f6 \
  --from mykey \
  --chain-id manifest-1 \
  --fees 5000umfx
```

### Successful Response

```json
{
  "code": 0,
  "txhash": "ABC123...",
  "events": [
    {
      "type": "provider_created",
      "attributes": [
        {"key": "provider_id", "value": "1"},
        {"key": "address", "value": "manifest1provideraddr..."},
        {"key": "payout_address", "value": "manifest1payoutaddr..."}
      ]
    }
  ]
}
```

Note the `provider_id` from the response - you'll need it when creating SKUs.

## Step 4: Verify the Provider

Query your newly created provider:

```bash
# By ID
manifestd query sku provider 1 --output json
```

Response:
```json
{
  "provider": {
    "id": "1",
    "address": "manifest1provideraddr...",
    "payout_address": "manifest1payoutaddr...",
    "meta_hash": "oLLD1OX2",
    "active": true
  }
}
```

List all providers:
```bash
manifestd query sku providers --output json
```

## Step 5: Update Provider (Optional)

If you need to update provider details:

```bash
manifestd tx sku update-provider \
  <provider_id> \
  <new_address> \
  <new_payout_address> \
  <active> \
  --meta-hash <new_meta_hash> \
  --from <your_key> \
  --chain-id manifest-1
```

### Example: Change Payout Address

```bash
manifestd tx sku update-provider \
  1 \
  manifest1provideraddr123456789abcdef \
  manifest1newpayoutaddr111222333 \
  true \
  --from mykey \
  --chain-id manifest-1
```

## Step 6: Deactivate Provider (If Needed)

To deactivate a provider (soft delete):

```bash
manifestd tx sku deactivate-provider 1 \
  --from mykey \
  --chain-id manifest-1
```

> **Important:** Deactivating a provider:
> - Prevents creation of new SKUs for this provider
> - Does NOT affect existing SKUs (they remain as-is)
> - Does NOT affect existing leases (billing continues)
> - The provider can still receive withdrawals from active leases

## Next Steps

Once your provider is created, you can:

1. **Create SKUs** - See [SKU Setup Guide](SKU_GUIDE.md) for creating billable items
2. **Monitor Activity** - Query provider's SKUs and associated leases
3. **Withdraw Earnings** - Use the billing module to withdraw accrued tokens

## Common Issues

### "unauthorized: expected authority or allowed address"

**Cause:** Your address is not authorized to manage providers.

**Solution:** 
- Ask the module authority to add your address to the allowed list
- Or submit the transaction through governance if you're using the authority

### "provider not found"

**Cause:** The provider ID doesn't exist.

**Solution:** 
- List all providers: `manifestd query sku providers`
- Verify you're using the correct provider ID

### "invalid address"

**Cause:** The address format is invalid.

**Solution:**
- Ensure addresses start with `manifest1`
- Verify the address is the correct length (typically 43 characters for bech32)

## Provider Lifecycle

```
┌─────────────────────────────────────────────────────────────────┐
│                      Provider Lifecycle                         │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  ┌──────────┐    ┌──────────┐    ┌──────────────┐              │
│  │  Create  │───>│  Active  │───>│  Deactivated │              │
│  └──────────┘    └──────────┘    └──────────────┘              │
│                       │                  │                      │
│                       │                  │                      │
│                       v                  v                      │
│                  Can create         Cannot create               │
│                  new SKUs           new SKUs                    │
│                       │                  │                      │
│                       v                  v                      │
│                  Existing SKUs     Existing SKUs                │
│                  work normally     work normally                │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

## Related Documentation

- [SKU Setup Guide](SKU_GUIDE.md) - Creating and managing SKUs
- [Billing Module README](../billing/README.md) - Understanding the billing system
- [Migration Guide](../billing/MIGRATION.md) - Migrating existing off-chain leases
