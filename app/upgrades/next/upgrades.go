package next

import (
	"context"

	storetypes "cosmossdk.io/store/types"
	upgradetypes "cosmossdk.io/x/upgrade/types"

	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/manifest-network/manifest-ledger/app/upgrades"
)

// NewUpgrade creates a noop upgrade handler for the next version.
// The x/sku and x/billing modules were already added in v2.0.0.
func NewUpgrade(name string) upgrades.Upgrade {
	return upgrades.Upgrade{
		UpgradeName:          name,
		CreateUpgradeHandler: CreateUpgradeHandler,
		StoreUpgrades:        storetypes.StoreUpgrades{},
	}
}

// CreateUpgradeHandler returns a noop upgrade handler that only runs module migrations.
func CreateUpgradeHandler(
	mm *module.Manager,
	configurator module.Configurator,
	_ *upgrades.AppKeepers,
) upgradetypes.UpgradeHandler {
	return func(ctx context.Context, _ upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
		return mm.RunMigrations(ctx, configurator, fromVM)
	}
}
