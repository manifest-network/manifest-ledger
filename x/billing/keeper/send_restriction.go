package keeper

import (
	"context"
	"errors"

	"cosmossdk.io/collections"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

// CreditAccountSendRestriction creates a send restriction that only allows
// the billing module's configured denom to be sent to credit account addresses.
// This prevents users from accidentally sending wrong tokens to credit accounts,
// which would result in loss of funds.
//
// The restriction follows a "fail-closed" security model:
// - Allows any transfer if the destination is NOT a credit account
// - For credit account destinations, only allows the configured billing denom
// - Returns an error if params lookup fails (fail-closed for security)
// - Returns an error if attempting to send non-billing tokens to a credit account
func (k *Keeper) CreditAccountSendRestriction(ctx context.Context, _, toAddr sdk.AccAddress, amt sdk.Coins) (sdk.AccAddress, error) {
	// Check if destination is a credit account by checking if any tenant's
	// derived credit address matches the toAddr
	isCreditAccount, err := k.isCreditAccountAddress(ctx, toAddr)
	if err != nil {
		// Fail closed: if we can't determine credit account status, block the transfer
		// This prevents potential bypass if the index is corrupted
		return toAddr, types.ErrInvalidCreditOperation.Wrapf(
			"unable to verify credit account status: %v", err,
		)
	}

	if !isCreditAccount {
		// Not a credit account, allow any transfer
		return toAddr, nil
	}

	// This is a credit account destination - validate the denomination
	params, err := k.GetParams(ctx)
	if err != nil {
		// Fail closed: if we can't get params, block the transfer to credit accounts
		// This prevents sending wrong denomination if params are corrupted/missing
		return toAddr, types.ErrInvalidCreditOperation.Wrapf(
			"unable to retrieve billing params for denomination validation: %v", err,
		)
	}

	// Check that all coins being sent are the allowed denomination
	for _, coin := range amt {
		if coin.Denom != params.Denom {
			return toAddr, types.ErrInvalidDenomination.Wrapf(
				"cannot send %s to credit account; only %s is allowed",
				coin.Denom,
				params.Denom,
			)
		}
	}

	return toAddr, nil
}

// isCreditAccountAddress checks if the given address is a derived credit account address.
// Uses O(1) reverse index lookup instead of iterating through all credit accounts.
func (k *Keeper) isCreditAccountAddress(ctx context.Context, addr sdk.AccAddress) (bool, error) {
	// Use the reverse index for O(1) lookup
	_, err := k.CreditAddressIndex.Get(ctx, addr)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, collections.ErrNotFound) {
		return false, nil
	}
	return false, err
}

// RegisterSendRestriction registers the credit account send restriction with the bank keeper.
// This should be called during app initialization after the billing keeper is set up.
func (k *Keeper) RegisterSendRestriction() {
	if k.bankKeeper != nil {
		k.bankKeeper.AppendSendRestriction(k.CreditAccountSendRestriction)
	}
}
