# Provider Setup Guide

This guide walks you through setting up a Provider in the x/sku module.

## Prerequisites

- Access to the Manifest Network chain (testnet or mainnet)
- A funded wallet for transaction fees
- Authorization: Either be the module authority or be added to the `allowed_list` parameter

## Overview

A Provider represents an entity (organization, business, or individual) that offers services through the Manifest Network billing system. Before you can create SKUs (billable items), you must first create a Provider.

Each Provider has:
- **UUID**: Auto-generated unique UUIDv7 identifier (deterministic for consensus)
- **Address**: Management address for the provider
- **Payout Address**: Where earned tokens are sent during withdrawals
- **API URL**: HTTPS endpoint for the provider's off-chain API (tenant authentication)
- **Meta Hash**: Optional hash of off-chain metadata
- **Active**: Whether the provider can have new SKUs created

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
2. You are the module authority (POA admin group)

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
| **API URL** | HTTPS endpoint where tenants can authenticate to get connection details | `https://api.provider.com` |
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
  --api-url <https_url> \
  --meta-hash <hex_encoded_hash> \
  --from <your_key> \
  --chain-id manifest-1 \
  --fees 5000upwr
```

### Example

```bash
manifestd tx sku create-provider \
  manifest1provideraddr123456789abcdef \
  manifest1payoutaddr987654321fedcba \
  --api-url https://api.myprovider.com \
  --meta-hash a1b2c3d4e5f6 \
  --from mykey \
  --chain-id manifest-1 \
  --fees 5000upwr
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
        {"key": "provider_uuid", "value": "01912345-6789-7abc-8def-0123456789ab"},
        {"key": "address", "value": "manifest1provideraddr..."},
        {"key": "payout_address", "value": "manifest1payoutaddr..."}
      ]
    }
  ]
}
```

Note the `provider_uuid` from the response - you'll need it when creating SKUs.

## Step 4: Verify the Provider

Query your newly created provider:

```bash
# By UUID
manifestd query sku provider 01912345-6789-7abc-8def-0123456789ab --output json
```

Response:
```json
{
  "provider": {
    "uuid": "01912345-6789-7abc-8def-0123456789ab",
    "address": "manifest1provideraddr...",
    "payout_address": "manifest1payoutaddr...",
    "api_url": "https://api.myprovider.com",
    "meta_hash": "oLLD1OX2",
    "active": true
  }
}
```

List all providers:
```bash
manifestd query sku providers --output json
```

List active providers only:
```bash
manifestd query sku providers --active-only --output json
```

## Step 5: Update Provider (Optional)

If you need to update provider details:

```bash
manifestd tx sku update-provider \
  <provider_uuid> \
  <new_address> \
  <new_payout_address> \
  <active> \
  --api-url <new_api_url> \
  --meta-hash <new_meta_hash> \
  --from <your_key> \
  --chain-id manifest-1
```

### Example: Change Payout Address

```bash
manifestd tx sku update-provider \
  01912345-6789-7abc-8def-0123456789ab \
  manifest1provideraddr123456789abcdef \
  manifest1newpayoutaddr111222333 \
  true \
  --api-url https://api.myprovider.com \
  --from mykey \
  --chain-id manifest-1
```

## Step 6: Deactivate Provider (If Needed)

To deactivate a provider (soft delete):

```bash
manifestd tx sku deactivate-provider 01912345-6789-7abc-8def-0123456789ab \
  --from mykey \
  --chain-id manifest-1
```

> **Important:** Deactivating a provider:
> - Prevents creation of new SKUs for this provider
> - Does NOT affect existing SKUs (they remain as-is)
> - Does NOT affect existing leases (billing continues)
> - The provider can still receive withdrawals from active leases
> - Can be reactivated via `update-provider` with `active=true`

## Next Steps

Once your provider is created, you can:

1. **Create SKUs** - See [SKU Setup Guide](SKU_GUIDE.md) for creating billable items
2. **Monitor Activity** - Query provider's SKUs and associated leases
3. **Withdraw Earnings** - Use the billing module to withdraw accrued tokens

## Common Issues

### "unauthorized"

**Cause:** Your address is not authorized to manage providers.

**Solution:** 
- Ask the module authority to add your address to the allowed list
- Or submit the transaction through governance if you're using the authority

### "provider not found"

**Cause:** The provider UUID doesn't exist.

**Solution:** 
- List all providers: `manifestd query sku providers`
- Verify you're using the correct provider UUID

### "invalid provider" (invalid address)

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
│                       │                  │                      │
│                       v                  v                      │
│                  Can receive       Can receive                  │
│                  withdrawals       withdrawals                  │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

## Related Documentation

- [SKU Setup Guide](SKU_GUIDE.md) - Creating and managing SKUs
- [API Reference](API.md) - Complete API documentation
- [Billing Module](../../billing/README.md) - Understanding the billing system
- [Billing Migration Guide](../../billing/docs/MIGRATION.md) - Migrating existing off-chain leases
