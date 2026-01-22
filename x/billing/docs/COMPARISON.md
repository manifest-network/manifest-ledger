# Billing Architecture Comparison

This document compares the Manifest billing and SKU modules with similar systems (notably Akash) and documents key architectural trade-offs and considerations.

## Comparison with Akash

Akash is a decentralized cloud compute marketplace with on-chain order matching and escrow-based settlement. The Manifest billing module takes a deliberately simpler approach.

### Feature Comparison

| Aspect | Manifest Billing/SKU | Akash |
|--------|---------------------|-------|
| **Settlement Model** | Lazy (on-touch) - O(1) per operation | Per-block escrow - O(n) active leases |
| **Price Discovery** | Off-chain (fixed SKU prices) | On-chain order book + bidding |
| **Lease Creation** | 1-2 tx (create + acknowledge) | 4+ tx (order → bid → accept → lease) |
| **State Complexity** | Simple (Provider, SKU, Lease, Credit) | Complex (Deployment, Group, Order, Bid, Lease) |
| **EndBlocker Load** | Light (pending expiration only, capped at 100/block) | Heavy (escrow settlement for all active leases) |
| **Provider Matching** | Manual (tenant selects SKU) | Automatic (providers bid on orders) |
| **Price Model** | Fixed per-SKU pricing | Dynamic auction-based |

### On-Chain Weight

**Manifest is significantly lighter on-chain.** The key architectural decisions that reduce chain load:

1. **Lazy Settlement**: Charges are calculated on-demand during `Withdraw` or `CloseLease` operations, not every block. This eliminates O(n) EndBlocker overhead.

2. **Off-Chain Discovery**: Price discovery and provider selection happen off-chain. The chain only records the final lease agreement.

3. **Simple State Machine**: PENDING → ACTIVE → CLOSED is simpler than Akash's multi-phase deployment lifecycle.

4. **Batched Operations**: Provider-wide withdrawals process multiple leases in one transaction.

## What We Gained

| Benefit | Description |
|---------|-------------|
| **Scalability** | Supports millions of leases without EndBlocker performance degradation |
| **Simpler UX** | No bidding complexity - tenants select SKUs directly |
| **Predictable Pricing** | Prices locked at lease creation, no auction dynamics |
| **Lower Gas Costs** | Fewer transactions per lease lifecycle |
| **Faster Finality** | Lease active in 2 transactions vs 4+ |

## What We Traded Away

| Trade-off | Impact | Mitigation Strategy |
|-----------|--------|---------------------|
| **No on-chain price discovery** | Tenants can't compare providers on-chain | Off-chain marketplace UI, provider directories |
| **No competitive bidding** | May not achieve lowest market price | Off-chain competition, provider reputation |
| **Trust-based SLA** | No on-chain service level enforcement | Off-chain contracts, reputation systems |
| **No dispute resolution** | No on-chain mechanism if provider doesn't deliver | Pending timeout, manual resolution, future governance |
| **Centralized provider approval** | Authority controls who can be a provider | Allowed list delegation, future self-registration |

## Architectural Considerations

### Economic Security

| Consideration | Current State | Notes |
|---------------|---------------|-------|
| **Lease spam prevention** | `min_lease_duration` requires credit coverage | Default 1 hour - evaluate if sufficient |
| **Provider non-acknowledgement** | `pending_timeout` expires unacknowledged leases | Tenants lose time but not funds |
| **Provider griefing** | Provider can acknowledge then immediately close | Allowed by design - reputation is off-chain |
| **Credit lockup** | No withdrawal mechanism for unused credits | Intentional (prevents gaming) but impacts UX |

### State Growth

| Consideration | Current State | Future Improvement |
|---------------|---------------|-------------------|
| **Soft deletes** | Closed leases remain in state forever | Consider archival/pruning after X days |
| **Index overhead** | 5+ indexes per lease (tenant, provider, state, SKU, time) | Monitor storage costs at scale |
| **Provider/SKU accumulation** | Inactive entities never removed | Acceptable - provides audit trail |

### Estimated State Sizes

```
Entity sizes (approximate):
- Provider: ~200 bytes (UUID, addresses, meta_hash, api_url, active)
- SKU: ~250 bytes (UUID, provider_uuid, name, unit, price, meta_hash, active)
- Lease: ~400 bytes (UUID, tenant, provider, items[], timestamps, state)
- CreditAccount: ~100 bytes (tenant, credit_addr, lease counts)

Projected growth:
- 10K providers: ~2 MB
- 100K SKUs: ~25 MB
- 1M leases (including closed): ~400 MB
- 100K credit accounts: ~10 MB

Note: Closed leases accumulate indefinitely under current design.
```

### Missing Features vs Cloud Platforms

| Feature | Status | Rationale |
|---------|--------|-----------|
| **Usage-based billing** | Not supported | Would require trusted oracles or reporters |
| **Credit withdrawal** | Not supported | Prevents gaming, mimics cloud provider credits |
| **Lease modification** | Not supported | Requires close + reopen (future: scaling feature) |
| **Multi-provider leases** | Not supported | Simplifies settlement and authorization |
| **Automatic renewal** | Not supported | Future improvement candidate |

### Trust Model

| Aspect | Current Approach | Consideration |
|--------|------------------|---------------|
| **Provider vetting** | Authority or allowed_list approval required | Centralized but controlled |
| **Service delivery** | Trust-based, no on-chain verification | Off-chain monitoring recommended |
| **Dispute resolution** | None on-chain | Manual resolution, future governance |
| **Authority security** | Single POA admin group | Consider multi-sig, key rotation |

## Questions for Future Development

### High Priority

1. **State pruning**: Should closed leases be archived after a retention period?
2. **Credit withdrawal**: Should tenants be able to withdraw unused credits (with conditions)?
3. **Provider SLA tracking**: Should acknowledgement times be tracked on-chain?

### Medium Priority

4. **Lease scaling**: Can tenants modify quantities without closing leases?
5. **Scheduled closure**: Can tenants set lease end times upfront?
6. **Provider self-registration**: Can we move to permissionless provider onboarding?

### Lower Priority

7. **Usage metering**: Is there demand for non-time-based billing?
8. **Multi-provider leases**: Is single-provider-per-lease too limiting?
9. **On-chain reputation**: Should provider performance be tracked on-chain?

## Design Validation Checklist

When evaluating the current design, consider:

- [ ] Can the system handle 1M+ leases without performance issues?
- [ ] Is the trust model acceptable for the target use cases?
- [ ] Are gas costs reasonable for typical operations?
- [ ] Is the UX acceptable without on-chain price discovery?
- [ ] Is state growth manageable over 5+ years?
- [ ] Are there sufficient off-chain tools for provider discovery?
- [ ] Is the authority model sufficiently decentralized?

## Technical Deep Dive

This section provides a detailed technical comparison of the internal architectures.

### Module Structure

| Aspect | Akash Network | Manifest Ledger |
|--------|---------------|-----------------|
| **Custom Modules** | `x/deployment`, `x/market`, `x/escrow`, `x/provider`, `x/audit`, `x/cert`, `x/take` (7+) | `x/sku`, `x/billing`, `x/manifest` (3) |
| **Module Coupling** | High - tight dependencies between deployment/market/escrow | Lower - cleaner separation between sku/billing |
| **Escrow Approach** | Dedicated `x/escrow` module with separate account types | Integrated into `x/billing` using derived credit addresses (bank module) |
| **Provider Registration** | Separate `x/provider` + `x/audit` for attestations | Combined in `x/sku` (Provider + SKU entities) |
| **Lines of Code** | ~15,000+ across escrow/market/deployment | ~3,500 in billing module |

### Leasing System

| Feature | Akash | Manifest |
|---------|-------|----------|
| **Lease creation flow** | Order → Bid → Lease (auction) | Direct lease creation with price lock |
| **Price discovery** | Providers bid, tenants select winning bid | Fixed SKU pricing, locked at lease creation |
| **Billing granularity** | Per-block (escrow drains continuously) | Per-second with lazy settlement |
| **Settlement timing** | Block-by-block micropayments | On-touch (withdraw triggers settlement) |
| **Multi-denom** | Supported via IBC token conversion | Native support via `sdk.Coins` |

### Escrow vs Credit Accounts

**Akash's Escrow Model:**
```
Tenant → Escrow Account (x/escrow) → Provider (block-by-block)
```
- Escrow is a separate module with its own account types and state
- Requires deposit upfront for entire deployment
- Funds locked in escrow account until lease ends
- Block-by-block settlement in EndBlocker

**Manifest's Credit Account Model:**
```
Tenant → Credit Account (bank module) → Provider (on settlement)
```
- Uses Cosmos bank module with derived addresses (`billing/credit/{tenant}`)
- No separate escrow state - just bank balances + reservation tracking
- Lazy settlement reduces on-chain operations
- Settlement only during explicit operations (withdraw, close)

### Overbooking Prevention

Both systems must prevent tenants from creating more leases than they can pay for.

**Akash**: Pre-funds deployments with full escrow deposit. Each deployment requires a deposit that covers expected costs.

**Manifest**: Credit reservation system that reserves `rate × min_lease_duration` when a lease enters PENDING state:

```go
// On lease creation (PENDING state):
reservationAmount := CalculateLeaseReservation(items, params.MinLeaseDuration)
availableCredit := GetAvailableCredit(balance, creditAccount.ReservedAmounts)

if availableCredit.AmountOf(denom) < reservationAmount.AmountOf(denom) {
    return ErrInsufficientCredit
}

creditAccount.ReservedAmounts = AddReservation(creditAccount.ReservedAmounts, reservationAmount)
```

This ensures each lease has guaranteed funds for at least `min_lease_duration`, preventing overbooking at both PENDING and ACTIVE stages.

## Event Hooks Pattern

Akash uses an elegant **subscriber pattern** for module coordination, while Manifest uses inline logic.

### Akash's Hook System

Akash's escrow module defines callback types that other modules can register:

```go
// In x/escrow/keeper/keeper.go
type AccountHook func(sdk.Context, etypes.Account) error
type PaymentHook func(sdk.Context, etypes.Payment) error

type Keeper struct {
    hooks struct {
        onAccountClosed []AccountHook
        onPaymentClosed []PaymentHook
    }
}

// Registration methods
func (k Keeper) AddOnAccountClosedHook(hook AccountHook) Keeper
func (k Keeper) AddOnPaymentClosedHook(hook PaymentHook) Keeper
```

When an escrow account closes (via `saveAccount()`), all registered hooks are invoked:

```go
func (k Keeper) saveAccount(ctx sdk.Context, account etypes.Account) error {
    // ... save to store ...

    if account.State == StateClosed || account.State == StateOverdrawn {
        for _, hook := range k.hooks.onAccountClosed {
            if err := hook(ctx, account); err != nil {
                return err
            }
        }
    }
    return nil
}
```

Modules like `x/market` and `x/deployment` register hooks during app wiring:

```go
// In app setup
escrowKeeper.AddOnAccountClosedHook(func(ctx sdk.Context, acc etypes.Account) error {
    return marketKeeper.OnEscrowAccountClosed(ctx, acc.ID)
})
```

### Manifest's Inline Approach

Manifest handles side effects inline where they occur. For example, in `CloseLease`:

```go
// In msg_server.go - CloseLease
// 1. Settlement
result, err := ms.k.PerformSettlement(cacheCtx, &lease, closeTime)

// 2. State update
lease.State = types.LEASE_STATE_CLOSED
lease.ClosedAt = &closeTime

// 3. Persist lease
if err := ms.k.SetLease(cacheCtx, lease); err != nil { ... }

// 4. Update credit account counts
ms.k.DecrementActiveLeaseCount(&creditAccount, lease.Uuid)

// 5. Release reservation
reservationAmount := types.CalculateLeaseReservation(lease.Items, params.MinLeaseDuration)
creditAccount.ReservedAmounts = types.SubtractReservation(
    creditAccount.ReservedAmounts,
    reservationAmount,
)

// 6. Save credit account
if err := ms.k.SetCreditAccount(cacheCtx, creditAccount); err != nil { ... }

// 7. Emit event
leaseEvents = append(leaseEvents, leaseEvent{...})
```

This pattern repeats in `RejectLease`, `CancelLease`, `ExpirePendingLease`, and auto-close handlers.

### Comparison

| Aspect | Akash Hooks | Manifest Inline |
|--------|-------------|-----------------|
| **Code organization** | Centralized - one place triggers all callbacks | Distributed - repeated in each handler |
| **Adding new side effects** | Register new hook, no existing code changes | Edit every handler that closes leases |
| **Testing** | Test hooks in isolation | Test each handler separately |
| **Error handling** | Hooks can abort the operation | Already in transaction context |
| **Discoverability** | Look at hook registrations | Grep for state transitions |
| **Coupling** | Loose - modules don't know about each other | Tight - billing knows about credit reservations |
| **Infrastructure** | More upfront code | Less infrastructure, more repetition |

### Why Manifest Uses Inline Logic

1. **Single consumer**: Only the billing module reacts to lease events currently
2. **Explicit flow**: Easier to trace what happens during a close operation
3. **Transaction semantics**: All operations are clearly within the same atomic context
4. **Simplicity**: No hook registration infrastructure to maintain

### When to Consider Hooks

Hooks become valuable when:
- Multiple modules need to react to billing events
- Optional behaviors (webhooks, analytics) should be added without touching core logic
- The number of "on close" side effects grows beyond 3-4

**Example future hook candidates:**
- `x/notifications` - Send alerts when leases close
- `x/analytics` - Track lease lifecycle metrics
- `x/webhooks` - Notify external systems of state changes

If these modules are added, refactoring to a hook pattern would reduce coupling and simplify the billing module.

### Hypothetical Hook Implementation

If Manifest adopted hooks, it might look like:

```go
// x/billing/keeper/hooks.go
type LeaseHook func(ctx context.Context, lease types.Lease) error

type Keeper struct {
    // ...existing fields...
    hooks struct {
        onLeaseClosed   []LeaseHook
        onLeaseCreated  []LeaseHook
        onLeaseRejected []LeaseHook
    }
}

func (k *Keeper) AddOnLeaseClosedHook(hook LeaseHook) {
    k.hooks.onLeaseClosed = append(k.hooks.onLeaseClosed, hook)
}

// Called automatically when lease state transitions to CLOSED
func (k *Keeper) fireLeaseClosedHooks(ctx context.Context, lease types.Lease) error {
    for _, hook := range k.hooks.onLeaseClosed {
        if err := hook(ctx, lease); err != nil {
            return err
        }
    }
    return nil
}
```

Then reservation release could be a registered hook rather than inline code.

## Why We Didn't Fork Akash

The decision to build a custom billing system rather than forking Akash was deliberate:

### Different Problem Domain

| Akash | Manifest |
|-------|----------|
| Decentralized compute marketplace | Credit-based billing for fixed-price catalog |
| Auction-based price discovery | Pre-defined SKU pricing |
| Anonymous provider bidding | Authorized provider registration |
| General-purpose compute | AI infrastructure focus |

### What Forking Would Have Required

1. **Remove bidding/auction logic** (~60% of `x/market`)
2. **Simplify escrow** to just track balances (essentially rewriting it)
3. **Fight tight module coupling** between deployment/market/escrow
4. **Maintain compatibility** with Akash updates or diverge significantly
5. **Adapt to different use case** - fixed pricing vs dynamic auctions

### What We Gained by Building Custom

- **Modern Collections API** vs Akash's raw KVStore in places
- **Cleaner module boundaries** - sku and billing are loosely coupled
- **Purpose-built** for fixed-price SKU catalog model
- **Credit reservation system** designed for our overbooking prevention needs
- **No upstream maintenance burden**

### What's Worth Borrowing from Akash

1. **Hook pattern** - If we add more modules that react to billing events
2. **IBC token payment flow** - Their stable payment feature (AKT2.0) for cross-currency settlement
3. **Genesis handling patterns** - Battle-tested export/import logic

## Filecoin PDP: Proof-Based Billing Deep Dive

Filecoin's [Proof of Data Possession (PDP)](https://docs.filecoin.cloud/core-concepts/pdp-overview/) represents a fundamentally different trust model worth understanding for future iterations.

### The Core Difference

```
Manifest (Trust-Based):   Provider says "I delivered" → We pay
Filecoin (Proof-Based):   Provider proves "I have data" → Contract verifies → Event → Pay
```

The trust is in **math**, not reputation.

### How PDP Works

PDP is a challenge-response protocol that verifies a storage provider still holds data without re-downloading it:

```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│   Client    │     │  Provider   │     │ PDPVerifier │     │ PaymentRail │
│  uploads    │────▶│  computes   │     │  Contract   │     │  Contract   │
│   data      │     │ Merkle tree │     │             │     │             │
└─────────────┘     └──────┬──────┘     └─────────────┘     └─────────────┘
                          │
                          ▼
              ┌───────────────────────┐
              │ Register piece on-chain │
              │ (PieceCID = Merkle root) │
              └───────────┬───────────┘
                          │
        ┌─────────────────┼─────────────────┐
        │                 │                 │
        ▼                 ▼                 ▼
   ┌─────────┐      ┌─────────┐      ┌─────────┐
   │Challenge│      │Challenge│      │Challenge│   ← Random challenges
   │ Period 1│      │ Period 2│      │ Period 3│     from drand beacon
   └────┬────┘      └────┬────┘      └────┬────┘
        │                │                │
        ▼                ▼                ▼
   Provider submits Merkle inclusion proof
        │
        ▼
   PDPVerifier.verify() → recomputes challenge → checks against Merkle root
        │
        ├── FAIL: Transaction reverts, no event, payment halts
        │
        └── SUCCESS: Emits ProofVerified event
                          │
                          ▼
              PaymentRail contract listens for event
                          │
                          ▼
              Funds released to provider
```

### Technical Components

**Piece**: The fundamental storage unit containing:
- Unique ID within a data set
- PieceCID (Merkle root of binary tree with 32-byte leaves)
- Size in bytes

**Data Set**: Logical grouping per client-provider relationship, tracking challenge timing.

**Proof Structure**: Contains leaf hash, leaf offset position, and Merkle proof array.

### Contract Architecture (Separation of Concerns)

| Contract | Responsibility |
|----------|----------------|
| `PDPVerifier` | Cryptographic verification only - manages data sets, generates challenges, verifies proofs |
| `PDPListener` | Business logic - fault detection, proving period management |
| `PaymentRail` | Listens for verification events, releases funds |

This is the **hooks pattern** taken to its logical extreme - contracts communicate via events, not direct calls.

### Security Guarantees

1. **Integrity**: Data has not been altered or replaced
2. **Availability**: Physical presence and retrievability upon challenge
3. **Accountability**: Payments tied to verifiable proofs, not assertions

### What Happens When Proofs Fail

- Transaction **reverts** - invalid proofs are not recorded
- No event emitted - payment rail doesn't activate
- Funds remain locked - provider doesn't get paid
- Automated enforcement - no manual intervention needed

### Could Manifest Adopt This?

| Use Case | Feasibility | Notes |
|----------|-------------|-------|
| **Storage** | High | PDP is well-understood; could adopt directly |
| **Compute** | Low | "Proof of computation" is an open research problem |
| **AI Inference** | Very Low | No production-ready solution exists |
| **Availability** | Medium | Heartbeat proofs (provider online) are simpler |

**Proof of computation options (all have trade-offs):**
- **TEEs** (Trusted Execution Environments) - Hardware attestation, vendor lock-in
- **ZK proofs** - Expensive to generate for general compute
- **Optimistic verification** - Assume correct, challenge window for disputes
- **Redundant execution** - Multiple providers, compare results (expensive)

### Realistic Near-Term Enhancement

**Proof of availability** (heartbeats) - provider proves they're online and responsive:

```protobuf
message MsgProviderHeartbeat {
    string provider = 1;
    bytes challenge_response = 2;  // Response to random challenge
    google.protobuf.Timestamp timestamp = 3;
}
```

This doesn't prove correct computation, but it does prove the provider is responsive. Combined with reputation, this improves the trust model incrementally.

### Key Takeaway

Filecoin's approach works because storage has a natural proof structure (Merkle trees over data). Compute doesn't have an equivalent - the "proof" of computation is the output itself, which is what we're paying for.

For V1 with trusted providers (us), this isn't needed. For V2+ with untrusted providers, availability proofs + reputation + dispute resolution may be more practical than cryptographic compute proofs.

## Comparison Summary

### What Each System Optimizes For

| System | Optimizes For | Trust Model |
|--------|---------------|-------------|
| **Akash** | Price discovery, competitive bidding | Reputation + escrow |
| **Filecoin** | Verifiable storage | Cryptographic proofs |
| **Superfluid** | Real-time streaming, gas efficiency | Smart contract enforcement |
| **Golem/Render** | Task completion, batch processing | Reputation + redundancy |
| **Manifest** | Simplicity, scalability, fixed pricing | Trusted provider (V1) |

### The Trust Spectrum

```
Trustless ◄──────────────────────────────────────────────► Trust-Based
    │                                                           │
    │  Filecoin    Akash      Superfluid   Golem    Manifest   │
    │  (proofs)    (escrow)   (streaming)  (tasks)  (trusted)  │
    │                                                           │
    ▼                                                           ▼
Complex, expensive                              Simple, efficient
```

Manifest is deliberately on the trust-based end for V1. This is appropriate when:
- Provider is known (us)
- Off-chain accountability exists
- Simplicity and scalability are priorities

Future versions can move left on this spectrum by adding:
1. Availability proofs (heartbeats)
2. Reputation system
3. Dispute resolution
4. Eventually, compute verification (if/when practical)

## Related Documentation

- [Architecture](ARCHITECTURE.md) - Technical implementation details
- [Design Decisions](DESIGN_DECISIONS.md) - Rationale for specific choices
- [Capabilities](CAPABILITIES.md) - Feature overview and roadmap
- [SKU Module](../../sku/README.md) - Provider and SKU management
