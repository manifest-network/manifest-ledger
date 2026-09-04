# Billing Module Migration Guide

This guide is for authority members responsible for migrating existing off-chain leases to the on-chain billing system.

## Overview

The migration process involves:
1. Setting up providers in the SKU module
2. Creating SKUs for billable items in the SKU module
3. Configuring billing parameters (limits, timeouts)
4. Funding tenant credit accounts
5. Creating leases on behalf of tenants using `MsgCreateLeaseForTenant`

### Module consensus v2→v3

The v3 billing migration rewrites disk encoding and repairs two historical
credit-account aggregates. Public protobufs continue to use Bech32 strings,
while stored Params allowed-list entries, Lease tenants, and CreditAccount
tenant/credit identities are rewritten as raw SDK address bytes. A separate
ascending credit-account pass uses the byte-addressed tenant/state index in
fixed PENDING→ACTIVE order to reconstruct live lease counts and calculate the
exact reservation floor from each live non-legacy lease's stored creation
duration, without decoding terminal lease history.
Equivalent allowed-list Bech32 spellings are collapsed by decoded address
identity while preserving first-seen list order.
The canonical Params are validated, including the 100-entry allow-list and
reserved-suffix caps, before the first migration write. An invalid or
over-limit list aborts the upgrade atomically; the migration never truncates
authority or domain-policy data. Migration and import preparation may collapse
more than 100 raw equivalent Bech32 spellings below the cap, while newly
submitted `MsgUpdateParams` values are capped and must already contain distinct
decoded identities.
Fully verifiable accounts, including accounts with only terminal legacy lease
history, are reconciled exactly to that floor. For an account with a live
legacy lease, each reservation denomination becomes the maximum of its stored
value and the provable floor, so unknown live-legacy excess is preserved and no
legacy amount is guessed. This changes only billing metadata; it does not mint,
burn, or transfer bank balances.

Value rewrites use bounded pages and close each iterator before writing. The
already-byte-addressed keys and indexes remain unchanged. Re-running the
migration produces the same bytes.

The existing tenant/state index must be complete and internally consistent for
each credit account's aggregate repair. Every PENDING or ACTIVE entry visited
for that account is checked against its primary lease's decoded tenant and
state; a disagreement fails closed instead of silently producing an incomplete
or double-counted reservation. This is not a comprehensive secondary-index
audit: tenants without credit accounts, missing live entries, and entries
stranded only under terminal-state keys are outside the repair scan. This
migration repairs derived credit-account aggregates; rebuilding corrupt
secondary indexes requires a separate full-primary-state repair before the
migration can complete.

### Module consensus v3→v4

The v4 migration converts the v3 account-only reservation aggregate into
consumable per-lease guarantees. It does not mint, burn, transfer, or otherwise
change bank balances. After the cutover, each tenant and denomination satisfies:

```
R = SUM(A for each live modern lease) + U
```

`R` is `CreditAccount.reserved_amounts`, `A` is a modern live lease's
`reservation.remaining_amounts`, and `U` is
`CreditAccount.unattributed_reserved_amounts`, the explicit shared allocation
for live historical leases whose individual claims cannot be reconstructed.
Terminal and historical leases receive initialized empty lease-side
reservation wrappers.

For each denomination, the migration's maximum allocation budget is
`min(v3 reserved_amounts, bank balance)`. It then:

1. Calculates every modern live lease's nominal creation-time claim.
2. Requires the v3 aggregate to cover the tenant's complete modern PENDING
   nominal sum in every denomination. A short aggregate is not a reachable
   bank-underbacking case; it indicates corrupt reservation or index state and
   halts the migration.
3. Compares the tenant's bank balances with that PENDING sum as one cohort. If
   every denomination is fully backed, every modern PENDING lease keeps its
   exact nominal claim. If any denomination is short, every modern PENDING lease
   for that tenant expires atomically at the upgrade block and receives an empty
   tranche. The migration never partially reserves a pending lease or selects
   arbitrary winners.
4. Treats modern ACTIVE nominal claims and one opaque live-historical cohort as
   claimants on the remaining bank-backed historical budget. When PENDING is
   preserved, its exact sum is removed from the budget first; when the cohort is
   expired, no PENDING claim participates.
5. Allocates that remainder proportionally with Hamilton's largest-remainder
   method. Modern ACTIVE claims use ascending lease UUID as the stable tie-break;
   the historical cohort follows them.
6. Writes the per-lease tranches, `U`, its exact live-member
   `unattributed_lease_count`, and aggregate `R` in ordered, bounded account
   pages. It also repairs active/pending counts and all affected state-dependent
   indexes from the resulting lease states. The historical count remains
   non-zero if the cohort allocation is fully consumed.

The tenant-wide expiration rule handles a state reachable under v2: settlement
could reduce the credit-address bank balance while a separate modern PENDING
claim remained in the account aggregate. Expiration sets each affected lease to
`EXPIRED` at the upgrade block, releases its claim without transferring funds,
updates its state indexes to EXPIRED, and removes its live custom-domain claim.
The original lease remains as terminal history. A client may submit a
replacement lease after the upgrade, subject to current prices, parameters,
limits, and available credit.

This cutover is a module migration or genesis normalization, not the normal
EndBlock timeout path, and it does not emit a `lease_expired` event for each
transition. Operators and indexers must reconcile the preflight UUID list with
post-cutover lease queries. A replacement has a new lease UUID: if deployment
data was already uploaded or a custom domain was claimed for the old request,
the client must repeat those steps and obtain a new provider acknowledgement.

The migration is idempotent when every lease already has a reservation wrapper
and fails closed on partially initialized state. A normal upgrade from v2 runs
v2→v3 before v3→v4, so raw-address canonicalization, count reconstruction,
and the provable aggregate repair happen before allocations are planned.

> **Required preflight:** For every tenant and denomination, calculate the
> complete modern PENDING nominal sum after the v2→v3 repair. If the repaired v3
> aggregate is smaller, treat the state as corruption and halt; investigate the
> tenant/state index and primary leases rather than manufacturing an allocation.
> A smaller bank balance is not a migration failure: it predicts tenant-wide
> atomic expiration of that tenant's modern PENDING cohort. Identify and notify
> affected clients so they can resubmit after the upgrade. Do not patch
> reservation metadata or manufacture backing. The cutover never mints, creates
> a partial pending guarantee, or favors a subset of pending leases.

The prediction is specific to the state height used for the preflight: funding,
new leases, and settlement can change it before the upgrade. Recompute it from
the final pre-upgrade snapshot. A tenant or other funder may prevent the
expiration by sending existing tokens through the normal `fund-credit` message
in every short denomination, but the balance must still cover the complete
modern PENDING cohort at cutover; an intervening v2 settlement can consume that
backing again.

Run the candidate binary's offline preflight against a complete exported
genesis. It reads the official billing and bank genesis types, uses the export's
`genesis_time` only as the planner's simulated timestamp, and writes
deterministic JSON to standard output without opening or changing application
state:

```bash
manifestd genesis preflight-billing-v4 exported-genesis.json \
  > billing-v4-preflight.json

jq '.reservation_change_tenant_count,
    .expiring_modern_pending_tenant_count,
    .expiring_modern_pending_lease_count' \
  billing-v4-preflight.json
jq '.tenants[] | select(.has_planned_reservation_change)' \
  billing-v4-preflight.json
jq '.tenants[] | select(.expiring_modern_pending_lease_uuids | length > 0)' \
  billing-v4-preflight.json
```

Archive the export, its committed app hash, the candidate binary checksum, and
the report together. The report carries `source_chain_id` and
`source_initial_height` from the export so automation can reject a wrong-chain
or wrong-height report. `source_initial_height` is the export's restart height
(normally the committed export height plus one, or zero for a zero-height
export), not the last committed block height itself. `input_genesis_time` is
also copied and normalized to UTC. Cosmos SDK export preserves the chain's
original genesis timestamp, so this field is provenance—not the future
upgrade-block time. Cohort selection does not depend on that timestamp in v4;
the real migration writes the actual upgrade block time to newly expired
leases.

`billing_state` is `pre_v4_aggregate` when the command actually applies the
cutover planner and `consumable_v4` when the input is already in the new
representation. Already-v4 input is never repaired: the command fails if its
reservation aggregate is not fully bank-backed. It also fails closed on a
missing or malformed billing/bank app state, mixed reservation formats,
duplicate decoded address identities, or any planner invariant failure.

`denominations` records the source aggregate, repaired pre-cutover aggregate,
planned post-cutover aggregate, pre/post opaque unattributed cohort allocation,
bank balance, complete modern PENDING requirement, and shortfall as base-10
integer strings. `modern_active_leases` is sorted by UUID and records each
modern ACTIVE lease's nominal and planned remaining denomination amounts;
legacy leases remain one opaque aggregate cohort and are never assigned
individual predictions. `reservation_change_tenant_count` counts tenants whose
source/pre/post aggregates, ACTIVE allocation, opaque cohort allocation, or
PENDING lifecycle will change economically; initializing fully backed wrapper
fields alone does not increment it. The two explicitly named PENDING counters
and `expiring_modern_pending_lease_uuids` describe only tenant-wide PENDING
expiration.

This command previews billing reservation migration only. It does not run
`ValidateWithBlockTime`, resolve provider/SKU references from `x/sku`, or
certify that the document will pass full `InitGenesis`. Use `validate-genesis`
and an isolated start/import rehearsal for those separate checks. The planner
timestamp affects the simulated, unreported `expired_at` transition but not
cohort selection. This remains a snapshot report, not a promise about later
chain state: rerun it on the final height-labelled export immediately before
the upgrade and compare that final report with the post-upgrade queries.

An exported genesis contains primary module records but not collections
secondary indexes. The command therefore cannot certify that the source node's
tenant/state indexes agree with those records; the in-place migration still
fails closed when an indexed row decodes to a mismatched tenant or state. Run
the source binary's state/invariant checks during rehearsal and investigate any
index or primary-record error rather than treating this offline report as a
general database-integrity certificate.

## Prerequisites

- You must be the **module authority** (POA admin group address) OR
- Your address must be in the `allowed_list` billing parameter
- The **SKU module** must have the required providers and SKUs already created
- You must have sufficient tokens (matching SKU denominations) to fund tenant credit accounts

## Step 1: Configure Billing Parameters

Before migrating, ensure billing parameters are properly set:

```bash
# Check current parameters
manifestd query billing params
```

If parameters need updating:

```bash
# Update params via authority
manifestd tx billing update-params \
  100 \
  20 \
  3600 \
  10 \
  1800 \
  --from authority
```

**Parameters:**
| Parameter | Description | Default |
|-----------|-------------|---------|
| `max_leases_per_tenant` | Max active leases per tenant | 100 |
| `max_items_per_lease` | Max items in single lease | 20 |
| `min_lease_duration` | Min seconds credit must cover | 3600 |
| `max_pending_leases_per_tenant` | Max pending leases per tenant | 10 |
| `pending_timeout` | Seconds before pending lease expires | 1800 |
| `allowed_list` | Up to 100 distinct decoded identities allowed to create leases for tenants | `[]` |
| `reserved_domain_suffixes` | Up to 100 DNS suffixes tenants may not claim as a `custom_domain`; each must begin with `.` | empty |

**Note:** `--allowed-list` and `--reserved-domain-suffixes` are preserve-on-omit: when the flag is absent the CLI round-trips the current on-chain value, so the bare `update-params 100 20 3600 10 1800` above will not wipe them. Pass the flag with an empty value (e.g. `--reserved-domain-suffixes=""`) to explicitly clear the list.

**Note:** There is no global `denom` parameter. Each SKU defines its own denomination in its `base_price`, enabling multi-denom billing.

### Adding Addresses to the Allow List

If you want non-authority addresses to create leases for tenants:

```bash
manifestd tx billing update-params \
  100 \
  20 \
  3600 \
  10 \
  1800 \
  --allowed-list "manifest1allowed1...,manifest1allowed2..." \
  --from authority
```

## Step 2: Verify SKU Setup

Before migrating leases, ensure all necessary providers and SKUs exist:

```bash
# List all providers
manifestd query sku providers

# List all SKUs
manifestd query sku skus

# Query specific SKU to verify details
manifestd query sku sku [sku-id]
```

**Important:** Note the SKU IDs and their per-second rates. You'll need these to calculate minimum credit requirements.

## Step 3: Fund Tenant Credit Accounts

Each tenant needs credit before a lease can be created for them. The credit must cover at least `min_lease_duration` seconds of the lease.

### Calculate Minimum Credit Required

For each denomination used by the SKUs in the lease:

```
min_credit[denom] = sum(sku_rate_per_second × quantity for SKUs with that denom) × min_lease_duration
```

**Example (single denom):**
- SKU 1: 1 upwr/second, quantity 2 → 2 upwr/second
- SKU 2: 5 upwr/second, quantity 1 → 5 upwr/second
- Total rate: 7 upwr/second
- Min duration: 3600 seconds
- Min credit: 7 × 3600 = 25,200 upwr

**Example (multi-denom):**
- SKU 1: 1 upwr/second, quantity 2 → 2 upwr/second
- SKU 2: 3 umfx/second, quantity 1 → 3 umfx/second
- Min duration: 3600 seconds
- Min credit needed: 7,200 upwr AND 10,800 umfx

### Fund the Credit Account

```bash
# Fund a tenant's credit account
manifestd tx billing fund-credit [tenant-address] [amount] --from [authority-key]

# Example: Fund with 100,000,000 upwr (100 PWR)
manifestd tx billing fund-credit manifest1abc... 100000000upwr --from authority

# For multi-denom, fund each denom separately
manifestd tx billing fund-credit manifest1abc... 100000000upwr --from authority
manifestd tx billing fund-credit manifest1abc... 50000000umfx --from authority

# Verify credit was received
manifestd query billing credit-account [tenant-address]
```

**Note:** Anyone can fund any tenant's credit account - this is not restricted to authority. Credit accounts support multiple denominations.

## Step 4: Create Leases for Tenants

Use `MsgCreateLeaseForTenant` to create leases on behalf of users:

```bash
# Create a lease for a tenant
# Format: sku-uuid:quantity[:service_name]
manifestd tx billing create-lease-for-tenant [tenant-address] [sku-uuid:quantity...] --from [authority-key]

# Optionally append :service_name (an RFC 1123 DNS label) for stack deployments. It is
# all-or-nothing across the lease's items; in service-name mode the same SKU may appear
# multiple times, and custom_domain can later be set per item via set-item-custom-domain.

# Example: Create lease with 2 units of SKU 1 and 1 unit of SKU 2
manifestd tx billing create-lease-for-tenant manifest1abc... 01912345-6789-7abc-8def-0123456789ab:2 01912345-6789-7abc-8def-0123456789ac:1 --from authority
```

### Important: Leases Start in PENDING State

When you create a lease (via `MsgCreateLeaseForTenant` or `MsgCreateLease`), it starts in **PENDING** state. The provider must acknowledge the lease before billing begins:

```bash
# Provider acknowledges the lease (transitions PENDING → ACTIVE)
manifestd tx billing acknowledge-lease [lease-uuid] --from provider-key
```

For migrations, you may want to have the provider pre-acknowledge leases immediately after creation.

### Important Constraints

1. **All SKUs must be from the same provider** - Create separate leases for different providers
2. **All SKUs must be active** - Deactivated SKUs cannot be leased
3. **Provider must be active** - Deactivated providers cannot have new leases
4. **Credit must cover min_lease_duration** - Otherwise creation fails
5. **Pending timeout** - If provider doesn't acknowledge within `pending_timeout` (default 30 min), lease expires

### Multiple SKUs in One Lease

A single lease can include multiple SKUs, but they must all belong to the same provider:

```bash
# Multiple SKUs from the same provider
manifestd tx billing create-lease-for-tenant manifest1abc... <sku-uuid-1>:1 <sku-uuid-2>:2 <sku-uuid-3>:1 --from authority
```

### Batch Migration Script Example

For migrating many leases, consider a script that creates and acknowledges:

```bash
#!/bin/bash
# migration_script.sh

AUTHORITY_KEY="authority"
PROVIDER_KEY="provider"
DENOM="upwr"  # or your factory denom

# Array of tenant migrations: "address|sku_items|credit_amount"
MIGRATIONS=(
  "manifest1abc...|<sku-uuid>:2|100000000"
  "manifest1def...|<sku-uuid-1>:1 <sku-uuid-2>:1|50000000"
  "manifest1ghi...|<sku-uuid-3>:5|200000000"
)

for migration in "${MIGRATIONS[@]}"; do
  IFS='|' read -r tenant items credit <<< "$migration"
  
  echo "Processing tenant: $tenant"
  
  # Fund credit account
  echo "  Funding ${credit}${DENOM}..."
  manifestd tx billing fund-credit "$tenant" "${credit}${DENOM}" \
    --from "$AUTHORITY_KEY" -y --gas auto --gas-adjustment 1.5
  
  sleep 6  # Wait for block
  
  # Create lease (starts in PENDING state)
  echo "  Creating lease with items: $items..."
  RESULT=$(manifestd tx billing create-lease-for-tenant "$tenant" $items \
    --from "$AUTHORITY_KEY" -y --gas auto --gas-adjustment 1.5 --output json)
  
  # In sync broadcast mode the tx response carries only code/txhash/raw_log and no
  # events, so capture the txhash, wait a block, then query the tx for its events.
  TXHASH=$(echo "$RESULT" | jq -r '.txhash')
  
  sleep 6  # Wait for block
  
  LEASE_UUID=$(manifestd query tx "$TXHASH" --output json | jq -r '.events[] | select(.type=="lease_created") | .attributes[] | select(.key=="lease_uuid") | .value')
  
  # Acknowledge lease (transitions to ACTIVE, billing starts)
  echo "  Acknowledging lease $LEASE_UUID..."
  manifestd tx billing acknowledge-lease "$LEASE_UUID" \
    --from "$PROVIDER_KEY" -y --gas auto --gas-adjustment 1.5
  
  sleep 6  # Wait for block
  
  echo "  Done!"
done

echo "Migration complete!"
```

## Step 5: Verify Migration

After migration, verify the leases were created and acknowledged correctly:

```bash
# Query tenant's leases (should show ACTIVE state)
manifestd query billing leases-by-tenant [tenant-address] --state active

# Query tenant's credit account
manifestd query billing credit-account [tenant-address]

# Query specific lease details
manifestd query billing lease [lease-uuid]
```

## Important Notes

### Lease Lifecycle

1. **PENDING**: Lease created, credit locked, awaiting provider acknowledgement
2. **ACTIVE**: Provider acknowledged, billing has started
3. **CLOSED**: Lease terminated normally

For migrations, ensure the provider acknowledges leases promptly to avoid them expiring.

### Price Locking

When you create a lease, the current SKU prices are **locked in** as per-second rates for the duration of that lease. If SKU prices change later, existing leases continue at their locked prices.

### Credit Persistence

Credit that remains in a tenant's credit account stays there. There is no mechanism to withdraw unused credit - this mimics typical cloud provider behavior where credits must be spent.

### Events

Each lease creation and state transition emits events for auditing:

```bash
# Query events for a transaction
manifestd query tx [txhash] --output json | jq '.events'
```

Key events:
- `lease_created` - Contains `lease_uuid`, `tenant`, `provider_uuid`
- `lease_acknowledged` - Contains `lease_uuid`, `tenant`, `provider_uuid`, `acknowledged_by` (the ack timestamp comes from the `acknowledged_at` field of `MsgAcknowledgeLeaseResponse`, not the event)
- `lease_rejected` - Contains `lease_uuid`, `tenant`, `provider_uuid`, `rejected_by`, `rejection_reason`
- `lease_closed` - Contains `lease_uuid`, `tenant`, `settled_amounts`
- `credit_funded` - Contains `tenant`, `amount`, `credit_address`

### Settlement

Leases created via `MsgCreateLeaseForTenant` work exactly like tenant-created leases:
- Billing only starts after provider acknowledgement (ACTIVE state)
- Settlement happens during `Withdraw` or `CloseLease` operations
- Auto-close triggers when credit is exhausted during write operations
- Tenants can close their own leases (even if created by authority)

## Rollback Considerations

If a migration needs to be reversed:

1. **For PENDING leases**: Cancel or reject them
   ```bash
   # Tenant cancels
   manifestd tx billing cancel-lease [lease-uuid] --from tenant
   # Or provider rejects
   manifestd tx billing reject-lease [lease-uuid] --from provider
   ```

2. **For ACTIVE leases**: Close the lease
   ```bash
   manifestd tx billing close-lease [lease-uuid] --from authority
   ```

3. **Settlement happens automatically**: Closure transfers
   `min(accrued, B - (R - A))` to the provider, consuming the target lease's
   guarantee before unreserved credit and preserving every other reservation

4. **Credit remains**: Any unspent credit stays in the tenant's credit account for future use

5. **Provider withdrawal**: Provider should withdraw any accrued funds before/after closing if needed
   ```bash
   manifestd tx billing withdraw [lease-uuid] --from provider
   ```

## Common Issues

### "insufficient credit balance"

The tenant doesn't have enough credit to cover `min_lease_duration` for one or more denominations. Calculate the minimum required for each denom:

```bash
# Check SKU rates and denoms
manifestd query sku sku [sku-uuid]

# For each denom: sum(rate × quantity) × min_lease_duration
# Fund accordingly
manifestd tx billing fund-credit [tenant] [amount_denom1] --from authority
manifestd tx billing fund-credit [tenant] [amount_denom2] --from authority
```

### "credit account not found"

This happens if you try to create a lease before funding. The credit account is created automatically when first funded:

```bash
manifestd tx billing fund-credit [tenant] [amount] --from authority
```

### "sku not found" or "sku not active"

Ensure the SKU exists and is active:

```bash
manifestd query sku sku [sku-uuid]
```

If the SKU doesn't exist, create it first via the SKU module.

### "all SKUs must belong to the same provider"

Multi-provider leases are not supported. Create separate leases for different providers:

```bash
# Provider 1 SKUs
manifestd tx billing create-lease-for-tenant manifest1abc... <provider1-sku-uuid>:1 --from authority

# Provider 2 SKUs (separate lease)
manifestd tx billing create-lease-for-tenant manifest1abc... <provider2-sku-uuid>:1 --from authority
```

### "unauthorized"

Your address is not the authority and not in the `allowed_list`. Check params:

```bash
manifestd query billing params
```

To add your address to the allow list, the authority must update params.

### "provider not active"

The provider associated with the SKU has been deactivated. Contact the authority to reactivate, or use SKUs from an active provider.

### Lease expires before acknowledgement

If the lease expires (remains in PENDING past `pending_timeout`), it transitions to EXPIRED state. The credit is not consumed and remains available. To avoid this during migration:

1. Increase `pending_timeout` temporarily via `update-params`
2. Have provider ready to acknowledge immediately after creation
3. Script the create and acknowledge together (as shown above)

## Genesis Import Validation

When importing billing state via genesis (e.g., during chain upgrades or migrations), the module performs three-phase validation and accepts two complete reservation representations:

- **v4 consumable state:** every lease has a `reservation` wrapper and every
  account satisfies `R = sum(live modern A) + U` exactly; its
  `unattributed_lease_count` exactly matches live historical leases.
- **pre-v4 aggregate-only (v2/v3) export:** every lease omits `reservation` and
  every account has empty `U` and zero `unattributed_lease_count`.
  `InitGenesis` normalizes this format in memory with the same no-mint,
  tenant-wide PENDING-cohort/Hamilton planner used by v3→v4.

Mixing present and absent reservation wrappers is rejected.

### Phase 1: Static Validation (`ValidateGenesis`)

Validates without blockchain context:
- Lease UUIDs are valid and unique
- Every lease has 1–100 items, matching the runtime hard limit
- `created_at` and `last_settled_at` are non-zero, with
  `created_at <= last_settled_at`
- Credit account addresses are correctly derived
- Required fields are present
- Canonical allowed lists and reserved-domain suffix lists contain at most 100 entries
- After import preparation, active and pending lease counts match the imported lease set
- The lease sequence is at least the total number of imported leases
- v4 lease tranches are valid and satisfy the exact account invariant, or a complete pre-v4 aggregate-only state can be deterministically reconciled to its statically reconstructible floor

The `validate-genesis` CLI and `InitGenesis` use the same import-safe
`GenesisState.Validate()` contract. For v4 state, modern PENDING tranches must
equal nominal, modern ACTIVE tranches must not exceed nominal, terminal and
historical lease tranches must be empty, and `U` may exist only with a live
historical lease. For a pre-v4 export, static validation cannot reconstruct an
individual historical claim, so it preserves the import-safe known-floor
check until `InitGenesis` creates explicit allocations.
At runtime, historical terminal transitions decrement the prepared
`unattributed_lease_count` in O(1) and release the exact remaining `U` when the
counter reaches zero; no bounded legacy scan or parameter-based estimate is
used.
Existing custom-domain claims are likewise not rechecked against the current
reserved-suffix list because a claim may predate a later reservation. For newly
authored state, `ValidateStrict()` opts into both present-day policy checks.

Static `validate-genesis` has no bank keeper and therefore cannot prove that a
pre-v4 aggregate is actually bank-backed. Import preparation first mirrors the
v2→v3 repair by reconstructing each tenant's modern live floor. With a live
zero-duration lease it keeps the per-denomination maximum of the exported
aggregate and that floor, preserving opaque historical excess; without a live
opaque claimant it uses the exact floor and drops stale residuals. This repairs
the reachable case where historical parameter-dependent release clamped through
a concurrent modern claim. The input JSON is not mutated, and
`ValidateStrict()` remains non-repairing.

For a pre-v4 representation, bank underbacking is handled only when
`InitGenesis` has bank keeper access. If any denomination is short, import
preparation expires all of that tenant's modern PENDING leases atomically at the
genesis block time, recomputes the resulting counts, and allocates the
bank-backed budget only among modern ACTIVE claims and the live historical
cohort. `InitGenesis` subsequently builds indexes from those normalized lease
states. Therefore a successful static `validate-genesis` does not promise that
pre-v4 PENDING leases will remain pending after import. An already-v4
consumable import is not re-planned: `InitGenesis` rejects it before billing
writes if its aggregate is not bank-backed.

A direct v2 genesis import deterministically rebuilds the cached active and
pending lease counts and the bounded aggregate repair above from primary lease
state before validation. Tenant leases are grouped by decoded SDK address bytes,
so equivalent historical Bech32 spellings contribute to the same account; the
repaired derived fields are what the planner starts from. If the bank-backed
policy expires a tenant's modern PENDING cohort, `InitGenesis` persists the
recomputed counts and builds indexes from the resulting terminal states.
`ValidateStrict()` does not perform these import repairs and rejects stale
derived state in newly authored genesis. A live v2 chain upgrade runs the
registered v2→v3 and v3→v4 migrations sequentially, with v2→v3 performing the
same initial in-place repair.

### Phase 2: Time-Based Validation (`ValidateWithBlockTime`)

Validates timestamps against block time during `InitGenesis`:

| Field | Validation |
|-------|------------|
| `last_settled_at` | Must not be in the future (static validation also requires it to be non-zero and no earlier than `created_at`) |
| `created_at` | Must not be in the future (static validation also requires it to be non-zero) |
| `closed_at` | Must not be in the future (for CLOSED leases with a non-nil `closed_at`) |

**Note:** `rejected_at`, `expired_at`, and `acknowledged_at` are NOT time-validated at genesis, so a future-dated value in those fields will import silently.

**Error Example:**
```
lease abc123 has last_settled_at (2025-01-08T00:00:00Z) in the future relative to block time (2025-01-07T12:00:00Z)
```

**Resolution:** Ensure all timestamps in genesis state are at or before the genesis block time.

### Phase 3: Cross-Module Validation (`InitGenesis`)

During `InitGenesis`, the module additionally performs cross-module checks against the SKU module:
- The lease's provider must exist (`skuKeeper.GetProvider`)
- Every item's SKU must exist (`skuKeeper.GetSKU`)
- Each SKU's `provider_uuid` must match the lease's `provider_uuid`
- Each prepared v4 reservation aggregate must be backed by the derived credit address's bank balance
- A bank-underbacked modern PENDING cohort is expired tenant-wide before final
  counts, indexes, and consumable allocations are persisted

These checks require the SKU module to be initialized first, which the genesis order (`sku -> billing`) guarantees.

## Related Documentation

- [Billing README](../README.md) - Complete billing module overview
- [API Reference](API.md) - Detailed API documentation
- [Troubleshooting Guide](TROUBLESHOOTING.md) - Common issues and solutions
- [Architecture](ARCHITECTURE.md) - Technical architecture details
