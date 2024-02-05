package keeper

import (
	"context"
	"fmt"

	sdkmath "cosmossdk.io/math"

	mintkeeper "github.com/cosmos/cosmos-sdk/x/mint/keeper"
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

// IsManualMintingEnabled returns true if inflation mint is 0% (disabled).
// Then used in TokenFactory
func (k Keeper) IsManualMintingEnabled(ctx context.Context) error {
	minter, err := k.mintKeeper.Minter.Get(ctx)
	if err != nil {
		return fmt.Errorf("IsManualMintingEnabled error getting minter: %s", err)
	}

	// if inflation is 0, then manual minting is enabled for the PoA admin
	if minter.Inflation.Equal(sdkmath.LegacyZeroDec()) {
		return nil
	}

	return fmt.Errorf("manual minting is disabled due to inflation being >0: %s", minter.Inflation.String())
}
