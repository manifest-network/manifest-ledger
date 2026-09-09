package keeper

import (
	"context"

	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// spendableCreditCoin matches the bank send path's per-denomination vesting
// restriction without iterating unrelated bank balances. SDK SpendableCoin
// panics if locks exceed the balance; report zero usable funds in that case so
// malformed backing fails billing's reservation checks instead of panicking.
func (k *Keeper) spendableCreditCoin(ctx context.Context, address sdk.AccAddress, denom string) sdk.Coin {
	return creditCoinAfterLocks(k.bankKeeper.GetBalance(ctx, address, denom), k.bankKeeper.LockedCoins(ctx, address))
}

func creditCoinAfterLocks(balance sdk.Coin, locked sdk.Coins) sdk.Coin {
	lockedAmount := locked.AmountOf(balance.Denom)
	if balance.Amount.LTE(lockedAmount) {
		return sdk.NewCoin(balance.Denom, sdkmath.ZeroInt())
	}
	return balance.SubAmount(lockedAmount)
}
