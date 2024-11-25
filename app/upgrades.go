package app

import (
	"fmt"

	upgradetypes "cosmossdk.io/x/upgrade/types"

	"github.com/liftedinit/manifest-ledger/app/upgrades"
	"github.com/liftedinit/manifest-ledger/app/upgrades/next"
)

// Upgrades list of chain upgrades
var Upgrades []upgrades.Upgrade

// RegisterUpgradeHandlers registers the chain upgrade handlers
func (app *ManifestApp) RegisterUpgradeHandlers() {
	Upgrades = append(Upgrades, next.NewUpgrade(app.Version()))

	keepers := upgrades.AppKeepers{AccountKeeper: app.AccountKeeper, BankKeeper: app.BankKeeper}
	// register all upgrade handlers
	for _, upgrade := range Upgrades {
		app.UpgradeKeeper.SetUpgradeHandler(
			upgrade.UpgradeName,
			upgrade.CreateUpgradeHandler(
				app.ModuleManager,
				app.configurator,
				&keepers,
			),
		)
	}

	upgradeInfo, err := app.UpgradeKeeper.ReadUpgradeInfoFromDisk()
	if err != nil {
		panic(fmt.Sprintf("failed to read upgrade info from disk %s", err))
	}

	if app.UpgradeKeeper.IsSkipHeight(upgradeInfo.Height) {
		return
	}

	// register store loader for current upgrade
	for _, upgrade := range Upgrades {
		if upgradeInfo.Name == upgrade.UpgradeName {
			app.SetStoreLoader(upgradetypes.UpgradeStoreLoader(upgradeInfo.Height, &upgrade.StoreUpgrades)) // nolint:gosec
			break
		}
	}
}
