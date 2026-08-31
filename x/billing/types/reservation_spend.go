package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// ReservationSpendPlan describes an own-allocation-first credit spend without
// mutating its inputs. Spendable preserves every reservation except the
// settling lease's allocation; Consumed is the part of Transfer paid from that
// allocation. All coin sets are canonical and denomination ordered.
type ReservationSpendPlan struct {
	Spendable          sdk.Coins
	Transfer           sdk.Coins
	Consumed           sdk.Coins
	BalanceAfter       sdk.Coins
	TotalReservedAfter sdk.Coins
	AllocationAfter    sdk.Coins
}

// PlanReservationSpend computes a settlement against a lease's own reservation
// allocation while preserving every other reservation:
//
//	other reserved = total reserved - allocation
//	spendable      = balance - other reserved
//	transfer       = min(charge, spendable)
//	consumed       = min(transfer, allocation)
//
// The balance must back the complete reservation aggregate, and the aggregate
// must contain the lease allocation. These checks make every subtraction exact
// and ensure the returned post-spend balance still backs the returned aggregate.
// The implementation operates only on canonical ordered coin slices.
func PlanReservationSpend(
	balance,
	totalReserved,
	allocation,
	charge sdk.Coins,
) (ReservationSpendPlan, error) {
	inputs := [...]struct {
		name  string
		coins sdk.Coins
	}{
		{name: "balance", coins: balance},
		{name: "total reserved", coins: totalReserved},
		{name: "allocation", coins: allocation},
		{name: "charge", coins: charge},
	}
	for _, input := range inputs {
		if err := validateCanonicalCoins(input.coins); err != nil {
			return ReservationSpendPlan{}, ErrReservationInvariant.Wrapf(
				"invalid %s coin set: %s",
				input.name,
				err,
			)
		}
	}

	otherReserved, err := SafeSubtractCoins(totalReserved, allocation)
	if err != nil {
		return ReservationSpendPlan{}, ErrReservationInvariant.Wrapf(
			"total reservation does not contain lease allocation: %s",
			err,
		)
	}

	if _, err := SafeSubtractCoins(balance, totalReserved); err != nil {
		return ReservationSpendPlan{}, ErrReservationInvariant.Wrapf(
			"credit balance does not back total reservation: %s",
			err,
		)
	}

	spendable, err := SafeSubtractCoins(balance, otherReserved)
	if err != nil {
		return ReservationSpendPlan{}, ErrReservationInvariant.Wrapf(
			"calculate spendable credit: %s",
			err,
		)
	}
	transfer := charge.Min(spendable)
	consumed := transfer.Min(allocation)

	balanceAfter, err := SafeSubtractCoins(balance, transfer)
	if err != nil {
		return ReservationSpendPlan{}, ErrReservationInvariant.Wrapf(
			"calculate post-transfer balance: %s",
			err,
		)
	}
	totalReservedAfter, err := SafeSubtractCoins(totalReserved, consumed)
	if err != nil {
		return ReservationSpendPlan{}, ErrReservationInvariant.Wrapf(
			"consume total reservation: %s",
			err,
		)
	}
	allocationAfter, err := SafeSubtractCoins(allocation, consumed)
	if err != nil {
		return ReservationSpendPlan{}, ErrReservationInvariant.Wrapf(
			"consume lease allocation: %s",
			err,
		)
	}

	return ReservationSpendPlan{
		Spendable:          spendable,
		Transfer:           transfer,
		Consumed:           consumed,
		BalanceAfter:       balanceAfter,
		TotalReservedAfter: totalReservedAfter,
		AllocationAfter:    allocationAfter,
	}, nil
}
