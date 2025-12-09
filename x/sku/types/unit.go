package types

import (
	"encoding/json"
	"fmt"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// SecondsPerHour is the number of seconds in an hour.
const SecondsPerHour = 3600

// SecondsPerDay is the number of seconds in a day.
const SecondsPerDay = 86400

// MarshalJSON implements the json.Marshaler interface for Unit.
func (u Unit) MarshalJSON() ([]byte, error) {
	return json.Marshal(u.String())
}

// CalculatePricePerSecond converts a base price to a per-second rate based on the unit.
// Returns the per-second rate and whether the conversion is valid (non-zero).
func CalculatePricePerSecond(basePrice sdk.Coin, unit Unit) (math.Int, bool) {
	var perSecond math.Int

	switch unit {
	case Unit_UNIT_PER_HOUR:
		perSecond = basePrice.Amount.Quo(math.NewInt(SecondsPerHour))
	case Unit_UNIT_PER_DAY:
		perSecond = basePrice.Amount.Quo(math.NewInt(SecondsPerDay))
	default:
		// UNIT_UNSPECIFIED - invalid
		return math.ZeroInt(), false
	}

	// Check if per-second rate is zero (would result in free usage)
	if perSecond.IsZero() {
		return math.ZeroInt(), false
	}

	return perSecond, true
}

// ValidatePriceAndUnit checks that the combination of base price and unit
// produces a valid (non-zero) per-second rate. This prevents SKUs that would
// be effectively free due to integer division truncation.
func ValidatePriceAndUnit(basePrice sdk.Coin, unit Unit) error {
	if _, valid := CalculatePricePerSecond(basePrice, unit); !valid {
		return fmt.Errorf("base price %s with unit %s results in zero per-second rate; increase price or change unit", basePrice, unit)
	}
	return nil
}

// UnmarshalJSON implements the json.Unmarshaler interface for Unit.
func (u *Unit) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		// Try unmarshaling as int for backward compatibility
		var i int32
		if err := json.Unmarshal(data, &i); err != nil {
			return fmt.Errorf("Unit should be a string or int, got %s", data)
		}
		*u = Unit(i)
		return nil
	}

	value, ok := Unit_value[s]
	if !ok {
		return fmt.Errorf("invalid Unit value: %s", s)
	}
	*u = Unit(value)
	return nil
}
