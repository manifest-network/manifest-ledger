package keeper

import (
	"context"

	"github.com/liftedinit/manifest-ledger/x/manifest/types"

	"cosmossdk.io/collections"
	storetypes "cosmossdk.io/core/store"
	"cosmossdk.io/log"
	sdkmath "cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/codec"
	mintkeeper "github.com/cosmos/cosmos-sdk/x/mint/keeper"
)

type Keeper struct {
	cdc codec.BinaryCodec

	logger log.Logger

	mintKeeper mintkeeper.Keeper

	// state management
	Schema collections.Schema
	Params collections.Item[types.Params]

	authority string
}

// NewKeeper creates a new poa Keeper instance
func NewKeeper(
	cdc codec.BinaryCodec,
	storeService storetypes.KVStoreService,
	mintKeeper mintkeeper.Keeper,
	logger log.Logger,
	authority string,
) Keeper {
	logger = logger.With(log.ModuleKey, "x/"+types.ModuleName)

	sb := collections.NewSchemaBuilder(storeService)

	k := Keeper{
		cdc:    cdc,
		logger: logger,

		mintKeeper: mintKeeper,

		// Stores
		Params: collections.NewItem(sb, types.ParamsKey, "params", codec.CollValue[types.Params](cdc)),
	}

	schema, err := sb.Build()
	if err != nil {
		panic(err)
	}

	k.Schema = schema

	return k
}

func (k Keeper) Logger() log.Logger {
	return k.logger
}

func (k *Keeper) SetAuthority(ctx context.Context, authority string) {
	k.authority = authority
}

// ExportGenesis exports the module's state to a genesis state.
func (k *Keeper) ExportGenesis(ctx context.Context) *types.GenesisState {
	params, err := k.Params.Get(ctx)
	if err != nil {
		panic(err)
	}

	return &types.GenesisState{
		Params: params,
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
