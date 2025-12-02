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

- `UNIT_UNSPECIFIED`: Default unspecified unit (invalid for SKUs)
- `UNIT_PER_HOUR`: Per-hour billing
- `UNIT_PER_DAY`: Per-day billing
- `UNIT_PER_MONTH`: Per-month billing
- `UNIT_PER_UNIT`: Per-unit billing (one-time charges)

## State

The module stores the following state:

- **SKUs**: A map of SKU ID to SKU data
- **NextID**: A sequence tracking the next available SKU ID

## Messages

### MsgCreateSKU

Creates a new SKU. Only the module authority can create SKUs.

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

### MsgUpdateSKU

Updates an existing SKU. Only the module authority can update SKUs.

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

### MsgDeleteSKU

Deletes an existing SKU. Only the module authority can delete SKUs.

```protobuf
message MsgDeleteSKU {
  string authority = 1;
  string provider = 2;
  uint64 id = 3;
}
```

## Queries

### SKU

Query a specific SKU by ID.

```bash
manifestd query sku sku [id]
```

### SKUs

Query all SKUs with pagination.

```bash
manifestd query sku skus
```

### SKUsByProvider

Query all SKUs for a specific provider.

```bash
manifestd query sku skus-by-provider [provider]
```

## Genesis

The module's genesis state contains:

- **skus**: List of existing SKUs
- **next_id**: The next SKU ID to be assigned

Example genesis configuration:

```json
{
  "sku": {
    "skus": [],
    "next_id": "1"
  }
}
```

## Events

The module emits events for SKU operations through standard Cosmos SDK logging.

## Parameters

The module currently has no configurable parameters.

## Client

### CLI

The module provides CLI commands for querying SKUs. Transaction commands are available through the autocli interface.

### gRPC

The module exposes gRPC endpoints for all queries:

- `liftedinit.sku.v1.Query/SKU`
- `liftedinit.sku.v1.Query/SKUs`
- `liftedinit.sku.v1.Query/SKUsByProvider`

### REST

REST endpoints are available through the gRPC gateway:

- `GET /liftedinit/sku/v1/sku/{id}`
- `GET /liftedinit/sku/v1/skus`
- `GET /liftedinit/sku/v1/skus/{provider}`
