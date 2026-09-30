//go:build !testnet_upgrade_fixture

package next

import (
	"context"

	upgradetypes "cosmossdk.io/x/upgrade/types"

	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/manifest-network/manifest-ledger/app/upgrades"
)

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
