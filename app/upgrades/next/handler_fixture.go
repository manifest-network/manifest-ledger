//go:build testnet_upgrade_fixture

package next

import (
	"context"

	upgradetypes "cosmossdk.io/x/upgrade/types"

	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/manifest-network/manifest-ledger/app/upgrades"
)

// CreateUpgradeHandler supplies a measurable migration for the disposable
// released-source rehearsal. Normal builds exclude this fixture entirely.
func CreateUpgradeHandler(
	mm *module.Manager,
	configurator module.Configurator,
	keepers *upgrades.AppKeepers,
) upgradetypes.UpgradeHandler {
	return func(ctx context.Context, _ upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
		versions, err := mm.RunMigrations(ctx, configurator, fromVM)
		if err != nil {
			return nil, err
		}
		params, err := keepers.BillingKeeper.GetParams(ctx)
		if err != nil {
			return nil, err
		}
		params.MaxLeasesPerTenant++
		if err := keepers.BillingKeeper.SetParams(ctx, params); err != nil {
			return nil, err
		}
		return versions, nil
	}
}
