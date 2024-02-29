package keeper

import (
	"context"

	"github.com/liftedinit/manifest-ledger/x/manifest/types"

	"cosmossdk.io/collections"
	storetypes "cosmossdk.io/core/store"
	"cosmossdk.io/log"
	sdkmath "cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	mintkeeper "github.com/cosmos/cosmos-sdk/x/mint/keeper"
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

func (k *Keeper) GetShareHolders(ctx context.Context) []*types.StakeHolders {
	params, err := k.Params.Get(ctx)
	if err != nil {
		panic(err)
	}

	return params.StakeHolders
}

// IsManualMintingEnabled returns nil if inflation mint is 0% (disabled)
func (k Keeper) IsManualMintingEnabled(ctx context.Context) bool {
	params, err := k.Params.Get(ctx)
	if err != nil {
		panic(err)
	}

	return !params.Inflation.AutomaticEnabled
}

// Returns the amount of coins to be distributed to the holders
func (k Keeper) CalculateShareHolderTokenPayout(ctx context.Context, c sdk.Coin) map[string]sdk.Coin {
	sh := k.GetShareHolders(ctx)

	pairs := make(map[string]sdk.Coin, len(sh))

	// iter each stakeholder, get their percent of the total 100%, and then split up their amount of coin cost
	for _, s := range sh {
		s := s
		pct := sdkmath.NewInt(int64(s.Percentage)).ToLegacyDec().QuoInt64(types.MaxPercentShare)
		coinAmt := pct.MulInt(c.Amount).RoundInt()

		if coinAmt.IsZero() {
			// too small of an amount to matter (< 1 utoken)
			continue
		}

		pairs[s.Address] = sdk.NewCoin(c.Denom, coinAmt)
	}

	return pairs
}

// PayoutStakeholders mints coins and sends them to the stakeholders.
// This is called from the endblocker, so panics should never happen.
// If it does, something is very wrong w/ the SDK. Any logic specific to auto minting
// should be kept out of this to properly handle and return nil instead.
func (k Keeper) PayoutStakeholders(ctx context.Context, c sdk.Coin) error {
	pairs := k.CalculateShareHolderTokenPayout(ctx, c)

	if err := k.bankKeeper.MintCoins(ctx, types.ModuleName, sdk.NewCoins(c)); err != nil {
		return err
	}

	for addr, coin := range pairs {
		accAddr, err := sdk.AccAddressFromBech32(addr)
		if err != nil {
			return err
		}

		// send from the mintKeeper -> the stakeholder
		if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, accAddr, sdk.NewCoins(coin)); err != nil {
			return err
		}
	}

	return nil
}

// BlockRewardsProvision Gets the amount of coins that are automatically minted every block
// per the automatic inflation
func (k Keeper) BlockRewardsProvision(ctx context.Context, denom string) sdk.Coin {
	mkParams, err := k.mintKeeper.Params.Get(ctx)
	if err != nil {
		panic(err)
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		panic(err)
	}

	amtPerYear := params.Inflation.YearlyAmount
	blocksPerYear := mkParams.BlocksPerYear

	if blocksPerYear < 10 {
		k.logger.Error("x/mint blocks per year param is too low", "blocks", blocksPerYear)
		return sdk.NewCoin(denom, sdkmath.ZeroInt())
	}

	div := amtPerYear / blocksPerYear

	// return the amount of coins to be minted per block
	return sdk.NewCoin(denom, sdkmath.NewIntFromUint64(div))
}
