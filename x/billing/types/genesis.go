package types

import (
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
		return ErrInvalidParams.Wrapf("invalid params: %s", err)
	}

	// Validate leases
	seenLeaseUUIDs := make(map[string]bool)
	for _, lease := range gs.Leases {
		if lease.Uuid == "" {
			return ErrInvalidLease.Wrap("lease has empty uuid")
		}

		if !pkguuid.IsValidUUID(lease.Uuid) {
			return ErrInvalidLease.Wrapf("lease has invalid uuid format: %s", lease.Uuid)
		}

		if seenLeaseUUIDs[lease.Uuid] {
			return ErrInvalidLease.Wrapf("duplicate lease uuid: %s", lease.Uuid)
		}
		seenLeaseUUIDs[lease.Uuid] = true

		if lease.Tenant == "" {
			return ErrInvalidLease.Wrapf("lease %s has empty tenant", lease.Uuid)
		}

		if _, err := sdk.AccAddressFromBech32(lease.Tenant); err != nil {
			return ErrInvalidLease.Wrapf("lease %s has invalid tenant address: %s", lease.Uuid, err)
		}

		if lease.ProviderUuid == "" {
			return ErrInvalidLease.Wrapf("lease %s has empty provider_uuid", lease.Uuid)
		}

		if !pkguuid.IsValidUUID(lease.ProviderUuid) {
			return ErrInvalidLease.Wrapf("lease %s has invalid provider_uuid format: %s", lease.Uuid, lease.ProviderUuid)
		}

		if len(lease.Items) == 0 {
			return ErrInvalidLease.Wrapf("lease %s has no items", lease.Uuid)
		}

		hasServiceName := 0
		for i, item := range lease.Items {
			if item.SkuUuid == "" {
				return ErrInvalidLease.Wrapf("lease %s item %d has empty sku_uuid", lease.Uuid, i)
			}
			if !pkguuid.IsValidUUID(item.SkuUuid) {
				return ErrInvalidLease.Wrapf("lease %s item %d has invalid sku_uuid format: %s", lease.Uuid, i, item.SkuUuid)
			}
			if item.Quantity == 0 {
				return ErrInvalidLease.Wrapf("lease %s item %d has zero quantity", lease.Uuid, i)
			}
			if !item.LockedPrice.IsValid() || item.LockedPrice.IsZero() {
				return ErrInvalidLease.Wrapf("lease %s item %d has invalid locked_price", lease.Uuid, i)
			}
			if item.ServiceName != "" {
				hasServiceName++
			}
		}

		// Validate service_name consistency: all-or-nothing
		if hasServiceName > 0 && hasServiceName != len(lease.Items) {
			return ErrInvalidServiceName.Wrapf("lease %s: all items must have service_name or none", lease.Uuid)
		}
		if hasServiceName > 0 {
			seenNames := make(map[string]bool, len(lease.Items))
			for i, item := range lease.Items {
				if !IsValidDNSLabel(item.ServiceName) {
					return ErrInvalidServiceName.Wrapf("lease %s item %d has invalid service_name: %q", lease.Uuid, i, item.ServiceName)
				}
				if seenNames[item.ServiceName] {
					return ErrInvalidServiceName.Wrapf("lease %s has duplicate service_name %q", lease.Uuid, item.ServiceName)
				}
				seenNames[item.ServiceName] = true
			}
		} else {
			// Legacy mode: enforce sku_uuid uniqueness.
			seenSKUs := make(map[string]bool, len(lease.Items))
			for _, item := range lease.Items {
				if seenSKUs[item.SkuUuid] {
					return ErrDuplicateSKU.Wrapf("lease %s has duplicate sku_uuid %s", lease.Uuid, item.SkuUuid)
				}
				seenSKUs[item.SkuUuid] = true
			}
		}

		if lease.State == LEASE_STATE_UNSPECIFIED {
			return ErrInvalidLease.Wrapf("lease %s has unspecified state", lease.Uuid)
		}

		// Validate meta_hash length
		if len(lease.MetaHash) > MaxMetaHashLength {
			return ErrInvalidMetaHash.Wrapf("lease %s has meta_hash exceeding maximum length of %d bytes", lease.Uuid, MaxMetaHashLength)
		}

		// Note: min_lease_duration_at_creation is a uint64 and doesn't require validation.
		// Zero value is valid for legacy leases (will fall back to current param).

		// For inactive leases, validate closed_at is set
		if lease.State == LEASE_STATE_CLOSED {
			if lease.ClosedAt == nil || lease.ClosedAt.IsZero() {
				return ErrInvalidLease.Wrapf("lease %s is closed but has no closed_at timestamp", lease.Uuid)
			}
		}
	}

	// Validate credit accounts
	seenTenants := make(map[string]bool)
	for _, ca := range gs.CreditAccounts {
		if ca.Tenant == "" {
			return ErrInvalidCreditOperation.Wrap("credit account has empty tenant")
		}

		if seenTenants[ca.Tenant] {
			return ErrInvalidCreditOperation.Wrapf("duplicate credit account for tenant: %s", ca.Tenant)
		}
		seenTenants[ca.Tenant] = true

		if _, err := sdk.AccAddressFromBech32(ca.Tenant); err != nil {
			return ErrInvalidCreditOperation.Wrapf("credit account has invalid tenant address: %s", err)
		}

		if ca.CreditAddress == "" {
			return ErrInvalidCreditOperation.Wrapf("credit account for %s has empty credit_address", ca.Tenant)
		}

		if _, err := sdk.AccAddressFromBech32(ca.CreditAddress); err != nil {
			return ErrInvalidCreditOperation.Wrapf("credit account for %s has invalid credit_address: %s", ca.Tenant, err)
		}

		// Verify credit address matches the deterministically derived address from tenant
		tenantAddr, _ := sdk.AccAddressFromBech32(ca.Tenant) // Already validated above
		expectedCreditAddr := DeriveCreditAddress(tenantAddr)
		if ca.CreditAddress != expectedCreditAddr.String() {
			return ErrInvalidCreditOperation.Wrapf("credit account for %s has mismatched credit_address: got %s, expected %s",
				ca.Tenant, ca.CreditAddress, expectedCreditAddr.String())
		}

		// Validate reserved_amounts if present
		if !ca.ReservedAmounts.IsValid() {
			return ErrInvalidCreditOperation.Wrapf("credit account for %s has invalid reserved_amounts", ca.Tenant)
		}

		// Balance is tracked in bank module, no validation needed here
	}

	// Cross-validate: reserved_amounts must match sum of lease reservations per tenant
	// Only PENDING and ACTIVE leases have reservations
	expectedReservations := CalculateExpectedReservationsByTenant(gs.Leases, gs.Params.MinLeaseDuration)

	// Check each credit account's reserved_amounts matches expected
	for _, ca := range gs.CreditAccounts {
		expected := expectedReservations[ca.Tenant]
		if expected == nil {
			expected = sdk.NewCoins()
		}

		// Normalize both for comparison (removes zero coins)
		actualNormalized := sdk.NewCoins(ca.ReservedAmounts...)
		expectedNormalized := sdk.NewCoins(expected...)

		if !actualNormalized.Equal(expectedNormalized) {
			return ErrInvalidCreditOperation.Wrapf(
				"credit account for %s has reserved_amounts %s but lease reservations sum to %s",
				ca.Tenant, actualNormalized.String(), expectedNormalized.String(),
			)
		}
	}

	// Check for tenants with leases but no credit account
	for tenant, expected := range expectedReservations {
		if !expected.IsZero() && !seenTenants[tenant] {
			return ErrInvalidCreditOperation.Wrapf(
				"tenant %s has lease reservations totaling %s but no credit account",
				tenant, expected.String(),
			)
		}
	}

	// Cross-validate: active_lease_count and pending_lease_count must match actual lease counts per tenant
	activeCounts := make(map[string]uint64)
	pendingCounts := make(map[string]uint64)
	for _, lease := range gs.Leases {
		switch lease.State {
		case LEASE_STATE_ACTIVE:
			activeCounts[lease.Tenant]++
		case LEASE_STATE_PENDING:
			pendingCounts[lease.Tenant]++
		}
	}

	for _, ca := range gs.CreditAccounts {
		expectedActive := activeCounts[ca.Tenant]
		if ca.ActiveLeaseCount != expectedActive {
			return ErrInvalidCreditOperation.Wrapf(
				"credit account for %s has active_lease_count %d but has %d active leases",
				ca.Tenant, ca.ActiveLeaseCount, expectedActive,
			)
		}

		expectedPending := pendingCounts[ca.Tenant]
		if ca.PendingLeaseCount != expectedPending {
			return ErrInvalidCreditOperation.Wrapf(
				"credit account for %s has pending_lease_count %d but has %d pending leases",
				ca.Tenant, ca.PendingLeaseCount, expectedPending,
			)
		}
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
			return ErrInvalidLease.Wrapf(
				"lease %s has last_settled_at (%s) in the future relative to block time (%s)",
				lease.Uuid,
				lease.LastSettledAt.String(),
				blockTime.String(),
			)
		}

		// Validate CreatedAt is not in the future
		if lease.CreatedAt.After(blockTime) {
			return ErrInvalidLease.Wrapf(
				"lease %s has created_at (%s) in the future relative to block time (%s)",
				lease.Uuid,
				lease.CreatedAt.String(),
				blockTime.String(),
			)
		}

		// For inactive leases, validate ClosedAt is not in the future
		if lease.State == LEASE_STATE_CLOSED && lease.ClosedAt != nil {
			if lease.ClosedAt.After(blockTime) {
				return ErrInvalidLease.Wrapf(
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
