package v3

import (
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	sdkmath "cosmossdk.io/math"

	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

// streamOwes recomputes what a stream dated and loaded by carryDebt owes at
// now, the way a settlement does: (remainder + rate × elapsed ns) / 1e9, with
// what is left below one unit.
func streamOwes(now, lastSettledAt time.Time, remainder, rate sdkmath.Int) (units, rest *big.Int) {
	total := new(big.Int).Mul(rate.BigInt(), big.NewInt(int64(now.Sub(lastSettledAt))))
	total.Add(total, remainder.BigInt())
	return new(big.Int).QuoRem(total, big.NewInt(nanosPerSecond), new(big.Int))
}

func TestCarryDebt(t *testing.T) {
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	pow2 := func(n uint) sdkmath.Int { return sdkmath.NewIntFromBigInt(new(big.Int).Lsh(big.NewInt(1), n)) }

	exact := []struct {
		name       string
		owed, rate sdkmath.Int
	}{
		{name: "whole seconds of the rate", owed: sdkmath.NewInt(500), rate: sdkmath.NewInt(1)},
		{name: "less than one second of the rate", owed: sdkmath.NewInt(1), rate: sdkmath.NewInt(2)},
		{name: "not a multiple of the rate", owed: sdkmath.NewInt(1_000_003), rate: sdkmath.NewInt(7)},
		{name: "rate above one unit per nanosecond", owed: sdkmath.NewInt(5), rate: sdkmath.NewInt(3_000_000_007)},
		{name: "nothing owed", owed: sdkmath.ZeroInt(), rate: sdkmath.NewInt(9)},
		// The debt times 1e9 does not fit 256 bits; the debt itself does.
		{name: "debt too large to scale to nanosecond units", owed: pow2(240), rate: pow2(240)},
		{name: "largest debts", owed: pow2(255), rate: pow2(250)},
	}
	for _, tc := range exact {
		t.Run(tc.name, func(t *testing.T) {
			lastSettledAt, remainder := carryDebt(now, tc.owed, tc.rate, false)

			require.False(t, lastSettledAt.After(now))
			require.False(t, remainder.IsNegative())
			require.True(t, remainder.LT(tc.rate), "the remainder is what is left of a division by the rate")

			units, rest := streamOwes(now, lastSettledAt, remainder, tc.rate)
			require.Zero(t, units.Cmp(tc.owed.BigInt()), "the stream owes exactly the debt: got %s, want %s", units, tc.owed)
			require.Zero(t, rest.Sign(), "and nothing more")
		})
	}

	beyondLimit := now.Add(-(maxSettlementGapSeconds + 1) * time.Second)
	overflow := []struct {
		name            string
		owed, rate      sdkmath.Int
		unrepresentable bool
	}{
		{name: "debt that was not representable", owed: sdkmath.NewInt(1), rate: sdkmath.NewInt(1), unrepresentable: true},
		{name: "debt worth more than the settlement limit", owed: sdkmath.NewInt(maxSettlementGapSeconds + 1), rate: sdkmath.NewInt(1)},
		{name: "debt worth more nanoseconds than fit a duration", owed: pow2(200), rate: sdkmath.NewInt(1)},
		{name: "zero rate", owed: sdkmath.NewInt(10), rate: sdkmath.ZeroInt()},
	}
	for _, tc := range overflow {
		t.Run(tc.name, func(t *testing.T) {
			lastSettledAt, remainder := carryDebt(now, tc.owed, tc.rate, tc.unrepresentable)
			// Dated just past what a settlement accrues over, so the first
			// settlement follows the accrual-overflow rule.
			require.Equal(t, beyondLimit, lastSettledAt)
			require.True(t, remainder.IsZero())
		})
	}

	t.Run("a debt worth exactly the settlement limit is still carried exactly", func(t *testing.T) {
		owed := sdkmath.NewInt(maxSettlementGapSeconds)
		lastSettledAt, remainder := carryDebt(now, owed, sdkmath.NewInt(1), false)
		require.Equal(t, now.Add(-maxSettlementGapSeconds*time.Second), lastSettledAt)
		require.True(t, remainder.IsZero())
	})
}

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
