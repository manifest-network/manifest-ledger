package module

import (
	"context"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"

	sdkmath "cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/telemetry"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	mintkeeper "github.com/cosmos/cosmos-sdk/x/mint/keeper"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
	"github.com/liftedinit/manifest-ledger/x/manifest/keeper"
	manifesttypes "github.com/liftedinit/manifest-ledger/x/manifest/types"
)

// BeginBlocker mints new tokens for the previous block.
func BeginBlocker(ctx context.Context, k keeper.Keeper, mk mintkeeper.Keeper, bk bankkeeper.Keeper) error {

	ic := minttypes.DefaultInflationCalculationFn

	defer telemetry.ModuleMeasureSince(manifesttypes.ModuleName, time.Now(), telemetry.MetricKeyBeginBlocker)

	// fetch stored minter & params
	minter, err := mk.Minter.Get(ctx)
	if err != nil {
		return err
	}

	params, err := mk.Params.Get(ctx)
	if err != nil {
		return err
	}

	// recalculate inflation rate
	totalSupply := bk.GetSupply(ctx, "umfx").Amount

	// bondedRatio, err := k.BondedRatio(ctx)
	// if err != nil {
	// 	return err
	// }
	// always 0 for us
	bondedRatio := sdkmath.LegacyZeroDec()

	minter.Inflation = ic(ctx, minter, params, bondedRatio)
	minter.AnnualProvisions = minter.NextAnnualProvisions(params, totalSupply)
	if err = mk.Minter.Set(ctx, minter); err != nil {
		return err
	}

	// mint coins, update supply
	mintedCoin := minter.BlockProvision(params)
	mintedCoins := sdk.NewCoins(mintedCoin)

	err = mk.MintCoins(ctx, mintedCoins)
	if err != nil {
		return err
	}

	// Payout
	if err := k.PayoutStakeholders(ctx, mintedCoin); err != nil {
		return err
	}

	// send the minted coins to the fee collector account
	err = mk.AddCollectedFees(ctx, mintedCoins)
	if err != nil {
		return err
	}

	if mintedCoin.Amount.IsInt64() {
		defer telemetry.ModuleSetGauge(minttypes.ModuleName, float32(mintedCoin.Amount.Int64()), "minted_tokens")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			minttypes.EventTypeMint,
			sdk.NewAttribute(minttypes.AttributeKeyBondedRatio, bondedRatio.String()),
			sdk.NewAttribute(minttypes.AttributeKeyInflation, minter.Inflation.String()),
			sdk.NewAttribute(minttypes.AttributeKeyAnnualProvisions, minter.AnnualProvisions.String()),
			sdk.NewAttribute(sdk.AttributeKeyAmount, mintedCoin.Amount.String()),
		),
	)

	return nil
}
