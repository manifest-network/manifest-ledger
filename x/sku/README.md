# x/sku

The SKU (Stock Keeping Unit) module provides on-chain management of providers and billing units for the Manifest Network.

## Overview

This module enables the creation, management, and querying of Providers and SKUs which represent service providers and their billable items. Each Provider has an address, payout address, and metadata. Each SKU contains pricing information and is linked to a Provider. This module is designed to work with a billing module for on-chain billing operations.

## Concepts

### Provider

A Provider represents an entity that offers services. Each Provider contains:

- **ID**: Unique identifier assigned automatically
- **Address**: The blockchain address of the provider
- **Payout Address**: The address where payments should be sent
- **Meta Hash**: A hash of off-chain metadata for extended information
- **Active**: Whether the provider is currently active

Providers can be deactivated (soft delete), which prevents creating new SKUs for that provider but allows existing SKUs and leases to continue operating.

### SKU

A SKU (Stock Keeping Unit) is a unique identifier for a billable item or service. Each SKU contains:

- **ID**: Unique identifier assigned automatically
- **Provider ID**: Reference to the Provider offering this SKU
- **Name**: Human-readable name for the SKU
- **Unit**: The billing unit type (per hour or per day)
- **Base Price**: The base price for the SKU in a specific denomination
- **Meta Hash**: A hash of off-chain metadata for extended information
- **Active**: Whether the SKU is currently active

SKUs can be deactivated (soft delete), which prevents them from being used for new leases but allows existing leases to continue with their locked prices.

### Billing Units

The module supports the following billing unit types:

| Value | Name | Description |
|-------|------|-------------|
| 0 | `UNIT_UNSPECIFIED` | Default unspecified unit (invalid for SKUs) |
| 1 | `UNIT_PER_HOUR` | Per-hour billing |
| 2 | `UNIT_PER_DAY` | Per-day billing |

> **Note:** In JSON/REST responses, the unit is returned as a string (e.g., `"UNIT_PER_HOUR"`).
> Both string names and integer values are accepted when unmarshaling JSON.

### Authorization

Provider and SKU operations (create, update, deactivate) can be performed by:

1. **Module Authority**: The governance address (typically `manifest10d07y265gmmuvt4z0w9aw880jnsr700jmq3jzm`)
2. **Allowed List**: Addresses explicitly added to the `allowed_list` parameter

Only the module authority can update the parameters (including the allowed list).

### Business Rules

- SKUs can only be created for active Providers
- Deactivating a Provider does not affect existing SKUs (they remain active/inactive as they were)
- Deactivating a SKU is a soft delete - the SKU remains queryable but cannot be used for new leases
- Provider and SKU IDs are auto-incremented and never reused

## State

The module stores the following state:

| Key | Description |
|-----|-------------|
| `Params` | Module parameters including the allowed list |
| `Providers` | A map of Provider ID to Provider data |
| `NextProviderID` | A sequence tracking the next available Provider ID |
| `SKUs` | A map of SKU ID to SKU data with a secondary index on provider_id |
| `NextSKUID` | A sequence tracking the next available SKU ID |

## Parameters

The module has the following configurable parameters:

| Parameter | Type | Description |
|-----------|------|-------------|
| `allowed_list` | `[]string` | List of addresses authorized to manage Providers and SKUs |

## Messages

### MsgCreateProvider

Creates a new Provider. Can be executed by the module authority or addresses in the allowed list.

```protobuf
message MsgCreateProvider {
  string authority = 1;
  string address = 2;
  string payout_address = 3;
  bytes meta_hash = 4;
}
```

**CLI Example:**

```bash
manifestd tx sku create-provider manifest1... manifest1... \
  --meta-hash deadbeef \
  --from mykey \
  --chain-id manifest-1
```

### MsgUpdateProvider

Updates an existing Provider. Can be executed by the module authority or addresses in the allowed list.

```protobuf
message MsgUpdateProvider {
  string authority = 1;
  uint64 id = 2;
  string address = 3;
  string payout_address = 4;
  bytes meta_hash = 5;
  bool active = 6;
}
```

**CLI Example:**

```bash
manifestd tx sku update-provider 1 manifest1... manifest1... true \
  --meta-hash cafebabe \
  --from mykey \
  --chain-id manifest-1
```

### MsgDeactivateProvider

Deactivates an existing Provider (soft delete). The Provider remains in state but is marked as inactive.
Inactive Providers cannot have new SKUs created for them.
Can be executed by the module authority or addresses in the allowed list.

```protobuf
message MsgDeactivateProvider {
  string authority = 1;
  uint64 id = 2;
}
```

**CLI Example:**

```bash
manifestd tx sku deactivate-provider 1 \
  --from mykey \
  --chain-id manifest-1
```

### MsgCreateSKU

Creates a new SKU for an active Provider. Can be executed by the module authority or addresses in the allowed list.

```protobuf
message MsgCreateSKU {
  string authority = 1;
  uint64 provider_id = 2;
  string name = 3;
  Unit unit = 4;
  cosmos.base.v1beta1.Coin base_price = 5;
  bytes meta_hash = 6;
}
```

**CLI Example:**

```bash
manifestd tx sku create-sku 1 "Compute Small" 1 100umfx \
  --meta-hash deadbeef \
  --from mykey \
  --chain-id manifest-1
```

### MsgUpdateSKU

Updates an existing SKU. Can be executed by the module authority or addresses in the allowed list.

```protobuf
message MsgUpdateSKU {
  string authority = 1;
  uint64 id = 2;
  uint64 provider_id = 3;
  string name = 4;
  Unit unit = 5;
  cosmos.base.v1beta1.Coin base_price = 6;
  bytes meta_hash = 7;
  bool active = 8;
}
```

**CLI Example:**

```bash
manifestd tx sku update-sku 1 1 "Compute Medium" 2 200umfx true \
  --meta-hash cafebabe \
  --from mykey \
  --chain-id manifest-1
```

### MsgDeactivateSKU

Deactivates an existing SKU (soft delete). The SKU remains in state but is marked as inactive.
Inactive SKUs cannot be used for new leases but existing leases continue with their locked prices.
Can be executed by the module authority or addresses in the allowed list.

```protobuf
message MsgDeactivateSKU {
  string authority = 1;
  uint64 id = 2;
}
```

**CLI Example:**

```bash
manifestd tx sku deactivate-sku 1 \
  --from mykey \
  --chain-id manifest-1
```

### MsgUpdateParams

Updates the module parameters. Only the module authority can execute this message.

```protobuf
message MsgUpdateParams {
  string authority = 1;
  Params params = 2;
}
```

**CLI Example:**

```bash
# Add addresses to the allowed list
manifestd tx sku update-params \
  --allowed-list "manifest1abc...,manifest1def..." \
  --from authority \
  --chain-id manifest-1

# Clear the allowed list
manifestd tx sku update-params \
  --allowed-list "" \
  --from authority \
  --chain-id manifest-1
```

## Queries

### Params

Query the module parameters.

```bash
manifestd query sku params
```

### Provider

Query a specific Provider by ID.

```bash
manifestd query sku provider [id]
```

### Providers

Query all Providers with pagination.

```bash
manifestd query sku providers

# With pagination
manifestd query sku providers --limit 10 --page-key "AAAAAAAAAAM="

# Filter to return only active Providers
manifestd query sku providers --active-only
```

### SKU

Query a specific SKU by ID.

```bash
manifestd query sku sku [id]
```

### SKUs

Query all SKUs with pagination.

```bash
manifestd query sku skus

# With pagination (limit and offset)
manifestd query sku skus --limit 10 --offset 0

# With pagination (using page key from previous response)
manifestd query sku skus --limit 10 --page-key "AAAAAAAAAAM="

# Filter to return only active SKUs
manifestd query sku skus --active-only
```

### SKUsByProvider

Query all SKUs for a specific Provider with pagination.

```bash
manifestd query sku skus-by-provider [provider_id]

# With pagination
manifestd query sku skus-by-provider 1 --limit 10

# With page key from previous response
manifestd query sku skus-by-provider 1 --limit 10 --page-key "AAAAAAAAAAM="

# Filter to return only active SKUs
manifestd query sku skus-by-provider 1 --active-only
```

## Events

The module emits the following events:

| Event Type | Attributes | Description |
|------------|------------|-------------|
| `provider_created` | `provider_id`, `address`, `payout_address` | Emitted when a Provider is created |
| `provider_updated` | `provider_id` | Emitted when a Provider is updated |
| `provider_activated` | `provider_id` | Emitted when a Provider is re-activated via update |
| `provider_deactivated` | `provider_id` | Emitted when a Provider is deactivated |
| `sku_created` | `sku_id`, `provider_id`, `name` | Emitted when a SKU is created |
| `sku_updated` | `sku_id`, `provider_id` | Emitted when a SKU is updated |
| `sku_activated` | `sku_id`, `provider_id` | Emitted when a SKU is re-activated via update |
| `sku_deactivated` | `sku_id`, `provider_id` | Emitted when a SKU is deactivated |
| `params_updated` | - | Emitted when module parameters are updated |

## Genesis

The module's genesis state contains:

```protobuf
message GenesisState {
  Params params = 1;
  repeated Provider providers = 2;
  uint64 next_provider_id = 3;
  repeated SKU skus = 4;
  uint64 next_sku_id = 5;
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
        "id": "1",
        "address": "manifest1provider...",
        "payout_address": "manifest1payout...",
        "meta_hash": "",
        "active": true
      }
    ],
    "next_provider_id": "2",
    "skus": [
      {
        "id": "1",
        "provider_id": "1",
        "name": "Compute Small",
        "unit": "UNIT_PER_HOUR",
        "base_price": {
          "denom": "umfx",
          "amount": "100"
        },
        "meta_hash": "",
        "active": true
      }
    ],
    "next_sku_id": "2"
  }
}
```

## Client

### CLI

The module provides CLI commands for both queries and transactions:

**Query Commands:**
- `manifestd query sku params` - Query module parameters
- `manifestd query sku provider [id]` - Query a specific Provider
- `manifestd query sku providers` - Query all Providers
- `manifestd query sku sku [id]` - Query a specific SKU
- `manifestd query sku skus` - Query all SKUs
- `manifestd query sku skus-by-provider [provider_id]` - Query SKUs by Provider

**Transaction Commands:**
- `manifestd tx sku create-provider` - Create a new Provider
- `manifestd tx sku update-provider` - Update an existing Provider
- `manifestd tx sku deactivate-provider` - Deactivate a Provider (soft delete)
- `manifestd tx sku create-sku` - Create a new SKU
- `manifestd tx sku update-sku` - Update an existing SKU
- `manifestd tx sku deactivate-sku` - Deactivate a SKU (soft delete)
- `manifestd tx sku update-params` - Update module parameters

### gRPC

The module exposes gRPC endpoints for all queries:

- `liftedinit.sku.v1.Query/Params`
- `liftedinit.sku.v1.Query/Provider`
- `liftedinit.sku.v1.Query/Providers`
- `liftedinit.sku.v1.Query/SKU`
- `liftedinit.sku.v1.Query/SKUs`
- `liftedinit.sku.v1.Query/SKUsByProvider`

### REST

REST endpoints are available through the gRPC gateway:

- `GET /liftedinit/sku/v1/params`
- `GET /liftedinit/sku/v1/provider/{id}`
- `GET /liftedinit/sku/v1/providers`
- `GET /liftedinit/sku/v1/sku/{id}`
- `GET /liftedinit/sku/v1/skus`
- `GET /liftedinit/sku/v1/skus/provider/{provider_id}`
