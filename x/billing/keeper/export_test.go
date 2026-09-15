package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

// ExecuteProviderLeaseWithdrawalForTesting exposes the private transition to
// the external keeper tests, which share the real application's SDK fixture.
func ExecuteProviderLeaseWithdrawalForTesting(ctx context.Context, k *Keeper, lease *types.Lease) (sdk.Coins, bool, bool, error) {
	result, err := k.executeProviderLeaseWithdrawal(ctx, lease)
	return result.transferAmounts, result.counted, result.autoClosed, err
}
