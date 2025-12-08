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
type SKUIndexes struct {
	// Provider is a multi-index that indexes SKUs by provider_id.
	Provider *indexes.Multi[uint64, uint64, types.SKU]
}

// IndexesList returns all indexes defined for the SKU collection.
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
			collections.Uint64Key,
			collections.Uint64Key,
			func(_ uint64, sku types.SKU) (uint64, error) {
				return sku.ProviderId, nil
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
	Schema         collections.Schema
	Params         collections.Item[types.Params]
	Providers      *collections.IndexedMap[uint64, types.Provider, noIndexes[uint64, types.Provider]]
	NextProviderID collections.Sequence
	SKUs           *collections.IndexedMap[uint64, types.SKU, SKUIndexes]
	NextSKUID      collections.Sequence

	authority string
}

// noIndexes is a placeholder for collections with no indexes.
type noIndexes[K, V any] struct{}

func (n noIndexes[K, V]) IndexesList() []collections.Index[K, V] {
	return nil
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
		Providers: collections.NewIndexedMap(
			sb,
			types.ProviderKey,
			"providers",
			collections.Uint64Key,
			codec.CollValue[types.Provider](cdc),
			noIndexes[uint64, types.Provider]{},
		),
		NextProviderID: collections.NewSequence(
			sb,
			types.ProviderSequenceKey,
			"next_provider_id",
		),
		SKUs: collections.NewIndexedMap(
			sb,
			types.SKUKey,
			"skus",
			collections.Uint64Key,
			codec.CollValue[types.SKU](cdc),
			NewSKUIndexes(sb),
		),
		NextSKUID: collections.NewSequence(
			sb,
			types.SKUSequenceKey,
			"next_sku_id",
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

	for _, provider := range gs.Providers {
		if err := k.Providers.Set(ctx, provider.Id, provider); err != nil {
			return err
		}
	}

	if err := k.NextProviderID.Set(ctx, gs.NextProviderId); err != nil {
		return err
	}

	for _, sku := range gs.Skus {
		if err := k.SKUs.Set(ctx, sku.Id, sku); err != nil {
			return err
		}
	}

	if err := k.NextSKUID.Set(ctx, gs.NextSkuId); err != nil {
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

	var providers []types.Provider
	err = k.Providers.Walk(ctx, nil, func(_ uint64, provider types.Provider) (bool, error) {
		providers = append(providers, provider)
		return false, nil
	})
	if err != nil {
		panic(err)
	}

	nextProviderID, err := k.NextProviderID.Peek(ctx)
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

	nextSKUID, err := k.NextSKUID.Peek(ctx)
	if err != nil {
		panic(err)
	}

	return &types.GenesisState{
		Params:         params,
		Providers:      providers,
		NextProviderId: nextProviderID,
		Skus:           skus,
		NextSkuId:      nextSKUID,
	}
}

// Provider operations

// GetProvider returns a Provider by its ID.
func (k *Keeper) GetProvider(ctx context.Context, id uint64) (types.Provider, error) {
	provider, err := k.Providers.Get(ctx, id)
	if err != nil {
		return types.Provider{}, types.ErrProviderNotFound
	}
	return provider, nil
}

// SetProvider sets a Provider in the store.
func (k *Keeper) SetProvider(ctx context.Context, provider types.Provider) error {
	return k.Providers.Set(ctx, provider.Id, provider)
}

// GetNextProviderID returns the next Provider ID and increments the sequence.
func (k *Keeper) GetNextProviderID(ctx context.Context) (uint64, error) {
	return k.NextProviderID.Next(ctx)
}

// GetAllProviders returns all Providers in the store.
func (k *Keeper) GetAllProviders(ctx context.Context) ([]types.Provider, error) {
	var providers []types.Provider

	err := k.Providers.Walk(ctx, nil, func(_ uint64, provider types.Provider) (bool, error) {
		providers = append(providers, provider)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	return providers, nil
}

// SKU operations

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

// GetNextSKUID returns the next SKU ID and increments the sequence.
func (k *Keeper) GetNextSKUID(ctx context.Context) (uint64, error) {
	return k.NextSKUID.Next(ctx)
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

// GetSKUsByProviderID returns all SKUs for a given provider ID using the provider index.
func (k *Keeper) GetSKUsByProviderID(ctx context.Context, providerID uint64) ([]types.SKU, error) {
	var skus []types.SKU

	iter, err := k.SKUs.Indexes.Provider.MatchExact(ctx, providerID)
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
