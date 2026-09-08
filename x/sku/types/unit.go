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

// PriceValidationError represents an error during price/unit validation.
type PriceValidationError struct {
	BasePrice sdk.Coin
	Unit      Unit
	IsZero    bool // true if rate is zero, false if not evenly divisible
	Remainder math.Int
}

func (e *PriceValidationError) Error() string {
	if e.IsZero {
		return fmt.Sprintf("base price %s with unit %s results in zero per-second rate; increase price or change unit", e.BasePrice, e.Unit)
	}
	return fmt.Sprintf("base price %s is not evenly divisible by %s (remainder: %s); price must be exactly divisible to avoid rounding errors", e.BasePrice, e.Unit, e.Remainder)
}

// divisorForUnit returns the number of seconds in the given unit's period.
// Returns zero and false for invalid/unspecified units.
func divisorForUnit(unit Unit) (math.Int, bool) {
	switch unit {
	case Unit_UNIT_PER_HOUR:
		return math.NewInt(SecondsPerHour), true
	case Unit_UNIT_PER_DAY:
		return math.NewInt(SecondsPerDay), true
	default:
		return math.Int{}, false
	}
}

// CalculatePricePerSecond converts a base price to a per-second rate based on the unit.
// It returns zero and false for an invalid coin or unit, a nonpositive price,
// or a price that does not yield a positive, exact integer rate.
func CalculatePricePerSecond(basePrice sdk.Coin, unit Unit) (math.Int, bool) {
	perSecond, err := pricePerSecond(basePrice, unit)
	return perSecond, err == nil
}

// ValidatePriceAndUnit checks that a valid, positive coin and a supported unit
// produce a valid per-second rate. This prevents SKUs that would:
// 1. Be effectively free due to integer division truncation (zero rate)
// 2. Have rounding errors due to non-exact division
//
// For exact billing, the base price must be evenly divisible by the number
// of seconds in the unit period:
// - UNIT_PER_HOUR: price must be divisible by 3600
// - UNIT_PER_DAY: price must be divisible by 86400
func ValidatePriceAndUnit(basePrice sdk.Coin, unit Unit) error {
	_, err := pricePerSecond(basePrice, unit)
	return err
}

// pricePerSecond is the shared validation and conversion boundary. Keep zero-rate
// and inexact-division errors distinct for callers that inspect PriceValidationError.
func pricePerSecond(basePrice sdk.Coin, unit Unit) (math.Int, error) {
	divisor, ok := divisorForUnit(unit)
	if !ok {
		return math.ZeroInt(), fmt.Errorf("invalid unit: %s", unit)
	}
	if err := basePrice.Validate(); err != nil {
		return math.ZeroInt(), fmt.Errorf("invalid base price: %w", err)
	}

	perSecond := basePrice.Amount.Quo(divisor)

	// Check if per-second rate is zero (would result in free usage)
	if perSecond.IsZero() {
		return math.ZeroInt(), &PriceValidationError{
			BasePrice: basePrice,
			Unit:      unit,
			IsZero:    true,
		}
	}

	// Check if division is exact (no remainder)
	remainder := basePrice.Amount.Mod(divisor)
	if !remainder.IsZero() {
		return math.ZeroInt(), &PriceValidationError{
			BasePrice: basePrice,
			Unit:      unit,
			IsZero:    false,
			Remainder: remainder,
		}
	}

	return perSecond, nil
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
