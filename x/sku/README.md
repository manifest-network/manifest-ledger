# x/sku

The SKU (Stock Keeping Unit) module provides on-chain management of billing units for the Manifest Network.

## Overview

This module enables the creation, management, and querying of SKUs which represent billable items or services. Each SKU contains pricing information and metadata that can be used for on-chain billing operations.

## Concepts

### SKU

A SKU (Stock Keeping Unit) is a unique identifier for a billable item or service. Each SKU contains:

- **ID**: Unique identifier assigned automatically
- **Provider**: The address or identifier of the entity providing the service
- **Name**: Human-readable name for the SKU
- **Unit**: The billing unit type (per hour, per day, per month, or per unit)
- **Base Price**: The base price for the SKU in a specific denomination
- **Meta Hash**: A hash of off-chain metadata for extended information
- **Active**: Whether the SKU is currently active

### Billing Units

The module supports the following billing unit types:

| Value | Name | Description |
|-------|------|-------------|
| 0 | `UNIT_UNSPECIFIED` | Default unspecified unit (invalid for SKUs) |
| 1 | `UNIT_PER_HOUR` | Per-hour billing |
| 2 | `UNIT_PER_DAY` | Per-day billing |
| 3 | `UNIT_PER_MONTH` | Per-month billing |
| 4 | `UNIT_PER_UNIT` | Per-unit billing (one-time charges) |

### Authorization

SKU operations (create, update, delete) can be performed by:

1. **Module Authority**: The governance address (typically `manifest10d07y265gmmuvt4z0w9aw880jnsr700jmq3jzm`)
2. **Allowed List**: Addresses explicitly added to the `allowed_list` parameter

Only the module authority can update the parameters (including the allowed list).

## State

The module stores the following state:

| Key | Description |
|-----|-------------|
| `Params` | Module parameters including the allowed list |
| `SKUs` | A map of SKU ID to SKU data with a secondary index on provider |
| `NextID` | A sequence tracking the next available SKU ID |

## Parameters

The module has the following configurable parameters:

| Parameter | Type | Description |
|-----------|------|-------------|
| `allowed_list` | `[]string` | List of addresses authorized to manage SKUs |

## Messages

### MsgCreateSKU

Creates a new SKU. Can be executed by the module authority or addresses in the allowed list.

```protobuf
message MsgCreateSKU {
  string authority = 1;
  string provider = 2;
  string name = 3;
  Unit unit = 4;
  cosmos.base.v1beta1.Coin base_price = 5;
  bytes meta_hash = 6;
}
```

**CLI Example:**

```bash
manifestd tx sku create-sku "provider1" "Compute Small" 1 100umfx \
  --meta-hash deadbeef \
  --from mykey \
  --chain-id manifest-1
```

### MsgUpdateSKU

Updates an existing SKU. Can be executed by the module authority or addresses in the allowed list.

```protobuf
message MsgUpdateSKU {
  string authority = 1;
  string provider = 2;
  uint64 id = 3;
  string name = 4;
  Unit unit = 5;
  cosmos.base.v1beta1.Coin base_price = 6;
  bytes meta_hash = 7;
  bool active = 8;
}
```

**CLI Example:**

```bash
manifestd tx sku update-sku "provider1" 1 "Compute Medium" 2 200umfx true \
  --meta-hash cafebabe \
  --from mykey \
  --chain-id manifest-1
```

### MsgDeleteSKU

Deletes an existing SKU. Can be executed by the module authority or addresses in the allowed list.

```protobuf
message MsgDeleteSKU {
  string authority = 1;
  string provider = 2;
  uint64 id = 3;
}
```

**CLI Example:**

```bash
manifestd tx sku delete-sku "provider1" 1 \
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

### SKU

Query a specific SKU by ID.

```bash
manifestd query sku sku [id]
```

### SKUs

Query all SKUs with pagination.

```bash
manifestd query sku skus

# With pagination
manifestd query sku skus --limit 10 --offset 0
```

### SKUsByProvider

Query all SKUs for a specific provider with pagination.

```bash
manifestd query sku skus-by-provider [provider]

# With pagination
manifestd query sku skus-by-provider "provider1" --limit 10
```

## Events

The module emits the following events:

| Event Type | Attributes | Description |
|------------|------------|-------------|
| `sku_created` | `sku_id`, `provider`, `name` | Emitted when a SKU is created |
| `sku_updated` | `sku_id`, `provider` | Emitted when a SKU is updated |
| `sku_deleted` | `sku_id`, `provider` | Emitted when a SKU is deleted |
| `params_updated` | - | Emitted when module parameters are updated |

## Genesis

The module's genesis state contains:

```protobuf
message GenesisState {
  Params params = 1;
  repeated SKU skus = 2;
  uint64 next_id = 3;
}
```

Example genesis configuration:

```json
{
  "sku": {
    "params": {
      "allowed_list": ["manifest1abc..."]
    },
    "skus": [
      {
        "id": "1",
        "provider": "provider1",
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
    "next_id": "2"
  }
}
```

## Client

### CLI

The module provides CLI commands for both queries and transactions:

**Query Commands:**
- `manifestd query sku params` - Query module parameters
- `manifestd query sku sku [id]` - Query a specific SKU
- `manifestd query sku skus` - Query all SKUs
- `manifestd query sku skus-by-provider [provider]` - Query SKUs by provider

**Transaction Commands:**
- `manifestd tx sku create-sku` - Create a new SKU
- `manifestd tx sku update-sku` - Update an existing SKU
- `manifestd tx sku delete-sku` - Delete a SKU
- `manifestd tx sku update-params` - Update module parameters

### gRPC

The module exposes gRPC endpoints for all queries:

- `liftedinit.sku.v1.Query/Params`
- `liftedinit.sku.v1.Query/SKU`
- `liftedinit.sku.v1.Query/SKUs`
- `liftedinit.sku.v1.Query/SKUsByProvider`

### REST

REST endpoints are available through the gRPC gateway:

- `GET /liftedinit/sku/v1/params`
- `GET /liftedinit/sku/v1/sku/{id}`
- `GET /liftedinit/sku/v1/skus`
- `GET /liftedinit/sku/v1/skus/{provider}`
