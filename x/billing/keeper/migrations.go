package keeper

import (
	"context"

	"cosmossdk.io/collections"
	"cosmossdk.io/log"

	sdk "github.com/cosmos/cosmos-sdk/types"

	v3 "github.com/manifest-network/manifest-ledger/x/billing/migrations/v3"
	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

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

// Migrate2to3 replaces per-lease settlement with settlement streams: it pays
// what every lease accrued up to the upgrade, creates one stream per
// (provider_uuid, tenant, denom) with ACTIVE leases, and clears the deprecated
// Lease.last_settled_at. The streams live at fresh prefixes of the existing
// billing store, so no StoreUpgrades entry is needed. See migrations/v3.
func (m Migrator) Migrate2to3(ctx sdk.Context) error {
	return v3.MigrateStore(ctx, v3Migration{k: &m.keeper})
}

// v3Migration adapts the keeper to the interface migrations/v3 works through.
type v3Migration struct {
	k *Keeper
}

var _ v3.Keeper = v3Migration{}

func (a v3Migration) Logger() log.Logger { return a.k.Logger() }

func (a v3Migration) GetAllLeases(ctx context.Context) ([]types.Lease, error) {
	return a.k.GetAllLeases(ctx)
}

func (a v3Migration) CheckStreamInvariant(ctx context.Context) error {
	return a.k.CheckStreamInvariant(ctx)
}

// RewriteLease overwrites a lease's stored value through the raw KV store.
// Going through the Leases IndexedMap would delete and rewrite all six index
// entries of every lease although none of the indexed fields change.
func (a v3Migration) RewriteLease(ctx context.Context, lease types.Lease) error {
	key, err := collections.EncodeKeyWithPrefix(types.LeaseKey.Bytes(), collections.StringKey, lease.Uuid)
	if err != nil {
		return err
	}
	bz, err := a.k.cdc.Marshal(&lease)
	if err != nil {
		return err
	}
	return a.k.storeService.OpenKVStore(ctx).Set(key, bz)
}

func (a v3Migration) SetMigratedStream(ctx context.Context, stream types.Stream, leaseUUIDs []string) error {
	tenant, err := sdk.AccAddressFromBech32(stream.Tenant)
	if err != nil {
		return err
	}
	key := NewStreamKey(stream.ProviderUuid, tenant, stream.Denom)

	if err := a.k.Streams.Set(ctx, key, stream.State); err != nil {
		return err
	}
	if err := a.k.StreamsByTenant.Set(ctx, streamTenantKey(key)); err != nil {
		return err
	}
	for _, leaseUUID := range leaseUUIDs {
		if err := a.k.StreamLeases.Set(ctx, collections.Join(key, leaseUUID)); err != nil {
			return err
		}
	}
	return nil
}
