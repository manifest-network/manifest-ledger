package types_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

func TestGenesisStateValidateAcceptsMaximumLeaseItems(t *testing.T) {
	items := make([]types.LeaseItem, types.MaxItemsPerLeaseHardLimit)
	for i := range items {
		items[i] = types.LeaseItem{
			SkuUuid:     fmt.Sprintf("01912345-6789-7abc-8def-%012d", i+1),
			Quantity:    1,
			LockedPrice: sdk.NewInt64Coin(testDenom, 1),
		}
	}
	now := time.Unix(1, 0).UTC()
	tenant := sdk.AccAddress([]byte("max-items-tenant____"))
	genesis := &types.GenesisState{
		Params: types.DefaultParams(),
		Leases: []types.Lease{{
			Uuid:          "01912345-6789-7abc-8def-0123456789ab",
			Tenant:        tenant.String(),
			ProviderUuid:  "01912345-6789-7abc-8def-0123456789ac",
			Items:         items,
			State:         types.LEASE_STATE_REJECTED,
			CreatedAt:     now,
			LastSettledAt: now,
			Reservation:   &types.LeaseReservation{RemainingAmounts: sdk.NewCoins()},
		}},
		LeaseSequence: 1,
	}

	require.NoError(t, genesis.Validate())
	require.NoError(t, genesis.ValidateStrict())
}
