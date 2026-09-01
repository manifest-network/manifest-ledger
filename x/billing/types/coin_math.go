package types

import (
	"cmp"
	"fmt"
	"slices"

	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// SafeAggregateCoins canonicalizes and sums an arbitrarily ordered collection
// of positive coins. It sorts a copy of the input and performs one checked
// linear fold, avoiding both nondeterministic map iteration and the quadratic
// cost of repeatedly merging a growing sdk.Coins value.
func SafeAggregateCoins(coins []sdk.Coin) (sdk.Coins, error) {
	if len(coins) == 0 {
		return sdk.Coins{}, nil
	}

	canonical := slices.Clone(coins)
	for index, coin := range canonical {
		if err := coin.Validate(); err != nil {
			return nil, ErrInvalidCreditOperation.Wrapf("invalid coin %d: %s", index, err)
		}
		if !coin.IsPositive() {
			return nil, ErrInvalidCreditOperation.Wrapf(
				"coin %d for denom %q must be positive",
				index,
				coin.Denom,
			)
		}
	}

	slices.SortFunc(canonical, func(left, right sdk.Coin) int {
		return cmp.Compare(left.Denom, right.Denom)
	})

	result := make(sdk.Coins, 0, len(canonical))
	for _, coin := range canonical {
		last := len(result) - 1
		if last < 0 || result[last].Denom != coin.Denom {
			result = append(result, coin)
			continue
		}

		amount, err := result[last].Amount.SafeAdd(coin.Amount)
		if err != nil {
			return nil, ErrArithmeticOverflow.Wrapf(
				"cannot aggregate %s amounts %s and %s",
				coin.Denom,
				result[last].Amount.String(),
				coin.Amount.String(),
			)
		}
		result[last].Amount = amount
	}

	return result, nil
}

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

// SafeSubtractCoins subtracts right from left in denomination order. It
// rejects malformed inputs and denomination underflow instead of calling
// sdk.Coins.Sub, which panics on underflow, and preserves typed billing errors
// for malformed or overflowing consensus state.
func SafeSubtractCoins(left, right sdk.Coins) (sdk.Coins, error) {
	if err := validateCanonicalCoins(left); err != nil {
		return nil, ErrInvalidCreditOperation.Wrapf("invalid left coin set: %s", err)
	}
	if err := validateCanonicalCoins(right); err != nil {
		return nil, ErrInvalidCreditOperation.Wrapf("invalid right coin set: %s", err)
	}

	result := make(sdk.Coins, 0, len(left))
	leftIndex, rightIndex := 0, 0
	for leftIndex < len(left) && rightIndex < len(right) {
		leftCoin := left[leftIndex]
		rightCoin := right[rightIndex]

		switch {
		case leftCoin.Denom < rightCoin.Denom:
			result = append(result, leftCoin)
			leftIndex++
		case leftCoin.Denom > rightCoin.Denom:
			return nil, ErrInvalidCreditOperation.Wrapf(
				"cannot subtract absent denom %s amount %s",
				rightCoin.Denom, rightCoin.Amount.String(),
			)
		default:
			if leftCoin.Amount.LT(rightCoin.Amount) {
				return nil, ErrInvalidCreditOperation.Wrapf(
					"cannot subtract %s amount %s from %s",
					leftCoin.Denom, rightCoin.Amount.String(), leftCoin.Amount.String(),
				)
			}
			amount, err := leftCoin.Amount.SafeSub(rightCoin.Amount)
			if err != nil {
				return nil, ErrArithmeticOverflow.Wrapf(
					"cannot subtract %s amount %s from %s",
					leftCoin.Denom, rightCoin.Amount.String(), leftCoin.Amount.String(),
				)
			}
			if amount.IsPositive() {
				result = append(result, sdk.Coin{Denom: leftCoin.Denom, Amount: amount})
			}
			leftIndex++
			rightIndex++
		}
	}

	if rightIndex < len(right) {
		rightCoin := right[rightIndex]
		return nil, ErrInvalidCreditOperation.Wrapf(
			"cannot subtract absent denom %s amount %s",
			rightCoin.Denom, rightCoin.Amount.String(),
		)
	}

	result = append(result, left[leftIndex:]...)
	if result == nil {
		return sdk.Coins{}, nil
	}
	return result, nil
}

// ReconcilePreV4ReservationAggregate applies the shared v2→v3/import repair
// policy to aggregate-only reservation state. Modern live leases establish the
// exact provable floor. A live zero-duration lease is an opaque claimant, so
// unknown historical excess must be preserved; without a live opaque claimant,
// the aggregate is reconciled exactly to the modern floor.
func ReconcilePreV4ReservationAggregate(
	actual,
	knownModernFloor sdk.Coins,
	hasLiveLegacy bool,
) (sdk.Coins, error) {
	normalizedActual, err := SafeAddCoins(sdk.NewCoins(), actual)
	if err != nil {
		return nil, fmt.Errorf("validate pre-v4 reservation aggregate: %w", err)
	}
	normalizedFloor, err := SafeAddCoins(sdk.NewCoins(), knownModernFloor)
	if err != nil {
		return nil, fmt.Errorf("validate modern reservation floor: %w", err)
	}
	if hasLiveLegacy {
		return normalizedActual.Max(normalizedFloor), nil
	}
	return normalizedFloor, nil
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
