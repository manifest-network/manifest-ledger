package keeper

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

// prepareGenesisReservationState converts a pre-v4 aggregate-only (v2/v3)
// export into the same consumable representation produced by Migrate3to4. The
// conversion is planned entirely in memory and the returned state is validated
// before InitGenesis performs its first store write.
func (k *Keeper) prepareGenesisReservationState(
	ctx sdk.Context,
	gs *types.GenesisState,
) (*types.GenesisState, error) {
	legacy, err := gs.HasLegacyReservationState()
	if err != nil {
		return nil, err
	}
	if !legacy {
		if err := k.validateGenesisReservationBacking(ctx, gs); err != nil {
			return nil, err
		}
		return gs, nil
	}

	prepared := *gs
	prepared.Leases = append([]types.Lease(nil), gs.Leases...)
	prepared.CreditAccounts = append([]types.CreditAccount(nil), gs.CreditAccounts...)

	liveLeaseIndexes := make(map[string][]int)
	for index := range prepared.Leases {
		lease := &prepared.Leases[index]
		lease.Reservation = &types.LeaseReservation{RemainingAmounts: sdk.NewCoins()}
		if !isLiveLeaseState(lease.State) {
			continue
		}
		tenant, err := sdk.AccAddressFromBech32(lease.Tenant)
		if err != nil {
			return nil, types.ErrReservationInvariant.Wrapf(
				"decode genesis lease %s tenant: %s", lease.Uuid, err,
			)
		}
		tenantKey := tenant.String()
		liveLeaseIndexes[tenantKey] = append(liveLeaseIndexes[tenantKey], index)
	}

	migrator := NewMigrator(*k)
	for accountIndex := range prepared.CreditAccounts {
		account := &prepared.CreditAccounts[accountIndex]
		tenant, err := sdk.AccAddressFromBech32(account.Tenant)
		if err != nil {
			return nil, types.ErrReservationInvariant.Wrapf(
				"decode genesis credit-account tenant %q: %s", account.Tenant, err,
			)
		}
		creditAddress, err := sdk.AccAddressFromBech32(account.CreditAddress)
		if err != nil {
			return nil, types.ErrReservationInvariant.Wrapf(
				"decode genesis credit address for tenant %q: %s", account.Tenant, err,
			)
		}
		oldAggregate, err := types.SafeAddCoins(sdk.NewCoins(), account.ReservedAmounts)
		if err != nil {
			return nil, types.ErrReservationInvariant.Wrapf(
				"tenant %q has invalid genesis reservations: %s", tenant.String(), err,
			)
		}
		bankBalances, err := migrator.reservationMigrationBankBalances(ctx, creditAddress, oldAggregate)
		if err != nil {
			return nil, err
		}

		indexes := liveLeaseIndexes[tenant.String()]
		records := make([]reservationMigrationLease, 0, len(indexes))
		for _, leaseIndex := range indexes {
			records = append(records, reservationMigrationLease{
				value: prepared.Leases[leaseIndex],
			})
		}
		planned, aggregate, legacyAllocation, err := planConsumableReservationCutover(
			oldAggregate,
			bankBalances,
			records,
		)
		if err != nil {
			return nil, types.ErrReservationInvariant.Wrapf(
				"normalize imported reservations for tenant %q: %s", tenant.String(), err,
			)
		}
		legacyLeaseCount, err := countLiveLegacyReservationLeases(records)
		if err != nil {
			return nil, types.ErrReservationInvariant.Wrapf(
				"count imported live legacy leases for tenant %q: %s", tenant.String(), err,
			)
		}
		for recordIndex, leaseIndex := range indexes {
			prepared.Leases[leaseIndex] = planned[recordIndex].value
		}
		account.ReservedAmounts = aggregate
		account.UnattributedReservedAmounts = legacyAllocation
		account.UnattributedLeaseCount = legacyLeaseCount
	}

	if err := prepared.Validate(); err != nil {
		return nil, fmt.Errorf("validate normalized billing genesis reservations: %w", err)
	}
	if err := k.validateGenesisReservationBacking(ctx, &prepared); err != nil {
		return nil, err
	}
	return &prepared, nil
}

func (k *Keeper) validateGenesisReservationBacking(ctx sdk.Context, gs *types.GenesisState) error {
	for index := range gs.CreditAccounts {
		account := &gs.CreditAccounts[index]
		creditAddress, err := sdk.AccAddressFromBech32(account.CreditAddress)
		if err != nil {
			return types.ErrReservationInvariant.Wrapf(
				"decode genesis credit address for tenant %q: %s", account.Tenant, err,
			)
		}
		for _, reserved := range account.ReservedAmounts {
			balance := k.bankKeeper.GetBalance(ctx, creditAddress, reserved.Denom)
			if balance.Amount.LT(reserved.Amount) {
				return types.ErrReservationInvariant.Wrapf(
					"credit account for %s has bank balance %s below reservation %s%s",
					account.Tenant,
					balance.String(),
					reserved.Amount.String(),
					reserved.Denom,
				)
			}
		}
	}
	return nil
}
