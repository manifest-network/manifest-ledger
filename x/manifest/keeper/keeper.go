package keeper

import (
	"context"

	"cosmossdk.io/collections"
	storetypes "cosmossdk.io/core/store"
	"cosmossdk.io/log"
	sdkmath "cosmossdk.io/math"

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

func (k *Keeper) GetShareHolders(ctx context.Context) ([]*types.StakeHolders, error) {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}

	return params.StakeHolders, nil
}

// IsManualMintingEnabled returns nil if inflation mint is 0% (disabled)
func (k Keeper) IsManualMintingEnabled(ctx context.Context) bool {
	params, err := k.Params.Get(ctx)
	if err != nil {
		panic(err)
	}

	return !params.Inflation.AutomaticEnabled
}

type StakeHolderPayout struct {
	Address string
	Coin    sdk.Coin
}

// Returns the amount of coins to be distributed to the holders
func (k Keeper) CalculateShareHolderTokenPayout(ctx context.Context, c sdk.Coin) ([]StakeHolderPayout, error) {
	sh, err := k.GetShareHolders(ctx)
	if err != nil {
		return nil, err
	}

	pairs := make([]StakeHolderPayout, 0, len(sh))

	for _, s := range sh {
		s := s
		pct := sdkmath.NewInt(int64(s.Percentage)).ToLegacyDec().QuoInt64(types.MaxPercentShare)
		coinAmt := pct.MulInt(c.Amount).RoundInt()

		if coinAmt.IsZero() {
			// too small of an amount to matter (< 1 utoken)
			continue
		}

		pairs = append(pairs, StakeHolderPayout{
			Address: s.Address,
			Coin:    sdk.NewCoin(c.Denom, coinAmt),
		})

	}

	return pairs, nil
}

// PayoutStakeholders mints coins and sends them to the stakeholders.
// This is called from the endblocker, so panics should never happen.
// If it does, something is very wrong w/ the SDK. Any logic specific to auto minting
// should be kept out of this to properly handle and return nil instead.
func (k Keeper) PayoutStakeholders(ctx context.Context, c sdk.Coin) error {
	pairs, err := k.CalculateShareHolderTokenPayout(ctx, c)
	if err != nil {
		return err
	}

	if err := k.bankKeeper.MintCoins(ctx, types.ModuleName, sdk.NewCoins(c)); err != nil {
		return err
	}

	for _, p := range pairs {
		accAddr, err := sdk.AccAddressFromBech32(p.Address)
		if err != nil {
			return err
		}

		if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, accAddr, sdk.NewCoins(p.Coin)); err != nil {
			return err
		}
	}

	return nil
}

// BlockRewardsProvision Gets the amount of coins that are automatically minted every block
// per the automatic inflation
func (k Keeper) BlockRewardsProvision(ctx context.Context, denom string) (sdk.Coin, error) {
	mkParams, err := k.mintKeeper.Params.Get(ctx)
	if err != nil {
		return sdk.NewCoin(denom, sdkmath.ZeroInt()), err
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return sdk.NewCoin(denom, sdkmath.ZeroInt()), err
	}

	amtPerYear := params.Inflation.YearlyAmount
	blocksPerYear := mkParams.BlocksPerYear

	if blocksPerYear < 10 {
		k.logger.Error("x/mint blocks per year param is too low", "blocks", blocksPerYear)
		return sdk.NewCoin(denom, sdkmath.ZeroInt()), nil
	}

	div := amtPerYear / blocksPerYear

	// return the amount of coins to be minted per block
	return sdk.NewCoin(denom, sdkmath.NewIntFromUint64(div)), nil
}
