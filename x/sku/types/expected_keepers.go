package types

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// AccountKeeper defines the account operation required by SKU simulations.
type AccountKeeper interface {
	GetAccount(context.Context, sdk.AccAddress) sdk.AccountI
}

// BankKeeper defines the bank operation required by SKU simulations.
type BankKeeper interface {
	SpendableCoins(context.Context, sdk.AccAddress) sdk.Coins
}
