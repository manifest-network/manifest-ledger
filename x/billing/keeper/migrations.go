package keeper

import (
	"fmt"

	"cosmossdk.io/collections"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/types"
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

// Migrate1to2 marks the v1→v2 consensus-version bump for the custom_domain
// feature. It is a no-op: the new Params.ReservedDomainSuffixes field defaults
// to an empty slice (proto3 zero value) and the new CustomDomainIndex
// collection lives at a fresh store prefix, so no on-chain state needs
// rewriting.
//
// Operators are responsible for seeding Params.ReservedDomainSuffixes with
// the network's provider wildcard zones either:
//   - in the upgrade plan's genesis overlay at upgrade time, or
//   - via MsgUpdateParams from the module authority post-upgrade.
//
// Provider-zone defaults are intentionally NOT baked into the binary; once a
// hostname ships in a release tag it cannot be unshipped from chains that
// have run the upgrade. See ENG-82 for the planned automation
// (provider-declared wildcard zones in x/sku) that will replace manual
// reservation for the common case.
func (m Migrator) Migrate1to2(_ sdk.Context) error {
	return nil
}

// Migrate2to3 rewrites billing values so account identities are persisted as
// raw bytes instead of Bech32 strings. Collection keys and secondary indexes
// were already byte-addressed and are deliberately left untouched.
//
// Pages are read in key order and closed before writes begin, satisfying the KV
// iterator contract without relying on cache-store behavior. The migration is
// idempotent because the value codecs decode both v2 and v3 but always encode v3.
func (m Migrator) Migrate2to3(ctx sdk.Context) error {
	params, err := m.keeper.Params.Get(ctx)
	if err != nil {
		return fmt.Errorf("read billing params: %w", err)
	}
	if err := m.keeper.Params.Set(ctx, params); err != nil {
		return fmt.Errorf("rewrite billing params: %w", err)
	}

	if err := m.rewriteLeaseValues(ctx); err != nil {
		return err
	}
	if err := m.rewriteCreditAccountValues(ctx); err != nil {
		return err
	}
	return nil
}

type leaseMigrationEntry struct {
	key   string
	value types.Lease
}

func (m Migrator) rewriteLeaseValues(ctx sdk.Context) error {
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
		iterator, err := m.keeper.Leases.Iterate(ctx, keyRange)
		if err != nil {
			return fmt.Errorf("iterate billing leases: %w", err)
		}

		entries := make([]leaseMigrationEntry, 0, storageMigrationPageSize)
		for iterator.Valid() && len(entries) < storageMigrationPageSize {
			entry, entryErr := iterator.KeyValue()
			if entryErr != nil {
				_ = iterator.Close()
				return fmt.Errorf("decode billing lease migration entry: %w", entryErr)
			}
			entries = append(entries, leaseMigrationEntry{key: entry.Key, value: entry.Value})
			iterator.Next()
		}
		hasMore := iterator.Valid()
		if err := iterator.Close(); err != nil {
			return fmt.Errorf("close billing lease migration iterator: %w", err)
		}

		for _, entry := range entries {
			encoded, err := m.keeper.Leases.ValueCodec().Encode(entry.value)
			if err != nil {
				return fmt.Errorf("encode billing lease %q: %w", entry.key, err)
			}
			storeKey, err := collections.EncodeKeyWithPrefix(types.LeaseKey.Bytes(), collections.StringKey, entry.key)
			if err != nil {
				return fmt.Errorf("encode billing lease key %q: %w", entry.key, err)
			}
			if err := store.Set(storeKey, encoded); err != nil {
				return fmt.Errorf("rewrite billing lease %q: %w", entry.key, err)
			}
		}

		if !hasMore {
			return nil
		}
		lastKey = entries[len(entries)-1].key
		hasStart = true
	}
}

type creditAccountMigrationEntry struct {
	key   sdk.AccAddress
	value types.CreditAccount
}

func (m Migrator) rewriteCreditAccountValues(ctx sdk.Context) error {
	var (
		lastKey  sdk.AccAddress
		hasStart bool
	)
	store := m.keeper.storeService.OpenKVStore(ctx)

	for {
		var keyRange collections.Ranger[sdk.AccAddress]
		if hasStart {
			keyRange = new(collections.Range[sdk.AccAddress]).StartExclusive(lastKey)
		}
		iterator, err := m.keeper.CreditAccounts.Iterate(ctx, keyRange)
		if err != nil {
			return fmt.Errorf("iterate billing credit accounts: %w", err)
		}

		entries := make([]creditAccountMigrationEntry, 0, storageMigrationPageSize)
		for iterator.Valid() && len(entries) < storageMigrationPageSize {
			entry, entryErr := iterator.KeyValue()
			if entryErr != nil {
				_ = iterator.Close()
				return fmt.Errorf("decode billing credit-account migration entry: %w", entryErr)
			}
			tenant, entryErr := sdk.AccAddressFromBech32(entry.Value.Tenant)
			if entryErr != nil {
				_ = iterator.Close()
				return fmt.Errorf("decode billing credit-account tenant %q: %w", entry.Value.Tenant, entryErr)
			}
			if !tenant.Equals(entry.Key) {
				_ = iterator.Close()
				return fmt.Errorf("billing credit-account tenant %q does not match its store key", entry.Value.Tenant)
			}
			entries = append(entries, creditAccountMigrationEntry{
				key:   append(sdk.AccAddress(nil), entry.Key...),
				value: entry.Value,
			})
			iterator.Next()
		}
		hasMore := iterator.Valid()
		if err := iterator.Close(); err != nil {
			return fmt.Errorf("close billing credit-account migration iterator: %w", err)
		}

		for _, entry := range entries {
			encoded, err := m.keeper.CreditAccounts.ValueCodec().Encode(entry.value)
			if err != nil {
				return fmt.Errorf("encode billing credit account %q: %w", entry.key.String(), err)
			}
			storeKey, err := collections.EncodeKeyWithPrefix(types.CreditAccountKey.Bytes(), sdk.AccAddressKey, entry.key)
			if err != nil {
				return fmt.Errorf("encode billing credit-account key %q: %w", entry.key.String(), err)
			}
			if err := store.Set(storeKey, encoded); err != nil {
				return fmt.Errorf("rewrite billing credit account %q: %w", entry.key.String(), err)
			}
		}

		if !hasMore {
			return nil
		}
		lastKey = append(lastKey[:0], entries[len(entries)-1].key...)
		hasStart = true
	}
}
