package module

import (
	"os"

	"cosmossdk.io/core/address"
	"cosmossdk.io/core/appmodule"
	"cosmossdk.io/core/store"
	"cosmossdk.io/depinject"
	"cosmossdk.io/log"

	"github.com/cosmos/cosmos-sdk/codec"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	mintkeeper "github.com/cosmos/cosmos-sdk/x/mint/keeper"

	modulev1 "github.com/liftedinit/manifest-ledger/api/liftedinit/manifest/module/v1"
	"github.com/liftedinit/manifest-ledger/x/manifest/keeper"
)

var _ appmodule.AppModule = AppModule{}

// IsOnePerModuleType implements the depinject.OnePerModuleType interface.
func (am AppModule) IsOnePerModuleType() {}

// IsAppModule implements the appmodule.AppModule interface.
func (am AppModule) IsAppModule() {}

func init() {
	appmodule.Register(
		&modulev1.Module{},
		appmodule.Provide(ProvideModule),
	)
}

//nolint:revive
type ModuleInputs struct {
	depinject.In

	Cdc          codec.Codec
	StoreService store.KVStoreService
	AddressCodec address.Codec

	MintKeeper mintkeeper.Keeper
	BankKeeper bankkeeper.Keeper
}

//nolint:revive
type ModuleOutputs struct {
	depinject.Out

	Module appmodule.AppModule
	Keeper keeper.Keeper
}

func ProvideModule(in ModuleInputs) ModuleOutputs {
	k := keeper.NewKeeper(in.Cdc, in.StoreService, in.MintKeeper, in.BankKeeper, log.NewLogger(os.Stderr), authtypes.NewModuleAddress(govtypes.ModuleName).String())
	m := NewAppModule(in.Cdc, k, in.MintKeeper)

	return ModuleOutputs{Module: m, Keeper: k, Out: depinject.Out{}}
}
