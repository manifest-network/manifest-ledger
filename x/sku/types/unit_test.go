/*
Package types contains unit tests for SKU types validation.

Test Coverage:
- CalculatePricePerSecond: converts base price to per-second rate
- ValidatePriceAndUnit: validates price/unit combinations
- Unit JSON marshaling/unmarshaling
*/
package types

import (
	"encoding/json"
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
		{
			name:      "per hour: 3600 -> 1 per second (valid)",
			basePrice: sdk.NewCoin(testDenom, math.NewInt(3600)),
			unit:      Unit_UNIT_PER_HOUR,
			expected:  math.NewInt(1),
			valid:     true,
		},
		{
			name:      "per hour: 7200 -> 2 per second (valid)",
			basePrice: sdk.NewCoin(testDenom, math.NewInt(7200)),
			unit:      Unit_UNIT_PER_HOUR,
			expected:  math.NewInt(2),
			valid:     true,
		},
		{
			name:      "per day: 86400 -> 1 per second (valid)",
			basePrice: sdk.NewCoin(testDenom, math.NewInt(86400)),
			unit:      Unit_UNIT_PER_DAY,
			expected:  math.NewInt(1),
			valid:     true,
		},
		{
			name:      "per day: 172800 -> 2 per second (valid)",
			basePrice: sdk.NewCoin(testDenom, math.NewInt(172800)),
			unit:      Unit_UNIT_PER_DAY,
			expected:  math.NewInt(2),
			valid:     true,
		},
		{
			name:      "per hour: small amount results in zero (invalid)",
			basePrice: sdk.NewCoin(testDenom, math.NewInt(100)),
			unit:      Unit_UNIT_PER_HOUR,
			expected:  math.ZeroInt(),
			valid:     false,
		},
		{
			name:      "per day: small amount results in zero (invalid)",
			basePrice: sdk.NewCoin(testDenom, math.NewInt(1000)),
			unit:      Unit_UNIT_PER_DAY,
			expected:  math.ZeroInt(),
			valid:     false,
		},
		{
			name:      "unspecified unit (invalid)",
			basePrice: sdk.NewCoin(testDenom, math.NewInt(100)),
			unit:      Unit_UNIT_UNSPECIFIED,
			expected:  math.ZeroInt(),
			valid:     false,
		},
		{
			name:      "per hour: large amount (valid)",
			basePrice: sdk.NewCoin(testDenom, math.NewInt(36000000)),
			unit:      Unit_UNIT_PER_HOUR,
			expected:  math.NewInt(10000),
			valid:     true,
		},
		{
			name:      "per hour: minimum valid (3600)",
			basePrice: sdk.NewCoin(testDenom, math.NewInt(3600)),
			unit:      Unit_UNIT_PER_HOUR,
			expected:  math.NewInt(1),
			valid:     true,
		},
		{
			name:      "per hour: just below minimum (3599, invalid)",
			basePrice: sdk.NewCoin(testDenom, math.NewInt(3599)),
			unit:      Unit_UNIT_PER_HOUR,
			expected:  math.ZeroInt(),
			valid:     false,
		},
		{
			name:      "per day: minimum valid (86400)",
			basePrice: sdk.NewCoin(testDenom, math.NewInt(86400)),
			unit:      Unit_UNIT_PER_DAY,
			expected:  math.NewInt(1),
			valid:     true,
		},
		{
			name:      "per day: just below minimum (86399, invalid)",
			basePrice: sdk.NewCoin(testDenom, math.NewInt(86399)),
			unit:      Unit_UNIT_PER_DAY,
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
		name      string
		basePrice sdk.Coin
		unit      Unit
		expectErr bool
	}{
		{
			name:      "valid: per hour with sufficient price",
			basePrice: sdk.NewCoin(testDenom, math.NewInt(3600)),
			unit:      Unit_UNIT_PER_HOUR,
			expectErr: false,
		},
		{
			name:      "valid: per day with sufficient price",
			basePrice: sdk.NewCoin(testDenom, math.NewInt(86400)),
			unit:      Unit_UNIT_PER_DAY,
			expectErr: false,
		},
		{
			name:      "invalid: per hour with too low price",
			basePrice: sdk.NewCoin(testDenom, math.NewInt(100)),
			unit:      Unit_UNIT_PER_HOUR,
			expectErr: true,
		},
		{
			name:      "invalid: per day with too low price",
			basePrice: sdk.NewCoin(testDenom, math.NewInt(1000)),
			unit:      Unit_UNIT_PER_DAY,
			expectErr: true,
		},
		{
			name:      "invalid: unspecified unit",
			basePrice: sdk.NewCoin(testDenom, math.NewInt(1000000)),
			unit:      Unit_UNIT_UNSPECIFIED,
			expectErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidatePriceAndUnit(tc.basePrice, tc.unit)
			if tc.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
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
