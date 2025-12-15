# SKU Module API Reference

This document provides a comprehensive API reference for the SKU module, covering both CLI commands and gRPC/REST endpoints.

## Table of Contents

- [CLI Commands](#cli-commands)
  - [Transaction Commands](#transaction-commands)
  - [Query Commands](#query-commands)
- [gRPC API](#grpc-api)
  - [Msg Service](#msg-service)
  - [Query Service](#query-service)
- [REST API](#rest-api)

---

## CLI Commands

### Transaction Commands

#### create-provider

Create a new provider with management and payout addresses.

```bash
manifestd tx sku create-provider [address] [payout-address] [flags]
```

**Arguments:**
| Argument | Type | Description |
|----------|------|-------------|
| address | string | Bech32 address of the provider (management address) |
| payout-address | string | Bech32 address where payments will be sent |

**Flags:**
| Flag | Type | Description |
|------|------|-------------|
| --meta-hash | string | Hex-encoded hash of off-chain metadata (optional) |

**Example:**
```bash
manifestd tx sku create-provider manifest1provider... manifest1payout... \
  --meta-hash deadbeef \
  --from authority
```

---

#### update-provider

Update an existing provider.

```bash
manifestd tx sku update-provider [id] [address] [payout-address] [active] [flags]
```

**Arguments:**
| Argument | Type | Description |
|----------|------|-------------|
| id | uint64 | Provider ID |
| address | string | New management address |
| payout-address | string | New payout address |
| active | bool | Whether the provider is active (true/false) |

**Flags:**
| Flag | Type | Description |
|------|------|-------------|
| --meta-hash | string | Hex-encoded hash of off-chain metadata (optional) |

**Example:**
```bash
manifestd tx sku update-provider 1 manifest1provider... manifest1payout... true \
  --meta-hash cafebabe \
  --from authority
```

---

#### deactivate-provider

Deactivate a provider (soft delete). The provider remains in state but is marked inactive. Inactive providers cannot have new SKUs created for them.

```bash
manifestd tx sku deactivate-provider [id] [flags]
```

**Arguments:**
| Argument | Type | Description |
|----------|------|-------------|
| id | uint64 | Provider ID to deactivate |

**Example:**
```bash
manifestd tx sku deactivate-provider 1 --from authority
```

---

#### create-sku

Create a new SKU for an active provider.

```bash
manifestd tx sku create-sku [provider-id] [name] [unit] [base-price] [flags]
```

**Arguments:**
| Argument | Type | Description |
|----------|------|-------------|
| provider-id | uint64 | ID of the provider this SKU belongs to |
| name | string | Human-readable name for the SKU |
| unit | int | Billing unit: 1 = per hour, 2 = per day |
| base-price | coin | Base price (e.g., `3600umfx` for 1/second rate per hour) |

**Flags:**
| Flag | Type | Description |
|------|------|-------------|
| --meta-hash | string | Hex-encoded hash of off-chain metadata (optional) |

**Example:**
```bash
manifestd tx sku create-sku 1 "Compute Instance Small" 1 3600000umfx \
  --meta-hash deadbeef \
  --from authority
```

**Note:** The base price must be exactly divisible by the billing unit's seconds:
- UNIT_PER_HOUR (1): Price must be divisible by 3600
- UNIT_PER_DAY (2): Price must be divisible by 86400

---

#### update-sku

Update an existing SKU.

```bash
manifestd tx sku update-sku [id] [provider-id] [name] [unit] [base-price] [active] [flags]
```

**Arguments:**
| Argument | Type | Description |
|----------|------|-------------|
| id | uint64 | SKU ID |
| provider-id | uint64 | Provider ID |
| name | string | SKU name |
| unit | int | Billing unit: 1 = per hour, 2 = per day |
| base-price | coin | Base price |
| active | bool | Whether the SKU is active (true/false) |

**Flags:**
| Flag | Type | Description |
|------|------|-------------|
| --meta-hash | string | Hex-encoded hash of off-chain metadata (optional) |

**Example:**
```bash
manifestd tx sku update-sku 1 1 "Compute Instance Medium" 1 7200000umfx true \
  --meta-hash cafebabe \
  --from authority
```

---

#### deactivate-sku

Deactivate a SKU (soft delete). The SKU remains in state but is marked inactive. Inactive SKUs cannot be used for new leases but existing leases continue with their locked prices.

```bash
manifestd tx sku deactivate-sku [id] [flags]
```

**Arguments:**
| Argument | Type | Description |
|----------|------|-------------|
| id | uint64 | SKU ID to deactivate |

**Example:**
```bash
manifestd tx sku deactivate-sku 1 --from authority
```

---

#### update-params

Update the module parameters (authority only).

```bash
manifestd tx sku update-params [flags]
```

**Flags:**
| Flag | Type | Description |
|------|------|-------------|
| --allowed-list | string | Comma-separated list of addresses allowed to manage SKUs |

**Example:**
```bash
# Add addresses to the allowed list
manifestd tx sku update-params \
  --allowed-list "manifest1abc...,manifest1def..." \
  --from authority

# Clear the allowed list
manifestd tx sku update-params \
  --allowed-list "" \
  --from authority
```

---

### Query Commands

#### params

Query module parameters.

```bash
manifestd query sku params
```

**Response:**
```json
{
  "params": {
    "allowed_list": ["manifest1abc..."]
  }
}
```

---

#### provider

Query a provider by ID.

```bash
manifestd query sku provider [id]
```

**Arguments:**
| Argument | Type | Description |
|----------|------|-------------|
| id | uint64 | Provider ID |

**Response:**
```json
{
  "provider": {
    "id": "1",
    "address": "manifest1provider...",
    "payout_address": "manifest1payout...",
    "meta_hash": "",
    "active": true
  }
}
```

---

#### providers

Query all providers with pagination.

```bash
manifestd query sku providers [flags]
```

**Flags:**
| Flag | Type | Description |
|------|------|-------------|
| --active-only | bool | Filter to return only active providers |
| --limit | uint64 | Pagination limit |
| --page-key | string | Pagination key from previous response |

**Example:**
```bash
manifestd query sku providers --active-only --limit 10
```

**Response:**
```json
{
  "providers": [
    {
      "id": "1",
      "address": "manifest1provider...",
      "payout_address": "manifest1payout...",
      "meta_hash": "",
      "active": true
    }
  ],
  "pagination": {
    "next_key": null,
    "total": "1"
  }
}
```

---

#### sku

Query a SKU by ID.

```bash
manifestd query sku sku [id]
```

**Arguments:**
| Argument | Type | Description |
|----------|------|-------------|
| id | uint64 | SKU ID |

**Response:**
```json
{
  "sku": {
    "id": "1",
    "provider_id": "1",
    "name": "Compute Instance Small",
    "unit": "UNIT_PER_HOUR",
    "base_price": {
      "denom": "umfx",
      "amount": "3600000"
    },
    "meta_hash": "",
    "active": true
  }
}
```

---

#### skus

Query all SKUs with pagination.

```bash
manifestd query sku skus [flags]
```

**Flags:**
| Flag | Type | Description |
|------|------|-------------|
| --active-only | bool | Filter to return only active SKUs |
| --limit | uint64 | Pagination limit |
| --page-key | string | Pagination key from previous response |

**Example:**
```bash
manifestd query sku skus --active-only --limit 10
```

---

#### skus-by-provider

Query all SKUs for a specific provider.

```bash
manifestd query sku skus-by-provider [provider-id] [flags]
```

**Arguments:**
| Argument | Type | Description |
|----------|------|-------------|
| provider-id | uint64 | Provider ID |

**Flags:**
| Flag | Type | Description |
|------|------|-------------|
| --active-only | bool | Filter to return only active SKUs |
| --limit | uint64 | Pagination limit |
| --page-key | string | Pagination key from previous response |

**Example:**
```bash
manifestd query sku skus-by-provider 1 --active-only
```

---

## gRPC API

### Msg Service

The Msg service handles all state-changing operations.

**Service Definition:**
```protobuf
service Msg {
  rpc CreateProvider(MsgCreateProvider) returns (MsgCreateProviderResponse);
  rpc UpdateProvider(MsgUpdateProvider) returns (MsgUpdateProviderResponse);
  rpc DeactivateProvider(MsgDeactivateProvider) returns (MsgDeactivateProviderResponse);
  rpc CreateSKU(MsgCreateSKU) returns (MsgCreateSKUResponse);
  rpc UpdateSKU(MsgUpdateSKU) returns (MsgUpdateSKUResponse);
  rpc DeactivateSKU(MsgDeactivateSKU) returns (MsgDeactivateSKUResponse);
  rpc UpdateParams(MsgUpdateParams) returns (MsgUpdateParamsResponse);
}
```

#### MsgCreateProvider

Create a new provider.

**Request:**
```protobuf
message MsgCreateProvider {
  string authority = 1;       // Authority or allowed address
  string address = 2;         // Provider's management address
  string payout_address = 3;  // Provider's payout address
  bytes meta_hash = 4;        // Off-chain metadata hash
}
```

**Response:**
```protobuf
message MsgCreateProviderResponse {
  uint64 id = 1;  // Created provider ID
}
```

---

#### MsgUpdateProvider

Update an existing provider.

**Request:**
```protobuf
message MsgUpdateProvider {
  string authority = 1;       // Authority or allowed address
  uint64 id = 2;              // Provider ID
  string address = 3;         // New management address
  string payout_address = 4;  // New payout address
  bytes meta_hash = 5;        // New metadata hash
  bool active = 6;            // Active status
}
```

**Response:**
```protobuf
message MsgUpdateProviderResponse {}
```

---

#### MsgDeactivateProvider

Deactivate a provider (soft delete).

**Request:**
```protobuf
message MsgDeactivateProvider {
  string authority = 1;  // Authority or allowed address
  uint64 id = 2;         // Provider ID
}
```

**Response:**
```protobuf
message MsgDeactivateProviderResponse {}
```

---

#### MsgCreateSKU

Create a new SKU.

**Request:**
```protobuf
message MsgCreateSKU {
  string authority = 1;                    // Authority or allowed address
  uint64 provider_id = 2;                  // Provider ID
  string name = 3;                         // SKU name
  Unit unit = 4;                           // Billing unit
  cosmos.base.v1beta1.Coin base_price = 5; // Base price
  bytes meta_hash = 6;                     // Off-chain metadata hash
}
```

**Response:**
```protobuf
message MsgCreateSKUResponse {
  uint64 id = 1;  // Created SKU ID
}
```

---

#### MsgUpdateSKU

Update an existing SKU.

**Request:**
```protobuf
message MsgUpdateSKU {
  string authority = 1;                    // Authority or allowed address
  uint64 id = 2;                           // SKU ID
  uint64 provider_id = 3;                  // Provider ID
  string name = 4;                         // SKU name
  Unit unit = 5;                           // Billing unit
  cosmos.base.v1beta1.Coin base_price = 6; // Base price
  bytes meta_hash = 7;                     // Metadata hash
  bool active = 8;                         // Active status
}
```

**Response:**
```protobuf
message MsgUpdateSKUResponse {}
```

---

#### MsgDeactivateSKU

Deactivate a SKU (soft delete).

**Request:**
```protobuf
message MsgDeactivateSKU {
  string authority = 1;  // Authority or allowed address
  uint64 id = 2;         // SKU ID
}
```

**Response:**
```protobuf
message MsgDeactivateSKUResponse {}
```

---

#### MsgUpdateParams

Update module parameters (authority only).

**Request:**
```protobuf
message MsgUpdateParams {
  string authority = 1;  // Module authority
  Params params = 2;     // New parameters
}
```

**Response:**
```protobuf
message MsgUpdateParamsResponse {}
```

---

### Query Service

The Query service provides read-only access to state.

**Service Definition:**
```protobuf
service Query {
  rpc Params(QueryParamsRequest) returns (QueryParamsResponse);
  rpc Provider(QueryProviderRequest) returns (QueryProviderResponse);
  rpc Providers(QueryProvidersRequest) returns (QueryProvidersResponse);
  rpc SKU(QuerySKURequest) returns (QuerySKUResponse);
  rpc SKUs(QuerySKUsRequest) returns (QuerySKUsResponse);
  rpc SKUsByProvider(QuerySKUsByProviderRequest) returns (QuerySKUsByProviderResponse);
}
```

#### QueryParams

Get module parameters.

**Endpoint:** `liftedinit.sku.v1.Query/Params`

**Request:** Empty

**Response:**
```protobuf
message QueryParamsResponse {
  Params params = 1;
}
```

---

#### QueryProvider

Get a provider by ID.

**Endpoint:** `liftedinit.sku.v1.Query/Provider`

**Request:**
```protobuf
message QueryProviderRequest {
  uint64 id = 1;
}
```

**Response:**
```protobuf
message QueryProviderResponse {
  Provider provider = 1;
}
```

---

#### QueryProviders

List all providers with pagination.

**Endpoint:** `liftedinit.sku.v1.Query/Providers`

**Request:**
```protobuf
message QueryProvidersRequest {
  cosmos.base.query.v1beta1.PageRequest pagination = 1;
  bool active_only = 2;
}
```

**Response:**
```protobuf
message QueryProvidersResponse {
  repeated Provider providers = 1;
  cosmos.base.query.v1beta1.PageResponse pagination = 2;
}
```

---

#### QuerySKU

Get a SKU by ID.

**Endpoint:** `liftedinit.sku.v1.Query/SKU`

**Request:**
```protobuf
message QuerySKURequest {
  uint64 id = 1;
}
```

**Response:**
```protobuf
message QuerySKUResponse {
  SKU sku = 1;
}
```

---

#### QuerySKUs

List all SKUs with pagination.

**Endpoint:** `liftedinit.sku.v1.Query/SKUs`

**Request:**
```protobuf
message QuerySKUsRequest {
  cosmos.base.query.v1beta1.PageRequest pagination = 1;
  bool active_only = 2;
}
```

**Response:**
```protobuf
message QuerySKUsResponse {
  repeated SKU skus = 1;
  cosmos.base.query.v1beta1.PageResponse pagination = 2;
}
```

---

#### QuerySKUsByProvider

List SKUs for a specific provider.

**Endpoint:** `liftedinit.sku.v1.Query/SKUsByProvider`

**Request:**
```protobuf
message QuerySKUsByProviderRequest {
  uint64 provider_id = 1;
  cosmos.base.query.v1beta1.PageRequest pagination = 2;
  bool active_only = 3;
}
```

**Response:**
```protobuf
message QuerySKUsByProviderResponse {
  repeated SKU skus = 1;
  cosmos.base.query.v1beta1.PageResponse pagination = 2;
}
```

---

## REST API

REST endpoints are available via gRPC-gateway.

### Base URL

```
http://localhost:1317/liftedinit/sku/v1
```

### Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/params` | Get module parameters |
| GET | `/provider/{id}` | Get provider by ID |
| GET | `/providers` | List all providers |
| GET | `/sku/{id}` | Get SKU by ID |
| GET | `/skus` | List all SKUs |
| GET | `/skus/provider/{provider_id}` | List SKUs by provider |

### Examples

**Get Parameters:**
```bash
curl http://localhost:1317/liftedinit/sku/v1/params
```

**Get Provider:**
```bash
curl http://localhost:1317/liftedinit/sku/v1/provider/1
```

**List Active Providers:**
```bash
curl "http://localhost:1317/liftedinit/sku/v1/providers?active_only=true&pagination.limit=10"
```

**Get SKU:**
```bash
curl http://localhost:1317/liftedinit/sku/v1/sku/1
```

**List Active SKUs:**
```bash
curl "http://localhost:1317/liftedinit/sku/v1/skus?active_only=true&pagination.limit=10"
```

**List SKUs by Provider:**
```bash
curl "http://localhost:1317/liftedinit/sku/v1/skus/provider/1?active_only=true"
```

---

## Data Types

### Provider

```protobuf
message Provider {
  uint64 id = 1;              // Unique identifier
  string address = 2;         // Management address
  string payout_address = 3;  // Payout address
  bytes meta_hash = 4;        // Off-chain metadata hash
  bool active = 5;            // Active status
}
```

### SKU

```protobuf
message SKU {
  uint64 id = 1;                           // Unique identifier
  uint64 provider_id = 2;                  // Provider ID
  string name = 3;                         // Human-readable name
  Unit unit = 4;                           // Billing unit
  cosmos.base.v1beta1.Coin base_price = 5; // Base price
  bytes meta_hash = 6;                     // Off-chain metadata hash
  bool active = 7;                         // Active status
}
```

### Unit

```protobuf
enum Unit {
  UNIT_UNSPECIFIED = 0;  // Invalid
  UNIT_PER_HOUR = 1;     // Per-hour billing (3600 seconds)
  UNIT_PER_DAY = 2;      // Per-day billing (86400 seconds)
}
```

### Params

```protobuf
message Params {
  repeated string allowed_list = 1;  // Addresses allowed to manage SKUs
}
```

---

## Error Codes

| Error | Code | Description |
|-------|------|-------------|
| `ErrProviderNotFound` | 2 | Provider doesn't exist |
| `ErrProviderNotActive` | 3 | Provider is deactivated |
| `ErrSKUNotFound` | 4 | SKU doesn't exist |
| `ErrSKUNotActive` | 5 | SKU is deactivated |
| `ErrUnauthorized` | 6 | Sender not authorized |
| `ErrInvalidProvider` | 7 | Invalid provider parameters |
| `ErrInvalidSKU` | 8 | Invalid SKU parameters |
| `ErrInvalidUnit` | 9 | Invalid billing unit |
| `ErrInvalidPrice` | 10 | Price not divisible by unit seconds |

---

## Authorization

| Operation | Authority | Allowed List |
|-----------|-----------|--------------|
| CreateProvider | ✓ | ✓ |
| UpdateProvider | ✓ | ✓ |
| DeactivateProvider | ✓ | ✓ |
| CreateSKU | ✓ | ✓ |
| UpdateSKU | ✓ | ✓ |
| DeactivateSKU | ✓ | ✓ |
| UpdateParams | ✓ | ✗ |

---

## Related Documentation

- [Provider Setup Guide](PROVIDER_GUIDE.md) - Step-by-step guide to creating providers
- [SKU Setup Guide](SKU_GUIDE.md) - Step-by-step guide to creating SKUs
- [Architecture](ARCHITECTURE.md) - Internal architecture and data models
- [Design Decisions](DESIGN_DECISIONS.md) - Key design decisions and rationale
- [Billing Module](../../billing/README.md) - Understanding the billing system
