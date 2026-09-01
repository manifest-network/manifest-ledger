package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

// providerLeaseWithdrawalResult describes the state transition performed for
// one lease in a provider-wide withdrawal page. Counted mirrors
// MsgWithdrawResponse.withdrawal_count: a successful auto-close counts even
// when no tokens remain to transfer.
type providerLeaseWithdrawalResult struct {
	transferAmounts sdk.Coins
	counted         bool
	autoClosed      bool
}

// executeProviderLeaseWithdrawal applies the consensus lifecycle for one
// ACTIVE lease. The caller must provide a per-lease CacheContext and decides
// whether to commit it, which lets MsgWithdraw commit successful leases while
// ProviderWithdrawable commits them only into a discarded page simulation.
//
// The method deliberately owns the complete per-lease transition so the query
// and transaction cannot drift on auto-close, settlement, reservation, or
// persistence semantics.
func (k *Keeper) executeProviderLeaseWithdrawal(
	ctx context.Context,
	lease *types.Lease,
) (providerLeaseWithdrawalResult, error) {
	if lease.State != types.LEASE_STATE_ACTIVE {
		return providerLeaseWithdrawalResult{}, nil
	}

	creditAccount, err := k.GetCreditAccount(ctx, lease.Tenant)
	if err != nil {
		return providerLeaseWithdrawalResult{}, errorsmod.Wrapf(
			err,
			"get credit account for provider withdrawal lease %s",
			lease.Uuid,
		)
	}

	shouldAutoClose, closeTime, err := k.ShouldAutoCloseLease(ctx, lease, &creditAccount)
	if err != nil {
		return providerLeaseWithdrawalResult{}, errorsmod.Wrapf(
			err,
			"check auto-close for provider withdrawal lease %s",
			lease.Uuid,
		)
	}
	if shouldAutoClose {
		result, err := k.AutoCloseLease(ctx, lease, &creditAccount, closeTime)
		if err != nil {
			return providerLeaseWithdrawalResult{}, errorsmod.Wrapf(
				err,
				"auto-close provider withdrawal lease %s",
				lease.Uuid,
			)
		}
		return providerLeaseWithdrawalResult{
			transferAmounts: result.TransferAmounts,
			counted:         true,
			autoClosed:      true,
		}, nil
	}

	blockTime := sdk.UnwrapSDKContext(ctx).BlockTime()
	if !blockTime.After(lease.LastSettledAt) {
		return providerLeaseWithdrawalResult{}, nil
	}

	result, err := k.PerformSettlementSilent(ctx, lease, &creditAccount, blockTime)
	if err != nil {
		return providerLeaseWithdrawalResult{}, errorsmod.Wrapf(
			err,
			"settle provider withdrawal lease %s",
			lease.Uuid,
		)
	}
	if result.TransferAmounts.IsZero() {
		return providerLeaseWithdrawalResult{}, nil
	}

	// Advance only through charged whole seconds. The retained sub-second
	// remainder participates in the next live settlement.
	lease.LastSettledAt = result.SettledThrough
	if err := k.SetLease(ctx, *lease); err != nil {
		return providerLeaseWithdrawalResult{}, errorsmod.Wrapf(
			err,
			"persist provider withdrawal lease %s",
			lease.Uuid,
		)
	}
	if err := k.SetCreditAccount(ctx, creditAccount); err != nil {
		return providerLeaseWithdrawalResult{}, errorsmod.Wrapf(
			err,
			"persist credit account for provider withdrawal lease %s",
			lease.Uuid,
		)
	}

	return providerLeaseWithdrawalResult{
		transferAmounts: result.TransferAmounts,
		counted:         true,
	}, nil
}
