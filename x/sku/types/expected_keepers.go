package types

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// AccountKeeper defines the account operation required by SKU simulations.
type AccountKeeper interface {
	GetAccount(context.Context, sdk.AccAddress) sdk.AccountI
}

// BankKeeper defines payout-policy checks and simulation funding queries.
type BankKeeper interface {
	BlockedAddr(sdk.AccAddress) bool
	SpendableCoins(context.Context, sdk.AccAddress) sdk.Coins
}
