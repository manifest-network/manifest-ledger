package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	storetypes "cosmossdk.io/core/store"
	"cosmossdk.io/log"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	mintkeeper "github.com/cosmos/cosmos-sdk/x/mint/keeper"

	"github.com/liftedinit/manifest-ledger/x/manifest/types"
)

type Keeper struct {
	cdc codec.BinaryCodec

	logger log.Logger

	mintKeeper mintkeeper.Keeper
	bankKeeper bankkeeper.Keeper

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
	bankKeeper bankkeeper.Keeper,
	logger log.Logger,
	authority string,
) Keeper {
	logger = logger.With(log.ModuleKey, "x/"+types.ModuleName)

	sb := collections.NewSchemaBuilder(storeService)

	k := Keeper{
		cdc:    cdc,
		logger: logger,

		mintKeeper: mintKeeper,
		bankKeeper: bankKeeper,

		// Stores
		Params: collections.NewItem(sb, types.ParamsKey, "params", codec.CollValue[types.Params](cdc)),

		authority: authority,
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

func (k *Keeper) SetAuthority(authority string) {
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

// PayoutStakeholders mints and sends coins to stakeholders.
func (k Keeper) PayoutStakeholders(ctx context.Context, payouts map[string]sdk.Coin) error {
	for addr, p := range payouts {
		addr := addr
		p := p

		sdkAddr, err := sdk.AccAddressFromBech32(addr)
		if err != nil {
			return err
		}

		// ensure p is valid
		if !p.IsValid() {
			return fmt.Errorf("invalid payout: %v for address: %s", p, addr)
		}

		// mint & send coins
		coins := sdk.NewCoins(p)
		if err := k.bankKeeper.MintCoins(ctx, types.ModuleName, coins); err != nil {
			return err
		}

		if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, sdkAddr, coins); err != nil {
			return err
		}

		k.Logger().Info("Payout", "address", addr, "amount", coins)
	}

	return nil
}
