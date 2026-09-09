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
- Checks the bank module's send-enabled policy for the funded denomination;
  a rejected deposit creates no credit account, transfers no funds, and emits
  no funding event
- This deposit check does not change settlement's denomination policy for
  credit already funded

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
- The provider must be active and its payout address must be permitted by the
  bank module and distinct from this tenant's derived credit address; repair
  an ineligible payout before creating a lease
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

The same admission checks as `create-lease` apply, including rejecting a
provider whose stored payout address is blocked by the bank module or equals
the target tenant's derived credit address. Rejection occurs before allocating
a lease UUID or reserving tenant credit.

---

#### acknowledge-lease

Acknowledge one or more PENDING leases atomically (provider or module authority). After revalidating activation
gates, transitions leases to ACTIVE and starts billing.

```bash
manifestd tx billing acknowledge-lease [lease-uuid]... [flags]
```

**Arguments:**
| Argument | Type | Description |
|----------|------|-------------|
| lease-uuid | string (repeated) | Canonical lowercase UUIDv7 values of leases to acknowledge (1-100) |

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
- Block time must be at or before each lease's `created_at + current pending_timeout`; the exact cutoff is valid
- Each tenant's active count after applying the whole batch must be ≤ `max_leases_per_tenant`
- The current provider payout must be permitted by bank policy and distinct
  from every tenant's derived credit address in the batch
- Provider or SKU deactivation alone does not prevent acknowledgement of an
  existing PENDING lease; the payout, deadline, and tenant active-cap gates
  still apply
- Maximum 100 leases per transaction
- Atomic operation: all timeout, per-tenant cap, and payout gates pass before
  any lease, count, timestamp, reservation, or event changes
- A rejected acknowledgement leaves leases PENDING; tenants can cancel or
  providers can reject them to release reservations without a payout transfer,
  or retry acknowledgement after payout repair if the other gates still pass
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
| lease-uuid | string | Canonical lowercase UUIDv7 values of leases to reject (1-100) |

**Flags:**
| Flag | Type | Description |
|------|------|-------------|
| --reason | string | Optional rejection reason (max 256 UTF-8 bytes, applied to all leases) |

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
| lease-uuid | string | Canonical lowercase UUIDv7 values of leases to cancel (1-100) |

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
| lease-uuid | string | Canonical lowercase UUIDv7 values of leases to close (1-100) |

**Flags:**
| Flag | Type | Description |
|------|------|-------------|
| --reason | string | Reason for closing the leases (max 256 UTF-8 bytes, applied to all) |

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
- Before a nonzero final transfer, settlement rejects a provider payout address
  blocked by the bank module or equal to the target tenant's derived credit
  address. Equivalent Bech32 casing is the same account; a rejected transfer
  rolls back the entire batch.
- Response includes total_settled_amounts aggregated across all closed leases
- Emits `batch_closed` event when multiple leases are processed (includes lease_count, closed_by)
- Transfers `min(accrued, B - (R - A))` to the provider payout address,
  preserving every other lease's reservation
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
| lease-uuid | string | Canonical lowercase UUIDv7 values of leases to withdraw from (1-100) |

**Flags:**
| Flag | Type | Default | Description |
|------|------|---------|-------------|
| --provider | string | - | Canonical lowercase provider UUIDv7 for provider-wide withdrawal |
| --limit | uint64 | 50 | Maximum leases to process in provider mode (max 100) |
| --key | string | "" | Base64 `next_key` from the previous provider-wide withdraw response; continues paging. Rejected in specific-leases mode. |

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
  - If any lease fails validation or settlement, the entire batch fails and all
    earlier transfers and timestamp updates in that batch are rolled back
  - `has_more` is always false in this mode
- **Mode 2 (Provider-wide):**
  - Processes up to `limit` active leases
  - Processes each lease in its own cached context. A lease-level settlement or
    store error is logged and skipped without failing successful leases in the
    same call; the failed lease remains unchanged and can be retried explicitly.
  - Returns every error-skipped lease in ordered `failed_lease_uuids`. Retain
    this list before advancing the cursor: `next_key` resumes after the full
    processed page, including failed leases.
  - Response includes `has_more` and an opaque `next_key` cursor if more leases remain
  - Pass the returned `next_key` back as `--key` on the next call and repeat until `has_more` is false. Calling again *without* `--key` restarts from the first lease and never advances past `limit`.
  - `--key` is only valid in provider-wide mode (setting it alongside lease UUIDs is rejected) and the decoded cursor may not exceed 64 bytes (`MaxWithdrawCursorLen`).
  - See [Provider-Wide Withdraw Workflow](#provider-wide-withdraw-workflow) below for example
- Calculates accrued amount since last settlement and settles at most each
  lease's spendable credit `B - (R - A)`
- Transfers the successful lease-spendable amounts, aggregated by denomination,
  to the provider's payout address
- Before a nonzero transfer, rejects a provider payout address blocked by the
  bank module or equal to that tenant's derived credit address; Bech32 text
  casing does not create a distinct account. Specific-lease batches fail
  atomically, while provider-wide mode logs and skips affected leases, listing
  them in `failed_lease_uuids`. Historical provider configurations must be
  repaired through `MsgUpdateProvider` before retrying those leases.
- May trigger auto-close if credit exhausted during withdrawal
- Response includes `withdrawal_count` and `total_amounts` aggregated across the
  successful leases in this request (the current provider-wide page in mode 2),
  plus ordered `failed_lease_uuids` in provider-wide mode
- Emits `batch_withdraw` for a specific-UUID batch with more than one requested
  lease, and for every provider-wide request (including zero successful leases)

##### Provider-Wide Withdraw Workflow

When a provider has many active leases, use provider-wide mode with cursor pagination. Each **decoded module response** returns `has_more` and an opaque `next_key`. Pass that base64 cursor verbatim as `--key` on the next transaction. Omitting it restarts the scan. Retain every `failed_lease_uuids` entry for explicit retry after correcting the failure, including failures on the final page.

`manifestd tx ... --broadcast-mode sync -o json` prints an SDK transaction admission response (`code`, `txhash`, `raw_log`), not `MsgWithdrawResponse`. Wait for block inclusion, check the execution code, then use [`withdraw-result`](#withdraw-result) to decode the committed response:

```bash
manifestd tx billing withdraw --provider 01912345-6789-7abc-8def-0123456789ab \
  --limit 100 --from provider-key --broadcast-mode sync -o json -y

# Use the returned txhash; query tx succeeds once the transaction is indexed.
manifestd query tx "$TXHASH" -o json
manifestd query billing withdraw-result "$TXHASH" -o json
# The second query prints MsgWithdrawResponse, including has_more, next_key,
# withdrawal_count, total_amounts, payout_address, and failed_lease_uuids.
```

**Resumable automation example (Bash + jq):**

Use one worker per state directory and keep the same provider and configured chain/RPC when resuming. The script saves the sync response before polling, retains every decoded page as a receipt, and atomically checkpoints the cursor and accumulated failures before submitting the next page. A timeout, execution failure, or undecodable response stops the script with its pending transaction intact. Restarting resumes the lookup of that transaction. If broadcasting itself fails or leaves an incomplete response, resolve whether the transaction was submitted before removing `pending.json`; restarting must not blindly send a replacement.

A committed transaction with a nonzero execution code will remain failed on every lookup. Inspect its `raw_log` and correct the cause, then archive `pending.json` together with `included.json` under that failed transaction's hash. Restart with `pending.json` absent to submit a **new transaction** at the unchanged checkpoint cursor. Do this only after confirming execution failure; a timeout or an ambiguous broadcast is not evidence that the transaction failed.

```bash
#!/usr/bin/env bash
set -euo pipefail
PROVIDER_UUID="01912345-6789-7abc-8def-0123456789ab"
STATE_DIR="./withdraw-state-$PROVIDER_UUID"  # use a separate directory per chain/run
mkdir -p "$STATE_DIR"
CHECKPOINT="$STATE_DIR/checkpoint.json"
PENDING="$STATE_DIR/pending.json"
if [ ! -f "$CHECKPOINT" ]; then
  jq -n --arg provider "$PROVIDER_UUID" \
    '{provider_uuid: $provider, key: "", has_more: true, failed_lease_uuids: [], last_txhash: ""}' \
    > "$CHECKPOINT"
fi
jq -e --arg provider "$PROVIDER_UUID" '.provider_uuid == $provider' "$CHECKPOINT" > /dev/null

while [ "$(jq -r '.has_more' "$CHECKPOINT")" = true ]; do
  if [ ! -f "$PENDING" ]; then
    KEY=$(jq -r '.key' "$CHECKPOINT")
    # Keep this file even on a broadcast error: submission may be ambiguous.
    manifestd tx billing withdraw --provider "$PROVIDER_UUID" --limit 100 --key "$KEY" \
      --from provider-key --broadcast-mode sync -o json -y > "$PENDING"
  fi
  jq -e '(.code | tonumber) == 0 and (.txhash | test("^[[:xdigit:]]{64}$"))' "$PENDING" > /dev/null
  TXHASH=$(jq -r '.txhash' "$PENDING")
  # A previous run may have checkpointed this page just before it stopped.
  if [ "$(jq -r '.last_txhash' "$CHECKPOINT")" = "$TXHASH" ]; then
    rm "$PENDING"
    continue
  fi

  INCLUDED=false
  for ((attempt = 0; attempt < 60; attempt++)); do
    if manifestd query tx "$TXHASH" -o json > "$STATE_DIR/included.json" 2> "$STATE_DIR/query-error.txt"; then
      INCLUDED=true
      break
    fi
    sleep 2
  done
  if [ "$INCLUDED" != true ]; then
    echo "Transaction $TXHASH not yet queryable; pending.json retained. Check RPC/indexing and resume." >&2
    exit 1
  fi
  jq -e '(.code | tonumber) == 0 and (.height | tonumber) > 0' "$STATE_DIR/included.json" > /dev/null
  manifestd query billing withdraw-result "$TXHASH" -o json > "$STATE_DIR/$TXHASH.json"
  jq --arg hash "$TXHASH" --slurpfile page "$STATE_DIR/$TXHASH.json" '
    .key = ($page[0].next_key // "") |
    .has_more = $page[0].has_more |
    .failed_lease_uuids = ((.failed_lease_uuids + ($page[0].failed_lease_uuids // [])) | unique) |
    .last_txhash = $hash
  ' "$CHECKPOINT" > "$CHECKPOINT.tmp"
  mv "$CHECKPOINT.tmp" "$CHECKPOINT"
  rm "$PENDING"
  jq -r '"Withdrew from \(.withdrawal_count) leases; has_more=\(.has_more)"' "$STATE_DIR/$TXHASH.json"
done
jq -r '.failed_lease_uuids[] | "RETRY AFTER REPAIR: \(.)"' "$CHECKPOINT"
echo "Pagination complete. Keep receipts and resolve every retained failure before starting a new run."
```

After repairing the reported cause, retry each failed lease using specific-leases mode (`manifestd tx billing withdraw "$LEASE_UUID" --from provider-key`). Wait for inclusion and verify execution success for retries too. A completed checkpoint remains completed on restart; use a new state directory for the next scheduled withdrawal sweep.

---

#### set-item-custom-domain

Set or clear the `custom_domain` on a specific lease item, addressed by `service_name`. Available since v2.1.0.

```bash
manifestd tx billing set-item-custom-domain [lease-uuid] [service-name] [domain] [flags]
```

**Arguments:**
| Argument | Type | Description |
|----------|------|-------------|
| lease-uuid | string | Canonical lowercase UUIDv7 of the lease that owns the target item |
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
- Closing, rejecting, expiring, or auto-closing the lease frees the live index entry automatically. The historical `LeaseItem.custom_domain` value is retained on the terminal lease; use `lease-by-domain` to find the current claim.

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
| min-lease-duration | uint64 | Seconds of credit reserved at lease creation; does not enforce a minimum elapsed runtime |
| max-pending-leases-per-tenant | uint64 | Max pending leases per tenant |
| pending-timeout | uint64 | Pending lease timeout in seconds |

**Flags:**
| Flag | Type | Description |
|------|------|-------------|
| --allowed-list | string | Comma-separated addresses with privileged authority for `create-lease-for-tenant` and `set-item-custom-domain`. Omit the flag to snapshot the on-chain value at construction time. Pass `--allowed-list=""` to explicitly clear. |
| --reserved-domain-suffixes | string | Comma-separated DNS suffixes (each beginning with `.`) that tenants are forbidden from claiming via `set-item-custom-domain`. Same construction-time snapshot semantics as `--allowed-list`. |
| --height | int64 | Height used when querying omitted lists (0 = latest). The CLI prints resolved lists to stderr. |

**Examples:**
```bash
# Update only numeric params; snapshot allowed_list and reserved_domain_suffixes from the chain.
manifestd tx billing update-params \
  100 20 3600 10 1800 \
  --from authority

# Update numeric params and overwrite both lists.
manifestd tx billing update-params \
  100 20 3600 10 1800 \
  --allowed-list "manifest1abc...,manifest1def..." \
  --reserved-domain-suffixes ".barney0.manifest0.net,.lifted.app" \
  --from authority

# Explicitly clear reserved_domain_suffixes (numeric-only updates snapshot its current value).
manifestd tx billing update-params \
  100 20 3600 10 1800 \
  --reserved-domain-suffixes="" \
  --from authority
```

This only snapshots lists at transaction construction. Governance execution
replaces every parameter, so a delayed proposal can overwrite intervening
changes. Recheck all fields before approval and rebuild stale proposals;
`--height` does not add an execution-time conflict check.

**Reserved-suffix validation:** each entry must begin with `.`, the substring after the dot must be a lowercase DNS zone (single-label zones such as `.internal` are valid), and duplicates are rejected.

---

### Query Commands

**Query cursor contract:** Standard query `pagination.next_key` values are opaque
`bytes`. JSON and CLI output encode them as base64; pass that string verbatim to
`--page-key`. Do not decode it in the shell or treat it as a lease UUID.
Programmatic gRPC clients pass the decoded bytes in `PageRequest.key`. The
cursor identifies the first unread row, and the next scan resumes inclusively
at that key. `--reverse` may be combined with a query cursor and resumes in the
same direction. General billing list queries use the SDK default page size of
100 and clamp oversized `limit` values to 1000. Cursor pages do work
proportional to that bounded page size. Value-filtered cursor pages inspect at
most 1000 physical index rows and can therefore be short or empty while still
returning a non-empty `next_key`; continue until that cursor is empty.

The five billing collection/index list queries support the standard SDK
`--offset`, `--page`, and `--count-total` compatibility modes. Unfiltered
compatibility requests may inspect at most 20,000 physical rows. Value-filtered
requests retain the 1000-row ceiling in every mode; currently this applies to
`LeasesBySKU --state`. A request that cannot return an exact page or total
within its ceiling fails with gRPC `ResourceExhausted` rather than returning a
partial result. Cursor pagination remains the efficient, unbounded-history
path. An omitted or zero limit defaults to 100 without implicitly enabling
`count_total`; request the total explicitly when needed. A request that combines
a page key with a nonzero offset fails with gRPC `InvalidArgument`; as in the
SDK, `count_total` is ignored when a page key is present. The `CreditAccount`
and `ProviderWithdrawable` queries remain cursor-only because they respectively
traverse bank balances and simulate the settlement lifecycle. Query cursors are
not interchangeable with provider-wide `MsgWithdrawResponse.next_key`, whose
separate contract is described below.

The five UUID-taking billing query commands validate locally before constructing
an RPC. `lease` and `withdrawable` report `invalid lease_uuid format: {uuid}`;
`leases-by-provider` and `provider-withdrawable` report
`invalid provider_uuid format: {uuid}`; and `leases-by-sku` reports
`invalid sku_uuid format: {uuid}`. Each requires canonical lowercase UUIDv7
input.

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
| lease-uuid | string | Canonical lowercase UUIDv7 of the lease |

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
    "min_lease_duration_at_creation": "3600",
    "reservation": {
      "remaining_amounts": [
        {"denom": "upwr", "amount": "340000"}
      ]
    }
  }
}
```

**Notes:**
- `locked_price` is a Coin with denom and amount, representing the per-second rate
- `acknowledged_at` is set when provider acknowledges (ACTIVE state)
- `closed_at` is set when lease is closed (CLOSED state)
- `rejected_at` is set when provider rejects or tenant cancels (REJECTED state)
- `expired_at` is set when pending lease times out (EXPIRED state)
- `rejection_reason` contains the provider's reason for rejection (max 256 UTF-8 bytes)
- `closure_reason` contains the reason for closure (max 256 UTF-8 bytes)
- `meta_hash` contains the optional hash/reference to off-chain deployment data (max 64 bytes, immutable)
- `min_lease_duration_at_creation` stores the `min_lease_duration` parameter value at creation time for consistent reservation calculation
- `reservation.remaining_amounts` is this modern lease's consumable remaining guarantee. It decreases as settlement consumes the tranche and is empty after release. Historical leases use an initialized empty reservation and share the account's `unattributed_reserved_amounts` instead.

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
| --page-key | string | Base64 `pagination.next_key` from the previous response |

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
| --limit | uint64 | Pagination limit |
| --page-key | string | Base64 `pagination.next_key` from the previous response |

---

#### leases-by-provider

Query leases for a specific provider.

```bash
manifestd query billing leases-by-provider [provider-uuid] [flags]
```

**Arguments:**
| Argument | Type | Description |
|----------|------|-------------|
| provider-uuid | string | Canonical lowercase UUIDv7 of the provider |

**Flags:**
| Flag | Type | Description |
|------|------|-------------|
| --state | string | Filter by state (pending, active, closed, rejected, expired) |
| --limit | uint64 | Pagination limit |
| --page-key | string | Base64 `pagination.next_key` from the previous response |

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

**Flags:** cursor pagination over bank balances (`--page-key`, `--limit`,
`--reverse`). The default limit is 100 and the maximum is 1000. `--offset` and
`--count-total` are rejected so bank-store iteration remains page-bounded. Pass
the prior response's base64 `pagination.next_key` verbatim to `--page-key`.
Reverse pages follow `x/bank` and return `balances` and `available_balances` in
descending denomination order. Go callers must call `Sort()` before using
`sdk.Coins` operations that require canonical ascending order (including
`AmountOf`, `Add`, and `Validate`).

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
    ],
    "unattributed_reserved_amounts": [],
    "unattributed_lease_count": "0"
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
  ],
  "pagination": {
    "next_key": null,
    "total": "0"
  }
}
```

**Response Fields:**
- `credit_account.reserved_amounts`: Exact aggregate of every live modern lease's `reservation.remaining_amounts` plus `unattributed_reserved_amounts`. New leases start with `rate_per_second × min_lease_duration`, but settlement consumes that tranche, so this is not a fixed nominal sum.
- `credit_account.unattributed_reserved_amounts`: Subset of `reserved_amounts` allocated to the live historical cohort whose individual reservations cannot be reconstructed. It is normally empty on newly created state.
- `credit_account.unattributed_lease_count`: Exact number of live historical leases sharing that cohort, including when its remaining amount is zero. Terminal transitions decrement it in O(1) and release the exact remaining `unattributed_reserved_amounts` when it reaches zero.
- `balances`: One ordered page of spendable bank balances at the credit address. Vesting locked coins are excluded. Fully locked denominations are omitted; follow `pagination.next_key` until empty even when the coin arrays are empty.
- `available_balances`: Credit available for new leases (`balances - reserved_amounts`) for the same denom page. New leases can only be created if the full account covers the required reservation.
- `pagination`: The SDK bank-balance cursor. Offset and total-count scans are intentionally unsupported.

The embedded `credit_account` record is returned whole. New lease creation may
increase reservation-denom cardinality only when the resulting set contains at
most 1,000 denominations. Historical v2 accounts already above that limit
remain readable and releasable; a new lease may use denoms already present but
cannot introduce another denom until the account falls below the cap. Their
one-time account decode can therefore be larger than a balance page.

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

Query the amount one lease could transfer now. **This query calculates current
accrual and applies the lease's reservation-safe spend cap.**

```bash
manifestd query billing withdrawable [lease-uuid]
```

**Arguments:**
| Argument | Type | Description |
|----------|------|-------------|
| lease-uuid | string | Canonical lowercase UUIDv7 of the lease |

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

**Note:** For each denomination, the result is
`min(accrued_since_last_settlement, B - (R - A))`, where `B` is the tenant's
credit balance, `R` is its aggregate reservation, and `A` is this lease's
remaining tranche (or the explicit historical cohort allocation). The cap
protects every other lease's reservation. The query is read-only and does NOT
trigger actual settlement or a token transfer. Only ACTIVE leases accrue new
charges.

---

#### withdraw-result

Decode the `MsgWithdrawResponse` of a successful committed transaction. This is a local CLI decoder over the CometBFT transaction query; it adds no module gRPC or REST endpoint.

```bash
manifestd query billing withdraw-result [tx-hash] [flags]
```

| Argument / flag | Default | Description |
|-----------------|---------|-------------|
| `tx-hash` | required | 64-character hexadecimal transaction hash |
| `--msg-index` | `0` | Zero-based top-level message response index in a multi-message transaction |

The command fails if the transaction is not indexed, has not committed, has a nonzero execution code, or the selected response is missing, malformed, or belongs to another message type. It does not broadcast or poll. Successful JSON output contains the decoded module fields, with `next_key` base64-encoded. Nested authz/group responses require decoding their respective wrappers and are not selected by `--msg-index`.

See the [provider withdrawal workflow](#provider-wide-withdraw-workflow) for inclusion polling, checkpointing, and failed-lease retries.

---

#### provider-withdrawable

Dry-run a withdrawal of one ordered page of the provider's ACTIVE leases.
**This query calculates a page-local execution estimate against shared tenant
balances and reservations.**

```bash
manifestd query billing provider-withdrawable [provider-uuid]

# Forward page comparable to one provider-wide MsgWithdraw (transaction max: 100)
manifestd query billing provider-withdrawable [provider-uuid] --limit 100
```

**Arguments:**
| Argument | Type | Description |
|----------|------|-------------|
| provider-uuid | string | Canonical lowercase UUIDv7 of the provider |

**Flags:** cursor pagination (`--page-key`, `--limit`) plus `--reverse`. Pass the
prior query response's base64 `pagination.next_key` verbatim to `--page-key`.
Page size defaults to 50 and is capped at 100. Non-zero `--offset` and
`--count-total` are rejected so query work remains page-bounded. The query
iterates the provider's active leases. Reverse pages are useful for read-only
inspection, but no provider-wide `MsgWithdraw` call mirrors them; the
transaction is forward-only and capped at 100 leases.

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
  "failed_lease_uuids": [],
  "pagination": {
    "next_key": null,
    "total": "0"
  }
}
```

Leases are evaluated in the returned index order. Within the page, earlier
leases consume a virtual copy of their tenant's balance and their own
reservation tranche before later leases are estimated, so shared unreserved
credit is counted only once. Each lease is simulated in its own nested cache,
matching provider-wide withdrawal's best-effort behavior: a lease-level failure
is discarded, skipped, and listed in ordered `failed_lease_uuids`, while
successful virtual effects are visible to later leases in the page. The outer
query cache is never committed to chain state.
`lease_count` matches the comparable provider-wide transaction's
`withdrawal_count`: it includes successful zero-transfer auto-closes, but not
failed simulations or ordinary zero-accrual leases. At identical state and
block time, `failed_lease_uuids` also matches that transaction's failure list.

**Do not sum independently queried pages.** Each page begins from current chain
state, so pages that contain leases for the same tenant can count the same
balance. Every forward query page is comparable to one provider-wide withdrawal
over the same current segment because the query limit is capped at the
transaction maximum of 100. Submit the transaction and wait for it to commit.
Query the next segment with the prior query response's `pagination.next_key`;
withdraw the next segment with the prior transaction response's `next_key`.

Query and transaction cursors are different contracts: the query's
`pagination.next_key` identifies its first unread index entry, while
`MsgWithdrawResponse.next_key` identifies the last processed lease and resumes
strictly after it. Never pass a query cursor as `MsgWithdraw.key` (or vice
versa). Reverse query pages are estimates only, not one-transaction previews;
offset and count-total requests are rejected. The query itself is read-only and
transfers no tokens.

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
| --page-key | string | Base64 `pagination.next_key` from the previous response |
| --count-total | bool | Return the exact total when it can be computed within the applicable scan ceiling |

**Example:**
```bash
manifestd query billing credit-accounts --limit 10 --count-total
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
| sku-uuid | string | Canonical lowercase UUIDv7 of the SKU |

**Flags:**
| Flag | Type | Description |
|------|------|-------------|
| --state | string | Filter by state (pending, active, closed, rejected, expired) |
| --limit | uint64 | Pagination limit |
| --page-key | string | Base64 `pagination.next_key` from the previous response |
| --count-total | bool | Return the exact total when it can be computed within the applicable scan ceiling |

**Example:**
```bash
manifestd query billing leases-by-sku 01912345-6789-7abc-8def-0123456789ab --state active --count-total
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

Report the tenant's gross bank-balance runway at the aggregate current ACTIVE
lease rate. This is a coarse funding metric, not an auto-close forecast.

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
| `current_balance` | Tenant's current credit balance for denominations used by active leases |
| `total_rate_per_second` | Combined burn rate of all active leases (per denom) |
| `estimated_duration_seconds` | Gross `min(spendable bank balance / active rate)` runway across active denoms |
| `active_lease_count` | Number of currently active leases |

**Notes:**
- The estimate is calculated in real-time from spendable bank balances and active lease rates
- If no active leases exist, `estimated_duration_seconds` will be `0` and `total_rate_per_second` will be empty
- With multi-denom support, the estimate returns the minimum duration across all denominations (the limiting factor)
- A quotient at or above `18,446,744,073,709,551,615` seconds is saturated to that maximum `uint64` value; it is not reported as zero

**Limitations:**
- **Bounded work**: Active iteration uses the credit account's stored `active_lease_count`, not the current governance limit, so parameter reductions remain represented. `CreditEstimate` enforces the conservative ceiling of 11,000 ACTIVE leases (the exact v2 reachable maximum is 10,999) and 100,000 decoded lease items. Either work-bound violation returns `ErrLeaseQueryLimitExceeded` as gRPC `ResourceExhausted` instead of truncating the result. If an index contains more or fewer entries than its stored count, the query returns gRPC `Internal` with `ErrReservationInvariant`; it never returns a partial estimate. `CreditAccount` is independently cursor-paginated over bank balances and does not scan leases.
- **Not reservation-aware**: The quotient does not subtract PENDING or other-lease reservations. Those funds can be unavailable to a particular ACTIVE lease under the isolation invariant.
- **Does not account for pending withdrawals**: The estimate does not subtract unsettled accrued amounts from existing leases.
- **Not an auto-close prediction**: Per-lease tranche ownership, settlement timing, and rate changes can make lifecycle transitions happen earlier or later than the gross quotient.
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

The generated embedded descriptors omit protobuf `SourceCodeInfo`, so runtime
reflection exposes the schema but not source comments. Use this API reference
for the documented validation and error semantics.

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

The funded denomination must be send-enabled under the bank module's current
policy, including its default when no denomination-specific setting exists.
Rejection leaves bank balances and billing state unchanged. This check applies
to new deposits; existing lease settlement does not check denomination
send-enabled status.

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

> **Note:** `new_balance` returns only the funded denomination. Use the
> cursor-paginated `CreditAccount` query and follow `pagination.next_key` to
> read every denomination held by the credit account.

---

#### MsgCreateLease

Create a lease for the sender (tenant). Lease starts in PENDING state.

The provider must be active. Its payout must be permitted by bank policy and
distinct from the tenant's derived credit address; these checks precede credit
reservation and lease UUID allocation.

**Request:**
```protobuf
message MsgCreateLease {
  string tenant = 1;  // Tenant (must be signer)
  repeated LeaseItemInput items = 2;  // SKU items
  bytes meta_hash = 3;  // Optional hash/reference to off-chain deployment data (max 64 bytes)
}

message LeaseItemInput {
  string sku_uuid = 1;      // Canonical lowercase SKU UUIDv7
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

The same provider and payout eligibility checks as `MsgCreateLease` apply to
the target tenant.

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
All leases must belong to the same provider, be in PENDING state, be no later than their hard
pending deadline, and fit within each tenant's post-batch active cap. The current
provider payout must be permitted by bank policy and distinct from every
tenant's derived credit address in the batch.

**Request:**
```protobuf
message MsgAcknowledgeLease {
  string sender = 1;               // Provider or authority
  repeated string lease_uuids = 2; // Canonical lowercase lease UUIDv7 values (1-100)
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
- Block time must be ≤ `created_at + current pending_timeout` for every lease; strictly later acknowledgements fail even if EndBlock has not yet expired the lease
- Each tenant's active count after the entire batch must be ≤ `max_leases_per_tenant`
- The current provider payout must be permitted by bank policy and must not
  equal any batch tenant's derived credit address; payout updates after lease
  creation are checked again here
- Maximum 100 leases per call
- Atomic: all activation gates pass before any state, aggregate, timestamp, reservation, or event changes
- Pending leases with an ineligible payout remain cancellable/rejectable
  without a transfer; repair the payout before retrying acknowledgement

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
  repeated string lease_uuids = 2; // Canonical lowercase lease UUIDv7 values (1-100)
  string reason = 3;               // Optional reason (max 256 UTF-8 bytes, applied to all)
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
  repeated string lease_uuids = 2; // Canonical lowercase lease UUIDv7 values (1-100)
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
  repeated string lease_uuids = 2; // Canonical lowercase lease UUIDv7 values (1-100)
  string reason = 3;               // Optional closure reason (max 256 UTF-8 bytes, applied to all)
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
  repeated string lease_uuids = 2; // Mode 1: canonical lowercase lease UUIDv7 values (1-100)
  string provider_uuid = 3;        // Mode 2: canonical lowercase provider UUIDv7
  uint64 limit = 4;                // Max leases in provider mode (default 50, max 100)
  bytes key = 5;                   // Mode 2: opaque cursor from the previous response's next_key; base64 in JSON. Must be empty in mode 1.
}
```

> **Note:** `lease_uuids` and `provider_uuid` are mutually exclusive - exactly one must be specified.

**Response:**
```protobuf
message MsgWithdrawResponse {
  repeated cosmos.base.v1beta1.Coin total_amounts = 1;  // Total withdrawn per denom
  string payout_address = 2;        // Destination address
  uint64 withdrawal_count = 3;      // Successful leases, including zero-transfer auto-closes
  bool has_more = 4;                // More leases remain (only true in provider-wide mode)
  bytes next_key = 5;               // Opaque cursor; pass as MsgWithdraw.key for the next page. Non-empty iff has_more is true; always empty in lease_uuids mode.
  repeated string failed_lease_uuids = 6; // Ordered provider-wide failures; empty in atomic specific-lease mode.
}
```

`failed_lease_uuids` is an additive response-only wire field. It is not stored
and requires no billing store or consensus-version migration.

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
  string lease_uuid = 2;    // Canonical lowercase UUIDv7 of target PENDING/ACTIVE lease
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
  rpc WithdrawableAmount(QueryWithdrawableAmountRequest) returns (QueryWithdrawableAmountResponse);
  rpc ProviderWithdrawable(QueryProviderWithdrawableRequest) returns (QueryProviderWithdrawableResponse);
  rpc LeaseByCustomDomain(QueryLeaseByCustomDomainRequest) returns (QueryLeaseByCustomDomainResponse);
}
```

`LeasesByProvider.provider_uuid` and `LeasesBySKU.sku_uuid` must be non-empty
canonical lowercase UUIDv7 values. Empty fields fail with gRPC
`InvalidArgument` and `<field> cannot be empty`. Non-empty malformed, uppercase,
or non-v7 values fail with `InvalidArgument` and
`<field> must be a valid UUIDv7`. An unknown canonical lowercase UUIDv7 is
valid input and returns an empty page.

By contrast, `Lease.lease_uuid`, `WithdrawableAmount.lease_uuid`, and
`ProviderWithdrawable.provider_uuid` preserve direct-lookup behavior. They
reject an empty field with `InvalidArgument`, then look up every non-empty key
as supplied. A malformed, uppercase, non-v7, or unknown canonical value
therefore returns gRPC `NotFound` rather than a UUID-format error. `NotFound`
is reserved for an absent primary resource; unexpected primary-store or
value-decoding failures return `Internal` so state corruption remains visible.

**Important Note:** Lease queries (`Lease`, `Leases`, `LeasesByTenant`, `LeasesByProvider`) return stored state and do NOT trigger settlement or auto-close. `WithdrawableAmount` calculates the current amount for one lease. `ProviderWithdrawable` dry-runs the current ordered page against page-local virtual tenant state and reports skipped failed simulations in `failed_lease_uuids`, just as provider-wide withdrawal does. Its pages are not additive. Every forward page has a one-transaction analogue because the query limit is capped at the transaction maximum of 100. After commit, advance the query with its prior first-unread cursor and the transaction with its prior last-processed cursor; never interchange them. Settlement (actual token transfer) only happens during write operations (`Withdraw`, `CloseLease`). Only ACTIVE leases accrue charges.

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

Get a tenant's credit-account metadata plus one bounded bank-balance page.

**Endpoint:** `liftedinit.billing.v1.Query/CreditAccount`

**Request:**
```protobuf
message QueryCreditAccountRequest {
  string tenant = 1;
  cosmos.base.query.v1beta1.PageRequest pagination = 2;
}
```

**Response:**
```protobuf
message QueryCreditAccountResponse {
  CreditAccount credit_account = 1;
  repeated cosmos.base.v1beta1.Coin balances = 2;  // Current bank-balance page
  repeated cosmos.base.v1beta1.Coin available_balances = 3;  // Same page minus reserved_amounts
  cosmos.base.query.v1beta1.PageResponse pagination = 4;
}
```

Reverse pages follow `x/bank` and return both coin lists in descending
denomination order. Go callers must call `Sort()` before using `sdk.Coins`
operations that require canonical ascending order (including `AmountOf`, `Add`,
and `Validate`).

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
| GET | `/lease/{lease_uuid}/withdrawable` | Get withdrawable amount |
| GET | `/provider/{provider_uuid}/withdrawable` | Best-effort estimate for the current ordered page; every forward page mirrors one provider transaction because the query limit is capped at 100, after which clients re-query rather than summing pages |
| GET | `/lease/by-domain/{custom_domain}` | Look up lease by custom domain (v2.1.0+) |

The `/leases/provider/{provider_uuid}` and `/leases/sku/{sku_uuid}` routes apply
the same canonical lowercase UUIDv7 validation as their gRPC methods. A
non-empty malformed, uppercase, or non-v7 path value maps to HTTP 400 / gRPC
`InvalidArgument`; an unknown canonical value returns an empty page. A missing
path component does not match these routes; a trailing empty component does
match and maps the handler's `<field> cannot be empty` response to HTTP 400.

The direct-lookup routes `/lease/{lease_uuid}`,
`/lease/{lease_uuid}/withdrawable`, and
`/provider/{provider_uuid}/withdrawable` map any non-empty key that does not
exist—including malformed, uppercase, or non-v7 text—to HTTP 404 / gRPC
`NotFound`. `/lease/` reaches the lease handler with an empty value and returns
HTTP 400; an omitted UUID in either withdrawable route shape does not reach its
handler. An unexpected primary-store or decoding failure maps to HTTP 500 /
gRPC `Internal`.

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
  google.protobuf.Timestamp closed_at = 7;
  google.protobuf.Timestamp last_settled_at = 8;
  google.protobuf.Timestamp acknowledged_at = 9;
  google.protobuf.Timestamp rejected_at = 10;
  string rejection_reason = 11;       // Provider's rejection reason (max 256 UTF-8 bytes)
  google.protobuf.Timestamp expired_at = 12;
  string closure_reason = 13;         // Closure reason (max 256 UTF-8 bytes)
  bytes meta_hash = 14;               // Hash/reference to off-chain deployment data (max 64 bytes, immutable)
  uint64 min_lease_duration_at_creation = 15; // Snapshot of min_lease_duration param at creation
  LeaseReservation reservation = 16;  // Remaining guarantee; presence distinguishes pre-v4 (v2/v3) exports
}
```

**Field Notes:**
- `rejection_reason`: Set when a provider rejects a PENDING lease via `MsgRejectLease`. Contains the provider's explanation for rejecting the lease (e.g., "resources unavailable", "invalid configuration"). Maximum 256 UTF-8 bytes. Only present when `state` is `LEASE_STATE_REJECTED`.
- `closure_reason`: Set when a lease is closed via `MsgCloseLease` with a reason, or automatically set to `"credit exhausted"` when a lease is auto-closed due to insufficient credit during settlement. Maximum 256 UTF-8 bytes. Only present when `state` is `LEASE_STATE_CLOSED`.
- `meta_hash`: Optional immutable hash or reference linking to off-chain deployment data (e.g., deployment manifest hash, configuration reference). Set at lease creation and cannot be modified afterward. Maximum 64 bytes to accommodate SHA-256 or SHA-512 hashes.
- `min_lease_duration_at_creation`: Snapshot of the `min_lease_duration` parameter at the time this lease was created. Used to calculate consistent credit reservations (`reservation = sum(locked_price × quantity) × min_lease_duration_at_creation`) regardless of subsequent governance changes to the parameter. This ensures existing reservations remain valid when parameters are updated.
- `reservation`: Nullable only as a pre-v4 (v2/v3) genesis-format marker. Persisted v4 leases always initialize it. A modern live lease stores its remaining tranche; terminal and historical leases store an empty tranche.

```protobuf
message LeaseReservation {
  repeated Coin remaining_amounts = 1;
}
```

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
- `custom_domain`: Optional fully-qualified domain name routed to this item's container by the provider after off-chain verification. Set or cleared via `MsgSetItemCustomDomain` (not via lease creation). Validated by `IsValidFQDN` (≤253 bytes, lowercase, ≥1 dot, RFC 1123 labels, non-numeric TLD) and rejected if it matches any `params.reserved_domain_suffixes` entry. Globally unique across PENDING/ACTIVE leases — enforced by the `CustomDomainIndex` reverse-lookup. Closing, rejecting, expiring, or auto-closing the lease releases the live index entry while retaining this field as history. A terminal lease's stored value does not reserve the domain; use `lease-by-domain` and the returned lease state to determine the current claim.

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
  repeated Coin reserved_amounts = 5; // R = sum(live modern remaining tranches) + U
  repeated Coin unattributed_reserved_amounts = 6; // U: shared live historical cohort
  uint64 unattributed_lease_count = 7; // Exact live historical cohort size
}
```

**Field Notes:**
- `reserved_amounts`: Exact remaining reservation aggregate. For each tenant, `R = sum(Lease.reservation.remaining_amounts for live modern leases) + U`. Available credit for creating new leases is `balances - R`.
- `unattributed_reserved_amounts`: The explicit `U` subset reserved for live leases that predate reconstructible per-lease guarantees. It is consumed as a shared cohort and cleared when its last live member terminates.
- `unattributed_lease_count`: Exact number of live historical leases sharing `U`, even when `U` is empty. This makes terminal release O(1): decrement the count and, when it reaches zero, subtract exactly the remaining `U` from `R`.
- Settlement protects every other reservation. A lease with allocation `A`, balance `B`, and aggregate `R` can transfer at most `B - (R - A)`; the amount funded by `A` is subtracted from both `A` and `R`. Modern terminal transitions release exactly the remaining `A`.

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
- `max_leases_per_tenant`: Revalidated when PENDING leases are acknowledged, using each tenant's active count after the complete batch.
- `pending_timeout`: Defines a hard acknowledgement deadline at `created_at + current pending_timeout`. The exact cutoff is valid; a strictly later block time is rejected even before rate-limited EndBlock cleanup.
- `allowed_list`: Up to 100 addresses with privileged authority for `MsgCreateLeaseForTenant` and `MsgSetItemCustomDomain`, in addition to the module authority. Addresses must be valid and distinct by decoded identity.
- `reserved_domain_suffixes`: Up to 100 DNS suffixes (each must begin with `.`) that tenants are forbidden from claiming as a `LeaseItem.custom_domain`. Match is case-insensitive at a label boundary, plus the apex (e.g. `.foo.example` matches both `app.foo.example` and `foo.example`). Each entry's substring after the leading dot must be a lowercase DNS zone; a single-label zone such as `.internal` is valid. Tunable via `MsgUpdateParams`.

**Defaults and validation bounds:**
| Param | Default | Valid range |
|-------|---------|-------------|
| `max_leases_per_tenant` | 100 | 1 – 10,000 (`MaxLeasesPerTenantUpperBound`) |
| `max_items_per_lease` | 20 | 1 – 100 (`MaxItemsPerLeaseHardLimit`) |
| `min_lease_duration` | 3600 | 1 – 2,592,000 seconds / 30 days (`MaxMinLeaseDuration`) |
| `max_pending_leases_per_tenant` | 10 | 1 – 1,000 (`MaxPendingLeasesPerTenantUpperBound`) |
| `pending_timeout` | 1800 | 60 (`MinPendingTimeout`) – 86,400 (`MaxPendingTimeout`) seconds |
| `allowed_list` | empty | 0 – 100 entries (`MaxAllowedListEntries`) |
| `reserved_domain_suffixes` | empty | 0 – 100 entries (`MaxReservedDomainSuffixEntries`) |

---

## Events

The billing module emits the following events for state changes:

| Event | Attributes | Description |
|-------|------------|-------------|
| `credit_funded` | tenant, credit_address, sender, amount, new_balance | Credit account funded |
| `lease_created` | lease_uuid, tenant, provider_uuid, item_count, total_rate_per_second, pending_lease_count, created_by, sender, meta_hash (optional, hex-encoded) | Lease created in PENDING state |
| `lease_acknowledged` | lease_uuid, tenant, provider_uuid, acknowledged_by | Provider acknowledged lease (→ ACTIVE) |
| `batch_acknowledged` | lease_count, provider_uuid, acknowledged_by | Batch summary when multiple leases acknowledged |
| `lease_rejected` | lease_uuid, tenant, provider_uuid, rejected_by, rejection_reason | Provider rejected lease |
| `batch_rejected` | lease_count, provider_uuid, rejected_by | Batch summary when multiple leases rejected |
| `lease_cancelled` | lease_uuid, tenant, provider_uuid, cancelled_by | Tenant cancelled pending lease |
| `batch_cancelled` | lease_count, tenant, cancelled_by | Batch summary when multiple leases cancelled |
| `lease_expired` | lease_uuid, tenant, provider_uuid, reason | Pending lease expired |
| `lease_closed` | lease_uuid, tenant, provider_uuid, settled_amounts, closed_by, duration_seconds, active_lease_count, closure_reason (optional) | Lease closed (manually, or auto-closed on credit exhaustion) |
| `batch_closed` | lease_count, closed_by, settled_amounts | Batch summary when multiple leases closed |
| `lease_auto_closed` | lease_uuid, tenant, provider_uuid, amount, payout_address, reason | Lease auto-closed by provider-wide withdrawal; amount is the actual final transfer |
| `provider_withdraw` | lease_uuid, amount, provider_uuid, payout_address, auto_closed (auto-close only) | One committed lease withdrawal in either specific or provider-wide mode; amount is the actual transfer |
| `batch_withdraw` | lease_count, provider_uuid, amount, payout_address; provider-wide also: auto_closed, failed_lease_count, failed_lease_uuids | Specific-UUID batch summary when more than one lease is requested; provider-wide summary on every call, including zero-success calls. Provider-wide failed UUIDs are comma-separated in processing order. |
| `params_updated` | | Module parameters updated |
| `lease_custom_domain_set` | lease_uuid, tenant, provider_uuid, service_name, custom_domain, set_by | `LeaseItem.custom_domain` set or changed (v2.1.0+; `provider_uuid` added v2.2.0+) |
| `lease_custom_domain_cleared` | lease_uuid, tenant, provider_uuid, service_name, custom_domain (previous value), set_by | `LeaseItem.custom_domain` cleared (v2.1.0+; `provider_uuid` added v2.2.0+) |

**Lease creation:** `created_by` records `tenant`, `authority`, or `allowed`;
`sender` is the canonical signer address. Allowed-list creation no longer
mislabels the role as authority. Indexers should use `sender` for identity.

**Custom-domain `set_by` attribute:** records the role under which the call was authorised. One of `tenant`, `authority`, `allowed`. No event is emitted for an idempotent re-set or a clear of an already-empty domain.

**`lease_closed` `closed_by` attribute:** records who closed the lease. One of `tenant`, `authority`, `provider`, or `credit_exhaustion`. `credit_exhaustion` is the auto-close sentinel set when lazy settlement finds the credit exhausted.

**Provider-wide `batch_withdraw` attributes:** `auto_closed` is an integer count
of auto-closed leases. `failed_lease_count` is the number of error-skipped
leases, and `failed_lease_uuids` is their comma-separated processing-order
list (empty when none). Specific-lease batches emit none of these three
provider-wide summary attributes.

**Credit-exhaustion events:** close emits `lease_closed` with
`closed_by=credit_exhaustion`. Both withdrawal modes emit one `provider_withdraw`
per committed withdrawal, with `auto_closed="true"` for exhaustion. Provider-wide
withdrawal also retains `lease_auto_closed` (`reason=credit_exhausted`) as
lifecycle information. That event and `batch_withdraw` describe the same
transfers; do not add their amounts to `provider_withdraw` totals. Failed or
zero-accrual skipped leases emit no payout event.

**Special Case - Withdrawal Auto-Close:** When a `MsgWithdraw`
finds accrued charges meet or exceed the lease's spendable credit
`B - (R - A)`, it settles and automatically closes the lease. Its
`provider_withdraw` event uses `auto_closed: "true"`; `amount` is the actual
final transfer (which may be zero) and `payout_address` is always present.

### Event Attribute Sanitization

Certain event attributes (like `rejection_reason` and `closure_reason`) are sanitized before being emitted to prevent log injection attacks. The original value is stored in state unchanged, but the sanitized version appears in events. This protects against malicious input containing control characters or log format strings.

**Sanitization Rules:**
- Every rune that is not `unicode.IsGraphic` (control characters, including `\n` and `\r`) is removed outright — nothing is substituted in its place
- No escaping or truncation is performed; reasons longer than 256 UTF-8 bytes are rejected at ValidateBasic rather than truncated here

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

Query the committed transaction and verify `code == 0` before extracting events.
Sync broadcast responses have no execution events. For transactions with multiple
messages, additionally filter events by their `msg_index` attribute to select the
intended message; the examples below assume a single creation message:

```bash
# Query events for a specific transaction
manifestd query tx [txhash] --output json | jq '.events'

# Example: Extract lease_uuid from a lease creation
manifestd query tx [txhash] --output json | jq -er 'select((.code | tonumber) == 0 and (.height | tonumber) > 0) | .events[] | select(.type=="lease_created") | .attributes[] | select(.key=="lease_uuid") | .value'
```

---

## Error Codes

| Error | Code | Description |
|-------|------|-------------|
| `ErrInvalidParams` | 1 | Invalid module parameters |
| `ErrLeaseNotFound` | 2 | Lease doesn't exist |
| `ErrLeaseNotActive` | 3 | Lease is not in ACTIVE state |
| `ErrInsufficientCredit` | 4 | Not enough credit balance |
| `ErrMaxLeasesReached` | 5 | Lease creation is blocked because the tenant is already at its active cap |
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
| `ErrInvalidQuantity` | 17 | Item quantity is zero, or exceeds `MaxQuantityPerItem` (1,000,000,000) |
| `ErrDuplicateSKU` | 18 | Same SKU appears multiple times |
| `ErrInvalidCreditOperation` | 19 | Credit operation failed |
| `ErrArithmeticOverflow` | 20 | Billing arithmetic cannot be represented safely |
| `ErrTooManyLeaseItems` | 21 | Lease exceeds max items |
| `ErrLeaseNotPending` | 22 | Lease is not in PENDING state |
| `ErrMaxPendingLeasesReached` | 23 | Tenant at max pending leases |
| `ErrInvalidRejectionReason` | 24 | Rejection reason too long (max 256 UTF-8 bytes) |
| `ErrInvalidRequest` | 25 | Invalid request (e.g., conflicting fields in MsgWithdraw; setting `key` alongside `lease_uuids`; or a `key` longer than `MaxWithdrawCursorLen` = 64 bytes in provider-wide mode) |
| `ErrInvalidClosureReason` | 26 | Closure reason too long (max 256 UTF-8 bytes) |
| `ErrInvalidMetaHash` | 27 | Meta hash exceeds maximum length (max 64 bytes) |
| `ErrInvalidServiceName` | 28 | Invalid service name (must be RFC 1123 DNS label: 1-63 lowercase alphanumeric/hyphens, no leading/trailing hyphen) |
| `ErrInvalidCustomDomain` | 29 | Invalid `LeaseItem.custom_domain` (failed `IsValidFQDN` checks, exceeded 253 bytes, or matched a reserved suffix) |
| `ErrCustomDomainAlreadyClaimed` | 30 | Domain is already claimed by another (lease, item) pair, or by a different item on the same lease |
| `ErrLeaseNotEditable` | 31 | Lease is not in PENDING or ACTIVE state — `custom_domain` cannot be edited on closed/rejected/expired leases |
| `ErrLeaseItemNotFound` | 32 | No lease item matched the supplied `service_name` |
| `ErrAmbiguousLeaseItem` | 33 | Lookup by `service_name` matched more than one item — happens for multi-item legacy leases (no `service_name`s); recreate the lease in service-name mode |
| `ErrLeaseAcknowledgementDeadlineExceeded` | 34 | Acknowledgement block time is strictly after a lease's hard pending deadline |
| `ErrLeaseAcknowledgementActiveCapExceeded` | 35 | Acknowledgement would exceed a tenant's post-batch active cap |
| `ErrReservationInvariant` | 36 | Stored balance/reservation state violates the consumable reservation invariant |
| `ErrLeaseQueryLimitExceeded` | 37 | `CreditEstimate` would exceed its conservative ACTIVE-lease or total-item work bound |
| `ErrReservationDenomLimitExceeded` | 38 | A new lease would increase a credit account's aggregate reservation beyond 1,000 denominations |
| `ErrSequenceExhausted` | 39 | The deterministic lease UUID sequence has exhausted its `uint64` range |
| `ErrInternalCorruption` | 40 | A stored billing record exists but cannot be decoded; distinct from a missing record |

**Note on Reserved Codes:** Error code 7 is explicitly reserved to maintain stable error code assignments across module versions. Code 20 is active and belongs to `ErrArithmeticOverflow`.

**Why reserve a code?** During development, an error type was removed or consolidated. Rather than renumbering subsequent codes (which would break client error handling that relies on specific codes), its code remains reserved. This ensures:
- Existing client code that handles specific error codes continues to work after upgrades
- Error codes in logs and metrics remain comparable across versions
- New errors get the next number after the highest assigned code rather than reusing gaps

**For developers:** Never assign new errors to reserved codes. Always use the next sequential number after the highest assigned code (currently 40).

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
