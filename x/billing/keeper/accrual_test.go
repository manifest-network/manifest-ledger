/*
Package keeper contains unit tests for the price conversion helper.

Test Coverage:
- ConvertBasePriceToPerSecond: price conversion for different units
- Precision loss scenarios with various price combinations

Accrual itself is computed per settlement stream; see stream_test.go.
*/
package keeper

import (
	"testing"

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
