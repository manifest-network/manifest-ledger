package v001rc5

import (
	"context"

	errorsmod "cosmossdk.io/errors"
	storetypes "cosmossdk.io/store/types"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	"github.com/liftedinit/manifest-ledger/app/helpers"

	"github.com/cosmos/cosmos-sdk/types/module"

	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"

	"github.com/liftedinit/manifest-ledger/app/upgrades"
)

func NewUpgrade(name string) upgrades.Upgrade {
	return upgrades.Upgrade{
		UpgradeName:          name,
		CreateUpgradeHandler: CreateUpgradeHandler,
		StoreUpgrades: storetypes.StoreUpgrades{
			Added: []string{
				wasmtypes.ModuleName,
			},
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
		fromVM, err := mm.RunMigrations(ctx, configurator, fromVM)
		if err != nil {
			return fromVM, err
		}

		// Set CosmWasm params
		wasmParams := wasmtypes.DefaultParams()
		wasmParams.CodeUploadAccess = wasmtypes.AccessConfig{
			Permission: wasmtypes.AccessTypeAnyOfAddresses,
			Addresses:  []string{helpers.GetPoAAdmin()},
		}
		wasmParams.InstantiateDefaultPermission = wasmtypes.AccessTypeAnyOfAddresses

		if err := keepers.WasmKeeper.SetParams(ctx, wasmParams); err != nil {
			return fromVM, errorsmod.Wrapf(err, "unable to set CosmWasm params")
		}

		return fromVM, nil
	}
}
