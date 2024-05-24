package module

import (
	"context"
	"time"

	"github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"
	mintkeeper "github.com/cosmos/cosmos-sdk/x/mint/keeper"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"

	"github.com/liftedinit/manifest-ledger/x/manifest/keeper"
	manifesttypes "github.com/liftedinit/manifest-ledger/x/manifest/types"
)

// BeginBlocker mints new tokens for the previous block.
func BeginBlocker(ctx context.Context, k keeper.Keeper, mk mintkeeper.Keeper) error {
	defer telemetry.ModuleMeasureSince(manifesttypes.ModuleName, time.Now(), telemetry.MetricKeyBeginBlocker)

	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}

	// If there is no one to pay out, skip
	if len(params.StakeHolders) == 0 {
		return nil
	}

	if !params.Inflation.AutomaticEnabled {
		k.Logger().Debug("Automatic inflation is disabled")
		return nil
	}

	// Calculate the per block inflation rewards to pay out in coins
	mintedCoin, err := k.BlockRewardsProvision(ctx, params.Inflation.MintDenom)
	if err != nil {
		return err
	}
	mintedCoins := sdk.NewCoins(mintedCoin)

	// If no inflation payout this block, skip
	if mintedCoin.IsZero() {
		return nil
	}

	// mint the tokens to the network
	if err := mk.MintCoins(ctx, mintedCoins); err != nil {
		return err
	}

	// Payout all the stakeholders with their respective share of the minted coins
	if err := k.PayoutStakeholders(ctx, mintedCoin); err != nil {
		return err
	}

	if mintedCoin.Amount.IsInt64() {
		defer telemetry.ModuleSetGauge(minttypes.ModuleName, float32(mintedCoin.Amount.Int64()), "minted_tokens")
	}

	bondedRatio, err := mk.BondedRatio(ctx)
	if err != nil {
		return err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			minttypes.EventTypeMint,
			sdk.NewAttribute(minttypes.AttributeKeyBondedRatio, bondedRatio.String()),
			sdk.NewAttribute(minttypes.AttributeKeyInflation, mintedCoin.String()),
			// sdk.NewAttribute(minttypes.AttributeKeyAnnualProvisions, minter.AnnualProvisions.String()),
			sdk.NewAttribute(sdk.AttributeKeyAmount, mintedCoin.Amount.String()),
		),
	)

	return nil
}
