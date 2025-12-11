# Billing Module API Reference

This document provides a comprehensive API reference for the billing module, covering both CLI commands and gRPC/REST endpoints.

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

#### fund-credit

Fund a tenant's credit account with billing tokens.

```bash
manifestd tx billing fund-credit [tenant] [amount] [flags]
```

**Arguments:**
| Argument | Type | Description |
|----------|------|-------------|
| tenant | string | Bech32 address of the tenant |
| amount | coin | Amount to fund (e.g., `1000000000upwr`) |

**Example:**
```bash
manifestd tx billing fund-credit manifest1abc... 1000000000factory/manifest1.../upwr --from mykey
```

---

#### create-lease

Create a new lease for the sender.

```bash
manifestd tx billing create-lease [sku_id:quantity...] [flags]
```

**Arguments:**
| Argument | Type | Description |
|----------|------|-------------|
| items | string... | Space-separated list of `sku_id:quantity` pairs |

**Example:**
```bash
manifestd tx billing create-lease 1:2 2:1 3:5 --from mykey
```

---

#### create-lease-for-tenant

Create a lease on behalf of a tenant (authority/allowed addresses only).

```bash
manifestd tx billing create-lease-for-tenant [tenant] [sku_id:quantity...] [flags]
```

**Arguments:**
| Argument | Type | Description |
|----------|------|-------------|
| tenant | string | Bech32 address of the tenant |
| items | string... | Space-separated list of `sku_id:quantity` pairs |

**Example:**
```bash
manifestd tx billing create-lease-for-tenant manifest1abc... 1:2 2:1 --from authority
```

---

#### close-lease

Close an active lease.

```bash
manifestd tx billing close-lease [lease-id] [flags]
```

**Arguments:**
| Argument | Type | Description |
|----------|------|-------------|
| lease-id | uint64 | ID of the lease to close |

**Example:**
```bash
manifestd tx billing close-lease 1 --from mykey
```

---

#### withdraw

Withdraw accrued funds from a specific lease.

```bash
manifestd tx billing withdraw [lease-id] [flags]
```

**Arguments:**
| Argument | Type | Description |
|----------|------|-------------|
| lease-id | uint64 | ID of the lease to withdraw from |

**Example:**
```bash
manifestd tx billing withdraw 1 --from provider-key
```

---

#### withdraw-all

Withdraw accrued funds from all leases for a provider.

```bash
manifestd tx billing withdraw-all [provider-id] [flags]
```

**Arguments:**
| Argument | Type | Description |
|----------|------|-------------|
| provider-id | uint64 | ID of the provider |

**Flags:**
| Flag | Type | Default | Description |
|------|------|---------|-------------|
| --limit | uint64 | 50 | Maximum leases to process (max 100) |

**Example:**
```bash
manifestd tx billing withdraw-all 1 --limit 100 --from provider-key
```

---

#### update-params

Update module parameters (authority only).

```bash
manifestd tx billing update-params [flags]
```

**Flags:**
| Flag | Type | Description |
|------|------|-------------|
| --denom | string | Billing denomination |
| --min-credit-balance | string | Minimum credit balance |
| --max-leases-per-tenant | uint64 | Max active leases per tenant |
| --max-items-per-lease | uint64 | Max items per lease |
| --allowed-list | string | Comma-separated allowed addresses |

**Example:**
```bash
manifestd tx billing update-params \
  --denom "factory/manifest1.../upwr" \
  --min-credit-balance 5000000 \
  --max-leases-per-tenant 100 \
  --max-items-per-lease 20 \
  --allowed-list "manifest1abc...,manifest1def..." \
  --from authority
```

---

### Query Commands

#### params

Query module parameters.

```bash
manifestd query billing params
```

**Response:**
```json
{
  "params": {
    "denom": "factory/manifest1.../upwr",
    "min_credit_balance": "5000000",
    "max_leases_per_tenant": "100",
    "max_items_per_lease": "20",
    "allowed_list": []
  }
}
```

---

#### lease

Query a lease by ID.

```bash
manifestd query billing lease [lease-id]
```

**Arguments:**
| Argument | Type | Description |
|----------|------|-------------|
| lease-id | uint64 | ID of the lease |

**Response:**
```json
{
  "lease": {
    "id": "1",
    "tenant": "manifest1abc...",
    "provider_id": "1",
    "items": [
      {
        "sku_id": "1",
        "quantity": "2",
        "locked_price": "100"
      }
    ],
    "state": "LEASE_STATE_ACTIVE",
    "created_at": "2024-01-01T00:00:00Z",
    "closed_at": null,
    "last_settled_at": "2024-01-01T00:00:00Z"
  }
}
```

---

#### leases

Query all leases with pagination.

```bash
manifestd query billing leases [flags]
```

**Flags:**
| Flag | Type | Description |
|------|------|-------------|
| --active-only | bool | Filter to active leases only |
| --limit | uint64 | Pagination limit |
| --page-key | string | Pagination key |

**Example:**
```bash
manifestd query billing leases --active-only --limit 10
```

---

#### leases-by-tenant

Query leases for a specific tenant.

```bash
manifestd query billing leases-by-tenant [tenant] [flags]
```

**Arguments:**
| Argument | Type | Description |
|----------|------|-------------|
| tenant | string | Bech32 address of the tenant |

**Flags:**
| Flag | Type | Description |
|------|------|-------------|
| --active-only | bool | Filter to active leases only |

---

#### leases-by-provider

Query leases for a specific provider.

```bash
manifestd query billing leases-by-provider [provider-id] [flags]
```

**Arguments:**
| Argument | Type | Description |
|----------|------|-------------|
| provider-id | uint64 | ID of the provider |

**Flags:**
| Flag | Type | Description |
|------|------|-------------|
| --active-only | bool | Filter to active leases only |

---

#### credit-account

Query a tenant's credit account.

```bash
manifestd query billing credit-account [tenant]
```

**Arguments:**
| Argument | Type | Description |
|----------|------|-------------|
| tenant | string | Bech32 address of the tenant |

**Response:**
```json
{
  "credit_account": {
    "tenant": "manifest1abc...",
    "credit_address": "manifest1xyz...",
    "active_lease_count": "2"
  },
  "balance": {
    "denom": "factory/manifest1.../upwr",
    "amount": "1000000000"
  }
}
```

---

#### credit-address

Derive the credit address for a tenant (doesn't require existing account).

```bash
manifestd query billing credit-address [tenant]
```

**Arguments:**
| Argument | Type | Description |
|----------|------|-------------|
| tenant | string | Bech32 address of the tenant |

**Response:**
```json
{
  "credit_address": "manifest1xyz..."
}
```

---

#### withdrawable

Query withdrawable amount for a lease.

```bash
manifestd query billing withdrawable [lease-id]
```

**Arguments:**
| Argument | Type | Description |
|----------|------|-------------|
| lease-id | uint64 | ID of the lease |

**Response:**
```json
{
  "amount": {
    "denom": "factory/manifest1.../upwr",
    "amount": "500000"
  },
  "payout_address": "manifest1provider..."
}
```

---

#### provider-withdrawable

Query total withdrawable for a provider across all leases.

```bash
manifestd query billing provider-withdrawable [provider-id]
```

**Arguments:**
| Argument | Type | Description |
|----------|------|-------------|
| provider-id | uint64 | ID of the provider |

**Response:**
```json
{
  "amount": {
    "denom": "factory/manifest1.../upwr",
    "amount": "5000000"
  },
  "lease_count": "10",
  "payout_address": "manifest1provider..."
}
```

---

## gRPC API

### Msg Service

The Msg service handles all state-changing operations.

**Service Definition:**
```protobuf
service Msg {
  rpc FundCredit(MsgFundCredit) returns (MsgFundCreditResponse);
  rpc CreateLease(MsgCreateLease) returns (MsgCreateLeaseResponse);
  rpc CreateLeaseForTenant(MsgCreateLeaseForTenant) returns (MsgCreateLeaseForTenantResponse);
  rpc CloseLease(MsgCloseLease) returns (MsgCloseLeaseResponse);
  rpc Withdraw(MsgWithdraw) returns (MsgWithdrawResponse);
  rpc WithdrawAll(MsgWithdrawAll) returns (MsgWithdrawAllResponse);
  rpc UpdateParams(MsgUpdateParams) returns (MsgUpdateParamsResponse);
}
```

#### MsgFundCredit

Fund a tenant's credit account.

**Request:**
```protobuf
message MsgFundCredit {
  string sender = 1;   // Sender's address
  string tenant = 2;   // Tenant's address
  cosmos.base.v1beta1.Coin amount = 3;  // Amount to fund
}
```

**Response:**
```protobuf
message MsgFundCreditResponse {
  string credit_address = 1;  // Credit account address
  string new_balance = 2;     // New credit balance
}
```

---

#### MsgCreateLease

Create a lease for the sender.

**Request:**
```protobuf
message MsgCreateLease {
  string tenant = 1;  // Tenant (must be signer)
  repeated LeaseItemInput items = 2;  // SKU items
}

message LeaseItemInput {
  uint64 sku_id = 1;
  uint64 quantity = 2;
}
```

**Response:**
```protobuf
message MsgCreateLeaseResponse {
  uint64 lease_id = 1;  // Created lease ID
}
```

---

#### MsgCreateLeaseForTenant

Create a lease on behalf of a tenant (authority/allowed only).

**Request:**
```protobuf
message MsgCreateLeaseForTenant {
  string authority = 1;  // Authority or allowed address
  string tenant = 2;     // Tenant's address
  repeated LeaseItemInput items = 3;  // SKU items
}
```

**Response:**
```protobuf
message MsgCreateLeaseForTenantResponse {
  uint64 lease_id = 1;  // Created lease ID
}
```

---

#### MsgCloseLease

Close an active lease.

**Request:**
```protobuf
message MsgCloseLease {
  string sender = 1;    // Sender (tenant, provider, or authority)
  uint64 lease_id = 2;  // Lease to close
}
```

**Response:**
```protobuf
message MsgCloseLeaseResponse {
  cosmos.base.v1beta1.Coin settled_amount = 1;  // Final settled amount
}
```

---

#### MsgWithdraw

Withdraw from a specific lease.

**Request:**
```protobuf
message MsgWithdraw {
  string sender = 1;    // Provider or authority
  uint64 lease_id = 2;  // Lease ID
}
```

**Response:**
```protobuf
message MsgWithdrawResponse {
  cosmos.base.v1beta1.Coin amount = 1;  // Withdrawn amount
  string payout_address = 2;  // Destination address
}
```

---

#### MsgWithdrawAll

Withdraw from all provider leases.

**Request:**
```protobuf
message MsgWithdrawAll {
  string sender = 1;       // Provider or authority
  uint64 provider_id = 2;  // Provider ID
  uint64 limit = 3;        // Max leases (default 50, max 100)
}
```

**Response:**
```protobuf
message MsgWithdrawAllResponse {
  cosmos.base.v1beta1.Coin total_amount = 1;  // Total withdrawn
  uint64 lease_count = 2;   // Leases processed
  string payout_address = 3;  // Destination address
  bool has_more = 4;        // More leases remain
}
```

---

### Query Service

The Query service provides read-only access to state.

**Service Definition:**
```protobuf
service Query {
  rpc Params(QueryParamsRequest) returns (QueryParamsResponse);
  rpc Lease(QueryLeaseRequest) returns (QueryLeaseResponse);
  rpc Leases(QueryLeasesRequest) returns (QueryLeasesResponse);
  rpc LeasesByTenant(QueryLeasesByTenantRequest) returns (QueryLeasesByTenantResponse);
  rpc LeasesByProvider(QueryLeasesByProviderRequest) returns (QueryLeasesByProviderResponse);
  rpc CreditAccount(QueryCreditAccountRequest) returns (QueryCreditAccountResponse);
  rpc CreditAddress(QueryCreditAddressRequest) returns (QueryCreditAddressResponse);
  rpc WithdrawableAmount(QueryWithdrawableAmountRequest) returns (QueryWithdrawableAmountResponse);
  rpc ProviderWithdrawable(QueryProviderWithdrawableRequest) returns (QueryProviderWithdrawableResponse);
}
```

#### QueryParams

Get module parameters.

**Endpoint:** `liftedinit.billing.v1.Query/Params`

**Request:** Empty

**Response:**
```protobuf
message QueryParamsResponse {
  Params params = 1;
}
```

---

#### QueryLease

Get a lease by ID. **Note:** This may trigger auto-close if credit is exhausted.

**Endpoint:** `liftedinit.billing.v1.Query/Lease`

**Request:**
```protobuf
message QueryLeaseRequest {
  uint64 lease_id = 1;
}
```

**Response:**
```protobuf
message QueryLeaseResponse {
  Lease lease = 1;
}
```

---

#### QueryLeases

List all leases with pagination.

**Endpoint:** `liftedinit.billing.v1.Query/Leases`

**Request:**
```protobuf
message QueryLeasesRequest {
  cosmos.base.query.v1beta1.PageRequest pagination = 1;
  bool active_only = 2;
}
```

**Response:**
```protobuf
message QueryLeasesResponse {
  repeated Lease leases = 1;
  cosmos.base.query.v1beta1.PageResponse pagination = 2;
}
```

---

#### QueryCreditAccount

Get a tenant's credit account with balance.

**Endpoint:** `liftedinit.billing.v1.Query/CreditAccount`

**Request:**
```protobuf
message QueryCreditAccountRequest {
  string tenant = 1;
}
```

**Response:**
```protobuf
message QueryCreditAccountResponse {
  CreditAccount credit_account = 1;
  cosmos.base.v1beta1.Coin balance = 2;
}
```

---

#### QueryCreditAddress

Derive credit address without requiring existing account.

**Endpoint:** `liftedinit.billing.v1.Query/CreditAddress`

**Request:**
```protobuf
message QueryCreditAddressRequest {
  string tenant = 1;
}
```

**Response:**
```protobuf
message QueryCreditAddressResponse {
  string credit_address = 1;
}
```

---

## REST API

REST endpoints are available via gRPC-gateway.

### Base URL

```
http://localhost:1317/liftedinit/billing/v1
```

### Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/params` | Get module parameters |
| GET | `/lease/{lease_id}` | Get lease by ID |
| GET | `/leases` | List all leases |
| GET | `/leases/tenant/{tenant}` | List leases by tenant |
| GET | `/leases/provider/{provider_id}` | List leases by provider |
| GET | `/credit-account/{tenant}` | Get credit account |
| GET | `/credit-address/{tenant}` | Derive credit address |
| GET | `/withdrawable/{lease_id}` | Get withdrawable amount |
| GET | `/provider-withdrawable/{provider_id}` | Get provider total withdrawable |

### Examples

**Get Parameters:**
```bash
curl http://localhost:1317/liftedinit/billing/v1/params
```

**Get Lease:**
```bash
curl http://localhost:1317/liftedinit/billing/v1/lease/1
```

**List Active Leases:**
```bash
curl "http://localhost:1317/liftedinit/billing/v1/leases?active_only=true&pagination.limit=10"
```

**Get Credit Account:**
```bash
curl http://localhost:1317/liftedinit/billing/v1/credit-account/manifest1abc...
```

**Get Withdrawable Amount:**
```bash
curl http://localhost:1317/liftedinit/billing/v1/withdrawable/1
```

---

## Error Codes

| Code | Description |
|------|-------------|
| `ErrInvalidLease` | Invalid lease parameters |
| `ErrLeaseNotFound` | Lease doesn't exist |
| `ErrLeaseNotActive` | Lease is already closed |
| `ErrUnauthorized` | Sender not authorized |
| `ErrInsufficientCredit` | Not enough credit balance |
| `ErrCreditAccountNotFound` | Credit account doesn't exist |
| `ErrInvalidCreditOperation` | Invalid credit operation |
| `ErrSKUNotFound` | SKU doesn't exist |
| `ErrSKUNotActive` | SKU is deactivated |
| `ErrProviderNotActive` | Provider is deactivated |
| `ErrNoWithdrawableAmount` | Nothing to withdraw |
| `ErrOverflow` | Arithmetic overflow |

---

## Related Documentation

- [Provider Setup Guide](../sku/PROVIDER_GUIDE.md) - Creating and managing providers
- [SKU Setup Guide](../sku/SKU_GUIDE.md) - Creating and managing SKUs
- [Billing README](README.md) - Complete billing module overview
- [Migration Guide](MIGRATION.md) - Migrating existing off-chain leases
- [Troubleshooting Guide](TROUBLESHOOTING.md) - Common issues and solutions
