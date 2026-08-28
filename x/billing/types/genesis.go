package types

import (
	"maps"
	"slices"
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
func NewGenesisState(params Params, leases []Lease, creditAccounts []CreditAccount, leaseSequence uint64) *GenesisState {
	return &GenesisState{
		Params:         params,
		Leases:         leases,
		CreditAccounts: creditAccounts,
		LeaseSequence:  leaseSequence,
	}
}

type genesisValidationOptions struct {
	// The zero value intentionally matches the import-safe production contract.
	// Strict authoring policies must be enabled explicitly.
	enforceCurrentReservedSuffixes bool
	enforceExactLegacyReservations bool
}

// Validate validates all structural and accounting invariants required before
// writing genesis state. It accepts historical state that InitGenesis can
// safely import without replaying policies that cannot be reconstructed from
// that state: a domain may predate its reserved suffix, and a legacy lease with
// no stored creation duration may have been reserved under an earlier minimum
// duration.
func (gs *GenesisState) Validate() error {
	return gs.validate(genesisValidationOptions{})
}

// ValidateStrict additionally applies present-day authoring policies that are
// not safe to replay against historical exports. Use it to lint newly authored
// state, not as an import precondition.
func (gs *GenesisState) ValidateStrict() error {
	return gs.validate(genesisValidationOptions{
		enforceCurrentReservedSuffixes: true,
		enforceExactLegacyReservations: true,
	})
}

func (gs *GenesisState) validate(options genesisValidationOptions) error {
	if err := gs.Params.Validate(); err != nil {
		return ErrInvalidParams.Wrapf("invalid params: %s", err)
	}

	// Validate leases
	seenLeaseUUIDs := make(map[string]bool)
	leaseTenantKeys := make([]string, len(gs.Leases))
	for leaseIndex := range gs.Leases {
		lease := gs.Leases[leaseIndex]
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

		tenantAddr, err := sdk.AccAddressFromBech32(lease.Tenant)
		if err != nil {
			return ErrInvalidLease.Wrapf("lease %s has invalid tenant address: %s", lease.Uuid, err)
		}
		leaseTenantKeys[leaseIndex] = tenantAddr.String()

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

		// Defensive: any item carrying a custom_domain must (1) be a valid FQDN,
		// (2) not match a reserved provider suffix for newly supplied state, and
		// (3) be uniquely addressable by its service_name (specifically: a
		// multi-item legacy lease cannot host custom_domains because the lookup
		// would be ambiguous). Imported claims may predate a suffix reservation.
		for i, item := range lease.Items {
			if item.CustomDomain == "" {
				continue
			}
			if err := IsValidFQDN(item.CustomDomain); err != nil {
				return ErrInvalidCustomDomain.Wrapf("lease %s item %d: %s", lease.Uuid, i, err)
			}
			if options.enforceCurrentReservedSuffixes && MatchesReservedSuffix(item.CustomDomain, gs.Params.ReservedDomainSuffixes) {
				return ErrInvalidCustomDomain.Wrapf(
					"lease %s item %d custom_domain %q matches a reserved provider suffix in gs.Params.ReservedDomainSuffixes",
					lease.Uuid, i, item.CustomDomain,
				)
			}
			matches := 0
			for _, candidate := range lease.Items {
				if candidate.ServiceName == item.ServiceName {
					matches++
				}
			}
			if matches != 1 {
				return ErrAmbiguousLeaseItem.Wrapf(
					"lease %s item %d carries custom_domain %q but is not uniquely addressable by service_name %q",
					lease.Uuid, i, item.CustomDomain, item.ServiceName,
				)
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
	creditAccountTenantKeys := make([]string, len(gs.CreditAccounts))
	for creditAccountIndex := range gs.CreditAccounts {
		ca := gs.CreditAccounts[creditAccountIndex]
		if ca.Tenant == "" {
			return ErrInvalidCreditOperation.Wrap("credit account has empty tenant")
		}

		tenantAddr, err := sdk.AccAddressFromBech32(ca.Tenant)
		if err != nil {
			return ErrInvalidCreditOperation.Wrapf("credit account has invalid tenant address: %s", err)
		}
		tenantKey := tenantAddr.String()
		creditAccountTenantKeys[creditAccountIndex] = tenantKey

		if seenTenants[tenantKey] {
			return ErrInvalidCreditOperation.Wrapf("duplicate credit account for tenant: %s", tenantKey)
		}
		seenTenants[tenantKey] = true

		if ca.CreditAddress == "" {
			return ErrInvalidCreditOperation.Wrapf("credit account for %s has empty credit_address", ca.Tenant)
		}

		creditAddr, err := sdk.AccAddressFromBech32(ca.CreditAddress)
		if err != nil {
			return ErrInvalidCreditOperation.Wrapf("credit account for %s has invalid credit_address: %s", ca.Tenant, err)
		}

		// Verify credit address matches the deterministically derived address from tenant
		expectedCreditAddr := DeriveCreditAddress(tenantAddr)
		if !creditAddr.Equals(expectedCreditAddr) {
			return ErrInvalidCreditOperation.Wrapf("credit account for %s has mismatched credit_address: got %s, expected %s",
				ca.Tenant, ca.CreditAddress, expectedCreditAddr.String())
		}

		// Validate reserved_amounts if present
		if !ca.ReservedAmounts.IsValid() {
			return ErrInvalidCreditOperation.Wrapf("credit account for %s has invalid reserved_amounts", ca.Tenant)
		}

		// Balance is tracked in bank module, no validation needed here
	}

	// Cross-validate reserved_amounts against reconstructible lease reservations.
	// Only PENDING and ACTIVE leases require reservations. A tenant with legacy
	// lease history cannot be checked exactly because its creation-time minimum
	// was never stored; a terminal transition under a later parameter may also
	// have left an unreconstructible residual.
	expectedReservations := make(map[string]sdk.Coins)
	legacyReservationTenants := make(map[string]bool)
	knownReservations := make(map[string]sdk.Coins)
	for i := range gs.Leases {
		lease := &gs.Leases[i]
		tenantKey := leaseTenantKeys[i]
		if !options.enforceExactLegacyReservations && lease.MinLeaseDurationAtCreation == 0 {
			// A terminal legacy lease is evidence that a historical release may
			// have left an unreconstructible residual after a parameter change.
			legacyReservationTenants[tenantKey] = true
		}

		if lease.State != LEASE_STATE_PENDING && lease.State != LEASE_STATE_ACTIVE {
			continue
		}

		reservation := GetLeaseReservationAmount(lease, gs.Params.MinLeaseDuration)
		if existing, ok := expectedReservations[tenantKey]; ok {
			expectedReservations[tenantKey] = existing.Add(reservation...)
		} else {
			expectedReservations[tenantKey] = reservation
		}

		if !options.enforceExactLegacyReservations && lease.MinLeaseDurationAtCreation != 0 {
			if existing, ok := knownReservations[tenantKey]; ok {
				knownReservations[tenantKey] = existing.Add(reservation...)
			} else {
				knownReservations[tenantKey] = reservation
			}
		}
	}

	// Check each credit account's reserved_amounts matches expected
	for i := range gs.CreditAccounts {
		ca := gs.CreditAccounts[i]
		tenantKey := creditAccountTenantKeys[i]
		actualNormalized := sdk.NewCoins(ca.ReservedAmounts...)
		if legacyReservationTenants[tenantKey] {
			known := knownReservations[tenantKey]
			if known == nil {
				known = sdk.NewCoins()
			}
			if !actualNormalized.IsAllGTE(known) {
				return ErrInvalidCreditOperation.Wrapf(
					"credit account for %s has reserved_amounts %s below known non-legacy lease reservations %s",
					ca.Tenant, actualNormalized.String(), known.String(),
				)
			}
			continue
		}

		expected := expectedReservations[tenantKey]
		if expected == nil {
			expected = sdk.NewCoins()
		}

		// Normalize both for comparison (removes zero coins)
		expectedNormalized := sdk.NewCoins(expected...)

		if !actualNormalized.Equal(expectedNormalized) {
			return ErrInvalidCreditOperation.Wrapf(
				"credit account for %s has reserved_amounts %s but lease reservations sum to %s",
				ca.Tenant, actualNormalized.String(), expectedNormalized.String(),
			)
		}
	}

	// Check for tenants with leases but no credit account. Iterate sorted keys
	// so that a genesis with several offending tenants always names the same
	// one in the error, rather than an arbitrary one per run.
	for _, tenant := range slices.Sorted(maps.Keys(expectedReservations)) {
		expected := expectedReservations[tenant]
		if !expected.IsZero() && !seenTenants[tenant] {
			return ErrInvalidCreditOperation.Wrapf(
				"tenant %s has lease reservations totaling %s but no credit account",
				tenant, expected.String(),
			)
		}
	}

	if err := gs.ValidateCreditAccountLeaseCounts(); err != nil {
		return err
	}

	// Validate lease_sequence: must be >= number of leases to prevent UUID
	// collisions after genesis import. Each CreateLease call advances the
	// sequence, so a valid export always has sequence >= len(leases).
	if uint64(len(gs.Leases)) > gs.LeaseSequence {
		return ErrInvalidLease.Wrapf(
			"lease_sequence %d is less than number of leases %d",
			gs.LeaseSequence, len(gs.Leases),
		)
	}

	return nil
}

// ValidateCreditAccountLeaseCounts verifies that the stored credit-account
// aggregates match the imported lease set. Query iteration and message-handler
// accounting rely on these counts, so InitGenesis enforces this invariant even
// when the caller did not run the validate-genesis CLI path.
func (gs *GenesisState) ValidateCreditAccountLeaseCounts() error {
	activeCounts := make(map[string]uint64)
	pendingCounts := make(map[string]uint64)
	leaseTenantOrder := make([]string, 0)
	seenLeaseTenants := make(map[string]bool)
	for i := range gs.Leases {
		lease := &gs.Leases[i]
		tenantAddr, err := sdk.AccAddressFromBech32(lease.Tenant)
		if err != nil {
			return ErrInvalidLease.Wrapf("lease %s has invalid tenant address: %s", lease.Uuid, err)
		}
		tenantKey := tenantAddr.String()

		switch lease.State {
		case LEASE_STATE_ACTIVE:
			activeCounts[tenantKey]++
		case LEASE_STATE_PENDING:
			pendingCounts[tenantKey]++
		default:
			continue
		}
		if !seenLeaseTenants[tenantKey] {
			seenLeaseTenants[tenantKey] = true
			leaseTenantOrder = append(leaseTenantOrder, tenantKey)
		}
	}

	creditAccountTenants := make(map[string]bool, len(gs.CreditAccounts))
	for i := range gs.CreditAccounts {
		ca := &gs.CreditAccounts[i]
		tenantAddr, err := sdk.AccAddressFromBech32(ca.Tenant)
		if err != nil {
			return ErrInvalidCreditOperation.Wrapf("credit account has invalid tenant address: %s", err)
		}
		tenantKey := tenantAddr.String()
		creditAccountTenants[tenantKey] = true
		expectedActive := activeCounts[tenantKey]
		if ca.ActiveLeaseCount != expectedActive {
			return ErrInvalidCreditOperation.Wrapf(
				"credit account for %s has active_lease_count %d but has %d active leases",
				ca.Tenant, ca.ActiveLeaseCount, expectedActive,
			)
		}

		expectedPending := pendingCounts[tenantKey]
		if ca.PendingLeaseCount != expectedPending {
			return ErrInvalidCreditOperation.Wrapf(
				"credit account for %s has pending_lease_count %d but has %d pending leases",
				ca.Tenant, ca.PendingLeaseCount, expectedPending,
			)
		}
	}

	for _, tenant := range leaseTenantOrder {
		if !creditAccountTenants[tenant] {
			return ErrInvalidCreditOperation.Wrapf(
				"tenant %s has %d active and %d pending leases but no credit account",
				tenant, activeCounts[tenant], pendingCounts[tenant],
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
