package types

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
)

// AccountKeeper defines the account operations required by the billing module.
type AccountKeeper interface {
	GetAccount(context.Context, sdk.AccAddress) sdk.AccountI
	NewAccountWithAddress(context.Context, sdk.AccAddress) sdk.AccountI
	SetAccount(context.Context, sdk.AccountI)
}

// BankKeeper defines the bank operations required by the billing module.
type BankKeeper interface {
	SendCoins(context.Context, sdk.AccAddress, sdk.AccAddress, sdk.Coins) error
	GetBalance(context.Context, sdk.AccAddress, string) sdk.Coin
	SpendableCoins(context.Context, sdk.AccAddress) sdk.Coins
	AllBalances(context.Context, *banktypes.QueryAllBalancesRequest) (*banktypes.QueryAllBalancesResponse, error)
}
