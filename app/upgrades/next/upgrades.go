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
// so their KV stores are deleted. Module ConsensusVersion bumps are handled
// automatically by RunMigrations.
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
