package keeper

import "cosmossdk.io/errors"

var (
	ErrGettingMinter         = errors.Register(ModuleName, 1, "getting minter in ante handler")
	ErrManualMintingDisabled = errors.Register(ModuleName, 2, "manual minting is disabled due to inflation being >0")
)
