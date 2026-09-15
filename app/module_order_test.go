package app

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	crisistypes "github.com/cosmos/cosmos-sdk/x/crisis/types"

	billingtypes "github.com/manifest-network/manifest-ledger/x/billing/types"
	skutypes "github.com/manifest-network/manifest-ledger/x/sku/types"
)

func TestBillingGenesisDependencyOrder(t *testing.T) {
	manifestApp, _ := setup(t, false)
	order := manifestApp.ModuleManager.OrderInitGenesis
	bankIndex := slices.Index(order, banktypes.ModuleName)
	skuIndex := slices.Index(order, skutypes.ModuleName)
	billingIndex := slices.Index(order, billingtypes.ModuleName)
	crisisIndex := slices.Index(order, crisistypes.ModuleName)
	require.NotEqual(t, -1, bankIndex)
	require.NotEqual(t, -1, skuIndex)
	require.NotEqual(t, -1, billingIndex)
	require.NotEqual(t, -1, crisisIndex)
	require.Less(t, bankIndex, billingIndex)
	require.Less(t, skuIndex, billingIndex)
	require.Less(t, billingIndex, crisisIndex)
	require.Equal(t, order, manifestApp.ModuleManager.OrderExportGenesis)
}
