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

// DefaultReservedDomainSuffixesV2 are the provider wildcard zones that the
// Migrate1to2 migration seeds into Params.ReservedDomainSuffixes when the
// field is empty. New zones added later are managed via MsgUpdateParams.
var DefaultReservedDomainSuffixesV2 = []string{
	".barney0.manifest0.net",
	".barney8.manifest0.net",
}

// Migrate1to2 seeds Params.ReservedDomainSuffixes for the custom_domain
// feature. If operator-defined values already exist, the migration leaves
// them in place (idempotent / defensive).
func (m Migrator) Migrate1to2(ctx sdk.Context) error {
	params, err := m.keeper.GetParams(ctx)
	if err != nil {
		return err
	}
	if len(params.ReservedDomainSuffixes) == 0 {
		params.ReservedDomainSuffixes = append([]string{}, DefaultReservedDomainSuffixesV2...)
	}
	return m.keeper.SetParams(ctx, params)
}
