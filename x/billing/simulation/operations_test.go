package simulation_test

import (
	"bytes"
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/app"
	billingsimulation "github.com/manifest-network/manifest-ledger/x/billing/simulation"
	billingtypes "github.com/manifest-network/manifest-ledger/x/billing/types"
)

func TestSimulateMsgAcknowledgeLeaseFiltersUnacknowledgeableLeases(t *testing.T) {
	ctx, manifestApp := app.Setup(t)
	now := time.Unix(10_000, 0).UTC()
	ctx = ctx.WithBlockTime(now)

	params := billingtypes.DefaultParams()
	params.MaxLeasesPerTenant = 1
	params.PendingTimeout = 60
	require.NoError(t, manifestApp.BillingKeeper.SetParams(ctx, params))

	tenant := func(value byte) sdk.AccAddress {
		return sdk.AccAddress(bytes.Repeat([]byte{value}, 20))
	}
	missingAccountTenant := tenant(1)
	atCapTenant := tenant(2)
	lease := func(uuid string, owner sdk.AccAddress, state billingtypes.LeaseState, createdAt time.Time) billingtypes.Lease {
		return billingtypes.Lease{
			Uuid:         uuid,
			Tenant:       owner.String(),
			ProviderUuid: "01912345-6789-7abc-8def-0123456789ba",
			State:        state,
			CreatedAt:    createdAt,
		}
	}

	leases := []billingtypes.Lease{
		lease("01912345-6789-7abc-8def-0123456789ab", tenant(3), billingtypes.LEASE_STATE_ACTIVE, now),
		lease("01912345-6789-7abc-8def-0123456789ac", tenant(4), billingtypes.LEASE_STATE_PENDING, now.Add(-61*time.Second)),
		lease("01912345-6789-7abc-8def-0123456789ad", missingAccountTenant, billingtypes.LEASE_STATE_PENDING, now),
		lease("01912345-6789-7abc-8def-0123456789ae", missingAccountTenant, billingtypes.LEASE_STATE_PENDING, now),
		lease("01912345-6789-7abc-8def-0123456789af", atCapTenant, billingtypes.LEASE_STATE_PENDING, now),
		lease("01912345-6789-7abc-8def-0123456789b0", atCapTenant, billingtypes.LEASE_STATE_PENDING, now),
	}
	for _, storedLease := range leases {
		require.NoError(t, manifestApp.BillingKeeper.SetLease(ctx, storedLease))
	}
	require.NoError(t, manifestApp.BillingKeeper.SetCreditAccount(ctx, billingtypes.CreditAccount{
		Tenant:           atCapTenant.String(),
		CreditAddress:    billingtypes.DeriveCreditAddress(atCapTenant).String(),
		ActiveLeaseCount: params.MaxLeasesPerTenant,
	}))

	operation := billingsimulation.SimulateMsgAcknowledgeLease(
		nil,
		manifestApp.BillingKeeper,
		&manifestApp.SKUKeeper,
	)
	r := rand.New(rand.NewSource(1)) //nolint:gosec
	opMsg, futureOps, err := operation(r, nil, ctx, nil, "")
	require.NoError(t, err)
	require.Nil(t, futureOps)
	require.False(t, opMsg.OK)
	require.Equal(t, "no acknowledgeable pending leases found", opMsg.Comment)

	require.NoError(t, manifestApp.BillingKeeper.Params.Remove(ctx))
	opMsg, futureOps, err = operation(r, nil, ctx, nil, "")
	require.NoError(t, err)
	require.Nil(t, futureOps)
	require.False(t, opMsg.OK)
	require.Equal(t, "failed to get params", opMsg.Comment)
}
