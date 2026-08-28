package module_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"

	billingmodule "github.com/manifest-network/manifest-ledger/x/billing"
	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

const (
	testLeaseUUID    = "01912345-6789-7abc-8def-0123456789ab"
	testProviderUUID = "01912345-6789-7abc-8def-0123456789ac"
	testSKUUUID      = "01912345-6789-7abc-8def-0123456789ad"
	testDenom        = "umfx"
)

func validGenesis(minLeaseDuration uint64) *types.GenesisState {
	tenantAddr := sdk.AccAddress([]byte("billing-test-tenant"))
	items := []types.LeaseItem{{
		SkuUuid:     testSKUUUID,
		Quantity:    1,
		LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(100)),
		ServiceName: "web",
	}}

	return &types.GenesisState{
		Params: types.DefaultParams(),
		Leases: []types.Lease{{
			Uuid:                       testLeaseUUID,
			Tenant:                     tenantAddr.String(),
			ProviderUuid:               testProviderUUID,
			Items:                      items,
			State:                      types.LEASE_STATE_ACTIVE,
			MinLeaseDurationAtCreation: minLeaseDuration,
		}},
		CreditAccounts: []types.CreditAccount{{
			Tenant:           tenantAddr.String(),
			CreditAddress:    types.DeriveCreditAddress(tenantAddr).String(),
			ReservedAmounts:  types.CalculateLeaseReservation(items, minLeaseDuration),
			ActiveLeaseCount: 1,
		}},
		LeaseSequence: 1,
	}
}

func TestAppModuleBasicValidateGenesisAcceptsHistoricalExports(t *testing.T) {
	basic := billingmodule.AppModuleBasic{}
	encCfg := moduletestutil.MakeTestEncodingConfig(basic)
	validateGenesis := func(t *testing.T, gs *types.GenesisState) error {
		t.Helper()
		message := encCfg.Codec.MustMarshalJSON(gs)
		return basic.ValidateGenesis(encCfg.Codec, encCfg.TxConfig, message)
	}

	t.Run("legacy reservation from an earlier minimum duration", func(t *testing.T) {
		gs := validGenesis(types.DefaultMinLeaseDuration)
		gs.Params.MinLeaseDuration = 2 * types.DefaultMinLeaseDuration
		gs.Leases[0].MinLeaseDurationAtCreation = 0

		require.ErrorIs(t, gs.ValidateStrict(), types.ErrInvalidCreditOperation)
		require.NoError(t, gs.Validate())
		require.NoError(t, validateGenesis(t, gs))
	})

	t.Run("domain claimed before suffix reservation", func(t *testing.T) {
		gs := validGenesis(types.DefaultMinLeaseDuration)
		gs.Params.ReservedDomainSuffixes = []string{".manifest0.net"}
		gs.Leases[0].Items[0].CustomDomain = "app.manifest0.net"

		require.ErrorIs(t, gs.ValidateStrict(), types.ErrInvalidCustomDomain)
		require.NoError(t, gs.Validate())
		require.NoError(t, validateGenesis(t, gs))
	})
}

func TestAppModuleBasicValidateGenesisRetainsImportInvariants(t *testing.T) {
	basic := billingmodule.AppModuleBasic{}
	encCfg := moduletestutil.MakeTestEncodingConfig(basic)
	validateGenesis := func(t *testing.T, gs *types.GenesisState) error {
		t.Helper()
		message := encCfg.Codec.MustMarshalJSON(gs)
		return basic.ValidateGenesis(encCfg.Codec, encCfg.TxConfig, message)
	}

	tests := []struct {
		name     string
		mutate   func(*types.GenesisState)
		expected error
	}{
		{
			name: "duplicate lease UUID",
			mutate: func(gs *types.GenesisState) {
				gs.Leases = append(gs.Leases, gs.Leases[0])
				gs.CreditAccounts[0].ReservedAmounts = gs.CreditAccounts[0].ReservedAmounts.Add(gs.CreditAccounts[0].ReservedAmounts...)
				gs.CreditAccounts[0].ActiveLeaseCount = 2
				gs.LeaseSequence = 2
			},
			expected: types.ErrInvalidLease,
		},
		{
			name: "underreported active lease count",
			mutate: func(gs *types.GenesisState) {
				gs.CreditAccounts[0].ActiveLeaseCount = 0
			},
			expected: types.ErrInvalidCreditOperation,
		},
		{
			name: "low lease sequence",
			mutate: func(gs *types.GenesisState) {
				gs.LeaseSequence = 0
			},
			expected: types.ErrInvalidLease,
		},
		{
			name: "verifiable reservation mismatch",
			mutate: func(gs *types.GenesisState) {
				amount := gs.CreditAccounts[0].ReservedAmounts.AmountOf(testDenom).SubRaw(1)
				gs.CreditAccounts[0].ReservedAmounts = sdk.NewCoins(sdk.NewCoin(testDenom, amount))
			},
			expected: types.ErrInvalidCreditOperation,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gs := validGenesis(types.DefaultMinLeaseDuration)
			tc.mutate(gs)
			require.ErrorIs(t, validateGenesis(t, gs), tc.expected)
		})
	}

	err := basic.ValidateGenesis(encCfg.Codec, encCfg.TxConfig, json.RawMessage("{"))
	require.ErrorContains(t, err, "failed to unmarshal billing genesis state")
}
