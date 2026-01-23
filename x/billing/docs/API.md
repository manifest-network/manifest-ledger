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

**Flags:**
| Flag | Type | Description |
|------|------|-------------|
| --meta-hash | string | Hex-encoded hash/reference to off-chain deployment data (max 64 bytes) |

**Examples:**
```bash
# Create lease without meta_hash
manifestd tx billing create-lease 01912345-6789-7abc-8def-0123456789ab:2 01912345-6789-7abc-8def-0123456789ac:1 --from mykey

# Create lease with meta_hash (SHA-256 hash of deployment manifest)
manifestd tx billing create-lease 01912345-6789-7abc-8def-0123456789ab:1 --meta-hash a1b2c3d4e5f6... --from mykey
```

**Constraints:**
- Sender must have funded credit account
- Credit must cover `min_lease_duration` seconds for each denom used by the SKUs
- All SKUs must be from the same provider
- All SKUs must be active
- Cannot exceed `max_items_per_lease`
- Cannot exceed `max_leases_per_tenant`
- Cannot exceed `max_pending_leases_per_tenant`
- `meta_hash` cannot exceed 64 bytes (accommodates SHA-256/SHA-512 hashes)

**Notes:**
- Lease starts in PENDING state awaiting provider acknowledgement
- Credit is locked but billing does not start until acknowledgement
- Returns the lease UUID on success
- `meta_hash` is optional and immutable once set

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

**Flags:**
| Flag | Type | Description |
|------|------|-------------|
| --meta-hash | string | Hex-encoded hash/reference to off-chain deployment data (max 64 bytes) |

**Examples:**
```bash
# Create lease without meta_hash
manifestd tx billing create-lease-for-tenant manifest1abc... 01912345-6789-7abc-8def-0123456789ab:2 --from authority

# Create lease with meta_hash
manifestd tx billing create-lease-for-tenant manifest1abc... 01912345-6789-7abc-8def-0123456789ab:1 --meta-hash a1b2c3d4e5f6... --from authority
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

Reject one or more PENDING leases (provider only). Credit is unlocked and returned to tenants.

```bash
manifestd tx billing reject-lease [lease-uuid]... [flags]
```

**Arguments:**
| Argument | Type | Description |
|----------|------|-------------|
| lease-uuid | string | UUID(s) of leases to reject (1-100) |

**Flags:**
| Flag | Type | Description |
|------|------|-------------|
| --reason | string | Optional rejection reason (max 256 chars, applied to all leases) |

**Examples:**
```bash
# Reject a single lease
manifestd tx billing reject-lease 01912345-6789-7abc-8def-0123456789ab --reason "Resources unavailable" --from provider-key

# Reject multiple leases atomically (all must belong to same provider)
manifestd tx billing reject-lease 01912345-6789-7abc-8def-0123456789ab 01912345-6789-7abc-8def-fedcba987654 --reason "Batch rejection" --from provider-key
```

**Authorization:** Provider address or authority.

**Notes:**
- All leases must belong to the same provider (atomic operation)
- All leases must be in PENDING state
- If any lease fails validation, the entire batch fails (no partial rejections)
- Emits `batch_rejected` event when multiple leases are processed (includes lease_count, provider_uuid, rejected_by)

---

#### cancel-lease

Cancel one or more PENDING leases (tenant only). Credit is unlocked and returned.

```bash
manifestd tx billing cancel-lease [lease-uuid]... [flags]
```

**Arguments:**
| Argument | Type | Description |
|----------|------|-------------|
| lease-uuid | string | UUID(s) of leases to cancel (1-100) |

**Examples:**
```bash
# Cancel a single lease
manifestd tx billing cancel-lease 01912345-6789-7abc-8def-0123456789ab --from tenant-key

# Cancel multiple leases atomically
manifestd tx billing cancel-lease 01912345-6789-7abc-8def-0123456789ab 01912345-6789-7abc-8def-fedcba987654 --from tenant-key
```

**Authorization:** Tenant (owner) only.

**Notes:**
- Only PENDING leases can be cancelled
- All leases must belong to the tenant making the request
- If any lease fails validation, the entire batch fails (no partial cancellations)
- Credit is immediately unlocked
- Response includes cancelled_count showing how many leases were cancelled
- Emits `batch_cancelled` event when multiple leases are processed (includes lease_count, tenant, cancelled_by)

---

#### close-lease

Close one or more ACTIVE leases.

```bash
manifestd tx billing close-lease [lease-uuid]... [flags]
```

**Arguments:**
| Argument | Type | Description |
|----------|------|-------------|
| lease-uuid | string | UUID(s) of leases to close (1-100) |

**Flags:**
| Flag | Type | Description |
|------|------|-------------|
| --reason | string | Reason for closing the leases (max 256 characters, applied to all) |

**Examples:**
```bash
# Close a single lease
manifestd tx billing close-lease 01912345-6789-7abc-8def-0123456789ab --from mykey

# Close multiple leases with a reason
manifestd tx billing close-lease 01912345-6789-7abc-8def-0123456789ab 01912345-6789-7abc-8def-fedcba987654 --reason "service no longer needed" --from mykey
```

**Authorization:** Tenant (owner), provider (of SKUs), or authority.

**Notes:**
- Only ACTIVE leases can be closed
- Performs final settlement during closure
- Optional reason is stored on the lease and included in closure event
- All leases must pass authorization for the sender:
  - Tenant: All leases must have that tenant
  - Provider: All leases must belong to that provider
  - Authority: Can close any leases
- If any lease fails validation, the entire batch fails (no partial closures)
- Response includes total_settled_amounts aggregated across all closed leases
- Emits `batch_closed` event when multiple leases are processed (includes lease_count, closed_by)
- Transfers accrued amount to provider payout address
- Sets lease state to CLOSED
- **Auto-close**: When a lease is automatically closed due to credit exhaustion (lazy settlement), the closure_reason is automatically set to "credit exhausted"

---

#### withdraw

Withdraw accrued funds from leases. Supports two mutually exclusive modes:

1. **Specific leases mode**: Withdraw from one or more specific lease UUIDs
2. **Provider-wide mode**: Withdraw from all leases for a provider (paginated)

```bash
# Mode 1: Specific leases
manifestd tx billing withdraw [lease-uuid]... [flags]

# Mode 2: Provider-wide
manifestd tx billing withdraw --provider [provider-uuid] [flags]
```

**Arguments (Mode 1):**
| Argument | Type | Description |
|----------|------|-------------|
| lease-uuid | string | UUID(s) of leases to withdraw from (1-100) |

**Flags:**
| Flag | Type | Default | Description |
|------|------|---------|-------------|
| --provider | string | - | Provider UUID for provider-wide withdrawal |
| --limit | uint64 | 50 | Maximum leases to process in provider mode (max 100) |

**Examples:**
```bash
# Withdraw from a single lease
manifestd tx billing withdraw 01912345-6789-7abc-8def-0123456789ab --from provider-key

# Withdraw from multiple leases in one transaction
manifestd tx billing withdraw 01912345-6789-7abc-8def-0123456789ab 01912345-6789-7abc-8def-fedcba987654 --from provider-key

# Withdraw from all provider's leases (provider-wide mode)
manifestd tx billing withdraw --provider 01912345-6789-7abc-8def-0123456789ab --from provider-key

# Provider-wide withdrawal with custom limit
manifestd tx billing withdraw --provider 01912345-6789-7abc-8def-0123456789ab --limit 100 --from provider-key
```

**Authorization:** Provider (of SKUs) or authority.

**Notes:**
- **Mode 1 (Specific leases):**
  - All leases must belong to the same provider
  - If any lease fails validation, the entire batch fails (no partial withdrawals)
  - `has_more` is always false in this mode
- **Mode 2 (Provider-wide):**
  - Processes up to `limit` active leases
  - Response includes `has_more` if more leases remain
  - Call repeatedly until `has_more` is false
  - See [Provider-Wide Withdraw Workflow](#provider-wide-withdraw-workflow) below for example
- Settles accrued amount since last settlement for each lease
- Transfers aggregated amounts to provider's payout address
- May trigger auto-close if credit exhausted during withdrawal
- Response includes withdrawal_count and total_amounts aggregated across all leases
- Emits `batch_withdraw` event when multiple leases are processed

##### Provider-Wide Withdraw Workflow

When a provider has many active leases, use provider-wide mode with pagination to withdraw from all:

```bash
# Step 1: Initial withdrawal (processes up to 100 leases)
manifestd tx billing withdraw --provider 01912345-6789-7abc-8def-0123456789ab --limit 100 --from provider-key

# Response example:
# {
#   "total_amounts": [{"denom": "upwr", "amount": "5000000"}],
#   "payout_address": "manifest1payout...",
#   "withdrawal_count": "100",
#   "has_more": true    <-- More leases remain
# }

# Step 2: Continue withdrawing until has_more is false
manifestd tx billing withdraw --provider 01912345-6789-7abc-8def-0123456789ab --limit 100 --from provider-key

# Response:
# {
#   "total_amounts": [{"denom": "upwr", "amount": "2500000"}],
#   "payout_address": "manifest1payout...",
#   "withdrawal_count": "50",
#   "has_more": false   <-- All leases processed
# }
```

**Automation Script Example (bash):**
```bash
#!/bin/bash
PROVIDER_UUID="01912345-6789-7abc-8def-0123456789ab"
HAS_MORE=true

while [ "$HAS_MORE" = "true" ]; do
  RESULT=$(manifestd tx billing withdraw --provider $PROVIDER_UUID --limit 100 --from provider-key -o json -y)
  HAS_MORE=$(echo $RESULT | jq -r '.has_more')
  echo "Withdrew from $(echo $RESULT | jq -r '.withdrawal_count') leases, has_more=$HAS_MORE"
done
echo "All withdrawals complete"
```

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
    "rejection_reason": "",
    "closure_reason": "",
    "meta_hash": "a1b2c3d4...",
    "min_lease_duration_at_creation": "3600"
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
- `closure_reason` contains the reason for closure (max 256 chars)
- `meta_hash` contains the optional hash/reference to off-chain deployment data (max 64 bytes, immutable)
- `min_lease_duration_at_creation` stores the `min_lease_duration` parameter value at creation time for consistent reservation calculation

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
    "pending_lease_count": "1",
    "reserved_amounts": [
      {
        "denom": "upwr",
        "amount": "360000"
      }
    ]
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
  ],
  "available_balances": [
    {
      "denom": "upwr",
      "amount": "999640000"
    },
    {
      "denom": "umfx",
      "amount": "500000000"
    }
  ]
}
```

**Response Fields:**
- `credit_account.reserved_amounts`: Credit reserved by active and pending leases. Each lease reserves `rate_per_second × min_lease_duration` per denom.
- `balances`: Total credit balance at the credit address (from bank module).
- `available_balances`: Credit available for new leases (`balances - reserved_amounts`). New leases can only be created if this covers the required reservation.

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

# With custom limit (default: 100, max: 1000)
manifestd query billing provider-withdrawable [provider-uuid] --limit 500
```

**Arguments:**
| Argument | Type | Description |
|----------|------|-------------|
| provider-uuid | string | UUID of the provider |

**Flags:**
| Flag | Type | Default | Description |
|------|------|---------|-------------|
| --limit | uint64 | 100 | Maximum leases to process (max: 1000) |

**Response:**
```json
{
  "amounts": [
    {
      "denom": "upwr",
      "amount": "5000000"
    }
  ],
  "lease_count": "10",
  "has_more": false
}
```

**Note:** This calculates the real-time total withdrawable amount across all active leases for the provider. It is a read-only query and does NOT trigger actual settlement. For providers with many leases, use the `--limit` flag and check `has_more` to process in batches.

---

#### credit-accounts

Query all credit accounts with pagination.

```bash
manifestd query billing credit-accounts [flags]
```

**Flags:**
| Flag | Type | Description |
|------|------|-------------|
| --limit | uint64 | Pagination limit |
| --page-key | string | Pagination key |

**Example:**
```bash
manifestd query billing credit-accounts --limit 10
```

**Response:**
```json
{
  "credit_accounts": [
    {
      "tenant": "manifest1abc...",
      "credit_address": "manifest1xyz...",
      "active_lease_count": "2",
      "pending_lease_count": "1"
    }
  ],
  "pagination": {
    "next_key": "...",
    "total": "50"
  }
}
```

**Note:** This returns credit account metadata only. To get balances for a specific account, use the `credit-account` query.

---

#### leases-by-sku

Query leases that contain a specific SKU.

```bash
manifestd query billing leases-by-sku [sku-uuid] [flags]
```

**Arguments:**
| Argument | Type | Description |
|----------|------|-------------|
| sku-uuid | string | UUID of the SKU |

**Flags:**
| Flag | Type | Description |
|------|------|-------------|
| --state | string | Filter by state (pending, active, closed, rejected, expired) |
| --limit | uint64 | Pagination limit |
| --page-key | string | Pagination key |

**Example:**
```bash
manifestd query billing leases-by-sku 01912345-6789-7abc-8def-0123456789ab --state active
```

**Response:**
```json
{
  "leases": [
    {
      "uuid": "01912345-6789-7abc-8def-fedcba987654",
      "tenant": "manifest1abc...",
      "provider_uuid": "01912345-6789-7abc-8def-111111111111",
      "items": [...],
      "state": "LEASE_STATE_ACTIVE",
      ...
    }
  ],
  "pagination": {
    "next_key": "...",
    "total": "10"
  }
}
```

**Note:** This query uses the SKU index for efficient lookups. Use the `--state` filter to narrow results and pagination flags to page through large result sets.

---

#### credit-estimate

Estimate how long a tenant's credit balance will last based on current active leases.

```bash
manifestd query billing credit-estimate [tenant]
```

**Arguments:**
| Argument | Type | Description |
|----------|------|-------------|
| tenant | string | Bech32 address of the tenant |

**Example:**
```bash
manifestd query billing credit-estimate manifest1abc...
```

**Response:**
```json
{
  "current_balance": [
    {
      "denom": "upwr",
      "amount": "1000000000"
    }
  ],
  "total_rate_per_second": [
    {
      "denom": "upwr",
      "amount": "1000"
    }
  ],
  "estimated_duration_seconds": "1000000",
  "active_lease_count": "3"
}
```

**Fields:**
| Field | Description |
|-------|-------------|
| `current_balance` | Tenant's current credit balance (all denominations) |
| `total_rate_per_second` | Combined burn rate of all active leases (per denom) |
| `estimated_duration_seconds` | Seconds until credit exhaustion (minimum across all denoms) |
| `active_lease_count` | Number of currently active leases |

**Notes:**
- The estimate is calculated in real-time based on current balances and active lease rates
- If no active leases exist, `estimated_duration_seconds` will be `0` and `total_rate_per_second` will be empty
- With multi-denom support, the estimate returns the minimum duration across all denominations (the limiting factor)

**Limitations:**
- **Maximum 100 leases processed**: If a tenant has more than 100 active leases, only the first 100 are included in the calculation. The `active_lease_count` will show the actual count, but rates may be underestimated for tenants with >100 leases.
- **Does not account for pending withdrawals**: The estimate uses current balance, not accounting for any unsettled accrued amounts from existing leases.
- **Assumes constant rate**: The estimate assumes all current leases continue at their current rates. Actual duration may differ if leases are closed or new leases are created.

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
  bytes meta_hash = 3;  // Optional hash/reference to off-chain deployment data (max 64 bytes)
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
  bytes meta_hash = 4;  // Optional hash/reference to off-chain deployment data (max 64 bytes)
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

Provider rejects one or more PENDING leases atomically.

**Request:**
```protobuf
message MsgRejectLease {
  string sender = 1;               // Provider or authority
  repeated string lease_uuids = 2; // Leases to reject (1-100)
  string reason = 3;               // Optional reason (max 256 chars, applied to all)
}
```

**Response:**
```protobuf
message MsgRejectLeaseResponse {
  google.protobuf.Timestamp rejected_at = 1;  // When leases were rejected
  uint64 rejected_count = 2;                  // Number of leases rejected
}
```

**Constraints:**
- All leases must belong to the same provider
- All leases must be in PENDING state
- Maximum 100 leases per call
- Atomic: all succeed or all fail

---

#### MsgCancelLease

Tenant cancels one or more of their own PENDING leases atomically.

**Request:**
```protobuf
message MsgCancelLease {
  string tenant = 1;               // Tenant (must own all leases)
  repeated string lease_uuids = 2; // Leases to cancel (1-100)
}
```

**Response:**
```protobuf
message MsgCancelLeaseResponse {
  google.protobuf.Timestamp cancelled_at = 1;  // When leases were cancelled
  uint64 cancelled_count = 2;                  // Number of leases cancelled
}
```

**Constraints:**
- All leases must belong to the tenant
- All leases must be in PENDING state
- Maximum 100 leases per call
- Atomic: all succeed or all fail

---

#### MsgCloseLease

Close one or more ACTIVE leases atomically.

**Request:**
```protobuf
message MsgCloseLease {
  string sender = 1;               // Sender (tenant, provider, or authority)
  repeated string lease_uuids = 2; // Leases to close (1-100)
  string reason = 3;               // Optional closure reason (max 256 chars, applied to all)
}
```

**Response:**
```protobuf
message MsgCloseLeaseResponse {
  google.protobuf.Timestamp closed_at = 1;                       // When leases were closed
  uint64 closed_count = 2;                                       // Number of leases closed
  repeated cosmos.base.v1beta1.Coin total_settled_amounts = 3;   // Total amounts settled per denom
}
```

**Constraints:**
- All leases must be in ACTIVE state
- All leases must pass authorization for the sender
- Maximum 100 leases per call
- Atomic: all succeed or all fail
- Reason is stored on each lease's `closure_reason` field

---

#### MsgWithdraw

Withdraw from leases. Supports two mutually exclusive modes:
1. **Specific leases mode**: Provide `lease_uuids` to withdraw from specific leases
2. **Provider-wide mode**: Provide `provider_uuid` to withdraw from all provider's leases (paginated)

**Request:**
```protobuf
message MsgWithdraw {
  string sender = 1;               // Provider or authority
  repeated string lease_uuids = 2; // Mode 1: specific lease UUIDs (1-100)
  string provider_uuid = 3;        // Mode 2: provider UUID for provider-wide withdrawal
  uint64 limit = 4;                // Max leases in provider mode (default 50, max 100)
}
```

> **Note:** `lease_uuids` and `provider_uuid` are mutually exclusive - exactly one must be specified.

**Response:**
```protobuf
message MsgWithdrawResponse {
  repeated cosmos.base.v1beta1.Coin total_amounts = 1;  // Total withdrawn per denom
  string payout_address = 2;        // Destination address
  uint64 withdrawal_count = 3;      // Number of leases processed
  bool has_more = 4;                // More leases remain (only true in provider-wide mode)
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
  rpc LeasesBySKU(QueryLeasesBySKURequest) returns (QueryLeasesBySKUResponse);
  rpc CreditAccount(QueryCreditAccountRequest) returns (QueryCreditAccountResponse);
  rpc CreditAccounts(QueryCreditAccountsRequest) returns (QueryCreditAccountsResponse);
  rpc CreditEstimate(QueryCreditEstimateRequest) returns (QueryCreditEstimateResponse);
  rpc CreditAddress(QueryCreditAddressRequest) returns (QueryCreditAddressResponse);
  rpc WithdrawableAmount(QueryWithdrawableAmountRequest) returns (QueryWithdrawableAmountResponse);
  rpc ProviderWithdrawable(QueryProviderWithdrawableRequest) returns (QueryProviderWithdrawableResponse);
}
```

**Important Note:** Lease queries (`Lease`, `Leases`, `LeasesByTenant`, `LeasesByProvider`) return stored state and do NOT trigger settlement or auto-close. However, `WithdrawableAmount` and `ProviderWithdrawable` queries calculate real-time accrued amounts based on elapsed time. Settlement (actual token transfer) only happens during write operations (Withdraw, CloseLease). Only ACTIVE leases accrue charges.

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
| GET | `/leases/sku/{sku_uuid}` | List leases by SKU |
| GET | `/credit/{tenant}` | Get credit account |
| GET | `/credits` | List all credit accounts |
| GET | `/credit/{tenant}/estimate` | Estimate credit duration |
| GET | `/credit-address/{tenant}` | Derive credit address |
| GET | `/lease/{lease_uuid}/withdrawable` | Get withdrawable amount |
| GET | `/provider/{provider_uuid}/withdrawable` | Get provider total withdrawable |

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
curl http://localhost:1317/liftedinit/billing/v1/credit/manifest1abc...
```

**Get Withdrawable Amount:**
```bash
curl http://localhost:1317/liftedinit/billing/v1/lease/01912345-6789-7abc-8def-0123456789ab/withdrawable
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
  string closure_reason = 13;         // Closure reason (max 256 chars)
  bytes meta_hash = 14;               // Hash/reference to off-chain deployment data (max 64 bytes, immutable)
  uint64 min_lease_duration_at_creation = 15; // Snapshot of min_lease_duration param at creation
}
```

**Field Notes:**
- `rejection_reason`: Set when a provider rejects a PENDING lease via `MsgRejectLease`. Contains the provider's explanation for rejecting the lease (e.g., "resources unavailable", "invalid configuration"). Maximum 256 characters. Only present when `state` is `LEASE_STATE_REJECTED`.
- `closure_reason`: Set when a lease is closed via `MsgCloseLease` with a reason, or automatically set to `"credit exhausted"` when a lease is auto-closed due to insufficient credit during settlement. Maximum 256 characters. Only present when `state` is `LEASE_STATE_CLOSED`.
- `meta_hash`: Optional immutable hash or reference linking to off-chain deployment data (e.g., deployment manifest hash, configuration reference). Set at lease creation and cannot be modified afterward. Maximum 64 bytes to accommodate SHA-256 or SHA-512 hashes.
- `min_lease_duration_at_creation`: Snapshot of the `min_lease_duration` parameter at the time this lease was created. Used to calculate consistent credit reservations (`reservation = sum(locked_price × quantity) × min_lease_duration_at_creation`) regardless of subsequent governance changes to the parameter. This ensures existing reservations remain valid when parameters are updated.

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
  repeated Coin reserved_amounts = 5; // Credit reserved by active/pending leases
}
```

**Field Notes:**
- `reserved_amounts`: Sum of credit reservations for all PENDING and ACTIVE leases. Each lease reserves `rate_per_second × min_lease_duration` per denom. This prevents overbooking by ensuring credit availability before lease creation. Available credit = balances - reserved_amounts.

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
| `lease_created` | lease_uuid, tenant, provider_uuid, item_count, total_rate_per_second, pending_lease_count, created_by, meta_hash (optional, hex-encoded) | Lease created in PENDING state |
| `lease_acknowledged` | lease_uuid, tenant, provider_uuid, acknowledged_by | Provider acknowledged lease (→ ACTIVE) |
| `batch_acknowledged` | lease_count, provider_uuid, acknowledged_by | Batch summary when multiple leases acknowledged |
| `lease_rejected` | lease_uuid, tenant, provider_uuid, rejected_by, rejection_reason | Provider rejected lease |
| `batch_rejected` | lease_count, provider_uuid, rejected_by | Batch summary when multiple leases rejected |
| `lease_cancelled` | lease_uuid, tenant, provider_uuid, cancelled_by | Tenant cancelled pending lease |
| `batch_cancelled` | lease_count, tenant, cancelled_by | Batch summary when multiple leases cancelled |
| `lease_expired` | lease_uuid, tenant, provider_uuid, reason | Pending lease expired |
| `lease_closed` | lease_uuid, tenant, provider_uuid, settled_amounts, closed_by, duration_seconds, active_lease_count, closure_reason (optional) | Lease closed manually |
| `batch_closed` | lease_count, closed_by | Batch summary when multiple leases closed |
| `lease_auto_closed` | lease_uuid, tenant, provider_uuid, settled_amounts, reason | Lease auto-closed due to credit exhaustion |
| `provider_withdraw` | lease_uuid, provider_uuid, payout_address | Provider withdrawal from single lease |
| `batch_withdraw` | lease_count, provider_uuid, amount, payout_address | Batch summary when multiple leases withdrawn from |
| `params_updated` | | Module parameters updated |

**Special Case - Withdrawal Auto-Close:** When a `MsgWithdraw` operation discovers the lease's credit is exhausted (balance = 0), it automatically closes the lease. In this case, the `provider_withdraw` event includes an additional `auto_closed: "true"` attribute and `amount: "0"` to indicate no funds were transferred. Note that the `payout_address` attribute is omitted in this case since no transfer occurred.

### Event Attribute Sanitization

Certain event attributes (like `rejection_reason` and `closure_reason`) are sanitized before being emitted to prevent log injection attacks. The original value is stored in state unchanged, but the sanitized version appears in events. This protects against malicious input containing control characters or log format strings.

**Sanitization Rules:**
- Control characters (ASCII 0-31, 127) are removed
- Newlines (`\n`, `\r`) are replaced with spaces
- Log format specifiers (e.g., `%s`, `%d`) are escaped
- Maximum length enforced (256 characters for reasons)

**Example:**
```
# Original input to MsgRejectLease:
reason: "Invalid config\nSee logs for details"

# Stored in state (unchanged):
rejection_reason: "Invalid config\nSee logs for details"

# Emitted in event (sanitized):
rejection_reason: "Invalid config See logs for details"
```

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
| `ErrInvalidRequest` | 25 | Invalid request (e.g., conflicting fields in MsgWithdraw) |
| `ErrInvalidClosureReason` | 26 | Closure reason too long (max 256 chars) |
| `ErrInvalidMetaHash` | 27 | Meta hash exceeds maximum length (max 64 bytes) |

**Note on Reserved Codes:** Error codes 7 and 20 are explicitly reserved to maintain stable error code assignments across module versions.

**Why reserve codes?** During development, some error types were removed or consolidated (e.g., separate errors that were merged into a single error). Rather than renumbering all subsequent codes (which would break client error handling that relies on specific codes), the removed codes are marked as reserved. This ensures:
- Existing client code that handles specific error codes continues to work after upgrades
- Error codes in logs and metrics remain comparable across versions
- New errors get the next available number (28+) rather than reusing gaps

**For developers:** Never assign new errors to reserved codes. Always use the next sequential number after the highest assigned code.

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
