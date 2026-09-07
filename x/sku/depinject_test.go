package module_test

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	appv1alpha1 "cosmossdk.io/api/cosmos/app/v1alpha1"
	"cosmossdk.io/collections/colltest"
	"cosmossdk.io/core/appconfig"
	"cosmossdk.io/depinject"
	"cosmossdk.io/log"

	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	accountkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"

	modulev1 "github.com/manifest-network/manifest-ledger/api/liftedinit/sku/module/v1"
	skumodule "github.com/manifest-network/manifest-ledger/x/sku"
	skukeeper "github.com/manifest-network/manifest-ledger/x/sku/keeper"
)

func TestProvideModuleInjectsSimulationKeepers(t *testing.T) {
	storeService, _ := colltest.MockStore()
	encoding := moduletestutil.MakeTestEncodingConfig()
	accountKeeper := accountkeeper.AccountKeeper{}
	bankKeeper := bankkeeper.BaseKeeper{}

	var skuKeeper skukeeper.Keeper
	require.NoError(t, depinject.Inject(depinject.Configs(
		appconfig.Compose(&appv1alpha1.Config{Modules: []*appv1alpha1.ModuleConfig{{
			Name: "sku", Config: appconfig.WrapAny(&modulev1.Module{}),
		}}}),
		depinject.Supply(encoding.Codec, storeService, log.NewNopLogger(), accountKeeper, bankKeeper),
	), &skuKeeper))
	require.NotNil(t, skuKeeper.GetAccountKeeper())
	require.NotNil(t, skuKeeper.GetBankKeeper())
}

func TestModuleDescriptorRegistrationPackage(t *testing.T) {
	options := (&modulev1.Module{}).ProtoReflect().Descriptor().Options()
	require.True(t, proto.HasExtension(options, appv1alpha1.E_Module))
	descriptor := proto.GetExtension(options, appv1alpha1.E_Module).(*appv1alpha1.ModuleDescriptor)
	require.Equal(t, reflect.TypeFor[skumodule.AppModule]().PkgPath(), descriptor.GoImport)
}
