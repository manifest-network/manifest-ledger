package keeper

import (
	"cmp"
	"fmt"
	"math"
	"math/big"
	"slices"

	"cosmossdk.io/collections"
	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

type reservationMigrationLease struct {
	value      types.Lease
	nominal    sdk.Coins
	allocation sdk.Coins
}

type reservationMigrationClaim struct {
	recordIndex int
	uuid        string
	nominal     sdk.Coins
	allocation  sdk.Coins
}

type reservationMigrationPlan struct {
	account types.CreditAccount
	leases  []reservationMigrationLease
}

type reservationMigrationShare struct {
	claimantIndex int
	remainder     *big.Int
	allocation    sdkmath.Int
}

type reservationMigrationDenomClaims struct {
	denom  string
	total  sdkmath.Int
	claims []reservationMigrationDenomClaim
}

type reservationMigrationDenomClaim struct {
	claimantIndex int
	amount        sdkmath.Int
}

// migrateConsumableReservations performs the v3→v4 no-mint cutover. All
// collection and index iteration is ordered, and every iterator is closed
// before a value is written.
func (m Migrator) migrateConsumableReservations(ctx sdk.Context) error {
	initialized, uninitialized, err := m.reservationWrapperState(ctx)
	if err != nil {
		return err
	}
	if initialized && uninitialized {
		return types.ErrReservationInvariant.Wrap(
			"billing reservation migration found partially initialized lease state",
		)
	}
	if initialized {
		// The module VersionMap prevents a production migration from running
		// twice. A successful direct rerun is also a no-op. Upgrade execution is
		// block-atomic; this marker is not a recovery protocol for partial writes
		// performed outside the SDK migration runner.
		return nil
	}

	if err := m.migrateReservationAccounts(ctx); err != nil {
		return err
	}
	return m.initializeTerminalReservationWrappers(ctx)
}

func (m Migrator) reservationWrapperState(ctx sdk.Context) (initialized, uninitialized bool, err error) {
	iterator, err := m.keeper.Leases.Iterate(ctx, nil)
	if err != nil {
		return false, false, fmt.Errorf("iterate billing leases for reservation initialization: %w", err)
	}
	for ; iterator.Valid(); iterator.Next() {
		lease, valueErr := iterator.Value()
		if valueErr != nil {
			_ = iterator.Close()
			return false, false, fmt.Errorf("decode billing lease for reservation initialization: %w", valueErr)
		}
		if lease.Reservation == nil {
			uninitialized = true
		} else {
			initialized = true
		}
		if initialized && uninitialized {
			break
		}
	}
	if err := iterator.Close(); err != nil {
		return false, false, fmt.Errorf("close billing reservation initialization iterator: %w", err)
	}
	return initialized, uninitialized, nil
}

func (m Migrator) migrateReservationAccounts(ctx sdk.Context) error {
	var (
		lastKey  sdk.AccAddress
		hasStart bool
	)
	store := m.keeper.storeService.OpenKVStore(ctx)

	for {
		var keyRange collections.Ranger[sdk.AccAddress]
		if hasStart {
			keyRange = new(collections.Range[sdk.AccAddress]).StartExclusive(lastKey)
		}
		iterator, err := m.keeper.CreditAccounts.Iterate(ctx, keyRange)
		if err != nil {
			return fmt.Errorf("iterate billing credit accounts for reservation migration: %w", err)
		}

		entries := make([]creditAccountMigrationEntry, 0, storageMigrationPageSize)
		for iterator.Valid() && len(entries) < storageMigrationPageSize {
			entry, entryErr := iterator.KeyValue()
			if entryErr != nil {
				_ = iterator.Close()
				return fmt.Errorf("decode billing reservation account entry: %w", entryErr)
			}
			entries = append(entries, creditAccountMigrationEntry{
				key:   append(sdk.AccAddress(nil), entry.Key...),
				value: entry.Value,
			})
			iterator.Next()
		}
		hasMore := iterator.Valid()
		if err := iterator.Close(); err != nil {
			return fmt.Errorf("close billing reservation account iterator: %w", err)
		}

		for _, entry := range entries {
			plan, err := m.buildReservationMigrationPlan(ctx, entry.key, entry.value)
			if err != nil {
				return err
			}

			for _, lease := range plan.leases {
				encoded, err := m.keeper.Leases.ValueCodec().Encode(lease.value)
				if err != nil {
					return fmt.Errorf("encode billing lease %q reservation: %w", lease.value.Uuid, err)
				}
				storeKey, err := collections.EncodeKeyWithPrefix(
					types.LeaseKey.Bytes(),
					collections.StringKey,
					lease.value.Uuid,
				)
				if err != nil {
					return fmt.Errorf("encode billing lease key %q: %w", lease.value.Uuid, err)
				}
				if err := store.Set(storeKey, encoded); err != nil {
					return fmt.Errorf("write billing lease %q reservation: %w", lease.value.Uuid, err)
				}
			}

			encoded, err := m.keeper.CreditAccounts.ValueCodec().Encode(plan.account)
			if err != nil {
				return fmt.Errorf("encode billing reservation account %q: %w", entry.key.String(), err)
			}
			storeKey, err := collections.EncodeKeyWithPrefix(
				types.CreditAccountKey.Bytes(),
				sdk.AccAddressKey,
				entry.key,
			)
			if err != nil {
				return fmt.Errorf("encode billing credit-account key %q: %w", entry.key.String(), err)
			}
			if err := store.Set(storeKey, encoded); err != nil {
				return fmt.Errorf("write billing reservation account %q: %w", entry.key.String(), err)
			}
		}

		if !hasMore {
			return nil
		}
		lastKey = append(lastKey[:0], entries[len(entries)-1].key...)
		hasStart = true
	}
}

func (m Migrator) buildReservationMigrationPlan(
	ctx sdk.Context,
	tenant sdk.AccAddress,
	account types.CreditAccount,
) (reservationMigrationPlan, error) {
	plan := reservationMigrationPlan{account: account}

	accountTenant, err := sdk.AccAddressFromBech32(account.Tenant)
	if err != nil {
		return plan, types.ErrReservationInvariant.Wrapf(
			"decode billing credit-account tenant %q: %s", account.Tenant, err,
		)
	}
	if !accountTenant.Equals(tenant) {
		return plan, types.ErrReservationInvariant.Wrapf(
			"billing credit-account tenant %q does not match its byte key", account.Tenant,
		)
	}
	creditAddress, err := sdk.AccAddressFromBech32(account.CreditAddress)
	if err != nil {
		return plan, types.ErrReservationInvariant.Wrapf(
			"decode billing credit address for tenant %q: %s", tenant.String(), err,
		)
	}
	expectedCreditAddress := types.DeriveCreditAddress(tenant)
	if !creditAddress.Equals(expectedCreditAddress) {
		return plan, types.ErrReservationInvariant.Wrapf(
			"billing credit address for tenant %q does not match its derived byte identity",
			tenant.String(),
		)
	}
	if !account.UnattributedReservedAmounts.IsZero() {
		return plan, types.ErrReservationInvariant.Wrapf(
			"tenant %q has unattributed reservations before the v3 to v4 migration",
			tenant.String(),
		)
	}
	if account.UnattributedLeaseCount != 0 {
		return plan, types.ErrReservationInvariant.Wrapf(
			"tenant %q has unattributed lease count %d before the v3 to v4 migration",
			tenant.String(), account.UnattributedLeaseCount,
		)
	}

	oldAggregate, err := types.SafeAddCoins(sdk.NewCoins(), account.ReservedAmounts)
	if err != nil {
		return plan, types.ErrReservationInvariant.Wrapf(
			"tenant %q has invalid pre-migration reservations: %s", tenant.String(), err,
		)
	}

	records, err := m.collectLiveReservationMigrationLeases(ctx, tenant)
	if err != nil {
		return plan, err
	}
	legacyLeaseCount, err := countLiveLegacyReservationLeases(records)
	if err != nil {
		return plan, types.ErrReservationInvariant.Wrapf(
			"count live legacy leases for tenant %q: %s", tenant.String(), err,
		)
	}
	bankBalances, err := m.reservationMigrationBankBalances(ctx, expectedCreditAddress, oldAggregate)
	if err != nil {
		return plan, err
	}
	records, newAggregate, legacyAllocation, err := planConsumableReservationCutover(
		oldAggregate,
		bankBalances,
		records,
	)
	if err != nil {
		return plan, types.ErrReservationInvariant.Wrapf(
			"plan consumable reservations for tenant %q: %s", tenant.String(), err,
		)
	}

	plan.account.ReservedAmounts = newAggregate
	plan.account.UnattributedReservedAmounts = legacyAllocation
	plan.account.UnattributedLeaseCount = legacyLeaseCount
	plan.leases = records
	return plan, nil
}

func countLiveLegacyReservationLeases(records []reservationMigrationLease) (uint64, error) {
	var count uint64
	for index := range records {
		if records[index].value.MinLeaseDurationAtCreation != 0 {
			continue
		}
		if count == math.MaxUint64 {
			return 0, fmt.Errorf("legacy lease count overflows uint64")
		}
		count++
	}
	return count, nil
}

// planConsumableReservationCutover is the shared pure v3-state conversion used
// by both in-place migrations and exported-genesis imports. It never creates a
// claim from unreserved bank credit: its total budget is bounded by both the
// historical aggregate and the actual bank balance. Exact PENDING claims are
// funded first; modern ACTIVE claims and the opaque live-legacy cohort share
// the remainder proportionally with a stable UUID/cohort tie-break.
func planConsumableReservationCutover(
	oldAggregate,
	bankBalances sdk.Coins,
	records []reservationMigrationLease,
) (planned []reservationMigrationLease, newAggregate, legacyAllocation sdk.Coins, err error) {
	oldAggregate, err = types.SafeAddCoins(sdk.NewCoins(), oldAggregate)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("validate historical reservation aggregate: %w", err)
	}
	bankBalances, err = types.SafeAddCoins(sdk.NewCoins(), bankBalances)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("validate reservation bank balances: %w", err)
	}
	budget := boundedReservationBudget(oldAggregate, bankBalances)
	planned = append([]reservationMigrationLease(nil), records...)

	modernPendingEntries := make([]sdk.Coin, 0)
	modernActiveEntries := make([]sdk.Coin, 0)
	activeClaims := make([]reservationMigrationClaim, 0)
	hasLegacyCohort := false

	for index := range planned {
		planned[index].allocation = sdk.NewCoins()
		lease := &planned[index].value
		if lease.MinLeaseDurationAtCreation == 0 {
			hasLegacyCohort = true
			continue
		}

		nominal, err := types.CalculateLeaseReservation(
			lease.Items,
			lease.MinLeaseDurationAtCreation,
		)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("calculate billing lease %q nominal reservation: %w", lease.Uuid, err)
		}
		planned[index].nominal = nominal
		switch lease.State {
		case types.LEASE_STATE_PENDING:
			planned[index].allocation = nominal
			modernPendingEntries = append(modernPendingEntries, nominal...)
		case types.LEASE_STATE_ACTIVE:
			modernActiveEntries = append(modernActiveEntries, nominal...)
			activeClaims = append(activeClaims, reservationMigrationClaim{
				recordIndex: index,
				uuid:        lease.Uuid,
				nominal:     nominal,
				allocation:  sdk.NewCoins(),
			})
		default:
			return nil, nil, nil, types.ErrReservationInvariant.Wrapf(
				"live reservation scan returned terminal lease %q", lease.Uuid,
			)
		}
	}

	// Primary UUID order is the canonical claimant order. The opaque legacy
	// cohort is appended after these claims by allocateReservationClaims.
	slices.SortFunc(activeClaims, func(left, right reservationMigrationClaim) int {
		return cmp.Compare(left.uuid, right.uuid)
	})
	modernPending, err := types.SafeAggregateCoins(modernPendingEntries)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("sum PENDING reservations: %w", err)
	}
	modernActive, err := types.SafeAggregateCoins(modernActiveEntries)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("sum ACTIVE claims: %w", err)
	}

	if _, err := types.SafeSubtractCoins(oldAggregate, modernPending); err != nil {
		return nil, nil, nil, types.ErrReservationInvariant.Wrapf(
			"old aggregate cannot fully back exact PENDING reservations %s: %s",
			modernPending.String(), err,
		)
	}
	if _, err := types.SafeSubtractCoins(bankBalances, modernPending); err != nil {
		return nil, nil, nil, types.ErrReservationInvariant.Wrapf(
			"bank balance cannot fully back exact PENDING reservations %s: %s",
			modernPending.String(), err,
		)
	}
	remainingBudget, err := types.SafeSubtractCoins(budget, modernPending)
	if err != nil {
		return nil, nil, nil, types.ErrReservationInvariant.Wrapf(
			"subtract exact PENDING reservations from migration budget: %s",
			err,
		)
	}

	modernLive, err := types.SafeAddCoins(modernPending, modernActive)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("sum modern live claims: %w", err)
	}
	legacyClaim := sdk.NewCoins()
	if hasLegacyCohort {
		legacyClaim = positiveCoinDifference(oldAggregate, modernLive)
	}

	legacyAllocation, err = allocateReservationClaims(remainingBudget, activeClaims, legacyClaim)
	if err != nil {
		return nil, nil, nil, types.ErrReservationInvariant.Wrapf(
			"allocate ACTIVE and legacy reservations: %s", err,
		)
	}
	for index := range activeClaims {
		claim := activeClaims[index]
		planned[claim.recordIndex].allocation = claim.allocation
	}

	newAggregateEntries := append([]sdk.Coin(nil), modernPending...)
	for index := range activeClaims {
		newAggregateEntries = append(newAggregateEntries, activeClaims[index].allocation...)
	}
	newAggregateEntries = append(newAggregateEntries, legacyAllocation...)
	newAggregate, err = types.SafeAggregateCoins(newAggregateEntries)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("sum migration allocations: %w", err)
	}
	if _, err := types.SafeSubtractCoins(budget, newAggregate); err != nil {
		return nil, nil, nil, types.ErrReservationInvariant.Wrapf(
			"migration allocation exceeds its bank-backed old aggregate: %s",
			err,
		)
	}

	for index := range planned {
		planned[index].value.Reservation = &types.LeaseReservation{
			RemainingAmounts: planned[index].allocation,
		}
	}
	return planned, newAggregate, legacyAllocation, nil
}

func boundedReservationBudget(oldAggregate, bankBalances sdk.Coins) sdk.Coins {
	budget := make(sdk.Coins, 0, len(oldAggregate))
	balanceIndex := 0
	for _, reserved := range oldAggregate {
		amount := reserved.Amount
		for balanceIndex < len(bankBalances) && bankBalances[balanceIndex].Denom < reserved.Denom {
			balanceIndex++
		}
		balance := sdkmath.ZeroInt()
		if balanceIndex < len(bankBalances) && bankBalances[balanceIndex].Denom == reserved.Denom {
			balance = bankBalances[balanceIndex].Amount
		}
		if balance.LT(amount) {
			amount = balance
		}
		if amount.IsPositive() {
			budget = append(budget, sdk.Coin{Denom: reserved.Denom, Amount: amount})
		}
	}
	return budget
}

func (m Migrator) collectLiveReservationMigrationLeases(
	ctx sdk.Context,
	tenant sdk.AccAddress,
) ([]reservationMigrationLease, error) {
	records := make([]reservationMigrationLease, 0)
	liveStates := [...]types.LeaseState{
		types.LEASE_STATE_PENDING,
		types.LEASE_STATE_ACTIVE,
	}
	for _, state := range liveStates {
		key := collections.Join(tenant, int32(state))
		iterator, err := m.keeper.Leases.Indexes.TenantState.MatchExact(ctx, key)
		if err != nil {
			return nil, fmt.Errorf(
				"iterate %s billing leases for tenant %q reservation migration: %w",
				state.String(), tenant.String(), err,
			)
		}
		for ; iterator.Valid(); iterator.Next() {
			leaseUUID, err := iterator.PrimaryKey()
			if err != nil {
				_ = iterator.Close()
				return nil, fmt.Errorf(
					"decode %s billing tenant-state reservation entry for %q: %w",
					state.String(), tenant.String(), err,
				)
			}
			lease, err := m.keeper.Leases.Get(ctx, leaseUUID)
			if err != nil {
				_ = iterator.Close()
				return nil, fmt.Errorf("read billing lease %q for reservation migration: %w", leaseUUID, err)
			}
			leaseTenant, err := sdk.AccAddressFromBech32(lease.Tenant)
			if err != nil {
				_ = iterator.Close()
				return nil, types.ErrReservationInvariant.Wrapf(
					"decode billing lease %q tenant %q: %s", leaseUUID, lease.Tenant, err,
				)
			}
			if !tenant.Equals(leaseTenant) || lease.State != state {
				_ = iterator.Close()
				return nil, types.ErrReservationInvariant.Wrapf(
					"billing lease %q does not match its %s tenant-state index entry",
					leaseUUID, state.String(),
				)
			}
			records = append(records, reservationMigrationLease{value: lease})
		}
		if err := iterator.Close(); err != nil {
			return nil, fmt.Errorf(
				"close %s billing tenant-state reservation iterator for %q: %w",
				state.String(), tenant.String(), err,
			)
		}
	}

	slices.SortFunc(records, func(left, right reservationMigrationLease) int {
		return cmp.Compare(left.value.Uuid, right.value.Uuid)
	})
	return records, nil
}

func (m Migrator) reservationMigrationBankBalances(
	ctx sdk.Context,
	creditAddress sdk.AccAddress,
	oldAggregate sdk.Coins,
) (sdk.Coins, error) {
	bankBalances := sdk.NewCoins()
	for _, reserved := range oldAggregate {
		balance := m.keeper.bankKeeper.GetBalance(ctx, creditAddress, reserved.Denom)
		if err := balance.Validate(); err != nil {
			return nil, types.ErrReservationInvariant.Wrapf(
				"credit address %q has invalid %s bank balance: %s",
				creditAddress.String(), reserved.Denom, err,
			)
		}
		if balance.Amount.IsPositive() {
			bankBalances = append(bankBalances, balance)
		}
	}
	return bankBalances, nil
}

// allocateReservationClaims applies Hamilton's largest-remainder method per
// denomination. Active claims are ordered by UUID and the legacy cohort is the
// final claimant, so equal remainders have a stable consensus tie-break.
func allocateReservationClaims(
	budget sdk.Coins,
	activeClaims []reservationMigrationClaim,
	legacyClaim sdk.Coins,
) (sdk.Coins, error) {
	denomClaimIndexes := make(map[string]int)
	denomClaims := make([]reservationMigrationDenomClaims, 0)
	addClaim := func(denom string, claimantIndex int, amount sdkmath.Int) error {
		denomIndex, found := denomClaimIndexes[denom]
		if !found {
			denomIndex = len(denomClaims)
			denomClaimIndexes[denom] = denomIndex
			denomClaims = append(denomClaims, reservationMigrationDenomClaims{
				denom: denom,
				total: sdkmath.ZeroInt(),
			})
		}

		total, err := denomClaims[denomIndex].total.SafeAdd(amount)
		if err != nil {
			return types.ErrArithmeticOverflow.Wrapf(
				"sum proportional %s reservation claims", denom,
			)
		}
		denomClaims[denomIndex].total = total
		denomClaims[denomIndex].claims = append(
			denomClaims[denomIndex].claims,
			reservationMigrationDenomClaim{
				claimantIndex: claimantIndex,
				amount:        amount,
			},
		)
		return nil
	}

	for claimantIndex := range activeClaims {
		for _, coin := range activeClaims[claimantIndex].nominal {
			if err := addClaim(coin.Denom, claimantIndex, coin.Amount); err != nil {
				return nil, err
			}
		}
	}
	for _, coin := range legacyClaim {
		if err := addClaim(coin.Denom, len(activeClaims), coin.Amount); err != nil {
			return nil, err
		}
	}
	slices.SortFunc(denomClaims, func(left, right reservationMigrationDenomClaims) int {
		return cmp.Compare(left.denom, right.denom)
	})

	legacyAllocation := make(sdk.Coins, 0, len(legacyClaim))
	budgetIndex := 0
	for denomIndex := range denomClaims {
		denomClaim := &denomClaims[denomIndex]
		for budgetIndex < len(budget) && budget[budgetIndex].Denom < denomClaim.denom {
			budgetIndex++
		}
		pool := sdkmath.ZeroInt()
		if budgetIndex < len(budget) && budget[budgetIndex].Denom == denomClaim.denom {
			pool = budget[budgetIndex].Amount
		}
		if !pool.IsPositive() {
			continue
		}
		if pool.GT(denomClaim.total) {
			pool = denomClaim.total
		}

		shares := make([]reservationMigrationShare, 0, len(denomClaim.claims))
		allocatedFloor := sdkmath.ZeroInt()
		for _, claim := range denomClaim.claims {
			product := new(big.Int).Mul(pool.BigInt(), claim.amount.BigInt())
			quotient := new(big.Int)
			remainder := new(big.Int)
			quotient.QuoRem(product, denomClaim.total.BigInt(), remainder)
			floor := sdkmath.NewIntFromBigInt(quotient)
			var err error
			allocatedFloor, err = allocatedFloor.SafeAdd(floor)
			if err != nil {
				return nil, types.ErrArithmeticOverflow.Wrapf(
					"sum proportional %s reservation floors", denomClaim.denom,
				)
			}
			shares = append(shares, reservationMigrationShare{
				claimantIndex: claim.claimantIndex,
				remainder:     remainder,
				allocation:    floor,
			})
		}

		leftover, err := pool.SafeSub(allocatedFloor)
		if err != nil || leftover.IsNegative() {
			return nil, types.ErrReservationInvariant.Wrapf(
				"invalid proportional %s reservation remainder", denomClaim.denom,
			)
		}
		slices.SortStableFunc(shares, func(left, right reservationMigrationShare) int {
			if order := right.remainder.Cmp(left.remainder); order != 0 {
				return order
			}
			return cmp.Compare(left.claimantIndex, right.claimantIndex)
		})
		for index := 0; leftover.IsPositive(); index++ {
			if index >= len(shares) {
				return nil, types.ErrReservationInvariant.Wrapf(
					"too many proportional %s reservation remainder units", denomClaim.denom,
				)
			}
			shares[index].allocation, err = shares[index].allocation.SafeAdd(sdkmath.OneInt())
			if err != nil {
				return nil, types.ErrArithmeticOverflow.Wrapf(
					"increment proportional %s reservation allocation", denomClaim.denom,
				)
			}
			leftover, err = leftover.SafeSub(sdkmath.OneInt())
			if err != nil {
				return nil, types.ErrArithmeticOverflow.Wrapf(
					"decrement proportional %s reservation remainder", denomClaim.denom,
				)
			}
		}

		for _, share := range shares {
			if !share.allocation.IsPositive() {
				continue
			}
			coin := sdk.Coin{Denom: denomClaim.denom, Amount: share.allocation}
			if share.claimantIndex < len(activeClaims) {
				activeClaims[share.claimantIndex].allocation = append(
					activeClaims[share.claimantIndex].allocation,
					coin,
				)
			} else {
				legacyAllocation = append(legacyAllocation, coin)
			}
		}
	}

	for index := range activeClaims {
		if _, err := types.SafeSubtractCoins(activeClaims[index].nominal, activeClaims[index].allocation); err != nil {
			return nil, types.ErrReservationInvariant.Wrapf(
				"ACTIVE lease %q allocation exceeds its nominal claim: %s",
				activeClaims[index].uuid, err,
			)
		}
	}
	if _, err := types.SafeSubtractCoins(legacyClaim, legacyAllocation); err != nil {
		return nil, types.ErrReservationInvariant.Wrapf(
			"legacy cohort allocation exceeds its opaque claim: %s", err,
		)
	}
	return legacyAllocation, nil
}

func positiveCoinDifference(left, right sdk.Coins) sdk.Coins {
	difference := make(sdk.Coins, 0, len(left))
	rightIndex := 0
	for _, leftCoin := range left {
		for rightIndex < len(right) && right[rightIndex].Denom < leftCoin.Denom {
			rightIndex++
		}
		if rightIndex >= len(right) || right[rightIndex].Denom != leftCoin.Denom {
			difference = append(difference, leftCoin)
			continue
		}
		if leftCoin.Amount.GT(right[rightIndex].Amount) {
			difference = append(difference, sdk.Coin{
				Denom:  leftCoin.Denom,
				Amount: leftCoin.Amount.Sub(right[rightIndex].Amount),
			})
		}
	}
	return difference
}

func (m Migrator) initializeTerminalReservationWrappers(ctx sdk.Context) error {
	var (
		lastKey  string
		hasStart bool
	)
	store := m.keeper.storeService.OpenKVStore(ctx)

	for {
		var keyRange collections.Ranger[string]
		if hasStart {
			keyRange = new(collections.Range[string]).StartExclusive(lastKey)
		}
		iterator, err := m.keeper.Leases.Iterate(ctx, keyRange)
		if err != nil {
			return fmt.Errorf("iterate billing leases for terminal reservation initialization: %w", err)
		}

		entries := make([]leaseMigrationEntry, 0, storageMigrationPageSize)
		for iterator.Valid() && len(entries) < storageMigrationPageSize {
			entry, entryErr := iterator.KeyValue()
			if entryErr != nil {
				_ = iterator.Close()
				return fmt.Errorf("decode billing terminal reservation entry: %w", entryErr)
			}
			entries = append(entries, leaseMigrationEntry{key: entry.Key, value: entry.Value})
			iterator.Next()
		}
		hasMore := iterator.Valid()
		if err := iterator.Close(); err != nil {
			return fmt.Errorf("close billing terminal reservation iterator: %w", err)
		}

		for _, entry := range entries {
			switch entry.value.State {
			case types.LEASE_STATE_PENDING, types.LEASE_STATE_ACTIVE:
				if entry.value.Reservation == nil {
					return types.ErrReservationInvariant.Wrapf(
						"live lease %q was not reached through its tenant account and state index",
						entry.key,
					)
				}
				continue
			case types.LEASE_STATE_CLOSED, types.LEASE_STATE_REJECTED, types.LEASE_STATE_EXPIRED:
				entry.value.Reservation = &types.LeaseReservation{RemainingAmounts: sdk.NewCoins()}
			default:
				return types.ErrReservationInvariant.Wrapf(
					"lease %q has invalid state %s during reservation migration",
					entry.key, entry.value.State.String(),
				)
			}

			encoded, err := m.keeper.Leases.ValueCodec().Encode(entry.value)
			if err != nil {
				return fmt.Errorf("encode billing terminal lease %q reservation: %w", entry.key, err)
			}
			storeKey, err := collections.EncodeKeyWithPrefix(
				types.LeaseKey.Bytes(),
				collections.StringKey,
				entry.key,
			)
			if err != nil {
				return fmt.Errorf("encode billing terminal lease key %q: %w", entry.key, err)
			}
			if err := store.Set(storeKey, encoded); err != nil {
				return fmt.Errorf("write billing terminal lease %q reservation: %w", entry.key, err)
			}
		}

		if !hasMore {
			return nil
		}
		lastKey = entries[len(entries)-1].key
		hasStart = true
	}
}
