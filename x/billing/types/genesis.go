package types

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// DefaultGenesis returns the default genesis state.
func DefaultGenesis() *GenesisState {
	return &GenesisState{
		Params:         DefaultParams(),
		Leases:         []Lease{},
		CreditAccounts: []CreditAccount{},
		NextLeaseId:    1,
	}
}

// NewGenesisState creates a new genesis state with the given parameters.
func NewGenesisState(params Params, leases []Lease, creditAccounts []CreditAccount, nextLeaseID uint64) *GenesisState {
	return &GenesisState{
		Params:         params,
		Leases:         leases,
		CreditAccounts: creditAccounts,
		NextLeaseId:    nextLeaseID,
	}
}

// Validate performs basic genesis state validation.
func (gs *GenesisState) Validate() error {
	if err := gs.Params.Validate(); err != nil {
		return fmt.Errorf("invalid params: %w", err)
	}

	// NextLeaseId must be at least 1
	if gs.NextLeaseId == 0 {
		return fmt.Errorf("next_lease_id cannot be zero")
	}

	// Validate leases
	seenLeaseIDs := make(map[uint64]bool)
	for _, lease := range gs.Leases {
		if seenLeaseIDs[lease.Id] {
			return fmt.Errorf("duplicate lease id: %d", lease.Id)
		}
		seenLeaseIDs[lease.Id] = true

		if lease.Id >= gs.NextLeaseId {
			return fmt.Errorf("lease id %d is greater than or equal to next_lease_id %d", lease.Id, gs.NextLeaseId)
		}

		if lease.Tenant == "" {
			return fmt.Errorf("lease %d has empty tenant", lease.Id)
		}

		if _, err := sdk.AccAddressFromBech32(lease.Tenant); err != nil {
			return fmt.Errorf("lease %d has invalid tenant address: %w", lease.Id, err)
		}

		if lease.ProviderId == 0 {
			return fmt.Errorf("lease %d has zero provider_id", lease.Id)
		}

		if len(lease.Items) == 0 {
			return fmt.Errorf("lease %d has no items", lease.Id)
		}

		for i, item := range lease.Items {
			if item.SkuId == 0 {
				return fmt.Errorf("lease %d item %d has zero sku_id", lease.Id, i)
			}
			if item.Quantity == 0 {
				return fmt.Errorf("lease %d item %d has zero quantity", lease.Id, i)
			}
			if item.LockedPrice.IsNil() || item.LockedPrice.IsNegative() || item.LockedPrice.IsZero() {
				return fmt.Errorf("lease %d item %d has invalid locked_price", lease.Id, i)
			}
		}

		if lease.State == LEASE_STATE_UNSPECIFIED {
			return fmt.Errorf("lease %d has unspecified state", lease.Id)
		}

		// For inactive leases, validate closed_at is set
		if lease.State == LEASE_STATE_INACTIVE {
			if lease.ClosedAt == nil || lease.ClosedAt.IsZero() {
				return fmt.Errorf("lease %d is inactive but has no closed_at timestamp", lease.Id)
			}
		}
	}

	// Validate credit accounts
	seenTenants := make(map[string]bool)
	for _, ca := range gs.CreditAccounts {
		if seenTenants[ca.Tenant] {
			return fmt.Errorf("duplicate credit account for tenant: %s", ca.Tenant)
		}
		seenTenants[ca.Tenant] = true

		if ca.Tenant == "" {
			return fmt.Errorf("credit account has empty tenant")
		}

		if _, err := sdk.AccAddressFromBech32(ca.Tenant); err != nil {
			return fmt.Errorf("credit account has invalid tenant address: %w", err)
		}

		if ca.CreditAddress == "" {
			return fmt.Errorf("credit account for %s has empty credit_address", ca.Tenant)
		}

		if _, err := sdk.AccAddressFromBech32(ca.CreditAddress); err != nil {
			return fmt.Errorf("credit account for %s has invalid credit_address: %w", ca.Tenant, err)
		}

		if !ca.Balance.IsValid() {
			return fmt.Errorf("credit account for %s has invalid balance", ca.Tenant)
		}

		// Validate balance denom matches params denom
		if ca.Balance.Denom != gs.Params.Denom {
			return fmt.Errorf("credit account for %s has balance denom %s, expected %s", ca.Tenant, ca.Balance.Denom, gs.Params.Denom)
		}
	}

	return nil
}
