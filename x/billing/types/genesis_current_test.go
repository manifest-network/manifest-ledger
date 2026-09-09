package types_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

func TestGenesisCurrentValidationKeepsImportCompatibilitySeparate(t *testing.T) {
	tenant := sdk.AccAddress([]byte("current-state-tenant"))
	now := time.Unix(1_700_000_000, 0).UTC()
	genesis := &types.GenesisState{
		Params: types.DefaultParams(),
		Leases: []types.Lease{{
			Uuid: "01912345-6789-7abc-8def-0123456789ab", Tenant: tenant.String(),
			ProviderUuid: "01912345-6789-7abc-8def-0123456789ac",
			Items: []types.LeaseItem{{
				SkuUuid: "01912345-6789-7abc-8def-0123456789ad", Quantity: 1,
				LockedPrice: sdk.NewInt64Coin(testDenom, 10),
			}},
			State: types.LEASE_STATE_ACTIVE, CreatedAt: now, LastSettledAt: now,
			MinLeaseDurationAtCreation: 1,
		}},
		CreditAccounts: []types.CreditAccount{{
			Tenant: tenant.String(), CreditAddress: types.DeriveCreditAddress(tenant).String(),
			ActiveLeaseCount: 1, ReservedAmounts: sdk.NewCoins(sdk.NewInt64Coin(testDenom, 10)),
		}},
		LeaseSequence: 1,
	}
	// A legacy import remains valid, while exactly the same state cannot pass
	// the current consensus invariant by masquerading as importable v2/v3 data.
	require.NoError(t, genesis.Validate())
	require.ErrorContains(t, genesis.ValidateCurrentState(), "no initialized reservation")
	genesis.Leases[0].Reservation = &types.LeaseReservation{
		RemainingAmounts: sdk.NewCoins(sdk.NewInt64Coin(testDenom, 10)),
	}
	require.NoError(t, genesis.ValidateCurrentState())

	genesis.CreditAccounts[0].ActiveLeaseCount = 0
	require.NoError(t, genesis.Validate())
	require.ErrorContains(t, genesis.ValidateCurrentState(), "active_lease_count 0 but has 1")
	require.Zero(t, genesis.CreditAccounts[0].ActiveLeaseCount, "validation must not repair the source")
	genesis.CreditAccounts[0].ActiveLeaseCount = 1

	genesis.Params.AllowedList = []string{tenant.String(), tenant.String()}
	require.NoError(t, genesis.Validate())
	require.Error(t, genesis.ValidateCurrentState())
	genesis.Params.AllowedList = nil

	// Runtime validation must still preserve a domain that predates a newly
	// reserved suffix. Strict authoring policies apply only to newly authored state.
	genesis.Leases[0].Items[0].ServiceName = "api"
	genesis.Leases[0].Items[0].CustomDomain = "api.example.com"
	genesis.Params.ReservedDomainSuffixes = []string{".example.com"}
	require.NoError(t, genesis.ValidateCurrentState())
	require.Error(t, genesis.ValidateStrict())
}
