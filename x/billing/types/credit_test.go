package types_test

import (
	"bytes"
	"math/big"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

func maxBillingInt() math.Int {
	value := new(big.Int).Lsh(big.NewInt(1), 256)
	value.Sub(value, big.NewInt(1))
	return math.NewIntFromBigInt(value)
}

func highBitBillingInt() math.Int {
	return math.NewIntFromBigInt(new(big.Int).Lsh(big.NewInt(1), 255))
}

// ============================================================================
// GetAvailableCredit Tests
// ============================================================================

func TestGetAvailableCredit(t *testing.T) {
	tests := []struct {
		name     string
		balance  sdk.Coins
		reserved sdk.Coins
		expected sdk.Coins
	}{
		{
			name:     "no reservations - full balance available",
			balance:  sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(1000))),
			reserved: sdk.NewCoins(),
			expected: sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(1000))),
		},
		{
			name:     "partial reservation - remaining available",
			balance:  sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(1000))),
			reserved: sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(300))),
			expected: sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(700))),
		},
		{
			name:     "full reservation - zero available",
			balance:  sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(1000))),
			reserved: sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(1000))),
			expected: sdk.NewCoins(),
		},
		{
			name:     "over reservation (shouldn't happen) - zero available",
			balance:  sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(1000))),
			reserved: sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(1500))),
			expected: sdk.NewCoins(),
		},
		{
			name: "multi-denom - different availability",
			balance: sdk.NewCoins(
				sdk.NewCoin("upwr", math.NewInt(1000)),
				sdk.NewCoin("uatom", math.NewInt(500)),
			),
			reserved: sdk.NewCoins(
				sdk.NewCoin("upwr", math.NewInt(300)),
				sdk.NewCoin("uatom", math.NewInt(100)),
			),
			expected: sdk.NewCoins(
				sdk.NewCoin("upwr", math.NewInt(700)),
				sdk.NewCoin("uatom", math.NewInt(400)),
			),
		},
		{
			name: "reserved denom not in balance - ignored",
			balance: sdk.NewCoins(
				sdk.NewCoin("upwr", math.NewInt(1000)),
			),
			reserved: sdk.NewCoins(
				sdk.NewCoin("upwr", math.NewInt(300)),
				sdk.NewCoin("uatom", math.NewInt(100)), // Not in balance
			),
			expected: sdk.NewCoins(
				sdk.NewCoin("upwr", math.NewInt(700)),
			),
		},
		{
			name: "balance denom not reserved - full availability",
			balance: sdk.NewCoins(
				sdk.NewCoin("upwr", math.NewInt(1000)),
				sdk.NewCoin("uatom", math.NewInt(500)), // Not reserved
			),
			reserved: sdk.NewCoins(
				sdk.NewCoin("upwr", math.NewInt(300)),
			),
			expected: sdk.NewCoins(
				sdk.NewCoin("upwr", math.NewInt(700)),
				sdk.NewCoin("uatom", math.NewInt(500)),
			),
		},
		{
			name:     "empty balance - nothing available",
			balance:  sdk.NewCoins(),
			reserved: sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(100))),
			expected: sdk.NewCoins(),
		},
		{
			name:     "nil coins - empty result",
			balance:  nil,
			reserved: nil,
			expected: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := types.GetAvailableCredit(tc.balance, tc.reserved)
			require.Equal(t, tc.expected, result)
		})
	}
}

// ============================================================================
// AddReservation Tests
// ============================================================================

func TestAddReservation(t *testing.T) {
	tests := []struct {
		name     string
		reserved sdk.Coins
		toAdd    sdk.Coins
		expected sdk.Coins
	}{
		{
			name:     "add to empty reservation",
			reserved: sdk.NewCoins(),
			toAdd:    sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(100))),
			expected: sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(100))),
		},
		{
			name:     "add to existing reservation - same denom",
			reserved: sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(100))),
			toAdd:    sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(50))),
			expected: sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(150))),
		},
		{
			name:     "add different denom",
			reserved: sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(100))),
			toAdd:    sdk.NewCoins(sdk.NewCoin("uatom", math.NewInt(50))),
			expected: sdk.NewCoins(sdk.NewCoin("uatom", math.NewInt(50)), sdk.NewCoin("upwr", math.NewInt(100))),
		},
		{
			name: "add multi-denom",
			reserved: sdk.NewCoins(
				sdk.NewCoin("upwr", math.NewInt(100)),
			),
			toAdd: sdk.NewCoins(
				sdk.NewCoin("upwr", math.NewInt(50)),
				sdk.NewCoin("uatom", math.NewInt(30)),
			),
			expected: sdk.NewCoins(
				sdk.NewCoin("uatom", math.NewInt(30)),
				sdk.NewCoin("upwr", math.NewInt(150)),
			),
		},
		{
			name:     "add nothing",
			reserved: sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(100))),
			toAdd:    sdk.NewCoins(),
			expected: sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(100))),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := types.AddReservation(tc.reserved, tc.toAdd)
			require.NoError(t, err)
			require.Equal(t, tc.expected, result)
		})
	}
}

func TestAddReservation_CheckedBoundary(t *testing.T) {
	maxAmount := maxBillingInt()
	nearMax, err := maxAmount.SafeSub(math.OneInt())
	require.NoError(t, err)

	result, err := types.AddReservation(
		sdk.NewCoins(sdk.NewCoin("upwr", nearMax)),
		sdk.NewCoins(sdk.NewCoin("upwr", math.OneInt())),
	)
	require.NoError(t, err)
	require.Equal(t, maxAmount, result.AmountOf("upwr"))

	require.NotPanics(t, func() {
		_, err = types.AddReservation(result, sdk.NewCoins(sdk.NewCoin("upwr", math.OneInt())))
	})
	require.ErrorIs(t, err, types.ErrArithmeticOverflow)
}

func TestSafeAddCoins_ExactMaximumAndOverflow(t *testing.T) {
	maxAmount := maxBillingInt()
	nearMax, err := maxAmount.SafeSub(math.OneInt())
	require.NoError(t, err)

	result, err := types.SafeAddCoins(
		sdk.NewCoins(sdk.NewCoin("upwr", nearMax)),
		sdk.NewCoins(sdk.NewCoin("upwr", math.OneInt())),
	)
	require.NoError(t, err)
	require.Equal(t, maxAmount, result.AmountOf("upwr"))

	require.NotPanics(t, func() {
		_, err = types.SafeAddCoins(result, sdk.NewCoins(sdk.NewCoin("upwr", math.OneInt())))
	})
	require.ErrorIs(t, err, types.ErrArithmeticOverflow)
}

func TestSafeAggregateCoins(t *testing.T) {
	input := []sdk.Coin{
		sdk.NewInt64Coin("zeta", 4),
		sdk.NewInt64Coin("alpha", 2),
		sdk.NewInt64Coin("zeta", 3),
		sdk.NewInt64Coin("beta", 5),
		sdk.NewInt64Coin("alpha", 6),
	}
	original := append([]sdk.Coin(nil), input...)

	result, err := types.SafeAggregateCoins(input)
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoins(
		sdk.NewInt64Coin("alpha", 8),
		sdk.NewInt64Coin("beta", 5),
		sdk.NewInt64Coin("zeta", 7),
	), result)
	require.Equal(t, original, input, "aggregation must not reorder the caller's slice")

	empty, err := types.SafeAggregateCoins(nil)
	require.NoError(t, err)
	require.NotNil(t, empty)
	require.Empty(t, empty)
}

func TestSafeAggregateCoinsRejectsMalformedAndOverflowingInput(t *testing.T) {
	_, err := types.SafeAggregateCoins([]sdk.Coin{sdk.NewInt64Coin("upwr", 0)})
	require.ErrorIs(t, err, types.ErrInvalidCreditOperation)

	_, err = types.SafeAggregateCoins([]sdk.Coin{{Denom: "upwr"}})
	require.ErrorIs(t, err, types.ErrInvalidCreditOperation)

	require.NotPanics(t, func() {
		_, err = types.SafeAggregateCoins([]sdk.Coin{
			sdk.NewCoin("upwr", maxBillingInt()),
			sdk.NewInt64Coin("upwr", 1),
		})
	})
	require.ErrorIs(t, err, types.ErrArithmeticOverflow)
}

func TestSafeSubtractCoins(t *testing.T) {
	left := sdk.NewCoins(
		sdk.NewInt64Coin("alpha", 7),
		sdk.NewInt64Coin("beta", 5),
		sdk.NewInt64Coin("gamma", 3),
	)

	result, err := types.SafeSubtractCoins(left, sdk.NewCoins(
		sdk.NewInt64Coin("alpha", 2),
		sdk.NewInt64Coin("beta", 5),
	))
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoins(
		sdk.NewInt64Coin("alpha", 5),
		sdk.NewInt64Coin("gamma", 3),
	), result)

	tests := []struct {
		name  string
		right sdk.Coins
	}{
		{name: "amount underflow", right: sdk.NewCoins(sdk.NewInt64Coin("alpha", 8))},
		{name: "absent denom", right: sdk.NewCoins(sdk.NewInt64Coin("delta", 1))},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := types.SafeSubtractCoins(left, test.right)
			require.ErrorIs(t, err, types.ErrInvalidCreditOperation)
		})
	}
}

// ============================================================================
// SubtractReservation Tests
// ============================================================================

func TestSubtractReservation(t *testing.T) {
	tests := []struct {
		name       string
		reserved   sdk.Coins
		toSubtract sdk.Coins
		expected   sdk.Coins
	}{
		{
			name:       "subtract from existing reservation",
			reserved:   sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(100))),
			toSubtract: sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(30))),
			expected:   sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(70))),
		},
		{
			name:       "subtract entire reservation",
			reserved:   sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(100))),
			toSubtract: sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(100))),
			expected:   sdk.NewCoins(),
		},
		{
			name:       "subtract more than reserved - capped at zero",
			reserved:   sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(100))),
			toSubtract: sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(150))),
			expected:   sdk.NewCoins(),
		},
		{
			name:       "subtract non-existent denom - no change",
			reserved:   sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(100))),
			toSubtract: sdk.NewCoins(sdk.NewCoin("uatom", math.NewInt(50))),
			expected:   sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(100))),
		},
		{
			name: "subtract from multi-denom",
			reserved: sdk.NewCoins(
				sdk.NewCoin("upwr", math.NewInt(100)),
				sdk.NewCoin("uatom", math.NewInt(50)),
			),
			toSubtract: sdk.NewCoins(
				sdk.NewCoin("upwr", math.NewInt(30)),
				sdk.NewCoin("uatom", math.NewInt(50)),
			),
			expected: sdk.NewCoins(
				sdk.NewCoin("upwr", math.NewInt(70)),
			),
		},
		{
			name:       "subtract from empty - no panic",
			reserved:   sdk.NewCoins(),
			toSubtract: sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(50))),
			expected:   sdk.NewCoins(),
		},
		{
			name:       "subtract nothing",
			reserved:   sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(100))),
			toSubtract: sdk.NewCoins(),
			expected:   sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(100))),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := types.SubtractReservation(tc.reserved, tc.toSubtract)
			require.Equal(t, tc.expected, result)
		})
	}
}

// ============================================================================
// CalculateLeaseReservation Tests
// ============================================================================

func TestCalculateLeaseReservation(t *testing.T) {
	tests := []struct {
		name             string
		items            []types.LeaseItem
		minLeaseDuration uint64
		expected         sdk.Coins
	}{
		{
			name: "single item - simple calculation",
			items: []types.LeaseItem{
				{
					SkuUuid:     "01912345-6789-7abc-8def-0123456789ab",
					Quantity:    1,
					LockedPrice: sdk.NewCoin("upwr", math.NewInt(10)), // 10 per second
				},
			},
			minLeaseDuration: 3600,                                                  // 1 hour
			expected:         sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(36000))), // 10 * 1 * 3600
		},
		{
			name: "single item - multiple quantity",
			items: []types.LeaseItem{
				{
					SkuUuid:     "01912345-6789-7abc-8def-0123456789ab",
					Quantity:    5,
					LockedPrice: sdk.NewCoin("upwr", math.NewInt(10)), // 10 per second per unit
				},
			},
			minLeaseDuration: 3600,                                                   // 1 hour
			expected:         sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(180000))), // 10 * 5 * 3600
		},
		{
			name: "multiple items - same denom",
			items: []types.LeaseItem{
				{
					SkuUuid:     "01912345-6789-7abc-8def-0123456789ab",
					Quantity:    2,
					LockedPrice: sdk.NewCoin("upwr", math.NewInt(10)),
				},
				{
					SkuUuid:     "01912345-6789-7abc-8def-0123456789ac",
					Quantity:    3,
					LockedPrice: sdk.NewCoin("upwr", math.NewInt(20)),
				},
			},
			minLeaseDuration: 3600,
			expected:         sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(288000))), // (10*2 + 20*3) * 3600 = 80 * 3600
		},
		{
			name: "multiple items - different denoms",
			items: []types.LeaseItem{
				{
					SkuUuid:     "01912345-6789-7abc-8def-0123456789ab",
					Quantity:    1,
					LockedPrice: sdk.NewCoin("upwr", math.NewInt(10)),
				},
				{
					SkuUuid:     "01912345-6789-7abc-8def-0123456789ac",
					Quantity:    2,
					LockedPrice: sdk.NewCoin("uatom", math.NewInt(5)),
				},
			},
			minLeaseDuration: 3600,
			expected: sdk.NewCoins(
				sdk.NewCoin("uatom", math.NewInt(36000)), // 5 * 2 * 3600
				sdk.NewCoin("upwr", math.NewInt(36000)),  // 10 * 1 * 3600
			),
		},
		{
			name:             "empty items - zero reservation",
			items:            []types.LeaseItem{},
			minLeaseDuration: 3600,
			expected:         sdk.NewCoins(),
		},
		{
			name: "zero duration - zero reservation",
			items: []types.LeaseItem{
				{
					SkuUuid:     "01912345-6789-7abc-8def-0123456789ab",
					Quantity:    1,
					LockedPrice: sdk.NewCoin("upwr", math.NewInt(10)),
				},
			},
			minLeaseDuration: 0,
			expected:         sdk.NewCoins(),
		},
		{
			name:             "nil items - zero reservation",
			items:            nil,
			minLeaseDuration: 3600,
			expected:         sdk.NewCoins(),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := types.CalculateLeaseReservation(tc.items, tc.minLeaseDuration)
			require.NoError(t, err)
			require.Equal(t, tc.expected, result)
		})
	}
}

// ============================================================================
// CalculateLeaseReservationFromRates Tests
// ============================================================================

func TestCalculateLeaseReservationFromRates(t *testing.T) {
	tests := []struct {
		name                string
		totalRatesPerSecond sdk.Coins
		minLeaseDuration    uint64
		expected            sdk.Coins
	}{
		{
			name:                "single denom rate",
			totalRatesPerSecond: sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(100))),
			minLeaseDuration:    3600,
			expected:            sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(360000))),
		},
		{
			name: "multi denom rates",
			totalRatesPerSecond: sdk.NewCoins(
				sdk.NewCoin("upwr", math.NewInt(100)),
				sdk.NewCoin("uatom", math.NewInt(50)),
			),
			minLeaseDuration: 3600,
			expected: sdk.NewCoins(
				sdk.NewCoin("uatom", math.NewInt(180000)),
				sdk.NewCoin("upwr", math.NewInt(360000)),
			),
		},
		{
			name:                "zero rates - zero reservation",
			totalRatesPerSecond: sdk.NewCoins(),
			minLeaseDuration:    3600,
			expected:            sdk.NewCoins(),
		},
		{
			name:                "zero duration - zero reservation",
			totalRatesPerSecond: sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(100))),
			minLeaseDuration:    0,
			expected:            sdk.NewCoins(),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := types.CalculateLeaseReservationFromRates(tc.totalRatesPerSecond, tc.minLeaseDuration)
			require.NoError(t, err)
			require.Equal(t, tc.expected, result)
		})
	}
}

func TestCalculateLeaseReservation_CheckedBoundaries(t *testing.T) {
	highBit := highBitBillingInt()

	t.Run("stored pricing business invariants are enforced", func(t *testing.T) {
		tests := []struct {
			name             string
			item             types.LeaseItem
			minLeaseDuration uint64
			expected         error
		}{
			{
				name:             "zero quantity",
				item:             types.LeaseItem{SkuUuid: "sku-zero", Quantity: 0, LockedPrice: sdk.NewCoin("upwr", math.OneInt())},
				minLeaseDuration: 1,
				expected:         types.ErrInvalidQuantity,
			},
			{
				name:             "quantity above maximum",
				item:             types.LeaseItem{SkuUuid: "sku-too-many", Quantity: types.MaxQuantityPerItem + 1, LockedPrice: sdk.NewCoin("upwr", math.OneInt())},
				minLeaseDuration: 1,
				expected:         types.ErrInvalidQuantity,
			},
			{
				name:             "zero price even at zero reservation duration",
				item:             types.LeaseItem{SkuUuid: "sku-zero-price", Quantity: 1, LockedPrice: sdk.NewCoin("upwr", math.ZeroInt())},
				minLeaseDuration: 0,
				expected:         types.ErrInvalidCreditOperation,
			},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				require.NotPanics(t, func() {
					_, err := types.CalculateLeaseReservation([]types.LeaseItem{tc.item}, tc.minLeaseDuration)
					require.ErrorIs(t, err, tc.expected)
				})
			})
		}
	})

	t.Run("quantity multiplication overflow", func(t *testing.T) {
		items := []types.LeaseItem{{
			SkuUuid:     "sku-overflow",
			Quantity:    2,
			LockedPrice: sdk.NewCoin("upwr", highBit),
		}}

		require.NotPanics(t, func() {
			_, err := types.CalculateLeaseReservation(items, 1)
			require.ErrorIs(t, err, types.ErrArithmeticOverflow)
		})
	})

	t.Run("duration multiplication overflow", func(t *testing.T) {
		rates := sdk.NewCoins(sdk.NewCoin("upwr", highBit))

		require.NotPanics(t, func() {
			_, err := types.CalculateLeaseReservationFromRates(rates, 2)
			require.ErrorIs(t, err, types.ErrArithmeticOverflow)
		})
	})

	t.Run("near maximum succeeds", func(t *testing.T) {
		nearHalf, err := highBit.SafeSub(math.OneInt())
		require.NoError(t, err)
		rates := sdk.NewCoins(sdk.NewCoin("upwr", nearHalf))

		reservation, err := types.CalculateLeaseReservationFromRates(rates, 2)
		require.NoError(t, err)
		expected, err := nearHalf.SafeMul(math.NewInt(2))
		require.NoError(t, err)
		require.Equal(t, expected, reservation.AmountOf("upwr"))
	})

	t.Run("precomputed rates must be canonical", func(t *testing.T) {
		unsortedRates := sdk.Coins{
			sdk.NewCoin("upwr", math.OneInt()),
			sdk.NewCoin("uatom", math.OneInt()),
		}

		_, err := types.CalculateLeaseReservationFromRates(unsortedRates, 1)
		require.ErrorIs(t, err, types.ErrInvalidCreditOperation)
	})
}

// ============================================================================
// Integration Scenarios
// ============================================================================

func TestReservationScenario_PreventOverbooking(t *testing.T) {
	// Scenario: Tenant has 100 credits, min_lease_duration = 1 second (for simplicity)
	// Each lease reserves 30 credits
	// Lease A: 30 credits reserved
	// Lease B: 30 credits reserved
	// Lease C: 30 credits reserved
	// Lease D: 30 credits needed but only 10 available (should fail)

	balance := sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(100)))

	// Use 30 credit per second rate, 1 second duration = 30 reservation each
	ratePerSecond := math.NewInt(30)
	duration := uint64(1) // 1 second for simplicity

	reserved := sdk.NewCoins()

	// Lease A creation: need 30, have 100 available
	leaseAItems := []types.LeaseItem{{SkuUuid: "a", Quantity: 1, LockedPrice: sdk.NewCoin("upwr", ratePerSecond)}}
	leaseAReservation, err := types.CalculateLeaseReservation(leaseAItems, duration)
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(30))), leaseAReservation)

	available := types.GetAvailableCredit(balance, reserved)
	require.True(t, available.AmountOf("upwr").GTE(leaseAReservation.AmountOf("upwr")))
	reserved, err = types.AddReservation(reserved, leaseAReservation)
	require.NoError(t, err)
	// Reserved: 30, Available: 70

	// Lease B creation: need 30, have 70 available
	leaseBItems := []types.LeaseItem{{SkuUuid: "b", Quantity: 1, LockedPrice: sdk.NewCoin("upwr", ratePerSecond)}}
	leaseBReservation, err := types.CalculateLeaseReservation(leaseBItems, duration)
	require.NoError(t, err)

	available = types.GetAvailableCredit(balance, reserved)
	require.Equal(t, math.NewInt(70), available.AmountOf("upwr"))
	require.True(t, available.AmountOf("upwr").GTE(leaseBReservation.AmountOf("upwr")))
	reserved, err = types.AddReservation(reserved, leaseBReservation)
	require.NoError(t, err)
	// Reserved: 60, Available: 40

	// Lease C creation: need 30, have 40 available
	leaseCItems := []types.LeaseItem{{SkuUuid: "c", Quantity: 1, LockedPrice: sdk.NewCoin("upwr", ratePerSecond)}}
	leaseCReservation, err := types.CalculateLeaseReservation(leaseCItems, duration)
	require.NoError(t, err)

	available = types.GetAvailableCredit(balance, reserved)
	require.Equal(t, math.NewInt(40), available.AmountOf("upwr"))
	require.True(t, available.AmountOf("upwr").GTE(leaseCReservation.AmountOf("upwr")))
	reserved, err = types.AddReservation(reserved, leaseCReservation)
	require.NoError(t, err)
	// Reserved: 90, Available: 10

	// Lease D creation: need 30, have 10 available - SHOULD FAIL
	leaseDItems := []types.LeaseItem{{SkuUuid: "d", Quantity: 1, LockedPrice: sdk.NewCoin("upwr", ratePerSecond)}}
	leaseDReservation, err := types.CalculateLeaseReservation(leaseDItems, duration)
	require.NoError(t, err)

	available = types.GetAvailableCredit(balance, reserved)
	require.Equal(t, math.NewInt(10), available.AmountOf("upwr"))
	require.False(t, available.AmountOf("upwr").GTE(leaseDReservation.AmountOf("upwr")))
	// Lease D would be rejected due to insufficient available credit
}

func TestReservationScenario_ReleaseOnClose(t *testing.T) {
	// Scenario: After closing a lease, its reservation should be released
	balance := sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(100)))
	ratePerSecond := math.NewInt(40)
	duration := uint64(1)

	reserved := sdk.NewCoins()

	// Lease A creation: reserve 40
	leaseAItems := []types.LeaseItem{{SkuUuid: "a", Quantity: 1, LockedPrice: sdk.NewCoin("upwr", ratePerSecond)}}
	leaseAReservation, err := types.CalculateLeaseReservation(leaseAItems, duration)
	require.NoError(t, err)
	reserved, err = types.AddReservation(reserved, leaseAReservation)
	require.NoError(t, err)

	// Lease B creation: reserve 40 (total reserved: 80, available: 20)
	leaseBItems := []types.LeaseItem{{SkuUuid: "b", Quantity: 1, LockedPrice: sdk.NewCoin("upwr", ratePerSecond)}}
	leaseBReservation, err := types.CalculateLeaseReservation(leaseBItems, duration)
	require.NoError(t, err)
	reserved, err = types.AddReservation(reserved, leaseBReservation)
	require.NoError(t, err)

	available := types.GetAvailableCredit(balance, reserved)
	require.Equal(t, math.NewInt(20), available.AmountOf("upwr"))

	// Lease C would fail: need 40, only 20 available
	leaseCItems := []types.LeaseItem{{SkuUuid: "c", Quantity: 1, LockedPrice: sdk.NewCoin("upwr", ratePerSecond)}}
	leaseCReservation, err := types.CalculateLeaseReservation(leaseCItems, duration)
	require.NoError(t, err)
	require.False(t, available.AmountOf("upwr").GTE(leaseCReservation.AmountOf("upwr")))

	// Close Lease A: release its reservation
	reserved = types.SubtractReservation(reserved, leaseAReservation)

	// Now available: 100 - 40 = 60
	available = types.GetAvailableCredit(balance, reserved)
	require.Equal(t, math.NewInt(60), available.AmountOf("upwr"))

	// Lease C can now be created: need 40, have 60 available
	require.True(t, available.AmountOf("upwr").GTE(leaseCReservation.AmountOf("upwr")))
}

// ============================================================================
// GetLeaseReservationAmount Tests
// ============================================================================

func TestGetLeaseReservationAmount(t *testing.T) {
	currentMinDuration := uint64(3600)
	ratePerSecond := math.NewInt(10)

	tests := []struct {
		name           string
		lease          types.Lease
		minLeaseDur    uint64
		expectedAmount math.Int
		expectedDenom  string
	}{
		{
			name: "uses stored min_lease_duration when available",
			lease: types.Lease{
				Items: []types.LeaseItem{
					{SkuUuid: "sku1", Quantity: 1, LockedPrice: sdk.NewCoin("upwr", ratePerSecond)},
				},
				// Stored at creation with different min_lease_duration (7200)
				MinLeaseDurationAtCreation: 7200,
			},
			minLeaseDur:    currentMinDuration, // Current param is 3600, but stored is 7200
			expectedAmount: math.NewInt(72000), // 10 * 7200 = 72000 (uses stored)
			expectedDenom:  "upwr",
		},
		{
			name: "falls back to current param for legacy lease without stored duration",
			lease: types.Lease{
				Items: []types.LeaseItem{
					{SkuUuid: "sku1", Quantity: 1, LockedPrice: sdk.NewCoin("upwr", ratePerSecond)},
				},
				MinLeaseDurationAtCreation: 0, // Zero - legacy lease
			},
			minLeaseDur:    currentMinDuration,
			expectedAmount: math.NewInt(36000), // 10 * 3600 = 36000 (uses current)
			expectedDenom:  "upwr",
		},
		{
			name: "multi-item lease with stored duration",
			lease: types.Lease{
				Items: []types.LeaseItem{
					{SkuUuid: "sku1", Quantity: 2, LockedPrice: sdk.NewCoin("upwr", ratePerSecond)},
					{SkuUuid: "sku2", Quantity: 1, LockedPrice: sdk.NewCoin("umfx", math.NewInt(5))},
				},
				MinLeaseDurationAtCreation: 3600,
			},
			minLeaseDur:    currentMinDuration,
			expectedAmount: math.NewInt(72000), // (10 * 2) * 3600 = 72000 for upwr
			expectedDenom:  "upwr",
		},
		{
			name: "multi-denom calculation",
			lease: types.Lease{
				Items: []types.LeaseItem{
					{SkuUuid: "sku1", Quantity: 1, LockedPrice: sdk.NewCoin("upwr", ratePerSecond)},
					{SkuUuid: "sku2", Quantity: 1, LockedPrice: sdk.NewCoin("umfx", math.NewInt(5))},
				},
				MinLeaseDurationAtCreation: 3600,
			},
			minLeaseDur:    currentMinDuration,
			expectedAmount: math.NewInt(18000), // 5 * 1 * 3600 = 18000 for umfx
			expectedDenom:  "umfx",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := types.GetLeaseReservationAmount(&tc.lease, tc.minLeaseDur)
			require.NoError(t, err)
			require.Equal(t, tc.expectedAmount, result.AmountOf(tc.expectedDenom))
		})
	}
}

// TestGetLeaseReservationAmount_ParamChangeScenario tests the fix for M-01 vulnerability.
// Ensures that parameter changes don't cause inconsistent reservation accounting.
func TestGetLeaseReservationAmount_ParamChangeScenario(t *testing.T) {
	// Scenario: MinLeaseDuration changes between lease creation and closure
	ratePerSecond := math.NewInt(10)

	// At creation time: MinLeaseDuration = 3600
	originalMinDuration := uint64(3600)
	items := []types.LeaseItem{
		{SkuUuid: "sku1", Quantity: 1, LockedPrice: sdk.NewCoin("upwr", ratePerSecond)},
	}

	// Calculate expected reservation at creation
	reservationAtCreation, err := types.CalculateLeaseReservation(items, originalMinDuration)
	require.NoError(t, err)
	require.Equal(t, math.NewInt(36000), reservationAtCreation.AmountOf("upwr")) // 10 * 3600

	// Create lease with stored min_lease_duration
	lease := types.Lease{
		Items:                      items,
		MinLeaseDurationAtCreation: originalMinDuration, // Store the duration, not the calculated amount
	}

	// Governance changes MinLeaseDuration to 1800
	newMinDuration := uint64(1800)

	// At closure time: GetLeaseReservationAmount should use STORED duration
	releaseAmount, err := types.GetLeaseReservationAmount(&lease, newMinDuration)
	require.NoError(t, err)

	// Should calculate using stored duration (3600), not current (1800)
	require.Equal(t, math.NewInt(36000), releaseAmount.AmountOf("upwr"))

	// Verify that using current param would give wrong answer
	wrongAmount, err := types.CalculateLeaseReservation(items, newMinDuration)
	require.NoError(t, err)
	require.Equal(t, math.NewInt(18000), wrongAmount.AmountOf("upwr")) // 10 * 1800 = wrong!

	// The stored duration ensures correct release
	require.NotEqual(t, releaseAmount, wrongAmount)
}

// ============================================================================
// CheckReservationRelease Tests
// ============================================================================

func TestCheckReservationRelease(t *testing.T) {
	tests := []struct {
		name              string
		reserved          sdk.Coins
		toRelease         sdk.Coins
		expectedUnderflow map[string]math.Int
	}{
		{
			name:              "no underflow - exact match",
			reserved:          sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(1000))),
			toRelease:         sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(1000))),
			expectedUnderflow: map[string]math.Int{},
		},
		{
			name:              "no underflow - releasing less than reserved",
			reserved:          sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(1000))),
			toRelease:         sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(500))),
			expectedUnderflow: map[string]math.Int{},
		},
		{
			name:              "underflow - releasing more than reserved",
			reserved:          sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(500))),
			toRelease:         sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(1000))),
			expectedUnderflow: map[string]math.Int{"upwr": math.NewInt(500)},
		},
		{
			name:              "underflow - denom not in reserved",
			reserved:          sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(1000))),
			toRelease:         sdk.NewCoins(sdk.NewCoin("uatom", math.NewInt(500))),
			expectedUnderflow: map[string]math.Int{"uatom": math.NewInt(500)},
		},
		{
			name:     "multi-denom - partial underflow",
			reserved: sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(1000)), sdk.NewCoin("uatom", math.NewInt(200))),
			toRelease: sdk.NewCoins(
				sdk.NewCoin("upwr", math.NewInt(500)),  // OK
				sdk.NewCoin("uatom", math.NewInt(500)), // Underflow by 300
			),
			expectedUnderflow: map[string]math.Int{"uatom": math.NewInt(300)},
		},
		{
			name:              "empty reserved - any release is underflow",
			reserved:          sdk.NewCoins(),
			toRelease:         sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(100))),
			expectedUnderflow: map[string]math.Int{"upwr": math.NewInt(100)},
		},
		{
			name:              "empty release - no underflow",
			reserved:          sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(1000))),
			toRelease:         sdk.NewCoins(),
			expectedUnderflow: map[string]math.Int{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := types.CheckReservationRelease(tc.reserved, tc.toRelease)

			require.Equal(t, len(tc.expectedUnderflow), len(result),
				"expected %d underflows, got %d", len(tc.expectedUnderflow), len(result))

			for denom, expectedAmount := range tc.expectedUnderflow {
				actualAmount, ok := result[denom]
				require.True(t, ok, "expected underflow for denom %s", denom)
				require.True(t, expectedAmount.Equal(actualAmount),
					"denom %s: expected underflow %s, got %s", denom, expectedAmount.String(), actualAmount.String())
			}
		})
	}
}

// ============================================================================
// CalculateExpectedReservationsByTenant Tests
// ============================================================================

func TestCalculateExpectedReservationsByTenant(t *testing.T) {
	tenant1 := sdk.AccAddress(bytes.Repeat([]byte{1}, 20)).String()
	tenant2 := sdk.AccAddress(bytes.Repeat([]byte{2}, 20)).String()
	coins := func(amount int64) sdk.Coins {
		if amount == 0 {
			return sdk.NewCoins()
		}
		return sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(amount)))
	}
	lease := func(
		uuid, tenant string,
		state types.LeaseState,
		minDuration uint64,
		remaining sdk.Coins,
	) types.Lease {
		return types.Lease{
			Uuid:                       uuid,
			Tenant:                     tenant,
			State:                      state,
			MinLeaseDurationAtCreation: minDuration,
			Reservation: &types.LeaseReservation{
				RemainingAmounts: remaining,
			},
		}
	}

	tests := []struct {
		name               string
		leases             []types.Lease
		deprecatedFallback uint64
		expected           map[string]sdk.Coins
	}{
		{
			name:     "empty",
			expected: map[string]sdk.Coins{},
		},
		{
			name: "live modern leases use remaining tranches and ignore fallback",
			leases: []types.Lease{
				lease("active", tenant1, types.LEASE_STATE_ACTIVE, 7200, coins(7)),
				lease("pending", tenant1, types.LEASE_STATE_PENDING, 3600, coins(3)),
			},
			deprecatedFallback: 999_999,
			expected: map[string]sdk.Coins{
				tenant1: coins(10),
			},
		},
		{
			name: "multiple tenants remain separate",
			leases: []types.Lease{
				lease("tenant-1", tenant1, types.LEASE_STATE_ACTIVE, 1, coins(11)),
				lease("tenant-2", tenant2, types.LEASE_STATE_PENDING, 1, coins(13)),
			},
			expected: map[string]sdk.Coins{
				tenant1: coins(11),
				tenant2: coins(13),
			},
		},
		{
			name: "legacy and terminal empty tranches do not contribute",
			leases: []types.Lease{
				lease("legacy-active", tenant1, types.LEASE_STATE_ACTIVE, 0, coins(0)),
				lease("modern-closed", tenant1, types.LEASE_STATE_CLOSED, 1, coins(0)),
				lease("modern-rejected", tenant1, types.LEASE_STATE_REJECTED, 1, coins(0)),
				lease("modern-expired", tenant1, types.LEASE_STATE_EXPIRED, 1, coins(0)),
			},
			expected: map[string]sdk.Coins{},
		},
		{
			name: "equivalent bech32 spellings share canonical tenant identity",
			leases: []types.Lease{
				lease("lowercase", tenant1, types.LEASE_STATE_ACTIVE, 1, coins(17)),
				lease("uppercase", strings.ToUpper(tenant1), types.LEASE_STATE_ACTIVE, 1, coins(19)),
			},
			expected: map[string]sdk.Coins{
				tenant1: coins(36),
			},
		},
		{
			name: "fully consumed live tranche contributes zero",
			leases: []types.Lease{
				lease("consumed", tenant1, types.LEASE_STATE_ACTIVE, 1, coins(0)),
			},
			expected: map[string]sdk.Coins{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := types.CalculateExpectedReservationsByTenant(tc.leases, tc.deprecatedFallback)
			require.NoError(t, err)

			require.Equal(t, len(tc.expected), len(result),
				"expected %d tenants, got %d", len(tc.expected), len(result))

			for tenant, expectedCoins := range tc.expected {
				actualCoins, ok := result[tenant]
				require.True(t, ok, "expected tenant %s in result", tenant)
				require.True(t, expectedCoins.Equal(actualCoins),
					"tenant %s: expected %s, got %s", tenant, expectedCoins.String(), actualCoins.String())
			}
		})
	}
}

func TestCalculateExpectedReservationsByTenantRejectsInvalidV4State(t *testing.T) {
	tenant := sdk.AccAddress(bytes.Repeat([]byte{3}, 20)).String()
	coins := sdk.NewCoins(sdk.NewCoin("upwr", math.OneInt()))
	tests := []struct {
		name  string
		lease types.Lease
	}{
		{
			name: "missing reservation wrapper",
			lease: types.Lease{
				Uuid:                       "missing",
				Tenant:                     tenant,
				State:                      types.LEASE_STATE_ACTIVE,
				MinLeaseDurationAtCreation: 1,
			},
		},
		{
			name: "legacy attributed tranche",
			lease: types.Lease{
				Uuid:        "legacy",
				Tenant:      tenant,
				State:       types.LEASE_STATE_ACTIVE,
				Reservation: &types.LeaseReservation{RemainingAmounts: coins},
			},
		},
		{
			name: "terminal attributed tranche",
			lease: types.Lease{
				Uuid:                       "terminal",
				Tenant:                     tenant,
				State:                      types.LEASE_STATE_CLOSED,
				MinLeaseDurationAtCreation: 1,
				Reservation:                &types.LeaseReservation{RemainingAmounts: coins},
			},
		},
		{
			name: "invalid tenant",
			lease: types.Lease{
				Uuid:                       "bad-tenant",
				Tenant:                     "not-bech32",
				State:                      types.LEASE_STATE_ACTIVE,
				MinLeaseDurationAtCreation: 1,
				Reservation:                &types.LeaseReservation{RemainingAmounts: coins},
			},
		},
		{
			name: "malformed remaining coins",
			lease: types.Lease{
				Uuid:                       "malformed",
				Tenant:                     tenant,
				State:                      types.LEASE_STATE_ACTIVE,
				MinLeaseDurationAtCreation: 1,
				Reservation: &types.LeaseReservation{RemainingAmounts: sdk.Coins{
					sdk.NewCoin("upwr", math.OneInt()),
					sdk.NewCoin("upwr", math.OneInt()),
				}},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := types.CalculateExpectedReservationsByTenant([]types.Lease{tc.lease}, 3600)
			require.Error(t, err)
			require.ErrorIs(t, err, types.ErrReservationInvariant)
		})
	}
}

func TestCalculateExpectedReservationsByTenantReturnsOverflow(t *testing.T) {
	tenant := sdk.AccAddress(bytes.Repeat([]byte{4}, 20)).String()
	lease := func(uuid string) types.Lease {
		return types.Lease{
			Uuid:                       uuid,
			Tenant:                     tenant,
			State:                      types.LEASE_STATE_ACTIVE,
			MinLeaseDurationAtCreation: 1,
			Reservation: &types.LeaseReservation{RemainingAmounts: sdk.NewCoins(
				sdk.NewCoin("upwr", highBitBillingInt()),
			)},
		}
	}

	_, err := types.CalculateExpectedReservationsByTenant(
		[]types.Lease{lease("first"), lease("second")},
		0,
	)
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrArithmeticOverflow)
}
