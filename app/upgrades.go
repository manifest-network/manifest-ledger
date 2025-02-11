package app

import (
	"fmt"

	upgradetypes "cosmossdk.io/x/upgrade/types"

	"github.com/liftedinit/manifest-ledger/app/upgrades"
	v2 "github.com/liftedinit/manifest-ledger/app/upgrades/v2"
)

// Upgrades list of chain upgrades
var Upgrades []upgrades.Upgrade

// RegisterUpgradeHandlers registers the chain upgrade handlers
func (app *ManifestApp) RegisterUpgradeHandlers() {
	// Use "v2" to match the package name in app/upgrades/v2/
	Upgrades = append(Upgrades, v2.NewUpgrade("v2"))

	keepers := upgrades.AppKeepers{
		AccountKeeper: app.AccountKeeper,
		BankKeeper:    app.BankKeeper,
		WasmKeeper:    app.WasmKeeper,
	}

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
