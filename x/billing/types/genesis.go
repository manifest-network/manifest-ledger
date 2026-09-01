package types

import (
	"math"
	"slices"
	"time"

	errorsmod "cosmossdk.io/errors"

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

// Validate applies the same import-safe preparation and validation used before
// InitGenesis writes state. It accepts bounded, historically reachable drift in
// derived counts and pre-v4 aggregate reservations, and does not replay policies
// that cannot be reconstructed: a domain may predate its reserved suffix, and a
// legacy lease with no stored creation duration may have been reserved under an
// earlier minimum duration. Use ValidateStrict for newly authored state.
func (gs *GenesisState) Validate() error {
	_, err := gs.PrepareForImport()
	return err
}

// PrepareForImport returns an import-safe copy. It canonicalizes equivalent
// Bech32 allowed-list spellings, reconstructs cached live-lease counts, and
// reconciles a complete pre-v4 aggregate-only export to the reservation floor
// provable from modern leases. Import-safe validation is applied to the
// prepared copy; the caller's genesis value is not mutated.
func (gs *GenesisState) PrepareForImport() (*GenesisState, error) {
	prepared := *gs
	prepared.Params = gs.Params
	prepared.CreditAccounts = append([]CreditAccount(nil), gs.CreditAccounts...)
	legacyReservationState, err := prepared.HasLegacyReservationState()
	if err != nil {
		return nil, err
	}

	allowedList, err := CanonicalUniqueAddresses(gs.Params.AllowedList)
	if err != nil {
		return nil, err
	}
	prepared.Params.AllowedList = allowedList
	if err := prepared.repairCreditAccountDerivedStateForImport(legacyReservationState); err != nil {
		return nil, err
	}

	if err := prepared.validate(genesisValidationOptions{}); err != nil {
		return nil, err
	}
	return &prepared, nil
}

// repairCreditAccountDerivedStateForImport reconstructs cached live-lease
// counts from primary lease state. For a complete pre-v4 aggregate-only export,
// it also mirrors Migrate2to3's reservation repair: accounts with a live opaque
// legacy cohort preserve unknown excess but are raised to the modern provable
// floor, while every other account is reconciled exactly to that floor.
//
// Historical v2 handlers could under-report counts when equivalent Bech32
// spellings identified the same account. They could also clamp the aggregate
// below a concurrent modern claim when releasing a zero-duration legacy lease
// after the minimum duration changed. Grouping uses decoded address bytes, and
// credit-account slice order drives every update; the maps are lookup-only and
// are never iterated.
func (gs *GenesisState) repairCreditAccountDerivedStateForImport(repairLegacyReservations bool) error {
	activeCounts := make(map[string]uint64)
	pendingCounts := make(map[string]uint64)
	knownReservationCoins := make(map[string][]sdk.Coin)
	hasLiveLegacy := make(map[string]bool)
	for i := range gs.Leases {
		lease := &gs.Leases[i]
		if lease.State != LEASE_STATE_ACTIVE && lease.State != LEASE_STATE_PENDING {
			continue
		}
		if lease.Tenant == "" {
			return ErrInvalidLease.Wrapf("lease %s has empty tenant", lease.Uuid)
		}

		tenant, err := sdk.AccAddressFromBech32(lease.Tenant)
		if err != nil {
			return ErrInvalidLease.Wrapf("lease %s has invalid tenant address: %s", lease.Uuid, err)
		}
		tenantKey := string(tenant.Bytes())
		if lease.State == LEASE_STATE_ACTIVE {
			activeCounts[tenantKey]++
		} else {
			pendingCounts[tenantKey]++
		}

		if !repairLegacyReservations {
			continue
		}
		if lease.MinLeaseDurationAtCreation == 0 {
			hasLiveLegacy[tenantKey] = true
			continue
		}
		reservation, err := CalculateLeaseReservation(
			lease.Items,
			lease.MinLeaseDurationAtCreation,
		)
		if err != nil {
			return errorsmod.Wrapf(err, "calculate billing lease %q import reservation", lease.Uuid)
		}
		knownReservationCoins[tenantKey] = append(
			knownReservationCoins[tenantKey],
			reservation...,
		)
	}

	for i := range gs.CreditAccounts {
		account := &gs.CreditAccounts[i]
		if account.Tenant == "" {
			return ErrInvalidCreditOperation.Wrap("credit account has empty tenant")
		}
		tenant, err := sdk.AccAddressFromBech32(account.Tenant)
		if err != nil {
			return ErrInvalidCreditOperation.Wrapf("credit account has invalid tenant address: %s", err)
		}
		tenantKey := string(tenant.Bytes())
		account.ActiveLeaseCount = activeCounts[tenantKey]
		account.PendingLeaseCount = pendingCounts[tenantKey]

		if !repairLegacyReservations {
			continue
		}
		knownFloor, err := SafeAggregateCoins(knownReservationCoins[tenantKey])
		if err != nil {
			return errorsmod.Wrapf(err, "sum known import reservations for tenant %s", account.Tenant)
		}
		account.ReservedAmounts, err = ReconcilePreV4ReservationAggregate(
			account.ReservedAmounts,
			knownFloor,
			hasLiveLegacy[tenantKey],
		)
		if err != nil {
			return ErrInvalidCreditOperation.Wrapf(
				"repair credit account for %s reserved_amounts: %s",
				account.Tenant,
				err,
			)
		}
	}

	return nil
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
	liveDomainClaims := make(map[string]struct {
		leaseUUID   string
		serviceName string
	})
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
			if err := ValidateLeaseItemPricing(item.LockedPrice, item.Quantity); err != nil {
				return ErrInvalidLease.Wrapf("lease %s item %d has invalid pricing: %s", lease.Uuid, i, err)
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
			if lease.State == LEASE_STATE_PENDING || lease.State == LEASE_STATE_ACTIVE {
				if previous, exists := liveDomainClaims[item.CustomDomain]; exists {
					return ErrCustomDomainAlreadyClaimed.Wrapf(
						"custom_domain %q on lease %s item %q is already claimed by lease %s item %q",
						item.CustomDomain,
						lease.Uuid,
						item.ServiceName,
						previous.leaseUUID,
						previous.serviceName,
					)
				}
				liveDomainClaims[item.CustomDomain] = struct {
					leaseUUID   string
					serviceName string
				}{leaseUUID: lease.Uuid, serviceName: item.ServiceName}
			}
		}

		switch lease.State {
		case LEASE_STATE_PENDING,
			LEASE_STATE_ACTIVE,
			LEASE_STATE_CLOSED,
			LEASE_STATE_REJECTED,
			LEASE_STATE_EXPIRED:
			// Valid persisted states.
		case LEASE_STATE_UNSPECIFIED:
			return ErrInvalidLease.Wrapf("lease %s has unspecified state", lease.Uuid)
		default:
			return ErrInvalidLease.Wrapf("lease %s has unknown state %d", lease.Uuid, lease.State)
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
		if err := validateCanonicalCoins(ca.ReservedAmounts); err != nil {
			return ErrInvalidCreditOperation.Wrapf(
				"credit account for %s has invalid reserved_amounts: %s",
				ca.Tenant,
				err,
			)
		}
		if err := validateCanonicalCoins(ca.UnattributedReservedAmounts); err != nil {
			return ErrInvalidCreditOperation.Wrapf(
				"credit account for %s has invalid unattributed_reserved_amounts: %s",
				ca.Tenant,
				err,
			)
		}

		// Balance is tracked in bank module, no validation needed here
	}

	legacyReservationState, err := gs.HasLegacyReservationState()
	if err != nil {
		return err
	}
	if legacyReservationState {
		err = gs.validateLegacyReservationState(
			options,
			leaseTenantKeys,
			creditAccountTenantKeys,
			seenTenants,
		)
	} else {
		err = gs.validateConsumableReservationState(
			leaseTenantKeys,
			creditAccountTenantKeys,
			seenTenants,
		)
	}
	if err != nil {
		return err
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

// HasLegacyReservationState reports whether the genesis uses the aggregate-only
// reservation representation written before billing consensus version 4.
// Presence of Lease.reservation is the wire-compatible format marker: all
// leases must either omit it or include it, even when a tranche is empty.
func (gs *GenesisState) HasLegacyReservationState() (bool, error) {
	hasLegacyLease := false
	hasConsumableLease := false
	for i := range gs.Leases {
		if gs.Leases[i].Reservation == nil {
			hasLegacyLease = true
		} else {
			hasConsumableLease = true
		}
	}
	if hasLegacyLease && hasConsumableLease {
		return false, ErrReservationInvariant.Wrap(
			"genesis mixes leases with legacy and consumable reservation state",
		)
	}
	if !hasLegacyLease {
		return false, nil
	}

	for i := range gs.CreditAccounts {
		if !gs.CreditAccounts[i].UnattributedReservedAmounts.IsZero() {
			return false, ErrReservationInvariant.Wrapf(
				"legacy reservation state for tenant %s carries unattributed_reserved_amounts",
				gs.CreditAccounts[i].Tenant,
			)
		}
		if gs.CreditAccounts[i].UnattributedLeaseCount != 0 {
			return false, ErrReservationInvariant.Wrapf(
				"legacy reservation state for tenant %s carries unattributed_lease_count %d",
				gs.CreditAccounts[i].Tenant,
				gs.CreditAccounts[i].UnattributedLeaseCount,
			)
		}
	}
	return true, nil
}

// validateLegacyReservationState preserves the import contract for pre-v4
// aggregate-only (v2/v3) exports, where only the account aggregate was stored.
// Exact per-lease amounts are established by InitGenesis before this state is
// persisted in v4 format.
func (gs *GenesisState) validateLegacyReservationState(
	options genesisValidationOptions,
	leaseTenantKeys,
	creditAccountTenantKeys []string,
	seenTenants map[string]bool,
) error {
	expectedReservationCoins := make(map[string][]sdk.Coin)
	legacyReservationTenants := make(map[string]bool)
	knownReservationCoins := make(map[string][]sdk.Coin)
	reservationTenantOrder := make([]string, 0)
	seenReservationTenants := make(map[string]bool)
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
		if !seenReservationTenants[tenantKey] {
			seenReservationTenants[tenantKey] = true
			reservationTenantOrder = append(reservationTenantOrder, tenantKey)
		}

		reservation, err := GetLeaseReservationAmount(lease, gs.Params.MinLeaseDuration)
		if err != nil {
			return errorsmod.Wrapf(err, "calculate reservation for lease %s", lease.Uuid)
		}
		expectedReservationCoins[tenantKey] = append(
			expectedReservationCoins[tenantKey],
			reservation...,
		)

		if !options.enforceExactLegacyReservations && lease.MinLeaseDurationAtCreation != 0 {
			knownReservationCoins[tenantKey] = append(
				knownReservationCoins[tenantKey],
				reservation...,
			)
		}
	}

	// Check each credit account's reserved_amounts matches expected
	for i := range gs.CreditAccounts {
		ca := gs.CreditAccounts[i]
		tenantKey := creditAccountTenantKeys[i]
		actualNormalized := sdk.NewCoins(ca.ReservedAmounts...)
		if legacyReservationTenants[tenantKey] {
			known, err := SafeAggregateCoins(knownReservationCoins[tenantKey])
			if err != nil {
				return errorsmod.Wrapf(err, "sum known reservations for tenant %s", tenantKey)
			}
			if !actualNormalized.IsAllGTE(known) {
				return ErrInvalidCreditOperation.Wrapf(
					"credit account for %s has reserved_amounts %s below known non-legacy lease reservations %s",
					ca.Tenant, actualNormalized.String(), known.String(),
				)
			}
			continue
		}

		expected, err := SafeAggregateCoins(expectedReservationCoins[tenantKey])
		if err != nil {
			return errorsmod.Wrapf(err, "sum reservations for tenant %s", tenantKey)
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

	// Sort the explicitly collected keys rather than ranging over a Go map in a
	// consensus validation path.
	slices.Sort(reservationTenantOrder)
	for _, tenant := range reservationTenantOrder {
		expected, err := SafeAggregateCoins(expectedReservationCoins[tenant])
		if err != nil {
			return errorsmod.Wrapf(err, "sum reservations for tenant %s", tenant)
		}
		if !expected.IsZero() && !seenTenants[tenant] {
			return ErrInvalidCreditOperation.Wrapf(
				"tenant %s has lease reservations totaling %s but no credit account",
				tenant, expected.String(),
			)
		}
	}
	return nil
}

// validateConsumableReservationState enforces the exact v4 accounting model:
// each modern live lease owns its remaining tranche, historical live leases
// share only an explicit unattributed cohort, and the account aggregate equals
// the sum of those claims.
func (gs *GenesisState) validateConsumableReservationState(
	leaseTenantKeys,
	creditAccountTenantKeys []string,
	seenTenants map[string]bool,
) error {
	modernReservationCoins := make(map[string][]sdk.Coin)
	legacyLeaseCounts := make(map[string]uint64)
	liveTenantOrder := make([]string, 0)
	seenLiveTenants := make(map[string]bool)

	for i := range gs.Leases {
		lease := &gs.Leases[i]
		tenantKey := leaseTenantKeys[i]
		remaining, err := SafeAddCoins(sdk.NewCoins(), lease.Reservation.RemainingAmounts)
		if err != nil {
			return ErrReservationInvariant.Wrapf(
				"lease %s has invalid remaining reservation: %s",
				lease.Uuid, err,
			)
		}

		live := lease.State == LEASE_STATE_PENDING || lease.State == LEASE_STATE_ACTIVE
		if !live {
			if !remaining.IsZero() {
				return ErrReservationInvariant.Wrapf(
					"terminal lease %s has remaining reservation %s",
					lease.Uuid, remaining.String(),
				)
			}
			continue
		}
		if !seenLiveTenants[tenantKey] {
			seenLiveTenants[tenantKey] = true
			liveTenantOrder = append(liveTenantOrder, tenantKey)
		}

		if lease.MinLeaseDurationAtCreation == 0 {
			if !remaining.IsZero() {
				return ErrReservationInvariant.Wrapf(
					"legacy lease %s has attributed reservation %s",
					lease.Uuid, remaining.String(),
				)
			}
			if legacyLeaseCounts[tenantKey] == math.MaxUint64 {
				return ErrReservationInvariant.Wrapf(
					"live legacy lease count overflows uint64 for tenant %s", tenantKey,
				)
			}
			legacyLeaseCounts[tenantKey]++
			continue
		}

		nominal, err := GetLeaseReservationAmount(lease, gs.Params.MinLeaseDuration)
		if err != nil {
			return errorsmod.Wrapf(err, "calculate nominal reservation for lease %s", lease.Uuid)
		}
		if lease.State == LEASE_STATE_PENDING && !remaining.Equal(nominal) {
			return ErrReservationInvariant.Wrapf(
				"pending lease %s has remaining reservation %s, expected %s",
				lease.Uuid, remaining.String(), nominal.String(),
			)
		}
		if _, err := SafeSubtractCoins(nominal, remaining); err != nil {
			return ErrReservationInvariant.Wrapf(
				"lease %s remaining reservation %s exceeds nominal reservation %s: %s",
				lease.Uuid, remaining.String(), nominal.String(), err,
			)
		}

		modernReservationCoins[tenantKey] = append(
			modernReservationCoins[tenantKey],
			remaining...,
		)
	}

	for i := range gs.CreditAccounts {
		ca := &gs.CreditAccounts[i]
		tenantKey := creditAccountTenantKeys[i]
		expectedLegacyCount := legacyLeaseCounts[tenantKey]
		if ca.UnattributedLeaseCount != expectedLegacyCount {
			return ErrReservationInvariant.Wrapf(
				"credit account for %s has unattributed_lease_count %d but has %d live legacy leases",
				ca.Tenant, ca.UnattributedLeaseCount, expectedLegacyCount,
			)
		}
		if expectedLegacyCount == 0 && !ca.UnattributedReservedAmounts.IsZero() {
			return ErrReservationInvariant.Wrapf(
				"credit account for %s has unattributed reservation %s without a live legacy lease",
				ca.Tenant, ca.UnattributedReservedAmounts.String(),
			)
		}

		modernReservations, err := SafeAggregateCoins(modernReservationCoins[tenantKey])
		if err != nil {
			return errorsmod.Wrapf(err, "sum attributed reservations for tenant %s", tenantKey)
		}
		expected, err := SafeAddCoins(modernReservations, ca.UnattributedReservedAmounts)
		if err != nil {
			return errorsmod.Wrapf(err, "sum reservations for tenant %s", tenantKey)
		}
		actual := sdk.NewCoins(ca.ReservedAmounts...)
		if !actual.Equal(expected) {
			return ErrReservationInvariant.Wrapf(
				"credit account for %s has reserved_amounts %s but consumable reservations sum to %s",
				ca.Tenant, actual.String(), expected.String(),
			)
		}
	}

	slices.Sort(liveTenantOrder)
	for _, tenant := range liveTenantOrder {
		if !seenTenants[tenant] {
			return ErrReservationInvariant.Wrapf(
				"tenant %s has live reservation claims but no credit account",
				tenant,
			)
		}
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
		if ca.ActiveLeaseCount > MaxActiveLeasesPerTenantStateUpperBound {
			return ErrInvalidCreditOperation.Wrapf(
				"credit account for %s has active_lease_count %d above hard upper bound %d",
				ca.Tenant, ca.ActiveLeaseCount, MaxActiveLeasesPerTenantStateUpperBound,
			)
		}
		if ca.PendingLeaseCount > MaxPendingLeasesPerTenantUpperBound {
			return ErrInvalidCreditOperation.Wrapf(
				"credit account for %s has pending_lease_count %d above hard upper bound %d",
				ca.Tenant, ca.PendingLeaseCount, MaxPendingLeasesPerTenantUpperBound,
			)
		}
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
