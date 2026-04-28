package next

import (
	"context"

	storetypes "cosmossdk.io/store/types"
	upgradetypes "cosmossdk.io/x/upgrade/types"

	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/manifest-network/manifest-ledger/app/upgrades"
)

// NewUpgrade creates the upgrade handler for the next version.
// v3.0.0 migrates the chain from Cosmos SDK v0.50 to v0.53 and from ibc-go v8
// to v10. ibc-go v10 removes the capability module and the 29-fee middleware,
// so their KV stores are deleted.
//
// No custom in-place migrations are registered. Every module's v0.50→v0.53
// transition is handled by its registered upstream migrator (notably ibc-go
// core 6→8 and transfer 5→6). Any future custom-module schema change must
// register its own migrator before this handler is invoked, otherwise
// RunMigrations silently no-ops.
func NewUpgrade(name string) upgrades.Upgrade {
	return upgrades.Upgrade{
		UpgradeName:          name,
		CreateUpgradeHandler: CreateUpgradeHandler,
		StoreUpgrades: storetypes.StoreUpgrades{
			Deleted: []string{
				"capability", // capabilitytypes.StoreKey (removed in ibc-go v10)
				"feeibc",     // ibcfeetypes.StoreKey (29-fee removed in ibc-go v10)
			},
		},
	}
}

// CreateUpgradeHandler returns an upgrade handler that runs module migrations.
func CreateUpgradeHandler(
	mm *module.Manager,
	configurator module.Configurator,
	_ *upgrades.AppKeepers,
) upgradetypes.UpgradeHandler {
	return func(ctx context.Context, _ upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
		return mm.RunMigrations(ctx, configurator, fromVM)
	}
}
