package app

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"

	tokenfactorytypes "github.com/strangelove-ventures/tokenfactory/x/tokenfactory/types"

	billingtypes "github.com/manifest-network/manifest-ledger/x/billing/types"
)

type creditAddressIndexFunc func(context.Context, sdk.AccAddress) (bool, error)

func (f creditAddressIndexFunc) Has(ctx context.Context, address sdk.AccAddress) (bool, error) {
	return f(ctx, address)
}

type debitBankSpy struct {
	tokenfactorytypes.BankKeeper
	calls int
	err   error
}

func (b *debitBankSpy) SendCoins(context.Context, sdk.AccAddress, sdk.AccAddress, sdk.Coins) error {
	b.calls++
	return b.err
}

func (b *debitBankSpy) SendCoinsFromAccountToModule(context.Context, sdk.AccAddress, string, sdk.Coins) error {
	b.calls++
	return b.err
}

func TestTokenFactoryBankKeeperChecksCreditSourceBeforeWrites(t *testing.T) {
	indexErr := errors.New("unavailable credit address index")
	bankErr := errors.New("underlying bank error")
	for _, operation := range []string{"force transfer", "burn from"} {
		t.Run(operation, func(t *testing.T) {
			for _, tc := range []struct {
				name      string
				protected bool
				indexErr  error
				wantErr   error
				wantCalls int
			}{
				{name: "credit account", protected: true, wantErr: billingtypes.ErrInvalidCreditOperation},
				{name: "lookup failure", indexErr: indexErr, wantErr: indexErr},
				{name: "ordinary account", wantErr: bankErr, wantCalls: 1},
			} {
				t.Run(tc.name, func(t *testing.T) {
					ctx := t.Context()
					from := sdk.AccAddress("source address")
					bank := &debitBankSpy{err: bankErr}
					indexCalls := 0
					guard := tokenFactoryBankKeeper{
						BankKeeper: bank,
						creditAddresses: creditAddressIndexFunc(func(gotCtx context.Context, address sdk.AccAddress) (bool, error) {
							indexCalls++
							require.Equal(t, ctx, gotCtx)
							require.Equal(t, from, address)
							return tc.protected, tc.indexErr
						}),
					}
					amount := sdk.NewCoins(sdk.NewInt64Coin("utest", 1))
					var err error
					if operation == "force transfer" {
						err = guard.SendCoins(ctx, from, sdk.AccAddress("recipient address"), amount)
					} else {
						err = guard.SendCoinsFromAccountToModule(ctx, from, tokenfactorytypes.ModuleName, amount)
					}
					require.ErrorIs(t, err, tc.wantErr)
					require.Equal(t, 1, indexCalls)
					require.Equal(t, tc.wantCalls, bank.calls)
				})
			}
		})
	}
}
