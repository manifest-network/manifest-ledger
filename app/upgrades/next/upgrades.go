// Package next defines the placeholder handler for the next application upgrade.
package next

import (
	"context"

	storetypes "cosmossdk.io/store/types"
	upgradetypes "cosmossdk.io/x/upgrade/types"

	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/manifest-network/manifest-ledger/app/upgrades"
)

// NewUpgrade creates a module-migration-only upgrade handler for the next
// version. The x/sku and x/billing modules were already added in v2.0.0, so no
// store keys need to be mounted or removed.
func NewUpgrade(name string) upgrades.Upgrade {
	return upgrades.Upgrade{
		UpgradeName:          name,
		CreateUpgradeHandler: CreateUpgradeHandler,
		StoreUpgrades:        storetypes.StoreUpgrades{},
	}
}

// CreateUpgradeHandler returns an upgrade handler that runs every registered
// module migration from the on-chain version map to the binary's versions.
func CreateUpgradeHandler(
	mm *module.Manager,
	configurator module.Configurator,
	_ *upgrades.AppKeepers,
) upgradetypes.UpgradeHandler {
	return func(ctx context.Context, _ upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
		return mm.RunMigrations(ctx, configurator, fromVM)
	}
}
