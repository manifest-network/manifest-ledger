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

---

## Core Features

| Capability | Description |
|------------|-------------|
| **Credit Accounts** | Deterministic derived addresses hold tenant funds |
| **Lease Lifecycle** | PENDING → ACTIVE → CLOSED/REJECTED/EXPIRED states |
| **Two-Phase Commit** | Tenant creates, provider acknowledges (or rejects) |
| **Price Locking** | SKU prices locked at lease creation for duration |
| **Lazy Settlement** | Charges calculated on-touch (withdraw/close), not per-block |
| **Auto-Close** | Leases automatically close when credit exhausted |
| **Multi-Denom Support** | Credit accounts can hold multiple token types |
| **Batch Operations** | Provider-wide withdrawal, batch acknowledge/reject |
| **Pending Timeout** | Unacknowledged leases expire automatically (EndBlocker) |

---

## Lease Lifecycle

```
    ┌─────────────────────────────────────────────────────────┐
    │                                                         │
    ▼                                                         │
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
1. Transfer available balance to provider (partial payment)
2. Auto-close the lease
3. Emit `lease_auto_closed` event with reason `credit_exhausted`

### Overflow Protection

- Maximum duration capped at ~100 years (`MaxDurationSeconds`)
- Maximum quantity per item: 1 billion (`MaxQuantityPerItem`)
- Uses `math.Int` (big.Int) for all arithmetic

---

## Query Capabilities

| Query | Description |
|-------|-------------|
| `Lease` | Get a single lease by UUID |
| `Leases` | List all leases with pagination |
| `LeasesByTenant` | All leases for a tenant (with state filter) |
| `LeasesByProvider` | All leases for a provider (with state filter) |
| `LeasesBySKU` | All leases using a specific SKU |
| `CreditAccount` | Balance and lease counts for a tenant |
| `CreditAccounts` | List all credit accounts |
| `CreditEstimate` | Estimated remaining duration based on active leases |
| `CreditAddress` | Derive credit address for a tenant |
| `WithdrawableAmount` | Accrued amount for a specific lease |
| `ProviderWithdrawable` | Total withdrawable across all provider's leases |

---

## Authorization Model

| Action | Authority | Provider | Tenant | Allowed List |
|--------|-----------|----------|--------|--------------|
| Fund Credit | - | - | Anyone | - |
| Create Lease | - | - | Self | - |
| Create Lease for Tenant | ✓ | - | - | ✓ |
| Acknowledge Lease | ✓ | ✓ | - | - |
| Reject Lease | ✓ | ✓ | - | - |
| Cancel Pending Lease | - | - | Self | - |
| Close Lease | ✓ | ✓ | Self | - |
| Withdraw | ✓ | ✓ | - | - |
| Update Params | ✓ | - | - | - |

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
| **Extract Auto-Close Helper** | DRY up duplicated logic in withdraw functions | Maintainability |
| **Stateful Pagination for Provider Withdraw** | Cursor-based iteration | Handle 1000s of leases efficiently |
| **Event Indexing** | Structured event data for indexers | Better off-chain analytics |
| **Simulation Tests** | Fuzz testing for billing math | Confidence in edge cases |
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

    // 4. Settle up to now (before rate changes)
    _, err = ms.k.PerformSettlement(cacheCtx, &lease, blockTime)
    if err != nil {
        return nil, err
    }

    // 5. If scaling UP, validate credit covers new burn rate
    if newQuantity > oldQuantity {
        if err := ms.k.ValidateCreditForScaleUp(cacheCtx, &lease, itemIdx, newQuantity); err != nil {
            return nil, err
        }
    }

    // 6. Update quantity and timestamp
    lease.Items[itemIdx].Quantity = newQuantity
    lease.LastSettledAt = blockTime

    if err := ms.k.SetLease(cacheCtx, lease); err != nil {
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

### Validation for Scale Up

```go
func (k *Keeper) ValidateCreditForScaleUp(
    ctx context.Context,
    lease *types.Lease,
    itemIdx int,
    newQuantity uint64,
) error {
    // Calculate new total rate with updated quantity
    var newTotalRate sdkmath.Int
    for i, item := range lease.Items {
        qty := item.Quantity
        if i == itemIdx {
            qty = newQuantity
        }
        itemRate := item.LockedPrice.Amount.Mul(sdkmath.NewIntFromUint64(qty))
        newTotalRate = newTotalRate.Add(itemRate)
    }

    // Get available credit
    creditAddr, _ := types.DeriveCreditAddressFromBech32(lease.Tenant)
    balance := k.bankKeeper.GetBalance(ctx, creditAddr, lease.Items[0].LockedPrice.Denom)

    // Check if credit covers min_lease_duration at new rate
    params, _ := k.GetParams(ctx)
    minRequired := newTotalRate.Mul(sdkmath.NewIntFromUint64(params.MinLeaseDuration))

    if balance.Amount.LT(minRequired) {
        return types.ErrInsufficientCredit.Wrapf(
            "scaling up requires %s, but only %s available",
            minRequired.String(),
            balance.Amount.String(),
        )
    }

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

## Related Documentation

- [README](../README.md) - Module overview
- [API Reference](API.md) - Complete CLI and gRPC/REST API
- [Architecture](ARCHITECTURE.md) - Internal architecture details
- [Design Decisions](DESIGN_DECISIONS.md) - Key design rationale
- [Comparison](COMPARISON.md) - Comparison with Akash and architectural trade-offs
- [Integration Guide](INTEGRATION.md) - Tenant authentication (ADR-036)
- [SKU Module Capabilities](../../sku/docs/CAPABILITIES.md) - Provider and SKU management
