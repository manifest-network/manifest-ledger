package module_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"

	skumodule "github.com/manifest-network/manifest-ledger/x/sku"
	"github.com/manifest-network/manifest-ledger/x/sku/types"
)

func validGenesis() *types.GenesisState {
	provider := types.Provider{
		Uuid:          "01912345-6789-7abc-8def-0123456789ab",
		Address:       sdk.AccAddress([]byte("provider_address____")).String(),
		PayoutAddress: sdk.AccAddress([]byte("payout_address______")).String(),
		Active:        true,
	}
	return &types.GenesisState{
		Params:    types.DefaultParams(),
		Providers: []types.Provider{provider},
		Skus: []types.SKU{{
			Uuid:         "01912345-6789-7abc-8def-0123456789ac",
			ProviderUuid: provider.Uuid,
			Name:         "Compute",
			Unit:         types.Unit_UNIT_PER_HOUR,
			BasePrice:    sdk.NewInt64Coin("umfx", 3600),
			Active:       true,
		}},
		ProviderSequence: 1,
		SkuSequence:      1,
	}
}

func TestAppModuleBasicValidateGenesisAcceptsValidImports(t *testing.T) {
	basic := skumodule.AppModuleBasic{}
	encCfg := moduletestutil.MakeTestEncodingConfig(basic)

	t.Run("default genesis", func(t *testing.T) {
		require.NoError(t, basic.ValidateGenesis(encCfg.Codec, encCfg.TxConfig, basic.DefaultGenesis(encCfg.Codec)))
	})

	t.Run("populated genesis", func(t *testing.T) {
		message := encCfg.Codec.MustMarshalJSON(validGenesis())
		require.NoError(t, basic.ValidateGenesis(encCfg.Codec, encCfg.TxConfig, message))
	})

	t.Run("historical Bech32 spellings", func(t *testing.T) {
		gs := validGenesis()
		allowed := gs.Providers[0].Address
		gs.Params.AllowedList = []string{allowed, strings.ToUpper(allowed)}
		gs.Providers[0].Address = strings.ToUpper(gs.Providers[0].Address)
		gs.Providers[0].PayoutAddress = strings.ToUpper(gs.Providers[0].PayoutAddress)

		// The module gate must use the import contract, which canonicalizes
		// historical aliases before applying current parameter validation.
		require.ErrorIs(t, gs.Params.Validate(), types.ErrInvalidConfig)
		message := encCfg.Codec.MustMarshalJSON(gs)
		require.NoError(t, basic.ValidateGenesis(encCfg.Codec, encCfg.TxConfig, message))
	})
}

func TestAppModuleBasicValidateGenesisRejectsMalformedJSON(t *testing.T) {
	basic := skumodule.AppModuleBasic{}
	encCfg := moduletestutil.MakeTestEncodingConfig(basic)
	for name, message := range map[string]json.RawMessage{
		"invalid syntax":    json.RawMessage("{"),
		"invalid sequence":  json.RawMessage(`{"provider_sequence":"not-a-number"}`),
		"invalid providers": json.RawMessage(`{"providers":"not-an-array"}`),
	} {
		t.Run(name, func(t *testing.T) {
			err := basic.ValidateGenesis(encCfg.Codec, encCfg.TxConfig, message)
			require.ErrorContains(t, err, "failed to unmarshal sku genesis state")
		})
	}
}

func TestAppModuleBasicValidateGenesisRetainsImportInvariants(t *testing.T) {
	basic := skumodule.AppModuleBasic{}
	encCfg := moduletestutil.MakeTestEncodingConfig(basic)
	tests := []struct {
		name     string
		mutate   func(*types.GenesisState)
		expected error
	}{
		{
			name: "invalid allowed address",
			mutate: func(gs *types.GenesisState) {
				gs.Params.AllowedList = []string{"invalid"}
			},
			expected: types.ErrInvalidConfig,
		},
		{
			name: "invalid provider payout address",
			mutate: func(gs *types.GenesisState) {
				gs.Providers[0].PayoutAddress = "invalid"
			},
			expected: types.ErrInvalidProvider,
		},
		{
			name: "missing SKU provider",
			mutate: func(gs *types.GenesisState) {
				gs.Skus[0].ProviderUuid = "01912345-6789-7abc-8def-0123456789ad"
			},
			expected: types.ErrInvalidSKU,
		},
		{
			name: "nonintegral per-second price",
			mutate: func(gs *types.GenesisState) {
				gs.Skus[0].BasePrice = sdk.NewInt64Coin("umfx", 3601)
			},
			expected: types.ErrInvalidSKU,
		},
		{
			name: "low provider sequence",
			mutate: func(gs *types.GenesisState) {
				gs.ProviderSequence = 0
			},
			expected: types.ErrInvalidProvider,
		},
		{
			name: "low SKU sequence",
			mutate: func(gs *types.GenesisState) {
				gs.SkuSequence = 0
			},
			expected: types.ErrInvalidSKU,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gs := validGenesis()
			tc.mutate(gs)
			message := encCfg.Codec.MustMarshalJSON(gs)
			require.ErrorIs(t, basic.ValidateGenesis(encCfg.Codec, encCfg.TxConfig, message), tc.expected)
		})
	}
}
