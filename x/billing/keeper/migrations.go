package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
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
