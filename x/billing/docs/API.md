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
- [Data Types](#data-types)
- [Events](#events)
- [Error Codes](#error-codes)
- [Authorization](#authorization)

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
| amount | coin | Amount to fund (e.g., `1000000upwr`) |

**Example:**
```bash
manifestd tx billing fund-credit manifest1abc... 1000000upwr --from mykey
```

**Notes:**
- Anyone can fund any tenant's credit account
- Credit accounts support multiple denominations
- The denomination funded must match what the tenant needs for their target SKUs
- Creates the credit account if it doesn't exist

---

#### create-lease

Create a new lease for the sender (tenant). The lease starts in PENDING state.

```bash
manifestd tx billing create-lease [sku-uuid:quantity...] [flags]
```

**Arguments:**
| Argument | Type | Description |
|----------|------|-------------|
| items | string... | Space-separated list of `sku-uuid:quantity` pairs |

**Example:**
```bash
manifestd tx billing create-lease 01912345-6789-7abc-8def-0123456789ab:2 01912345-6789-7abc-8def-0123456789ac:1 --from mykey
```

**Constraints:**
- Sender must have funded credit account
- Credit must cover `min_lease_duration` seconds for each denom used by the SKUs
- All SKUs must be from the same provider
- All SKUs must be active
- Cannot exceed `max_items_per_lease`
- Cannot exceed `max_leases_per_tenant`
- Cannot exceed `max_pending_leases_per_tenant`

**Notes:**
- Lease starts in PENDING state awaiting provider acknowledgement
- Credit is locked but billing does not start until acknowledgement
- Returns the lease UUID on success

---

#### create-lease-for-tenant

Create a lease on behalf of a tenant (authority/allowed addresses only). The lease starts in PENDING state.

```bash
manifestd tx billing create-lease-for-tenant [tenant] [sku-uuid:quantity...] [flags]
```

**Arguments:**
| Argument | Type | Description |
|----------|------|-------------|
| tenant | string | Bech32 address of the tenant |
| items | string... | Space-separated list of `sku-uuid:quantity` pairs |

**Example:**
```bash
manifestd tx billing create-lease-for-tenant manifest1abc... 01912345-6789-7abc-8def-0123456789ab:2 --from authority
```

**Authorization:** Only module authority or addresses in `allowed_list` param.

---

#### acknowledge-lease

Acknowledge one or more PENDING leases atomically (provider only). Transitions leases to ACTIVE and starts billing.

```bash
manifestd tx billing acknowledge-lease [lease-uuid]... [flags]
```

**Arguments:**
| Argument | Type | Description |
|----------|------|-------------|
| lease-uuid | string (repeated) | UUIDs of leases to acknowledge (1-100) |

**Examples:**
```bash
# Single lease
manifestd tx billing acknowledge-lease 01912345-6789-7abc-8def-0123456789ab --from provider-key

# Multiple leases
manifestd tx billing acknowledge-lease uuid1 uuid2 uuid3 --from provider-key
```

**Authorization:** Provider address or authority.

**Notes:**
- Only PENDING leases can be acknowledged
- All leases must belong to the same provider
- Maximum 100 leases per transaction
- Atomic operation: all succeed or all fail
- Billing starts from the acknowledgement timestamp
- Emits `lease_acknowledged` event for each lease
- Emits `batch_acknowledged` event when multiple leases are processed (includes lease_count, provider_uuid, acknowledged_by)

---

#### reject-lease

Reject a PENDING lease (provider only). Credit is unlocked and returned to tenant.

```bash
manifestd tx billing reject-lease [lease-uuid] [flags]
```

**Arguments:**
| Argument | Type | Description |
|----------|------|-------------|
| lease-uuid | string | UUID of the lease to reject |

**Flags:**
| Flag | Type | Description |
|------|------|-------------|
| --reason | string | Optional rejection reason (max 256 chars) |

**Example:**
```bash
manifestd tx billing reject-lease 01912345-6789-7abc-8def-0123456789ab --reason "Resources unavailable" --from provider-key
```

**Authorization:** Provider address or authority.

---

#### cancel-lease

Cancel a PENDING lease (tenant only). Credit is unlocked and returned.

```bash
manifestd tx billing cancel-lease [lease-uuid] [flags]
```

**Arguments:**
| Argument | Type | Description |
|----------|------|-------------|
| lease-uuid | string | UUID of the lease to cancel |

**Example:**
```bash
manifestd tx billing cancel-lease 01912345-6789-7abc-8def-0123456789ab --from tenant-key
```

**Authorization:** Tenant (owner) only.

**Notes:**
- Only PENDING leases can be cancelled
- Credit is immediately unlocked

---

#### close-lease

Close an ACTIVE lease.

```bash
manifestd tx billing close-lease [lease-uuid] [flags]
```

**Arguments:**
| Argument | Type | Description |
|----------|------|-------------|
| lease-uuid | string | UUID of the lease to close |

**Example:**
```bash
manifestd tx billing close-lease 01912345-6789-7abc-8def-0123456789ab --from mykey
```

**Authorization:** Tenant (owner), provider (of SKUs), or authority.

**Notes:**
- Only ACTIVE leases can be closed
- Performs final settlement during closure
- Transfers accrued amount to provider payout address
- Sets lease state to CLOSED

---

#### withdraw

Withdraw accrued funds from a specific lease.

```bash
manifestd tx billing withdraw [lease-uuid] [flags]
```

**Arguments:**
| Argument | Type | Description |
|----------|------|-------------|
| lease-uuid | string | UUID of the lease to withdraw from |

**Example:**
```bash
manifestd tx billing withdraw 01912345-6789-7abc-8def-0123456789ab --from provider-key
```

**Authorization:** Provider (of SKUs) or authority.

**Notes:**
- Settles accrued amount since last settlement
- Transfers to provider's payout address
- May trigger auto-close if credit exhausted

---

#### withdraw-all

Withdraw accrued funds from all leases for a provider.

```bash
manifestd tx billing withdraw-all [provider-uuid] [flags]
```

**Arguments:**
| Argument | Type | Description |
|----------|------|-------------|
| provider-uuid | string | UUID of the provider |

**Flags:**
| Flag | Type | Default | Description |
|------|------|---------|-------------|
| --limit | uint64 | 50 | Maximum leases to process (max 100) |

**Example:**
```bash
manifestd tx billing withdraw-all 01912345-6789-7abc-8def-0123456789ab --limit 100 --from provider-key
```

**Authorization:** Provider (address) or authority.

**Notes:**
- Processes up to `limit` active leases
- Response includes `has_more` if more leases remain
- Call repeatedly until `has_more` is false

---

#### update-params

Update module parameters (authority only).

```bash
manifestd tx billing update-params [max-leases-per-tenant] [max-items-per-lease] [min-lease-duration] [max-pending-leases-per-tenant] [pending-timeout] [flags]
```

**Arguments:**
| Argument | Type | Description |
|----------|------|-------------|
| max-leases-per-tenant | uint64 | Max active leases per tenant |
| max-items-per-lease | uint64 | Max items per lease |
| min-lease-duration | uint64 | Minimum lease duration in seconds |
| max-pending-leases-per-tenant | uint64 | Max pending leases per tenant |
| pending-timeout | uint64 | Pending lease timeout in seconds |

**Flags:**
| Flag | Type | Description |
|------|------|-------------|
| --allowed-list | string | Comma-separated allowed addresses (optional) |

**Example:**
```bash
manifestd tx billing update-params \
  100 \
  20 \
  3600 \
  10 \
  1800 \
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
    "max_leases_per_tenant": "100",
    "max_items_per_lease": "20",
    "min_lease_duration": "3600",
    "max_pending_leases_per_tenant": "10",
    "pending_timeout": "1800",
    "allowed_list": []
  }
}
```

**Note:** There is no global `denom` parameter. Each SKU defines its own denomination in its `base_price`.

---

#### lease

Query a lease by UUID.

```bash
manifestd query billing lease [lease-uuid]
```

**Arguments:**
| Argument | Type | Description |
|----------|------|-------------|
| lease-uuid | string | UUID of the lease |

**Response:**
```json
{
  "lease": {
    "uuid": "01912345-6789-7abc-8def-0123456789ab",
    "tenant": "manifest1abc...",
    "provider_uuid": "01912345-6789-7abc-8def-fedcba987654",
    "items": [
      {
        "sku_uuid": "01912345-6789-7abc-8def-111111111111",
        "quantity": "2",
        "locked_price": {
          "denom": "upwr",
          "amount": "100"
        }
      }
    ],
    "state": "LEASE_STATE_ACTIVE",
    "created_at": "2024-01-01T00:00:00Z",
    "acknowledged_at": "2024-01-01T00:01:00Z",
    "closed_at": null,
    "rejected_at": null,
    "expired_at": null,
    "last_settled_at": "2024-01-01T00:01:00Z",
    "rejection_reason": ""
  }
}
```

**Notes:**
- `locked_price` is a Coin with denom and amount, representing the per-second rate
- `acknowledged_at` is set when provider acknowledges (ACTIVE state)
- `closed_at` is set when lease is closed (CLOSED state)
- `rejected_at` is set when provider rejects or tenant cancels (REJECTED state)
- `expired_at` is set when pending lease times out (EXPIRED state)
- `rejection_reason` contains the provider's reason for rejection (max 256 chars)

---

#### leases

Query all leases with pagination.

```bash
manifestd query billing leases [flags]
```

**Flags:**
| Flag | Type | Description |
|------|------|-------------|
| --state | string | Filter by state (pending, active, closed, rejected, expired) |
| --limit | uint64 | Pagination limit |
| --page-key | string | Pagination key |

**Example:**
```bash
manifestd query billing leases --state active --limit 10
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
| --state | string | Filter by state (pending, active, closed, rejected, expired) |

---

#### leases-by-provider

Query leases for a specific provider.

```bash
manifestd query billing leases-by-provider [provider-uuid] [flags]
```

**Arguments:**
| Argument | Type | Description |
|----------|------|-------------|
| provider-uuid | string | UUID of the provider |

**Flags:**
| Flag | Type | Description |
|------|------|-------------|
| --state | string | Filter by state (pending, active, closed, rejected, expired) |

---

#### pending-leases-by-provider (gRPC/REST only)

Query pending leases for a specific provider. Useful for providers to see which leases need acknowledgement.

**Note:** This query is available via gRPC/REST only. For CLI, use:
```bash
manifestd query billing leases-by-provider [provider-uuid] --state pending
```

See the [gRPC API](#query-service) and [REST API](#rest-api) sections for endpoint details.

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
    "active_lease_count": "2",
    "pending_lease_count": "1"
  },
  "balances": [
    {
      "denom": "upwr",
      "amount": "1000000000"
    },
    {
      "denom": "umfx",
      "amount": "500000000"
    }
  ]
}
```

**Note:** Credit accounts can hold multiple denominations. The balances shown include all tokens in the account.

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

Query withdrawable amount for a lease. **This query calculates real-time accrued amounts.**

```bash
manifestd query billing withdrawable [lease-uuid]
```

**Arguments:**
| Argument | Type | Description |
|----------|------|-------------|
| lease-uuid | string | UUID of the lease |

**Response:**
```json
{
  "amounts": [
    {
      "denom": "upwr",
      "amount": "500000"
    }
  ]
}
```

**Note:** This calculates the real-time withdrawable amount based on time elapsed since `last_settled_at`. It is a read-only query and does NOT trigger actual settlement (no token transfer occurs). Only ACTIVE leases accrue charges.

---

#### provider-withdrawable

Query total withdrawable for a provider across all leases. **This query calculates real-time accrued amounts.**

```bash
manifestd query billing provider-withdrawable [provider-uuid]
```

**Arguments:**
| Argument | Type | Description |
|----------|------|-------------|
| provider-uuid | string | UUID of the provider |

**Response:**
```json
{
  "amounts": [
    {
      "denom": "upwr",
      "amount": "5000000"
    }
  ],
  "lease_count": "10"
}
```

**Note:** This calculates the real-time total withdrawable amount across all active leases for the provider. It is a read-only query and does NOT trigger actual settlement.

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
  rpc AcknowledgeLease(MsgAcknowledgeLease) returns (MsgAcknowledgeLeaseResponse);
  rpc RejectLease(MsgRejectLease) returns (MsgRejectLeaseResponse);
  rpc CancelLease(MsgCancelLease) returns (MsgCancelLeaseResponse);
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
  string sender = 1;   // Sender's address (anyone)
  string tenant = 2;   // Tenant's address
  cosmos.base.v1beta1.Coin amount = 3;  // Amount to fund
}
```

**Response:**
```protobuf
message MsgFundCreditResponse {
  string credit_address = 1;  // Credit account address
  cosmos.base.v1beta1.Coin new_balance = 2;  // New credit balance
}
```

> **Note:** `new_balance` returns only the balance for the funded denomination, not all denominations in the credit account. To query all balances, use the `CreditAccount` query which includes full balance information.

---

#### MsgCreateLease

Create a lease for the sender (tenant). Lease starts in PENDING state.

**Request:**
```protobuf
message MsgCreateLease {
  string tenant = 1;  // Tenant (must be signer)
  repeated LeaseItemInput items = 2;  // SKU items
}

message LeaseItemInput {
  string sku_uuid = 1;  // UUIDv7 of SKU
  uint64 quantity = 2;
}
```

**Response:**
```protobuf
message MsgCreateLeaseResponse {
  string lease_uuid = 1;  // Created lease UUID (UUIDv7)
}
```

---

#### MsgCreateLeaseForTenant

Create a lease on behalf of a tenant (authority/allowed only). Lease starts in PENDING state.

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
  string lease_uuid = 1;  // Created lease UUID (UUIDv7)
}
```

---

#### MsgAcknowledgeLease

Provider acknowledges one or more PENDING leases atomically, transitioning them to ACTIVE.
All leases must belong to the same provider and be in PENDING state.

**Request:**
```protobuf
message MsgAcknowledgeLease {
  string sender = 1;               // Provider or authority
  repeated string lease_uuids = 2; // Leases to acknowledge (1-100)
}
```

**Response:**
```protobuf
message MsgAcknowledgeLeaseResponse {
  google.protobuf.Timestamp acknowledged_at = 1;  // When billing starts
  uint64 acknowledged_count = 2;                  // Number of leases acknowledged
}
```

**Constraints:**
- All leases must belong to the same provider
- All leases must be in PENDING state
- Maximum 100 leases per call
- Atomic: all succeed or all fail

**CLI:**
```bash
manifestd tx billing acknowledge-lease <uuid1> [uuid2] [uuid3]... --from provider
```

---

#### MsgRejectLease

Provider rejects a PENDING lease.

**Request:**
```protobuf
message MsgRejectLease {
  string sender = 1;      // Provider or authority
  string lease_uuid = 2;  // Lease to reject
  string reason = 3;      // Optional reason (max 256 chars)
}
```

**Response:**
```protobuf
message MsgRejectLeaseResponse {
  google.protobuf.Timestamp rejected_at = 1;
}
```

---

#### MsgCancelLease

Tenant cancels their own PENDING lease.

**Request:**
```protobuf
message MsgCancelLease {
  string tenant = 1;      // Tenant (must own lease)
  string lease_uuid = 2;  // Lease to cancel
}
```

**Response:**
```protobuf
message MsgCancelLeaseResponse {
  google.protobuf.Timestamp cancelled_at = 1;
}
```

---

#### MsgCloseLease

Close an ACTIVE lease.

**Request:**
```protobuf
message MsgCloseLease {
  string sender = 1;      // Sender (tenant, provider, or authority)
  string lease_uuid = 2;  // Lease to close
}
```

**Response:**
```protobuf
message MsgCloseLeaseResponse {
  repeated cosmos.base.v1beta1.Coin settled_amounts = 1;  // Amounts settled per denom
}
```

---

#### MsgWithdraw

Withdraw from a specific lease.

**Request:**
```protobuf
message MsgWithdraw {
  string sender = 1;      // Provider or authority
  string lease_uuid = 2;  // Lease UUID
}
```

**Response:**
```protobuf
message MsgWithdrawResponse {
  repeated cosmos.base.v1beta1.Coin amounts = 1;  // Withdrawn amounts per denom
  string payout_address = 2;  // Destination address
}
```

---

#### MsgWithdrawAll

Withdraw from all provider leases.

**Request:**
```protobuf
message MsgWithdrawAll {
  string sender = 1;         // Provider or authority
  string provider_uuid = 2;  // Provider UUID
  uint64 limit = 3;        // Max leases (default 50, max 100)
}
```

**Response:**
```protobuf
message MsgWithdrawAllResponse {
  repeated cosmos.base.v1beta1.Coin total_amounts = 1;  // Total withdrawn per denom
  uint64 lease_count = 2;   // Leases processed
  string payout_address = 3;  // Destination address
  bool has_more = 4;        // More leases remain
}
```

---

#### MsgUpdateParams

Update module parameters (authority only).

**Request:**
```protobuf
message MsgUpdateParams {
  string authority = 1;  // Must be module authority
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
  rpc Lease(QueryLeaseRequest) returns (QueryLeaseResponse);
  rpc Leases(QueryLeasesRequest) returns (QueryLeasesResponse);
  rpc LeasesByTenant(QueryLeasesByTenantRequest) returns (QueryLeasesByTenantResponse);
  rpc LeasesByProvider(QueryLeasesByProviderRequest) returns (QueryLeasesByProviderResponse);
  rpc PendingLeasesByProvider(QueryPendingLeasesByProviderRequest) returns (QueryPendingLeasesByProviderResponse);
  rpc CreditAccount(QueryCreditAccountRequest) returns (QueryCreditAccountResponse);
  rpc CreditAddress(QueryCreditAddressRequest) returns (QueryCreditAddressResponse);
  rpc WithdrawableAmount(QueryWithdrawableAmountRequest) returns (QueryWithdrawableAmountResponse);
  rpc ProviderWithdrawable(QueryProviderWithdrawableRequest) returns (QueryProviderWithdrawableResponse);
}
```

**Important Note:** Lease queries (`Lease`, `Leases`, `LeasesByTenant`, `LeasesByProvider`) return stored state and do NOT trigger settlement or auto-close. However, `WithdrawableAmount` and `ProviderWithdrawable` queries calculate real-time accrued amounts based on elapsed time. Settlement (actual token transfer) only happens during write operations (Withdraw, CloseLease, WithdrawAll). Only ACTIVE leases accrue charges.

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

Get a lease by UUID.

**Endpoint:** `liftedinit.billing.v1.Query/Lease`

**Request:**
```protobuf
message QueryLeaseRequest {
  string lease_uuid = 1;
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
  LeaseState state = 2;  // Optional filter by state
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
  repeated cosmos.base.v1beta1.Coin balances = 2;  // All token balances at credit address
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
| GET | `/lease/{lease_uuid}` | Get lease by UUID |
| GET | `/leases` | List all leases |
| GET | `/leases/tenant/{tenant}` | List leases by tenant |
| GET | `/leases/provider/{provider_uuid}` | List leases by provider |
| GET | `/pending-leases/provider/{provider_uuid}` | List pending leases for provider |
| GET | `/credit-account/{tenant}` | Get credit account |
| GET | `/credit-address/{tenant}` | Derive credit address |
| GET | `/withdrawable/{lease_uuid}` | Get withdrawable amount |
| GET | `/provider-withdrawable/{provider_uuid}` | Get provider total withdrawable |

### Examples

**Get Parameters:**
```bash
curl http://localhost:1317/liftedinit/billing/v1/params
```

**Get Lease:**
```bash
curl http://localhost:1317/liftedinit/billing/v1/lease/01912345-6789-7abc-8def-0123456789ab
```

**List Active Leases:**
```bash
curl "http://localhost:1317/liftedinit/billing/v1/leases?state=active&pagination.limit=10"
```

**Get Credit Account:**
```bash
curl http://localhost:1317/liftedinit/billing/v1/credit-account/manifest1abc...
```

**Get Withdrawable Amount:**
```bash
curl http://localhost:1317/liftedinit/billing/v1/withdrawable/01912345-6789-7abc-8def-0123456789ab
```

---

## Data Types

### Lease

```protobuf
message Lease {
  string uuid = 1;                    // Unique UUIDv7 identifier
  string tenant = 2;                  // Tenant address
  string provider_uuid = 3;           // Provider UUID (from SKU module)
  repeated LeaseItem items = 4;       // List of leased SKU items
  LeaseState state = 5;               // Current state
  google.protobuf.Timestamp created_at = 6;
  google.protobuf.Timestamp acknowledged_at = 7;
  google.protobuf.Timestamp closed_at = 8;
  google.protobuf.Timestamp rejected_at = 9;
  google.protobuf.Timestamp expired_at = 10;
  google.protobuf.Timestamp last_settled_at = 11;
  string rejection_reason = 12;       // Provider's rejection reason (max 256 chars)
}
```

### LeaseItem

```protobuf
message LeaseItem {
  string sku_uuid = 1;                         // SKU UUID
  uint64 quantity = 2;                         // Number of instances
  cosmos.base.v1beta1.Coin locked_price = 3;   // Per-second rate locked at creation
}
```

### LeaseState

```protobuf
enum LeaseState {
  LEASE_STATE_UNSPECIFIED = 0;
  LEASE_STATE_PENDING = 1;    // Awaiting provider acknowledgement
  LEASE_STATE_ACTIVE = 2;     // Provider acknowledged, billing active
  LEASE_STATE_CLOSED = 3;     // Lease terminated normally
  LEASE_STATE_REJECTED = 4;   // Provider rejected or tenant cancelled
  LEASE_STATE_EXPIRED = 5;    // Pending lease timed out
}
```

### CreditAccount

```protobuf
message CreditAccount {
  string tenant = 1;              // Tenant address
  string credit_address = 2;      // Derived credit account address
  uint64 active_lease_count = 3;  // Number of ACTIVE leases
  uint64 pending_lease_count = 4; // Number of PENDING leases
}
```

### Params

```protobuf
message Params {
  uint64 max_leases_per_tenant = 1;
  uint64 max_items_per_lease = 2;
  uint64 min_lease_duration = 3;
  uint64 max_pending_leases_per_tenant = 4;
  uint64 pending_timeout = 5;
  repeated string allowed_list = 6;
}
```

---

## Events

The billing module emits the following events for state changes:

| Event | Attributes | Description |
|-------|------------|-------------|
| `credit_funded` | tenant, credit_address, sender, amount, new_balance | Credit account funded |
| `lease_created` | lease_uuid, tenant, provider_uuid, item_count, total_rate_per_second, pending_lease_count, created_by | Lease created in PENDING state |
| `lease_acknowledged` | lease_uuid, tenant, provider_uuid, acknowledged_by | Provider acknowledged lease (→ ACTIVE) |
| `batch_acknowledged` | lease_count, provider_uuid, acknowledged_by | Batch summary when multiple leases acknowledged |
| `lease_rejected` | lease_uuid, tenant, provider_uuid, rejected_by, rejection_reason | Provider rejected lease |
| `lease_cancelled` | lease_uuid, tenant, provider_uuid, cancelled_by | Tenant cancelled pending lease |
| `lease_expired` | lease_uuid, tenant, provider_uuid, reason | Pending lease expired |
| `lease_closed` | lease_uuid, tenant, provider_uuid, settled_amounts, closed_by, duration_seconds, active_lease_count | Lease closed manually |
| `lease_auto_closed` | lease_uuid, tenant, provider_uuid, settled_amounts, reason | Lease auto-closed due to credit exhaustion |
| `provider_withdraw` | lease_uuid, provider_uuid, amount, payout_address | Provider withdrawal |
| `provider_withdraw_all` | provider_uuid, amount, lease_count, payout_address | Provider withdrew from all leases |
| `params_updated` | | Module parameters updated |

**Special Case - Withdrawal Auto-Close:** When a `MsgWithdraw` operation discovers the lease's credit is exhausted (balance = 0), it automatically closes the lease. In this case, the `provider_withdraw` event includes an additional `auto_closed: "true"` attribute and `amount: "0"` to indicate no funds were transferred. Note that the `payout_address` attribute is omitted in this case since no transfer occurred.

### Event Attribute Sanitization

Certain event attributes (like `rejection_reason`) are sanitized before being emitted to prevent log injection attacks. The original value is stored in state unchanged, but the sanitized version appears in events. This protects against malicious input containing control characters or log format strings.

### Querying Events

Events can be queried from transaction results:

```bash
# Query events for a specific transaction
manifestd query tx [txhash] --output json | jq '.events'

# Example: Extract lease_uuid from a lease creation
manifestd query tx [txhash] --output json | jq -r '.logs[0].events[] | select(.type=="lease_created") | .attributes[] | select(.key=="lease_uuid") | .value'
```

---

## Error Codes

| Error | Code | Description |
|-------|------|-------------|
| `ErrInvalidParams` | 1 | Invalid module parameters |
| `ErrLeaseNotFound` | 2 | Lease doesn't exist |
| `ErrLeaseNotActive` | 3 | Lease is not in ACTIVE state |
| `ErrInsufficientCredit` | 4 | Not enough credit balance |
| `ErrMaxLeasesReached` | 5 | Tenant at max active leases |
| `ErrUnauthorized` | 6 | Sender not authorized |
| `ErrReserved7` | 7 | Reserved for future use |
| `ErrCreditAccountNotFound` | 8 | Credit account doesn't exist |
| `ErrInvalidLease` | 9 | Invalid lease parameters |
| `ErrSKUNotFound` | 10 | SKU doesn't exist |
| `ErrSKUNotActive` | 11 | SKU is deactivated |
| `ErrProviderNotFound` | 12 | Provider doesn't exist |
| `ErrProviderNotActive` | 13 | Provider is deactivated |
| `ErrMixedProviders` | 14 | SKUs from different providers in one lease |
| `ErrNoWithdrawableAmount` | 15 | Nothing to withdraw |
| `ErrEmptyLeaseItems` | 16 | Lease has no items |
| `ErrInvalidQuantity` | 17 | Item quantity is zero |
| `ErrDuplicateSKU` | 18 | Same SKU appears multiple times |
| `ErrInvalidCreditOperation` | 19 | Credit operation failed |
| `ErrReserved20` | 20 | Reserved for future use |
| `ErrTooManyLeaseItems` | 21 | Lease exceeds max items |
| `ErrLeaseNotPending` | 22 | Lease is not in PENDING state |
| `ErrMaxPendingLeasesReached` | 23 | Tenant at max pending leases |
| `ErrInvalidRejectionReason` | 24 | Rejection reason too long (max 256 chars) |

**Note on Reserved Codes:** Error codes 7 and 20 are explicitly reserved to maintain stable error code assignments. When adding new error types, these reserved codes ensure that error numbers remain consistent across module versions. Do not use these codes for new errors; instead, assign the next available number.

---

## Authorization

| Operation | Tenant | Provider | Authority | Allowed List |
|-----------|--------|----------|-----------|--------------|
| FundCredit | ✓ (anyone) | ✓ (anyone) | ✓ | ✓ |
| CreateLease | ✓ (self only) | ✗ | ✗ | ✗ |
| CreateLeaseForTenant | ✗ | ✗ | ✓ | ✓ |
| AcknowledgeLease | ✗ | ✓ | ✓ | ✗ |
| RejectLease | ✗ | ✓ | ✓ | ✗ |
| CancelLease | ✓ (own leases) | ✗ | ✗ | ✗ |
| CloseLease | ✓ (own leases) | ✓ | ✓ | ✗ |
| Withdraw | ✗ | ✓ | ✓ | ✗ |
| WithdrawAll | ✗ | ✓ | ✓ | ✗ |
| UpdateParams | ✗ | ✗ | ✓ | ✗ |

**Notes:**
- "Tenant" refers to the lease owner
- "Provider" refers to the provider address associated with the lease's SKUs
- "Authority" is the module authority (POA admin group)
- "Allowed List" contains addresses permitted to create leases on behalf of tenants

---

## Related Documentation

- [Billing README](../README.md) - Complete billing module overview
- [Migration Guide](MIGRATION.md) - Migrating existing off-chain leases
- [Troubleshooting Guide](TROUBLESHOOTING.md) - Common issues and solutions
- [Architecture](ARCHITECTURE.md) - Technical architecture details
