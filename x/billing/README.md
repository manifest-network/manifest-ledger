# x/billing

The `billing` module provides a credit-based billing system for leasing SKU resources. It enables tenants to fund credit accounts and create leases for SKU items, with automatic settlement and provider withdrawal capabilities.

## Concepts

### Credit Accounts

Each tenant has a credit account with a derived address. Credit accounts can hold any token denomination that matches the SKU's base_price denomination.

- **Credit Address**: Deterministically derived from the tenant's address
- **Balances**: Current credit balances (supports multiple denominations)
- **Top-up**: Anyone can fund a tenant's credit account with any token

### Credit Reservation System

The credit reservation system prevents overbooking by tracking reserved amounts per tenant. When a lease is created, credits are reserved to guarantee that sufficient funds exist for at least `min_lease_duration` seconds of operation.

**Why Reservations Matter:**

Without reservations, a tenant could create multiple leases that exceed their credit balance:
```
Tenant balance: 100 credits
MinLeaseDuration: 1 hour

Lease A: 30/hour → Check: 100 >= 30 ✓ Created
Lease B: 30/hour → Check: 100 >= 30 ✓ Created
Lease C: 30/hour → Check: 100 >= 30 ✓ Created
Lease D: 30/hour → Check: 100 >= 30 ✓ Created

Result: 4 leases × 30/hour = 120/hour liability, only 100 credits
→ Overbooking! Providers don't get paid fairly.
```

With the reservation system:
```
Tenant balance: 100 credits, reserved: 0
Available: 100 - 0 = 100

Lease A: 30/hour → Reserve 30 → Available: 100 - 30 = 70 ✓
Lease B: 30/hour → Reserve 30 → Available: 70 - 30 = 40 ✓
Lease C: 30/hour → Reserve 30 → Available: 40 - 30 = 10 ✓
Lease D: 30/hour → Reserve 30 → Available: 10 < 30 ✗ Rejected

Result: 3 leases, properly collateralized.
```

**Available Credit Calculation:**
```
AvailableCredit = CreditBalance - ReservedAmounts
```

New leases can only be created if `AvailableCredit >= NewLeaseReservation` for all required denominations.

**Reservation Lifecycle:**
- **Added**: When a lease is created (enters PENDING state)
- **Maintained**: When a lease is acknowledged (transitions to ACTIVE state)
- **Released**: When a lease transitions to CLOSED, REJECTED, or EXPIRED

**Parameter Change Protection:**

Each lease stores `MinLeaseDurationAtCreation` to ensure consistent reservation calculation regardless of subsequent governance changes to the `MinLeaseDuration` parameter. This prevents existing reservations from becoming inconsistent when parameters change.

### Multi-Denomination Support

The billing module supports multiple token denominations:
- Each SKU defines its own `base_price` with a specific denomination
- Credit accounts can hold multiple denominations
- When creating a lease, the credit account must have sufficient balance in the denominations used by the leased SKUs
- Settlement transfers use the denomination specified in the SKU's locked price

### Leases

A lease represents an agreement between a tenant and a provider for one or more SKU items.

- **Tenant**: The address that created and pays for the lease
- **Provider UUID**: All SKUs in a lease must belong to the same provider
- **Items**: List of SKU items with locked-in prices and quantities
- **State**: PENDING, ACTIVE, CLOSED, REJECTED, or EXPIRED
- **Settlement**: An ACTIVE lease accrues through its settlement streams (see [Settlement](#settlement)); the lease itself carries no settlement timestamp

### Lease Lifecycle

Leases follow a two-phase commit pattern:

1. **PENDING**: Tenant creates lease, credit is locked, awaiting provider acknowledgement
2. **ACTIVE**: Provider acknowledges, billing starts from acknowledgement time
3. **CLOSED**: Lease terminated normally (by tenant, provider, or credit exhaustion)
4. **REJECTED**: Provider rejected the pending lease, or tenant cancelled it
5. **EXPIRED**: Pending lease timed out (exceeded `pending_timeout` parameter)

### Custom Domains (per-item)

Each `LeaseItem` can carry an optional `custom_domain` — an FQDN that the
provider routes to that item's container with a TLS cert provisioned via
HTTP-01. Domains are set or cleared after lease creation via
`MsgSetItemCustomDomain` and addressed by `service_name` (use `""` for a
1-item legacy lease).

**Authorisation.** The lease tenant, the module authority, and any address in
`params.allowed_list` may set or clear a domain. The lease must be in
`PENDING` or `ACTIVE` state; closed/rejected/expired leases are immutable.

**Validation rules** (`IsValidFQDN`):
- 1 to **253** bytes (`MaxCustomDomainLength`); lowercase only.
- ≥ 1 dot separator (at least 2 labels).
- Each label is RFC 1123 (1-63 alphanumerics + hyphens, no leading/trailing
  hyphen).
- The TLD label must contain at least one non-digit (rejects raw IPs).
- No scheme (`://`), no leading or trailing dot, and none of the literal characters `/`, space, `\t` (tab), `@`, `*`, `?`, `#`.

**Reserved suffixes** (`params.reserved_domain_suffixes`). Tenants cannot
claim a domain that matches any reserved suffix. Each suffix entry must begin
with `.` (e.g. `.barney0.manifest0.net`); a domain matches when it ends with
the suffix at a label boundary, or equals the suffix's apex
(`barney0.manifest0.net`). The check is case-insensitive and fail-closed on
malformed entries. Reserved suffixes are tunable via governance — providers
that bring up new wildcard zones can be added without a chain upgrade.

**Uniqueness.** A domain may be claimed by at most one PENDING/ACTIVE lease
item at a time. The keeper maintains a `CustomDomainIndex` reverse-lookup
(prefix `0x0C`, value `CustomDomainTarget{lease_uuid, service_name}`) so
`QueryLeaseByCustomDomain` returns the routing target in O(1) without
iterating items. Closing, rejecting, expiring, or auto-closing a lease frees
the index entry. Re-setting the same domain on the same item is idempotent
(no event, no state change).

**Multi-item legacy leases cannot use custom_domain.** When a multi-item
lease was created without `service_name`s (legacy mode), the addressing key
is empty for all items and the lookup is ambiguous. Recreate the lease in
service-name mode instead.

**Events**:
- `lease_custom_domain_set` — attributes: `lease_uuid`, `tenant`, `provider_uuid`, `service_name`, `custom_domain`, `set_by`
- `lease_custom_domain_cleared` — same attributes (`custom_domain` carries the previous value)

`set_by` ∈ {`tenant`, `authority`, `allowed`} indicates the role under which
the call was authorised.

### Price Locking

When a lease is created, the current prices of all SKUs are locked in for the duration of the lease. Price changes to SKUs only affect newly created leases.

### Settlement

Accrual is settled per **stream**, not per lease. A stream is the aggregate of every ACTIVE lease one tenant holds with one provider in one denom, keyed by `(provider_uuid, tenant, denom)`:

- `rate`: the summed per-second rate of those leases (`locked_price × quantity` over their items priced in the stream's denom)
- `last_settled_at`: the time of the stream's last settlement
- `lease_count`: how many ACTIVE leases it aggregates
- `exhausted_at`: set once the tenant's credit ran out (see [Overdraw and Auto-Close](#overdraw-and-auto-close))
- `remainder`: accrual carried to the next settlement: what did not amount to a whole unit of the denom at the last one (see [Stream](#stream) for the one case where it holds more)

A stream exists if and only if it has at least one ACTIVE lease. A lease priced in two denoms belongs to two streams.

Accrual is exact. Rates are per second and block time has nanosecond precision, so a settlement at block time `T` computes:

```
total           = remainder + rate × nanoseconds elapsed since last_settled_at
payable         = total / 1,000,000,000        (whole units of the denom)
remainder       = total % 1,000,000,000        (carried to the next settlement)
last_settled_at = T
```

It pays the provider `payable`, capped at the tenant's credit balance in that denom, with a **single** bank transfer to the provider's payout address, whatever the number of leases behind it. Nothing is rounded away between settlements, however often a stream is settled, and because `last_settled_at` always moves to the settlement time, a lease that joins a stream is never charged for time before it joined.

Settlement is performed **lazily** (on-touch):
- When a provider withdraws (`MsgWithdraw`): every stream of the page is settled
- When a lease joins a stream (`MsgAcknowledgeLease`): the stream is settled before its rate grows
- When a lease leaves a stream (`MsgCloseLease`, or a closure by the EndBlocker): the stream is settled before its rate drops

A message settles each stream it touches at most once, however many of its leases belong to that stream.

This design keeps on-chain operations light and avoids per-block token transfers: the cost of a withdrawal depends on the number of streams (tenant and denom pairs) a provider has, not on the number of leases. A withdrawal of one stream costs about 54k gas whether the stream aggregates 1, 50 or 382 leases.

### Overdraw and Auto-Close

If a tenant's credit balance cannot cover what a stream has accrued, the stream is **frozen** and its leases are closed by the EndBlocker.

**How it works:**
1. A settlement (on withdraw, acknowledge or close) finds `accrued >= credit balance` for the stream's denom
2. Inside that transaction the settlement:
   - Transfers what is left of the balance to the provider
   - Sets the stream's `exhausted_at` to the block time (the stream is now frozen) and queues it for the EndBlocker
   - Emits `stream_exhausted`
   - Closes **no** lease
3. The EndBlocker of that block and, if needed, of the following blocks closes the frozen stream's leases. It attempts at most 100 closures per block (`MaxAutoClosesPerBlock`), a budget shared between the frozen streams: within a block, each stream it serves first gets an equal share, then what is left is spent in queue order; when more than 100 streams are queued, successive blocks rotate through the queue. Each closed lease gets: state `CLOSED`, `closed_at` = the time of the block that closes the lease, `closure_reason = "credit exhausted"`, reservation released, `active_lease_count` decremented, one `lease_auto_closed` event per lease
4. When its last lease is closed, the stream is deleted

**Consequences of freezing:**
- A frozen stream accrues nothing more, however long its leases wait to be closed.
- A top-up that lands **before** the settlement that would have frozen the stream keeps the leases alive. A top-up **after** the freeze does not: the stream stays frozen and its leases are closed. The new credit is not spent on the frozen stream.
- A lease acknowledged into a frozen stream is closed with the stream's other leases.
- A lease priced in several denoms is closed as soon as one of its streams is frozen. It is removed from its other streams too, which are settled up to the closing block first.
- Once the frozen stream has been drained, a new lease for the same `(provider, tenant, denom)` starts a fresh, healthy stream.

**Credit exhaustion is reported by these events:**

| Trigger | Event | Distinguishing attribute |
|---|---|---|
| A settlement freezes a stream (`MsgWithdraw`, `MsgAcknowledgeLease`, `MsgCloseLease`, or the EndBlocker settling a sibling stream) | `stream_exhausted` | `provider_uuid`, `tenant`, `denom` |
| The EndBlocker closes a lease of a frozen stream | `lease_auto_closed` (block event, not attached to a transaction) | `reason = credit_exhausted` |
| `MsgCloseLease` closes a lease whose stream is frozen | `lease_closed` | `closed_by = credit_exhaustion` on a genuine exhaustion (shortfall, accrual overflow, a zero balance with nothing accrued, or a stream already frozen before the message); a close whose credit exactly covered a positive charge keeps the caller's `reason`/role |

A consumer that subscribes only to `lease_auto_closed` will miss leases of a frozen stream that were closed through `MsgCloseLease` before the EndBlocker reached them.

**Design rationale:**
- **Bounded transactions**: a withdrawal that finds an exhausted tenant does one transfer and one stream update; it does not pay for closing that tenant's leases
- **Bounded blocks**: EndBlocker work is not gas-metered, so it is capped per block and resumes in the next one
- **Fair draining**: an equal share within a block, rotation across blocks. A frozen stream can keep receiving newly acknowledged leases, so neither one large stream nor the first streams of the queue may absorb the budget block after block. Sharing the budget protects the streams served in the same block; resuming the walk of the queue after the last stream served protects the streams further back when more than 100 are queued
- **Chain safety**: each closure in the EndBlocker runs in its own cached context; a lease that cannot be closed is logged, skipped and retried later, and never halts the chain or blocks the leases queued behind it
- **No scanning**: the EndBlocker reads only the queue of frozen streams, never the full set of leases

**Note**: Queries do NOT settle streams or close leases. They return the stored state. A stream whose tenant has run out of credit stays unfrozen, and its leases stay `ACTIVE`, until a write operation settles it.

**Note**: A lease's `closed_at` is the time of the block in which it was closed, not the instant its tenant's credit ran out.

## State

### Storage Key Prefixes

| Prefix | Key Type | Description |
|--------|----------|-------------|
| `0x00` | Params | Module parameters |
| `0x01` | Lease | Primary lease storage (UUID → Lease) |
| `0x02` | LeaseSequence | Sequence counter for UUIDv7 generation |
| `0x03` | LeaseByTenant | Index: tenant → lease UUIDs |
| `0x04` | LeaseByProvider | Index: provider UUID → lease UUIDs |
| `0x05` | CreditAccount | Credit accounts (tenant → CreditAccount) |
| `0x06` | CreditAddressIndex | Reverse lookup: credit address → tenant |
| `0x07` | LeaseByState | Index: state → lease UUIDs (for pending expiration) |
| `0x08` | LeaseByProviderState | Compound index: provider+state → lease UUIDs |
| `0x09` | LeaseByTenantState | Compound index: tenant+state → lease UUIDs |
| `0x0A` | LeaseBySKU | Many-to-many index: SKU UUID → lease UUIDs |
| `0x0B` | LeaseByStateCreatedAt | Compound index: state+created_at → lease UUIDs |
| `0x0C` | CustomDomainIndex | Reverse index: custom_domain → CustomDomainTarget (lease_uuid + service_name) |
| `0x0D` | Streams | Settlement streams: (provider UUID, (tenant, denom)) → StreamState. Provider-first ordering lets a withdrawal range over exactly one provider's streams. |
| `0x0E` | StreamsByTenant | Reverse lookup: (tenant, (provider UUID, denom)). Written only when a stream is created or deleted, never on settlement. |
| `0x0F` | StreamLeases | Stream membership: (stream key, lease UUID), one entry per ACTIVE lease and denom it is priced in. Lets the EndBlocker enumerate the leases of a frozen stream. |
| `0x10` | ExhaustedStreams | Queue of frozen streams whose leases the EndBlocker still has to close. The value is the drain cursor (UUID of the last lease that failed to close). |
| `0x11` | ExhaustedStreamsStart | Single item: the key of the last queued stream the EndBlocker drain served. The next block resumes after it, so the drain rotates through the queue. Only set while more streams are queued than one block can serve (100); cleared otherwise. Not exported in genesis. |

### Params

Module parameters stored at key `0x00`:

| Field | Type | Description |
|-------|------|-------------|
| max_leases_per_tenant | uint64 | Maximum active leases per tenant (must be > 0) |
| max_items_per_lease | uint64 | Maximum items per lease (default: 20, hard limit: 100) |
| min_lease_duration | uint64 | Minimum lease duration in seconds (default: 3600 = 1 hour) |
| max_pending_leases_per_tenant | uint64 | Maximum pending leases per tenant (default: 10) |
| pending_timeout | uint64 | Seconds before pending lease expires (default: 1800 = 30 minutes, min: 60, max: 86400) |
| allowed_list | []string | List of addresses allowed to create leases on behalf of tenants and to set lease custom_domains |
| reserved_domain_suffixes | []string | DNS suffixes (each beginning with `.`) that tenants are forbidden from claiming via `set-item-custom-domain`. Used to gate provider wildcard zones. Tunable via governance. |

**Validation Constraints:**
- `max_leases_per_tenant`: Must be > 0 and ≤ 10,000
- `max_items_per_lease`: Must be > 0 and ≤ 100 (hard limit)
- `min_lease_duration`: Must be > 0 and ≤ 2,592,000 (30 days)
- `max_pending_leases_per_tenant`: Must be > 0 and ≤ 1,000
- `pending_timeout`: Must be between 60 seconds (1 minute) and 86400 seconds (24 hours)
- `reserved_domain_suffixes`: Each entry must begin with `.`, the substring after the dot must be a valid FQDN, no duplicates

**Note:** There is no global `denom` parameter. Each SKU defines its own denomination in its `base_price`, enabling multi-denom billing.

### Hard Limits (Constants)

These values are compile-time constants and cannot be changed via governance:

| Constant | Value | Description |
|----------|-------|-------------|
| `MaxItemsPerLeaseHardLimit` | 100 | Absolute maximum items per lease |
| `MaxQuantityPerItem` | 1,000,000,000 | Maximum quantity per lease item (1 billion). Defined in `types/errors.go`. |
| `MaxPendingLeaseExpirationsPerBlock` | 100 | Maximum pending lease expirations processed per block (DoS protection) |
| `MaxAutoClosesPerBlock` | 100 | Maximum lease closures the EndBlocker attempts per block while draining frozen streams (DoS protection) |
| `DefaultProviderWithdrawLimit` | 50 | Default number of streams settled per withdraw call (can be increased to MaxBatchLeaseSize) |
| `MaxBatchLeaseSize` | 100 | Hard limit for any batch operation. For withdraw: maximum streams per call, configurable via `--limit` up to this value. For lease operations: maximum UUIDs per call. |
| `MaxRejectionReasonLength` | 256 | Maximum characters for lease rejection reason |
| `MaxClosureReasonLength` | 256 | Maximum characters for lease closure reason |
| `MaxCustomDomainLength` | 253 | Maximum bytes for `LeaseItem.custom_domain` (RFC 1035 max FQDN length) |
| `MaxWithdrawCursorLen` | 512 | Maximum bytes of the opaque `--key` cursor for withdraw. The cursor encodes a `(tenant, denom)` pair. Defined in `types/msgs.go`. |
| `MaxDurationSeconds` | 3,153,600,000 (100 years) | Maximum duration a stream may accrue in one settlement; a longer gap is treated as an accrual overflow. Defined in `keeper/accrual.go`. |
| `CreditAccountAddressPrefix` | `billing/credit/` | Prefix used for deterministic credit address derivation |
| `DefaultProviderWithdrawableQueryLimit` | 100 | Default page size (in streams) for the ProviderWithdrawable, StreamsByProvider and StreamsByTenant queries (`pagination.limit`). The ProviderWithdrawable request's old top-level `limit` field was removed; proto field 2 is reserved. |
| `MaxProviderWithdrawableQueryLimit` | 1000 | Maximum page size for the same three queries (`pagination.limit` is clamped to this) |

> **CreditEstimate / CreditAccount query iteration** is not a fixed constant — it tracks the lease params. The CreditAccount query iterates the tenant's active leases up to `max_leases_per_tenant + max_pending_leases_per_tenant`, a safe upper bound on the reachable active count: because `AcknowledgeLease` moves pending→active without re-gating the active limit, a tenant can reach `max_leases_per_tenant - 1 + max_pending_leases_per_tenant` active leases, and the cap adds a one-lease margin on top. Its pending-lease iteration is bounded by `max_pending_leases_per_tenant`. Each is clamped to the params' upper bounds (10,000 / 1,000) as a hard safety ceiling, so the caps never truncate a legal state yet stay bounded against DoS. The CreditEstimate query walks the tenant's streams instead of its leases, bounded by that same active-lease cap multiplied by `max_items_per_lease`.

### Batch Operations

Several messages support batch processing of multiple leases in a single transaction:

| Message | Max Leases | Behavior |
|---------|------------|----------|
| `MsgAcknowledgeLease` | 100 | All leases must be PENDING, same provider. Atomic. |
| `MsgRejectLease` | 100 | All leases must be PENDING, same provider. Atomic. |
| `MsgCancelLease` | 100 | All leases must be PENDING, same tenant. Atomic. |
| `MsgCloseLease` | 100 | All leases must be ACTIVE, authorized for sender. Atomic. |
| `MsgWithdraw` | 50 streams (default), 100 (max) | Paginated over the provider's streams; pass `next_key` back as `--key` until `has_more` is false. Not atomic: each stream settles on its own. |

**Atomic Batch Operations:** The lease-UUID batch messages above are atomic—all leases succeed or all fail. If any lease fails validation (wrong state, unauthorized, etc.), the entire transaction is rejected.

**Withdraw:** Unlike the lease-UUID operations, withdraw is paginated and works on streams. It settles up to `--limit` streams of the provider (default 50, max 100) and returns `has_more: true` plus an opaque `next_key` cursor if more remain. Pass `next_key` back as `--key` on the next call and repeat until `has_more: false`; calling again without `--key` restarts from the first stream. Streams are processed in key order (tenant address, then denom); `next_key` encodes the `(tenant, denom)` of the last stream of the page and the next call resumes strictly after it, even if that stream has been deleted in the meantime. The provider is always taken from the message, so a cursor cannot move the iteration into another provider's streams. Each stream settles in its own cached context: a stream that fails to settle is logged and skipped without affecting the others.

> **Deprecated:** `MsgWithdraw.lease_uuids` (withdrawing from specific leases) is no longer supported. A message with a non-empty `lease_uuids` is rejected with `ErrInvalidRequest`; use `provider_uuid` instead.

### Lease

Leases stored at key prefix `0x01`:

| Field | Type | Description |
|-------|------|-------------|
| uuid | string | UUIDv7 unique identifier |
| tenant | string | Tenant address |
| provider_uuid | string | Provider UUID (from SKU module) |
| items | []LeaseItem | List of SKU items |
| state | LeaseState | PENDING, ACTIVE, CLOSED, REJECTED, or EXPIRED |
| created_at | Timestamp | Creation time (credit locked) |
| acknowledged_at | Timestamp | Provider acknowledgement time (billing starts) |
| closed_at | Timestamp | Closure time |
| rejected_at | Timestamp | Rejection time |
| expired_at | Timestamp | Expiration time |
| last_settled_at | Timestamp | **Deprecated.** Never set by the node and rejected in genesis. Settlement is tracked per stream: see `StreamState.last_settled_at` ([Stream](#stream)). |
| rejection_reason | string | Provider's rejection reason (max 256 chars) |
| closure_reason | string | Closure reason (max 256 chars) |
| meta_hash | bytes | Hash/reference to off-chain deployment data (max 64 bytes, immutable) |
| min_lease_duration_at_creation | uint64 | Snapshot of `min_lease_duration` param at creation (for consistent reservation calculation) |

### LeaseItem

| Field | Type | Description |
|-------|------|-------------|
| sku_uuid | string | SKU UUID being leased |
| quantity | uint64 | Number of instances |
| locked_price | Coin | Price locked at creation (per second rate, includes denom) |
| service_name | string | Optional DNS-label for stack deployments (all-or-nothing per lease, unique within lease) |
| custom_domain | string | Optional FQDN (≤253 bytes) routed to this item by the provider. Mutable via `MsgSetItemCustomDomain`. Globally unique while the lease is PENDING/ACTIVE; the keeper enforces uniqueness through the `CustomDomainIndex` reverse-index. |

> **Note on `service_name` and `custom_domain`:** these two fields are compute-specific and live on `LeaseItem` today as a pragmatic shortcut. When a non-compute lease kind ships, both fields will be migrated to a future `x/deployment` module via a state migration (tracked: ENG-80).

### CreditAccount

Credit accounts stored at key prefix `0x05`:

| Field | Type | Description |
|-------|------|-------------|
| tenant | string | Tenant address |
| credit_address | string | Derived credit account address |
| active_lease_count | uint64 | Number of ACTIVE leases |
| pending_lease_count | uint64 | Number of PENDING leases |
| reserved_amounts | []Coin | Sum of all credit reservations for active and pending leases |

Note: The actual balance is tracked by the bank module at the `credit_address`. Query the bank module or use `QueryCreditAccount` which includes the balance. The `reserved_amounts` field tracks how much credit is reserved by existing leases (rate × min_lease_duration per denom), preventing overbooking.

### Stream

Settlement streams stored at key prefix `0x0D`, keyed by `(provider_uuid, tenant, denom)`. The stored value is a `StreamState`:

| Field | Type | Description |
|-------|------|-------------|
| rate | Int | Summed per-second rate of the stream's ACTIVE leases in the stream's denom |
| last_settled_at | Timestamp | Time up to which the stream's accrual has been settled. Every settlement sets it to the block time. |
| lease_count | uint64 | Number of ACTIVE leases contributing to `rate` |
| exhausted_at | Timestamp | Set when a settlement found the tenant's credit exhausted. A frozen stream stops accruing and its leases are closed by the EndBlocker. |
| remainder | Int | Accrual carried to the next settlement, in (denom units × nanoseconds): 1,000,000,000 is one whole unit of the denom. After a settlement it is what did not amount to a whole unit, so it is below 1,000,000,000. It holds a whole unit or more only when the stream carries a debt that could not be paid (the 2 → 3 store migration stores a failed payout here); the next settlement pays it along with what accrued since. Never negative. |

In genesis and in query responses a stream is a `Stream`: `provider_uuid`, `tenant`, `denom` and the `state` above.

**Tenant spelling:** a stream identifies its tenant by address, not by how the address string is spelled. Bech32 is also valid in upper case and a lease stores its tenant as it was sent, so `Lease.tenant` may be upper case while `Stream.tenant` is always the canonical lower-case spelling. Compare the two as addresses, not as strings.

**Invariant:** for every `(provider_uuid, tenant, denom)`, `rate` equals the sum of `locked_price × quantity` over the items in that denom of all ACTIVE leases of that tenant with that provider, `lease_count` equals the number of those leases, and a stream exists if and only if `lease_count > 0`. A frozen stream keeps its rate and lease count; it only stops accruing.

## State Transitions

### Fund Credit

Transfers tokens from sender to tenant's credit account.

```
sender → credit_address
```

### Create Lease (PENDING)

1. Verify item count ≤ `max_items_per_lease`
2. Verify tenant has a credit account
3. Verify tenant hasn't exceeded max active or pending leases
4. Verify all SKUs exist, are active, and belong to the same provider (locking per-second prices)
5. Verify the provider exists and is active
6. Verify `AvailableCredit >= rate × min_lease_duration` per denom and add the reservation
7. Create lease in PENDING state and increment pending_lease_count

### Acknowledge Lease (PENDING → ACTIVE)

1. Provider verifies they own the SKUs in the lease
2. Set lease state to ACTIVE
3. Set acknowledged_at to current block time (billing starts)
4. Add the lease to its streams, one per denom it is priced in: each stream is settled up to the block time, then its rate grows by the lease's rate and its lease_count by one. A stream that does not exist yet is created, settled at the block time.
5. Decrement pending_lease_count, increment active_lease_count

### Reject Lease (PENDING → REJECTED)

1. Provider verifies they own the SKUs in the lease
2. Set lease state to REJECTED
3. Set rejected_at and rejection_reason
4. Decrement pending_lease_count
5. Release credit reservation (rate × min_lease_duration)

### Cancel Lease (Tenant cancels PENDING)

1. Verify sender is the lease tenant
2. Verify lease is in PENDING state
3. Set lease state to REJECTED
4. Decrement pending_lease_count
5. Release credit reservation (rate × min_lease_duration)

### Expire Lease (EndBlocker)

The EndBlocker automatically expires pending leases that exceed the `pending_timeout`:

1. Query all leases in PENDING state using the state index
2. For each lease where `now > created_at + pending_timeout`:
   - Set lease state to EXPIRED
   - Set expired_at timestamp
   - Decrement pending_lease_count
   - Release credit reservation (rate × min_lease_duration)

**Rate Limiting:** To prevent DoS attacks, the EndBlocker processes a maximum of **100 lease expirations per block** (`MaxPendingLeaseExpirationsPerBlock`). If more than 100 leases need to expire, the remaining leases are processed in subsequent blocks. This uses a two-pass approach to avoid iterator invalidation during state modification.

### Auto-Close Leases of Frozen Streams (EndBlocker)

After expiring pending leases, the EndBlocker drains the queue of frozen streams (see [Overdraw and Auto-Close](#overdraw-and-auto-close)). For each lease of a frozen stream:

1. Remove the lease from every stream it belongs to. A sibling stream in another denom that is still healthy is settled up to the block time first.
2. Set lease state to CLOSED, closed_at to the block time and closure_reason to `credit exhausted`
3. Decrement active_lease_count
4. Release credit reservation (rate × min_lease_duration)
5. Delete any stream left without leases, together with its queue entry
6. Emit `lease_auto_closed`

**Rate Limiting:** The EndBlocker attempts a maximum of **100 closures per block** (`MaxAutoClosesPerBlock`) and resumes in the next block. The budget is shared: every stream served in the block first gets an equal share of it (at least one attempt, so at most 100 streams are served per block), then what is left is spent in queue order, so several frozen streams drain side by side rather than one after another. When more than 100 streams are queued, each block resumes the walk of the queue after the last stream the previous block served and wraps around to the beginning, so the drain rotates through the whole queue and no stream waits indefinitely behind others. Each closure is atomic and isolated in its own cached context: a lease that cannot be closed is logged and skipped, and a per-stream cursor makes the next block start with the leases behind it.

### Close Lease (ACTIVE → CLOSED)

1. Settle each stream the lease belongs to up to the block time: transfer what the **whole stream** accrued (capped at the tenant's credit) to the provider's payout address. A stream shared by several leases of the message is settled once.
2. Remove the lease from its streams (rate and lease_count drop; a stream left without leases is deleted)
3. Set lease state to CLOSED
4. Record closed_at = block time
5. Set closure_reason: the caller's reason, or `credit exhausted` when one of the lease's streams is frozen for a genuine exhaustion
6. Decrement active_lease_count
7. Release credit reservation (rate × min_lease_duration)

Because settlement is per stream, `total_settled_amounts` in the response is what the touched streams paid out: closing 1 of N leases of a stream pays the accrual of all N.

### Withdraw

For each stream of the provider in the page:

1. Calculate what the stream accrued: its carried `remainder` plus `rate ×` the time elapsed since its last_settled_at, to the nanosecond
2. Transfer the whole units of that amount, capped at the tenant's credit balance, to the provider's payout address
3. Store what is left over (less than one unit) as the new `remainder` and set the stream's last_settled_at to the block time
4. If the accrual consumed the whole balance, freeze the stream and queue it for the EndBlocker

No lease is read or written.

## Messages

The billing module supports the following transaction messages:

| Message | Description |
|---------|-------------|
| `MsgFundCredit` | Fund a tenant's credit account |
| `MsgCreateLease` | Create a new lease (starts in PENDING state) |
| `MsgCreateLeaseForTenant` | Create a lease on behalf of a tenant (authority/allowed only) |
| `MsgAcknowledgeLease` | Provider acknowledges a pending lease (→ ACTIVE) |
| `MsgRejectLease` | Provider rejects a pending lease |
| `MsgCancelLease` | Tenant cancels their own pending lease |
| `MsgCloseLease` | Close an active lease |
| `MsgWithdraw` | Withdraw what a provider's settlement streams have accrued (paginated over streams) |
| `MsgSetItemCustomDomain` | Set or clear `custom_domain` on a specific lease item (tenant / authority / `allowed_list`) |
| `MsgUpdateParams` | Update module parameters (authority only) |

For detailed message definitions, request/response formats, and CLI usage, see [API Reference](docs/API.md#cli-commands).

## Queries

| Query | Description |
|-------|-------------|
| Params | Get module parameters |
| Lease | Get a lease by ID |
| Leases | List all leases with pagination |
| LeasesByTenant | List leases for a tenant |
| LeasesByProvider | List leases for a provider (use `--state pending` filter for pending leases) |
| LeasesBySKU | List leases using a specific SKU |
| CreditAccount | Get a tenant's credit account |
| CreditAccounts | List all credit accounts |
| CreditEstimate | Estimate remaining credit duration. The burn rate comes from the tenant's non-frozen streams, and what they have accrued but not yet settled is deducted from the balance. `current_balance` covers the denoms of all the tenant's streams, frozen ones included. |
| CreditAddress | Derive credit address for a tenant |
| WithdrawableAmount | **Deprecated.** Returns gRPC `Unimplemented`: a lease has no withdrawable amount of its own. Use ProviderWithdrawable or StreamsByProvider. |
| ProviderWithdrawable | Withdrawable amount across the provider's streams, one page at a time — sum `amounts` across pages until `pagination.next_key` is empty. `lease_count` is the number of leases covered by the streams of the current page that have a non-zero withdrawable amount. |
| StreamsByProvider | List the settlement streams of a provider (rate, last_settled_at, lease_count, exhausted_at per tenant and denom) |
| StreamsByTenant | List the settlement streams paid by a tenant |
| LeaseByCustomDomain | Look up the active or pending lease that has claimed a given custom_domain (and the `service_name` of the matching item) |

**Events**: See [API Reference - Events](docs/API.md#events) for the complete list of events emitted by this module.

## Client

For complete CLI commands, gRPC endpoints, and REST API documentation, see [API Reference](docs/API.md).

**Quick examples:**
```bash
# Fund credit, create lease, query status
manifestd tx billing fund-credit [tenant] 1000000upwr --from [key]
manifestd tx billing create-lease [sku-uuid]:2 --from [key]
manifestd query billing leases-by-tenant [tenant] --state active
```

## Default Parameters

| Parameter | Default Value |
|-----------|---------------|
| max_leases_per_tenant | 100 |
| max_items_per_lease | 20 (hard limit: 100) |
| min_lease_duration | 3600 (1 hour) |
| max_pending_leases_per_tenant | 10 |
| pending_timeout | 1800 (30 minutes) |

## Authorization

For detailed authorization matrix, see [API Reference - Authorization](docs/API.md#authorization).

**Summary:**
- **Fund Credit**: Anyone can fund any tenant's credit account
- **Create Lease**: Tenants create their own leases; Authority/allow-list can create for others
- **Acknowledge/Reject Lease**: Provider or Authority
- **Cancel Lease**: Tenant (own pending leases only)
- **Close Lease**: Tenant, Provider, or Authority
- **Withdraw**: Provider or Authority
- **Set Item Custom Domain**: Tenant (own leases), Authority, or any address in `params.allowed_list`

## Integration with SKU Module

The billing module depends on the SKU module for:
- Validating SKU existence and active status
- Getting SKU prices for price locking (see [Price Locking](docs/DESIGN_DECISIONS.md#decision-4-price-locking-at-lease-creation))
- Getting provider information for authorization and payouts
- Per-second rate calculation (see [SKU Pricing](../sku/README.md#pricing-and-exact-divisibility))

The SKU module remains independent and does not know about the billing module.

**Key SKU Module Concepts for Billing:**
- [Provider and Payout Addresses](../sku/README.md#provider) - Where lease payments are sent
- [SKU Deactivation Impact](../sku/README.md#deactivation-impact-on-existing-leases) - How deactivated SKUs affect leases
- [Billing Units](../sku/README.md#billing-units) - Per-hour vs per-day pricing

## Known Limitations

### Credit Withdrawal Policy

There is no mechanism to withdraw unused credit from a credit account. Once tokens are funded, they can only be spent on leases. This mimics typical cloud providers (AWS credits, etc.) and prevents gaming of the system. Unused credit remains available for future leases.

### Provider/SKU Deactivation

When a provider or SKU is deactivated:
- Active leases continue with locked-in prices
- Providers can still withdraw accrued funds
- No new leases can be created with deactivated providers/SKUs

### Provider-Wide Withdraw Pagination

Withdraw (`--provider` flag) settles up to 50 streams per call by default (max 100). A stream is one `(tenant, denom)` pair of the provider, so a provider needs more than one call only when it serves more than `--limit` such pairs, however many leases they hold. Use `--limit` to increase, and pass the response's `next_key` back as `--key` to page through all streams until `has_more` is false:
```bash
manifestd tx billing withdraw --provider [provider-uuid] --limit 100 --from provider-key
# then, using next_key from the previous response:
manifestd tx billing withdraw --provider [provider-uuid] --limit 100 --key [next_key] --from provider-key
```

> `next_key` is a `bytes` value, so it appears base64-encoded in the JSON/CLI response. Pass that string verbatim to `--key` — it is an opaque encoding of a `(tenant, denom)` pair, not a UUID.

For detailed scalability analysis, time manipulation considerations, and future improvement plans, see [Architecture](docs/ARCHITECTURE.md#scalability-considerations).

## Genesis Validation

The billing module performs comprehensive validation during genesis initialization to ensure state consistency.

### Reservation Invariant Validation

Genesis validation enforces the credit reservation invariant:

```
CreditAccount.ReservedAmounts == SUM(GetLeaseReservationAmount(lease, params.MinLeaseDuration))
                                 for all PENDING and ACTIVE leases of the tenant
```

**Validation Steps:**
1. Compute expected reservations by iterating all leases and summing reservation amounts for PENDING/ACTIVE leases per tenant
2. Compare each credit account's `reserved_amounts` against the computed expected value
3. Verify that every tenant with active reservations has a corresponding credit account

**Error Examples:**
```
# Mismatch between stored and calculated reservations
invalid credit operation: credit account for manifest1abc... has reserved_amounts 500upwr but lease reservations sum to 600upwr

# Tenant with leases but missing credit account
invalid credit operation: tenant manifest1def... has lease reservations totaling 1000upwr but no credit account
```

### Other Genesis Validations

- **Lease UUIDs**: All leases must have valid UUIDv7 format, no duplicates
- **Tenant/Provider addresses**: All addresses must be valid bech32 format. The cross-checks between leases, credit accounts and streams compare tenants as canonical addresses, so the same tenant spelled in upper case in one record and lower case in another still matches (and two credit accounts for one address in different spellings are duplicates)
- **Credit address derivation**: Credit addresses must match deterministic derivation from tenant address
- **Lease state consistency**: CLOSED leases must have a non-nil, non-zero `closed_at`. At InitGenesis, a lease's `created_at` and `closed_at` must not be after the block time.
- **Deprecated `last_settled_at`**: A lease that carries `last_settled_at` is rejected. Such a genesis was exported before stream settlement and its leases have no streams.
- **Parameter validation**: All params must pass validation constraints

### Stream Validation

`GenesisState.streams` must be exactly the streams the ACTIVE leases imply:

- Every stream has a valid tenant address, a positive `rate`, a non-zero `last_settled_at`, a `remainder` that is not negative, and appears only once
- Every stream matches at least one ACTIVE lease, and its `rate` and `lease_count` equal what those leases sum to
- Every `(provider_uuid, tenant, denom)` with an ACTIVE lease has its stream
- At InitGenesis, a stream's `last_settled_at` and `exhausted_at` must not be after the block time

A genesis with ACTIVE leases therefore needs matching streams. `InitGenesis` runs these stream checks itself (genesis validation is not run on InitChain), restores the streams, rebuilds the state derived from them (tenant reverse lookup, lease membership, the queue of frozen streams from `exhausted_at`) and then re-checks the stream invariant against the stored leases, failing the import on any mismatch.

## Additional Documentation

### User Guides
- [Provider Setup Guide](../sku/docs/PROVIDER_GUIDE.md) - Creating and managing providers
- [SKU Setup Guide](../sku/docs/SKU_GUIDE.md) - Creating and managing SKUs (billable items)
- [Migration Guide](docs/MIGRATION.md) - Guide for authority members migrating off-chain leases
- [Integration Guide](docs/INTEGRATION.md) - Tenant authentication to provider APIs (ADR-036)
- [Troubleshooting](docs/TROUBLESHOOTING.md) - Common errors and solutions
- [API Reference](docs/API.md) - Complete CLI and gRPC/REST API reference
- [Frontend / Integrator Cookbook](../../docs/FRONTEND.md) - Client-side signing recipes and the type-URL / amino-name reference table

### Developer Documentation
- [Architecture](docs/ARCHITECTURE.md) - Internal architecture, data models, and flow diagrams
- [Design Decisions](docs/DESIGN_DECISIONS.md) - Key design decisions and rationale
- [Comparison](docs/COMPARISON.md) - Comparison with Akash and architectural trade-offs
- [Capabilities](docs/CAPABILITIES.md) - Feature overview and future roadmap

### Related Modules
- [SKU Module README](../sku/README.md) - Provider and SKU management (prerequisite for billing)
- [SKU API Reference](../sku/docs/API.md) - SKU module CLI and API documentation
- [SKU Design Decisions](../sku/docs/DESIGN_DECISIONS.md) - SKU architecture rationale
