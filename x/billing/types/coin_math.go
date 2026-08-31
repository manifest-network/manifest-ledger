package types

import (
	"fmt"

	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// ValidateLeaseItemPricing applies the business invariants shared by
// reservation, accrual, import, migration, and query paths. Locked prices are
// per-second rates persisted in lease state and must be valid and positive.
func ValidateLeaseItemPricing(lockedPrice sdk.Coin, quantity uint64) error {
	if err := lockedPrice.Validate(); err != nil || !lockedPrice.IsPositive() {
		return ErrInvalidCreditOperation.Wrapf(
			"invalid locked price for denom %q",
			lockedPrice.Denom,
		)
	}
	if quantity == 0 || quantity > MaxQuantityPerItem {
		return ErrInvalidQuantity.Wrapf(
			"quantity must be between 1 and %d, got %d",
			MaxQuantityPerItem, quantity,
		)
	}
	return nil
}

// SafeMultiplyCoin multiplies a coin amount without allowing math.Int's
// fixed-width overflow panic to escape into a transaction or query path.
func SafeMultiplyCoin(coin sdk.Coin, multiplier sdkmath.Int) (sdk.Coin, error) {
	if err := coin.Validate(); err != nil {
		return sdk.Coin{}, ErrInvalidCreditOperation.Wrapf("invalid coin: %s", err)
	}
	if multiplier.IsNil() {
		return sdk.Coin{}, ErrInvalidCreditOperation.Wrap("coin multiplier is nil")
	}
	if multiplier.IsNegative() {
		return sdk.Coin{}, ErrInvalidCreditOperation.Wrapf("coin multiplier must not be negative: %s", multiplier.String())
	}

	amount, err := coin.Amount.SafeMul(multiplier)
	if err != nil {
		return sdk.Coin{}, ErrArithmeticOverflow.Wrapf(
			"cannot multiply %s amount %s by %s",
			coin.Denom, coin.Amount.String(), multiplier.String(),
		)
	}

	return sdk.Coin{Denom: coin.Denom, Amount: amount}, nil
}

// SafeAddCoins merges two canonical coin sets in denomination order, checking
// same-denomination math.Int additions for overflow. Unlike sdk.Coins.Add, it
// returns malformed-state and overflow errors instead of panicking. The merge
// deliberately operates on the ordered slices and does not iterate over a map.
func SafeAddCoins(left, right sdk.Coins) (sdk.Coins, error) {
	if err := validateCanonicalCoins(left); err != nil {
		return nil, ErrInvalidCreditOperation.Wrapf("invalid left coin set: %s", err)
	}
	if err := validateCanonicalCoins(right); err != nil {
		return nil, ErrInvalidCreditOperation.Wrapf("invalid right coin set: %s", err)
	}

	result := make(sdk.Coins, 0, len(left)+len(right))
	leftIndex, rightIndex := 0, 0
	for leftIndex < len(left) && rightIndex < len(right) {
		leftCoin := left[leftIndex]
		rightCoin := right[rightIndex]

		switch {
		case leftCoin.Denom < rightCoin.Denom:
			result = append(result, leftCoin)
			leftIndex++
		case leftCoin.Denom > rightCoin.Denom:
			result = append(result, rightCoin)
			rightIndex++
		default:
			amount, err := leftCoin.Amount.SafeAdd(rightCoin.Amount)
			if err != nil {
				return nil, ErrArithmeticOverflow.Wrapf(
					"cannot add %s amounts %s and %s",
					leftCoin.Denom, leftCoin.Amount.String(), rightCoin.Amount.String(),
				)
			}
			if amount.IsPositive() {
				result = append(result, sdk.Coin{Denom: leftCoin.Denom, Amount: amount})
			}
			leftIndex++
			rightIndex++
		}
	}

	result = append(result, left[leftIndex:]...)
	result = append(result, right[rightIndex:]...)
	if result == nil {
		return sdk.Coins{}, nil
	}
	return result, nil
}

func validateCanonicalCoins(coins sdk.Coins) error {
	// Validate each coin first because sdk.Coins.Validate assumes non-nil
	// amounts when checking positivity.
	for index, coin := range coins {
		if err := coin.Validate(); err != nil {
			return fmt.Errorf("coin %d: %w", index, err)
		}
	}
	if err := coins.Validate(); err != nil {
		return err
	}
	return nil
}
