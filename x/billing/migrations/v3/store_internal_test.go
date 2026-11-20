package v3

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

func TestUnsettledSeconds(t *testing.T) {
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	at := func(d time.Duration) *time.Time {
		v := now.Add(d)
		return &v
	}

	tests := []struct {
		name        string
		lease       types.Lease
		settledAt   *time.Time
		wantSecs    int64
		wantAccrues bool
	}{
		{
			name:        "active lease accrues from its last settlement",
			lease:       types.Lease{State: types.LEASE_STATE_ACTIVE, CreatedAt: now.Add(-time.Hour), AcknowledgedAt: at(-time.Hour)},
			settledAt:   at(-90 * time.Second),
			wantSecs:    90,
			wantAccrues: true,
		},
		{
			name:        "sub-second parts are dropped, as per-lease settlement did",
			lease:       types.Lease{State: types.LEASE_STATE_ACTIVE, CreatedAt: now.Add(-time.Hour), AcknowledgedAt: at(-time.Hour)},
			settledAt:   at(-1999 * time.Millisecond),
			wantSecs:    1,
			wantAccrues: true,
		},
		{
			name:        "active lease without last_settled_at accrues from its acknowledgement",
			lease:       types.Lease{State: types.LEASE_STATE_ACTIVE, CreatedAt: now.Add(-2 * time.Hour), AcknowledgedAt: at(-time.Hour)},
			wantSecs:    3600,
			wantAccrues: true,
		},
		{
			name:        "last_settled_at before the acknowledgement is clamped to it",
			lease:       types.Lease{State: types.LEASE_STATE_ACTIVE, CreatedAt: now.Add(-2 * time.Hour), AcknowledgedAt: at(-time.Hour)},
			settledAt:   at(-90 * time.Minute),
			wantSecs:    3600,
			wantAccrues: true,
		},
		{
			name:        "last_settled_at in the future owes nothing but still joins a stream",
			lease:       types.Lease{State: types.LEASE_STATE_ACTIVE, CreatedAt: now.Add(-time.Hour), AcknowledgedAt: at(-time.Hour)},
			settledAt:   at(time.Hour),
			wantSecs:    0,
			wantAccrues: true,
		},
		{
			name:        "closed lease settled at its closure owes nothing",
			lease:       types.Lease{State: types.LEASE_STATE_CLOSED, ClosedAt: at(-time.Hour)},
			settledAt:   at(-time.Hour),
			wantAccrues: false,
		},
		{
			name:        "closed lease settled before its closure owes the gap",
			lease:       types.Lease{State: types.LEASE_STATE_CLOSED, ClosedAt: at(-time.Hour)},
			settledAt:   at(-time.Hour - 40*time.Second),
			wantSecs:    40,
			wantAccrues: true,
		},
		{
			name:        "closed lease without last_settled_at owes nothing",
			lease:       types.Lease{State: types.LEASE_STATE_CLOSED, ClosedAt: at(-time.Hour)},
			wantAccrues: false,
		},
		{
			name:        "pending lease owes nothing",
			lease:       types.Lease{State: types.LEASE_STATE_PENDING},
			settledAt:   at(-time.Hour),
			wantAccrues: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			secs, accrues := unsettledSeconds(&tc.lease, tc.settledAt, now)
			require.Equal(t, tc.wantAccrues, accrues)
			if tc.wantAccrues {
				require.Equal(t, tc.wantSecs, secs)
			}
		})
	}
}
