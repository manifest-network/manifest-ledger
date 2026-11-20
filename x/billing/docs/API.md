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
manifestd tx billing create-lease [sku-uuid:quantity[:service_name]...] [flags]
```

**Arguments:**
| Argument | Type | Description |
|----------|------|-------------|
| items | string... | Space-separated list of `sku-uuid:quantity[:service_name]` entries |

**Flags:**
| Flag | Type | Description |
|------|------|-------------|
| --meta-hash | string | Hex-encoded hash/reference to off-chain deployment data (max 64 bytes) |

**Examples:**
```bash
# Create lease (legacy mode, unique SKUs)
manifestd tx billing create-lease 01912345-6789-7abc-8def-0123456789ab:2 01912345-6789-7abc-8def-0123456789ac:1 --from mykey

# Create lease with meta_hash (SHA-256 hash of deployment manifest)
manifestd tx billing create-lease 01912345-6789-7abc-8def-0123456789ab:1 --meta-hash a1b2c3d4e5f6... --from mykey

# Create stack lease with service_names (same SKU, different services)
manifestd tx billing create-lease 01912345-6789-7abc-8def-0123456789ab:1:web 01912345-6789-7abc-8def-0123456789ab:1:db --from mykey
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
- If any item has a `service_name`, all items must have one (all-or-nothing)
- `service_name` must be a valid RFC 1123 DNS label (1-63 lowercase alphanumeric/hyphens, no leading/trailing hyphen)
- In service_name mode, uniqueness is by `service_name` (same SKU allowed); in legacy mode, uniqueness is by `sku_uuid`

**Notes:**
- Lease starts in PENDING state awaiting provider acknowledgement
- Credit is locked but billing does not start until acknowledgement
- Returns the lease UUID on success
- `meta_hash` is optional and immutable once set
- `service_name` enables stack deployments where the same SKU maps to different named services

---

#### create-lease-for-tenant

Create a lease on behalf of a tenant (authority/allowed addresses only). The lease starts in PENDING state.

```bash
manifestd tx billing create-lease-for-tenant [tenant] [sku-uuid:quantity[:service_name]...] [flags]
```

**Arguments:**
| Argument | Type | Description |
|----------|------|-------------|
| tenant | string | Bech32 address of the tenant |
| items | string... | Space-separated list of `sku-uuid:quantity[:service_name]` entries |

**Flags:**
| Flag | Type | Description |
|------|------|-------------|
| --meta-hash | string | Hex-encoded hash/reference to off-chain deployment data (max 64 bytes) |

**Examples:**
```bash
# Create lease (legacy mode)
manifestd tx billing create-lease-for-tenant manifest1abc... 01912345-6789-7abc-8def-0123456789ab:2 --from authority

# Create lease with meta_hash
manifestd tx billing create-lease-for-tenant manifest1abc... 01912345-6789-7abc-8def-0123456789ab:1 --meta-hash a1b2c3d4e5f6... --from authority

# Create stack lease with service_names
manifestd tx billing create-lease-for-tenant manifest1abc... 01912345-6789-7abc-8def-0123456789ab:1:web 01912345-6789-7abc-8def-0123456789ab:1:db --from authority
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
- Each lease joins its settlement streams (one per denom it is priced in). A stream that already exists is settled up to the block time before its rate grows, so the transaction may pay the provider what that stream had accrued (`stream_settled`) or freeze it if the tenant's credit is exhausted (`stream_exhausted`)
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
- Settles each stream the closed leases belong to, up to the block time, then removes the leases from those streams. A stream is settled once per transaction, however many of its leases are closed
- Optional reason is stored on the lease and included in closure event
- All leases must pass authorization for the sender:
  - Tenant: All leases must have that tenant
  - Provider: All leases must belong to that provider
  - Authority: Can close any leases
- If any lease fails validation, the entire batch fails (no partial closures)
- Response includes `total_settled_amounts`: what the settled streams paid out. Because settlement is per stream, closing 1 of N leases of a stream pays the accrual of all N, not only the closed lease's share
- Response `closed_at`, and each lease's `closed_at`, is the block time
- Emits `lease_closed` for each lease and `batch_closed` when multiple leases are processed (includes lease_count, closed_by, settled_amounts)
- Emits `stream_settled` for each stream that paid out
- Transfers accrued amount to provider payout address
- Sets lease state to CLOSED
- **Credit exhaustion**: When one of a lease's streams is frozen for exhausted credit (by this transaction's settlement, or already before it), the lease's `closure_reason` is set to "credit exhausted" and `closed_by` to `credit_exhaustion`, whatever `--reason` says. The exception is a charge the credit exactly covered: that is a full payment, and the caller's reason and role are kept
- Leases whose stream is frozen do not need to be closed by hand: the EndBlocker closes them (see [Events](#events), `lease_auto_closed`)

---

#### withdraw

Withdraw the funds accrued by a provider's settlement streams.

Accrual is settled per **stream**: one stream per `(provider, tenant, denom)`, aggregating every active lease the tenant holds with the provider in that denom. A withdrawal settles the provider's streams, paginated, and its cost depends on the number of streams, not on the number of leases.

```bash
manifestd tx billing withdraw --provider [provider-uuid] [flags]
```

The command takes no positional arguments.

> **Deprecated:** withdrawing from individual leases (`withdraw [lease-uuid]...`) is no longer supported. The CLI rejects positional arguments, and a `MsgWithdraw` with a non-empty `lease_uuids` is rejected by the chain. Use `--provider`.

**Flags:**
| Flag | Type | Default | Description |
|------|------|---------|-------------|
| --provider | string | - | Provider UUID whose streams are withdrawn (required) |
| --limit | uint64 | 0 | Maximum streams to settle (0 = default 50, max 100) |
| --key | string | "" | Base64 `next_key` from the previous withdraw response; continues paging |

**Examples:**
```bash
# Withdraw from all of the provider's streams (first page)
manifestd tx billing withdraw --provider 01912345-6789-7abc-8def-0123456789ab --from provider-key

# Withdrawal with custom limit
manifestd tx billing withdraw --provider 01912345-6789-7abc-8def-0123456789ab --limit 100 --from provider-key

# Continue paging using the next_key from the previous response
manifestd tx billing withdraw --provider 01912345-6789-7abc-8def-0123456789ab --key <base64-next-key> --from provider-key
```

**Authorization:** Provider address or authority.

**Notes:**
- Settles up to `limit` streams of the provider, in key order (tenant address, then denom)
- For each stream, transfers what the stream accrued since its `last_settled_at` — its carried `remainder` plus `rate ×` the elapsed time, to the nanosecond, in whole units of the denom — capped at the tenant's credit balance, to the provider's payout address: one bank transfer per stream, whatever the number of leases it aggregates. What is less than one unit is carried in the stream's `remainder` to the next settlement, and `last_settled_at` is set to the block time
- Response includes `has_more` and an opaque `next_key` cursor if more streams remain
- Pass the returned `next_key` back as `--key` on the next call and repeat until `has_more` is false. Calling again *without* `--key` restarts from the first stream and never advances past `limit`.
- The decoded cursor may not exceed 512 bytes (`MaxWithdrawCursorLen`); a cursor that does not decode is rejected
- Each stream settles independently: a stream that fails to settle is logged and skipped, and does not fail the transaction
- Response `withdrawal_count` is the number of streams that paid out, and `total_amounts` the sum of what they paid, per denom
- A withdrawal with nothing accrued succeeds with empty `total_amounts` and `withdrawal_count` 0
- **Credit exhaustion**: a stream whose accrual consumes the tenant's whole balance pays what is left and is frozen. The withdrawal closes no lease; the EndBlocker closes the frozen stream's leases (at most 100 closures per block, shared between all frozen streams) and emits `lease_auto_closed` for each
- Emits `stream_settled` for each stream that paid out, `stream_exhausted` for each stream it froze, and one `batch_withdraw` summary event
- See [Provider-Wide Withdraw Workflow](#provider-wide-withdraw-workflow) below for example

##### Provider-Wide Withdraw Workflow

When a provider has more streams than fit in one call (more than `--limit` tenant and denom pairs), use cursor pagination to withdraw from all. Each response returns an opaque `next_key`; pass it back as `--key` on the next call to advance. Calling again **without** `--key` restarts the scan from the first stream and never gets past `limit`. `next_key` is a `bytes` value, so it appears base64-encoded in the JSON output — pass that string verbatim to `--key` (it is an opaque encoding of a tenant and denom, not a UUID).

```bash
# Step 1: Initial withdrawal (settles up to 100 streams), no cursor
manifestd tx billing withdraw --provider 01912345-6789-7abc-8def-0123456789ab --limit 100 --from provider-key

# Response example (MsgWithdrawResponse):
# {
#   "total_amounts": [{"denom": "upwr", "amount": "5000000"}],
#   "payout_address": "manifest1payout...",
#   "withdrawal_count": "100",      <-- Streams that paid out
#   "has_more": true,               <-- More streams remain
#   "next_key": "FAECAwQF..."       <-- Opaque cursor for the next page
# }

# Step 2: Continue by passing next_key back as --key, until has_more is false
manifestd tx billing withdraw --provider 01912345-6789-7abc-8def-0123456789ab --limit 100 --key FAECAwQF... --from provider-key

# Response:
# {
#   "total_amounts": [{"denom": "upwr", "amount": "2500000"}],
#   "payout_address": "manifest1payout...",
#   "withdrawal_count": "50",
#   "has_more": false               <-- All streams processed (next_key empty)
# }
```

**Automation Script Example (bash):**
```bash
#!/bin/bash
PROVIDER_UUID="01912345-6789-7abc-8def-0123456789ab"
HAS_MORE=true
KEY=""

while [ "$HAS_MORE" = "true" ]; do
  KEY_ARG=""
  [ -n "$KEY" ] && KEY_ARG="--key $KEY"
  RESULT=$(manifestd tx billing withdraw --provider $PROVIDER_UUID --limit 100 $KEY_ARG --from provider-key -o json -y)
  HAS_MORE=$(echo $RESULT | jq -r '.has_more')
  KEY=$(echo $RESULT | jq -r '.next_key // ""')   # pass this back as --key on the next call
  echo "Withdrew from $(echo $RESULT | jq -r '.withdrawal_count') streams, has_more=$HAS_MORE"
done
echo "All withdrawals complete"
```

---

#### set-item-custom-domain

Set or clear the `custom_domain` on a specific lease item, addressed by `service_name`. Available since v2.1.0.

```bash
manifestd tx billing set-item-custom-domain [lease-uuid] [service-name] [domain] [flags]
```

**Arguments:**
| Argument | Type | Description |
|----------|------|-------------|
| lease-uuid | string | UUID of the lease that owns the target item |
| service-name | string | `service_name` of the target item; pass `""` for a 1-item legacy lease |
| domain | string | FQDN to set, or `""` to clear |

**Examples:**
```bash
# 1-item legacy lease (item.service_name == "")
manifestd tx billing set-item-custom-domain 01902a9b-1234-7000-8000-000000000001 "" app.example.com --from tenant-key

# multi-item lease, target the "web" item
manifestd tx billing set-item-custom-domain 01902a9b-1234-7000-8000-000000000001 web app.example.com --from tenant-key

# clear the domain on the "web" item
manifestd tx billing set-item-custom-domain 01902a9b-1234-7000-8000-000000000001 web "" --from tenant-key
```

**Constraints:**
- Sender must be the lease tenant, the module authority, or an address in `params.allowed_list`.
- Lease must be in `PENDING` or `ACTIVE` state. Closed/rejected/expired leases are immutable.
- Multi-item legacy leases (no `service_name`s) cannot set `custom_domain` — recreate in service-name mode.
- Domain must pass `IsValidFQDN`: 1–253 bytes, lowercase, ≥ 1 dot separator, each label is RFC 1123 (1–63 alphanumerics + hyphens, no leading/trailing hyphen), TLD has at least one non-digit, no scheme/path/whitespace/`@`/`*`/`?`/`#`/leading or trailing dot.
- Domain must not match any entry in `params.reserved_domain_suffixes` (case-insensitive, label-boundary suffix check; entries also match their apex).
- Domain must be globally unique across all PENDING/ACTIVE leases. Re-setting the same domain on the same item is idempotent (no state change, no event).

**Notes:**
- Emits `lease_custom_domain_set` (with `set_by` ∈ `{tenant, authority, allowed}`) on a successful set, or `lease_custom_domain_cleared` on clear. No event is emitted for an idempotent re-set or a clear of an already-empty domain.
- The transaction requires lowercase `custom_domain` — `MsgSetItemCustomDomain.ValidateBasic()` rejects mixed case before the keeper runs. Lower-case any user-supplied input client-side. The keeper does its own `strings.ToLower(strings.TrimSpace(...))` as defence-in-depth on the storage path, but you can't rely on it as a normalisation point for input.
- Closing, rejecting, expiring, or auto-closing the lease frees the index entry automatically.

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
| --allowed-list | string | Comma-separated addresses with privileged authority for `create-lease-for-tenant` and `set-item-custom-domain`. **Preserve-on-omit**: omit the flag to round-trip the current on-chain value unchanged. Pass `--allowed-list=""` to explicitly clear. |
| --reserved-domain-suffixes | string | Comma-separated DNS suffixes (each beginning with `.`) that tenants are forbidden from claiming via `set-item-custom-domain`. Same **preserve-on-omit** semantics as `--allowed-list`. |

**Examples:**
```bash
# Update only numeric params; allowed_list and reserved_domain_suffixes are preserved unchanged.
manifestd tx billing update-params \
  100 20 3600 10 1800 \
  --from authority

# Update numeric params and overwrite both lists.
manifestd tx billing update-params \
  100 20 3600 10 1800 \
  --allowed-list "manifest1abc...,manifest1def..." \
  --reserved-domain-suffixes ".barney0.manifest0.net,.lifted.app" \
  --from authority

# Explicitly clear reserved_domain_suffixes (numeric-only updates would preserve it).
manifestd tx billing update-params \
  100 20 3600 10 1800 \
  --reserved-domain-suffixes="" \
  --from authority
```

**Reserved-suffix validation:** each entry must begin with `.`, the substring after the dot must be a valid FQDN, and duplicates are rejected.

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
    "allowed_list": [],
    "reserved_domain_suffixes": []
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
- `closed_at` is set when lease is closed (CLOSED state). It is the time of the block that closed the lease
- `last_settled_at` is **deprecated** and never set: a lease has no settlement time of its own. Settlement is tracked per stream; query `streams-by-tenant` or `streams-by-provider` for a stream's `last_settled_at`
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

> **Removed.** `manifestd query billing withdrawable [lease-uuid]` no longer exists. Settlement is per stream, so a lease has no withdrawable amount of its own. The underlying gRPC method `Query/WithdrawableAmount` is deprecated and returns `Unimplemented`. Use [`provider-withdrawable`](#provider-withdrawable) for the amount a provider can withdraw, or [`streams-by-provider`](#streams-by-provider) for the per-tenant, per-denom breakdown.

---

#### provider-withdrawable

Query withdrawable amounts for a provider across the provider's settlement streams, one page at a time. **This query calculates real-time accrued amounts.**

```bash
manifestd query billing provider-withdrawable [provider-uuid]

# With custom limit (default: 100, max: 1000)
manifestd query billing provider-withdrawable [provider-uuid] --limit 500

# Next page: pass the previous response's pagination.next_key, as printed (base64)
manifestd query billing provider-withdrawable [provider-uuid] --limit 500 --page-key <base64-next-key>
```

**Arguments:**
| Argument | Type | Description |
|----------|------|-------------|
| provider-uuid | string | UUID of the provider |

**Flags:** standard pagination flags (`--page-key`, `--limit`, `--offset`, `--count-total`, `--reverse`). Page size defaults to 100, capped at 1000. Iterates the provider's streams.

`--page-key` takes the base64 `next_key` exactly as the previous response printed it. A stream key contains raw address bytes, which cannot be typed on a command line, so the CLI decodes the base64 for you; a value that is not valid base64 is rejected with `invalid --page-key`. The same holds for `streams-by-provider` and `streams-by-tenant`.

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
  "pagination": {
    "next_key": null,
    "total": "0"
  }
}
```

**Fields:**
| Field | Description |
|-------|-------------|
| `amounts` | Sum, per denom, of what settling each stream of the page would pay now: the stream's accrual capped at its tenant's credit balance. A frozen stream contributes nothing. |
| `lease_count` | Number of leases covered by the streams of the page that have a non-zero withdrawable amount. It is not the number of transfers a withdrawal would make (that is the number of streams). |

When `pagination.next_key` is non-empty, `amounts` is a partial total: re-query passing that base64 string verbatim as `--page-key` and sum the per-page `amounts` until `next_key` is empty. This is the read-only complement to `MsgWithdraw`.

**Note:** This calculates the real-time total withdrawable amount across the provider's streams, with the same exact arithmetic as a settlement (carried `remainder` plus `rate ×` elapsed time, in whole units of the denom). It is a read-only query and does NOT trigger actual settlement. For providers with many streams, paginate as shown above (thread `pagination.next_key` via `--page-key`) to process in batches.

---

#### streams-by-provider

Query the settlement streams of a provider. A stream aggregates every active lease one tenant holds with the provider in one denom.

```bash
manifestd query billing streams-by-provider [provider-uuid]

# Next page: pass the previous response's pagination.next_key, as printed (base64)
manifestd query billing streams-by-provider [provider-uuid] --page-key <base64-next-key>
```

**Arguments:**
| Argument | Type | Description |
|----------|------|-------------|
| provider-uuid | string | UUID of the provider |

**Flags:** standard pagination flags (`--page-key`, `--limit`, `--offset`, `--count-total`, `--reverse`). Page size defaults to 100, capped at 1000. `--page-key` takes the base64 `next_key` exactly as printed by the previous response.

**Response:**
```json
{
  "streams": [
    {
      "provider_uuid": "01912345-6789-7abc-8def-fedcba987654",
      "tenant": "manifest1abc...",
      "denom": "upwr",
      "state": {
        "rate": "200",
        "last_settled_at": "2024-01-01T00:10:00.250Z",
        "lease_count": "2",
        "remainder": "50000000"
      }
    }
  ],
  "pagination": {
    "next_key": null,
    "total": "0"
  }
}
```

**Fields (`state`):**
| Field | Description |
|-------|-------------|
| `rate` | Summed per-second rate of the stream's ACTIVE leases, in `denom` |
| `last_settled_at` | Time up to which the stream has been paid: the block time of its last settlement. |
| `lease_count` | Number of ACTIVE leases the stream aggregates |
| `exhausted_at` | Present only on a frozen stream: the block time at which a settlement found the tenant's credit exhausted. A frozen stream no longer accrues, and its leases are closed by the EndBlocker. |
| `remainder` | Accrual carried to the next settlement, in (denom units × nanoseconds): 1,000,000,000 is one whole unit of `denom`. After a settlement it is what did not amount to a whole unit, so it is below 1,000,000,000. It holds a whole unit or more only when the stream carries a debt that could not be paid (see [Stream](#stream)). Never negative. |

**Note:** This returns stored state. What a stream has accrued at time `T` is exactly `(remainder + rate × nanoseconds between last_settled_at and T) / 1,000,000,000`, rounded down, capped at the tenant's credit balance in `denom`. `rate × seconds elapsed` is a close approximation of it: it only ignores the carried `remainder`, which is worth less than one unit, or less than one nanosecond of the stream's rate for a stream carrying an unpaid debt. `provider-withdrawable` computes the exact sum for you.

---

#### streams-by-tenant

Query the settlement streams paid by a tenant: one per provider and denom the tenant has active leases with.

```bash
manifestd query billing streams-by-tenant [tenant]

# Next page: pass the previous response's pagination.next_key, as printed (base64)
manifestd query billing streams-by-tenant [tenant] --page-key <base64-next-key>
```

**Arguments:**
| Argument | Type | Description |
|----------|------|-------------|
| tenant | string | Bech32 address of the tenant |

**Flags:** standard pagination flags (`--page-key`, `--limit`, `--offset`, `--count-total`, `--reverse`). Page size defaults to 100, capped at 1000. `--page-key` takes the base64 `next_key` exactly as printed by the previous response.

**Response:** same shape as [`streams-by-provider`](#streams-by-provider).

**Note:** A tenant can use this to see whether one of its streams is frozen (`exhausted_at` set): the leases behind a frozen stream are about to be closed and a top-up will not save them.

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
| `current_balance` | Tenant's current credit balance, in the denoms of all its streams, frozen ones included. It still includes what the streams have accrued but not yet settled. |
| `total_rate_per_second` | Combined burn rate of the tenant's non-frozen streams (per denom) |
| `estimated_duration_seconds` | Seconds until credit exhaustion (minimum across all denoms), after deducting what the streams have already accrued but not yet settled |
| `active_lease_count` | Number of currently active leases (from the credit account) |

**Notes:**
- The estimate is calculated in real-time from the tenant's settlement streams, not from its leases
- If the tenant has no accruing stream, `estimated_duration_seconds` will be `0` and `total_rate_per_second` will be empty. `current_balance` still lists the denoms of any frozen streams
- With multi-denom support, the estimate returns the minimum duration across all denominations (the limiting factor)
- **Unsettled accrual is deducted**: per denom, `remaining = balance − accrued since each stream's last_settled_at` (the exact accrual, including each stream's carried `remainder`, in whole units), and the estimate is `remaining ÷ rate`. If nothing remains in some denom, the estimate is `0`
- **Sums are checked, not assumed to fit**: per-denom rates and unsettled accruals are added with overflow checks. If a stream's unsettled accrual, or the sum of the accruals of the tenant's streams in a denom, is not representable, the estimate is `0` (such a debt certainly exceeds the balance). If the summed rate itself is not representable, the query fails with gRPC `OutOfRange`.
- **Frozen streams are left out of the burn rate**: a stream frozen for exhausted credit no longer accrues, so it contributes neither to `total_rate_per_second` nor to the estimate. Its denom is still reported in `current_balance`. Its leases are still counted in `active_lease_count` until the EndBlocker closes them

**Limitations:**
- **Bounded stream iteration**: The walk over the tenant's streams is bounded by `(max_leases_per_tenant + max_pending_leases_per_tenant) × max_items_per_lease` — an upper bound on the streams a tenant can reach, with the lease term clamped to the params' upper bounds (10,000 / 1,000) as a hard DoS ceiling.
- **Assumes constant rate**: The estimate assumes all current leases continue at their current rates. Actual duration may differ if leases are closed or new leases are created.

---

#### lease-by-domain

Look up the active or pending lease that has claimed a given `custom_domain`, and the `service_name` of the matching item.

```bash
manifestd query billing lease-by-domain [custom-domain]
```

**Arguments:**
| Argument | Type | Description |
|----------|------|-------------|
| custom-domain | string | FQDN to look up (case-insensitive; the chain stores the canonical lower-cased form) |

**Example:**
```bash
manifestd query billing lease-by-domain app.example.com
```

**Response:**
```json
{
  "lease": {
    "uuid": "01902a9b-1234-7000-8000-000000000001",
    "tenant": "manifest1abc...",
    "provider_uuid": "01912345-6789-7abc-8def-fedcba987654",
    "items": [ /* ... */ ],
    "state": "LEASE_STATE_ACTIVE"
  },
  "service_name": "web"
}
```

**Notes:**
- Returns the lease and the `service_name` of the LeaseItem that owns the domain. For a 1-item legacy lease the `service_name` is `""`.
- Returns gRPC `NotFound` (CLI: error) when no PENDING/ACTIVE item has claimed the domain.
- Backed by the `CustomDomainIndex` reverse-lookup (O(1)); domains held by closed/rejected/expired leases are not indexed.

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
  rpc SetItemCustomDomain(MsgSetItemCustomDomain) returns (MsgSetItemCustomDomainResponse);
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
  string sku_uuid = 1;      // UUIDv7 of SKU
  uint64 quantity = 2;
  string service_name = 3;  // Optional RFC 1123 DNS label for stack deployments
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
  google.protobuf.Timestamp closed_at = 1;                       // When leases were closed (block time)
  uint64 closed_count = 2;                                       // Number of leases closed
  repeated cosmos.base.v1beta1.Coin total_settled_amounts = 3;   // What the settled streams paid out, per denom
}
```

**Constraints:**
- All leases must be in ACTIVE state
- All leases must pass authorization for the sender
- Maximum 100 leases per call
- Atomic: all succeed or all fail
- Reason is stored on each lease's `closure_reason` field, unless one of the lease's streams is frozen for exhausted credit: the lease is then recorded with `closure_reason = "credit exhausted"` (and its `lease_closed` event with `closed_by = credit_exhaustion`). A charge the credit exactly covered is a full payment and keeps the caller's reason and role

**Settlement:**
- Each stream the closed leases belong to is settled once, up to the block time, before the leases are removed from it. A stream left without leases is deleted
- `total_settled_amounts` is the sum of those stream transfers. A stream aggregates all the active leases of a tenant with a provider in a denom, so closing 1 of N leases of a stream pays the provider the accrual of all N
- `closed_at`, in the response and on each lease, is the block time

---

#### MsgWithdraw

Withdraw what a provider's settlement streams have accrued. Accrual is settled per `(provider, tenant, denom)` stream, so the cost depends on the number of streams, not the number of leases. Withdrawal is paginated over the provider's streams; each stream settles independently, and a stream that fails to settle is skipped.

**Request:**
```protobuf
message MsgWithdraw {
  string sender = 1;               // Provider or authority
  repeated string lease_uuids = 2 [deprecated = true]; // Rejected if non-empty; use provider_uuid
  string provider_uuid = 3;        // Provider whose streams are withdrawn (required)
  uint64 limit = 4;                // Max streams to settle (0 = default 50, max 100)
  bytes key = 5;                   // Opaque cursor from the previous response's next_key; base64 in JSON. Empty starts from the beginning.
}
```

> **Deprecated:** `lease_uuids` selected individual leases to withdraw from. Settlement is per stream, so a single lease can no longer be withdrawn on its own. A message with a non-empty `lease_uuids` is rejected with `ErrInvalidRequest`; use `provider_uuid`.

**Response:**
```protobuf
message MsgWithdrawResponse {
  repeated cosmos.base.v1beta1.Coin total_amounts = 1;  // Total withdrawn across the settled streams, per denom
  string payout_address = 2;        // Destination address
  uint64 withdrawal_count = 3;      // Number of streams that paid out
  bool has_more = 4;                // The provider has more streams to process
  bytes next_key = 5;               // Opaque cursor; pass as MsgWithdraw.key for the next page. Non-empty iff has_more is true.
}
```

**Constraints:**
- `provider_uuid` is required and must be a valid UUID; the sender must be the provider's address or the authority
- `limit` above 100 is rejected
- `key` longer than 512 bytes (`MaxWithdrawCursorLen`), or one that does not decode, is rejected with `ErrInvalidRequest`. The cursor only carries a tenant and a denom; the provider always comes from `provider_uuid`
- Not atomic: each stream settles in its own cached context, and a stream that fails is logged and skipped
- A withdrawal that finds nothing accrued succeeds with empty `total_amounts`
- No lease is closed by this message. A stream whose tenant's credit is exhausted is frozen, and the EndBlocker closes its leases

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

#### MsgSetItemCustomDomain

Set or clear `custom_domain` on a specific lease item identified by `service_name`. Available since v2.1.0.

**Authorisation.** `sender` must be the lease tenant, the module authority, or an address in `params.allowed_list`.

**Request:**
```protobuf
message MsgSetItemCustomDomain {
  string sender = 1;        // Tenant, authority, or allowed_list member
  string lease_uuid = 2;    // Target lease (must be PENDING or ACTIVE)
  string service_name = 3;  // Item addressing key; "" for a 1-item legacy lease
  string custom_domain = 4; // FQDN to set, or "" to clear
}
```

**Response:**
```protobuf
message MsgSetItemCustomDomainResponse {}
```

**Behaviour notes:**
- Lease state must be `PENDING` or `ACTIVE` (`ErrLeaseNotEditable` otherwise).
- Multi-item legacy leases (no `service_name`s) cannot use `custom_domain` (`ErrAmbiguousLeaseItem`).
- Empty `custom_domain` clears the field and removes the `CustomDomainIndex` entry.
- `custom_domain` is normalised on write (`strings.ToLower` + `TrimSpace`).
- A non-empty domain is validated by `IsValidFQDN` and rejected if it matches any `params.reserved_domain_suffixes` entry.
- Re-setting the same domain on the same `(lease_uuid, service_name)` is idempotent (no event, no state change).
- Cross-lease and within-lease cross-item collisions are rejected with `ErrCustomDomainAlreadyClaimed`.

**Emitted events:**
- `lease_custom_domain_set` on a successful set.
- `lease_custom_domain_cleared` on a successful clear (with the previous value in `custom_domain`).

Both carry attributes `lease_uuid`, `tenant`, `provider_uuid` (v2.2.0+), `service_name`, `custom_domain`, `set_by`. `set_by` ∈ `{tenant, authority, allowed}` records which authorisation path matched.

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
  rpc WithdrawableAmount(QueryWithdrawableAmountRequest) returns (QueryWithdrawableAmountResponse) {
    option deprecated = true; // returns Unimplemented; use ProviderWithdrawable or StreamsByProvider
  }
  rpc ProviderWithdrawable(QueryProviderWithdrawableRequest) returns (QueryProviderWithdrawableResponse);
  rpc StreamsByProvider(QueryStreamsByProviderRequest) returns (QueryStreamsByProviderResponse);
  rpc StreamsByTenant(QueryStreamsByTenantRequest) returns (QueryStreamsByTenantResponse);
  rpc LeaseByCustomDomain(QueryLeaseByCustomDomainRequest) returns (QueryLeaseByCustomDomainResponse);
}
```

**Important Note:** Queries return stored state and do NOT trigger settlement, freeze a stream or close a lease. `ProviderWithdrawable` and `CreditEstimate` additionally calculate real-time accrued amounts from the streams, based on elapsed time. Settlement (actual token transfer) only happens during write operations (Withdraw, AcknowledgeLease, CloseLease, and the EndBlocker closing leases of frozen streams). Only ACTIVE leases accrue charges.

**Deprecated: `WithdrawableAmount`.** Settlement is per stream, so a lease has no withdrawable amount of its own. The method and its REST route are kept so that existing clients get an explicit error instead of a missing-method failure: every call returns gRPC `Unimplemented` with a message naming the replacement. Use `ProviderWithdrawable` (amount a provider can withdraw) or `StreamsByProvider` (per-tenant, per-denom streams).

#### QueryProviderWithdrawable

Get the amount a provider can withdraw, paginated over its streams.

**Endpoint:** `liftedinit.billing.v1.Query/ProviderWithdrawable`

**Request:**
```protobuf
message QueryProviderWithdrawableRequest {
  string provider_uuid = 1;
  reserved 2; // was: uint64 limit (superseded by pagination.limit)
  cosmos.base.query.v1beta1.PageRequest pagination = 3; // over the provider's streams; default 100, max 1000
}
```

**Response:**
```protobuf
message QueryProviderWithdrawableResponse {
  repeated cosmos.base.v1beta1.Coin amounts = 1;  // Withdrawable across the streams of this page, per denom
  uint64 lease_count = 2;                         // Leases covered by this page's streams with a non-zero amount
  reserved 3; // was: bool has_more (superseded by pagination.next_key)
  cosmos.base.query.v1beta1.PageResponse pagination = 4;
}
```

Loop until `pagination.next_key` is empty and sum `amounts` for the provider's full withdrawable total.

---

#### QueryStreamsByProvider / QueryStreamsByTenant

List settlement streams, by the provider being paid or by the tenant paying.

**Endpoints:** `liftedinit.billing.v1.Query/StreamsByProvider`, `liftedinit.billing.v1.Query/StreamsByTenant`

**Request:**
```protobuf
message QueryStreamsByProviderRequest {
  string provider_uuid = 1;
  cosmos.base.query.v1beta1.PageRequest pagination = 2; // default 100, max 1000
}

message QueryStreamsByTenantRequest {
  string tenant = 1;
  cosmos.base.query.v1beta1.PageRequest pagination = 2; // default 100, max 1000
}
```

**Response** (both):
```protobuf
message QueryStreamsByProviderResponse {
  repeated Stream streams = 1;
  cosmos.base.query.v1beta1.PageResponse pagination = 2;
}
```

See [Stream](#stream) for the `Stream` type.

---

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
  LeaseState state_filter = 2;  // Optional filter by state
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
  repeated cosmos.base.v1beta1.Coin available_balances = 3;  // Available for new leases (balances - reserved_amounts)
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

#### QueryLeaseByCustomDomain

Look up the PENDING/ACTIVE lease (and the `service_name` of the matching item) that owns a given `custom_domain`.

**Endpoint:** `liftedinit.billing.v1.Query/LeaseByCustomDomain`

**Request:**
```protobuf
message QueryLeaseByCustomDomainRequest {
  string custom_domain = 1;
}
```

**Response:**
```protobuf
message QueryLeaseByCustomDomainResponse {
  Lease lease = 1;
  string service_name = 2;  // "" for a 1-item legacy lease
}
```

**Notes:**
- The query lower-cases and trims `custom_domain` before lookup.
- Backed by `CustomDomainIndex` (O(1)). Closing/rejecting/expiring/auto-closing the lease frees the index entry, so domains held by terminal leases are not returned.
- Returns gRPC `NotFound` when no claim exists.

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
| GET | `/lease/{lease_uuid}/withdrawable` | **Deprecated.** Always fails with gRPC `Unimplemented` (HTTP 501). Use the two provider endpoints below. |
| GET | `/provider/{provider_uuid}/withdrawable` | Get provider total withdrawable (paginated over streams) |
| GET | `/provider/{provider_uuid}/streams` | List the provider's settlement streams |
| GET | `/credit/{tenant}/streams` | List the settlement streams paid by a tenant |
| GET | `/lease/by-domain/{custom_domain}` | Look up lease by custom domain (v2.1.0+) |

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
curl "http://localhost:1317/liftedinit/billing/v1/leases?state_filter=2&pagination.limit=10"
```

**Get Credit Account:**
```bash
curl http://localhost:1317/liftedinit/billing/v1/credit/manifest1abc...
```

**Get Provider Withdrawable Amount:**
```bash
curl http://localhost:1317/liftedinit/billing/v1/provider/01912345-6789-7abc-8def-fedcba987654/withdrawable
```

**List a Provider's Streams:**
```bash
curl "http://localhost:1317/liftedinit/billing/v1/provider/01912345-6789-7abc-8def-fedcba987654/streams?pagination.limit=100"
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
  google.protobuf.Timestamp closed_at = 7;
  google.protobuf.Timestamp last_settled_at = 8 [deprecated = true]; // Never set; see Stream
  google.protobuf.Timestamp acknowledged_at = 9;
  google.protobuf.Timestamp rejected_at = 10;
  string rejection_reason = 11;       // Provider's rejection reason (max 256 chars)
  google.protobuf.Timestamp expired_at = 12;
  string closure_reason = 13;         // Closure reason (max 256 chars)
  bytes meta_hash = 14;               // Hash/reference to off-chain deployment data (max 64 bytes, immutable)
  uint64 min_lease_duration_at_creation = 15; // Snapshot of min_lease_duration param at creation
}
```

**Field Notes:**
- `rejection_reason`: Set when a provider rejects a PENDING lease via `MsgRejectLease`. Contains the provider's explanation for rejecting the lease (e.g., "resources unavailable", "invalid configuration"). Maximum 256 characters. Only present when `state` is `LEASE_STATE_REJECTED`.
- `closed_at`: The time of the block in which the lease was closed. For a lease closed for exhausted credit this is the block in which the EndBlocker (or a `MsgCloseLease`) closed it, which is later than the moment the credit ran out.
- `last_settled_at`: **Deprecated.** Settlement is tracked per stream (see [Stream](#stream)), not per lease. The field is never set by the node, and a genesis file in which a lease carries it is rejected.
- `closure_reason`: Set when a lease is closed via `MsgCloseLease` with a reason, or automatically set to `"credit exhausted"` when the lease is closed because one of its streams was frozen for exhausted credit (by the EndBlocker, or by a `MsgCloseLease` on such a lease). Maximum 256 characters. Only present when `state` is `LEASE_STATE_CLOSED`.
- `meta_hash`: Optional immutable hash or reference linking to off-chain deployment data (e.g., deployment manifest hash, configuration reference). Set at lease creation and cannot be modified afterward. Maximum 64 bytes to accommodate SHA-256 or SHA-512 hashes.
- `min_lease_duration_at_creation`: Snapshot of the `min_lease_duration` parameter at the time this lease was created. Used to calculate consistent credit reservations (`reservation = sum(locked_price × quantity) × min_lease_duration_at_creation`) regardless of subsequent governance changes to the parameter. This ensures existing reservations remain valid when parameters are updated.

### LeaseItem

```protobuf
message LeaseItem {
  string sku_uuid = 1;                         // SKU UUID
  uint64 quantity = 2;                         // Number of instances
  cosmos.base.v1beta1.Coin locked_price = 3;   // Per-second rate locked at creation
  string service_name = 4;                     // Optional RFC 1123 DNS label for stack deployments
  string custom_domain = 5;                    // Optional FQDN routed to this item (v2.1.0+, max 253 bytes)
}
```

**Field notes:**
- `custom_domain`: Optional fully-qualified domain name routed to this item's container by the provider. Set or cleared via `MsgSetItemCustomDomain` (not via lease creation). Validated by `IsValidFQDN` (≤253 bytes, lowercase, ≥1 dot, RFC 1123 labels, non-numeric TLD) and rejected if it matches any `params.reserved_domain_suffixes` entry. Globally unique across PENDING/ACTIVE leases — enforced by the `CustomDomainIndex` reverse-lookup. Cleared automatically when the lease closes/rejects/expires.

### CustomDomainTarget

The value type stored in `CustomDomainIndex` (the reverse lookup keyed by domain). Returned indirectly by `QueryLeaseByCustomDomain`.

```protobuf
message CustomDomainTarget {
  string lease_uuid = 1;
  string service_name = 2;  // "" for a 1-item legacy lease
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

### Stream

A settlement stream: the aggregate of every ACTIVE lease one tenant holds with one provider in one denom. Accrual is settled once per stream instead of once per lease. Returned by `StreamsByProvider` / `StreamsByTenant` and carried in genesis.

```protobuf
message Stream {
  string provider_uuid = 1;  // Provider being paid
  string tenant = 2;         // Tenant whose credit account pays the stream
  string denom = 3;          // Denomination the stream accrues in
  StreamState state = 4;     // Settlement aggregate
}

message StreamState {
  string rate = 1;                             // cosmos.Int: summed per-second rate of the stream's ACTIVE leases
  google.protobuf.Timestamp last_settled_at = 2; // Time up to which accrual has been settled
  uint64 lease_count = 3;                      // Number of ACTIVE leases contributing to rate
  google.protobuf.Timestamp exhausted_at = 4;  // Set when the tenant's credit was found exhausted
  string remainder = 5;                        // cosmos.Int: accrual carried to the next settlement (1,000,000,000 = one unit of the denom)
}
```

**Field Notes:**
- `rate`: `sum(locked_price × quantity)` over the items priced in `denom` of all ACTIVE leases of `tenant` with `provider_uuid`.
- `last_settled_at`: Set to the block time by every settlement, so it always equals the time of the stream's last settlement.
- `remainder`: Rates are per second and time is tracked to the nanosecond, so it is expressed in (denom units × nanoseconds): 1,000,000,000 is one whole unit of the denom. A settlement at time `T` computes `total = remainder + rate × nanoseconds since last_settled_at`, pays `total / 1,000,000,000` (capped at the tenant's credit balance) and stores `total % 1,000,000,000` as the new `remainder`, which is therefore below 1,000,000,000 after a settlement. No fraction of a second is dropped between settlements, and a lease that joins a stream is never charged for time before it joined. It can hold a whole unit or more only when the stream carries a debt that could not be paid: the 2 → 3 store migration dates such a stream back and keeps in `remainder` what is left of the debt, which is smaller than the stream's rate. The next settlement pays it along with what accrued since. Genesis validation rejects a negative value and sets no upper bound.
- `tenant` (on `Stream`): Always the canonical lower-case bech32 spelling of the tenant's address. Bech32 is also valid in upper case and a lease stores its tenant as it was sent, so `Lease.tenant` may be upper case for the same account. Compare them as addresses, not as strings.
- `exhausted_at`: Unset on a healthy stream. Once set, the stream is frozen: it stops accruing, a top-up does not unfreeze it, and its leases are closed by the EndBlocker. The stream is deleted when its last lease is closed.
- A stream exists if and only if `lease_count > 0`.

### Params

```protobuf
message Params {
  uint64 max_leases_per_tenant = 1;
  repeated string allowed_list = 2;
  uint64 max_items_per_lease = 3;
  uint64 min_lease_duration = 4;
  uint64 max_pending_leases_per_tenant = 5;
  uint64 pending_timeout = 6;
  repeated string reserved_domain_suffixes = 7; // v2.1.0+
}
```

**Field notes:**
- `allowed_list`: Addresses with privileged authority for `MsgCreateLeaseForTenant` and `MsgSetItemCustomDomain`, in addition to the module authority.
- `reserved_domain_suffixes`: DNS suffixes (each must begin with `.`) that tenants are forbidden from claiming as a `LeaseItem.custom_domain`. Match is case-insensitive at a label boundary, plus the apex (e.g. `.foo.example` matches both `app.foo.example` and `foo.example`). Each entry's substring after the leading dot must itself be a valid FQDN. Tunable via `MsgUpdateParams`.

**Defaults and validation bounds (numeric params):**
| Param | Default | Valid range |
|-------|---------|-------------|
| `max_leases_per_tenant` | 100 | 1 – 10,000 (`MaxLeasesPerTenantUpperBound`) |
| `max_items_per_lease` | 20 | 1 – 100 (`MaxItemsPerLeaseHardLimit`) |
| `min_lease_duration` | 3600 | 1 – 2,592,000 seconds / 30 days (`MaxMinLeaseDuration`) |
| `max_pending_leases_per_tenant` | 10 | 1 – 1,000 (`MaxPendingLeasesPerTenantUpperBound`) |
| `pending_timeout` | 1800 | 60 (`MinPendingTimeout`) – 86,400 (`MaxPendingTimeout`) seconds |

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
| `lease_closed` | lease_uuid, tenant, provider_uuid, closed_by, active_lease_count, closure_reason (optional) | Lease closed by `MsgCloseLease` |
| `batch_closed` | lease_count, closed_by, settled_amounts | Batch summary when multiple leases closed by one `MsgCloseLease`. `settled_amounts` is what the settled streams paid out |
| `lease_auto_closed` | lease_uuid, tenant, provider_uuid, reason | Lease closed by the EndBlocker because one of its streams was frozen for exhausted credit. Block event (see below). `reason` is always `credit_exhausted` |
| `stream_settled` | provider_uuid, tenant, amount, payout_address | A stream settlement transferred `amount` (a single coin) to the provider. One event per paying settlement; not emitted when nothing was transferred |
| `stream_exhausted` | provider_uuid, tenant, denom | A settlement found the tenant's credit exhausted and froze the stream. Its leases will be closed by the EndBlocker |
| `batch_withdraw` | provider_uuid, amount, stream_count, payout_address, exhausted_count | Summary of one `MsgWithdraw`, always emitted. `amount` is the total withdrawn (all denoms), `stream_count` the number of streams that paid out, `exhausted_count` the number of streams this withdrawal froze |
| `params_updated` | | Module parameters updated |
| `lease_custom_domain_set` | lease_uuid, tenant, provider_uuid, service_name, custom_domain, set_by | `LeaseItem.custom_domain` set or changed (v2.1.0+; `provider_uuid` added v2.2.0+) |
| `lease_custom_domain_cleared` | lease_uuid, tenant, provider_uuid, service_name, custom_domain (previous value), set_by | `LeaseItem.custom_domain` cleared (v2.1.0+; `provider_uuid` added v2.2.0+) |

**Custom-domain `set_by` attribute:** records the role under which the call was authorised. One of `tenant`, `authority`, `allowed`. No event is emitted for an idempotent re-set or a clear of an already-empty domain.

**`lease_closed` `closed_by` attribute:** records who closed the lease. One of `tenant`, `authority`, `provider`, or `credit_exhaustion`. `credit_exhaustion` is the sentinel set when the lease is closed through `MsgCloseLease` while one of its streams is frozen for a genuine credit exhaustion (an unpaid remainder, an accrual overflow, a zero balance with nothing accrued, or a stream already frozen before the message). `closure_reason` is then `credit exhausted`. If the credit exactly covered a positive charge, the caller's role and reason are kept.

**`lease_closed` no longer carries `settled_amounts` or `duration_seconds`.** Settlement is per stream, so there is no per-lease settled amount to report. Use the `stream_settled` events of the same transaction (or `total_settled_amounts` in the response) for what was paid, and the lease's `acknowledged_at` and `closed_at` for its duration.

**`provider_withdraw` is no longer emitted.** There is no per-lease withdrawal. A withdrawal emits one `stream_settled` per stream that paid out, followed by one `batch_withdraw` summary.

**Where `stream_settled` and `stream_exhausted` appear.** Every settlement can emit them, so they appear in `MsgWithdraw`, `MsgAcknowledgeLease` and `MsgCloseLease` transactions, and as block events when the EndBlocker settles a healthy stream while closing a multi-denom lease. A stream is settled at most once per message, so a message emits at most one `stream_settled` and one `stream_exhausted` per stream.

**`lease_auto_closed` is a block event.** Credit exhaustion never closes a lease inside the transaction that discovers it. A settlement freezes the stream (`stream_exhausted`, in the transaction), and the EndBlocker closes the stream's leases, emitting one `lease_auto_closed` per lease. It attempts at most 100 closures per block, a budget shared between all frozen streams. These events are in the block's `finalize_block_events` (CometBFT `NewBlock` / `NewBlockEvents` subscriptions, `block_results` RPC), not in any transaction result, and may be spread over several blocks after the `stream_exhausted` event.

**Credit-exhaustion closures surface as two events:** `lease_auto_closed` (`reason=credit_exhausted`) when the EndBlocker closes the lease, and `lease_closed` (`closed_by=credit_exhaustion`) when someone closes a lease of a frozen stream with `MsgCloseLease` before the EndBlocker reaches it. A consumer tracking credit-exhaustion closures must handle both.

### Event Attribute Sanitization

Certain event attributes (like `rejection_reason` and `closure_reason`) are sanitized before being emitted to prevent log injection attacks. The original value is stored in state unchanged, but the sanitized version appears in events. This protects against malicious input containing control characters or log format strings.

**Sanitization Rules:**
- Every rune that is not `unicode.IsGraphic` (control characters, including `\n` and `\r`) is removed outright — nothing is substituted in its place
- No escaping or truncation is performed; reasons longer than 256 chars are rejected at ValidateBasic rather than truncated here

**Example:**
```
# Original input to MsgRejectLease:
reason: "Invalid config\nSee logs for details"

# Stored in state (unchanged):
rejection_reason: "Invalid config\nSee logs for details"

# Emitted in event (sanitized):
rejection_reason: "Invalid configSee logs for details"
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
| `ErrNoWithdrawableAmount` | 15 | No longer returned. A withdrawal that finds nothing accrued succeeds with empty `total_amounts`. The code stays registered so later codes keep their numbers |
| `ErrEmptyLeaseItems` | 16 | Lease has no items |
| `ErrInvalidQuantity` | 17 | Item quantity is zero, or exceeds `MaxQuantityPerItem` (1,000,000,000) |
| `ErrDuplicateSKU` | 18 | Same SKU appears multiple times |
| `ErrInvalidCreditOperation` | 19 | Credit operation failed |
| `ErrReserved20` | 20 | Reserved for future use |
| `ErrTooManyLeaseItems` | 21 | Lease exceeds max items |
| `ErrLeaseNotPending` | 22 | Lease is not in PENDING state |
| `ErrMaxPendingLeasesReached` | 23 | Tenant at max pending leases |
| `ErrInvalidRejectionReason` | 24 | Rejection reason too long (max 256 chars) |
| `ErrInvalidRequest` | 25 | Invalid request (e.g., a `MsgWithdraw` with the deprecated `lease_uuids` set, without `provider_uuid`, with a `key` longer than `MaxWithdrawCursorLen` = 512 bytes, or with a `key` that does not decode) |
| `ErrInvalidClosureReason` | 26 | Closure reason too long (max 256 chars) |
| `ErrInvalidMetaHash` | 27 | Meta hash exceeds maximum length (max 64 bytes) |
| `ErrInvalidServiceName` | 28 | Invalid service name (must be RFC 1123 DNS label: 1-63 lowercase alphanumeric/hyphens, no leading/trailing hyphen) |
| `ErrInvalidCustomDomain` | 29 | Invalid `LeaseItem.custom_domain` (failed `IsValidFQDN` checks, exceeded 253 bytes, or matched a reserved suffix) |
| `ErrCustomDomainAlreadyClaimed` | 30 | Domain is already claimed by another (lease, item) pair, or by a different item on the same lease |
| `ErrLeaseNotEditable` | 31 | Lease is not in PENDING or ACTIVE state — `custom_domain` cannot be edited on closed/rejected/expired leases |
| `ErrLeaseItemNotFound` | 32 | No lease item matched the supplied `service_name` |
| `ErrAmbiguousLeaseItem` | 33 | Lookup by `service_name` matched more than one item — happens for multi-item legacy leases (no `service_name`s); recreate the lease in service-name mode |

**Note on Reserved Codes:** Error codes 7 and 20 are explicitly reserved to maintain stable error code assignments across module versions.

**Why reserve codes?** During development, some error types were removed or consolidated (e.g., separate errors that were merged into a single error). Rather than renumbering all subsequent codes (which would break client error handling that relies on specific codes), the removed codes are marked as reserved. This ensures:
- Existing client code that handles specific error codes continues to work after upgrades
- Error codes in logs and metrics remain comparable across versions
- New errors get the next number after the highest assigned code rather than reusing gaps

**For developers:** Never assign new errors to reserved codes. Always use the next sequential number after the highest assigned code (currently 33).

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
| SetItemCustomDomain | ✓ (own leases) | ✗ | ✓ | ✓ |
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
