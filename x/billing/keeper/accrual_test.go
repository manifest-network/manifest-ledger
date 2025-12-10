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

func TestConvertBasePriceToPerSecond(t *testing.T) {
	const testDenom = "upwr"

	tests := []struct {
		name      string
		basePrice sdk.Coin
		unit      skutypes.Unit
		expected  math.Int
	}{
		{
			name:      "per hour: 3600 -> 1 per second",
			basePrice: sdk.NewCoin(testDenom, math.NewInt(3600)),
			unit:      skutypes.Unit_UNIT_PER_HOUR,
			expected:  math.NewInt(1),
		},
		{
			name:      "per hour: 7200 -> 2 per second",
			basePrice: sdk.NewCoin(testDenom, math.NewInt(7200)),
			unit:      skutypes.Unit_UNIT_PER_HOUR,
			expected:  math.NewInt(2),
		},
		{
			name:      "per day: 86400 -> 1 per second",
			basePrice: sdk.NewCoin(testDenom, math.NewInt(86400)),
			unit:      skutypes.Unit_UNIT_PER_DAY,
			expected:  math.NewInt(1),
		},
		{
			name:      "per day: 172800 -> 2 per second",
			basePrice: sdk.NewCoin(testDenom, math.NewInt(172800)),
			unit:      skutypes.Unit_UNIT_PER_DAY,
			expected:  math.NewInt(2),
		},
		{
			name:      "unspecified: returns zero (invalid unit)",
			basePrice: sdk.NewCoin(testDenom, math.NewInt(100)),
			unit:      skutypes.Unit_UNIT_UNSPECIFIED,
			expected:  math.ZeroInt(),
		},
		{
			name:      "per hour: small amount (precision loss)",
			basePrice: sdk.NewCoin(testDenom, math.NewInt(100)),
			unit:      skutypes.Unit_UNIT_PER_HOUR,
			expected:  math.ZeroInt(), // 100/3600 = 0 due to integer division
		},
		{
			name:      "per hour: large amount",
			basePrice: sdk.NewCoin(testDenom, math.NewInt(36000000)),
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
			result, err := CalculateAccruedAmount(tc.lockedPricePerSecond, tc.quantity, tc.duration)
			require.NoError(t, err)
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
			name:      "normal duration: 10 years",
			duration:  10 * 365 * 24 * time.Hour,
			expectErr: false,
		},
		{
			name:      "excessive duration: 101 years (exceeds max)",
			duration:  101 * 365 * 24 * time.Hour,
			expectErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Use a reasonable price per second
			pricePerSecond := math.NewInt(1)
			_, err := CalculateAccruedAmount(pricePerSecond, 1, tc.duration)
			if tc.expectErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), "exceeds maximum allowed")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestPrecisionLossScenarios tests various scenarios where integer division
// could cause precision loss and verifies our handling is correct.
func TestPrecisionLossScenarios(t *testing.T) {
	tests := []struct {
		name        string
		basePrice   int64
		unit        skutypes.Unit
		quantity    uint64
		duration    time.Duration
		description string
	}{
		{
			name:        "small hourly price: 100 upwr/hour",
			basePrice:   100,
			unit:        skutypes.Unit_UNIT_PER_HOUR,
			quantity:    1,
			duration:    time.Hour,
			description: "100/3600 = 0 per second, so no accrual",
		},
		{
			name:        "minimum non-zero hourly rate: 3600 upwr/hour",
			basePrice:   3600,
			unit:        skutypes.Unit_UNIT_PER_HOUR,
			quantity:    1,
			duration:    time.Hour,
			description: "3600/3600 = 1 per second, accrual = 3600",
		},
		{
			name:        "small daily price: 86399 upwr/day",
			basePrice:   86399,
			unit:        skutypes.Unit_UNIT_PER_DAY,
			quantity:    1,
			duration:    24 * time.Hour,
			description: "86399/86400 = 0 per second (floor division)",
		},
		{
			name:        "minimum non-zero daily rate: 86400 upwr/day",
			basePrice:   86400,
			unit:        skutypes.Unit_UNIT_PER_DAY,
			quantity:    1,
			duration:    24 * time.Hour,
			description: "86400/86400 = 1 per second, accrual = 86400",
		},
		{
			name:        "large quantity compensates for small price",
			basePrice:   3600,
			unit:        skutypes.Unit_UNIT_PER_HOUR,
			quantity:    100,
			duration:    time.Hour,
			description: "1 per second * 100 quantity * 3600 seconds = 360000",
		},
		{
			name:        "fractional loss accumulates over time",
			basePrice:   3601, // 3601/3600 = 1 (loses 1 upwr/hour)
			unit:        skutypes.Unit_UNIT_PER_HOUR,
			quantity:    1,
			duration:    24 * time.Hour, // 24 hours
			description: "integer division loses ~24 upwr over 24 hours",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			basePrice := sdk.NewCoin("upwr", math.NewInt(tc.basePrice))
			perSecond := ConvertBasePriceToPerSecond(basePrice, tc.unit)

			accrued, err := CalculateAccruedAmount(perSecond, tc.quantity, tc.duration)
			require.NoError(t, err)

			t.Logf("Description: %s", tc.description)
			t.Logf("Base price: %d, Unit: %s, Per-second rate: %s", tc.basePrice, tc.unit, perSecond)
			t.Logf("Quantity: %d, Duration: %s, Accrued: %s", tc.quantity, tc.duration, accrued)

			// Verify the result is non-negative
			require.True(t, accrued.GTE(math.ZeroInt()), "accrued should be non-negative")

			// If per-second rate is zero, accrued should be zero
			if perSecond.IsZero() {
				require.True(t, accrued.IsZero(), "zero rate should result in zero accrual")
			}
		})
	}
}

// TestLargeValueCalculations tests that our math.Int-based calculations
// handle large values correctly without overflow.
func TestLargeValueCalculations(t *testing.T) {
	tests := []struct {
		name                 string
		lockedPricePerSecond math.Int
		quantity             uint64
		duration             time.Duration
		expectErr            bool
	}{
		{
			name:                 "large price: 1 billion per second for 1 year",
			lockedPricePerSecond: math.NewInt(1_000_000_000),
			quantity:             1,
			duration:             365 * 24 * time.Hour,
			expectErr:            false,
		},
		{
			name:                 "large quantity: 1M instances for 1 month",
			lockedPricePerSecond: math.NewInt(1),
			quantity:             1_000_000,
			duration:             30 * 24 * time.Hour,
			expectErr:            false,
		},
		{
			name:                 "combined large values: enterprise scale",
			lockedPricePerSecond: math.NewInt(1_000_000), // 1M per second
			quantity:             10_000,                 // 10K instances
			duration:             30 * 24 * time.Hour,    // 1 month
			expectErr:            false,
		},
		{
			name:                 "maximum supported duration: ~100 years",
			lockedPricePerSecond: math.NewInt(1),
			quantity:             1,
			duration:             99 * 365 * 24 * time.Hour,
			expectErr:            false,
		},
		{
			name:                 "exceeds maximum duration: 101 years",
			lockedPricePerSecond: math.NewInt(1),
			quantity:             1,
			duration:             101 * 365 * 24 * time.Hour,
			expectErr:            true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := CalculateAccruedAmount(tc.lockedPricePerSecond, tc.quantity, tc.duration)

			if tc.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.True(t, result.IsPositive() || result.IsZero(), "result should be non-negative")
				t.Logf("Calculated accrual: %s", result)
			}
		})
	}
}

// TestAccrualConsistency verifies that accrual calculations are consistent
// across different calculation approaches.
func TestAccrualConsistency(t *testing.T) {
	pricePerSecond := math.NewInt(100)
	quantity := uint64(5)

	// Test that sum of smaller periods equals one large period
	oneHour, err := CalculateAccruedAmount(pricePerSecond, quantity, time.Hour)
	require.NoError(t, err)

	// Calculate 60 minutes separately and sum
	oneMinute, err := CalculateAccruedAmount(pricePerSecond, quantity, time.Minute)
	require.NoError(t, err)
	sixtyMinutes := oneMinute.MulRaw(60)

	// Due to integer division, these should be equal (no fractional seconds)
	require.True(t, oneHour.Equal(sixtyMinutes),
		"1 hour (%s) should equal 60 minutes (%s)", oneHour, sixtyMinutes)

	// Test that order of operations doesn't matter for total accrual
	items := []LeaseItemWithPrice{
		{SkuID: 1, Quantity: 2, LockedPricePerSecond: math.NewInt(10)},
		{SkuID: 2, Quantity: 3, LockedPricePerSecond: math.NewInt(20)},
	}
	duration := 100 * time.Second

	totalFromFunc, err := CalculateTotalAccruedForLease(items, duration)
	require.NoError(t, err)

	// Calculate manually
	item1Accrued, _ := CalculateAccruedAmount(math.NewInt(10), 2, duration)
	item2Accrued, _ := CalculateAccruedAmount(math.NewInt(20), 3, duration)
	manualTotal := item1Accrued.Add(item2Accrued)

	require.True(t, totalFromFunc.Equal(manualTotal),
		"total from function (%s) should equal manual calculation (%s)",
		totalFromFunc, manualTotal)
}
