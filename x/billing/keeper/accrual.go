package keeper

import (
	"fmt"
	"time"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	skutypes "github.com/manifest-network/manifest-ledger/x/sku/types"
)

// MaxDurationSeconds is the maximum duration in seconds we support for accrual calculations.
// This is approximately 100 years, which should be more than sufficient for any lease.
// This limit prevents integer overflow in accrual calculations.
const MaxDurationSeconds = 100 * 365 * 24 * 60 * 60 // ~100 years in seconds

// ConvertBasePriceToPerSecond converts a SKU's base price to a per-second rate.
// The SKU's Unit determines how to interpret the base price:
// - UNIT_PER_HOUR: divide by 3600
// - UNIT_PER_DAY: divide by 86400
// Returns the per-second rate as a Coin with the same denom as the base price.
// Note: Integer division may result in precision loss for small amounts.
// SKUs should be validated at creation time to ensure non-zero per-second rates.
func ConvertBasePriceToPerSecond(basePrice sdk.Coin, unit skutypes.Unit) sdk.Coin {
	perSecond, _ := skutypes.CalculatePricePerSecond(basePrice, unit)
	return sdk.NewCoin(basePrice.Denom, perSecond)
}

// CalculateAccruedAmount calculates the amount accrued for a lease item
// over a given duration.
// accrued = lockedPricePerSecond * quantity * durationSeconds
// Returns an error if the calculation would overflow.
func CalculateAccruedAmount(lockedPricePerSecond sdk.Coin, quantity uint64, duration time.Duration) (sdk.Coin, error) {
	durationSeconds := int64(duration.Seconds())
	if durationSeconds < 0 {
		return sdk.NewCoin(lockedPricePerSecond.Denom, math.ZeroInt()), nil
	}

	// Check for excessive duration that could cause overflow
	if durationSeconds > MaxDurationSeconds {
		return sdk.Coin{}, fmt.Errorf("duration %d seconds exceeds maximum allowed %d seconds (approx 100 years)", durationSeconds, MaxDurationSeconds)
	}

	// accrued = price_per_second * quantity * seconds
	quantityInt := math.NewIntFromUint64(quantity)
	secondsInt := math.NewInt(durationSeconds)

	// Perform multiplication with overflow checking
	// math.Int uses big.Int internally, so it won't overflow, but we check for unreasonable values
	result := lockedPricePerSecond.Amount.Mul(quantityInt).Mul(secondsInt)

	// Sanity check: ensure result is non-negative
	if result.IsNegative() {
		return sdk.Coin{}, fmt.Errorf("accrual calculation resulted in negative value")
	}

	return sdk.NewCoin(lockedPricePerSecond.Denom, result), nil
}

// CalculateTotalAccruedForLease calculates the total accrued amounts for all items
// in a lease over the given duration. Returns a Coins collection (one entry per denom).
// Returns an error if any item calculation would overflow.
func CalculateTotalAccruedForLease(items []LeaseItemWithPrice, duration time.Duration) (sdk.Coins, error) {
	totals := sdk.NewCoins()

	for _, item := range items {
		accrued, err := CalculateAccruedAmount(item.LockedPricePerSecond, item.Quantity, duration)
		if err != nil {
			return nil, fmt.Errorf("overflow calculating accrual for SKU %s: %w", item.SkuUUID, err)
		}
		if accrued.IsPositive() {
			totals = totals.Add(accrued)
		}
	}

	return totals, nil
}

// LeaseItemWithPrice holds a lease item with its locked price per second.
type LeaseItemWithPrice struct {
	SkuUUID              string
	Quantity             uint64
	LockedPricePerSecond sdk.Coin
}
