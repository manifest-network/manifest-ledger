package keeper

import (
	"time"

	"cosmossdk.io/math"

	skutypes "github.com/manifest-network/manifest-ledger/x/sku/types"
)

// SecondsPerHour is the number of seconds in an hour.
const SecondsPerHour = 3600

// SecondsPerDay is the number of seconds in a day.
const SecondsPerDay = 86400

// ConvertBasePriceToPerSecond converts a SKU's base price to a per-second rate.
// The SKU's Unit determines how to interpret the base price:
// - UNIT_PER_HOUR: divide by 3600
// - UNIT_PER_DAY: divide by 86400
// Returns the per-second rate in the smallest denomination.
// Note: Integer division may result in precision loss for small amounts.
func ConvertBasePriceToPerSecond(basePrice math.Int, unit skutypes.Unit) math.Int {
	switch unit {
	case skutypes.Unit_UNIT_PER_HOUR:
		return basePrice.Quo(math.NewInt(SecondsPerHour))
	case skutypes.Unit_UNIT_PER_DAY:
		return basePrice.Quo(math.NewInt(SecondsPerDay))
	default:
		// UNIT_UNSPECIFIED - treat as per second
		return basePrice
	}
}

// CalculateAccruedAmount calculates the amount accrued for a lease item
// over a given duration.
// accrued = lockedPricePerSecond * quantity * durationSeconds
func CalculateAccruedAmount(lockedPricePerSecond math.Int, quantity uint64, duration time.Duration) math.Int {
	durationSeconds := int64(duration.Seconds())
	if durationSeconds < 0 {
		return math.ZeroInt()
	}

	// accrued = price_per_second * quantity * seconds
	quantityInt := math.NewIntFromUint64(quantity)
	secondsInt := math.NewInt(durationSeconds)

	return lockedPricePerSecond.Mul(quantityInt).Mul(secondsInt)
}

// CalculateTotalAccruedForLease calculates the total accrued amount for all items
// in a lease over the given duration.
func CalculateTotalAccruedForLease(items []LeaseItemWithPrice, duration time.Duration) math.Int {
	total := math.ZeroInt()

	for _, item := range items {
		accrued := CalculateAccruedAmount(item.LockedPricePerSecond, item.Quantity, duration)
		total = total.Add(accrued)
	}

	return total
}

// LeaseItemWithPrice holds a lease item with its locked price per second.
type LeaseItemWithPrice struct {
	SkuID                uint64
	Quantity             uint64
	LockedPricePerSecond math.Int
}
