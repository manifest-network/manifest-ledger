package app

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	tokenfactorytypes "github.com/strangelove-ventures/tokenfactory/x/tokenfactory/types"

	billingtypes "github.com/manifest-network/manifest-ledger/x/billing/types"
)

// creditAddressIndex is the context-aware reverse index maintained atomically
// with billing credit accounts. Looking up raw addresses also covers equivalent
// Bech32 spellings without scanning tenants or leases.
type creditAddressIndex interface {
	Has(context.Context, sdk.AccAddress) (bool, error)
}

// tokenFactoryBankKeeper restricts tokenfactory administrator debits from
// registered billing credit accounts. Their entire balance is nonrefundable
// credit, including amounts not currently reserved by a lease. Other keepers
// retain their normal bank dependency so deposits and billing payouts work.
type tokenFactoryBankKeeper struct {
	tokenfactorytypes.BankKeeper
	creditAddresses creditAddressIndex
}

var _ tokenfactorytypes.BankKeeper = tokenFactoryBankKeeper{}

func (k tokenFactoryBankKeeper) checkDebitSource(ctx context.Context, source sdk.AccAddress) error {
	protected, err := k.creditAddresses.Has(ctx, source)
	if err != nil {
		return fmt.Errorf("check tokenfactory debit source against billing credit accounts: %w", err)
	}
	if protected {
		return billingtypes.ErrInvalidCreditOperation.Wrap("tokenfactory cannot debit a billing credit account")
	}
	return nil
}

// SendCoins covers tokenfactory force transfers. Check before delegating so a
// denied operation cannot move coins or emit bank events, even without an outer
// transaction cache.
func (k tokenFactoryBankKeeper) SendCoins(ctx context.Context, from, to sdk.AccAddress, amount sdk.Coins) error {
	if err := k.checkDebitSource(ctx, from); err != nil {
		return err
	}
	return k.BankKeeper.SendCoins(ctx, from, to, amount)
}

// SendCoinsFromAccountToModule covers the first step of tokenfactory burn-from;
// the burn itself only runs after this transfer succeeds.
func (k tokenFactoryBankKeeper) SendCoinsFromAccountToModule(ctx context.Context, from sdk.AccAddress, to string, amount sdk.Coins) error {
	if err := k.checkDebitSource(ctx, from); err != nil {
		return err
	}
	return k.BankKeeper.SendCoinsFromAccountToModule(ctx, from, to, amount)
}
