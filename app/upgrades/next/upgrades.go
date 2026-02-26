package next

import (
	"context"

	storetypes "cosmossdk.io/store/types"
	upgradetypes "cosmossdk.io/x/upgrade/types"

	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/manifest-network/manifest-ledger/app/upgrades"
	billingtypes "github.com/manifest-network/manifest-ledger/x/billing/types"
	skutypes "github.com/manifest-network/manifest-ledger/x/sku/types"
)

// NewUpgrade creates a new upgrade handler for adding x/sku and x/billing modules.
// This upgrade introduces:
//   - x/sku: Provider and SKU management for billable resources
//   - x/billing: Credit-based billing system with lease lifecycle management
func NewUpgrade(name string) upgrades.Upgrade {
	return upgrades.Upgrade{
		UpgradeName:          name,
		CreateUpgradeHandler: CreateUpgradeHandler,
		StoreUpgrades: storetypes.StoreUpgrades{
			Added: []string{
				skutypes.StoreKey,
				billingtypes.StoreKey,
			},
			Deleted: []string{},
		},
	}
}

// CreateUpgradeHandler returns an upgrade handler that initializes the new modules.
// The module manager's RunMigrations will automatically call InitGenesis for new modules
// that don't have a version in the fromVM map.
func CreateUpgradeHandler(
	mm *module.Manager,
	configurator module.Configurator,
	_ *upgrades.AppKeepers,
) upgradetypes.UpgradeHandler {
	return func(ctx context.Context, _ upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
		// RunMigrations will:
		// 1. Detect that sku and billing modules are new (not in fromVM)
		// 2. Call InitGenesis for each new module with default genesis state
		// 3. Run any registered migrations for existing modules
		return mm.RunMigrations(ctx, configurator, fromVM)
	}
}
