package keeper_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/manifest-network/manifest-ledger/x/billing/keeper"
	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

func TestReservationAccountingInvariantRejectsFutureTimestamps(t *testing.T) {
	for _, test := range []struct {
		name   string
		closed bool
		mutate func(*types.Lease, time.Time)
		want   string
	}{
		{
			name: "settlement cursor", want: "last_settled_at",
			mutate: func(lease *types.Lease, future time.Time) { lease.LastSettledAt = future },
		},
		{
			name: "creation and settlement cursor", want: "last_settled_at",
			mutate: func(lease *types.Lease, future time.Time) {
				// Keep the normal CreatedAt <= LastSettledAt relationship valid
				// so only the context-dependent check can detect this corruption.
				lease.CreatedAt, lease.LastSettledAt = future, future
			},
		},
		{
			name: "terminal close", closed: true, want: "closed_at",
			mutate: func(lease *types.Lease, future time.Time) { lease.ClosedAt = &future },
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := setupCustomDomain(t)
			ctx := s.f.Ctx
			if test.closed {
				_, err := s.msgServer.CloseLease(ctx, &types.MsgCloseLease{
					Sender: s.tenant.String(), LeaseUuids: []string{s.leaseUUID},
				})
				require.NoError(t, err)
			}
			message, broken := keeper.ReservationAccountingInvariant(s.f.App.BillingKeeper)(ctx)
			require.False(t, broken, message, "timestamps equal to the current block remain valid")
			lease, err := s.f.App.BillingKeeper.GetLease(ctx, s.leaseUUID)
			require.NoError(t, err)
			future := ctx.BlockTime().Add(time.Second)
			test.mutate(&lease, future)
			require.NoError(t, s.f.App.BillingKeeper.SetLease(ctx, lease))
			require.NoError(t, s.f.App.BillingKeeper.ExportGenesis(ctx).ValidateCurrentState(), "ordinary state validation cannot see the current block time")
			message, broken = keeper.ReservationAccountingInvariant(s.f.App.BillingKeeper)(ctx)
			require.True(t, broken, message)
			require.Contains(t, message, "invalid billing timestamps")
			require.Contains(t, message, test.want)
			require.Contains(t, message, s.leaseUUID)
			message, broken = keeper.ReservationAccountingInvariant(s.f.App.BillingKeeper)(ctx.WithBlockTime(future))
			require.False(t, broken, message, "the equality boundary must remain valid")
		})
	}
}
