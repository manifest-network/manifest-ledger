package types

import (
	"fmt"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"

	pkguuid "github.com/manifest-network/manifest-ledger/pkg/uuid"
)

// DefaultGenesis returns the default genesis state.
func DefaultGenesis() *GenesisState {
	return &GenesisState{
		Params:         DefaultParams(),
		Leases:         []Lease{},
		CreditAccounts: []CreditAccount{},
	}
}

// NewGenesisState creates a new genesis state with the given parameters.
func NewGenesisState(params Params, leases []Lease, creditAccounts []CreditAccount) *GenesisState {
	return &GenesisState{
		Params:         params,
		Leases:         leases,
		CreditAccounts: creditAccounts,
	}
}

// Validate performs basic genesis state validation.
func (gs *GenesisState) Validate() error {
	if err := gs.Params.Validate(); err != nil {
		return fmt.Errorf("invalid params: %w", err)
	}

	// Validate leases
	seenLeaseUUIDs := make(map[string]bool)
	for _, lease := range gs.Leases {
		if lease.Uuid == "" {
			return fmt.Errorf("lease has empty uuid")
		}

		if !pkguuid.IsValidUUID(lease.Uuid) {
			return fmt.Errorf("lease has invalid uuid format: %s", lease.Uuid)
		}

		if seenLeaseUUIDs[lease.Uuid] {
			return fmt.Errorf("duplicate lease uuid: %s", lease.Uuid)
		}
		seenLeaseUUIDs[lease.Uuid] = true

		if lease.Tenant == "" {
			return fmt.Errorf("lease %s has empty tenant", lease.Uuid)
		}

		if _, err := sdk.AccAddressFromBech32(lease.Tenant); err != nil {
			return fmt.Errorf("lease %s has invalid tenant address: %w", lease.Uuid, err)
		}

		if lease.ProviderUuid == "" {
			return fmt.Errorf("lease %s has empty provider_uuid", lease.Uuid)
		}

		if !pkguuid.IsValidUUID(lease.ProviderUuid) {
			return fmt.Errorf("lease %s has invalid provider_uuid format: %s", lease.Uuid, lease.ProviderUuid)
		}

		if len(lease.Items) == 0 {
			return fmt.Errorf("lease %s has no items", lease.Uuid)
		}

		for i, item := range lease.Items {
			if item.SkuUuid == "" {
				return fmt.Errorf("lease %s item %d has empty sku_uuid", lease.Uuid, i)
			}
			if !pkguuid.IsValidUUID(item.SkuUuid) {
				return fmt.Errorf("lease %s item %d has invalid sku_uuid format: %s", lease.Uuid, i, item.SkuUuid)
			}
			if item.Quantity == 0 {
				return fmt.Errorf("lease %s item %d has zero quantity", lease.Uuid, i)
			}
			if !item.LockedPrice.IsValid() || item.LockedPrice.IsZero() {
				return fmt.Errorf("lease %s item %d has invalid locked_price", lease.Uuid, i)
			}
		}

		if lease.State == LEASE_STATE_UNSPECIFIED {
			return fmt.Errorf("lease %s has unspecified state", lease.Uuid)
		}

		// For inactive leases, validate closed_at is set
		if lease.State == LEASE_STATE_CLOSED {
			if lease.ClosedAt == nil || lease.ClosedAt.IsZero() {
				return fmt.Errorf("lease %s is inactive but has no closed_at timestamp", lease.Uuid)
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

		// Verify credit address matches the deterministically derived address from tenant
		tenantAddr, _ := sdk.AccAddressFromBech32(ca.Tenant) // Already validated above
		expectedCreditAddr := DeriveCreditAddress(tenantAddr)
		if ca.CreditAddress != expectedCreditAddr.String() {
			return fmt.Errorf("credit account for %s has mismatched credit_address: got %s, expected %s",
				ca.Tenant, ca.CreditAddress, expectedCreditAddr.String())
		}

		// Balance is tracked in bank module, no validation needed here
	}

	return nil
}

// ValidateWithBlockTime performs additional genesis state validation that requires block time.
// This is called during InitGenesis when block time is available.
// It validates that LastSettledAt timestamps are not in the future relative to block time.
func (gs *GenesisState) ValidateWithBlockTime(blockTime time.Time) error {
	for _, lease := range gs.Leases {
		// Validate LastSettledAt is not in the future
		if lease.LastSettledAt.After(blockTime) {
			return fmt.Errorf(
				"lease %s has last_settled_at (%s) in the future relative to block time (%s)",
				lease.Uuid,
				lease.LastSettledAt.String(),
				blockTime.String(),
			)
		}

		// Validate CreatedAt is not in the future
		if lease.CreatedAt.After(blockTime) {
			return fmt.Errorf(
				"lease %s has created_at (%s) in the future relative to block time (%s)",
				lease.Uuid,
				lease.CreatedAt.String(),
				blockTime.String(),
			)
		}

		// For inactive leases, validate ClosedAt is not in the future
		if lease.State == LEASE_STATE_CLOSED && lease.ClosedAt != nil {
			if lease.ClosedAt.After(blockTime) {
				return fmt.Errorf(
					"lease %s has closed_at (%s) in the future relative to block time (%s)",
					lease.Uuid,
					lease.ClosedAt.String(),
					blockTime.String(),
				)
			}
		}
	}

	return nil
}
