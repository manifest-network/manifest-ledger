# SKU Module Capabilities

This document provides a comprehensive overview of the SKU module's capabilities and architecture.

## Table of Contents

- [Core Features](#core-features)
- [Authorization Model](#authorization-model)
- [Data Model](#data-model)
- [Billing Units](#billing-units)
- [Future Improvements](#future-improvements)

---

## Core Features

| Capability | Description |
|------------|-------------|
| **Provider Management** | Create, update, deactivate service providers with management/payout addresses |
| **SKU Management** | Define billable items with per-hour or per-day pricing |
| **UUIDv7 Identifiers** | Deterministic, time-ordered unique IDs for consensus safety |
| **Soft Delete** | Deactivation preserves history; existing leases continue operating |
| **API URL Registration** | Providers can register HTTPS endpoints for off-chain API discovery |
| **Allowed List** | Delegate provider/SKU management to specific addresses |
| **Multi-Denom Pricing** | Each SKU can use any token denomination |

---

## Authorization Model

| Actor | Permissions |
|-------|-------------|
| **Authority (Governance)** | Full control: create, update, deactivate, reactivate, modify params |
| **Allowed List Members** | Create, update, deactivate, reactivate providers/SKUs |
| **Params Update** | Authority-only |

### Permission Matrix

| Action | Authority | Allowed List |
|--------|-----------|--------------|
| Create Provider | ✓ | ✓ |
| Update Provider | ✓ | ✓ |
| Deactivate Provider | ✓ | ✓ |
| Reactivate Provider | ✓ | ✓ |
| Create SKU | ✓ | ✓ |
| Update SKU | ✓ | ✓ |
| Deactivate SKU | ✓ | ✓ |
| Reactivate SKU | ✓ | ✓ |
| Update Params | ✓ | ✗ |

---

## Data Model

### Provider

```
Provider
├── uuid (UUIDv7)
├── address (management address)
├── payout_address (where payments are sent)
├── api_url (HTTPS endpoint for off-chain API)
├── meta_hash (off-chain metadata reference)
└── active (soft delete flag)
```

### SKU

```
SKU
├── uuid (UUIDv7)
├── provider_uuid (reference to provider)
├── name (human-readable name)
├── unit (UNIT_PER_HOUR | UNIT_PER_DAY)
├── base_price (Coin with denom and amount)
├── meta_hash (off-chain metadata reference)
└── active (soft delete flag)
```

### State Storage

| Key Prefix | Description |
|------------|-------------|
| `0x00` | Module parameters |
| `0x01` | Providers (UUID → Provider) |
| `0x02` | Provider sequence (for UUIDv7 generation) |
| `0x03` | SKUs (UUID → SKU) with provider index |
| `0x04` | SKU sequence (for UUIDv7 generation) |

---

## Billing Units

See [Billing Units](../README.md#billing-units) in the SKU README for the complete unit reference table.

See [Pricing and Exact Divisibility](../README.md#pricing-and-exact-divisibility) for price validation rules.

---

## Future Improvements

### High Value

| Improvement | Description | Benefit |
|-------------|-------------|---------|
| **Address Index for Providers** | Secondary index: address → provider UUID | O(1) `ProviderByAddress` query instead of O(n) scan |
| **Provider Reputation** | On-chain ratings/reviews | Trust signals for tenants |
| **Provider Categories** | Tagging/categorization system | Easier discovery |

### Medium Value

| Improvement | Description | Benefit |
|-------------|-------------|---------|
| **Tiered Pricing** | Volume discounts in SKU definitions | Enterprise pricing models |
| **SKU Bundles** | Group SKUs into packages | Simplified offerings |
| **Price History** | Track price changes over time | Audit trail |
| **SKU Templates** | Pre-defined SKU configurations | Faster onboarding |

### Technical Improvements

| Improvement | Description | Benefit |
|-------------|-------------|---------|
| **Batch Operations** | Create/update multiple SKUs in one tx | Gas efficiency |
| **Event Enrichment** | More detailed event attributes | Better indexing |
| **Validation Hooks** | Custom validation via CosmWasm | Flexible rules |

---

## Related Documentation

- [README](../README.md) - Module overview
- [API Reference](API.md) - Complete CLI and gRPC/REST API
- [Provider Guide](PROVIDER_GUIDE.md) - Step-by-step provider setup
- [SKU Guide](SKU_GUIDE.md) - Step-by-step SKU creation
- [Billing Module Capabilities](../../billing/docs/CAPABILITIES.md) - Billing system capabilities
