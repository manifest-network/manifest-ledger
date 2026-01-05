/*
Package keeper contains unit tests for accrual calculation functions.

Test Coverage:
- ConvertBasePriceToPerSecond: price conversion for different units
- CalculateAccruedAmount: accrual calculation for single items
- CalculateTotalAccruedForLease: total accrual for multiple items
- Precision loss scenarios with various price/duration combinations
- Overflow protection for long-running leases
- Large value calculations with big integers
*/
package keeper

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	skutypes "github.com/manifest-network/manifest-ledger/x/sku/types"
)

const testDenom = "upwr"

func TestConvertBasePriceToPerSecond(t *testing.T) {
	tests := []struct {
		name      string
		basePrice sdk.Coin
		unit      skutypes.Unit
		expected  sdk.Coin
	}{
		{
			name:      "per hour: 3600 -> 1 per second",
			basePrice: sdk.NewCoin(testDenom, math.NewInt(3600)),
			unit:      skutypes.Unit_UNIT_PER_HOUR,
			expected:  sdk.NewCoin(testDenom, math.NewInt(1)),
		},
		{
			name:      "per hour: 7200 -> 2 per second",
			basePrice: sdk.NewCoin(testDenom, math.NewInt(7200)),
			unit:      skutypes.Unit_UNIT_PER_HOUR,
			expected:  sdk.NewCoin(testDenom, math.NewInt(2)),
		},
		{
			name:      "per day: 86400 -> 1 per second",
			basePrice: sdk.NewCoin(testDenom, math.NewInt(86400)),
			unit:      skutypes.Unit_UNIT_PER_DAY,
			expected:  sdk.NewCoin(testDenom, math.NewInt(1)),
		},
		{
			name:      "per day: 172800 -> 2 per second",
			basePrice: sdk.NewCoin(testDenom, math.NewInt(172800)),
			unit:      skutypes.Unit_UNIT_PER_DAY,
			expected:  sdk.NewCoin(testDenom, math.NewInt(2)),
		},
		{
			name:      "unspecified: returns zero (invalid unit)",
			basePrice: sdk.NewCoin(testDenom, math.NewInt(100)),
			unit:      skutypes.Unit_UNIT_UNSPECIFIED,
			expected:  sdk.NewCoin(testDenom, math.ZeroInt()),
		},
		{
			name:      "per hour: small amount (precision loss)",
			basePrice: sdk.NewCoin(testDenom, math.NewInt(100)),
			unit:      skutypes.Unit_UNIT_PER_HOUR,
			expected:  sdk.NewCoin(testDenom, math.ZeroInt()), // 100/3600 = 0 due to integer division
		},
		{
			name:      "per hour: large amount",
			basePrice: sdk.NewCoin(testDenom, math.NewInt(36000000)),
			unit:      skutypes.Unit_UNIT_PER_HOUR,
			expected:  sdk.NewCoin(testDenom, math.NewInt(10000)),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := ConvertBasePriceToPerSecond(tc.basePrice, tc.unit)
			require.True(t, tc.expected.IsEqual(result), "expected %s, got %s", tc.expected, result)
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
			expected:             sdk.NewCoin(testDenom, math.NewInt(0)),
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

func TestCalculateAccruedAmountOverflow(t *testing.T) {
	tests := []struct {
		name      string
		duration  time.Duration
		expectErr bool
	}{
		{
			name:      "normal duration: 1 year",
			duration:  365 * 24 * time.Hour,
			expectErr: false,
		},
		{
			name:      "long duration: 50 years",
			duration:  50 * 365 * 24 * time.Hour,
			expectErr: false,
		},
		{
			name:      "very long duration: 100+ years",
			duration:  101 * 365 * 24 * time.Hour,
			expectErr: true, // Should exceed MaxDurationSeconds
		},
	}

	pricePerSecond := sdk.NewCoin(testDenom, math.NewInt(1))

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := CalculateAccruedAmount(pricePerSecond, 1, tc.duration)
			if tc.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
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
	// Test that integer division precision loss is as expected

	// Price of 1 per hour should be 0 per second due to integer division
	// 1 / 3600 = 0
	basePrice := sdk.NewCoin(testDenom, math.NewInt(1))
	perSecond := ConvertBasePriceToPerSecond(basePrice, skutypes.Unit_UNIT_PER_HOUR)
	require.True(t, perSecond.Amount.IsZero())

	// Price of 3599 per hour should be 0 per second (not evenly divisible)
	basePrice = sdk.NewCoin(testDenom, math.NewInt(3599))
	perSecond = ConvertBasePriceToPerSecond(basePrice, skutypes.Unit_UNIT_PER_HOUR)
	require.True(t, perSecond.Amount.IsZero())

	// Price of 3600 per hour should be exactly 1 per second
	basePrice = sdk.NewCoin(testDenom, math.NewInt(3600))
	perSecond = ConvertBasePriceToPerSecond(basePrice, skutypes.Unit_UNIT_PER_HOUR)
	require.Equal(t, math.NewInt(1), perSecond.Amount)

	// Price of 7199 per hour should be 0 per second (not evenly divisible)
	// The SKU module now requires exact divisibility for valid pricing
	basePrice = sdk.NewCoin(testDenom, math.NewInt(7199))
	perSecond = ConvertBasePriceToPerSecond(basePrice, skutypes.Unit_UNIT_PER_HOUR)
	require.True(t, perSecond.Amount.IsZero())

	// Price of 7200 per hour should be exactly 2 per second
	basePrice = sdk.NewCoin(testDenom, math.NewInt(7200))
	perSecond = ConvertBasePriceToPerSecond(basePrice, skutypes.Unit_UNIT_PER_HOUR)
	require.Equal(t, math.NewInt(2), perSecond.Amount)
}

// ============================================================================
// Benchmarks for Accrual Calculations
// ============================================================================

func BenchmarkConvertBasePriceToPerSecond(b *testing.B) {
	basePrice := sdk.NewCoin(testDenom, math.NewInt(3600000))

	b.Run("PerHour", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			ConvertBasePriceToPerSecond(basePrice, skutypes.Unit_UNIT_PER_HOUR)
		}
	})

	b.Run("PerDay", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			ConvertBasePriceToPerSecond(basePrice, skutypes.Unit_UNIT_PER_DAY)
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
