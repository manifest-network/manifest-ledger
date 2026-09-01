package module_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/collections/colltest"
	"cosmossdk.io/log"

	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	accountkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"

	skumodule "github.com/manifest-network/manifest-ledger/x/sku"
)

func TestProvideModuleInjectsSimulationKeepers(t *testing.T) {
	storeService, _ := colltest.MockStore()
	encoding := moduletestutil.MakeTestEncodingConfig()
	accountKeeper := accountkeeper.AccountKeeper{}
	bankKeeper := bankkeeper.BaseKeeper{}

	outputs := skumodule.ProvideModule(skumodule.ModuleInputs{
		Cdc:           encoding.Codec,
		StoreService:  storeService,
		Logger:        log.NewNopLogger(),
		AccountKeeper: accountKeeper,
		BankKeeper:    bankKeeper,
	})
	skuKeeper := outputs.Keeper
	require.NotNil(t, skuKeeper.GetAccountKeeper())
	require.NotNil(t, skuKeeper.GetBankKeeper())
}
