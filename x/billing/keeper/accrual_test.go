/*
Package keeper contains unit tests for accrual calculation functions.

Test Coverage:
- ConvertBasePriceToPerSecond: price conversion for different units
- CalculateAccruedAmount: accrual calculation for single items
- CalculateTotalAccruedForLease: total accrual for multiple items
*/
package keeper

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	skutypes "github.com/manifest-network/manifest-ledger/x/sku/types"
)

func TestConvertBasePriceToPerSecond(t *testing.T) {
	tests := []struct {
		name      string
		basePrice math.Int
		unit      skutypes.Unit
		expected  math.Int
	}{
		{
			name:      "per hour: 3600 -> 1 per second",
			basePrice: math.NewInt(3600),
			unit:      skutypes.Unit_UNIT_PER_HOUR,
			expected:  math.NewInt(1),
		},
		{
			name:      "per hour: 7200 -> 2 per second",
			basePrice: math.NewInt(7200),
			unit:      skutypes.Unit_UNIT_PER_HOUR,
			expected:  math.NewInt(2),
		},
		{
			name:      "per day: 86400 -> 1 per second",
			basePrice: math.NewInt(86400),
			unit:      skutypes.Unit_UNIT_PER_DAY,
			expected:  math.NewInt(1),
		},
		{
			name:      "per day: 172800 -> 2 per second",
			basePrice: math.NewInt(172800),
			unit:      skutypes.Unit_UNIT_PER_DAY,
			expected:  math.NewInt(2),
		},
		{
			name:      "unspecified: treated as per second",
			basePrice: math.NewInt(100),
			unit:      skutypes.Unit_UNIT_UNSPECIFIED,
			expected:  math.NewInt(100),
		},
		{
			name:      "per hour: small amount (precision loss)",
			basePrice: math.NewInt(100),
			unit:      skutypes.Unit_UNIT_PER_HOUR,
			expected:  math.ZeroInt(), // 100/3600 = 0 due to integer division
		},
		{
			name:      "per hour: large amount",
			basePrice: math.NewInt(36000000),
			unit:      skutypes.Unit_UNIT_PER_HOUR,
			expected:  math.NewInt(10000),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := ConvertBasePriceToPerSecond(tc.basePrice, tc.unit)
			require.True(t, tc.expected.Equal(result), "expected %s, got %s", tc.expected, result)
		})
	}
}

func TestCalculateAccruedAmount(t *testing.T) {
	tests := []struct {
		name                 string
		lockedPricePerSecond math.Int
		quantity             uint64
		duration             time.Duration
		expected             math.Int
	}{
		{
			name:                 "1 per second, 1 quantity, 100 seconds",
			lockedPricePerSecond: math.NewInt(1),
			quantity:             1,
			duration:             100 * time.Second,
			expected:             math.NewInt(100),
		},
		{
			name:                 "1 per second, 5 quantity, 100 seconds",
			lockedPricePerSecond: math.NewInt(1),
			quantity:             5,
			duration:             100 * time.Second,
			expected:             math.NewInt(500),
		},
		{
			name:                 "10 per second, 2 quantity, 60 seconds",
			lockedPricePerSecond: math.NewInt(10),
			quantity:             2,
			duration:             60 * time.Second,
			expected:             math.NewInt(1200),
		},
		{
			name:                 "zero duration",
			lockedPricePerSecond: math.NewInt(100),
			quantity:             5,
			duration:             0,
			expected:             math.NewInt(0),
		},
		{
			name:                 "negative duration",
			lockedPricePerSecond: math.NewInt(100),
			quantity:             5,
			duration:             -10 * time.Second,
			expected:             math.NewInt(0),
		},
		{
			name:                 "zero price",
			lockedPricePerSecond: math.ZeroInt(),
			quantity:             5,
			duration:             100 * time.Second,
			expected:             math.NewInt(0),
		},
		{
			name:                 "1 hour duration",
			lockedPricePerSecond: math.NewInt(1),
			quantity:             1,
			duration:             time.Hour,
			expected:             math.NewInt(3600),
		},
		{
			name:                 "24 hour duration",
			lockedPricePerSecond: math.NewInt(1),
			quantity:             1,
			duration:             24 * time.Hour,
			expected:             math.NewInt(86400),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := CalculateAccruedAmount(tc.lockedPricePerSecond, tc.quantity, tc.duration)
			require.True(t, tc.expected.Equal(result), "expected %s, got %s", tc.expected, result)
		})
	}
}

func TestCalculateTotalAccruedForLease(t *testing.T) {
	tests := []struct {
		name     string
		items    []LeaseItemWithPrice
		duration time.Duration
		expected math.Int
	}{
		{
			name: "single item",
			items: []LeaseItemWithPrice{
				{SkuID: 1, Quantity: 2, LockedPricePerSecond: math.NewInt(10)},
			},
			duration: 100 * time.Second,
			expected: math.NewInt(2000), // 10 * 2 * 100
		},
		{
			name: "multiple items",
			items: []LeaseItemWithPrice{
				{SkuID: 1, Quantity: 1, LockedPricePerSecond: math.NewInt(10)},
				{SkuID: 2, Quantity: 2, LockedPricePerSecond: math.NewInt(5)},
				{SkuID: 3, Quantity: 3, LockedPricePerSecond: math.NewInt(1)},
			},
			duration: 100 * time.Second,
			expected: math.NewInt(2300), // (10*1 + 5*2 + 1*3) * 100 = 23 * 100
		},
		{
			name:     "empty items",
			items:    []LeaseItemWithPrice{},
			duration: 100 * time.Second,
			expected: math.NewInt(0),
		},
		{
			name: "zero duration",
			items: []LeaseItemWithPrice{
				{SkuID: 1, Quantity: 5, LockedPricePerSecond: math.NewInt(100)},
			},
			duration: 0,
			expected: math.NewInt(0),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := CalculateTotalAccruedForLease(tc.items, tc.duration)
			require.True(t, tc.expected.Equal(result), "expected %s, got %s", tc.expected, result)
		})
	}
}
