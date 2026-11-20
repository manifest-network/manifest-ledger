# Billing Module Troubleshooting Guide

This guide covers common errors and issues users may encounter when using the billing module.

## Credit Account Issues

### "credit account not found"

**Cause**: The tenant has never had their credit account funded.

**Solution**: Fund the credit account first:
```bash
manifestd tx billing fund-credit [tenant-address] [amount] --from [key]
```

The credit account is created automatically when first funded.

### "insufficient credit balance"

**Cause**: The check is against *available* credit (available = balance − reserved_amounts), where reserved_amounts is the sum of reservations held by the tenant's existing PENDING and ACTIVE leases — not the raw balance. A tenant whose balance covers the `min_lease_duration` requirement can still hit this error if existing leases have that credit reserved. The full error text includes both balance and reserved, e.g. `insufficient available credit for denom <denom>: need <x>, have <y> available (balance: <b>, reserved: <r>)`. Check the `available_balances` field of `query billing credit-account` rather than the raw balance.

**Solution**: 
1. Check current balances:
   ```bash
   manifestd query billing credit-account [tenant-address]
   ```
2. Calculate minimum required for each denom used by SKUs in the lease:
   ```
   min_required[denom] = sum(sku_rate_per_second × quantity for that denom) × min_lease_duration
   ```
3. Fund additional credit for each denom:
   ```bash
   manifestd tx billing fund-credit [tenant-address] [amount_denom1] --from [key]
   manifestd tx billing fund-credit [tenant-address] [amount_denom2] --from [key]
   ```

**Example**: For a lease with SKU1 (10 upwr/second) and SKU2 (5 umfx/second) with `min_lease_duration` of 3600 seconds:
- Need at least 36,000 upwr
- Need at least 18,000 umfx

### "invalid denomination for credit account"

**Cause**: This error has been removed. Credit accounts now support any denomination.

**Note**: Credit accounts can hold multiple denominations. Simply fund the account with the tokens matching your target SKUs' `base_price` denoms.

## Lease Creation Issues

### "sku not found"

**Cause**: The specified SKU ID doesn't exist.

**Solution**: 
1. Query available SKUs:
   ```bash
   manifestd query sku skus
   ```
2. Use a valid SKU ID in your lease creation.

### "sku not active"

**Cause**: The SKU exists but has been deactivated.

**Solution**: 
1. Query the SKU to confirm:
   ```bash
   manifestd query sku sku [sku-id]
   ```
2. Contact the authority to reactivate the SKU, or use a different active SKU.

### "provider not found" or "provider not active"

**Cause**: The provider associated with the SKU doesn't exist or has been deactivated.

**Solution**: Contact the authority to create/reactivate the provider, or use SKUs from an active provider.

### "all SKUs in a lease must belong to the same provider"

**Cause**: Attempting to create a lease with SKUs from different providers.

**Solution**: Create separate leases for SKUs from different providers:
```bash
# Instead of this (fails):
manifestd tx billing create-lease <sku-uuid-provider1>:1 <sku-uuid-provider2>:1 --from tenant  # SKUs from different providers

# Do this:
manifestd tx billing create-lease <sku-uuid-provider1>:1 --from tenant  # Provider 1 SKUs only
manifestd tx billing create-lease <sku-uuid-provider2>:1 --from tenant  # Provider 2 SKUs only
```

### "maximum leases per tenant reached"

**Cause**: The tenant has too many active leases.

**Solution**: 
1. Check current leases:
   ```bash
   manifestd query billing leases-by-tenant [tenant-address] --state active
   ```
2. Close unused leases to free up slots:
   ```bash
   manifestd tx billing close-lease [lease-uuid] --from tenant
   ```
3. The default limit is 100 active leases per tenant.

### "maximum pending leases per tenant reached"

**Cause**: The tenant has too many pending leases awaiting acknowledgement.

**Solution**: 
1. Check current pending leases:
   ```bash
   manifestd query billing leases-by-tenant [tenant-address] --state pending
   ```
2. Wait for providers to acknowledge or reject pending leases, or cancel them:
   ```bash
   manifestd tx billing cancel-lease [lease-uuid] --from tenant
   ```
3. The default limit is 10 pending leases per tenant.

### "too many items in lease"

**Cause**: Attempting to create a lease with too many SKU items.

**Solution**: Split the lease into multiple smaller leases. The default limit is 20 items per lease (hard limit is 100).

### "quantity must be greater than zero"

**Cause**: Creating a lease item with quantity 0.

**Solution**: Ensure all items have quantity ≥ 1:
```bash
manifestd tx billing create-lease 01912345-6789-7abc-8def-0123456789ab:1 --from tenant
```

### "duplicate sku in lease items"

**Cause**: The same SKU UUID appears multiple times in the lease items without using service names.

**Solution**: Either combine quantities into a single item, or use service names for stack deployments:
```bash
# Option 1: Combine quantities into a single item
manifestd tx billing create-lease <sku-uuid>:5 --from tenant

# Option 2: Use service_names for stack deployments (same SKU, different services)
manifestd tx billing create-lease <sku-uuid>:2:web <sku-uuid>:3:db --from tenant
```

### "lease must contain at least one item"

**Cause**: Trying to create a lease with no items.

**Solution**: Specify at least one SKU item:
```bash
manifestd tx billing create-lease 01912345-6789-7abc-8def-0123456789ab:1 --from tenant
```

## Lease Acknowledgement Issues

### "lease not in pending state"

**Cause**: Attempting to acknowledge, reject, or cancel a lease that is not in PENDING state.

**Solution**: Check the lease state:
```bash
manifestd query billing lease [lease-uuid]
```

Only PENDING leases can be acknowledged/rejected by providers or cancelled by tenants.

### "unauthorized" (for acknowledge/reject)

**Cause**: The sender is neither the provider's management address for the SKUs in the lease nor the module authority. (Note: `allowed_list` does NOT grant acknowledge/reject — it only covers `CreateLeaseForTenant` and `SetItemCustomDomain`.)

**Solution**: Use the correct provider key:
```bash
manifestd tx billing acknowledge-lease [lease-uuid] --from [provider-key]
```

### "invalid rejection reason"

**Cause**: The rejection reason provided to `reject-lease` exceeds the maximum length of 256 characters.

**Solution**: Provide a shorter rejection reason:
```bash
# Maximum 256 characters
manifestd tx billing reject-lease [lease-uuid] --reason "Service unavailable" --from provider
```

### Provider not responding to pending lease

**What happens**:
1. Lease remains in PENDING state
2. After `pending_timeout` (default 30 minutes), the EndBlocker expires the lease
3. Lease state changes to EXPIRED
4. Credit is unlocked and remains in tenant's account

**What tenant can do**:
- Wait for automatic expiration
- Cancel the lease manually:
  ```bash
  manifestd tx billing cancel-lease [lease-uuid] --from tenant
  ```

## Lease Closure Issues

### "lease not found"

**Cause**: The specified lease UUID doesn't exist or is invalid.

**Solution**: 
1. Query your leases:
   ```bash
   manifestd query billing leases-by-tenant [your-address]
   ```
2. Use a valid lease UUID.

### "lease not active"

**Cause**: Attempting to close a lease that's not in ACTIVE state. Could be PENDING, CLOSED, REJECTED, or EXPIRED.

**Solution**: Check the lease state:
```bash
manifestd query billing lease [lease-uuid]
```

- **PENDING**: Use `cancel-lease` (tenant) or `reject-lease` (provider) instead
- **CLOSED/REJECTED/EXPIRED**: No action needed, lease is already terminated

### "unauthorized"

**Cause**: Attempting to close a lease you don't have permission to close.

**Solution**: Only the following can close an ACTIVE lease:
- The tenant who owns the lease
- The provider of the SKUs in the lease
- The module authority

## Withdrawal Issues

Withdrawals settle **streams**, not leases. A stream is the aggregate of every ACTIVE lease one tenant holds with the provider in one denom. See [Settlement](../README.md#settlement).

### Withdraw succeeded but nothing was withdrawn

A withdrawal that finds nothing to pay is not an error: the transaction succeeds with an empty `total_amounts` and `withdrawal_count` of 0. (The former "no withdrawable amount" error is no longer returned.)

**Cause**: 
1. The provider has no ACTIVE lease, so it has no stream (PENDING leases have not started billing, and CLOSED leases were settled when they closed), OR
2. Nothing has accrued since the streams were last settled. A stream is settled by every withdrawal, and also whenever one of its leases is acknowledged or closed. A second withdrawal in the same block pays nothing, because no time has passed since the first. A stream with a very low rate can also have accrued less than one unit of the denom; that amount is not lost, it is carried in the stream's `remainder` to the next settlement, OR
3. The streams are frozen for exhausted credit. A frozen stream accrues nothing more; its leases are about to be closed by the EndBlocker, OR
4. The tenant's credit balance in the stream's denom is already zero: nothing can be paid, and the withdrawal freezes the stream

**Solution**: 
1. Check what the provider can withdraw and the state of its streams:
   ```bash
   manifestd query billing provider-withdrawable [provider-uuid]
   manifestd query billing streams-by-provider [provider-uuid]
   ```
2. For PENDING leases, acknowledge them first
3. For healthy streams, wait for more time to pass for charges to accrue
4. For a stream with `exhausted_at` set, there is nothing more to collect on it

### "withdrawing from individual leases is no longer supported" / "lease_uuids is no longer supported"

**Cause**: Lease UUIDs were passed to `withdraw` (CLI), or `MsgWithdraw.lease_uuids` was set (rejected by the chain with `ErrInvalidRequest`). Withdrawing from specific leases is deprecated: settlement is per stream, so a lease cannot be withdrawn on its own.

**Solution**: Withdraw for the provider instead. It pays everything the provider's streams have accrued, including what the lease in question contributed:
```bash
manifestd tx billing withdraw --provider [provider-uuid] --from [provider-key]
```

### "unauthorized"

**Cause**: Only the provider (or authority) can withdraw a provider's accrued funds.

**Solution**: Use the provider's key to withdraw:
```bash
manifestd tx billing withdraw --provider [provider-uuid] --from [provider-key]
```

### `Query/WithdrawableAmount` returns "Unimplemented"

**Cause**: The per-lease withdrawable query (`manifestd query billing withdrawable`, REST `/lease/{lease_uuid}/withdrawable`) is deprecated. A lease has no withdrawable amount of its own. The CLI command has been removed; the gRPC method and REST route remain and always return `Unimplemented`.

**Solution**: Use the provider-level queries:
```bash
manifestd query billing provider-withdrawable [provider-uuid]
manifestd query billing streams-by-provider [provider-uuid]
```

### "provider not found"

**Cause**: The provider_uuid doesn't exist in the SKU module.

**Solution**: 
1. Query available providers:
   ```bash
   manifestd query sku providers
   ```
2. Use a valid provider UUID.

### Stream not paid by a withdraw

**Cause**: A withdraw settles each stream of the page in its own cached context; if a single stream fails, it is logged and skipped so the rest of the page still succeeds. Three things determine what a withdrawal pays:

1. **Only streams exist to be paid.** A stream exists while it has at least one ACTIVE lease. A tenant whose leases with the provider are all CLOSED has no stream: those leases were settled when they closed.
2. **A stream pays nothing for two different reasons.** A *normal* one: the stream has nothing to pay — no time elapsed since its last settlement, an accrual still below one unit of the denom (carried in `remainder`), a zero credit balance (the stream is then frozen), or a stream already frozen for exhausted credit. This is silent and expected, and the stream is still settled: its `last_settled_at` moves to the block time. An *error* skip: the stream's settlement fails — a bank transfer failure, or a missing or invalid provider payout address — in which case that stream's changes are discarded.
3. **The page may not have reached it.** A withdraw settles at most `--limit` streams (default 50, max 100). If the response has `has_more: true`, the stream may be on a later page.

**What happens**:
1. After an error skip the stream is left unchanged, including its `last_settled_at` and `remainder`, so what it accrued is paid by the next settlement that succeeds; other streams of the page are processed normally
2. The overall transaction still succeeds
3. No error is returned for such a stream — error skips are logged by the node, normal (nothing-to-pay) settlements are silent

> **Arithmetic overflow does NOT skip the stream.** If a stream's accrual is not representable (more than ~100 years since its last settlement, `MaxDurationSeconds`, or a payable amount too large to fit the chain's 256-bit integer), the settlement instead transfers the tenant's **entire remaining credit** in the stream's denom to the provider and freezes the stream, so the EndBlocker closes its leases. The funds are **not** preserved. Overflow is effectively unreachable in normal operation but can arise from a genesis import with an ancient stream `last_settled_at`.

**Solution**:
1. Inspect the stream: its `last_settled_at` tells you whether the last withdraw settled it, and `exhausted_at` whether it is frozen:
   ```bash
   manifestd query billing streams-by-provider [provider-uuid]
   ```
2. Check the node logs for `failed to settle stream` to see the actual error of an error skip.
3. Verify the provider's payout address is set and valid (`manifestd query sku provider [provider-uuid]`), then retry.

---

## Provider-Wide Withdraw Issues

### "must specify the --provider flag" / "provider_uuid is required"

**Cause**: `withdraw` was called without `--provider` (CLI), or `MsgWithdraw.provider_uuid` is empty (rejected by the chain with `ErrInvalidRequest`). A withdrawal is always for one provider.

**Solution**:
```bash
manifestd tx billing withdraw --provider [provider-uuid] --from [key]
```

### "invalid key (pagination cursor)"

**Cause**: The `--key` value does not decode as a withdraw cursor. The cursor is an opaque encoding of the tenant and denom of the last stream of the previous page; it is not a lease UUID, and a `next_key` saved from a release that paginated over leases is not valid.

**Solution**: Restart without `--key`, then pass the base64 `next_key` of each response verbatim.

### "key length N exceeds maximum 512"

**Cause**: The `--key` value is not a `next_key` from a prior response. The cursor is bounded by `MaxWithdrawCursorLen` (512 bytes).

**Solution**: Pass the base64 `next_key` from the previous response verbatim.

### "limit N exceeds maximum allowed 100"

**Cause**: `--limit` is above `MaxBatchLeaseSize` (100 streams per withdraw).

**Solution**: Use `--limit 100` or less and page with `--key`.

### Response shows "has_more: true"

**Cause**: The provider has more streams than the limit allows in one call.

**Solution**: Keep calling withdraw, passing the response's `next_key` back as `--key`, until `has_more` is false. Calling again without `--key` restarts from the first stream and never advances past the limit:
```bash
# First call (no cursor), larger limit (max 100)
manifestd tx billing withdraw --provider [provider-uuid] --limit 100 --from [key]

# Subsequent calls: pass next_key from the previous response as --key
manifestd tx billing withdraw --provider [provider-uuid] --limit 100 --key [next_key] --from [key]
```

> `next_key` is base64-encoded in the JSON/CLI response (it is a `bytes` field). Pass it verbatim to `--key` — it is not a UUID.

## Batch Operation Failures

### Batch operation failed entirely

**Cause**: All lease-UUID batch operations (acknowledge, reject, cancel, close) are **atomic**. (Withdraw is not a lease-UUID batch: it settles a provider's streams one by one and skips a stream that fails.) If any single lease in the batch fails validation, the entire batch fails and no changes are made.

**Common failure reasons:**
- One or more leases are in the wrong state (e.g., trying to acknowledge a CLOSED lease)
- Authorization failure on one lease (e.g., not the provider for that lease)
- One lease doesn't exist
- Leases belong to different providers (for operations requiring same-provider)

**Solution:**
1. Check each lease individually to identify the problem:
   ```bash
   for uuid in uuid1 uuid2 uuid3; do
     manifestd query billing lease $uuid
   done
   ```
2. Remove the problematic lease(s) from the batch
3. Retry the batch without the problematic lease(s)
4. Handle the problematic lease separately

**Example:**
```bash
# If batch acknowledge fails:
manifestd tx billing acknowledge-lease uuid1 uuid2 uuid3 --from provider  # FAILS

# Check each lease:
manifestd query billing lease uuid1  # PENDING - ok
manifestd query billing lease uuid2  # CLOSED - this is the problem!
manifestd query billing lease uuid3  # PENDING - ok

# Retry without uuid2:
manifestd tx billing acknowledge-lease uuid1 uuid3 --from provider  # SUCCESS
```

### "too many leases: N exceeds maximum 100"

**Cause**: Exceeding `MaxBatchLeaseSize` (100 leases) in a single batch operation. This is returned as `ErrInvalidLease` by `ValidateBatchLeaseUUIDs`.

**Solution**: Split into multiple smaller batches:
```bash
# Instead of 150 leases in one call:
manifestd tx billing acknowledge-lease uuid1 uuid2 ... uuid150 --from provider  # FAILS

# Split into batches of 100:
manifestd tx billing acknowledge-lease uuid1 ... uuid100 --from provider
manifestd tx billing acknowledge-lease uuid101 ... uuid150 --from provider
```

### Batch event not showing all details

**Cause**: Batch operations emit summary events (`batch_acknowledged`, `batch_rejected`, etc.) alongside individual events. The batch event shows aggregate information (count, provider) while individual events show per-lease details.

**Solution**: To see all details, check both event types:
```bash
# Get transaction events
manifestd query tx [txhash] --output json | jq '.events[] | select(.type | startswith("batch_") or startswith("lease_"))'
```

**Event pattern:**
- Individual operations: `lease_acknowledged`, `lease_rejected`, etc. (one per lease)
- Batch summary: `batch_acknowledged`, `batch_rejected`, etc. (one per transaction, emitted only when the batch contains more than one lease — a single-lease call emits only the individual event). Exception: withdraw always emits `batch_withdraw`, even when nothing was withdrawn, and has no per-lease event: it emits one `stream_settled` per stream that paid out.

---

## Query Issues

### "invalid lease_uuid format"

**Cause**: The lease UUID is not in valid UUIDv7 format.

**Solution**: Ensure you're using a valid UUID (format: `xxxxxxxx-xxxx-7xxx-xxxx-xxxxxxxxxxxx`):
```bash
manifestd query billing lease 01912345-6789-7abc-8def-0123456789ab
```

### "invalid tenant address"

**Cause**: The address format is invalid.

**Solution**: Use a valid bech32 address with the correct prefix:
```bash
manifestd query billing credit-account manifest1abc...
```

## Pending Lease Expiration

### Understanding Pending Expiration

Pending leases expire automatically if providers don't acknowledge them within `pending_timeout` (default 30 minutes).

**How it works**:
1. EndBlocker runs each block
2. Checks pending leases against `created_at + pending_timeout`
3. Expired leases transition to EXPIRED state
4. Credit is unlocked (was never billed since lease was never active)
5. `lease_expired` event is emitted (attributes: lease_uuid, tenant, provider_uuid, reason="pending_timeout")

**Rate limiting**: Max 100 expirations per block to prevent DoS.

### Lease expired while waiting for provider

**What happened**:
1. Lease was created in PENDING state
2. Provider did not acknowledge within `pending_timeout`
3. EndBlocker expired the lease

**What to do**:
1. Credit remains in your account - no funds lost
2. Contact the provider if this was unexpected
3. Create a new lease if desired

## Auto-Close Behavior (for ACTIVE leases)

### Understanding Auto-Close

Auto-close happens in two steps.

**Step 1: a transaction freezes the stream.** When a stream is settled and its accrual meets or exceeds the tenant's credit balance in the stream's denom (accrued >= balance) — the balance does not have to reach exactly zero — the settlement pays the provider what is left and freezes the stream (`exhausted_at` is set, `stream_exhausted` is emitted). When no time has elapsed since the last settlement, a zero balance in the stream's denom triggers it. A stream is settled during write operations only:
- `Withdraw` - Every stream of the page
- `AcknowledgeLease` - The streams the new leases join
- `CloseLease` - The streams the closed leases leave

No lease is closed by that transaction.

**Step 2: the EndBlocker closes the leases.** At the end of the block, and of the following blocks if needed, the EndBlocker closes the leases of frozen streams. It attempts at most 100 closures per block, shared between the frozen streams: within a block each stream served gets an equal share first, then what is left is spent in queue order, and when more than 100 streams are queued, successive blocks rotate through the queue. Each closed lease gets `closure_reason = "credit exhausted"` and a `lease_auto_closed` event, and its reservation is released.

**Important:** Auto-close does NOT happen:
- For PENDING leases (they don't accrue charges)
- During query operations (queries are read-only)
- Without a settlement first: the EndBlocker only closes leases of streams a transaction has already frozen. It does not look for tenants whose credit has run out, so those leases stay ACTIVE until the provider withdraws or a lease of the stream is acknowledged or closed

### Lease closed automatically

**What happened**:
1. During a Withdraw, AcknowledgeLease or CloseLease operation, a settlement found the tenant's credit exhausted in one of the lease's denoms
2. That settlement transferred the remaining balance to the provider and froze the stream
3. The EndBlocker closed the lease, in that block or one of the next few

**How to verify**:
```bash
# Check lease state: closure_reason is "credit exhausted", closed_at is the closing block's time
manifestd query billing lease [lease-uuid]

# The freeze is in the transaction that settled the stream
manifestd query tx [txhash] --output json | jq '.events[] | select(.type == "stream_exhausted")'

# The closure itself is a block event, not a transaction event
manifestd query block-results [height] --output json | jq '.finalize_block_events[] | select(.type == "lease_auto_closed")'
```

Events to look for:
- `stream_exhausted` (attributes `provider_uuid`, `tenant`, `denom`) in the transaction whose settlement froze the stream
- `lease_auto_closed` with `reason="credit_exhausted"` among the block events of the block in which the EndBlocker closed the lease
- `lease_closed` with `closed_by="credit_exhaustion"` if someone closed a lease of the frozen stream with `CloseLease` before the EndBlocker reached it (a `CloseLease` whose credit exactly covered the charge instead keeps the caller-supplied `closure_reason`/role)

**Resolution**: 
1. Fund the credit account again
2. Create a new lease

### I topped up, but my leases were closed anyway

**Cause**: The top-up landed after a settlement had already frozen the stream. A frozen stream is never unfrozen: its leases are closed by the EndBlocker regardless of the balance, and a new lease acknowledged into it before it is drained is closed with it. The credit you added is not spent on the frozen stream.

**Solution**:
1. Check whether a stream is frozen (`exhausted_at` set) and wait until it is gone:
   ```bash
   manifestd query billing streams-by-tenant [tenant]
   ```
2. Create new leases once the frozen stream no longer appears. They start a fresh stream.
3. To avoid it, top up **before** credit runs out. `manifestd query billing credit-estimate [tenant]` already deducts what your streams have accrued but not yet settled.

### A lease stays ACTIVE although the credit is exhausted

**Cause**: Credit exhaustion is only discovered when a transaction settles the stream. If nobody withdraws and no lease of that stream is acknowledged or closed, nothing is settled, the stream is not frozen and the lease stays ACTIVE. The provider is not paid more than the tenant's balance in any case.

**Solution**: The provider withdraws (`manifestd tx billing withdraw --provider [provider-uuid]`). That settles the stream, freezes it, and the EndBlocker closes its leases.

If the stream is already frozen (`exhausted_at` set) and the lease is still ACTIVE, the drain simply has not reached it yet. The EndBlocker attempts at most 100 closures per block and shares them between all frozen streams, so a stream with many leases, or many streams frozen at once, take several blocks. With up to 100 frozen streams, every one of them makes progress each block. With more, a block serves 100 of them and the next block resumes after the last one served, rotating through the whole queue, so a stream may wait for its turn (the longer the queue, the longer the wait) but none waits indefinitely behind others. A lease that fails to close is logged by the node (`failed to auto-close lease of exhausted stream`) and retried in a later block.

### Why doesn't my query show accurate accrued amounts?

**Cause**: Lease and stream queries return stored state without performing settlement calculations. A lease carries no settlement time at all (`Lease.last_settled_at` is deprecated and never set); a stream's `last_settled_at` shows when that stream was last settled.

**Explanation**: 
- `streams-by-provider` and `streams-by-tenant` return each stream's stored `rate`, `last_settled_at` and `remainder`
- They don't calculate time elapsed since then
- PENDING leases don't accrue charges (billing starts at acknowledgement)
- There is no per-lease accrued amount: accrual is tracked per stream, which aggregates every active lease of a tenant with a provider in a denom
- Use the `ProviderWithdrawable` query to get **real-time calculated amounts**:
  ```bash
  # Get real-time withdrawable for a provider's streams, one page at a time
  manifestd query billing provider-withdrawable [provider-uuid]
  ```
  A single `provider-withdrawable` response is a partial, per-page total over the
  provider's streams (page size default 100, max 1000). Loop, passing
  `pagination.next_key` back as `--page-key` (the base64 string exactly as printed),
  and sum the per-page `amounts` until `next_key` is empty. This query has no
  `has_more` field — branch on `len(pagination.next_key) > 0`.
- For one tenant's share, read its stream from `streams-by-provider`. The exact accrual is `(remainder + rate × nanoseconds since last_settled_at) / 1,000,000,000`, rounded down and capped at the tenant's credit balance in that denom. `rate ×` seconds since `last_settled_at` is a close approximation: it only ignores the carried `remainder`. A stream whose `last_settled_at` is earlier than the 2 → 3 upgrade block carries a debt from a payout that failed during the migration; the same formula covers it, and the next settlement pays it

### "invalid --page-key" on a stream query

**Cause**: `provider-withdrawable`, `streams-by-provider` and `streams-by-tenant` page over stream keys, which contain raw address bytes. Their `--page-key` must be the base64 `next_key` exactly as the previous response printed it; anything that is not valid base64 is rejected.

**Solution**: Copy `pagination.next_key` from the previous response verbatim:
```bash
manifestd query billing streams-by-provider [provider-uuid] --page-key <base64-next-key>
```

## Parameter Issues

### "invalid params"

**Cause**: Invalid parameter values in UpdateParams.

**Common issues**:
- `min_lease_duration` must be > 0 and ≤ 2592000 (30 days)
- `max_leases_per_tenant` must be > 0 and ≤ 10000
- `max_items_per_lease` must be > 0 and ≤ 100
- `max_pending_leases_per_tenant` must be > 0 and ≤ 1000
- `pending_timeout` must be between 60 (1 min) and 86400 (24 hours)
- `reserved_domain_suffixes`: each entry must start with '.', the part after the dot must be a valid FQDN, and duplicates are rejected

**Solution**: Check current params and ensure new values are valid:
```bash
manifestd query billing params
```

### "unauthorized" on UpdateParams

**Cause**: Only the module authority can update parameters.

**Solution**: Use the authority account (POA admin group):
```bash
manifestd tx billing update-params ... --from authority
```

## Gas and Transaction Issues

### "out of gas"

**Cause**: The transaction requires more gas than allocated.

**Solution**: Increase gas limit:
```bash
manifestd tx billing create-lease 01912345-6789-7abc-8def-0123456789ab:10 --from tenant --gas 500000
# Or use auto
manifestd tx billing create-lease 01912345-6789-7abc-8def-0123456789ab:10 --from tenant --gas auto --gas-adjustment 1.5
```

### Transaction pending/not included

**Cause**: Gas price too low or network congestion.

**Solution**: 
1. Wait for inclusion, or
2. Resubmit with higher gas price

## Getting Help

If you encounter an issue not covered here:

1. **Check the full error message**: The error often contains specific details
2. **Query relevant state**:
   ```bash
   manifestd query billing params
   manifestd query billing lease [lease-id]
   manifestd query billing credit-account [tenant]
   manifestd query sku sku [sku-id]
   manifestd query sku provider [provider-id]
   ```
3. **Check events**: Query the transaction to see emitted events
   ```bash
   manifestd query tx [txhash] --output json | jq '.events'
   ```

## Related Documentation

- [Billing README](../README.md) - Complete billing module overview
- [Migration Guide](MIGRATION.md) - Migrating existing off-chain leases
- [API Reference](API.md) - Detailed API documentation
- [Architecture](ARCHITECTURE.md) - Technical architecture details
