package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

func reservationCoins(amounts ...sdk.Coin) sdk.Coins {
	return sdk.NewCoins(amounts...)
}

func TestPlanReservationSpend(t *testing.T) {
	tests := []struct {
		name          string
		balance       sdk.Coins
		totalReserved sdk.Coins
		allocation    sdk.Coins
		charge        sdk.Coins
		want          types.ReservationSpendPlan
	}{
		{
			name:          "charge within own allocation",
			balance:       reservationCoins(sdk.NewInt64Coin("umfx", 200)),
			totalReserved: reservationCoins(sdk.NewInt64Coin("umfx", 200)),
			allocation:    reservationCoins(sdk.NewInt64Coin("umfx", 100)),
			charge:        reservationCoins(sdk.NewInt64Coin("umfx", 40)),
			want: types.ReservationSpendPlan{
				Spendable:          reservationCoins(sdk.NewInt64Coin("umfx", 100)),
				Transfer:           reservationCoins(sdk.NewInt64Coin("umfx", 40)),
				Consumed:           reservationCoins(sdk.NewInt64Coin("umfx", 40)),
				BalanceAfter:       reservationCoins(sdk.NewInt64Coin("umfx", 160)),
				TotalReservedAfter: reservationCoins(sdk.NewInt64Coin("umfx", 160)),
				AllocationAfter:    reservationCoins(sdk.NewInt64Coin("umfx", 60)),
			},
		},
		{
			name:          "other lease guarantee caps transfer",
			balance:       reservationCoins(sdk.NewInt64Coin("umfx", 200)),
			totalReserved: reservationCoins(sdk.NewInt64Coin("umfx", 200)),
			allocation:    reservationCoins(sdk.NewInt64Coin("umfx", 100)),
			charge:        reservationCoins(sdk.NewInt64Coin("umfx", 150)),
			want: types.ReservationSpendPlan{
				Spendable:          reservationCoins(sdk.NewInt64Coin("umfx", 100)),
				Transfer:           reservationCoins(sdk.NewInt64Coin("umfx", 100)),
				Consumed:           reservationCoins(sdk.NewInt64Coin("umfx", 100)),
				BalanceAfter:       reservationCoins(sdk.NewInt64Coin("umfx", 100)),
				TotalReservedAfter: reservationCoins(sdk.NewInt64Coin("umfx", 100)),
				AllocationAfter:    sdk.NewCoins(),
			},
		},
		{
			name:          "unreserved credit is spent after own allocation",
			balance:       reservationCoins(sdk.NewInt64Coin("umfx", 250)),
			totalReserved: reservationCoins(sdk.NewInt64Coin("umfx", 200)),
			allocation:    reservationCoins(sdk.NewInt64Coin("umfx", 100)),
			charge:        reservationCoins(sdk.NewInt64Coin("umfx", 150)),
			want: types.ReservationSpendPlan{
				Spendable:          reservationCoins(sdk.NewInt64Coin("umfx", 150)),
				Transfer:           reservationCoins(sdk.NewInt64Coin("umfx", 150)),
				Consumed:           reservationCoins(sdk.NewInt64Coin("umfx", 100)),
				BalanceAfter:       reservationCoins(sdk.NewInt64Coin("umfx", 100)),
				TotalReservedAfter: reservationCoins(sdk.NewInt64Coin("umfx", 100)),
				AllocationAfter:    sdk.NewCoins(),
			},
		},
		{
			name: "multi denom allocation and unreserved charge",
			balance: reservationCoins(
				sdk.NewInt64Coin("aaa", 250),
				sdk.NewInt64Coin("bbb", 120),
				sdk.NewInt64Coin("ccc", 10),
			),
			totalReserved: reservationCoins(
				sdk.NewInt64Coin("aaa", 200),
				sdk.NewInt64Coin("bbb", 100),
			),
			allocation: reservationCoins(
				sdk.NewInt64Coin("aaa", 100),
				sdk.NewInt64Coin("bbb", 40),
			),
			charge: reservationCoins(
				sdk.NewInt64Coin("aaa", 120),
				sdk.NewInt64Coin("bbb", 100),
				sdk.NewInt64Coin("ccc", 7),
			),
			want: types.ReservationSpendPlan{
				Spendable: reservationCoins(
					sdk.NewInt64Coin("aaa", 150),
					sdk.NewInt64Coin("bbb", 60),
					sdk.NewInt64Coin("ccc", 10),
				),
				Transfer: reservationCoins(
					sdk.NewInt64Coin("aaa", 120),
					sdk.NewInt64Coin("bbb", 60),
					sdk.NewInt64Coin("ccc", 7),
				),
				Consumed: reservationCoins(
					sdk.NewInt64Coin("aaa", 100),
					sdk.NewInt64Coin("bbb", 40),
				),
				BalanceAfter: reservationCoins(
					sdk.NewInt64Coin("aaa", 130),
					sdk.NewInt64Coin("bbb", 60),
					sdk.NewInt64Coin("ccc", 3),
				),
				TotalReservedAfter: reservationCoins(
					sdk.NewInt64Coin("aaa", 100),
					sdk.NewInt64Coin("bbb", 60),
				),
				AllocationAfter: sdk.NewCoins(),
			},
		},
		{
			name:          "empty allocation spends only unreserved credit",
			balance:       reservationCoins(sdk.NewInt64Coin("umfx", 150)),
			totalReserved: reservationCoins(sdk.NewInt64Coin("umfx", 100)),
			allocation:    sdk.NewCoins(),
			charge:        reservationCoins(sdk.NewInt64Coin("umfx", 80)),
			want: types.ReservationSpendPlan{
				Spendable:          reservationCoins(sdk.NewInt64Coin("umfx", 50)),
				Transfer:           reservationCoins(sdk.NewInt64Coin("umfx", 50)),
				Consumed:           sdk.NewCoins(),
				BalanceAfter:       reservationCoins(sdk.NewInt64Coin("umfx", 100)),
				TotalReservedAfter: reservationCoins(sdk.NewInt64Coin("umfx", 100)),
				AllocationAfter:    sdk.NewCoins(),
			},
		},
		{
			name:          "zero charge changes nothing",
			balance:       reservationCoins(sdk.NewInt64Coin("umfx", 200)),
			totalReserved: reservationCoins(sdk.NewInt64Coin("umfx", 100)),
			allocation:    reservationCoins(sdk.NewInt64Coin("umfx", 60)),
			charge:        sdk.NewCoins(),
			want: types.ReservationSpendPlan{
				Spendable:          reservationCoins(sdk.NewInt64Coin("umfx", 160)),
				Transfer:           sdk.NewCoins(),
				Consumed:           sdk.NewCoins(),
				BalanceAfter:       reservationCoins(sdk.NewInt64Coin("umfx", 200)),
				TotalReservedAfter: reservationCoins(sdk.NewInt64Coin("umfx", 100)),
				AllocationAfter:    reservationCoins(sdk.NewInt64Coin("umfx", 60)),
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			balanceBefore := append(sdk.Coins{}, tc.balance...)
			totalBefore := append(sdk.Coins{}, tc.totalReserved...)
			allocationBefore := append(sdk.Coins{}, tc.allocation...)
			chargeBefore := append(sdk.Coins{}, tc.charge...)

			got, err := types.PlanReservationSpend(
				tc.balance,
				tc.totalReserved,
				tc.allocation,
				tc.charge,
			)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
			require.Equal(t, balanceBefore, tc.balance)
			require.Equal(t, totalBefore, tc.totalReserved)
			require.Equal(t, allocationBefore, tc.allocation)
			require.Equal(t, chargeBefore, tc.charge)
		})
	}
}

func TestPlanReservationSpend_MaxIntBoundary(t *testing.T) {
	maxAmount := maxBillingInt()
	maxMinusOne, err := maxAmount.SafeSub(sdkmath.OneInt())
	require.NoError(t, err)

	plan, err := types.PlanReservationSpend(
		reservationCoins(sdk.NewCoin("umfx", maxAmount)),
		reservationCoins(sdk.NewCoin("umfx", maxAmount)),
		reservationCoins(sdk.NewInt64Coin("umfx", 1)),
		reservationCoins(sdk.NewCoin("umfx", maxAmount)),
	)
	require.NoError(t, err)
	require.Equal(t, sdkmath.OneInt(), plan.Spendable.AmountOf("umfx"))
	require.Equal(t, sdkmath.OneInt(), plan.Transfer.AmountOf("umfx"))
	require.Equal(t, sdkmath.OneInt(), plan.Consumed.AmountOf("umfx"))
	require.Equal(t, maxMinusOne, plan.BalanceAfter.AmountOf("umfx"))
	require.Equal(t, maxMinusOne, plan.TotalReservedAfter.AmountOf("umfx"))
	require.True(t, plan.AllocationAfter.IsZero())
}

func TestPlanReservationSpend_RejectsInvariantViolations(t *testing.T) {
	tests := []struct {
		name          string
		balance       sdk.Coins
		totalReserved sdk.Coins
		allocation    sdk.Coins
		charge        sdk.Coins
		wantMessage   string
	}{
		{
			name:          "allocation amount exceeds total",
			balance:       reservationCoins(sdk.NewInt64Coin("umfx", 200)),
			totalReserved: reservationCoins(sdk.NewInt64Coin("umfx", 50)),
			allocation:    reservationCoins(sdk.NewInt64Coin("umfx", 51)),
			charge:        reservationCoins(sdk.NewInt64Coin("umfx", 1)),
			wantMessage:   "total reservation does not contain lease allocation",
		},
		{
			name:          "allocation denom absent from total",
			balance:       reservationCoins(sdk.NewInt64Coin("aaa", 100), sdk.NewInt64Coin("bbb", 100)),
			totalReserved: reservationCoins(sdk.NewInt64Coin("aaa", 50)),
			allocation:    reservationCoins(sdk.NewInt64Coin("bbb", 1)),
			charge:        reservationCoins(sdk.NewInt64Coin("bbb", 1)),
			wantMessage:   "total reservation does not contain lease allocation",
		},
		{
			name:          "balance amount below total",
			balance:       reservationCoins(sdk.NewInt64Coin("umfx", 99)),
			totalReserved: reservationCoins(sdk.NewInt64Coin("umfx", 100)),
			allocation:    reservationCoins(sdk.NewInt64Coin("umfx", 50)),
			charge:        reservationCoins(sdk.NewInt64Coin("umfx", 1)),
			wantMessage:   "credit balance does not back total reservation",
		},
		{
			name:          "reserved denom absent from balance",
			balance:       reservationCoins(sdk.NewInt64Coin("aaa", 100)),
			totalReserved: reservationCoins(sdk.NewInt64Coin("bbb", 1)),
			allocation:    sdk.NewCoins(),
			charge:        reservationCoins(sdk.NewInt64Coin("aaa", 1)),
			wantMessage:   "credit balance does not back total reservation",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := types.PlanReservationSpend(
				tc.balance,
				tc.totalReserved,
				tc.allocation,
				tc.charge,
			)
			require.ErrorIs(t, err, types.ErrReservationInvariant)
			require.ErrorContains(t, err, tc.wantMessage)
		})
	}
}

func TestPlanReservationSpend_RejectsMalformedCoinsWithoutPanic(t *testing.T) {
	valid := reservationCoins(sdk.NewInt64Coin("umfx", 10))
	malformed := []struct {
		name  string
		coins sdk.Coins
	}{
		{
			name:  "nil amount",
			coins: sdk.Coins{{Denom: "umfx"}},
		},
		{
			name: "unsorted",
			coins: sdk.Coins{
				sdk.NewInt64Coin("bbb", 1),
				sdk.NewInt64Coin("aaa", 1),
			},
		},
		{
			name: "duplicate denom",
			coins: sdk.Coins{
				sdk.NewInt64Coin("umfx", 1),
				sdk.NewInt64Coin("umfx", 2),
			},
		},
	}

	for _, malformedInput := range malformed {
		for inputIndex, inputName := range []string{"balance", "total reserved", "allocation", "charge"} {
			t.Run(malformedInput.name+"/"+inputName, func(t *testing.T) {
				inputs := [4]sdk.Coins{valid, valid, valid, valid}
				inputs[inputIndex] = malformedInput.coins

				var err error
				require.NotPanics(t, func() {
					_, err = types.PlanReservationSpend(inputs[0], inputs[1], inputs[2], inputs[3])
				})
				require.ErrorIs(t, err, types.ErrReservationInvariant)
				require.ErrorContains(t, err, "invalid "+inputName+" coin set")
			})
		}
	}
}
