package keeper

import (
	"bytes"
	"cmp"
	"fmt"
	"math"
	"slices"
	"time"

	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

const (
	// ReservationPreflightStatePreV4 identifies aggregate-only billing state
	// that the v3→v4 migration will convert to consumable reservations.
	ReservationPreflightStatePreV4 = "pre_v4_aggregate"
	// ReservationPreflightStateV4 identifies billing state that already has
	// consumable per-lease reservations and therefore will not be migrated.
	ReservationPreflightStateV4 = "consumable_v4"
)

// ReservationMigrationPreflight is the deterministic, reservation-specific
// result of applying the billing cutover planner to exported module state. It
// deliberately excludes source-document provenance, which belongs to the CLI
// envelope, and does not claim to preview unrelated InitGenesis validation.
// It is operator tooling, not consensus state or a wire-protocol type.
type ReservationMigrationPreflight struct {
	BillingState                     string                                `json:"billing_state"`
	ReservationChangeTenantCount     uint64                                `json:"reservation_change_tenant_count"`
	ExpiringModernPendingTenantCount uint64                                `json:"expiring_modern_pending_tenant_count"`
	ExpiringModernPendingLeaseCount  uint64                                `json:"expiring_modern_pending_lease_count"`
	Tenants                          []ReservationMigrationTenantPreflight `json:"tenants"`
}

// ReservationMigrationTenantPreflight describes one credit account. Tenants
// are ordered by decoded address bytes; ACTIVE entries and both UUID slices are
// ordered lexicographically.
type ReservationMigrationTenantPreflight struct {
	Tenant                          string                                `json:"tenant"`
	CreditAddress                   string                                `json:"credit_address"`
	HasPlannedReservationChange     bool                                  `json:"has_planned_reservation_change"`
	Denominations                   []ReservationMigrationDenomPreflight  `json:"denominations"`
	ModernActiveLeases              []ReservationMigrationActivePreflight `json:"modern_active_leases"`
	ModernPendingLeaseUUIDs         []string                              `json:"modern_pending_lease_uuids"`
	ExpiringModernPendingLeaseUUIDs []string                              `json:"expiring_modern_pending_lease_uuids"`
}

// ReservationMigrationActivePreflight reports one modern ACTIVE lease's
// nominal creation-time claim and the remaining reservation the cutover will
// assign. Entries are ordered lexicographically by lease UUID.
type ReservationMigrationActivePreflight struct {
	LeaseUUID               string                                `json:"lease_uuid"`
	NominalAmounts          []ReservationMigrationAmountPreflight `json:"nominal_amounts"`
	PlannedRemainingAmounts []ReservationMigrationAmountPreflight `json:"planned_remaining_amounts"`
}

// ReservationMigrationAmountPreflight is a precision-safe denomination amount.
type ReservationMigrationAmountPreflight struct {
	Denom  string `json:"denom"`
	Amount string `json:"amount"`
}

// ReservationMigrationDenomPreflight reports source, planner-input, and planned
// aggregate accounting for one relevant denomination. Amounts are base-10
// integer strings so JSON consumers do not lose precision.
type ReservationMigrationDenomPreflight struct {
	Denom                              string `json:"denom"`
	SourceReservationAggregate         string `json:"source_reservation_aggregate"`
	PreCutoverReservationAggregate     string `json:"pre_cutover_reservation_aggregate"`
	PostCutoverReservationAggregate    string `json:"post_cutover_reservation_aggregate"`
	PreCutoverUnattributedReservation  string `json:"pre_cutover_unattributed_reservation"`
	PostCutoverUnattributedReservation string `json:"post_cutover_unattributed_reservation"`
	BankBalance                        string `json:"bank_balance"`
	ModernPendingRequired              string `json:"modern_pending_required"`
	ModernPendingShortfall             string `json:"modern_pending_shortfall"`
}

type reservationMigrationPreflightAccount struct {
	tenant                sdk.AccAddress
	creditAddress         sdk.AccAddress
	sourceReservedAmounts sdk.Coins
	account               types.CreditAccount
	records               []reservationMigrationLease
}

// BuildReservationMigrationPreflight applies the production reservation
// planner to an exported billing and bank genesis without mutating either input
// or writing state. The narrow exported API exists for offline operator tooling;
// consensus code continues to call the unexported planner directly.
//
// Pre-v4 input first goes through the same import preparation that mirrors the
// v2→v3 derived-state repair. Already-v4 input is not replanned and fails if
// its reservation aggregate is not fully bank-backed. An exported genesis does
// not carry collection indexes, so this preview assumes the source node's
// indexes agree with the exported primary records.
func BuildReservationMigrationPreflight(
	plannerTime time.Time,
	billingGenesis *types.GenesisState,
	bankGenesis *banktypes.GenesisState,
) (ReservationMigrationPreflight, error) {
	report := ReservationMigrationPreflight{
		Tenants: []ReservationMigrationTenantPreflight{},
	}
	if plannerTime.IsZero() {
		return report, fmt.Errorf("billing reservation migration preflight requires a non-zero planner time")
	}
	if billingGenesis == nil {
		return report, fmt.Errorf("billing reservation migration preflight requires billing genesis state")
	}
	if bankGenesis == nil {
		return report, fmt.Errorf("billing reservation migration preflight requires bank genesis state")
	}
	if err := bankGenesis.Validate(); err != nil {
		return report, fmt.Errorf("validate bank genesis for billing reservation migration preflight: %w", err)
	}

	legacy, err := billingGenesis.HasLegacyReservationState()
	if err != nil {
		return report, fmt.Errorf("detect billing reservation format for migration preflight: %w", err)
	}
	prepared, err := billingGenesis.PrepareForImport()
	if err != nil {
		return report, fmt.Errorf("prepare billing genesis for reservation migration preflight: %w", err)
	}
	if legacy {
		report.BillingState = ReservationPreflightStatePreV4
	} else {
		report.BillingState = ReservationPreflightStateV4
	}

	bankBalancesByAddress := make(map[string]sdk.Coins, len(bankGenesis.Balances))
	for index := range bankGenesis.Balances {
		balance := &bankGenesis.Balances[index]
		address, err := sdk.AccAddressFromBech32(balance.Address)
		if err != nil {
			return report, fmt.Errorf("decode bank balance address %q: %w", balance.Address, err)
		}
		addressKey := string(address.Bytes())
		if _, exists := bankBalancesByAddress[addressKey]; exists {
			return report, fmt.Errorf(
				"bank genesis contains duplicate decoded address identity %q",
				address.String(),
			)
		}
		coins, err := types.SafeAddCoins(sdk.NewCoins(), balance.Coins)
		if err != nil {
			return report, fmt.Errorf("normalize bank balance for %q: %w", address.String(), err)
		}
		bankBalancesByAddress[addressKey] = coins
	}

	liveLeasesByTenant := make(map[string][]reservationMigrationLease)
	for index := range prepared.Leases {
		lease := prepared.Leases[index]
		if !isLiveLeaseState(lease.State) {
			continue
		}
		tenant, err := sdk.AccAddressFromBech32(lease.Tenant)
		if err != nil {
			return report, fmt.Errorf("decode live billing lease %q tenant: %w", lease.Uuid, err)
		}
		tenantKey := string(tenant.Bytes())
		liveLeasesByTenant[tenantKey] = append(
			liveLeasesByTenant[tenantKey],
			reservationMigrationLease{value: lease},
		)
	}

	accounts := make([]reservationMigrationPreflightAccount, 0, len(prepared.CreditAccounts))
	for index := range prepared.CreditAccounts {
		account := prepared.CreditAccounts[index]
		sourceAccount := billingGenesis.CreditAccounts[index]
		tenant, err := sdk.AccAddressFromBech32(account.Tenant)
		if err != nil {
			return report, fmt.Errorf("decode billing credit-account tenant %q: %w", account.Tenant, err)
		}
		creditAddress, err := sdk.AccAddressFromBech32(account.CreditAddress)
		if err != nil {
			return report, fmt.Errorf(
				"decode billing credit address for tenant %q: %w",
				tenant.String(),
				err,
			)
		}
		records := slices.Clone(liveLeasesByTenant[string(tenant.Bytes())])
		slices.SortFunc(records, func(left, right reservationMigrationLease) int {
			return cmp.Compare(left.value.Uuid, right.value.Uuid)
		})
		accounts = append(accounts, reservationMigrationPreflightAccount{
			tenant:                tenant,
			creditAddress:         creditAddress,
			sourceReservedAmounts: sourceAccount.ReservedAmounts,
			account:               account,
			records:               records,
		})
	}
	slices.SortFunc(accounts, func(left, right reservationMigrationPreflightAccount) int {
		return bytes.Compare(left.tenant, right.tenant)
	})

	for index := range accounts {
		entry := &accounts[index]
		sourceAggregate, err := types.SafeAddCoins(
			sdk.NewCoins(),
			entry.sourceReservedAmounts,
		)
		if err != nil {
			return report, fmt.Errorf(
				"normalize source billing reservations for tenant %q: %w",
				entry.tenant.String(),
				err,
			)
		}
		oldAggregate, err := types.SafeAddCoins(sdk.NewCoins(), entry.account.ReservedAmounts)
		if err != nil {
			return report, fmt.Errorf(
				"normalize billing reservations for tenant %q: %w",
				entry.tenant.String(),
				err,
			)
		}
		allBankBalances := bankBalancesByAddress[string(entry.creditAddress.Bytes())]
		bankBalances, err := reservationMigrationBankBalancesForAggregate(
			entry.creditAddress,
			oldAggregate,
			func(denom string) sdk.Coin {
				return sdk.NewCoin(denom, allBankBalances.AmountOf(denom))
			},
		)
		if err != nil {
			return report, err
		}

		planned := entry.records
		postAggregate := oldAggregate
		legacyAllocation, err := types.SafeAddCoins(
			sdk.NewCoins(),
			entry.account.UnattributedReservedAmounts,
		)
		if err != nil {
			return report, fmt.Errorf(
				"normalize unattributed reservations for tenant %q: %w",
				entry.tenant.String(),
				err,
			)
		}
		if legacy {
			planned, postAggregate, legacyAllocation, err = planConsumableReservationCutover(
				plannerTime,
				oldAggregate,
				bankBalances,
				entry.records,
			)
			if err != nil {
				return report, types.ErrReservationInvariant.Wrapf(
					"preflight consumable reservations for tenant %q: %s",
					entry.tenant.String(),
					err,
				)
			}
		} else if _, err := types.SafeSubtractCoins(bankBalances, oldAggregate); err != nil {
			return report, types.ErrReservationInvariant.Wrapf(
				"consumable v4 billing state for tenant %q is under-backed: bank %s, reservations %s: %s",
				entry.tenant.String(),
				bankBalances.String(),
				oldAggregate.String(),
				err,
			)
		}

		pending, pendingUUIDs, expiringUUIDs, err := reservationMigrationPendingPreflight(
			legacy,
			entry.records,
			planned,
		)
		if err != nil {
			return report, fmt.Errorf(
				"build modern PENDING preflight for tenant %q: %w",
				entry.tenant.String(),
				err,
			)
		}
		activeLeases, activeAllocationChanged, err := reservationMigrationActivePreflights(
			legacy,
			entry.records,
			planned,
		)
		if err != nil {
			return report, fmt.Errorf(
				"build modern ACTIVE preflight for tenant %q: %w",
				entry.tenant.String(),
				err,
			)
		}
		preCutoverUnattributed, err := reservationMigrationPreCutoverUnattributedClaim(
			legacy,
			oldAggregate,
			entry.records,
			planned,
		)
		if err != nil {
			return report, fmt.Errorf(
				"build pre-cutover unattributed reservation for tenant %q: %w",
				entry.tenant.String(),
				err,
			)
		}
		if !legacy {
			preCutoverUnattributed = legacyAllocation
		}
		denominations, err := reservationMigrationDenomPreflights(
			sourceAggregate,
			oldAggregate,
			postAggregate,
			preCutoverUnattributed,
			legacyAllocation,
			allBankBalances,
			pending,
		)
		if err != nil {
			return report, fmt.Errorf(
				"build denomination preflight for tenant %q: %w",
				entry.tenant.String(),
				err,
			)
		}

		hasPlannedReservationChange := legacy && (!sourceAggregate.Equal(oldAggregate) ||
			!oldAggregate.Equal(postAggregate) ||
			activeAllocationChanged ||
			!preCutoverUnattributed.Equal(legacyAllocation) ||
			len(expiringUUIDs) != 0)
		if hasPlannedReservationChange {
			if report.ReservationChangeTenantCount == math.MaxUint64 {
				return report, types.ErrArithmeticOverflow.Wrap(
					"count preflight tenants with planned reservation changes",
				)
			}
			report.ReservationChangeTenantCount++
		}
		if len(expiringUUIDs) != 0 {
			if report.ExpiringModernPendingTenantCount == math.MaxUint64 {
				return report, types.ErrArithmeticOverflow.Wrap(
					"count preflight tenants with expiring modern PENDING leases",
				)
			}
			report.ExpiringModernPendingTenantCount++
			expiringCount := uint64(len(expiringUUIDs))
			if math.MaxUint64-report.ExpiringModernPendingLeaseCount < expiringCount {
				return report, types.ErrArithmeticOverflow.Wrap("count expiring preflight leases")
			}
			report.ExpiringModernPendingLeaseCount += expiringCount
		}
		report.Tenants = append(report.Tenants, ReservationMigrationTenantPreflight{
			Tenant:                          entry.tenant.String(),
			CreditAddress:                   entry.creditAddress.String(),
			HasPlannedReservationChange:     hasPlannedReservationChange,
			Denominations:                   denominations,
			ModernActiveLeases:              activeLeases,
			ModernPendingLeaseUUIDs:         pendingUUIDs,
			ExpiringModernPendingLeaseUUIDs: expiringUUIDs,
		})
	}

	return report, nil
}

func reservationMigrationPendingPreflight(
	legacy bool,
	original,
	planned []reservationMigrationLease,
) (sdk.Coins, []string, []string, error) {
	if len(original) != len(planned) {
		return nil, nil, nil, types.ErrReservationInvariant.Wrap(
			"reservation migration preflight planner changed record cardinality",
		)
	}

	entries := make([]sdk.Coin, 0)
	pendingUUIDs := make([]string, 0)
	expiringUUIDs := make([]string, 0)
	for index := range original {
		lease := &original[index].value
		if lease.State != types.LEASE_STATE_PENDING || lease.MinLeaseDurationAtCreation == 0 {
			continue
		}
		pendingUUIDs = append(pendingUUIDs, lease.Uuid)
		if legacy {
			entries = append(entries, planned[index].nominal...)
			if planned[index].stateChanged {
				expiringUUIDs = append(expiringUUIDs, lease.Uuid)
			}
			continue
		}
		if lease.Reservation == nil {
			return nil, nil, nil, types.ErrReservationInvariant.Wrapf(
				"consumable v4 PENDING lease %q has no reservation wrapper",
				lease.Uuid,
			)
		}
		entries = append(entries, lease.Reservation.RemainingAmounts...)
	}
	slices.Sort(pendingUUIDs)
	slices.Sort(expiringUUIDs)
	pending, err := types.SafeAggregateCoins(entries)
	if err != nil {
		return nil, nil, nil, err
	}
	return pending, pendingUUIDs, expiringUUIDs, nil
}

func reservationMigrationActivePreflights(
	legacy bool,
	original,
	planned []reservationMigrationLease,
) ([]ReservationMigrationActivePreflight, bool, error) {
	if len(original) != len(planned) {
		return nil, false, types.ErrReservationInvariant.Wrap(
			"reservation migration preflight planner changed record cardinality",
		)
	}

	activeLeases := make([]ReservationMigrationActivePreflight, 0)
	allocationChanged := false
	for index := range original {
		lease := &original[index].value
		if lease.State != types.LEASE_STATE_ACTIVE || lease.MinLeaseDurationAtCreation == 0 {
			continue
		}

		nominal := planned[index].nominal
		allocation := planned[index].allocation
		if !legacy {
			var err error
			nominal, err = types.CalculateLeaseReservation(
				lease.Items,
				lease.MinLeaseDurationAtCreation,
			)
			if err != nil {
				return nil, false, fmt.Errorf(
					"calculate billing lease %q nominal reservation: %w",
					lease.Uuid,
					err,
				)
			}
			if lease.Reservation == nil {
				return nil, false, types.ErrReservationInvariant.Wrapf(
					"consumable v4 ACTIVE lease %q has no reservation wrapper",
					lease.Uuid,
				)
			}
			allocation, err = types.SafeAddCoins(
				sdk.NewCoins(),
				lease.Reservation.RemainingAmounts,
			)
			if err != nil {
				return nil, false, fmt.Errorf(
					"normalize billing lease %q remaining reservation: %w",
					lease.Uuid,
					err,
				)
			}
		}
		if !nominal.Equal(allocation) {
			allocationChanged = true
		}

		activeLeases = append(activeLeases, ReservationMigrationActivePreflight{
			LeaseUUID:               lease.Uuid,
			NominalAmounts:          reservationMigrationAmountPreflights(nominal),
			PlannedRemainingAmounts: reservationMigrationAmountPreflights(allocation),
		})
	}
	slices.SortFunc(activeLeases, func(left, right ReservationMigrationActivePreflight) int {
		return cmp.Compare(left.LeaseUUID, right.LeaseUUID)
	})
	return activeLeases, allocationChanged, nil
}

func reservationMigrationPreCutoverUnattributedClaim(
	legacy bool,
	oldAggregate sdk.Coins,
	original,
	planned []reservationMigrationLease,
) (sdk.Coins, error) {
	if !legacy {
		return sdk.NewCoins(), nil
	}

	if len(original) != len(planned) {
		return nil, types.ErrReservationInvariant.Wrap(
			"reservation migration preflight planner changed record cardinality",
		)
	}

	hasLegacyCohort := false
	modernPendingEntries := make([]sdk.Coin, 0)
	modernActiveEntries := make([]sdk.Coin, 0)
	for index := range planned {
		lease := &original[index].value
		if lease.MinLeaseDurationAtCreation == 0 {
			hasLegacyCohort = true
			continue
		}
		switch lease.State {
		case types.LEASE_STATE_PENDING:
			modernPendingEntries = append(modernPendingEntries, planned[index].nominal...)
		case types.LEASE_STATE_ACTIVE:
			modernActiveEntries = append(modernActiveEntries, planned[index].nominal...)
		}
	}
	if !hasLegacyCohort {
		return sdk.NewCoins(), nil
	}

	modernPending, err := types.SafeAggregateCoins(modernPendingEntries)
	if err != nil {
		return nil, fmt.Errorf("sum modern PENDING reservations: %w", err)
	}
	remainingHistorical, err := types.SafeSubtractCoins(oldAggregate, modernPending)
	if err != nil {
		return nil, types.ErrReservationInvariant.Wrapf(
			"old aggregate cannot back modern PENDING reservations: %s",
			err,
		)
	}
	modernActive, err := types.SafeAggregateCoins(modernActiveEntries)
	if err != nil {
		return nil, fmt.Errorf("sum modern ACTIVE reservations: %w", err)
	}
	return positiveCoinDifference(remainingHistorical, modernActive), nil
}

func reservationMigrationAmountPreflights(coins sdk.Coins) []ReservationMigrationAmountPreflight {
	amounts := make([]ReservationMigrationAmountPreflight, 0, len(coins))
	for _, coin := range coins {
		amounts = append(amounts, ReservationMigrationAmountPreflight{
			Denom:  coin.Denom,
			Amount: coin.Amount.String(),
		})
	}
	return amounts
}

func reservationMigrationDenomPreflights(
	sourceAggregate,
	preCutoverAggregate,
	postCutoverAggregate,
	preCutoverUnattributed,
	postCutoverUnattributed,
	allBankBalances,
	modernPending sdk.Coins,
) ([]ReservationMigrationDenomPreflight, error) {
	denoms := make([]string, 0,
		len(sourceAggregate)+
			len(preCutoverAggregate)+
			len(postCutoverAggregate)+
			len(preCutoverUnattributed)+
			len(postCutoverUnattributed)+
			len(modernPending),
	)
	coinSets := [...]sdk.Coins{
		sourceAggregate,
		preCutoverAggregate,
		postCutoverAggregate,
		preCutoverUnattributed,
		postCutoverUnattributed,
		modernPending,
	}
	for _, coins := range coinSets {
		for _, coin := range coins {
			denoms = append(denoms, coin.Denom)
		}
	}
	slices.Sort(denoms)
	denoms = slices.Compact(denoms)

	denominations := make([]ReservationMigrationDenomPreflight, 0, len(denoms))
	for _, denom := range denoms {
		bankBalance := allBankBalances.AmountOf(denom)
		pendingRequired := modernPending.AmountOf(denom)
		shortfall := sdkmath.ZeroInt()
		if pendingRequired.GT(bankBalance) {
			var err error
			shortfall, err = pendingRequired.SafeSub(bankBalance)
			if err != nil {
				return nil, types.ErrArithmeticOverflow.Wrapf(
					"subtract %s modern PENDING bank balance",
					denom,
				)
			}
		}
		denominations = append(denominations, ReservationMigrationDenomPreflight{
			Denom:                              denom,
			SourceReservationAggregate:         sourceAggregate.AmountOf(denom).String(),
			PreCutoverReservationAggregate:     preCutoverAggregate.AmountOf(denom).String(),
			PostCutoverReservationAggregate:    postCutoverAggregate.AmountOf(denom).String(),
			PreCutoverUnattributedReservation:  preCutoverUnattributed.AmountOf(denom).String(),
			PostCutoverUnattributedReservation: postCutoverUnattributed.AmountOf(denom).String(),
			BankBalance:                        bankBalance.String(),
			ModernPendingRequired:              pendingRequired.String(),
			ModernPendingShortfall:             shortfall.String(),
		})
	}
	return denominations, nil
}
