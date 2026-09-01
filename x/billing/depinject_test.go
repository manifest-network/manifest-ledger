package module_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/collections/colltest"
	"cosmossdk.io/log"

	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	accountkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"

	billingmodule "github.com/manifest-network/manifest-ledger/x/billing"
	skukeeper "github.com/manifest-network/manifest-ledger/x/sku/keeper"
)

func TestProvideModuleInjectsRequiredKeepers(t *testing.T) {
	billingStore, _ := colltest.MockStore()
	skuStore, _ := colltest.MockStore()
	encoding := moduletestutil.MakeTestEncodingConfig()
	accountKeeper := accountkeeper.AccountKeeper{}
	bankKeeper := bankkeeper.BaseKeeper{}
	skuKeeper := skukeeper.NewKeeper(
		encoding.Codec,
		skuStore,
		log.NewNopLogger(),
		"authority",
		accountKeeper,
		bankKeeper,
	)

	outputs := billingmodule.ProvideModule(billingmodule.ModuleInputs{
		Cdc:           encoding.Codec,
		StoreService:  billingStore,
		Logger:        log.NewNopLogger(),
		SKUKeeper:     skuKeeper,
		BankKeeper:    bankKeeper,
		AccountKeeper: accountKeeper,
	})
	billingKeeper := outputs.Keeper
	require.NotNil(t, billingKeeper.GetAccountKeeper())
	require.NotNil(t, billingKeeper.GetBankKeeper())
}
