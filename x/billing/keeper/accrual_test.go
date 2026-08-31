/*
Package keeper contains unit tests for accrual calculation functions.

Test Coverage:
- ConvertBasePriceToPerSecond: price conversion for different units
- CalculateAccruedAmount: accrual calculation for single items
- CalculateTotalAccruedForLease: total accrual for multiple items
- Precision loss scenarios with various price/duration combinations
- Checked overflow protection at the SDK math.Int boundary
- Large value calculations with big integers
*/
package keeper

import (
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/status"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	billingtypes "github.com/manifest-network/manifest-ledger/x/billing/types"
	skutypes "github.com/manifest-network/manifest-ledger/x/sku/types"
)

const testDenom = "upwr"

func highBitAccrualInt() math.Int {
	return math.NewIntFromBigInt(new(big.Int).Lsh(big.NewInt(1), 255))
}

func TestConvertBasePriceToPerSecond(t *testing.T) {
	tests := []struct {
		name      string
		basePrice sdk.Coin
		unit      skutypes.Unit
		expected  sdk.Coin
		expectErr bool
	}{
		{
			name:      "per hour: 3600 -> 1 per second",
			basePrice: sdk.NewCoin(testDenom, math.NewInt(3600)),
			unit:      skutypes.Unit_UNIT_PER_HOUR,
			expected:  sdk.NewCoin(testDenom, math.NewInt(1)),
			expectErr: false,
		},
		{
			name:      "per hour: 7200 -> 2 per second",
			basePrice: sdk.NewCoin(testDenom, math.NewInt(7200)),
			unit:      skutypes.Unit_UNIT_PER_HOUR,
			expected:  sdk.NewCoin(testDenom, math.NewInt(2)),
			expectErr: false,
		},
		{
			name:      "per day: 86400 -> 1 per second",
			basePrice: sdk.NewCoin(testDenom, math.NewInt(86400)),
			unit:      skutypes.Unit_UNIT_PER_DAY,
			expected:  sdk.NewCoin(testDenom, math.NewInt(1)),
			expectErr: false,
		},
		{
			name:      "per day: 172800 -> 2 per second",
			basePrice: sdk.NewCoin(testDenom, math.NewInt(172800)),
			unit:      skutypes.Unit_UNIT_PER_DAY,
			expected:  sdk.NewCoin(testDenom, math.NewInt(2)),
			expectErr: false,
		},
		{
			name:      "unspecified: returns error (invalid unit)",
			basePrice: sdk.NewCoin(testDenom, math.NewInt(100)),
			unit:      skutypes.Unit_UNIT_UNSPECIFIED,
			expectErr: true,
		},
		{
			name:      "per hour: small amount returns error (zero rate)",
			basePrice: sdk.NewCoin(testDenom, math.NewInt(100)),
			unit:      skutypes.Unit_UNIT_PER_HOUR,
			expectErr: true, // 100/3600 = 0, which is invalid
		},
		{
			name:      "per hour: large amount",
			basePrice: sdk.NewCoin(testDenom, math.NewInt(36000000)),
			unit:      skutypes.Unit_UNIT_PER_HOUR,
			expected:  sdk.NewCoin(testDenom, math.NewInt(10000)),
			expectErr: false,
		},
		{
			name:      "per hour: inexact division returns error",
			basePrice: sdk.NewCoin(testDenom, math.NewInt(3601)), // Not evenly divisible by 3600
			unit:      skutypes.Unit_UNIT_PER_HOUR,
			expectErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := ConvertBasePriceToPerSecond(tc.basePrice, tc.unit)
			if tc.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.True(t, tc.expected.IsEqual(result), "expected %s, got %s", tc.expected, result)
			}
		})
	}
}

func TestCalculateAccruedAmount(t *testing.T) {
	tests := []struct {
		name                 string
		lockedPricePerSecond sdk.Coin
		quantity             uint64
		duration             time.Duration
		expected             sdk.Coin
		expectErr            bool
	}{
		{
			name:                 "1 per second, 1 quantity, 100 seconds",
			lockedPricePerSecond: sdk.NewCoin(testDenom, math.NewInt(1)),
			quantity:             1,
			duration:             100 * time.Second,
			expected:             sdk.NewCoin(testDenom, math.NewInt(100)),
		},
		{
			name:                 "1 per second, 5 quantity, 100 seconds",
			lockedPricePerSecond: sdk.NewCoin(testDenom, math.NewInt(1)),
			quantity:             5,
			duration:             100 * time.Second,
			expected:             sdk.NewCoin(testDenom, math.NewInt(500)),
		},
		{
			name:                 "10 per second, 2 quantity, 60 seconds",
			lockedPricePerSecond: sdk.NewCoin(testDenom, math.NewInt(10)),
			quantity:             2,
			duration:             60 * time.Second,
			expected:             sdk.NewCoin(testDenom, math.NewInt(1200)),
		},
		{
			name:                 "zero duration",
			lockedPricePerSecond: sdk.NewCoin(testDenom, math.NewInt(100)),
			quantity:             5,
			duration:             0,
			expected:             sdk.NewCoin(testDenom, math.NewInt(0)),
		},
		{
			name:                 "negative duration",
			lockedPricePerSecond: sdk.NewCoin(testDenom, math.NewInt(100)),
			quantity:             5,
			duration:             -10 * time.Second,
			expected:             sdk.NewCoin(testDenom, math.NewInt(0)),
		},
		{
			name:                 "zero price",
			lockedPricePerSecond: sdk.NewCoin(testDenom, math.ZeroInt()),
			quantity:             5,
			duration:             100 * time.Second,
			expectErr:            true,
		},
		{
			name:                 "1 hour duration",
			lockedPricePerSecond: sdk.NewCoin(testDenom, math.NewInt(1)),
			quantity:             1,
			duration:             time.Hour,
			expected:             sdk.NewCoin(testDenom, math.NewInt(3600)),
		},
		{
			name:                 "24 hour duration",
			lockedPricePerSecond: sdk.NewCoin(testDenom, math.NewInt(1)),
			quantity:             1,
			duration:             24 * time.Hour,
			expected:             sdk.NewCoin(testDenom, math.NewInt(86400)),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := CalculateAccruedAmount(tc.lockedPricePerSecond, tc.quantity, tc.duration)
			if tc.expectErr {
				require.ErrorIs(t, err, billingtypes.ErrInvalidCreditOperation)
				return
			}
			require.NoError(t, err)
			require.True(t, tc.expected.IsEqual(result), "expected %s, got %s", tc.expected, result)
		})
	}
}

func TestCalculateTotalAccruedForLease(t *testing.T) {
	tests := []struct {
		name     string
		items    []LeaseItemWithPrice
		duration time.Duration
		expected sdk.Coins
	}{
		{
			name: "single item",
			items: []LeaseItemWithPrice{
				{SkuUUID: "sku-1", Quantity: 2, LockedPricePerSecond: sdk.NewCoin(testDenom, math.NewInt(10))},
			},
			duration: 100 * time.Second,
			expected: sdk.NewCoins(sdk.NewCoin(testDenom, math.NewInt(2000))), // 10 * 2 * 100
		},
		{
			name: "multiple items same denom",
			items: []LeaseItemWithPrice{
				{SkuUUID: "sku-1", Quantity: 1, LockedPricePerSecond: sdk.NewCoin(testDenom, math.NewInt(10))},
				{SkuUUID: "sku-2", Quantity: 2, LockedPricePerSecond: sdk.NewCoin(testDenom, math.NewInt(5))},
				{SkuUUID: "sku-3", Quantity: 3, LockedPricePerSecond: sdk.NewCoin(testDenom, math.NewInt(1))},
			},
			duration: 100 * time.Second,
			expected: sdk.NewCoins(sdk.NewCoin(testDenom, math.NewInt(2300))), // (10*1 + 5*2 + 1*3) * 100 = 23 * 100
		},
		{
			name: "multiple items different denoms",
			items: []LeaseItemWithPrice{
				{SkuUUID: "sku-1", Quantity: 1, LockedPricePerSecond: sdk.NewCoin(testDenom, math.NewInt(10))},
				{SkuUUID: "sku-2", Quantity: 2, LockedPricePerSecond: sdk.NewCoin("uother", math.NewInt(5))},
			},
			duration: 100 * time.Second,
			expected: sdk.NewCoins(
				sdk.NewCoin(testDenom, math.NewInt(1000)), // 10 * 1 * 100
				sdk.NewCoin("uother", math.NewInt(1000)),  // 5 * 2 * 100
			),
		},
		{
			name:     "empty items",
			items:    []LeaseItemWithPrice{},
			duration: 100 * time.Second,
			expected: sdk.NewCoins(),
		},
		{
			name: "zero duration",
			items: []LeaseItemWithPrice{
				{SkuUUID: "sku-1", Quantity: 5, LockedPricePerSecond: sdk.NewCoin(testDenom, math.NewInt(100))},
			},
			duration: 0,
			expected: sdk.NewCoins(),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := CalculateTotalAccruedForLease(tc.items, tc.duration)
			require.NoError(t, err)
			require.True(t, tc.expected.Equal(result), "expected %s, got %s", tc.expected, result)
		})
	}
}

func TestCalculateAccruedAmount_LongDurations(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
	}{
		{
			name:     "normal duration: 1 year",
			duration: 365 * 24 * time.Hour,
		},
		{
			name:     "long duration: 50 years",
			duration: 50 * 365 * 24 * time.Hour,
		},
		{
			name:     "very long duration: 100+ years",
			duration: 101 * 365 * 24 * time.Hour,
		},
	}

	pricePerSecond := sdk.NewCoin(testDenom, math.NewInt(1))

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			accrued, err := CalculateAccruedAmount(pricePerSecond, 1, tc.duration)
			require.NoError(t, err)
			require.Equal(t, math.NewInt(int64(tc.duration/time.Second)), accrued.Amount)
		})
	}
}

func TestCalculateAccruedAmount_CheckedBitBoundaries(t *testing.T) {
	highBit := highBitAccrualInt()

	t.Run("near maximum succeeds", func(t *testing.T) {
		nearHalf, err := highBit.SafeSub(math.OneInt())
		require.NoError(t, err)

		accrued, err := CalculateAccruedAmount(sdk.NewCoin(testDenom, nearHalf), 2, time.Second)
		require.NoError(t, err)
		expected, err := nearHalf.SafeMul(math.NewInt(2))
		require.NoError(t, err)
		require.Equal(t, expected, accrued.Amount)
	})

	t.Run("item multiplication overflow returns module error", func(t *testing.T) {
		require.NotPanics(t, func() {
			_, err := CalculateAccruedAmount(sdk.NewCoin(testDenom, highBit), 2, time.Second)
			require.ErrorIs(t, err, billingtypes.ErrArithmeticOverflow)
		})
	})

	t.Run("same denom sum overflow returns module error", func(t *testing.T) {
		items := []LeaseItemWithPrice{
			{SkuUUID: "sku-a", Quantity: 1, LockedPricePerSecond: sdk.NewCoin(testDenom, highBit)},
			{SkuUUID: "sku-b", Quantity: 1, LockedPricePerSecond: sdk.NewCoin(testDenom, highBit)},
		}
		require.NotPanics(t, func() {
			accrued, err := CalculateTotalAccruedForLease(items, time.Second)
			require.ErrorIs(t, err, billingtypes.ErrArithmeticOverflow)
			require.Empty(t, accrued)
			var overflow *AccrualOverflowError
			require.ErrorAs(t, err, &overflow)
			require.Equal(t, []string{testDenom}, overflow.Denoms)
		})
	})

	t.Run("overflow is isolated by denom in first-detected order", func(t *testing.T) {
		items := []LeaseItemWithPrice{
			{SkuUUID: "sku-z", Quantity: 2, LockedPricePerSecond: sdk.NewCoin("zoverflow", highBit)},
			{SkuUUID: "sku-ok-a", Quantity: 3, LockedPricePerSecond: sdk.NewCoin("uok", math.NewInt(7))},
			{SkuUUID: "sku-a", Quantity: 2, LockedPricePerSecond: sdk.NewCoin("aoverflow", highBit)},
			{SkuUUID: "sku-ok-b", Quantity: 2, LockedPricePerSecond: sdk.NewCoin("uok", math.NewInt(2))},
		}

		accrued, err := CalculateTotalAccruedForLease(items, time.Second)
		require.ErrorIs(t, err, billingtypes.ErrArithmeticOverflow)
		require.Equal(t, sdk.NewCoins(sdk.NewCoin("uok", math.NewInt(25))), accrued)
		var overflow *AccrualOverflowError
		require.ErrorAs(t, err, &overflow)
		require.Equal(t, []string{"zoverflow", "aoverflow"}, overflow.Denoms)
	})

	t.Run("later malformed item is not hidden by prior denom overflow", func(t *testing.T) {
		tests := []struct {
			name     string
			item     LeaseItemWithPrice
			expected error
		}{
			{
				name:     "invalid locked price",
				item:     LeaseItemWithPrice{SkuUUID: "sku-invalid-price", Quantity: 1, LockedPricePerSecond: sdk.Coin{Denom: testDenom}},
				expected: billingtypes.ErrInvalidCreditOperation,
			},
			{
				name:     "zero quantity",
				item:     LeaseItemWithPrice{SkuUUID: "sku-zero", Quantity: 0, LockedPricePerSecond: sdk.NewCoin(testDenom, math.OneInt())},
				expected: billingtypes.ErrInvalidQuantity,
			},
			{
				name:     "quantity above maximum",
				item:     LeaseItemWithPrice{SkuUUID: "sku-too-many", Quantity: billingtypes.MaxQuantityPerItem + 1, LockedPricePerSecond: sdk.NewCoin(testDenom, math.OneInt())},
				expected: billingtypes.ErrInvalidQuantity,
			},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				items := []LeaseItemWithPrice{
					{SkuUUID: "sku-overflow", Quantity: 2, LockedPricePerSecond: sdk.NewCoin(testDenom, highBit)},
					tc.item,
				}
				accrued, err := CalculateTotalAccruedForLease(items, time.Second)
				require.ErrorIs(t, err, tc.expected)
				require.Nil(t, accrued)
				var overflow *AccrualOverflowError
				require.False(t, errors.As(err, &overflow))
			})
		}
	})

	t.Run("nil stored amount returns registered invalid operation", func(t *testing.T) {
		require.NotPanics(t, func() {
			_, err := CalculateAccruedAmount(sdk.Coin{Denom: testDenom}, 1, time.Second)
			require.ErrorIs(t, err, billingtypes.ErrInvalidCreditOperation)
		})
	})
}

func TestAccrualOverflowError_PreservesCosmosErrorIdentity(t *testing.T) {
	err := errorsmod.Wrap(&AccrualOverflowError{Denoms: []string{testDenom}}, "calculate lease accrual")

	codespace, code, _ := errorsmod.ABCIInfo(err, false)
	require.Equal(t, billingtypes.ModuleName, codespace)
	require.Equal(t, uint32(20), code)

	grpcStatus, ok := status.FromError(err)
	require.True(t, ok)
	require.Contains(t, grpcStatus.Message(), "codespace billing code 20")
	require.Contains(t, grpcStatus.Message(), billingtypes.ErrArithmeticOverflow.Error())
	require.Contains(t, grpcStatus.Message(), "calculate lease accrual")
}

func TestLargeValueCalculations(t *testing.T) {
	// Test with values that might cause overflow in naive implementations
	// but should work with math.Int (big.Int)

	// Large price: 1 trillion per second
	largePrice := sdk.NewCoin(testDenom, math.NewInt(1_000_000_000_000))

	// 1 year duration
	yearSeconds := int64(365 * 24 * 60 * 60)

	result, err := CalculateAccruedAmount(largePrice, 100, time.Duration(yearSeconds)*time.Second)
	require.NoError(t, err)

	// Expected: 1 trillion * 100 * 31536000 seconds
	// This is a very large number but math.Int should handle it
	require.True(t, result.Amount.IsPositive())
	require.Equal(t, testDenom, result.Denom)
}

func TestPrecisionLoss(t *testing.T) {
	// Test that invalid pricing returns errors as expected

	// Price of 1 per hour should error (would result in 0 per second)
	// 1 / 3600 = 0
	basePrice := sdk.NewCoin(testDenom, math.NewInt(1))
	_, err := ConvertBasePriceToPerSecond(basePrice, skutypes.Unit_UNIT_PER_HOUR)
	require.Error(t, err, "zero rate should return error")

	// Price of 3599 per hour should error (not evenly divisible)
	basePrice = sdk.NewCoin(testDenom, math.NewInt(3599))
	_, err = ConvertBasePriceToPerSecond(basePrice, skutypes.Unit_UNIT_PER_HOUR)
	require.Error(t, err, "inexact division should return error")

	// Price of 3600 per hour should be exactly 1 per second
	basePrice = sdk.NewCoin(testDenom, math.NewInt(3600))
	perSecond, err := ConvertBasePriceToPerSecond(basePrice, skutypes.Unit_UNIT_PER_HOUR)
	require.NoError(t, err)
	require.Equal(t, math.NewInt(1), perSecond.Amount)

	// Price of 7199 per hour should error (not evenly divisible)
	// The SKU module now requires exact divisibility for valid pricing
	basePrice = sdk.NewCoin(testDenom, math.NewInt(7199))
	_, err = ConvertBasePriceToPerSecond(basePrice, skutypes.Unit_UNIT_PER_HOUR)
	require.Error(t, err, "inexact division should return error")

	// Price of 7200 per hour should be exactly 2 per second
	basePrice = sdk.NewCoin(testDenom, math.NewInt(7200))
	perSecond, err = ConvertBasePriceToPerSecond(basePrice, skutypes.Unit_UNIT_PER_HOUR)
	require.NoError(t, err)
	require.Equal(t, math.NewInt(2), perSecond.Amount)
}

// ============================================================================
// Benchmarks for Accrual Calculations
// ============================================================================

func BenchmarkConvertBasePriceToPerSecond(b *testing.B) {
	basePrice := sdk.NewCoin(testDenom, math.NewInt(3600000))

	b.Run("PerHour", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, _ = ConvertBasePriceToPerSecond(basePrice, skutypes.Unit_UNIT_PER_HOUR)
		}
	})

	b.Run("PerDay", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, _ = ConvertBasePriceToPerSecond(basePrice, skutypes.Unit_UNIT_PER_DAY)
		}
	})
}

func BenchmarkCalculateAccruedAmount(b *testing.B) {
	pricePerSecond := sdk.NewCoin(testDenom, math.NewInt(1000))

	b.Run("SmallDuration_100s", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, _ = CalculateAccruedAmount(pricePerSecond, 1, 100*time.Second)
		}
	})

	b.Run("MediumDuration_1hr", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, _ = CalculateAccruedAmount(pricePerSecond, 10, time.Hour)
		}
	})

	b.Run("LargeDuration_1yr", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, _ = CalculateAccruedAmount(pricePerSecond, 100, 365*24*time.Hour)
		}
	})

	b.Run("LargePrice_Trillion", func(b *testing.B) {
		largePrice := sdk.NewCoin(testDenom, math.NewInt(1_000_000_000_000))
		for i := 0; i < b.N; i++ {
			_, _ = CalculateAccruedAmount(largePrice, 100, 365*24*time.Hour)
		}
	})
}

func BenchmarkCalculateTotalAccruedForLease(b *testing.B) {
	singleItem := []LeaseItemWithPrice{
		{SkuUUID: "sku-1", Quantity: 1, LockedPricePerSecond: sdk.NewCoin(testDenom, math.NewInt(100))},
	}

	fiveItems := []LeaseItemWithPrice{
		{SkuUUID: "sku-1", Quantity: 1, LockedPricePerSecond: sdk.NewCoin(testDenom, math.NewInt(100))},
		{SkuUUID: "sku-2", Quantity: 2, LockedPricePerSecond: sdk.NewCoin(testDenom, math.NewInt(200))},
		{SkuUUID: "sku-3", Quantity: 3, LockedPricePerSecond: sdk.NewCoin("umfx", math.NewInt(300))},
		{SkuUUID: "sku-4", Quantity: 4, LockedPricePerSecond: sdk.NewCoin("uother", math.NewInt(400))},
		{SkuUUID: "sku-5", Quantity: 5, LockedPricePerSecond: sdk.NewCoin(testDenom, math.NewInt(500))},
	}

	twentyItems := make([]LeaseItemWithPrice, 20)
	for i := range 20 {
		twentyItems[i] = LeaseItemWithPrice{
			SkuUUID:              "sku-" + string(rune('a'+i)),
			Quantity:             uint64(i) + 1, //nolint:gosec // i is bounded [0,19]
			LockedPricePerSecond: sdk.NewCoin(testDenom, math.NewInt(int64(100*(i+1)))),
		}
	}

	duration := time.Hour

	b.Run("SingleItem", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, _ = CalculateTotalAccruedForLease(singleItem, duration)
		}
	})

	b.Run("FiveItems_MultiDenom", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, _ = CalculateTotalAccruedForLease(fiveItems, duration)
		}
	})

	b.Run("TwentyItems", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, _ = CalculateTotalAccruedForLease(twentyItems, duration)
		}
	})
}
