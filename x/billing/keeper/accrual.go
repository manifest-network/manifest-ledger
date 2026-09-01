package keeper

import (
	"errors"
	"fmt"
	"strings"
	"time"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/types"
	skutypes "github.com/manifest-network/manifest-ledger/x/sku/types"
)

// ConvertBasePriceToPerSecond converts a SKU's base price to a per-second rate.
// The SKU's Unit determines how to interpret the base price:
// - UNIT_PER_HOUR: divide by 3600
// - UNIT_PER_DAY: divide by 86400
// Returns the per-second rate as a Coin with the same denom as the base price.
// Returns an error if the conversion fails (invalid unit, zero rate, or inexact division).
// Note: SKUs are validated at creation time, so this should not fail for valid SKUs.
func ConvertBasePriceToPerSecond(basePrice sdk.Coin, unit skutypes.Unit) (sdk.Coin, error) {
	if err := basePrice.Validate(); err != nil || !basePrice.IsPositive() {
		return sdk.Coin{}, types.ErrInvalidCreditOperation.Wrapf(
			"invalid base price for denom %q",
			basePrice.Denom,
		)
	}
	perSecond, ok := skutypes.CalculatePricePerSecond(basePrice, unit)
	if !ok {
		return sdk.Coin{}, fmt.Errorf("failed to convert base price %s with unit %s to per-second rate", basePrice, unit)
	}
	return sdk.Coin{Denom: basePrice.Denom, Amount: perSecond}, nil
}

// CalculateAccruedAmount calculates the amount accrued for a lease item
// over a given duration.
// accrued = lockedPricePerSecond * quantity * durationSeconds
// Returns an error if the calculation would overflow.
func CalculateAccruedAmount(lockedPricePerSecond sdk.Coin, quantity uint64, duration time.Duration) (sdk.Coin, error) {
	return calculateAccruedAmountForSeconds(
		lockedPricePerSecond,
		quantity,
		math.NewInt(int64(duration/time.Second)),
	)
}

// calculateAccruedAmountForSeconds is the monetary accrual core. Runtime
// callers derive seconds directly from timestamps so time.Duration's roughly
// 292-year saturation cannot truncate a historical settlement interval.
func calculateAccruedAmountForSeconds(lockedPricePerSecond sdk.Coin, quantity uint64, durationSeconds math.Int) (sdk.Coin, error) {
	if err := types.ValidateLeaseItemPricing(lockedPricePerSecond, quantity); err != nil {
		return sdk.Coin{}, err
	}

	if !durationSeconds.IsPositive() {
		return sdk.Coin{Denom: lockedPricePerSecond.Denom, Amount: math.ZeroInt()}, nil
	}

	// accrued = price_per_second * quantity * seconds
	quantityInt := math.NewIntFromUint64(quantity)

	quantityRate, err := types.SafeMultiplyCoin(lockedPricePerSecond, quantityInt)
	if err != nil {
		return sdk.Coin{}, errorsmod.Wrap(err, "multiply locked price by quantity")
	}
	result, err := types.SafeMultiplyCoin(quantityRate, durationSeconds)
	if err != nil {
		return sdk.Coin{}, errorsmod.Wrap(err, "multiply item rate by duration")
	}

	return result, nil
}

func validateLeaseAccrualItems(items []LeaseItemWithPrice) error {
	for _, item := range items {
		if err := types.ValidateLeaseItemPricing(item.LockedPricePerSecond, item.Quantity); err != nil {
			return errorsmod.Wrapf(err, "validate accrual input for sku %s", item.SkuUUID)
		}
	}
	return nil
}

// AccrualOverflowError reports denoms whose complete accrued amount cannot be
// represented by math.Int. Denoms retain their first-detected overflow order.
// Callers may still use the exact totals returned for every unaffected denom.
type AccrualOverflowError struct {
	Denoms []string
}

func (e *AccrualOverflowError) Error() string {
	return fmt.Sprintf("%s: denoms %s", types.ErrArithmeticOverflow.Error(), strings.Join(e.Denoms, ","))
}

// Unwrap supports errors.Is/errors.As while Cause and the ABCI methods retain
// the registered billing error code through Cosmos SDK error wrapping.
func (e *AccrualOverflowError) Unwrap() error { return types.ErrArithmeticOverflow }

// Cause returns the registered billing arithmetic-overflow error.
func (e *AccrualOverflowError) Cause() error { return types.ErrArithmeticOverflow }

// ABCICode returns the registered billing arithmetic-overflow ABCI code.
func (e *AccrualOverflowError) ABCICode() uint32 {
	return types.ErrArithmeticOverflow.ABCICode()
}

// Codespace returns the billing module's error codespace.
func (e *AccrualOverflowError) Codespace() string {
	return types.ErrArithmeticOverflow.Codespace()
}

// CalculateTotalAccruedForLease calculates exact accrued totals for all denoms
// that remain representable. If one or more denoms overflow, it returns the
// unaffected totals together with an AccrualOverflowError naming the overflowed
// denoms in deterministic first-detected overflow order.
func CalculateTotalAccruedForLease(items []LeaseItemWithPrice, duration time.Duration) (sdk.Coins, error) {
	return calculateTotalAccruedForLeaseSeconds(items, math.NewInt(int64(duration/time.Second)))
}

func calculateTotalAccruedForLeaseSeconds(items []LeaseItemWithPrice, durationSeconds math.Int) (sdk.Coins, error) {
	// Validate the entire stored item set before calculating. In particular, a
	// prior overflow for one denom must not hide a malformed later item.
	if err := validateLeaseAccrualItems(items); err != nil {
		return nil, err
	}

	totals := sdk.NewCoins()
	overflowed := make(map[string]struct{})
	overflowDenoms := make([]string, 0)
	markOverflow := func(denom string) {
		if _, exists := overflowed[denom]; exists {
			return
		}
		overflowed[denom] = struct{}{}
		overflowDenoms = append(overflowDenoms, denom)
		totals = removeCoinByDenom(totals, denom)
	}

	for _, item := range items {
		if _, alreadyOverflowed := overflowed[item.LockedPricePerSecond.Denom]; alreadyOverflowed {
			continue
		}
		accrued, err := calculateAccruedAmountForSeconds(item.LockedPricePerSecond, item.Quantity, durationSeconds)
		if err != nil {
			if errors.Is(err, types.ErrArithmeticOverflow) {
				markOverflow(item.LockedPricePerSecond.Denom)
				continue
			}
			return nil, errorsmod.Wrapf(err, "calculate accrual for sku %s", item.SkuUUID)
		}
		if accrued.IsPositive() {
			totals, err = types.SafeAddCoins(totals, sdk.Coins{accrued})
			if err != nil {
				if errors.Is(err, types.ErrArithmeticOverflow) {
					markOverflow(accrued.Denom)
					continue
				}
				return nil, errorsmod.Wrapf(err, "sum accrued amount for sku %s", item.SkuUUID)
			}
		}
	}

	if len(overflowDenoms) > 0 {
		return totals, &AccrualOverflowError{Denoms: overflowDenoms}
	}
	return totals, nil
}

// elapsedWholeSeconds returns end-start truncated toward zero at whole-second
// precision, matching time.Duration division without time.Duration saturation.
func elapsedWholeSeconds(start, end time.Time) (math.Int, error) {
	seconds, err := math.NewInt(end.Unix()).SafeSub(math.NewInt(start.Unix()))
	if err != nil {
		return math.Int{}, errorsmod.Wrap(types.ErrArithmeticOverflow, "calculate timestamp second difference")
	}

	nanosDelta := end.Nanosecond() - start.Nanosecond()
	switch {
	case seconds.IsPositive() && nanosDelta < 0:
		seconds, err = seconds.SafeSub(math.OneInt())
	case seconds.IsNegative() && nanosDelta > 0:
		seconds, err = seconds.SafeAdd(math.OneInt())
	}
	if err != nil {
		return math.Int{}, errorsmod.Wrap(types.ErrArithmeticOverflow, "adjust timestamp second difference")
	}
	return seconds, nil
}

// elapsedWholeSecondsAndAccrualCursor returns the billable whole seconds in a
// forward interval and the timestamp through which those seconds reach. The
// cursor deliberately excludes the interval's sub-second remainder so a live
// lease can carry that remainder into its next settlement.
//
// The cursor is derived by subtracting at most one second from end. It never
// converts the complete interval to time.Duration, whose roughly 292-year
// range would otherwise saturate and undercharge historical timestamps.
func elapsedWholeSecondsAndAccrualCursor(start, end time.Time) (math.Int, time.Time, error) {
	seconds, err := elapsedWholeSeconds(start, end)
	if err != nil {
		return math.Int{}, time.Time{}, err
	}
	if !end.After(start) {
		return seconds, start, nil
	}

	remainderNanoseconds := int64(end.Nanosecond()) - int64(start.Nanosecond())
	if remainderNanoseconds < 0 {
		remainderNanoseconds += int64(time.Second)
	}
	return seconds, end.Add(-time.Duration(remainderNanoseconds)), nil
}

func removeCoinByDenom(coins sdk.Coins, denom string) sdk.Coins {
	for index, coin := range coins {
		if coin.Denom != denom {
			continue
		}
		result := make(sdk.Coins, 0, len(coins)-1)
		result = append(result, coins[:index]...)
		result = append(result, coins[index+1:]...)
		return result
	}
	return coins
}

// LeaseItemWithPrice holds a lease item with its locked price per second.
type LeaseItemWithPrice struct {
	SkuUUID              string
	Quantity             uint64
	LockedPricePerSecond sdk.Coin
}
