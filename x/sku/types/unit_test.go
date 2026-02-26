/*
Package types contains unit tests for SKU types validation.

Test Coverage:
- CalculatePricePerSecond: converts base price to per-second rate with exact divisibility
- ValidatePriceAndUnit: validates price/unit combinations for exact division
- PriceValidationError: structured error type for validation failures
- Unit JSON marshaling/unmarshaling
*/
package types

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

func TestCalculatePricePerSecond(t *testing.T) {
	const testDenom = "upwr"

	tests := []struct {
		name      string
		basePrice sdk.Coin
		unit      Unit
		expected  math.Int
		valid     bool
	}{
		// Valid cases: exact division
		{
			name:      "per hour: 3600 -> 1 per second (exact)",
			basePrice: sdk.NewCoin(testDenom, math.NewInt(3600)),
			unit:      Unit_UNIT_PER_HOUR,
			expected:  math.NewInt(1),
			valid:     true,
		},
		{
			name:      "per hour: 7200 -> 2 per second (exact)",
			basePrice: sdk.NewCoin(testDenom, math.NewInt(7200)),
			unit:      Unit_UNIT_PER_HOUR,
			expected:  math.NewInt(2),
			valid:     true,
		},
		{
			name:      "per day: 86400 -> 1 per second (exact)",
			basePrice: sdk.NewCoin(testDenom, math.NewInt(86400)),
			unit:      Unit_UNIT_PER_DAY,
			expected:  math.NewInt(1),
			valid:     true,
		},
		{
			name:      "per day: 172800 -> 2 per second (exact)",
			basePrice: sdk.NewCoin(testDenom, math.NewInt(172800)),
			unit:      Unit_UNIT_PER_DAY,
			expected:  math.NewInt(2),
			valid:     true,
		},
		{
			name:      "per hour: large amount 36000000 -> 10000 per second (exact)",
			basePrice: sdk.NewCoin(testDenom, math.NewInt(36000000)),
			unit:      Unit_UNIT_PER_HOUR,
			expected:  math.NewInt(10000),
			valid:     true,
		},

		// Invalid cases: zero result (price too low)
		{
			name:      "per hour: 100 results in zero (invalid)",
			basePrice: sdk.NewCoin(testDenom, math.NewInt(100)),
			unit:      Unit_UNIT_PER_HOUR,
			expected:  math.ZeroInt(),
			valid:     false,
		},
		{
			name:      "per day: 1000 results in zero (invalid)",
			basePrice: sdk.NewCoin(testDenom, math.NewInt(1000)),
			unit:      Unit_UNIT_PER_DAY,
			expected:  math.ZeroInt(),
			valid:     false,
		},
		{
			name:      "per hour: 3599 results in zero (just below minimum)",
			basePrice: sdk.NewCoin(testDenom, math.NewInt(3599)),
			unit:      Unit_UNIT_PER_HOUR,
			expected:  math.ZeroInt(),
			valid:     false,
		},
		{
			name:      "per day: 86399 results in zero (just below minimum)",
			basePrice: sdk.NewCoin(testDenom, math.NewInt(86399)),
			unit:      Unit_UNIT_PER_DAY,
			expected:  math.ZeroInt(),
			valid:     false,
		},

		// Invalid cases: not evenly divisible (remainder exists)
		{
			name:      "per hour: 3601 not evenly divisible (remainder 1)",
			basePrice: sdk.NewCoin(testDenom, math.NewInt(3601)),
			unit:      Unit_UNIT_PER_HOUR,
			expected:  math.ZeroInt(),
			valid:     false,
		},
		{
			name:      "per hour: 7201 not evenly divisible (remainder 1)",
			basePrice: sdk.NewCoin(testDenom, math.NewInt(7201)),
			unit:      Unit_UNIT_PER_HOUR,
			expected:  math.ZeroInt(),
			valid:     false,
		},
		{
			name:      "per day: 86401 not evenly divisible (remainder 1)",
			basePrice: sdk.NewCoin(testDenom, math.NewInt(86401)),
			unit:      Unit_UNIT_PER_DAY,
			expected:  math.ZeroInt(),
			valid:     false,
		},
		{
			name:      "per day: 100000 not evenly divisible (remainder 13600)",
			basePrice: sdk.NewCoin(testDenom, math.NewInt(100000)),
			unit:      Unit_UNIT_PER_DAY,
			expected:  math.ZeroInt(),
			valid:     false,
		},
		{
			name:      "per hour: 5000 not evenly divisible (remainder 1400)",
			basePrice: sdk.NewCoin(testDenom, math.NewInt(5000)),
			unit:      Unit_UNIT_PER_HOUR,
			expected:  math.ZeroInt(),
			valid:     false,
		},

		// Invalid cases: unspecified unit
		{
			name:      "unspecified unit (invalid)",
			basePrice: sdk.NewCoin(testDenom, math.NewInt(3600)),
			unit:      Unit_UNIT_UNSPECIFIED,
			expected:  math.ZeroInt(),
			valid:     false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, valid := CalculatePricePerSecond(tc.basePrice, tc.unit)
			require.Equal(t, tc.valid, valid, "validity mismatch")
			require.True(t, tc.expected.Equal(result), "expected %s, got %s", tc.expected, result)
		})
	}
}

func TestValidatePriceAndUnit(t *testing.T) {
	const testDenom = "upwr"

	tests := []struct {
		name         string
		basePrice    sdk.Coin
		unit         Unit
		expectErr    bool
		errIsZero    bool   // if error, is it a zero rate error?
		errRemainder string // if error due to remainder, what's the expected remainder?
	}{
		// Valid cases: exact division
		{
			name:      "valid: per hour with exact division (3600)",
			basePrice: sdk.NewCoin(testDenom, math.NewInt(3600)),
			unit:      Unit_UNIT_PER_HOUR,
			expectErr: false,
		},
		{
			name:      "valid: per hour with exact division (7200)",
			basePrice: sdk.NewCoin(testDenom, math.NewInt(7200)),
			unit:      Unit_UNIT_PER_HOUR,
			expectErr: false,
		},
		{
			name:      "valid: per day with exact division (86400)",
			basePrice: sdk.NewCoin(testDenom, math.NewInt(86400)),
			unit:      Unit_UNIT_PER_DAY,
			expectErr: false,
		},
		{
			name:      "valid: per day with exact division (172800)",
			basePrice: sdk.NewCoin(testDenom, math.NewInt(172800)),
			unit:      Unit_UNIT_PER_DAY,
			expectErr: false,
		},
		{
			name:      "valid: large per hour amount (36000000)",
			basePrice: sdk.NewCoin(testDenom, math.NewInt(36000000)),
			unit:      Unit_UNIT_PER_HOUR,
			expectErr: false,
		},

		// Invalid cases: zero result
		{
			name:      "invalid: per hour with too low price (zero result)",
			basePrice: sdk.NewCoin(testDenom, math.NewInt(100)),
			unit:      Unit_UNIT_PER_HOUR,
			expectErr: true,
			errIsZero: true,
		},
		{
			name:      "invalid: per day with too low price (zero result)",
			basePrice: sdk.NewCoin(testDenom, math.NewInt(1000)),
			unit:      Unit_UNIT_PER_DAY,
			expectErr: true,
			errIsZero: true,
		},

		// Invalid cases: not evenly divisible
		{
			name:         "invalid: per hour 3601 not evenly divisible",
			basePrice:    sdk.NewCoin(testDenom, math.NewInt(3601)),
			unit:         Unit_UNIT_PER_HOUR,
			expectErr:    true,
			errIsZero:    false,
			errRemainder: "1",
		},
		{
			name:         "invalid: per hour 7201 not evenly divisible",
			basePrice:    sdk.NewCoin(testDenom, math.NewInt(7201)),
			unit:         Unit_UNIT_PER_HOUR,
			expectErr:    true,
			errIsZero:    false,
			errRemainder: "1",
		},
		{
			name:         "invalid: per day 86401 not evenly divisible",
			basePrice:    sdk.NewCoin(testDenom, math.NewInt(86401)),
			unit:         Unit_UNIT_PER_DAY,
			expectErr:    true,
			errIsZero:    false,
			errRemainder: "1",
		},
		{
			name:         "invalid: per hour 5000 not evenly divisible",
			basePrice:    sdk.NewCoin(testDenom, math.NewInt(5000)),
			unit:         Unit_UNIT_PER_HOUR,
			expectErr:    true,
			errIsZero:    false,
			errRemainder: "1400",
		},
		{
			name:         "invalid: per day 100000 not evenly divisible",
			basePrice:    sdk.NewCoin(testDenom, math.NewInt(100000)),
			unit:         Unit_UNIT_PER_DAY,
			expectErr:    true,
			errIsZero:    false,
			errRemainder: "13600",
		},

		// Invalid cases: unspecified unit
		{
			name:      "invalid: unspecified unit",
			basePrice: sdk.NewCoin(testDenom, math.NewInt(3600)),
			unit:      Unit_UNIT_UNSPECIFIED,
			expectErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidatePriceAndUnit(tc.basePrice, tc.unit)
			if tc.expectErr {
				require.Error(t, err)

				// Check error type and details
				var priceErr *PriceValidationError
				if errors.As(err, &priceErr) {
					require.Equal(t, tc.errIsZero, priceErr.IsZero, "error IsZero mismatch")
					if !tc.errIsZero && tc.errRemainder != "" {
						require.Equal(t, tc.errRemainder, priceErr.Remainder.String(), "remainder mismatch")
					}
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestPriceValidationError(t *testing.T) {
	const testDenom = "upwr"

	t.Run("zero rate error message", func(t *testing.T) {
		err := &PriceValidationError{
			BasePrice: sdk.NewCoin(testDenom, math.NewInt(100)),
			Unit:      Unit_UNIT_PER_HOUR,
			IsZero:    true,
		}
		require.Contains(t, err.Error(), "zero per-second rate")
		require.Contains(t, err.Error(), "100upwr")
		require.Contains(t, err.Error(), "UNIT_PER_HOUR")
	})

	t.Run("not evenly divisible error message", func(t *testing.T) {
		err := &PriceValidationError{
			BasePrice: sdk.NewCoin(testDenom, math.NewInt(3601)),
			Unit:      Unit_UNIT_PER_HOUR,
			IsZero:    false,
			Remainder: math.NewInt(1),
		}
		require.Contains(t, err.Error(), "not evenly divisible")
		require.Contains(t, err.Error(), "3601upwr")
		require.Contains(t, err.Error(), "remainder: 1")
	})
}

func TestUnitJSONMarshal(t *testing.T) {
	tests := []struct {
		name     string
		unit     Unit
		expected string
	}{
		{
			name:     "unspecified",
			unit:     Unit_UNIT_UNSPECIFIED,
			expected: `"UNIT_UNSPECIFIED"`,
		},
		{
			name:     "per hour",
			unit:     Unit_UNIT_PER_HOUR,
			expected: `"UNIT_PER_HOUR"`,
		},
		{
			name:     "per day",
			unit:     Unit_UNIT_PER_DAY,
			expected: `"UNIT_PER_DAY"`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(tc.unit)
			require.NoError(t, err)
			require.Equal(t, tc.expected, string(data))
		})
	}
}

func TestUnitJSONUnmarshal(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		expected  Unit
		expectErr bool
	}{
		{
			name:      "string: UNIT_UNSPECIFIED",
			input:     `"UNIT_UNSPECIFIED"`,
			expected:  Unit_UNIT_UNSPECIFIED,
			expectErr: false,
		},
		{
			name:      "string: UNIT_PER_HOUR",
			input:     `"UNIT_PER_HOUR"`,
			expected:  Unit_UNIT_PER_HOUR,
			expectErr: false,
		},
		{
			name:      "string: UNIT_PER_DAY",
			input:     `"UNIT_PER_DAY"`,
			expected:  Unit_UNIT_PER_DAY,
			expectErr: false,
		},
		{
			name:      "int: 0 (backward compatibility)",
			input:     `0`,
			expected:  Unit_UNIT_UNSPECIFIED,
			expectErr: false,
		},
		{
			name:      "int: 1 (backward compatibility)",
			input:     `1`,
			expected:  Unit_UNIT_PER_HOUR,
			expectErr: false,
		},
		{
			name:      "int: 2 (backward compatibility)",
			input:     `2`,
			expected:  Unit_UNIT_PER_DAY,
			expectErr: false,
		},
		{
			name:      "invalid string",
			input:     `"UNIT_INVALID"`,
			expected:  Unit_UNIT_UNSPECIFIED,
			expectErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var unit Unit
			err := json.Unmarshal([]byte(tc.input), &unit)
			if tc.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.expected, unit)
			}
		})
	}
}
