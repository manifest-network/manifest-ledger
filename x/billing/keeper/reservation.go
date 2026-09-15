package keeper

import (
	"context"
	"slices"

	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

func isLiveLeaseState(state types.LeaseState) bool {
	return state == types.LEASE_STATE_PENDING || state == types.LEASE_STATE_ACTIVE
}

// leaseReservationAllocation returns the reservation tranche this lease may
// consume. Current leases own an isolated tranche. Historical zero-duration
// leases share only the explicitly unattributed legacy cohort; they can never
// consume a modern lease's allocation.
func leaseReservationAllocation(lease *types.Lease, account *types.CreditAccount) (sdk.Coins, error) {
	if lease.Reservation == nil {
		return nil, types.ErrReservationInvariant.Wrapf(
			"lease %s has no initialized reservation",
			lease.Uuid,
		)
	}

	remaining, err := types.SafeAddCoins(sdk.NewCoins(), lease.Reservation.RemainingAmounts)
	if err != nil {
		return nil, types.ErrReservationInvariant.Wrapf(
			"lease %s has invalid remaining reservation: %s",
			lease.Uuid, err,
		)
	}

	if lease.MinLeaseDurationAtCreation == 0 {
		if !remaining.IsZero() {
			return nil, types.ErrReservationInvariant.Wrapf(
				"legacy lease %s must not have an attributed reservation",
				lease.Uuid,
			)
		}
		if !isLiveLeaseState(lease.State) {
			return sdk.NewCoins(), nil
		}
		if account.UnattributedLeaseCount == 0 {
			return nil, types.ErrReservationInvariant.Wrapf(
				"live legacy lease %s is not represented in the unattributed lease count",
				lease.Uuid,
			)
		}
		allocation, err := types.SafeAddCoins(sdk.NewCoins(), account.UnattributedReservedAmounts)
		if err != nil {
			return nil, types.ErrReservationInvariant.Wrapf(
				"credit account for lease %s has invalid unattributed reservation: %s",
				lease.Uuid, err,
			)
		}
		return allocation, nil
	}

	if !isLiveLeaseState(lease.State) && !remaining.IsZero() {
		return nil, types.ErrReservationInvariant.Wrapf(
			"terminal lease %s has a non-empty reservation",
			lease.Uuid,
		)
	}
	return remaining, nil
}

func validateLeaseCreditAccountIdentity(lease *types.Lease, account *types.CreditAccount) error {
	leaseTenant, err := sdk.AccAddressFromBech32(lease.Tenant)
	if err != nil {
		return types.ErrReservationInvariant.Wrapf(
			"lease %s has invalid tenant address: %s",
			lease.Uuid, err,
		)
	}
	accountTenant, err := sdk.AccAddressFromBech32(account.Tenant)
	if err != nil {
		return types.ErrReservationInvariant.Wrapf(
			"credit account has invalid tenant address: %s",
			err,
		)
	}
	if !leaseTenant.Equals(accountTenant) {
		return types.ErrReservationInvariant.Wrapf(
			"lease %s tenant does not match credit account tenant",
			lease.Uuid,
		)
	}
	return nil
}

func projectCoinsToDenoms(coins sdk.Coins, denoms []string) (sdk.Coins, error) {
	canonical, err := types.SafeAddCoins(sdk.NewCoins(), coins)
	if err != nil {
		return nil, err
	}
	orderedDenoms := slices.Clone(denoms)
	for _, denom := range orderedDenoms {
		if err := sdk.ValidateDenom(denom); err != nil {
			return nil, types.ErrInvalidCreditOperation.Wrapf(
				"invalid projection denom %q: %s", denom, err,
			)
		}
	}
	slices.Sort(orderedDenoms)
	orderedDenoms = slices.Compact(orderedDenoms)

	projected := make(sdk.Coins, 0, min(len(canonical), len(orderedDenoms)))
	coinIndex, denomIndex := 0, 0
	for coinIndex < len(canonical) && denomIndex < len(orderedDenoms) {
		switch {
		case canonical[coinIndex].Denom < orderedDenoms[denomIndex]:
			coinIndex++
		case canonical[coinIndex].Denom > orderedDenoms[denomIndex]:
			denomIndex++
		default:
			projected = append(projected, canonical[coinIndex])
			coinIndex++
			denomIndex++
		}
	}
	return projected, nil
}

// reservationSpendInputs loads the balance and reservation subsets relevant
// to a lease. The account aggregate may contain unrelated denoms, so it is
// projected before the pure spend planner is called.
func (k *Keeper) reservationSpendInputs(
	ctx context.Context,
	lease *types.Lease,
	account *types.CreditAccount,
) (balances, reserved, allocation sdk.Coins, err error) {
	if err := validateLeaseCreditAccountIdentity(lease, account); err != nil {
		return nil, nil, nil, err
	}

	// Validate the legacy cohort as a subset even when this lease is modern.
	if !account.UnattributedReservedAmounts.IsZero() && account.UnattributedLeaseCount == 0 {
		return nil, nil, nil, types.ErrReservationInvariant.Wrapf(
			"credit account for lease %s has an unattributed reservation without live cohort members",
			lease.Uuid,
		)
	}
	attributedReserved, err := types.SafeSubtractCoins(
		account.ReservedAmounts,
		account.UnattributedReservedAmounts,
	)
	if err != nil {
		return nil, nil, nil, types.ErrReservationInvariant.Wrapf(
			"credit account for lease %s does not contain its unattributed reservation: %s",
			lease.Uuid, err,
		)
	}

	allocation, err = leaseReservationAllocation(lease, account)
	if err != nil {
		return nil, nil, nil, err
	}

	denoms := leaseItemDenoms(lease.Items)
	allowedDenoms := make(map[string]struct{}, len(denoms))
	for _, denom := range denoms {
		allowedDenoms[denom] = struct{}{}
	}
	if lease.MinLeaseDurationAtCreation != 0 {
		if _, err := types.SafeSubtractCoins(attributedReserved, allocation); err != nil {
			return nil, nil, nil, types.ErrReservationInvariant.Wrapf(
				"credit account for lease %s does not contain its attributed reservation: %s",
				lease.Uuid, err,
			)
		}
		for _, coin := range allocation {
			if _, ok := allowedDenoms[coin.Denom]; !ok {
				return nil, nil, nil, types.ErrReservationInvariant.Wrapf(
					"lease %s reservation contains unrelated denom %s",
					lease.Uuid, coin.Denom,
				)
			}
		}
	}

	reserved, err = projectCoinsToDenoms(account.ReservedAmounts, denoms)
	if err != nil {
		return nil, nil, nil, errorsmod.Wrapf(err, "project reservation for lease %s", lease.Uuid)
	}
	allocation, err = projectCoinsToDenoms(allocation, denoms)
	if err != nil {
		return nil, nil, nil, errorsmod.Wrapf(err, "project allocation for lease %s", lease.Uuid)
	}
	balances, err = k.getCreditBalancesForDenoms(ctx, lease.Tenant, denoms)
	if err != nil {
		return nil, nil, nil, err
	}
	return balances, reserved, allocation, nil
}
