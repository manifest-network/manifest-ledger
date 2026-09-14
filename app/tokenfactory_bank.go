package app

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

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
	bank            tokenfactorytypes.BankKeeper
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
	return k.bank.SendCoins(ctx, from, to, amount)
}

// SendCoinsFromAccountToModule covers the first step of tokenfactory burn-from;
// the burn itself only runs after this transfer succeeds.
func (k tokenFactoryBankKeeper) SendCoinsFromAccountToModule(ctx context.Context, from sdk.AccAddress, to string, amount sdk.Coins) error {
	if err := k.checkDebitSource(ctx, from); err != nil {
		return err
	}
	return k.bank.SendCoinsFromAccountToModule(ctx, from, to, amount)
}

// Forward only reviewed bank capabilities. A future upstream interface method
// must be implemented explicitly here before the compile-time contract passes.
func (k tokenFactoryBankKeeper) GetDenomMetaData(ctx context.Context, denom string) (banktypes.Metadata, bool) {
	return k.bank.GetDenomMetaData(ctx, denom)
}

func (k tokenFactoryBankKeeper) SetDenomMetaData(ctx context.Context, metadata banktypes.Metadata) {
	k.bank.SetDenomMetaData(ctx, metadata)
}

func (k tokenFactoryBankKeeper) HasSupply(ctx context.Context, denom string) bool {
	return k.bank.HasSupply(ctx, denom)
}

func (k tokenFactoryBankKeeper) IterateTotalSupply(ctx context.Context, cb func(sdk.Coin) bool) {
	k.bank.IterateTotalSupply(ctx, cb)
}

func (k tokenFactoryBankKeeper) SendCoinsFromModuleToAccount(ctx context.Context, sender string, recipient sdk.AccAddress, amount sdk.Coins) error {
	return k.bank.SendCoinsFromModuleToAccount(ctx, sender, recipient, amount)
}

func (k tokenFactoryBankKeeper) MintCoins(ctx context.Context, module string, amount sdk.Coins) error {
	return k.bank.MintCoins(ctx, module, amount)
}

func (k tokenFactoryBankKeeper) BurnCoins(ctx context.Context, module string, amount sdk.Coins) error {
	return k.bank.BurnCoins(ctx, module, amount)
}

func (k tokenFactoryBankKeeper) HasBalance(ctx context.Context, address sdk.AccAddress, amount sdk.Coin) bool {
	return k.bank.HasBalance(ctx, address, amount)
}

func (k tokenFactoryBankKeeper) GetAllBalances(ctx context.Context, address sdk.AccAddress) sdk.Coins {
	return k.bank.GetAllBalances(ctx, address)
}

func (k tokenFactoryBankKeeper) SpendableCoins(ctx context.Context, address sdk.AccAddress) sdk.Coins {
	return k.bank.SpendableCoins(ctx, address)
}

func (k tokenFactoryBankKeeper) GetBalance(ctx context.Context, address sdk.AccAddress, denom string) sdk.Coin {
	return k.bank.GetBalance(ctx, address, denom)
}

func (k tokenFactoryBankKeeper) BlockedAddr(address sdk.AccAddress) bool {
	return k.bank.BlockedAddr(address)
}
