# Billing Module Architecture

This document describes the internal architecture of the x/billing module for developers who need to understand, maintain, or extend the module.

## Overview

The Billing module implements a cloud-like billing system where tenants lease resources (SKUs) and are charged from a pre-funded credit account. It provides lazy evaluation of charges, automatic lease closure when funds are exhausted, and provider withdrawals.

## Module Dependencies

```mermaid
graph TD
    Billing[x/billing Module]
    SKU[x/sku Module]
    Bank[x/bank Module]
    POA[x/poa Module]
    
    Billing -->|SKU/Provider lookups| SKU
    Billing -->|token transfers| Bank
    Billing -->|authority validation| POA
    Bank -->|send restrictions| Billing
```

The Billing module:
- **Depends on**: 
  - `x/sku` for SKU and Provider information
  - `x/bank` for token transfers
  - `x/poa` for authority validation
- **Provides to**: `x/bank` send restriction for credit accounts

## Data Model

### Entity Relationship Diagram

```mermaid
erDiagram
    TENANT ||--o| CREDIT_ACCOUNT : "has one"
    TENANT ||--o{ LEASE : "owns"
    LEASE ||--|{ LEASE_ITEM : "contains"
    LEASE_ITEM }o--|| SKU : "references"
    SKU }o--|| PROVIDER : "belongs to"
    
    CREDIT_ACCOUNT {
        string tenant PK
        string credit_address
        uint64 active_lease_count
    }
    
    LEASE {
        uint64 id PK
        string tenant FK
        uint64 provider_id FK
        LeaseState state
        timestamp created_at
        timestamp closed_at
        timestamp last_settled_at
    }
    
    LEASE_ITEM {
        uint64 sku_id FK
        uint64 quantity
        Int locked_price
    }
    
    PROVIDER {
        uint64 id PK
        string payout_address
    }
```

### CreditAccount

Credit accounts hold pre-funded tokens for lease payments:

| Field | Type | Description |
|-------|------|-------------|
| `tenant` | `string` | Tenant's original address (primary key) |
| `credit_address` | `string` | Derived credit account address |
| `active_lease_count` | `uint64` | Number of active leases (for O(1) count) |

**Address Derivation:**
```go
creditAddr = sha256("billing" + tenantAddr)[:20]
```

### Lease

Leases represent active or closed resource rentals:

| Field | Type | Description |
|-------|------|-------------|
| `id` | `uint64` | Auto-incremented unique identifier |
| `tenant` | `string` | Tenant address |
| `provider_id` | `uint64` | Provider ID (denormalized for efficient querying) |
| `items` | `[]LeaseItem` | List of SKU items in this lease |
| `state` | `LeaseState` | ACTIVE or INACTIVE |
| `created_at` | `Timestamp` | When lease was created |
| `closed_at` | `*Timestamp` | When lease was closed (nil if active) |
| `last_settled_at` | `Timestamp` | Last settlement time for the entire lease |

### LeaseItem

Individual line items within a lease:

| Field | Type | Description |
|-------|------|-------------|
| `sku_id` | `uint64` | Reference to SKU |
| `quantity` | `uint64` | Number of units (e.g., 5 instances) |
| `locked_price` | `Coin` | Per-second price locked at lease creation (includes denom) |

**Note**: The `locked_price` is pre-computed at lease creation as the per-second rate for billing calculations. This is derived from the SKU's base price and unit at the time of lease creation. The denomination is preserved from the SKU's `base_price`, enabling multi-denom billing.

### LeaseState Enum

```
LEASE_STATE_UNSPECIFIED = 0  // Invalid
LEASE_STATE_ACTIVE      = 1  // Currently billing
LEASE_STATE_INACTIVE    = 2  // Closed
```

## Storage Layout

### Collections

```mermaid
graph LR
    subgraph "Primary Storage"
        CreditAccounts[CreditAccounts<br/>Map: AccAddress → CreditAccount]
        Leases[Leases<br/>Map: uint64 → Lease]
        Params[Params<br/>Item: Params]
    end
    
    subgraph "Indexes"
        TenantIdx[LeasesByTenant<br/>Map: tenant, lease_id → empty]
        ProviderIdx[LeasesByProvider<br/>Map: provider_id, lease_id → empty]
    end
    
    subgraph "Reverse Lookup"
        CreditReverse[CreditAccountReverse<br/>Map: credit_addr → tenant_addr]
    end
    
    subgraph "Sequences"
        LeaseSeq[LeaseSequence<br/>uint64]
    end
```

| Collection | Key Type | Value Type | Purpose |
|------------|----------|------------|---------|
| `CreditAccounts` | `sdk.AccAddress` | `CreditAccount` | Credit account storage |
| `CreditAccountReverse` | `sdk.AccAddress` | `sdk.AccAddress` | O(1) credit account detection |
| `Leases` | `uint64` | `Lease` | Primary lease storage |
| `LeaseSequence` | - | `uint64` | Auto-increment for lease IDs |
| `LeasesByTenant` | `(AccAddress, uint64)` | `bool` | Tenant → leases index |
| `LeasesByProvider` | `(uint64, uint64)` | `bool` | Provider → leases index |
| `Params` | - | `Params` | Module parameters |

## Core Flows

### Fund Credit Account

```mermaid
sequenceDiagram
    participant User
    participant MsgServer
    participant Keeper
    participant Bank
    participant Store
    
    User->>MsgServer: MsgFundCredit
    MsgServer->>MsgServer: ValidateBasic()
    MsgServer->>Keeper: GetParams()
    Keeper-->>MsgServer: Params (denom)
    
    alt Wrong Denomination
        MsgServer-->>User: Error: invalid denom
    else Correct Denom
        MsgServer->>Keeper: GetOrCreateCreditAccount()
        alt New Account
            Keeper->>Store: Create CreditAccount
            Keeper->>Store: Add to Reverse Lookup
        end
        Keeper-->>MsgServer: CreditAccount
        MsgServer->>Bank: SendCoins(user → credit_addr)
        Bank-->>MsgServer: OK
        MsgServer->>MsgServer: Emit Event
        MsgServer-->>User: Success
    end
```

### Create Lease

```mermaid
sequenceDiagram
    participant User
    participant MsgServer
    participant Keeper
    participant SKU
    participant Store
    
    User->>MsgServer: MsgCreateLease
    MsgServer->>MsgServer: ValidateBasic()
    MsgServer->>Keeper: GetCreditAccount()
    
    alt No Credit Account
        Keeper-->>MsgServer: Error
        MsgServer-->>User: Error
    else Has Credit Account
        Keeper-->>MsgServer: CreditAccount
        MsgServer->>Keeper: GetCreditBalance()
        
        alt Balance < MinLeaseDuration Cost
            MsgServer-->>User: Error: insufficient credit
        else Sufficient Credit
            MsgServer->>Keeper: CountActiveLeases()
            
            alt At Max Leases
                MsgServer-->>User: Error: max leases
            else Under Limit
                loop For Each Item
                    MsgServer->>SKU: GetSKU()
                    alt SKU Invalid/Inactive
                        SKU-->>MsgServer: Error
                        MsgServer-->>User: Error
                    else SKU OK
                        SKU-->>MsgServer: SKU
                        MsgServer->>MsgServer: Build LeaseItem with locked_price
                    end
                end
                
                MsgServer->>Store: Save Lease
                MsgServer->>Store: Update Indexes
                MsgServer->>Store: Increment active_lease_count
                MsgServer->>MsgServer: Emit Event
                MsgServer-->>User: Success + Lease ID
            end
        end
    end
```

### Settlement (Lazy Evaluation)

Settlement happens during withdrawal or lease closure, not continuously:

```mermaid
sequenceDiagram
    participant Trigger
    participant Keeper
    participant Bank
    participant Store
    
    Trigger->>Keeper: settleLease()
    Keeper->>Store: Get Lease
    
    alt Lease Inactive
        Keeper-->>Trigger: Skip (nothing to settle)
    else Lease Active
        Keeper->>Keeper: Get Current Time
        Keeper->>Keeper: Calculate Duration Since last_settled_at
        
        alt Duration <= 0
            Keeper-->>Trigger: Zero amount (nothing accrued)
        else Duration > 0
            loop For Each Item
                Keeper->>Keeper: Calculate Accrual
                Note over Keeper: accrual = duration_seconds × locked_price × quantity
            end
            
            Keeper->>Keeper: Sum Total Accrued
            Keeper->>Bank: Get Credit Balance
            
            alt Accrued > Balance
                Keeper->>Keeper: Cap Transfer to Balance
            end
            
            alt Transfer Amount > 0
                Keeper->>Bank: Transfer to Provider Payout Address
            end
            
            Keeper->>Store: Update last_settled_at
            Keeper-->>Trigger: Settlement Amount
        end
    end
```

### Close Lease with Settlement

```mermaid
sequenceDiagram
    participant Sender
    participant MsgServer
    participant Keeper
    participant Bank
    participant Store
    
    Sender->>MsgServer: MsgCloseLease
    MsgServer->>MsgServer: ValidateBasic()
    MsgServer->>Keeper: GetLease()
    
    alt Lease Not Found
        Keeper-->>MsgServer: Error
        MsgServer-->>Sender: Error: not found
    else Lease Found
        Keeper-->>MsgServer: Lease
        
        alt Lease Already Closed
            MsgServer-->>Sender: Error: not active
        else Lease Active
            MsgServer->>MsgServer: Verify Authorization
            alt Not Authorized
                MsgServer-->>Sender: Error: unauthorized
            else Authorized
                MsgServer->>Keeper: settleAndCloseLease()
                Keeper->>Keeper: Calculate Final Settlement
                Keeper->>Bank: Transfer Settlement to Provider
                Keeper->>Store: Update Lease State to INACTIVE
                Keeper->>Store: Set closed_at
                Keeper->>Store: Decrement active_lease_count
                Keeper-->>MsgServer: Settlement Amount
                MsgServer->>MsgServer: Emit Events
                MsgServer-->>Sender: Success
            end
        end
    end
```

### Withdrawal Flow

```mermaid
sequenceDiagram
    participant Provider
    participant MsgServer
    participant Keeper
    participant SKU
    participant Bank
    
    Provider->>MsgServer: MsgWithdraw
    MsgServer->>MsgServer: ValidateBasic()
    MsgServer->>Keeper: GetLease()
    Keeper-->>MsgServer: Lease
    
    alt Lease Not Active
        MsgServer-->>Provider: Error: not active
    else Lease Active
        MsgServer->>SKU: Get Provider for Lease
        SKU-->>MsgServer: Provider
        
        alt Sender Not Provider/Authority
            MsgServer-->>Provider: Error: unauthorized
        else Authorized
            MsgServer->>Keeper: settleLease()
            Note over Keeper: Calculate and transfer accrued amount
            
            alt Nothing Settled
                MsgServer-->>Provider: Error: no withdrawable amount
            else Has Settlement
                MsgServer->>MsgServer: Emit Event
                MsgServer-->>Provider: Success + Amount
            end
        end
    end
```

## Settlement Triggers

Settlement happens lazily at these points:

| Trigger | Scope | Reason |
|---------|-------|--------|
| `CloseLease` | Target lease only | Final settlement before closure |
| `Withdraw` | Target lease only | Settle accrued amount for provider |
| `WithdrawAll` | All provider's active leases | Batch settlement |

**Note**: Lease queries (`Lease`, `Leases`, `LeasesByTenant`, `LeasesByProvider`) return stored state and do NOT trigger settlement. Use `WithdrawableAmount` or `ProviderWithdrawable` queries to get real-time calculated accrued amounts. Settlement (actual token transfer) only happens during write operations.

### Auto-Close on Credit Exhaustion

When a lease's credit is exhausted, it can be auto-closed via `CheckAndCloseExhaustedLease`:

```mermaid
flowchart TD
    A[CheckAndCloseExhaustedLease] --> B{Is Lease Active?}
    B -->|No| C[Return: not closed]
    B -->|Yes| D[Get Credit Balance]
    D --> E{Balance == 0?}
    E -->|No| F[Return: not closed]
    E -->|Yes| G[Close Lease]
    G --> H[Set State = INACTIVE]
    H --> I[Set closed_at]
    I --> J[Decrement active_lease_count]
    J --> K[Return: closed]
```

## Credit Account Multi-Denom Support

Credit accounts support multiple token denominations. Since credit accounts are regular bank module accounts, they can hold any token type. This enables:

- Different SKUs can use different payment tokens
- Tenants fund their credit with the tokens required by their target SKUs
- Settlement transfers happen per-denom to the provider's payout address

**No send restrictions** are applied to credit accounts - any token can be sent to them.

## Accrual Calculation

### Per-Second Rate (at Lease Creation)

The `locked_price` stored in `LeaseItem` is already the per-second rate, calculated at lease creation:

```go
// During lease creation
lockedPricePerSecond = skutypes.CalculatePricePerSecond(sku.BasePrice, sku.Unit)
```

### Accrual Formula

```
elapsed_seconds = current_time - last_settled_at
item_accrual = elapsed_seconds × locked_price.Amount × quantity
total_accrual = sum(item_accrual for all items, grouped by denom)
```

### Multi-Denom Settlement

When a lease contains SKUs with different denominations:
1. Accruals are calculated per-item
2. Amounts are grouped by denomination
3. Each denom is transferred separately to the provider's payout address

### Example

SKU 1: 3600upwr per hour → 1upwr per second (locked_price = {denom: "upwr", amount: 1})
SKU 2: 7200umfx per hour → 2umfx per second (locked_price = {denom: "umfx", amount: 2})
Quantities: SKU 1 = 5 instances, SKU 2 = 3 instances
Elapsed: 100 seconds

```
item1_accrual = 100 × 1 × 5 = 500upwr
item2_accrual = 100 × 2 × 3 = 600umfx
total_accrual = [500upwr, 600umfx]
```

## Parameters

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `max_leases_per_tenant` | `uint64` | 100 | Max active leases per tenant |
| `max_items_per_lease` | `uint64` | 20 | Max items in single lease |
| `min_lease_duration` | `uint64` | 3600 | Minimum seconds of credit required to create a lease |
| `allowed_list` | `[]string` | `[]` | Addresses that can create leases for tenants (in addition to authority) |

**Note**: There is no global `denom` parameter. Each SKU defines its own denomination in its `base_price`. This enables multi-denom billing where different SKUs can be priced in different tokens.

**Note**: `WithdrawAll` limits are enforced via constants, not parameters:
- Default limit: 50 leases per call
- Maximum limit: 100 leases per call

## Events

| Event | Key Attributes | When Emitted |
|-------|----------------|--------------|
| `credit_funded` | `tenant`, `amount`, `credit_address` | Credit account funded |
| `lease_created` | `lease_id`, `tenant`, `provider_id`, `items` | New lease created |
| `lease_closed` | `lease_id`, `tenant`, `settled_amount` | Lease closed (manual or auto) |
| `provider_withdrawal` | `lease_id`, `provider_id`, `amount`, `payout_address` | Provider withdrew funds |

## Error Codes

| Error | Description |
|-------|-------------|
| `ErrCreditAccountNotFound` | Tenant has no credit account |
| `ErrInsufficientCredit` | Credit balance below minimum required |
| `ErrLeaseNotFound` | Lease does not exist |
| `ErrLeaseNotActive` | Lease already closed |
| `ErrUnauthorized` | Sender not authorized for operation |
| `ErrInvalidLease` | Lease validation failed |
| `ErrMaxLeasesReached` | Tenant at max active leases |
| `ErrNoWithdrawable` | Nothing to withdraw |
| `ErrInvalidCreditOperation` | Credit operation failed |
| `ErrProviderNotFound` | Referenced provider not found |
| `ErrSKUNotFound` | Referenced SKU not found |
| `ErrSKUInactive` | SKU is deactivated |
| `ErrInvalidDenomination` | Wrong token denomination |
| `ErrEmptyLeaseItems` | Lease has no items |
| `ErrTooManyLeaseItems` | Lease exceeds max items |
| `ErrDuplicateSKU` | Same SKU appears multiple times in lease |
| `ErrInvalidQuantity` | Item quantity is zero |
| `ErrInvalidParams` | Invalid module parameters |

## Security Considerations

### Authorization Matrix

| Operation | Tenant | Provider | Authority | Allow-Listed |
|-----------|--------|----------|-----------|--------------|
| FundCredit | ✓ (any) | ✓ (any) | ✓ | ✓ |
| CreateLease | ✓ (self) | ✗ | ✗ | ✗ |
| CreateLeaseForTenant | ✗ | ✗ | ✓ | ✓ |
| CloseLease | ✓ (own) | ✓ (own SKU) | ✓ | ✗ |
| Withdraw | ✗ | ✓ (own) | ✓ | ✗ |
| WithdrawAll | ✗ | ✓ (own) | ✓ | ✗ |
| UpdateParams | ✗ | ✗ | ✓ | ✗ |

### Overflow Protection

Accrual calculations use safe math operations to prevent overflow:

```go
func CalculateTotalAccruedForLease(items []LeaseItemWithPrice, duration time.Duration) (math.Int, error) {
    totalAccrued := math.ZeroInt()
    durationSeconds := int64(duration.Seconds())
    
    for _, item := range items {
        itemAccrued, err := CalculateAccruedAmount(durationSeconds, item.LockedPricePerSecond, item.Quantity)
        if err != nil {
            return math.Int{}, err
        }
        totalAccrued = totalAccrued.Add(itemAccrued)
    }
    return totalAccrued, nil
}
```

### DoS Mitigations

1. **Max leases per tenant** - Prevents lease spam
2. **Max items per lease** - Limits computation per lease
3. **Withdrawal batch size** - Caps WithdrawAll iterations (max 100)
4. **Min lease duration** - Prevents immediate exhaustion
5. **Lazy settlement** - No EndBlocker overhead
6. **Indexed lookups** - O(1) credit account detection

## Performance Characteristics

| Operation | Complexity | Notes |
|-----------|------------|-------|
| FundCredit | O(1) | Bank transfer + storage write |
| CreateLease | O(m) | m = items in lease |
| CloseLease | O(m) | m = items in lease |
| Withdraw | O(m) | m = items in lease |
| WithdrawAll | O(k×m) | k = leases (max 100), m = avg items |
| GetCreditBalance | O(1) | Bank query |
| isCreditAccount | O(1) | Reverse lookup map |
| GetLeasesByTenant | O(n) | n = tenant's leases |
| GetLeasesByProvider | O(n) | n = provider's leases |

## Testing Strategy

### Unit Tests (`x/billing/keeper/*_test.go`)
- Message validation (`ValidateBasic`)
- Accrual calculations
- Settlement logic (partial/full credit exhaustion)
- Send restrictions
- Authorization checks (tenant, provider, authority)
- Error scenarios (non-existent lease, unauthorized, closed lease)
- Genesis import/export

### Integration Tests (`x/billing/keeper/*_test.go`)
- Full message flows with real app context
- Multi-lease scenarios
- Credit account lifecycle

### E2E Tests (`interchaintest/billing_test.go`)
- Complete billing cycle
- Provider withdrawals
- Credit protection via send restrictions
- Multi-tenant scenarios

### Group/POA Tests (`interchaintest/poa_group_test.go`)
- Provider/SKU management via group proposals
- Lease creation for tenants via authority
- Withdrawal via group proposals

### Simulation (`x/billing/simulation/`)
- Random operations
- Stress testing
- State consistency
