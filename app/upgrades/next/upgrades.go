package next

import (
	"context"

	storetypes "cosmossdk.io/store/types"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/liftedinit/manifest-ledger/app/upgrades"
)

func NewUpgrade() upgrades.Upgrade {
	return upgrades.Upgrade{
		UpgradeName:          "umfx-denom-metadata",
		CreateUpgradeHandler: CreateUpgradeHandler,
		StoreUpgrades: storetypes.StoreUpgrades{
			Added:   []string{},
			Deleted: []string{},
		},
	}
}

func CreateUpgradeHandler(
	mm *module.Manager,
	configurator module.Configurator,
	keepers *upgrades.AppKeepers,
) upgradetypes.UpgradeHandler {
	return func(ctx context.Context, _ upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
		metadata := banktypes.Metadata{
			Description: "The Manifest Network token",
			DenomUnits: []*banktypes.DenomUnit{
				{
					Denom:    "umfx",
					Exponent: 0,
					Aliases:  []string{},
				},
				{
					Denom:    "MFX",
					Exponent: 6,
					Aliases:  []string{},
				},
			},
			Base:    "umfx",
			Display: "MFX",
			Symbol:  "MFX",
		}

		// Set the new metadata in the bank keeper
		keepers.BankKeeper.SetDenomMetaData(ctx, metadata)

		return mm.RunMigrations(ctx, configurator, fromVM)
	}
}
