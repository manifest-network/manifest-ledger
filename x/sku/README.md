# x/sku

The SKU (Stock Keeping Unit) module provides on-chain management of providers and billing units for the Manifest Network.

## Overview

This module enables the creation, management, and querying of Providers and SKUs which represent service providers and their billable items. Each Provider has an address, payout address, and metadata. Each SKU contains pricing information and is linked to a Provider. This module is designed to work with a billing module for on-chain billing operations.

## Concepts

### Provider

A Provider represents an entity that offers services. Each Provider contains:

- **UUID**: Unique UUIDv7 identifier assigned automatically (deterministic for consensus)
- **Address**: The blockchain address of the provider
- **Payout Address**: The address where payments should be sent
- **API URL**: HTTPS endpoint where the provider's off-chain API is hosted (for tenant authentication)
- **Meta Hash**: A hash of off-chain metadata for extended information
- **Active**: Whether the provider is currently active

Providers can be deactivated (soft delete), which prevents creating new SKUs for that provider but allows existing SKUs and leases to continue operating.

### SKU

A SKU (Stock Keeping Unit) is a unique identifier for a billable item or service. Each SKU contains:

- **UUID**: Unique UUIDv7 identifier assigned automatically (deterministic for consensus)
- **Provider UUID**: Reference to the Provider offering this SKU
- **Name**: Human-readable name for the SKU
- **Unit**: The billing unit type (per hour or per day)
- **Base Price**: The base price for the SKU in a specific denomination
- **Meta Hash**: A hash of off-chain metadata for extended information
- **Active**: Whether the SKU is currently active

SKUs can be deactivated (soft delete), which prevents them from being used for new leases but allows existing leases to continue with their locked prices.

### Billing Units

The module supports the following billing unit types:

| Value | Name | Description | Seconds |
|-------|------|-------------|---------|
| 0 | `UNIT_UNSPECIFIED` | Default unspecified unit (invalid for SKUs) | N/A |
| 1 | `UNIT_PER_HOUR` | Per-hour billing | 3600 |
| 2 | `UNIT_PER_DAY` | Per-day billing | 86400 |

> **Note:** In JSON/REST responses, the unit is returned as a string (e.g., `"UNIT_PER_HOUR"`).
> Both string names and integer values are accepted when unmarshaling JSON.

### Pricing and Exact Divisibility

To ensure accurate billing calculations without rounding errors, the base price of an SKU must be **exactly divisible** by the number of seconds in the billing unit:

- **UNIT_PER_HOUR**: Price must be divisible by 3600 (e.g., 3600, 7200, 10800, ...)
- **UNIT_PER_DAY**: Price must be divisible by 86400 (e.g., 86400, 172800, 259200, ...)

This requirement ensures that per-second rate calculations (used by the billing module) are exact with no truncation or rounding.

**Examples:**
| Unit | Price | Per-Second Rate | Valid? |
|------|-------|-----------------|--------|
| UNIT_PER_HOUR | 3600upwr | 1 | ✅ Yes |
| UNIT_PER_HOUR | 7200upwr | 2 | ✅ Yes |
| UNIT_PER_HOUR | 3601upwr | 1.000277... | ❌ No (not evenly divisible) |
| UNIT_PER_DAY | 86400upwr | 1 | ✅ Yes |
| UNIT_PER_DAY | 172800upwr | 2 | ✅ Yes |
| UNIT_PER_DAY | 100000upwr | 1.157... | ❌ No (not evenly divisible) |

### Authorization

Provider and SKU operations (create, update, deactivate) can be performed by:

1. **Module Authority**: The governance address (typically `manifest10d07y265gmmuvt4z0w9aw880jnsr700jmq3jzm`)
2. **Allowed List**: Addresses explicitly added to the `allowed_list` parameter

Only the module authority can update the parameters (including the allowed list).

### Validation Constants

| Constant | Value | Description |
|----------|-------|-------------|
| `MaxSKUNameLength` | 256 | Maximum length of SKU name in characters |
| `MaxAPIURLLength` | 2048 | Maximum length of provider API URL in characters |

**API URL Requirements:**
- Must use HTTPS scheme (http:// is rejected)
- Must have a valid host (empty host is rejected)
- Must not contain user credentials (e.g., `https://user:pass@host` is rejected)
- Must not exceed `MaxAPIURLLength` (2048 characters)

**Note on MsgUpdateProvider**: If `api_url` is an empty string during an update, the existing API URL is preserved rather than being cleared. This allows updating other fields without accidentally removing the API URL.

### Security

**SKU Name Sanitization:** When SKU names are emitted in events or logs, they are sanitized to prevent log injection attacks. The original name is stored in state unchanged, but the sanitized version is used for event attributes and log messages. This protects against malicious SKU names containing control characters or log format strings.

### Business Rules

- SKUs can only be created for active Providers
- SKU base price must be exactly divisible by the billing unit's seconds (no rounding)
- Deactivating a Provider **cascades to deactivate all its SKUs** (one-way cascade)
- Deactivating a SKU is a soft delete - the SKU remains queryable but cannot be used for new leases
- Provider and SKU UUIDs are generated deterministically using UUIDv7 format and never reused

### Deactivation Impact on Existing Leases

When a Provider or SKU is deactivated, **existing active leases continue to operate normally**:

**Provider Deactivation:**
- Existing active leases using the provider's SKUs continue with locked-in prices
- The provider can still withdraw accrued funds from existing leases
- Tenants can still close their leases normally
- Only **new lease creation** is blocked for the deactivated provider's SKUs

**SKU Deactivation:**
- Existing active leases using the SKU continue with locked-in prices
- The SKU remains queryable for reporting and auditing
- Settlement and withdrawal continue normally
- Only **new lease creation** is blocked for the deactivated SKU

This soft-delete approach maintains billing integrity and allows graceful phase-out of services.

## State

### Storage Key Prefixes

| Prefix | Key Type | Description |
|--------|----------|-------------|
| `0x00` | Params | Module parameters |
| `0x01` | SKU | Primary SKU storage (UUID → SKU) |
| `0x02` | SKUSequence | Sequence counter for UUIDv7 generation |
| `0x03` | SKUByProvider | Index: provider UUID → SKU UUIDs |
| `0x04` | Provider | Primary provider storage (UUID → Provider) |
| `0x05` | ProviderSequence | Sequence counter for UUIDv7 generation |
| `0x06` | ProviderByAddress | Index: address → provider UUIDs |
| `0x07` | ProviderByActive | Index: active status → provider UUIDs |
| `0x08` | SKUByActive | Index: active status → SKU UUIDs |
| `0x09` | SKUByProviderActive | Compound index: provider+active → SKU UUIDs |

### Collections

| Collection | Key Type | Value Type | Description |
|------------|----------|------------|-------------|
| `Params` | - | `Params` | Module parameters including the allowed list |
| `Providers` | `string` (UUID) | `Provider` | Primary provider storage |
| `ProviderSequence` | - | `uint64` | Sequence for deterministic UUID generation |
| `SKUs` | `string` (UUID) | `SKU` | Primary SKU storage |
| `SKUSequence` | - | `uint64` | Sequence for deterministic UUID generation |

## Parameters

The module has the following configurable parameters:

| Parameter | Type | Description |
|-----------|------|-------------|
| `allowed_list` | `[]string` | List of addresses authorized to manage Providers and SKUs |

**Note:** The `allowed_list` must not contain duplicate addresses. Duplicate addresses will cause parameter validation to fail during `UpdateParams`.

## Messages

The SKU module supports the following transaction messages:

| Message | Description |
|---------|-------------|
| `MsgCreateProvider` | Create a new provider |
| `MsgUpdateProvider` | Update an existing provider |
| `MsgDeactivateProvider` | Deactivate a provider (soft delete) |
| `MsgCreateSKU` | Create a new SKU for an active provider |
| `MsgUpdateSKU` | Update an existing SKU |
| `MsgDeactivateSKU` | Deactivate a SKU (soft delete) |
| `MsgUpdateParams` | Update module parameters (authority only) |

For detailed message definitions, request/response formats, and CLI usage, see [API Reference](docs/API.md#cli-commands).

## Queries

| Query | Description |
|-------|-------------|
| Params | Get module parameters |
| Provider | Get a provider by UUID |
| ProviderByAddress | Get providers by management address |
| Providers | List all providers (supports `--active-only` filter) |
| SKU | Get a SKU by UUID |
| SKUs | List all SKUs (supports `--active-only` filter) |
| SKUsByProvider | List SKUs for a specific provider |

For detailed query documentation with response formats, see [API Reference](docs/API.md#query-commands).

**Events & Error Codes**: See [API Reference](docs/API.md#events) for the complete list of events and error codes.

## Genesis

The module's genesis state contains:

```protobuf
message GenesisState {
  Params params = 1;
  repeated Provider providers = 2;
  repeated SKU skus = 3;
}
```

Example genesis configuration:

```json
{
  "sku": {
    "params": {
      "allowed_list": ["manifest1abc..."]
    },
    "providers": [
      {
        "uuid": "01912345-6789-7abc-8def-0123456789ab",
        "address": "manifest1provider...",
        "payout_address": "manifest1payout...",
        "meta_hash": "",
        "active": true,
        "api_url": "https://api.provider.com"
      }
    ],
    "skus": [
      {
        "uuid": "01912345-6789-7abc-8def-0123456789cd",
        "provider_uuid": "01912345-6789-7abc-8def-0123456789ab",
        "name": "Compute Small",
        "unit": "UNIT_PER_HOUR",
        "base_price": {
          "denom": "upwr",
          "amount": "3600"
        },
        "meta_hash": "",
        "active": true
      }
    ]
  }
}
```

**Genesis Validation:**
- Provider and SKU UUIDs must be valid UUIDv7 format
- Each SKU must reference an existing provider UUID from the same genesis state
- Provider API URLs are validated if provided (must be HTTPS, no credentials)
- No duplicate provider or SKU UUIDs allowed

## Client

For complete CLI commands, gRPC endpoints, and REST API documentation, see [API Reference](docs/API.md).

**Quick examples:**
```bash
# Create provider and SKU
manifestd tx sku create-provider [address] [payout-address] --api-url https://api.example.com --from [key]
manifestd tx sku create-sku [provider-uuid] "Compute Small" 1 3600upwr --from [key]

# Query providers and SKUs
manifestd query sku providers --active-only
manifestd query sku skus-by-provider [provider-uuid]
```

## Additional Documentation

### User Guides
- [Provider Setup Guide](docs/PROVIDER_GUIDE.md) - Step-by-step guide to creating providers
- [SKU Setup Guide](docs/SKU_GUIDE.md) - Step-by-step guide to creating SKUs
- [API Reference](docs/API.md) - Complete CLI and gRPC/REST API reference
- [Troubleshooting](docs/TROUBLESHOOTING.md) - Common errors and solutions

### Developer Documentation
- [Architecture](docs/ARCHITECTURE.md) - Internal architecture, data models, and flow diagrams
- [Design Decisions](docs/DESIGN_DECISIONS.md) - Key design decisions and rationale

### Related Modules
- [Billing Module README](../billing/README.md) - Understanding the billing system
- [Migration Guide](../billing/docs/MIGRATION.md) - Migrating existing off-chain leases
