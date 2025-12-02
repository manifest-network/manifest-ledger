package keeper

import (
	"context"

	"cosmossdk.io/collections"
	storetypes "cosmossdk.io/core/store"
	"cosmossdk.io/log"

	"github.com/cosmos/cosmos-sdk/codec"

	"github.com/manifest-network/manifest-ledger/x/sku/types"
)

// Keeper of the sku store.
type Keeper struct {
	cdc    codec.BinaryCodec
	logger log.Logger

	// state management
	Schema collections.Schema
	SKUs   collections.Map[uint64, types.SKU]
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

		SKUs: collections.NewMap(
			sb,
			types.SKUKey,
			"skus",
			collections.Uint64Key,
			codec.CollValue[types.SKU](cdc),
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

// InitGenesis initializes the module's state from a provided genesis state.
func (k *Keeper) InitGenesis(ctx context.Context, gs *types.GenesisState) error {
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
	skus := make([]types.SKU, 0)

	iter, err := k.SKUs.Iterate(ctx, nil)
	if err != nil {
		panic(err)
	}
	defer iter.Close()

	for ; iter.Valid(); iter.Next() {
		sku, err := iter.Value()
		if err != nil {
			panic(err)
		}
		skus = append(skus, sku)
	}

	nextID, err := k.NextID.Peek(ctx)
	if err != nil {
		panic(err)
	}

	return &types.GenesisState{
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
	skus := make([]types.SKU, 0)

	iter, err := k.SKUs.Iterate(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	for ; iter.Valid(); iter.Next() {
		sku, err := iter.Value()
		if err != nil {
			return nil, err
		}
		skus = append(skus, sku)
	}

	return skus, nil
}

// GetSKUsByProvider returns all SKUs for a given provider.
func (k *Keeper) GetSKUsByProvider(ctx context.Context, provider string) ([]types.SKU, error) {
	skus := make([]types.SKU, 0)

	iter, err := k.SKUs.Iterate(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	for ; iter.Valid(); iter.Next() {
		sku, err := iter.Value()
		if err != nil {
			return nil, err
		}
		if sku.Provider == provider {
			skus = append(skus, sku)
		}
	}

	return skus, nil
}
