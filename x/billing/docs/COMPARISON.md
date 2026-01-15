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

## Related Documentation

- [Architecture](ARCHITECTURE.md) - Technical implementation details
- [Design Decisions](DESIGN_DECISIONS.md) - Rationale for specific choices
- [Capabilities](CAPABILITIES.md) - Feature overview and roadmap
- [SKU Module](../../sku/README.md) - Provider and SKU management
