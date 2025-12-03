package keeper

import (
	"context"

	"cosmossdk.io/collections"
	"cosmossdk.io/collections/indexes"
	storetypes "cosmossdk.io/core/store"
	"cosmossdk.io/log"

	"github.com/cosmos/cosmos-sdk/codec"
	accountkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"

	"github.com/manifest-network/manifest-ledger/x/sku/types"
)

// SKUIndexes defines the indexes for the SKU collection.
// It enables efficient lookups and queries on SKUs by specific fields.
// Currently, it provides a multi-index on the Provider field, allowing fast retrieval
// of all SKUs associated with a given provider without scanning the entire collection.
type SKUIndexes struct {
	// Provider is a multi-index that indexes SKUs by provider.
	// This index maps a provider string to one or more SKU IDs.
	// It is implemented using indexes.NewMulti, which extracts the Provider field
	// from each SKU for indexing.
	Provider *indexes.Multi[string, uint64, types.SKU]
}

// IndexesList returns all indexes defined for the SKU collection.
// This is used by the collections framework to register and manage indexes.
func (i SKUIndexes) IndexesList() []collections.Index[uint64, types.SKU] {
	return []collections.Index[uint64, types.SKU]{i.Provider}
}

// NewSKUIndexes creates a new SKUIndexes instance.
func NewSKUIndexes(sb *collections.SchemaBuilder) SKUIndexes {
	return SKUIndexes{
		Provider: indexes.NewMulti(
			sb,
			types.SKUByProviderIndexKey,
			"skus_by_provider",
			collections.StringKey,
			collections.Uint64Key,
			func(_ uint64, sku types.SKU) (string, error) {
				return sku.Provider, nil
			},
		),
	}
}

// Keeper of the sku store.
type Keeper struct {
	cdc    codec.BinaryCodec
	logger log.Logger

	// keepers for simulation
	accountKeeper accountkeeper.AccountKeeper
	bankKeeper    bankkeeper.Keeper

	// state management
	Schema collections.Schema
	Params collections.Item[types.Params]
	SKUs   *collections.IndexedMap[uint64, types.SKU, SKUIndexes]
	NextID collections.Sequence

	authority string
}

// NewKeeper creates a new sku Keeper instance.
func NewKeeper(
	cdc codec.BinaryCodec,
	storeService storetypes.KVStoreService,
	logger log.Logger,
	authority string,
) Keeper {
	logger = logger.With(log.ModuleKey, "x/"+types.ModuleName)

	sb := collections.NewSchemaBuilder(storeService)

	k := Keeper{
		cdc:       cdc,
		logger:    logger,
		authority: authority,

		Params: collections.NewItem(
			sb,
			types.ParamsKey,
			"params",
			codec.CollValue[types.Params](cdc),
		),
		SKUs: collections.NewIndexedMap(
			sb,
			types.SKUKey,
			"skus",
			collections.Uint64Key,
			codec.CollValue[types.SKU](cdc),
			NewSKUIndexes(sb),
		),
		NextID: collections.NewSequence(
			sb,
			types.SequenceKey,
			"next_id",
		),
	}

	schema, err := sb.Build()
	if err != nil {
		panic(err)
	}

	k.Schema = schema

	return k
}

// Logger returns the module logger.
func (k *Keeper) Logger() log.Logger {
	return k.logger
}

// GetAuthority returns the module's authority.
func (k *Keeper) GetAuthority() string {
	return k.authority
}

// SetAuthority sets the module's authority (used for testing).
func (k *Keeper) SetAuthority(authority string) {
	k.authority = authority
}

// SetAccountKeeper sets the account keeper (used for simulation/testing).
func (k *Keeper) SetAccountKeeper(ak accountkeeper.AccountKeeper) {
	k.accountKeeper = ak
}

// GetAccountKeeper returns the account keeper.
func (k *Keeper) GetAccountKeeper() accountkeeper.AccountKeeper {
	return k.accountKeeper
}

// SetBankKeeper sets the bank keeper (used for simulation/testing).
func (k *Keeper) SetBankKeeper(bk bankkeeper.Keeper) {
	k.bankKeeper = bk
}

// GetBankKeeper returns the bank keeper.
func (k *Keeper) GetBankKeeper() bankkeeper.Keeper {
	return k.bankKeeper
}

// GetParams returns the module parameters.
func (k *Keeper) GetParams(ctx context.Context) (types.Params, error) {
	return k.Params.Get(ctx)
}

// SetParams sets the module parameters.
func (k *Keeper) SetParams(ctx context.Context, params types.Params) error {
	return k.Params.Set(ctx, params)
}

// InitGenesis initializes the module's state from a provided genesis state.
func (k *Keeper) InitGenesis(ctx context.Context, gs *types.GenesisState) error {
	if err := k.Params.Set(ctx, gs.Params); err != nil {
		return err
	}

	for _, sku := range gs.Skus {
		if err := k.SKUs.Set(ctx, sku.Id, sku); err != nil {
			return err
		}
	}

	if err := k.NextID.Set(ctx, gs.NextId); err != nil {
		return err
	}

	return nil
}

// ExportGenesis exports the module's state to a genesis state.
func (k *Keeper) ExportGenesis(ctx context.Context) *types.GenesisState {
	params, err := k.Params.Get(ctx)
	if err != nil {
		panic(err)
	}

	var skus []types.SKU
	err = k.SKUs.Walk(ctx, nil, func(_ uint64, sku types.SKU) (bool, error) {
		skus = append(skus, sku)
		return false, nil
	})
	if err != nil {
		panic(err)
	}

	nextID, err := k.NextID.Peek(ctx)
	if err != nil {
		panic(err)
	}

	return &types.GenesisState{
		Params: params,
		Skus:   skus,
		NextId: nextID,
	}
}

// GetSKU returns a SKU by its ID.
func (k *Keeper) GetSKU(ctx context.Context, id uint64) (types.SKU, error) {
	sku, err := k.SKUs.Get(ctx, id)
	if err != nil {
		return types.SKU{}, types.ErrSKUNotFound
	}
	return sku, nil
}

// SetSKU sets a SKU in the store.
func (k *Keeper) SetSKU(ctx context.Context, sku types.SKU) error {
	return k.SKUs.Set(ctx, sku.Id, sku)
}

// DeleteSKU removes a SKU from the store.
func (k *Keeper) DeleteSKU(ctx context.Context, id uint64) error {
	return k.SKUs.Remove(ctx, id)
}

// GetNextID returns the next SKU ID and increments the sequence.
func (k *Keeper) GetNextID(ctx context.Context) (uint64, error) {
	return k.NextID.Next(ctx)
}

// GetAllSKUs returns all SKUs in the store.
func (k *Keeper) GetAllSKUs(ctx context.Context) ([]types.SKU, error) {
	var skus []types.SKU

	err := k.SKUs.Walk(ctx, nil, func(_ uint64, sku types.SKU) (bool, error) {
		skus = append(skus, sku)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	return skus, nil
}

// GetSKUsByProvider returns all SKUs for a given provider using the provider index.
func (k *Keeper) GetSKUsByProvider(ctx context.Context, provider string) ([]types.SKU, error) {
	var skus []types.SKU

	iter, err := k.SKUs.Indexes.Provider.MatchExact(ctx, provider)
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	for ; iter.Valid(); iter.Next() {
		skuID, err := iter.PrimaryKey()
		if err != nil {
			return nil, err
		}
		sku, err := k.SKUs.Get(ctx, skuID)
		if err != nil {
			return nil, err
		}
		skus = append(skus, sku)
	}

	return skus, nil
}
