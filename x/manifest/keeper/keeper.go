package keeper

import (
	"context"

	sdkmath "cosmossdk.io/math"

	mintkeeper "github.com/cosmos/cosmos-sdk/x/mint/keeper"
)

const (
	ModuleName = "manifest"
)

type (
	Keeper struct {
		mintKeeper mintkeeper.Keeper
	}
)

func NewKeeper(mintKeeper mintkeeper.Keeper) Keeper {
	return Keeper{
		mintKeeper: mintKeeper,
	}
}

// IsManualMintingEnabled returns nil if inflation mint is 0% (disabled)
func (k Keeper) IsManualMintingEnabled(ctx context.Context) error {
	minter, err := k.mintKeeper.Minter.Get(ctx)
	if err != nil {
		return ErrGettingMinter.Wrapf("error getting minter: %s", err.Error())
	}

	// if inflation is 0, then manual minting is enabled for the PoA admin
	if minter.Inflation.Equal(sdkmath.LegacyZeroDec()) {
		return nil
	}

	return ErrManualMintingDisabled.Wrapf("inflation: %s", minter.Inflation.String())
}
