# Billing Module Capabilities & Future Improvements

This document provides a comprehensive overview of the Billing module's capabilities, architecture, and roadmap for future improvements.

> **See also:** [SKU Module Capabilities](../../sku/docs/CAPABILITIES.md) for provider and SKU management features.

## Table of Contents

- [Core Features](#core-features)
- [Lease Lifecycle](#lease-lifecycle)
- [Settlement Model](#settlement-model)
- [Query Capabilities](#query-capabilities)
- [Authorization Model](#authorization-model)
- [Architecture Overview](#architecture-overview)
- [Future Improvements](#future-improvements)
  - [Lease Scaling (Detailed Design)](#lease-scaling-detailed-design)
  - [Task-Based Billing (Detailed Design)](#task-based-billing-detailed-design)

---

## Core Features

| Capability | Description |
|------------|-------------|
| **Credit Accounts** | Deterministic derived addresses hold tenant funds |
| **Lease Lifecycle** | PENDING → ACTIVE → CLOSED/REJECTED/EXPIRED states |
| **Two-Phase Commit** | Tenant creates, provider acknowledges (or rejects) |
| **Price Locking** | SKU prices locked at lease creation for duration |
| **Consumable Reservations** | Per-lease remaining guarantees prevent one lease from spending another's reserved credit |
| **Lazy Settlement** | Charges calculated on-touch (withdraw/close), not per-block |
| **Auto-Close** | Leases automatically close when credit exhausted |
| **Multi-Denom Support** | Credit accounts can hold multiple token types |
| **Batch Operations** | Provider-wide withdrawal, batch acknowledge/reject |
| **Pending Timeout** | Unacknowledged leases expire automatically (EndBlocker) |
| **Per-Item Custom Domains** | Tenants attach FQDNs to lease items post-creation; provider routes traffic and provisions HTTP-01 TLS. Globally unique while the lease is PENDING/ACTIVE; reserved suffixes block claims on provider wildcard zones. (v2.1.0+) |

---

## Lease Lifecycle

```
┌─────────┐    acknowledge    ┌─────────┐    close/exhaust   ┌─────────┐
│ PENDING │ ───────────────► │ ACTIVE  │ ─────────────────► │ CLOSED  │
└─────────┘                   └─────────┘                    └─────────┘
    │
    │ reject/cancel/timeout
    ▼
┌──────────────┐
│ REJECTED /   │
│ EXPIRED      │
└──────────────┘
```

### State Descriptions

| State | Description |
|-------|-------------|
| **PENDING** | Tenant created lease, awaiting provider acknowledgement |
| **ACTIVE** | Provider acknowledged, billing in progress |
| **CLOSED** | Lease terminated (by tenant, provider, or credit exhaustion) |
| **REJECTED** | Provider rejected or tenant cancelled pending lease |
| **EXPIRED** | Pending lease timed out (exceeded `pending_timeout`) |

---

## Settlement Model

### Per-Second Billing

- SKU prices are defined per-hour or per-day
- Internally converted to per-second rate for precise billing
- Example: 3600 tokens/hour = 1 token/second

### Lazy Evaluation

Settlement is performed **on-touch**, not per-block:
- When provider withdraws from a lease
- When a lease is closed
- When checking for auto-close during withdrawal

This design enables:
- O(1) operations per lease
- No EndBlocker overhead for settlement
- Supports millions of leases without performance degradation

### Overdraw Handling

When credit is exhausted:
1. Transfer this lease's spendable credit `B - (R - A)` to the provider (partial payment), preserving every other reservation
2. Auto-close the lease
3. Emit an auto-close event whose type depends on the path: `lease_auto_closed` (reason=`credit_exhausted`) from provider-wide withdraw; `lease_closed` with `closed_by=credit_exhaustion` from CloseLease on a genuine exhaustion (shortfall, accrual overflow, or a zero-balance close) — a fully-paid CloseLease keeps the caller's reason/role; `provider_withdraw` with `auto_closed=true` from specific-lease withdraw

### Overflow Protection

- SDK `math.Int` values are fixed at 256 bits; price/quantity/duration
  multiplication and same-denom addition use checked operations
- Maximum quantity per item: 1 billion (`MaxQuantityPerItem`)
- Creation, CreditEstimate/non-spendable-capped queries, import, migration, and
  non-silent settlement return `ErrArithmeticOverflow` (code 20) instead of
  panicking; withdrawable queries cap affected denominations at this lease's
  spendable credit
- Long settlement intervals are charged from timestamp-derived whole seconds;
  only actual representational overflow triggers the conservative path
- Silent overflow settlement clamps only affected denominations to the lease's
  own tranche plus genuinely unreserved credit and preserves exact charges for
  unaffected denominations

### Reservation Accounting

For each tenant and denomination, consensus version 4 maintains:

```
R = SUM(A for each live modern lease) + U
```

`R` is the credit-account aggregate, `A` is a modern lease's consumable
remaining tranche, and `U` is the explicit shared allocation for live
historical leases. Settlement is own-tranche-first: spendable credit is
`B - (R - A)`, consumed reservation is removed from both `A` and `R`, and a
modern terminal transition releases exactly its remaining `A`. Historical and
terminal leases keep initialized empty lease-side reservation wrappers. The
account's `unattributed_lease_count` exactly tracks live historical cohort
membership even when `U` is empty; historical terminal transitions decrement
it in O(1) and release the exact remaining `U` when it reaches zero.

---

## Query Capabilities

| Query | Description |
|-------|-------------|
| `Lease` | Get a single lease by UUID |
| `Leases` | List all leases with pagination |
| `LeasesByTenant` | All leases for a tenant (with state filter) |
| `LeasesByProvider` | All leases for a provider (with state filter) |
| `LeasesBySKU` | All leases using a specific SKU |
| `CreditAccount` | Account state plus a cursor-paginated page of all bank balances and page-aligned available balances |
| `CreditAccounts` | List all credit accounts |
| `CreditEstimate` | Gross raw-bank-balance runway at the aggregate ACTIVE rate; not a reservation-aware auto-close forecast |
| `CreditAddress` | Derive credit address for a tenant |
| `WithdrawableAmount` | Current `min(accrued, B - (R - A))` for a specific lease |
| `ProviderWithdrawable` | Ordered best-effort execution estimate for the current ACTIVE-lease page. Failed lease simulations are discarded and reported in `failed_lease_uuids`; successful virtual effects feed later leases and no query state commits. Shared tenant credit is counted once within the page, and pages are not additive. Only forward `limit <= 100` is comparable to one provider withdrawal. After commit, advance the query with its prior first-unread cursor and the transaction with its prior last-processed cursor; never interchange them. |
| `LeaseByCustomDomain` | Look up the PENDING/ACTIVE lease + item that owns a given `custom_domain` (v2.1.0+) |

`CreditAccount` reads the SDK bank balance store through bounded cursor pages
(default 100, maximum 1000), so unrelated funded denoms remain visible without
an unbounded scan. Offset and `count_total` are rejected.

`CreditEstimate` uses the account's stored ACTIVE count so parameter reductions
cannot hide existing state. It enforces the conservative 11,000-lease bound
(exact v2 reachable maximum 10,999), a 100,000-item work bound, and exact
count/index agreement. Violations fail with `ErrLeaseQueryLimitExceeded` /
gRPC `ResourceExhausted` or `ErrReservationInvariant` / gRPC `Internal`; the
query never returns a partial estimate.

---

## Authorization Model

For the complete authorization matrix, see [API Reference - Authorization](API.md#authorization).

---

## Architecture Overview

### Module Dependency

```
┌─────────────┐     references      ┌─────────────┐
│  Billing    │ ─────────────────► │    SKU      │
│   Module    │                     │   Module    │
└─────────────┘                     └─────────────┘
      │                                   │
      │ • Validates SKU exists            │ • Provider/SKU CRUD
      │ • Gets SKU prices                 │ • Price definitions
      │ • Gets provider payout address    │ • Activation status
      │ • Checks SKU/provider active      │
      ▼                                   │
┌─────────────┐                           │
│    Bank     │ ◄─────────────────────────┘
│   Module    │   (payouts go to provider's payout_address)
└─────────────┘
```

### Key Design Principles

1. **Lazy Evaluation** - O(1) per-lease operations, no EndBlocker bottleneck for settlement
2. **Atomic Operations** - CacheContext ensures state consistency on partial failures
3. **Soft Deletes** - Audit trail preserved, graceful deprecation of providers/SKUs
4. **Multi-Denom Native** - No assumptions about token type
5. **Clear Separation** - SKU module is independent; billing depends on SKU

### Architecture Considerations

| Consideration | Current Behavior | Rationale |
|---------------|------------------|-----------|
| **No Credit Withdrawal** | Funds locked until spent | Prevents gaming, mimics cloud provider credits |
| **Provider Trust** | Providers can reject leases | No on-chain SLA enforcement |
| **Off-Chain Provisioning** | Resource allocation is off-chain | Blockchain handles billing, not compute |
| **Time-Based Only** | No usage metering | Would require oracles or trusted reporters |

---

## Future Improvements

### High Value

| Improvement | Description | Benefit |
|-------------|-------------|---------|
| **Lease Scaling** | Modify quantities mid-lease without closing | Flexibility for dynamic workloads |
| **Scheduled Lease Closure** | Tenant sets end time upfront | Predictable billing, automatic cleanup |
| **Credit Withdrawal** | Allow unused credit withdrawal (with rules) | Better UX, less lock-in |
| **Webhooks/IBC Callbacks** | Notify providers of lease events | Real-time provisioning automation |

### Medium Value

| Improvement | Description | Benefit |
|-------------|-------------|---------|
| **Usage-Based Billing** | Metered usage reported on-chain | Pay-per-use instead of time-based |
| **Lease Templates** | Pre-approved configurations | Faster lease creation |
| **Auto-Renewal** | Leases renew if credit available | Uninterrupted service |
| **Referral/Affiliate System** | Commission sharing | Growth incentives |

### Technical Improvements

| Improvement | Description | Benefit |
|-------------|-------------|---------|
| **Event Indexing** | Structured event data for indexers | Better off-chain analytics |
| **Gas Optimization** | Batch state writes | Lower transaction costs |

### Integration Improvements

| Improvement | Description | Benefit |
|-------------|-------------|---------|
| **IBC Billing** | Cross-chain lease creation/payment | Multi-chain provider ecosystem |
| **CosmWasm Hooks** | Smart contract callbacks on lease events | Programmable provisioning |
| **Oracle Price Feeds** | Dynamic pricing based on external data | Market-responsive pricing |
| **Governance Proposals** | Lease disputes, provider slashing | Decentralized conflict resolution |

---

## Lease Scaling (Detailed Design)

### Current Limitation

Today, to change capacity you must:
1. Close the existing lease (settle final charges)
2. Create a new lease (lose locked price, provider must re-acknowledge)
3. Risk service interruption during the transition

### Use Cases

- **Scale Up:** Deploy more compute instances during peak demand
- **Scale Down:** Reduce capacity during off-peak hours to save costs
- **Gradual Ramp:** Slowly increase capacity for a new deployment
- **Emergency Scale:** Quickly add resources during unexpected load

### Design Considerations

#### Scale Up (Add Capacity)

| Aspect | Options | Trade-offs |
|--------|---------|------------|
| **Price for new capacity** | A) Lock at current SKU price | Fair to provider (market rates) |
| | B) Use original locked price | Fair to tenant (contract honored) |
| | C) Weighted average | Complex but balanced |
| **Provider approval** | A) Required (like new lease) | Provider controls capacity |
| | B) Auto-approved | Faster, but provider may not have capacity |
| **Credit check** | Must validate increased burn rate | Prevents immediate exhaustion |

#### Scale Down (Reduce Capacity)

| Aspect | Options | Trade-offs |
|--------|---------|------------|
| **Minimum quantity** | Allow zero? Or minimum 1? | Zero = effectively paused lease |
| **Provider approval** | Usually not needed | Tenant reducing spend is their right |
| **Settlement** | Settle before applying reduction | Clean accounting |

### Proposed Data Model

```protobuf
// New message for scaling
message MsgScaleLease {
  string sender = 1;           // Tenant
  string lease_uuid = 2;
  string sku_uuid = 3;         // Which item to scale
  uint64 new_quantity = 4;     // New quantity (can be higher or lower)
}

message MsgScaleLeaseResponse {
  uint64 old_quantity = 1;
  uint64 new_quantity = 2;
  google.protobuf.Timestamp effective_at = 3;
}

// Extended LeaseItem (optional, for tracking)
message LeaseItem {
  string sku_uuid = 1;
  uint64 quantity = 2;
  cosmos.base.v1beta1.Coin locked_price = 3;
  google.protobuf.Timestamp added_at = 4;      // When item was added/modified
  repeated QuantityChange history = 5;          // Optional: audit trail
}

message QuantityChange {
  uint64 old_quantity = 1;
  uint64 new_quantity = 2;
  google.protobuf.Timestamp changed_at = 3;
}
```

### Recommended Implementation (Simple Scaling)

**Start simple** - scale existing items only, same locked price:

> **Reservation requirement:** The following is design pseudocode, not current
> API. A real implementation must load and persist the tenant credit account in
> the same `CacheContext`, settle through the existing own-tranche-first path,
> and atomically resize both `Lease.reservation.remaining_amounts` and
> `CreditAccount.reserved_amounts`. A raw bank-balance check is not sufficient
> because it can allocate credit already guaranteed to another lease.

```go
func (ms msgServer) ScaleLease(ctx context.Context, msg *types.MsgScaleLease) (*types.MsgScaleLeaseResponse, error) {
    sdkCtx := sdk.UnwrapSDKContext(ctx)
    blockTime := sdkCtx.BlockTime()

    // 1. Get and validate lease
    lease, err := ms.k.GetLease(ctx, msg.LeaseUuid)
    if err != nil {
        return nil, err
    }
    if lease.State != types.LEASE_STATE_ACTIVE {
        return nil, types.ErrLeaseNotActive
    }
    if lease.Tenant != msg.Sender {
        return nil, types.ErrUnauthorized
    }

    // 2. Find the item to scale
    itemIdx := -1
    for i, item := range lease.Items {
        if item.SkuUuid == msg.SkuUuid {
            itemIdx = i
            break
        }
    }
    if itemIdx == -1 {
        return nil, types.ErrSKUNotInLease
    }

    oldQuantity := lease.Items[itemIdx].Quantity
    newQuantity := msg.NewQuantity

    if newQuantity == oldQuantity {
        return nil, types.ErrNoChange
    }

    // 3. Use CacheContext for atomicity
    cacheCtx, write := sdkCtx.CacheContext()
    account, err := ms.k.GetCreditAccount(cacheCtx, lease.Tenant)
    if err != nil {
        return nil, err
    }

    // 4. Settle up to now (before rate changes)
    _, err = ms.k.PerformSettlement(cacheCtx, &lease, &account, blockTime)
    if err != nil {
        return nil, err
    }

    // 5. Resize A and R together. Scale-up reserves the incremental nominal
    //    guarantee from B - R; scale-down releases at most the remaining A.
    if err := ms.k.ResizeLeaseReservation(
        cacheCtx, &lease, &account, itemIdx, oldQuantity, newQuantity,
    ); err != nil {
        return nil, err
    }

    // 6. Update quantity and timestamp
    lease.Items[itemIdx].Quantity = newQuantity
    lease.LastSettledAt = blockTime

    if err := ms.k.SetLease(cacheCtx, lease); err != nil {
        return nil, err
    }
    if err := ms.k.SetCreditAccount(cacheCtx, account); err != nil {
        return nil, err
    }

    // 7. Commit atomically
    write()

    // 8. Emit event
    sdkCtx.EventManager().EmitEvent(
        sdk.NewEvent(
            types.EventTypeLeaseScaled,
            sdk.NewAttribute(types.AttributeKeyLeaseUUID, lease.Uuid),
            sdk.NewAttribute(types.AttributeKeySkuUUID, msg.SkuUuid),
            sdk.NewAttribute("old_quantity", strconv.FormatUint(oldQuantity, 10)),
            sdk.NewAttribute("new_quantity", strconv.FormatUint(newQuantity, 10)),
        ),
    )

    return &types.MsgScaleLeaseResponse{
        OldQuantity: oldQuantity,
        NewQuantity: newQuantity,
        EffectiveAt: &blockTime,
    }, nil
}
```

### Reservation Planning for Scale Changes

The reservation policy must be explicit. A simple policy that does not
replenish already consumed credit is:

- **Scale up:** compute the incremental nominal reservation for only the added
  capacity, require `B - R` to cover it per denomination, then add that delta
  to both lease `A` and account `R`.
- **Scale down:** compute the nominal reduction, release
  `min(A, nominal_reduction)` from both `A` and `R`, and never make either value
  negative.

The planner must use checked, canonical `sdk.Coins` operations across all lease
denominations. It must not assume `lease.Items[0]` represents the entire lease,
and must not replenish the consumed part of existing capacity unless a future
protocol decision explicitly chooses that economic policy.

```go
func (k *Keeper) ResizeLeaseReservation(
    ctx context.Context,
    lease *types.Lease,
    account *types.CreditAccount,
    itemIdx int,
    oldQuantity uint64,
    newQuantity uint64,
) error {
    params, _ := k.GetParams(ctx)
    deltaNominal, direction, err := CalculateCheckedReservationDelta(
        lease.Items[itemIdx], oldQuantity, newQuantity, params.MinLeaseDuration,
    )
    if err != nil { return err }

    if direction == ScaleUp {
        balances, err := k.getCreditBalancesForDenoms(
            ctx, lease.Tenant, coinDenoms(deltaNominal),
        )
        if err != nil { return err }
        available := types.GetAvailableCredit(balances, account.ReservedAmounts)
        if !available.IsAllGTE(deltaNominal) {
            return types.ErrInsufficientCredit
        }
        leaseAfter, err := types.SafeAddCoins(
            lease.Reservation.RemainingAmounts, deltaNominal,
        )
        if err != nil { return err }
        accountAfter, err := types.SafeAddCoins(
            account.ReservedAmounts, deltaNominal,
        )
        if err != nil { return err }
        lease.Reservation.RemainingAmounts = leaseAfter
        account.ReservedAmounts = accountAfter
        return nil
    }

    release := lease.Reservation.RemainingAmounts.Min(deltaNominal)
    leaseAfter, err := types.SafeSubtractCoins(
        lease.Reservation.RemainingAmounts, release,
    )
    if err != nil { return err }
    accountAfter, err := types.SafeSubtractCoins(
        account.ReservedAmounts, release,
    )
    if err != nil { return err }
    lease.Reservation.RemainingAmounts = leaseAfter
    account.ReservedAmounts = accountAfter
    return nil
}
```

### Extension Roadmap

| Version | Feature | Complexity |
|---------|---------|------------|
| **v1** | Simple quantity scaling (same SKU, same locked price) | Low |
| **v2** | Provider approval flag for scale-up (parameter-controlled) | Low |
| **v3** | Add new SKU items to existing lease (new price lock for new items) | Medium |
| **v4** | Full amendment model with audit trail | High |
| **v5** | Scheduled scaling (scale at future time) | Medium |

### CLI Commands

```bash
# Scale up
manifestd tx billing scale-lease [lease-uuid] [sku-uuid] 5 --from tenant

# Scale down
manifestd tx billing scale-lease [lease-uuid] [sku-uuid] 2 --from tenant

# Query (existing, shows current quantities)
manifestd query billing lease [lease-uuid]
```

### Benefits Summary

| Benefit | Description |
|---------|-------------|
| **No Service Interruption** | Scale without closing/reopening lease |
| **Price Lock Preserved** | Original locked price applies to all quantities |
| **Clean Settlement** | Automatic settlement before rate change |
| **Audit Trail** | Events track all scaling operations |
| **Simple UX** | Single transaction to scale |

---

## Task-Based Billing (Detailed Design)

### Current Limitation

The billing module only supports **time-based billing** - charges accrue per-second regardless of actual work performed. This doesn't fit use cases like:

- **Batch inference**: Run 1000 predictions, pay per prediction
- **Rendering jobs**: Render 50 frames, pay per frame
- **One-off tasks**: Process a file, pay for the task

### Use Cases

| Use Case | Current (Time-Based) | Desired (Task-Based) |
|----------|----------------------|----------------------|
| AI inference | Reserve GPU for 1 hour, pay $10/hour | Run 100 inferences, pay $0.10 each |
| Rendering | Reserve node for 2 hours | Render 20 frames, pay $0.50/frame |
| Data processing | Reserve compute for 30 min | Process 5GB, pay $0.02/GB |

### Design Overview

Extend leases to support a `billing_mode` that determines how charges accrue:

- **TIME**: Current behavior - charges accrue per-second
- **TASK**: Charges accrue per-task completion submitted by provider

### Proposed Data Model

```protobuf
// New enum for billing mode
enum BillingMode {
  BILLING_MODE_UNSPECIFIED = 0;
  BILLING_MODE_TIME = 1;   // Current behavior - per-second accrual
  BILLING_MODE_TASK = 2;   // Per-task completion
}

// Extended LeaseItem for task-based pricing
message LeaseItem {
  string sku_uuid = 1;
  uint64 quantity = 2;
  cosmos.base.v1beta1.Coin locked_price = 3;  // Per-second (TIME) or per-task (TASK)
}

// Extended Lease
message Lease {
  // ... existing fields ...

  // Task-based billing fields
  BillingMode billing_mode = 15;
  uint64 tasks_completed = 16;     // Counter for completed tasks
  uint64 max_tasks = 17;           // Optional: cap on tasks (0 = unlimited)
  cosmos.base.v1beta1.Coin max_cost = 18;  // Optional: spending limit
}

// New message for task completion
message MsgCompleteTask {
  string provider = 1;           // Must be the lease's provider
  string lease_uuid = 2;
  uint64 task_count = 3;         // How many tasks completed (usually 1)
  bytes task_proof = 4;          // Optional: attestation or proof data
  string task_id = 5;            // Optional: external reference ID
  string task_meta = 6;          // Optional: task metadata (e.g., input hash)
}

message MsgCompleteTaskResponse {
  cosmos.base.v1beta1.Coins charged = 1;    // Amount charged for this completion
  uint64 total_tasks_completed = 2;         // Running total
  cosmos.base.v1beta1.Coins total_charged = 3;  // Total charged so far
}
```

### SKU Extension

SKUs need to indicate whether they support task-based billing:

```protobuf
message SKU {
  // ... existing fields ...

  BillingMode billing_mode = 10;  // What billing mode this SKU uses
  // For TASK mode: base_price is per-task, not per-time-unit
}
```

### Settlement Logic

Task settlement must use the same reservation planner as time-based
settlement. Directly checking or sending the raw credit-account balance would
let task charges consume guarantees owned by other leases. The pseudocode below
therefore updates the lease tranche and account aggregate in the same cached
transaction as the bank send.

```go
func (ms msgServer) CompleteTask(ctx context.Context, msg *types.MsgCompleteTask) (*types.MsgCompleteTaskResponse, error) {
    sdkCtx := sdk.UnwrapSDKContext(ctx)
    blockTime := sdkCtx.BlockTime()

    // 1. Get and validate lease
    lease, err := ms.k.GetLease(ctx, msg.LeaseUuid)
    if err != nil {
        return nil, err
    }

    if lease.State != types.LEASE_STATE_ACTIVE {
        return nil, types.ErrLeaseNotActive
    }

    if lease.BillingMode != types.BILLING_MODE_TASK {
        return nil, types.ErrInvalidBillingMode.Wrap("lease is not task-based")
    }

    // 2. Verify sender is the provider
    provider, err := ms.k.skuKeeper.GetProvider(ctx, lease.ProviderUuid)
    if err != nil {
        return nil, err
    }
    if msg.Provider != provider.Address {
        return nil, types.ErrUnauthorized.Wrap("only provider can complete tasks")
    }

    // 3. Check max_tasks limit
    if lease.MaxTasks > 0 && lease.TasksCompleted+msg.TaskCount > lease.MaxTasks {
        return nil, types.ErrMaxTasksExceeded.Wrapf(
            "completing %d tasks would exceed max_tasks (%d), current: %d",
            msg.TaskCount, lease.MaxTasks, lease.TasksCompleted,
        )
    }

    // 4. Calculate cost: locked_price × quantity × task_count
    cost := sdk.NewCoins()
    for _, item := range lease.Items {
        itemCost := item.LockedPrice.Amount.
            MulRaw(int64(item.Quantity)).
            MulRaw(int64(msg.TaskCount))
        cost = cost.Add(sdk.NewCoin(item.LockedPrice.Denom, itemCost))
    }

    // 5. Check max_cost limit (if set)
    if !lease.MaxCost.IsZero() {
        totalAfter := ms.k.CalculateTotalCharged(ctx, lease).Add(cost...)
        if totalAfter.IsAnyGT(sdk.NewCoins(lease.MaxCost)) {
            return nil, types.ErrMaxCostExceeded.Wrapf(
                "completing tasks would exceed max_cost (%s)",
                lease.MaxCost.String(),
            )
        }
    }

    // 6. Use CacheContext for atomicity
    cacheCtx, write := sdkCtx.CacheContext()

    // 7. Plan an own-tranche-first transfer.
    account, err := ms.k.GetCreditAccount(cacheCtx, lease.Tenant)
    if err != nil {
        return nil, err
    }
    balances, reserved, allocation, err := ms.k.reservationSpendInputs(cacheCtx, &lease, &account)
    if err != nil {
        return nil, err
    }
    plan, err := types.PlanReservationSpend(balances, reserved, allocation, cost)
    if err != nil {
        return nil, err
    }
    if !plan.Transfer.Equal(cost) {
        return nil, types.ErrInsufficientCredit.Wrapf(
            "task completion costs %s, only %s is lease-spendable",
            cost.String(), plan.Spendable.String(),
        )
    }
    reservedAfter, err := types.SafeSubtractCoins(
        account.ReservedAmounts, plan.Consumed,
    )
    if err != nil { return err }

    creditAddr, _ := types.DeriveCreditAddressFromBech32(lease.Tenant)
    payoutAddr, _ := sdk.AccAddressFromBech32(provider.PayoutAddress)
    if payoutAddr.Equals(creditAddr) {
        return nil, types.ErrInvalidCreditOperation.Wrap("provider payout address must not equal tenant credit address")
    }

    if err := ms.k.bankKeeper.SendCoins(cacheCtx, creditAddr, payoutAddr, plan.Transfer); err != nil {
        return nil, err
    }
    account.ReservedAmounts = reservedAfter
    lease.Reservation.RemainingAmounts = plan.AllocationAfter

    // 8. Update lease
    lease.TasksCompleted += msg.TaskCount

    if err := ms.k.SetLease(cacheCtx, lease); err != nil {
        return nil, err
    }
    if err := ms.k.SetCreditAccount(cacheCtx, account); err != nil {
        return nil, err
    }

    // 9. Commit
    write()

    // 10. Emit event
    sdkCtx.EventManager().EmitEvent(
        sdk.NewEvent(
            types.EventTypeTaskCompleted,
            sdk.NewAttribute(types.AttributeKeyLeaseUUID, lease.Uuid),
            sdk.NewAttribute(types.AttributeKeyProviderUUID, lease.ProviderUuid),
            sdk.NewAttribute(types.AttributeKeyTenant, lease.Tenant),
            sdk.NewAttribute("task_count", strconv.FormatUint(msg.TaskCount, 10)),
            sdk.NewAttribute("task_id", msg.TaskId),
            sdk.NewAttribute(types.AttributeKeyAmount, cost.String()),
            sdk.NewAttribute("total_completed", strconv.FormatUint(lease.TasksCompleted, 10)),
        ),
    )

    return &types.MsgCompleteTaskResponse{
        Charged:             cost,
        TotalTasksCompleted: lease.TasksCompleted,
        TotalCharged:        ms.k.CalculateTotalCharged(ctx, lease),
    }, nil
}
```

### Credit Reservation for Task-Based Leases

For time-based leases, we reserve `rate × min_lease_duration`. For task-based leases:

```go
func CalculateLeaseReservation(lease *Lease, params Params) (sdk.Coins, error) {
    switch lease.BillingMode {
    case BILLING_MODE_TIME:
        // Existing logic: rate × min_lease_duration
        return CalculateTimeBasedReservation(lease.Items, params.MinLeaseDuration)

    case BILLING_MODE_TASK:
        // Reserve for max_tasks if set, otherwise min_tasks parameter
        taskCount := lease.MaxTasks
        if taskCount == 0 {
            taskCount = params.MinTaskReservation  // e.g., 10 tasks
        }
        return CalculateTaskBasedReservation(lease.Items, taskCount)
    }
}
```

Whichever nominal policy is chosen, lease creation must initialize
`Lease.reservation.remaining_amounts`, add the same coins to
`CreditAccount.reserved_amounts`, and thereafter consume/release the exact
remaining tranche. Task reservations are not a separate aggregate.

### Hybrid Billing Mode

A lease could support both time-based and task-based charges:

```
Example: AI API Service
- Base rate: $5/hour for API access (TIME)
- Per-inference: $0.01 per inference (TASK)

Tenant pays:
- Continuous $5/hour while lease is active
- Additional $0.01 for each inference completed
```

This would require:
- Separate `time_items` and `task_items` in the lease
- Both accrual mechanisms active
- More complex settlement

**Recommendation**: Start with pure TASK mode, add hybrid later if needed.

### Query Extensions

```protobuf
// Extended credit estimate for task-based leases
message QueryCreditEstimateResponse {
  // ... existing fields for TIME mode ...

  // Task-based estimates
  uint64 estimated_remaining_tasks = 5;  // How many tasks can be completed
  cosmos.base.v1beta1.Coins cost_per_task = 6;
}
```

### CLI Commands

```bash
# Create task-based lease
manifestd tx billing create-lease [provider-uuid] \
  --item [sku-uuid]:quantity \
  --billing-mode task \
  --max-tasks 1000 \
  --max-cost 100000umfx \
  --from tenant

# Provider completes task
manifestd tx billing complete-task [lease-uuid] \
  --task-count 1 \
  --task-id "inference-12345" \
  --from provider

# Query task-based lease
manifestd query billing lease [lease-uuid]
# Returns: tasks_completed, max_tasks, cost_per_task, etc.
```

### Implementation Phases

| Phase | Feature | Complexity | Dependencies |
|-------|---------|------------|--------------|
| **1** | `BillingMode` enum and field | Low | Proto regeneration |
| **2** | `MsgCompleteTask` message | Medium | None |
| **3** | Task settlement logic | Medium | Phase 2 |
| **4** | Task-based credit reservation | Medium | Credit reservation system |
| **5** | SKU billing mode support | Low | Phase 1 |
| **6** | `max_tasks` and `max_cost` limits | Low | Phase 2 |
| **7** | Task completion events and indexing | Low | Phase 2 |
| **8** | Hybrid billing mode | High | All above |

### Migration Considerations

- Existing leases default to `BILLING_MODE_TIME`
- New field `billing_mode` with default value
- SKUs need migration to specify their billing mode
- No breaking changes to existing functionality

### Security Considerations

| Concern | Mitigation |
|---------|------------|
| Provider spam (fake completions) | Off-chain verification, reputation |
| Task count manipulation | `max_tasks` limit, credit exhaustion check |
| Credit exhaustion mid-task | Check before allowing completion |
| Replay attacks | `task_id` uniqueness (optional enforcement) |

### Comparison with Other Systems

| System | Task Billing Approach |
|--------|----------------------|
| **Golem** | Task market - providers bid on tasks, payment on completion |
| **Render** | Job-based - submit render job, pay when frames complete |
| **AWS Lambda** | Invocation-based - pay per function call + duration |
| **Manifest (proposed)** | Provider-attested completion - trust provider reports |

Our approach is simpler (no bidding, no proofs) but requires trusted providers. This fits V1 where we are the provider.

### Benefits Summary

| Benefit | Description |
|---------|-------------|
| **Pay-Per-Use** | Only pay for actual work done |
| **No Idle Charges** | No cost when provider isn't processing |
| **Predictable Costs** | Know exact cost per task upfront |
| **Flexible Limits** | Set task count or cost caps |
| **Simple Provider Integration** | Just call `CompleteTask` when done |
| **Audit Trail** | Every task completion is an on-chain event |

---

## Related Documentation

- [README](../README.md) - Module overview
- [API Reference](API.md) - Complete CLI and gRPC/REST API
- [Architecture](ARCHITECTURE.md) - Internal architecture details
- [Design Decisions](DESIGN_DECISIONS.md) - Key design rationale
- [Comparison](COMPARISON.md) - Comparison with Akash and architectural trade-offs
- [Integration Guide](INTEGRATION.md) - Tenant authentication (ADR-036)
- [SKU Module Capabilities](../../sku/docs/CAPABILITIES.md) - Provider and SKU management
