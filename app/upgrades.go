package app

import (
	"fmt"

	upgradetypes "cosmossdk.io/x/upgrade/types"

	"github.com/manifest-network/manifest-ledger/app/upgrades"
	"github.com/manifest-network/manifest-ledger/app/upgrades/next"
)

// Upgrades list of chain upgrades
var Upgrades []upgrades.Upgrade

// RegisterUpgradeHandlers registers the chain upgrade handlers
func (app *ManifestApp) RegisterUpgradeHandlers() {
	Upgrades = append(Upgrades, next.NewUpgrade(app.Version()))

	// Halt-recovery binary swap: the chain's last applied on-chain upgrade is
	// "v2.1.1". x/upgrade's PreBlocker downgrade check (abci.go) refuses to boot
	// any binary that lacks a handler for the last applied upgrade, even when the
	// chain is recovered by swapping binaries rather than via a new governance
	// upgrade. next.NewUpgrade is a noop handler that never executes without a
	// matching plan; registering it only satisfies HasHandler("v2.1.1") so the
	// node boots. Do NOT carry this into the v2.2.x line (different last-applied
	// upgrade); the general fix is to register the last applied upgrade name.
	if app.Version() != "v2.1.1" {
		Upgrades = append(Upgrades, next.NewUpgrade("v2.1.1"))
	}

	keepers := upgrades.AppKeepers{
		AccountKeeper: app.AccountKeeper,
		BankKeeper:    app.BankKeeper,
		WasmKeeper:    app.WasmKeeper,
		SKUKeeper:     app.SKUKeeper,
		BillingKeeper: app.BillingKeeper,
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
