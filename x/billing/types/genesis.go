package types

import (
	"cmp"
	"maps"
	"slices"
	"time"

	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	pkguuid "github.com/manifest-network/manifest-ledger/pkg/uuid"
)

// streamRemainderLimit is the exclusive upper bound of StreamState.Remainder:
// one whole unit of the denom, expressed in (denom units × nanoseconds).
var streamRemainderLimit = sdkmath.NewInt(1_000_000_000)

// DefaultGenesis returns the default genesis state.
func DefaultGenesis() *GenesisState {
	return &GenesisState{
		Params:         DefaultParams(),
		Leases:         []Lease{},
		CreditAccounts: []CreditAccount{},
		Streams:        []Stream{},
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

		// Defensive: any item carrying a custom_domain must (1) be a valid FQDN,
		// (2) not match a reserved provider suffix from gs.Params, and (3) be
		// uniquely addressable by its service_name (specifically: a multi-item
		// legacy lease cannot host custom_domains because the lookup would be
		// ambiguous). Reject malformed genesis up front so the chain cannot
		// start in a state the msg layer would refuse.
		for i, item := range lease.Items {
			if item.CustomDomain == "" {
				continue
			}
			if err := IsValidFQDN(item.CustomDomain); err != nil {
				return ErrInvalidCustomDomain.Wrapf("lease %s item %d: %s", lease.Uuid, i, err)
			}
			if MatchesReservedSuffix(item.CustomDomain, gs.Params.ReservedDomainSuffixes) {
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

	// Validate lease_sequence: must be >= number of leases to prevent UUID
	// collisions after genesis import. Each CreateLease call advances the
	// sequence, so a valid export always has sequence >= len(leases).
	if uint64(len(gs.Leases)) > gs.LeaseSequence {
		return ErrInvalidLease.Wrapf(
			"lease_sequence %d is less than number of leases %d",
			gs.LeaseSequence, len(gs.Leases),
		)
	}

	return gs.ValidateStreams()
}

// ValidateStreams validates everything in the genesis that concerns settlement
// streams: no lease may still carry the deprecated last_settled_at, and the
// streams must be exactly the ones the ACTIVE leases imply, with the rate and
// lease count they imply.
//
// It is part of Validate and is also run by the keeper's InitGenesis, because
// Validate itself is not run on InitChain and a malformed stream moves money:
// a stream without last_settled_at, for example, would look centuries overdue
// and pay the tenant's whole credit to the provider on its first settlement.
func (gs *GenesisState) ValidateStreams() error {
	// last_settled_at is deprecated: settlement is tracked per stream. A
	// genesis that still carries it was exported before stream settlement and
	// cannot be imported as is.
	for i := range gs.Leases {
		if gs.Leases[i].LastSettledAt != nil { //nolint:staticcheck // the deprecated field must be read to be rejected
			return ErrInvalidLease.Wrapf(
				"lease %s has last_settled_at set: the field is deprecated, settlement is tracked in streams",
				gs.Leases[i].Uuid,
			)
		}
	}

	expected, err := ExpectedStreamAggregates(gs.Leases)
	if err != nil {
		return ErrInvalidLease.Wrap(err.Error())
	}

	seen := make(map[StreamID]bool, len(gs.Streams))
	for _, stream := range gs.Streams {
		id := stream.ID()

		if _, err := sdk.AccAddressFromBech32(stream.Tenant); err != nil {
			return ErrInvalidLease.Wrapf("stream %v has invalid tenant address: %s", id, err)
		}
		if seen[id] {
			return ErrInvalidLease.Wrapf("duplicate stream %v", id)
		}
		seen[id] = true

		if stream.State.Rate.IsNil() || !stream.State.Rate.IsPositive() {
			return ErrInvalidLease.Wrapf("stream %v has non-positive rate", id)
		}
		if stream.State.LastSettledAt.IsZero() {
			return ErrInvalidLease.Wrapf("stream %v has no last_settled_at timestamp", id)
		}
		// The remainder is what a settlement could not pay as a whole unit: it
		// is the rest of a division by 1e9 (see StreamState.remainder).
		if r := stream.State.Remainder; !r.IsNil() && (r.IsNegative() || r.GTE(streamRemainderLimit)) {
			return ErrInvalidLease.Wrapf("stream %v has remainder %s outside [0, %s)", id, r, streamRemainderLimit)
		}

		want, ok := expected[id]
		if !ok {
			return ErrInvalidLease.Wrapf("stream %v has no active lease", id)
		}
		if !stream.State.Rate.Equal(want.Rate) {
			return ErrInvalidLease.Wrapf(
				"stream %v has rate %s but its active leases sum to %s",
				id, stream.State.Rate, want.Rate,
			)
		}
		if stream.State.LeaseCount != want.LeaseCount {
			return ErrInvalidLease.Wrapf(
				"stream %v has lease_count %d but has %d active leases",
				id, stream.State.LeaseCount, want.LeaseCount,
			)
		}
	}

	// Every stream the leases imply must be present. Report the smallest
	// missing one so the error is the same on every run.
	missing := make([]StreamID, 0)
	for id := range expected {
		if !seen[id] {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		slices.SortFunc(missing, func(a, b StreamID) int {
			return cmp.Or(
				cmp.Compare(a.ProviderUUID, b.ProviderUUID),
				cmp.Compare(a.Tenant, b.Tenant),
				cmp.Compare(a.Denom, b.Denom),
			)
		})
		return ErrInvalidLease.Wrapf("active leases imply stream %v but it is missing", missing[0])
	}

	return nil
}

// ValidateWithBlockTime performs additional genesis state validation that requires block time.
// This is called during InitGenesis when block time is available.
// It validates that lease and stream timestamps are not in the future relative to block time.
func (gs *GenesisState) ValidateWithBlockTime(blockTime time.Time) error {
	for _, stream := range gs.Streams {
		// A stream settled in the future would accrue nothing until block time
		// catches up, giving the tenant free service.
		if stream.State.LastSettledAt.After(blockTime) {
			return ErrInvalidLease.Wrapf(
				"stream %v has last_settled_at (%s) in the future relative to block time (%s)",
				stream.ID(),
				stream.State.LastSettledAt.String(),
				blockTime.String(),
			)
		}
		if stream.State.ExhaustedAt != nil && stream.State.ExhaustedAt.After(blockTime) {
			return ErrInvalidLease.Wrapf(
				"stream %v has exhausted_at (%s) in the future relative to block time (%s)",
				stream.ID(),
				stream.State.ExhaustedAt.String(),
				blockTime.String(),
			)
		}
	}

	for _, lease := range gs.Leases {
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
