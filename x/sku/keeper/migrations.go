package keeper

import (
	"fmt"

	"cosmossdk.io/collections"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/sku/types"
)

const storageMigrationPageSize = 1000

// Migrator is a wrapper around Keeper used to register state migrations.
type Migrator struct {
	keeper Keeper
}

// NewMigrator returns a Migrator for the given keeper.
func NewMigrator(k Keeper) Migrator {
	return Migrator{keeper: k}
}

// Migrate1to2 rewrites SKU values so account identities are persisted as raw
// bytes instead of Bech32 strings. Provider secondary indexes were already
// keyed by decoded address bytes and are deliberately left untouched.
// Equivalent allowed-list spellings are collapsed in first-seen slice order.
//
// Pages are read in primary-key order and closed before writes begin, satisfying
// the KV iterator contract. The migration is idempotent because the value
// codecs decode both v1 and v2 but always encode v2.
func (m Migrator) Migrate1to2(ctx sdk.Context) error {
	params, err := m.keeper.Params.Get(ctx)
	if err != nil {
		return fmt.Errorf("read SKU params: %w", err)
	}
	params, err = params.CanonicalizeAllowedList()
	if err != nil {
		return fmt.Errorf("canonicalize SKU params allowed list: %w", err)
	}
	if err := m.keeper.Params.Set(ctx, params); err != nil {
		return fmt.Errorf("rewrite SKU params: %w", err)
	}

	return m.rewriteProviderValues(ctx)
}

type providerMigrationEntry struct {
	key   string
	value types.Provider
}

func (m Migrator) rewriteProviderValues(ctx sdk.Context) error {
	var (
		lastKey  string
		hasStart bool
	)
	store := m.keeper.storeService.OpenKVStore(ctx)

	for {
		var keyRange collections.Ranger[string]
		if hasStart {
			keyRange = new(collections.Range[string]).StartExclusive(lastKey)
		}
		iterator, err := m.keeper.Providers.Iterate(ctx, keyRange)
		if err != nil {
			return fmt.Errorf("iterate SKU providers: %w", err)
		}

		entries := make([]providerMigrationEntry, 0, storageMigrationPageSize)
		for iterator.Valid() && len(entries) < storageMigrationPageSize {
			entry, entryErr := iterator.KeyValue()
			if entryErr != nil {
				_ = iterator.Close()
				return fmt.Errorf("decode SKU provider migration entry: %w", entryErr)
			}
			entries = append(entries, providerMigrationEntry{key: entry.Key, value: entry.Value})
			iterator.Next()
		}
		hasMore := iterator.Valid()
		if err := iterator.Close(); err != nil {
			return fmt.Errorf("close SKU provider migration iterator: %w", err)
		}

		for _, entry := range entries {
			encoded, err := m.keeper.Providers.ValueCodec().Encode(entry.value)
			if err != nil {
				return fmt.Errorf("encode SKU provider %q: %w", entry.key, err)
			}
			storeKey, err := collections.EncodeKeyWithPrefix(
				types.ProviderKey.Bytes(),
				collections.StringKey,
				entry.key,
			)
			if err != nil {
				return fmt.Errorf("encode SKU provider key %q: %w", entry.key, err)
			}
			if err := store.Set(storeKey, encoded); err != nil {
				return fmt.Errorf("rewrite SKU provider %q: %w", entry.key, err)
			}
		}

		if !hasMore {
			return nil
		}
		lastKey = entries[len(entries)-1].key
		hasStart = true
	}
}
