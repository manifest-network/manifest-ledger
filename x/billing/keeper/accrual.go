package keeper

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	skutypes "github.com/manifest-network/manifest-ledger/x/sku/types"
)

// MaxDurationSeconds is the maximum duration in seconds a stream may accrue in
// one settlement. This is approximately 100 years, far beyond any real gap
// between settlements; a longer gap is treated as an accrual overflow.
const MaxDurationSeconds = 100 * 365 * 24 * 60 * 60 // ~100 years in seconds

// ConvertBasePriceToPerSecond converts a SKU's base price to a per-second rate.
// The SKU's Unit determines how to interpret the base price:
// - UNIT_PER_HOUR: divide by 3600
// - UNIT_PER_DAY: divide by 86400
// Returns the per-second rate as a Coin with the same denom as the base price.
// Returns an error if the conversion fails (invalid unit, zero rate, or inexact division).
// Note: SKUs are validated at creation time, so this should not fail for valid SKUs.
func ConvertBasePriceToPerSecond(basePrice sdk.Coin, unit skutypes.Unit) (sdk.Coin, error) {
	perSecond, ok := skutypes.CalculatePricePerSecond(basePrice, unit)
	if !ok {
		return sdk.Coin{}, fmt.Errorf("failed to convert base price %s with unit %s to per-second rate", basePrice, unit)
	}
	return sdk.NewCoin(basePrice.Denom, perSecond), nil
}
