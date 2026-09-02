// Package keeper implements SKU module state transitions and queries.
package keeper

import (
	"context"
	"errors"

	"cosmossdk.io/collections"
	"cosmossdk.io/collections/indexes"
	storetypes "cosmossdk.io/core/store"
	"cosmossdk.io/log"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	pkguuid "github.com/manifest-network/manifest-ledger/pkg/uuid"
	"github.com/manifest-network/manifest-ledger/x/sku/types"
)

// ProviderIndexes defines the indexes for the Provider collection.
type ProviderIndexes struct {
	// Address is a multi-index that indexes Providers by their management address.
	// This enables efficient lookup of providers by address (a single address can manage multiple providers).
	Address *indexes.Multi[sdk.AccAddress, string, types.Provider]

	// Active is a multi-index that indexes Providers by their active status.
	// This enables efficient queries for active providers only (O(k) instead of O(n)).
	Active *indexes.Multi[bool, string, types.Provider]
}

// IndexesList returns all indexes defined for the Provider collection.
func (i ProviderIndexes) IndexesList() []collections.Index[string, types.Provider] {
	return []collections.Index[string, types.Provider]{i.Address, i.Active}
}

// NewProviderIndexes creates a new ProviderIndexes instance.
func NewProviderIndexes(sb *collections.SchemaBuilder) ProviderIndexes {
	return ProviderIndexes{
		Address: indexes.NewMulti(
			sb,
			types.ProviderByAddressIndexKey,
			"providers_by_address",
			sdk.AccAddressKey,
			collections.StringKey,
			func(_ string, provider types.Provider) (sdk.AccAddress, error) {
				return sdk.AccAddressFromBech32(provider.Address)
			},
		),
		Active: indexes.NewMulti(
			sb,
			types.ProviderByActiveIndexKey,
			"providers_by_active",
			collections.BoolKey,
			collections.StringKey,
			func(_ string, provider types.Provider) (bool, error) {
				return provider.Active, nil
			},
		),
	}
}

// SKUIndexes defines the indexes for the SKU collection.
type SKUIndexes struct {
	// Provider is a multi-index that indexes SKUs by provider_uuid.
	Provider *indexes.Multi[string, string, types.SKU]

	// Active is a multi-index that indexes SKUs by their active status.
	// This enables efficient queries for active SKUs only (O(k) instead of O(n)).
	Active *indexes.Multi[bool, string, types.SKU]

	// ProviderActive is a compound multi-index that indexes SKUs by (provider_uuid, active).
	// This enables efficient queries for active SKUs filtered by provider.
	ProviderActive *indexes.Multi[collections.Pair[string, bool], string, types.SKU]
}

// IndexesList returns all indexes defined for the SKU collection.
func (i SKUIndexes) IndexesList() []collections.Index[string, types.SKU] {
	return []collections.Index[string, types.SKU]{i.Provider, i.Active, i.ProviderActive}
}

// NewSKUIndexes creates a new SKUIndexes instance.
func NewSKUIndexes(sb *collections.SchemaBuilder) SKUIndexes {
	return SKUIndexes{
		Provider: indexes.NewMulti(
			sb,
			types.SKUByProviderIndexKey,
			"skus_by_provider",
			collections.StringKey,
			collections.StringKey,
			func(_ string, sku types.SKU) (string, error) {
				return sku.ProviderUuid, nil
			},
		),
		Active: indexes.NewMulti(
			sb,
			types.SKUByActiveIndexKey,
			"skus_by_active",
			collections.BoolKey,
			collections.StringKey,
			func(_ string, sku types.SKU) (bool, error) {
				return sku.Active, nil
			},
		),
		ProviderActive: indexes.NewMulti(
			sb,
			types.SKUByProviderActiveIndexKey,
			"skus_by_provider_active",
			collections.PairKeyCodec(collections.StringKey, collections.BoolKey),
			collections.StringKey,
			func(_ string, sku types.SKU) (collections.Pair[string, bool], error) {
				return collections.Join(sku.ProviderUuid, sku.Active), nil
			},
		),
	}
}

// Keeper of the sku store.
type Keeper struct {
	cdc          codec.BinaryCodec
	storeService storetypes.KVStoreService
	logger       log.Logger

	// Simulation dependencies are constructor-injected so the module cannot be
	// wired with a partially initialized keeper.
	accountKeeper types.AccountKeeper
	bankKeeper    types.BankKeeper

	// state management
	Schema    collections.Schema
	Params    collections.Item[types.Params]
	Providers *collections.IndexedMap[string, types.Provider, ProviderIndexes]
	SKUs      *collections.IndexedMap[string, types.SKU, SKUIndexes]

	// Sequences for deterministic UUID generation
	ProviderSequence collections.Sequence
	SKUSequence      collections.Sequence

	authority string
}

// NewKeeper creates a new sku Keeper instance.
func NewKeeper(
	cdc codec.BinaryCodec,
	storeService storetypes.KVStoreService,
	logger log.Logger,
	authority string,
	accountKeeper types.AccountKeeper,
	bankKeeper types.BankKeeper,
) Keeper {
	logger = logger.With(log.ModuleKey, "x/"+types.ModuleName)

	sb := collections.NewSchemaBuilder(storeService)

	k := Keeper{
		cdc:           cdc,
		storeService:  storeService,
		logger:        logger,
		authority:     authority,
		accountKeeper: accountKeeper,
		bankKeeper:    bankKeeper,

		Params: collections.NewItem(
			sb,
			types.ParamsKey,
			"params",
			newParamsValueCodec(cdc),
		),
		Providers: collections.NewIndexedMap(
			sb,
			types.ProviderKey,
			"providers",
			collections.StringKey,
			newProviderValueCodec(cdc),
			NewProviderIndexes(sb),
		),
		SKUs: collections.NewIndexedMap(
			sb,
			types.SKUKey,
			"skus",
			collections.StringKey,
			codec.CollValue[types.SKU](cdc),
			NewSKUIndexes(sb),
		),
		// Keep sequences for deterministic UUID generation
		ProviderSequence: collections.NewSequence(
			sb,
			types.ProviderSequenceKey,
			"provider_sequence",
		),
		SKUSequence: collections.NewSequence(
			sb,
			types.SKUSequenceKey,
			"sku_sequence",
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

// GetAccountKeeper returns the account keeper.
func (k *Keeper) GetAccountKeeper() types.AccountKeeper {
	return k.accountKeeper
}

// GetBankKeeper returns the bank keeper.
func (k *Keeper) GetBankKeeper() types.BankKeeper {
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
	prepared, err := gs.PrepareForImport()
	if err != nil {
		return err
	}

	if err := k.Params.Set(ctx, prepared.Params); err != nil {
		return err
	}

	for _, provider := range prepared.Providers {
		if err := k.Providers.Set(ctx, provider.Uuid, provider); err != nil {
			return err
		}
	}

	for _, sku := range prepared.Skus {
		if err := k.SKUs.Set(ctx, sku.Uuid, sku); err != nil {
			return err
		}
	}

	// Restore UUID generation sequences so new entities don't collide
	// with previously generated UUIDs after a genesis export/import cycle.
	if prepared.ProviderSequence > 0 {
		if err := k.ProviderSequence.Set(ctx, prepared.ProviderSequence); err != nil {
			return err
		}
	}
	if prepared.SkuSequence > 0 {
		if err := k.SKUSequence.Set(ctx, prepared.SkuSequence); err != nil {
			return err
		}
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
	err = k.Providers.Walk(ctx, nil, func(_ string, provider types.Provider) (bool, error) {
		providers = append(providers, provider)
		return false, nil
	})
	if err != nil {
		panic(err)
	}

	var skus []types.SKU
	err = k.SKUs.Walk(ctx, nil, func(_ string, sku types.SKU) (bool, error) {
		skus = append(skus, sku)
		return false, nil
	})
	if err != nil {
		panic(err)
	}

	providerSeq, err := k.ProviderSequence.Peek(ctx)
	if err != nil {
		panic(err)
	}
	skuSeq, err := k.SKUSequence.Peek(ctx)
	if err != nil {
		panic(err)
	}

	return &types.GenesisState{
		Params:           params,
		Providers:        providers,
		Skus:             skus,
		ProviderSequence: providerSeq,
		SkuSequence:      skuSeq,
	}
}

// Provider operations

// GetProvider returns a Provider by its UUID.
func (k *Keeper) GetProvider(ctx context.Context, uuid string) (types.Provider, error) {
	provider, err := k.Providers.Get(ctx, uuid)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return types.Provider{}, types.ErrProviderNotFound
		}
		return types.Provider{}, err
	}
	return provider, nil
}

// SetProvider sets a Provider in the store.
func (k *Keeper) SetProvider(ctx context.Context, provider types.Provider) error {
	return k.Providers.Set(ctx, provider.Uuid, provider)
}

// GenerateProviderUUID generates a new deterministic UUIDv7 for a provider.
func (k *Keeper) GenerateProviderUUID(ctx context.Context) (string, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	seq, err := k.ProviderSequence.Next(ctx)
	if err != nil {
		return "", err
	}
	return pkguuid.GenerateUUIDv7(sdkCtx, types.ModuleName+"-provider", seq), nil
}

// GetAllProviders returns all Providers in the store.
func (k *Keeper) GetAllProviders(ctx context.Context) ([]types.Provider, error) {
	var providers []types.Provider

	err := k.Providers.Walk(ctx, nil, func(_ string, provider types.Provider) (bool, error) {
		providers = append(providers, provider)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	return providers, nil
}

// SKU operations

// GetSKU returns a SKU by its UUID.
func (k *Keeper) GetSKU(ctx context.Context, uuid string) (types.SKU, error) {
	sku, err := k.SKUs.Get(ctx, uuid)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return types.SKU{}, types.ErrSKUNotFound
		}
		return types.SKU{}, err
	}
	return sku, nil
}

// SetSKU sets a SKU in the store.
func (k *Keeper) SetSKU(ctx context.Context, sku types.SKU) error {
	return k.SKUs.Set(ctx, sku.Uuid, sku)
}

// GenerateSKUUUID generates a new deterministic UUIDv7 for a SKU.
func (k *Keeper) GenerateSKUUUID(ctx context.Context) (string, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	seq, err := k.SKUSequence.Next(ctx)
	if err != nil {
		return "", err
	}
	return pkguuid.GenerateUUIDv7(sdkCtx, types.ModuleName+"-sku", seq), nil
}

// GetAllSKUs returns all SKUs in the store.
func (k *Keeper) GetAllSKUs(ctx context.Context) ([]types.SKU, error) {
	var skus []types.SKU

	err := k.SKUs.Walk(ctx, nil, func(_ string, sku types.SKU) (bool, error) {
		skus = append(skus, sku)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	return skus, nil
}

// GetSKUsByProviderUUID returns all SKUs for a given provider UUID using the provider index.
// WARNING: This loads all SKUs into memory. For large datasets or batch operations,
// use IterateActiveSKUsByProvider instead to process SKUs with a limit.
func (k *Keeper) GetSKUsByProviderUUID(ctx context.Context, providerUUID string) (skus []types.SKU, err error) {
	iter, err := k.SKUs.Indexes.Provider.MatchExact(ctx, providerUUID)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := iter.Close(); err == nil {
			err = closeErr
		}
	}()

	for ; iter.Valid(); iter.Next() {
		skuUUID, err := iter.PrimaryKey()
		if err != nil {
			return nil, err
		}
		sku, err := k.SKUs.Get(ctx, skuUUID)
		if err != nil {
			return nil, err
		}
		skus = append(skus, sku)
	}

	return skus, nil
}

// IterateActiveSKUsByProvider iterates over active SKUs for a provider with a limit.
// The callback is called for each active SKU. If the callback returns true, iteration stops.
// Returns the number of SKUs processed and whether there are more active SKUs remaining.
func (k *Keeper) IterateActiveSKUsByProvider(
	ctx context.Context,
	providerUUID string,
	limit uint64,
	cb func(sku types.SKU) (stop bool, err error),
) (processed uint64, hasMore bool, err error) {
	// Use the ProviderActive compound index to efficiently query active SKUs for this provider
	iter, err := k.SKUs.Indexes.ProviderActive.MatchExact(ctx, collections.Join(providerUUID, true))
	if err != nil {
		return 0, false, err
	}
	defer func() {
		if closeErr := iter.Close(); err == nil {
			err = closeErr
		}
	}()

	for ; iter.Valid(); iter.Next() {
		// Check if we've reached the limit
		if processed >= limit {
			hasMore = true
			return processed, hasMore, nil
		}

		skuUUID, err := iter.PrimaryKey()
		if err != nil {
			return processed, false, err
		}

		sku, err := k.SKUs.Get(ctx, skuUUID)
		if err != nil {
			return processed, false, err
		}

		stop, err := cb(sku)
		if err != nil {
			return processed, false, err
		}

		processed++

		if stop {
			// Check if there are more items after this one
			iter.Next()
			hasMore = iter.Valid()
			return processed, hasMore, nil
		}
	}

	return processed, false, nil
}

// HasActiveSKUsByProvider returns true if the provider has any active SKUs.
func (k *Keeper) HasActiveSKUsByProvider(ctx context.Context, providerUUID string) (bool, error) {
	iter, err := k.SKUs.Indexes.ProviderActive.MatchExact(ctx, collections.Join(providerUUID, true))
	if err != nil {
		return false, err
	}

	hasActive := iter.Valid()
	if err := iter.Close(); err != nil {
		return false, err
	}
	return hasActive, nil
}
