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
- [Data Types](#data-types)
- [Events](#events)
- [Error Codes](#error-codes)
- [Authorization](#authorization)

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
| --api-url | string | HTTPS endpoint for provider's off-chain API (optional) |
| --meta-hash | string | Hex-encoded hash of off-chain metadata (optional) |

**Example:**
```bash
manifestd tx sku create-provider manifest1provider... manifest1payout... \
  --api-url https://api.provider.com \
  --meta-hash deadbeef \
  --from authority
```

---

#### update-provider

Update an existing provider.

```bash
manifestd tx sku update-provider [uuid] [address] [payout-address] [active] [flags]
```

**Arguments:**
| Argument | Type | Description |
|----------|------|-------------|
| uuid | string | Provider UUID (UUIDv7 format) |
| address | string | New management address |
| payout-address | string | New payout address |
| active | bool | Whether the provider is active (true/false) |

**Flags:**
| Flag | Type | Description |
|------|------|-------------|
| --api-url | string | HTTPS endpoint for provider's off-chain API (optional) |
| --meta-hash | string | Hex-encoded hash of off-chain metadata (optional) |

**Example:**
```bash
manifestd tx sku update-provider 01912345-6789-7abc-8def-0123456789ab manifest1provider... manifest1payout... true \
  --api-url https://api.provider.com \
  --meta-hash cafebabe \
  --from authority
```

**Note:** If `--api-url` is omitted (empty string), the existing API URL is preserved. This allows updating other fields without accidentally clearing the API URL.

---

#### deactivate-provider

Deactivate a provider (soft delete). The provider remains in state but is marked inactive. Inactive providers cannot have new SKUs created for them.

```bash
manifestd tx sku deactivate-provider [uuid] [flags]
```

**Arguments:**
| Argument | Type | Description |
|----------|------|-------------|
| uuid | string | Provider UUID to deactivate |

**Example:**
```bash
manifestd tx sku deactivate-provider 01912345-6789-7abc-8def-0123456789ab --from authority
```

---

#### create-sku

Create a new SKU for an active provider.

```bash
manifestd tx sku create-sku [provider-uuid] [name] [unit] [base-price] [flags]
```

**Arguments:**
| Argument | Type | Description |
|----------|------|-------------|
| provider-uuid | string | UUID of the provider this SKU belongs to |
| name | string | Human-readable name for the SKU |
| unit | int | Billing unit: 1 = per hour, 2 = per day |
| base-price | coin | Base price (e.g., `3600upwr` for 1/second rate per hour) |

**Flags:**
| Flag | Type | Description |
|------|------|-------------|
| --meta-hash | string | Hex-encoded hash of off-chain metadata (optional) |

**Example:**
```bash
manifestd tx sku create-sku 01912345-6789-7abc-8def-0123456789ab "Compute Instance Small" 1 3600000upwr \
  --meta-hash deadbeef \
  --from authority
```

**Price Validation:**

The base price must be exactly divisible by the billing unit's seconds. See [Pricing and Exact Divisibility](../README.md#pricing-and-exact-divisibility) for requirements and examples.

**Error Messages:**
- If price is not divisible: `invalid sku: price X is not evenly divisible by unit seconds Y`
- If price results in zero per-second rate: `invalid sku: price per second would be zero`

---

#### update-sku

Update an existing SKU.

```bash
manifestd tx sku update-sku [uuid] [provider-uuid] [name] [unit] [base-price] [active] [flags]
```

**Arguments:**
| Argument | Type | Description |
|----------|------|-------------|
| uuid | string | SKU UUID |
| provider-uuid | string | Provider UUID |
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
manifestd tx sku update-sku 01912345-6789-7abc-8def-0123456789ab 01912345-6789-7abc-8def-0123456789ab "Compute Instance Medium" 1 7200000upwr true \
  --meta-hash cafebabe \
  --from authority
```

---

#### deactivate-sku

Deactivate a SKU (soft delete). The SKU remains in state but is marked inactive. Inactive SKUs cannot be used for new leases but existing leases continue with their locked prices.

```bash
manifestd tx sku deactivate-sku [uuid] [flags]
```

**Arguments:**
| Argument | Type | Description |
|----------|------|-------------|
| uuid | string | SKU UUID to deactivate |

**Example:**
```bash
manifestd tx sku deactivate-sku 01912345-6789-7abc-8def-0123456789ab --from authority
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

Query a provider by UUID.

```bash
manifestd query sku provider [uuid]
```

**Arguments:**
| Argument | Type | Description |
|----------|------|-------------|
| uuid | string | Provider UUID |

**Response:**
```json
{
  "provider": {
    "uuid": "01912345-6789-7abc-8def-0123456789ab",
    "address": "manifest1provider...",
    "payout_address": "manifest1payout...",
    "api_url": "https://api.provider.com",
    "meta_hash": "",
    "active": true
  }
}
```

---

#### provider-by-address

Query all providers with a given management address.

```bash
manifestd query sku provider-by-address [address] [flags]
```

**Arguments:**
| Argument | Type | Description |
|----------|------|-------------|
| address | string | Provider's management address (Bech32) |

**Flags:**
| Flag | Type | Description |
|------|------|-------------|
| --active-only | bool | Filter to return only active providers |
| --limit | uint64 | Pagination limit |
| --page-key | string | Pagination key from previous response |

**Example:**
```bash
manifestd query sku provider-by-address manifest1abc... --active-only --limit 10
```

**Response:**
```json
{
  "providers": [
    {
      "uuid": "01912345-6789-7abc-8def-0123456789ab",
      "address": "manifest1provider...",
      "payout_address": "manifest1payout...",
      "api_url": "https://api.provider.com",
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

**Note:** A single address can manage multiple providers. This query returns all providers associated with the given address, using an efficient address index.

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
      "uuid": "01912345-6789-7abc-8def-0123456789ab",
      "address": "manifest1provider...",
      "payout_address": "manifest1payout...",
      "api_url": "https://api.provider.com",
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

Query a SKU by UUID.

```bash
manifestd query sku sku [uuid]
```

**Arguments:**
| Argument | Type | Description |
|----------|------|-------------|
| uuid | string | SKU UUID |

**Response:**
```json
{
  "sku": {
    "uuid": "01912345-6789-7abc-8def-0123456789cd",
    "provider_uuid": "01912345-6789-7abc-8def-0123456789ab",
    "name": "Compute Instance Small",
    "unit": "UNIT_PER_HOUR",
    "base_price": {
      "denom": "upwr",
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
manifestd query sku skus-by-provider [provider-uuid] [flags]
```

**Arguments:**
| Argument | Type | Description |
|----------|------|-------------|
| provider-uuid | string | Provider UUID |

**Flags:**
| Flag | Type | Description |
|------|------|-------------|
| --active-only | bool | Filter to return only active SKUs |
| --limit | uint64 | Pagination limit |
| --page-key | string | Pagination key from previous response |

**Example:**
```bash
manifestd query sku skus-by-provider 01912345-6789-7abc-8def-0123456789ab --active-only
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
  string api_url = 5;         // HTTPS endpoint for off-chain API
}
```

**Response:**
```protobuf
message MsgCreateProviderResponse {
  string uuid = 1;  // Created provider UUID
}
```

---

#### MsgUpdateProvider

Update an existing provider.

**Request:**
```protobuf
message MsgUpdateProvider {
  string authority = 1;       // Authority or allowed address
  string uuid = 2;            // Provider UUID
  string address = 3;         // New management address
  string payout_address = 4;  // New payout address
  bytes meta_hash = 5;        // New metadata hash
  bool active = 6;            // Active status
  string api_url = 7;         // HTTPS endpoint for off-chain API
}
```

**Response:**
```protobuf
message MsgUpdateProviderResponse {}
```

**Note:** If `api_url` is an empty string, the existing API URL is preserved rather than being cleared. This allows updating other fields without modifying the API URL.

---

#### MsgDeactivateProvider

Deactivate a provider (soft delete).

**Request:**
```protobuf
message MsgDeactivateProvider {
  string authority = 1;  // Authority or allowed address
  string uuid = 2;       // Provider UUID
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
  string provider_uuid = 2;                // Provider UUID
  string name = 3;                         // SKU name
  Unit unit = 4;                           // Billing unit
  cosmos.base.v1beta1.Coin base_price = 5; // Base price
  bytes meta_hash = 6;                     // Off-chain metadata hash
}
```

**Response:**
```protobuf
message MsgCreateSKUResponse {
  string uuid = 1;  // Created SKU UUID
}
```

---

#### MsgUpdateSKU

Update an existing SKU.

**Request:**
```protobuf
message MsgUpdateSKU {
  string authority = 1;                    // Authority or allowed address
  string uuid = 2;                         // SKU UUID
  string provider_uuid = 3;                // Provider UUID
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
  string uuid = 2;       // SKU UUID
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
  rpc ProviderByAddress(QueryProviderByAddressRequest) returns (QueryProviderByAddressResponse);
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

Get a provider by UUID.

**Endpoint:** `liftedinit.sku.v1.Query/Provider`

**Request:**
```protobuf
message QueryProviderRequest {
  string uuid = 1;
}
```

**Response:**
```protobuf
message QueryProviderResponse {
  Provider provider = 1;
}
```

---

#### QueryProviderByAddress

Get all providers with a given management address.

**Endpoint:** `liftedinit.sku.v1.Query/ProviderByAddress`

**Request:**
```protobuf
message QueryProviderByAddressRequest {
  string address = 1;  // Provider's management address
  cosmos.base.query.v1beta1.PageRequest pagination = 2;
  bool active_only = 3;
}
```

**Response:**
```protobuf
message QueryProviderByAddressResponse {
  repeated Provider providers = 1;
  cosmos.base.query.v1beta1.PageResponse pagination = 2;
}
```

**Note:** A single address can manage multiple providers. This query returns all providers associated with the given address, using an efficient address index.

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

Get a SKU by UUID.

**Endpoint:** `liftedinit.sku.v1.Query/SKU`

**Request:**
```protobuf
message QuerySKURequest {
  string uuid = 1;
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
  string provider_uuid = 1;
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
| GET | `/provider/{uuid}` | Get provider by UUID |
| GET | `/provider/address/{address}` | Get providers by management address |
| GET | `/providers` | List all providers |
| GET | `/sku/{uuid}` | Get SKU by UUID |
| GET | `/skus` | List all SKUs |
| GET | `/skus/provider/{provider_uuid}` | List SKUs by provider |

### Examples

**Get Parameters:**
```bash
curl http://localhost:1317/liftedinit/sku/v1/params
```

**Get Provider:**
```bash
curl http://localhost:1317/liftedinit/sku/v1/provider/01912345-6789-7abc-8def-0123456789ab
```

**Get Providers by Address:**
```bash
curl http://localhost:1317/liftedinit/sku/v1/provider/address/manifest1provider...
```

**List Active Providers:**
```bash
curl "http://localhost:1317/liftedinit/sku/v1/providers?active_only=true&pagination.limit=10"
```

**Get SKU:**
```bash
curl http://localhost:1317/liftedinit/sku/v1/sku/01912345-6789-7abc-8def-0123456789cd
```

**List Active SKUs:**
```bash
curl "http://localhost:1317/liftedinit/sku/v1/skus?active_only=true&pagination.limit=10"
```

**List SKUs by Provider:**
```bash
curl "http://localhost:1317/liftedinit/sku/v1/skus/provider/01912345-6789-7abc-8def-0123456789ab?active_only=true"
```

---

## Data Types

### Provider

```protobuf
message Provider {
  string uuid = 1;            // Unique UUIDv7 identifier
  string address = 2;         // Management address
  string payout_address = 3;  // Payout address
  string api_url = 4;         // HTTPS endpoint for off-chain API
  bytes meta_hash = 5;        // Off-chain metadata hash (max 64 bytes)
  bool active = 6;            // Active status
}
```

**Field Notes:**
- `meta_hash`: Optional hash or reference linking to off-chain metadata (e.g., provider description, terms of service, contact info). Maximum 64 bytes to accommodate SHA-256 or SHA-512 hashes. This value is mutable and can be updated via `MsgUpdateProvider`.

### SKU

```protobuf
message SKU {
  string uuid = 1;                         // Unique UUIDv7 identifier
  string provider_uuid = 2;                // Provider UUID
  string name = 3;                         // Human-readable name
  Unit unit = 4;                           // Billing unit
  cosmos.base.v1beta1.Coin base_price = 5; // Base price
  bytes meta_hash = 6;                     // Off-chain metadata hash (max 64 bytes)
  bool active = 7;                         // Active status
}
```

**Field Notes:**
- `meta_hash`: Optional hash or reference linking to off-chain metadata (e.g., detailed specifications, SLA terms, resource configurations). Maximum 64 bytes to accommodate SHA-256 or SHA-512 hashes. This value is mutable and can be updated via `MsgUpdateSKU`.

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

## Events

The SKU module emits the following events for state changes:

| Event | Attributes | Description |
|-------|------------|-------------|
| `provider_created` | provider_uuid, address, payout_address, created_by | Provider created |
| `provider_updated` | provider_uuid | Provider updated |
| `provider_activated` | provider_uuid | Provider transitioned from inactive → active |
| `provider_deactivated` | provider_uuid, deactivated_by | Provider deactivated |
| `sku_created` | sku_uuid, provider_uuid, name, base_price, created_by | SKU created |
| `sku_updated` | sku_uuid, provider_uuid | SKU updated |
| `sku_activated` | sku_uuid, provider_uuid | SKU transitioned from inactive → active |
| `sku_deactivated` | sku_uuid, provider_uuid, deactivated_by | SKU deactivated |
| `params_updated` | | Module parameters updated |

### Event Attribute Sanitization

SKU names are sanitized before being emitted in events to prevent log injection attacks. The original name is stored in state unchanged, but the sanitized version appears in event attributes.

### Querying Events

Events can be queried from transaction results:

```bash
# Query events for a specific transaction
manifestd query tx [txhash] --output json | jq '.events'

# Example: Extract provider_uuid from a provider creation
manifestd query tx [txhash] --output json | jq -r '.logs[0].events[] | select(.type=="provider_created") | .attributes[] | select(.key=="provider_uuid") | .value'
```

---

## Error Codes

| Error | Code | Description |
|-------|------|-------------|
| `ErrInvalidSKU` | 1 | Invalid SKU parameters (includes inactive check) |
| `ErrSKUNotFound` | 2 | SKU doesn't exist |
| `ErrUnauthorized` | 3 | Sender not authorized |
| `ErrInvalidConfig` | 4 | Invalid module configuration |
| `ErrInvalidProvider` | 5 | Invalid provider parameters (includes inactive check) |
| `ErrProviderNotFound` | 6 | Provider doesn't exist |
| `ErrInvalidAPIURL` | 7 | Invalid API URL (not HTTPS, too long, contains credentials, etc.) |

**Note:** Active status checks (e.g., "provider is not active", "SKU is not active") are reported via `ErrInvalidProvider` or `ErrInvalidSKU` respectively.

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
