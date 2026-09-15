package next

import (
	storetypes "cosmossdk.io/store/types"

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
