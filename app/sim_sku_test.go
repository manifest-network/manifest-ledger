package app_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"
	simulationtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	billingtypes "github.com/manifest-network/manifest-ledger/x/billing/types"
	skutypes "github.com/manifest-network/manifest-ledger/x/sku/types"
)

func TestSKUSimulationCoverage(t *testing.T) {
	for _, test := range []struct {
		name          string
		providerOK    int
		providerNoOp  int
		skuOK         int
		skuNoOp       int
		expectedError string
	}{
		{name: "empty or SKU-disabled run", expectedError: "no SKU operations selected"},
		{name: "tiny run can select only no-ops", providerNoOp: 19, skuNoOp: 19},
		{name: "provider starvation threshold", providerNoOp: 20, expectedError: "no provider creations after 20 attempts"},
		{name: "providers do not hide SKU starvation", providerOK: 50, skuNoOp: 20, expectedError: "no SKU creations after 20 attempts"},
		{name: "pre-existing providers still require SKU success", skuNoOp: 20, expectedError: "no SKU creations after 20 attempts"},
		{name: "provider creation intentionally disabled", skuOK: 1},
		{name: "SKU creation intentionally disabled", providerOK: 1},
		{name: "SKU exercised", providerOK: 1, providerNoOp: 30, skuOK: 1, skuNoOp: 30},
	} {
		t.Run(test.name, func(t *testing.T) {
			stats := simulation.EventStats{skutypes.ModuleName: {
				sdk.MsgTypeURL(&skutypes.MsgCreateProvider{}): {"ok": test.providerOK, "failure": test.providerNoOp},
				sdk.MsgTypeURL(&skutypes.MsgCreateSKU{}):      {"ok": test.skuOK, "failure": test.skuNoOp},
			}}
			err := skuSimulationCoverageError(stats)
			if test.expectedError != "" {
				require.ErrorContains(t, err, test.expectedError)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestSKUSimulationCoverageEnforcedAfterSuccessfulBilling(t *testing.T) {
	stats := simulation.EventStats{
		billingtypes.ModuleName: {
			sdk.MsgTypeURL(&billingtypes.MsgFundCredit{}):  {"ok": 100},
			sdk.MsgTypeURL(&billingtypes.MsgCreateLease{}): {"ok": 100},
		},
	}
	config := simulationtypes.Config{Seed: 1729, ExportStatsPath: filepath.Join(t.TempDir(), "simulation-stats.json")}
	stats.ExportJSON(config.ExportStatsPath)
	require.ErrorContains(t, checkBillingSimulationResult(nil, config, false, nil), "no SKU operations selected")
	stats[skutypes.ModuleName] = map[string]map[string]int{
		sdk.MsgTypeURL(&skutypes.MsgCreateProvider{}): {"ok": 100},
		sdk.MsgTypeURL(&skutypes.MsgCreateSKU{}):      {"failure": 20},
	}
	stats.ExportJSON(config.ExportStatsPath)
	require.ErrorContains(t, checkBillingSimulationResult(nil, config, false, nil), "no SKU creations after 20 attempts")
	require.NoError(t, checkBillingSimulationResult(nil, config, true, nil), "interrupted runs preserve diagnostics without imposing success thresholds")
}
