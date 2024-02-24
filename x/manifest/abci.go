package module

import (
	"context"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/cosmos/cosmos-sdk/telemetry"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	mintkeeper "github.com/cosmos/cosmos-sdk/x/mint/keeper"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
	"github.com/liftedinit/manifest-ledger/x/manifest/keeper"
	manifesttypes "github.com/liftedinit/manifest-ledger/x/manifest/types"
)

// BeginBlocker mints new tokens for the previous block.
func BeginBlocker(ctx context.Context, k keeper.Keeper, mk mintkeeper.Keeper, bk bankkeeper.Keeper) error {
	defer telemetry.ModuleMeasureSince(manifesttypes.ModuleName, time.Now(), telemetry.MetricKeyBeginBlocker)

	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}

	mintedCoin := k.BlockRewardsProvision(ctx, params.Inflation.MintDenom)
	mintedCoins := sdk.NewCoins(mintedCoin)

	if err := mk.MintCoins(ctx, mintedCoins); err != nil {
		return err
	}

	if err := k.PayoutStakeholders(ctx, mintedCoin); err != nil {
		return err
	}

	// send the minted coins to the fee collector account
	// if err := mk.AddCollectedFees(ctx, mintedCoins); err != nil {
	// 	return err
	// }

	if mintedCoin.Amount.IsInt64() {
		defer telemetry.ModuleSetGauge(minttypes.ModuleName, float32(mintedCoin.Amount.Int64()), "minted_tokens")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			minttypes.EventTypeMint,
			// sdk.NewAttribute(minttypes.AttributeKeyBondedRatio, bondedRatio.String()),
			// sdk.NewAttribute(minttypes.AttributeKeyInflation, minter.Inflation.String()),
			// sdk.NewAttribute(minttypes.AttributeKeyAnnualProvisions, minter.AnnualProvisions.String()),
			sdk.NewAttribute(sdk.AttributeKeyAmount, mintedCoin.Amount.String()),
		),
	)

	return nil
}
