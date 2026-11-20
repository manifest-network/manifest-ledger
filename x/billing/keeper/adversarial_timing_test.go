/*
Package keeper_test contains adversarial tests targeting timing and ordering edge cases.

These tests attempt to exploit clock skew, same-block race conditions, timestamp
boundaries, and EndBlocker execution patterns.
*/
package keeper_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/keeper"
	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

// =============================================================================
// Block Time Going Backwards
// =============================================================================

// TestAdversarial_BlockTimeGoesBackward tests behavior when block time goes backward
// (possible with clock skew in Tendermint). Settlement should not produce negative
// duration or double-charge.
func TestAdversarial_BlockTimeGoesBackward(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]

	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(1_000_000))))
	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{
		{SkuUuid: sku.Uuid, Quantity: 1},
	})
	withdraw := &types.MsgWithdraw{Sender: providerAddr.String(), ProviderUuid: provider.Uuid}
	streamKey := keeper.NewStreamKey(provider.Uuid, tenant, testDenom)

	// First: advance time normally and withdraw
	f.Ctx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(100 * time.Second))
	settledAt := f.Ctx.BlockTime()
	resp, err := msgServer.Withdraw(f.Ctx, withdraw)
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(100))), resp.TotalAmounts)

	// Now: time goes BACKWARD (clock skew)
	f.Ctx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(-50 * time.Second))

	// Withdrawing with backward time yields zero (duration <= 0) ...
	resp, err = msgServer.Withdraw(f.Ctx, withdraw)
	require.NoError(t, err)
	require.True(t, resp.TotalAmounts.IsZero())
	require.Zero(t, resp.WithdrawalCount)

	// ... neither charges the tenant nor rewinds the stream ...
	require.Equal(t, sdkmath.NewInt(1_000_000-100), f.App.BankKeeper.GetBalance(f.Ctx, creditAddr, testDenom).Amount)
	state, err := f.App.BillingKeeper.Streams.Get(f.Ctx, streamKey)
	require.NoError(t, err)
	require.Equal(t, settledAt, state.LastSettledAt, "a settlement in the past must not move last_settled_at backward")
	require.False(t, state.IsExhausted())

	// ... so the 50 seconds lived twice are not charged twice once time moves on.
	f.Ctx = f.Ctx.WithBlockTime(settledAt.Add(10 * time.Second))
	resp, err = msgServer.Withdraw(f.Ctx, withdraw)
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(10))), resp.TotalAmounts)
	require.Equal(t, sdkmath.NewInt(1_000_000-110), f.App.BankKeeper.GetBalance(f.Ctx, creditAddr, testDenom).Amount)
	requireInvariants(t, f)
}

// TestAdversarial_SettlementWithNothingPayable tests settling a stream (1/s)
// at, before, or less than a second after its last_settled_at. No whole unit
// has accrued, so nothing is transferred and no amount is ever negative. A
// settlement at or before last_settled_at changes nothing at all; one after it
// moves last_settled_at to the settlement time and carries the fraction accrued
// in the remainder, so it is not lost.
func TestAdversarial_SettlementWithNothingPayable(t *testing.T) {
	tests := []struct {
		name   string
		offset time.Duration
		// balance is the tenant's credit when the stream is settled.
		balance int64
		// wantSettledOffset is where last_settled_at ends up, relative to where it was.
		wantSettledOffset time.Duration
		// wantRemainder is the accrual carried, in billionths of a unit.
		wantRemainder int64
		// A stream with no credit left is exhausted even with nothing accrued.
		wantFrozen bool
	}{
		{name: "zero duration", offset: 0, balance: 1_000_000},
		{name: "negative duration", offset: -100 * time.Second, balance: 1_000_000},
		{
			name: "one nanosecond", offset: time.Nanosecond, balance: 1_000_000,
			wantSettledOffset: time.Nanosecond, wantRemainder: 1,
		},
		{
			name: "sub-second duration", offset: 999 * time.Millisecond, balance: 1_000_000,
			wantSettledOffset: 999 * time.Millisecond, wantRemainder: 999_000_000,
		},
		{
			name: "one nanosecond short of a whole unit", offset: time.Second - time.Nanosecond, balance: 1_000_000,
			wantSettledOffset: time.Second - time.Nanosecond, wantRemainder: 999_999_999,
		},
		{name: "zero duration and no credit left", offset: 0, balance: 0, wantFrozen: true},
		{name: "negative duration and no credit left", offset: -100 * time.Second, balance: 0, wantFrozen: true},
		{
			name: "sub-second duration and no credit left", offset: 999 * time.Millisecond, balance: 0,
			wantSettledOffset: 999 * time.Millisecond, wantRemainder: 999_000_000, wantFrozen: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newStreamFixture(t, testDenom)
			tenant := s.TestAccs[0]
			s.fundTenant(t, tenant, testDenom, 1_000_000)
			s.lease(t, tenant)
			now := s.Ctx.BlockTime()

			// Leave the tenant exactly tc.balance.
			creditAddr := types.DeriveCreditAddress(tenant)
			drain := sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(1_000_000-tc.balance)))
			if !drain.IsZero() {
				require.NoError(t, s.App.BankKeeper.SendCoins(s.Ctx, creditAddr, s.TestAccs[4], drain))
			}

			key := keeper.NewStreamKey(s.provider.Uuid, tenant, testDenom)
			result, err := s.App.BillingKeeper.SettleStream(s.Ctx, key, now.Add(tc.offset))
			require.NoError(t, err)
			require.True(t, result.Transferred.IsZero(), "less than a whole unit must not transfer anything")
			require.False(t, result.Transferred.IsNegative())
			require.False(t, result.PaidInFull, "no whole unit accrued, so nothing was paid in full")
			require.Equal(t, tc.wantFrozen, result.Exhausted)
			require.Equal(t, tc.wantFrozen, result.Froze)

			state, exists := s.stream(t, tenant, testDenom)
			require.True(t, exists)
			require.Equal(t, now.Add(tc.wantSettledOffset), state.LastSettledAt)
			require.False(t, state.Remainder.IsNil())
			require.Equal(t, sdkmath.NewInt(tc.wantRemainder).String(), state.Remainder.String())
			require.Equal(t, tc.wantFrozen, state.IsExhausted())
			require.True(t, s.payout(testDenom).IsZero())
			require.Equal(t, sdkmath.NewInt(tc.balance).String(), s.credit(tenant, testDenom).String())
			require.NoError(t, s.App.BillingKeeper.CheckStreamInvariant(s.Ctx))
		})
	}
}

// =============================================================================
// EndBlocker Edge Cases
// =============================================================================

// TestAdversarial_EndBlockerWithZeroPendingLeases tests that EndBlocker is a no-op
// when there are no pending leases.
func TestAdversarial_EndBlockerWithZeroPendingLeases(t *testing.T) {
	f := initFixture(t)

	// No leases at all — EndBlocker should not error
	err := f.App.BillingKeeper.EndBlocker(f.Ctx)
	require.NoError(t, err)
}

// TestAdversarial_EndBlockerRateLimiting tests that EndBlocker respects the
// MaxPendingLeaseExpirationsPerBlock rate limit.
func TestAdversarial_EndBlockerRateLimiting(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	providerAddr := f.TestAccs[1]
	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Increase pending limit to allow creating many
	err := f.App.BillingKeeper.SetParams(f.Ctx, types.Params{
		MaxLeasesPerTenant:        10000,
		MaxItemsPerLease:          20,
		MinLeaseDuration:          60, // Lower for faster testing
		MaxPendingLeasesPerTenant: 1000,
		PendingTimeout:            60,
	})
	require.NoError(t, err)

	// Create 150 pending leases across different tenants (to exceed rate limit)
	numLeases := 150
	for i := 0; i < numLeases; i++ {
		tenant := sdk.AccAddress([]byte("tenant" + string(rune(i+'A')) + string(rune(i%256))))
		creditAddr := types.DeriveCreditAddress(tenant)
		f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(100_000_000))))
		err := f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
			Tenant:        tenant.String(),
			CreditAddress: creditAddr.String(),
		})
		require.NoError(t, err)

		_, err = msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
			Tenant: tenant.String(),
			Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
		})
		require.NoError(t, err)
	}

	// Advance past timeout
	f.Ctx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(61 * time.Second))

	// Run EndBlocker — should only process MaxPendingLeaseExpirationsPerBlock
	err = f.App.BillingKeeper.EndBlocker(f.Ctx)
	require.NoError(t, err)

	// Count remaining pending leases
	pending, err := f.App.BillingKeeper.GetPendingLeases(f.Ctx)
	require.NoError(t, err)

	// Should have expired exactly MaxPendingLeaseExpirationsPerBlock
	expectedRemaining := numLeases - types.MaxPendingLeaseExpirationsPerBlock
	require.Equal(t, expectedRemaining, len(pending),
		"EndBlocker should expire exactly %d leases, leaving %d pending (got %d)",
		types.MaxPendingLeaseExpirationsPerBlock, expectedRemaining, len(pending))

	// Run EndBlocker again to expire the rest
	err = f.App.BillingKeeper.EndBlocker(f.Ctx)
	require.NoError(t, err)

	pending, err = f.App.BillingKeeper.GetPendingLeases(f.Ctx)
	require.NoError(t, err)
	require.Equal(t, 0, len(pending), "all pending leases should be expired after two EndBlocker runs")
}

// TestAdversarial_EndBlockerAutoCloseBudgetAcrossStreams tests that the
// EndBlocker's auto-close budget (MaxAutoClosesPerBlock) is a budget per block,
// not per stream: with a large frozen stream ahead of a small one in the drain
// queue, every block closes exactly as many leases as the budget allows (never
// more), the small stream is served in the first block instead of waiting
// behind the large one, and the budget it does not need goes back to the large
// stream.
func TestAdversarial_EndBlockerAutoCloseBudgetAcrossStreams(t *testing.T) {
	s := newStreamFixture(t, testDenom)
	k := s.App.BillingKeeper

	// The drain queue is in key order, so the large tenant's address sorts first.
	large := sdk.AccAddress(bytes.Repeat([]byte{0x01}, 20))
	small := sdk.AccAddress(bytes.Repeat([]byte{0xfe}, 20))
	const largeLeases = 2*types.MaxAutoClosesPerBlock + 50

	// Each tenant holds exactly its reservation: one hour at 1/s per lease.
	s.fundTenant(t, large, testDenom, largeLeases*3600)
	s.fundTenant(t, small, testDenom, 3600)
	for range largeLeases {
		s.lease(t, large)
	}
	smallLease := s.lease(t, small)

	// Two hours later both streams owe twice their credit: both freeze.
	s.advance(2 * time.Hour)
	s.Ctx = s.Ctx.WithEventManager(sdk.NewEventManager())
	s.withdraw(t, 0, nil)
	require.Equal(t, 2, countEvents(s.Ctx, types.EventTypeStreamExhausted))
	s.requireInvariants(t)

	remaining := largeLeases + 1
	blocks := 0
	for remaining > 0 {
		blocks++
		s.advance(6 * time.Second)
		s.Ctx = s.Ctx.WithEventManager(sdk.NewEventManager())
		require.NoError(t, k.EndBlocker(s.Ctx))

		closed := countEvents(s.Ctx, types.EventTypeLeaseAutoClose)
		require.Equal(t, min(remaining, types.MaxAutoClosesPerBlock), closed,
			"block %d must close exactly what the budget allows", blocks)
		remaining -= closed
		s.requireInvariants(t)

		if blocks == 1 {
			// The small stream did not wait behind the 250 leases ahead of it.
			lease, err := k.GetLease(s.Ctx, smallLease)
			require.NoError(t, err)
			require.Equal(t, types.LEASE_STATE_CLOSED, lease.State)
			require.Equal(t, types.ClosureReasonCreditExhausted, lease.ClosureReason)
			require.Equal(t, s.Ctx.BlockTime(), *lease.ClosedAt)

			state, exists := s.stream(t, large, testDenom)
			require.True(t, exists)
			require.Equal(t, uint64(largeLeases-(types.MaxAutoClosesPerBlock-1)), state.LeaseCount,
				"the large stream got the rest of the block's budget")
		}
	}
	require.Equal(t, 3, blocks, "250 + 1 leases at 100 per block")

	streams, err := k.GetAllStreams(s.Ctx)
	require.NoError(t, err)
	require.Empty(t, streams)
	for _, tenant := range []sdk.AccAddress{large, small} {
		account, err := k.GetCreditAccount(s.Ctx, tenant.String())
		require.NoError(t, err)
		require.Zero(t, account.ActiveLeaseCount)
		require.True(t, account.ReservedAmounts.IsZero())
	}

	// The drain moved no funds: the providers were paid by the settlement only.
	require.Equal(t, sdkmath.NewInt((largeLeases+1)*3600), s.payout(testDenom))
}

// TestAdversarial_EndBlockerDrainSharesBudgetBetweenStreams tests the fairness
// of the drain within one block: with two frozen streams, the first of which
// alone exceeds the block's budget, a single EndBlocker call must make progress
// on the second one too. Every queued stream first gets an equal share of the
// budget; what a stream does not use is then spent in queue order.
func TestAdversarial_EndBlockerDrainSharesBudgetBetweenStreams(t *testing.T) {
	const budget = types.MaxAutoClosesPerBlock

	tests := []struct {
		name          string
		first, second int
		// Leases closed in each stream by one EndBlocker call.
		wantFirst, wantSecond int
	}{
		{name: "second stream of one lease", first: budget + 50, second: 1, wantFirst: budget - 1, wantSecond: 1},
		{name: "second stream smaller than its share", first: budget + 50, second: 30, wantFirst: budget - 30, wantSecond: 30},
		{name: "both streams larger than their share", first: budget + 20, second: budget + 20, wantFirst: budget / 2, wantSecond: budget / 2},
		{name: "first stream smaller than its share", first: 10, second: budget + 50, wantFirst: 10, wantSecond: budget - 10},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newStreamFixture(t, testDenom)
			k := s.App.BillingKeeper

			// The drain queue is in key order: the first tenant's address sorts first.
			first := sdk.AccAddress(bytes.Repeat([]byte{0x01}, 20))
			second := sdk.AccAddress(bytes.Repeat([]byte{0xfe}, 20))
			s.fundTenant(t, first, testDenom, int64(tc.first)*3600)
			s.fundTenant(t, second, testDenom, int64(tc.second)*3600)
			for range tc.first {
				s.lease(t, first)
			}
			for range tc.second {
				s.lease(t, second)
			}

			s.advance(2 * time.Hour)
			s.Ctx = s.Ctx.WithEventManager(sdk.NewEventManager())
			s.withdraw(t, 0, nil)
			require.Equal(t, 2, countEvents(s.Ctx, types.EventTypeStreamExhausted))

			// leasesLeft returns how many leases a tenant's frozen stream still has.
			leasesLeft := func(tenant sdk.AccAddress) int {
				state, exists := s.stream(t, tenant, testDenom)
				if !exists {
					return 0
				}
				require.True(t, state.IsExhausted())
				return int(state.LeaseCount) //nolint:gosec // test code, a few hundred leases at most
			}

			// One block.
			s.advance(6 * time.Second)
			s.Ctx = s.Ctx.WithEventManager(sdk.NewEventManager())
			require.NoError(t, k.EndBlocker(s.Ctx))

			closedFirst := tc.first - leasesLeft(first)
			closedSecond := tc.second - leasesLeft(second)
			require.Positive(t, closedFirst, "the first stream made no progress")
			require.Positive(t, closedSecond, "the second stream made no progress behind the first")
			require.Equal(t, tc.wantFirst, closedFirst)
			require.Equal(t, tc.wantSecond, closedSecond)

			total := countEvents(s.Ctx, types.EventTypeLeaseAutoClose)
			require.Equal(t, closedFirst+closedSecond, total)
			require.LessOrEqual(t, total, budget, "the block's budget is never exceeded")
			require.Equal(t, min(tc.first+tc.second, budget), total, "the block's budget is never wasted")
			s.requireInvariants(t)
		})
	}
}

// TestAdversarial_EndBlockerDrainCannotBeStarvedByRefilledStreams tests that a
// frozen stream is drained within a bounded number of blocks even when the
// streams ahead of it in the drain queue are kept non-empty. The attack: a
// hundred tenants whose addresses sort first each keep one frozen stream alive
// by getting one new lease acknowledged into it per block (a lease joining a
// frozen stream costs them nothing: it never accrues, and its reservation is
// released when the EndBlocker closes it). If the EndBlocker always serves the
// head of the queue, those hundred streams absorb the whole budget of every
// block and the victim's lease stays ACTIVE, unpaid, forever.
//
// Sharing the budget between the streams collected in a block is not enough
// against this: with 100 frozen streams ahead of the victim, each gets a share
// of one closure, the budget is gone, and the victim (entry 101) is never
// collected. The drain therefore also rotates: when more streams are queued
// than one block can serve, the next block resumes after the last stream
// served (ExhaustedStreamsStart), which bounds the victim's wait.
func TestAdversarial_EndBlockerDrainCannotBeStarvedByRefilledStreams(t *testing.T) {
	const (
		attackers = types.MaxAutoClosesPerBlock
		blocks    = 5
	)

	s := newStreamFixture(t, testDenom)
	k := s.App.BillingKeeper

	// Attacker addresses sort before the victim's.
	attackerAddrs := make([]sdk.AccAddress, 0, attackers)
	for i := range attackers {
		addr := make([]byte, 20)
		addr[1] = byte(i + 1)
		attackerAddrs = append(attackerAddrs, sdk.AccAddress(addr))
	}
	victim := sdk.AccAddress(bytes.Repeat([]byte{0xfe}, 20))

	// Every stream starts with exactly its reservation, so two hours later all
	// of them are exhausted. Attackers hold two leases each: one for the
	// EndBlocker to close, one to keep the stream alive until the refill.
	for _, attacker := range attackerAddrs {
		s.fundTenant(t, attacker, testDenom, 2*3600)
		s.lease(t, attacker)
		s.lease(t, attacker)
	}
	s.fundTenant(t, victim, testDenom, 3600)
	victimLease := s.lease(t, victim)

	s.advance(2 * time.Hour)
	s.Ctx = s.Ctx.WithEventManager(sdk.NewEventManager())
	var key []byte
	for {
		resp := s.withdraw(t, types.MaxBatchLeaseSize, key)
		if !resp.HasMore {
			break
		}
		key = resp.NextKey
	}
	require.Equal(t, attackers+1, countEvents(s.Ctx, types.EventTypeStreamExhausted))

	// The attackers top up so they can keep creating leases.
	for _, attacker := range attackerAddrs {
		s.fundTenant(t, attacker, testDenom, 1_000_000)
	}
	s.requireInvariants(t)

	victimClosedAtBlock := 0
	for block := 1; block <= blocks; block++ {
		s.advance(6 * time.Second)
		s.Ctx = s.Ctx.WithEventManager(sdk.NewEventManager())
		require.NoError(t, k.EndBlocker(s.Ctx))
		require.LessOrEqual(t, countEvents(s.Ctx, types.EventTypeLeaseAutoClose), types.MaxAutoClosesPerBlock)

		lease, err := k.GetLease(s.Ctx, victimLease)
		require.NoError(t, err)
		if lease.State == types.LEASE_STATE_CLOSED {
			victimClosedAtBlock = block
			break
		}

		// Next block's transactions: every attacker gets one more lease
		// acknowledged into its still-frozen stream.
		for _, attacker := range attackerAddrs {
			s.lease(t, attacker)
			state, exists := s.stream(t, attacker, testDenom)
			require.True(t, exists)
			require.True(t, state.IsExhausted(), "the refill joined the frozen stream")
		}
		s.requireInvariants(t)
	}

	require.NotZero(t, victimClosedAtBlock,
		"the victim's frozen lease is still ACTIVE after %d blocks: the streams ahead of it absorbed the whole auto-close budget of every block", blocks)
}

// TestAdversarial_EndBlockerDoesNotExpireNonExpiredLeases tests that EndBlocker
// does not expire leases that haven't exceeded the timeout.
func TestAdversarial_EndBlockerDoesNotExpireNonExpiredLeases(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]

	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(100_000_000))))
	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Create a pending lease
	createResp, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
	})
	require.NoError(t, err)

	// Advance time but NOT past timeout (default 1800s)
	f.Ctx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(900 * time.Second)) // Only 900s

	err = f.App.BillingKeeper.EndBlocker(f.Ctx)
	require.NoError(t, err)

	// Lease should still be PENDING
	lease, err := f.App.BillingKeeper.GetLease(f.Ctx, createResp.LeaseUuid)
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_PENDING, lease.State, "lease should still be pending before timeout")
}

// =============================================================================
// Same-Block Multi-Operation
// =============================================================================

// TestAdversarial_CreateAndAcknowledgeSameBlock tests creating and acknowledging
// a lease in the same block. This should work since they're sequential operations.
func TestAdversarial_CreateAndAcknowledgeSameBlock(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]

	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(1_000_000))))
	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Create lease
	createResp, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
	})
	require.NoError(t, err)

	// Acknowledge in the same block (same context, no time advance)
	_, err = msgServer.AcknowledgeLease(f.Ctx, &types.MsgAcknowledgeLease{
		Sender:     providerAddr.String(),
		LeaseUuids: []string{createResp.LeaseUuid},
	})
	require.NoError(t, err)

	// Verify lease is ACTIVE
	lease, err := f.App.BillingKeeper.GetLease(f.Ctx, createResp.LeaseUuid)
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_ACTIVE, lease.State)

	// Verify counts are correct
	ca, err := f.App.BillingKeeper.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)
	require.Equal(t, uint64(1), ca.ActiveLeaseCount)
	require.Equal(t, uint64(0), ca.PendingLeaseCount)
}

// TestAdversarial_MultipleCreatesInSameBlock tests creating multiple leases in
// the same block doesn't cause reservation races.
func TestAdversarial_MultipleCreatesInSameBlock(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]

	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	// Fund enough for exactly 3 leases (each needs 3600 * 1 = 3600 reserved)
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(11000))))
	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Create 3 leases in the same block
	for i := 0; i < 3; i++ {
		_, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
			Tenant: tenant.String(),
			Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
		})
		require.NoError(t, err)
	}

	// The 4th should fail (insufficient available credit after reservations)
	_, err = msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "insufficient")

	// Verify reservation consistency
	ca, err := f.App.BillingKeeper.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)
	require.Equal(t, uint64(3), ca.PendingLeaseCount)
	// Each lease reserves 1 umfx/sec * 1 qty * 3600 duration = 3600
	expectedReserved := sdkmath.NewInt(3600 * 3)
	require.Equal(t, expectedReserved, ca.ReservedAmounts.AmountOf(testDenom),
		"reserved should be exactly 3 * 3600 = 10800")
}

// TestAdversarial_SubSecondJoinAndLeave pins how a stream bills leases that
// join or leave it between two whole seconds. Accrual is exact: a stream owes
// remainder + rate × elapsed nanoseconds, pays the whole units of that, and
// carries what is left (always less than one unit) in its remainder. Every
// settlement moves last_settled_at to the settlement time, so a lease pays for
// exactly the time between its acknowledgement and its close, and what was
// paid in total is always the floor of the exact accrual.
func TestAdversarial_SubSecondJoinAndLeave(t *testing.T) {
	const nanosPerUnit = int64(time.Second) // a rate of 1/s accrues one unit per 1e9 ns

	s := newStreamFixture(t, testDenom)
	tenant := s.TestAccs[0]
	s.fundTenant(t, tenant, testDenom, 1_000_000)
	start := s.Ctx.BlockTime()

	closeLease := func(uuid string) sdk.Coins {
		resp, err := s.msgServer.CloseLease(s.Ctx, &types.MsgCloseLease{Sender: tenant.String(), LeaseUuids: []string{uuid}})
		require.NoError(t, err)
		return resp.TotalSettledAmounts
	}

	// exact is what the stream has accrued since it was created, in billionths
	// of a unit, computed independently: rate × nanoseconds over every interval.
	exact := int64(0)
	advance := func(d time.Duration, rate int64) {
		s.advance(d)
		exact += rate * int64(d)
	}
	// requireSettled checks the stream right after a settlement at the current
	// block time: everything accrued so far is either paid or in the remainder.
	requireSettled := func(wantRate int64, wantPaid, wantRemainder int64) {
		t.Helper()
		require.Equal(t, wantPaid, exact/nanosPerUnit, "the test's own arithmetic")
		require.Equal(t, wantRemainder, exact%nanosPerUnit, "the test's own arithmetic")

		state, exists := s.stream(t, tenant, testDenom)
		require.True(t, exists)
		require.Equal(t, sdkmath.NewInt(wantRate), state.Rate)
		require.Equal(t, s.Ctx.BlockTime(), state.LastSettledAt, "the clock always moves to the settlement time")
		require.Equal(t, sdkmath.NewInt(wantRemainder).String(), state.Remainder.String())
		require.Equal(t, sdkmath.NewInt(wantPaid).String(), s.payout(testDenom).String(),
			"paid in total must be the floor of the exact accrual")
		require.Equal(t, sdkmath.NewInt(1_000_000-wantPaid), s.credit(tenant, testDenom))
	}
	coins := func(amount int64) sdk.Coins {
		return sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(amount)))
	}

	// 0.0s: lease A creates the stream and stays until the end.
	a := s.lease(t, tenant)
	requireSettled(1, 0, 0)

	// 1.5s: lease B joins. A alone accrued 1.5: 1 is paid, 0.5 is carried.
	advance(1500*time.Millisecond, 1)
	b := s.lease(t, tenant)
	require.Equal(t, start.Add(1500*time.Millisecond), s.Ctx.BlockTime())
	requireSettled(2, 1, 500_000_000)

	// 2.0s: B leaves after half a second. A and B accrued 2 × 0.5 = 1, plus the
	// carried 0.5: 1 is paid, 0.5 is carried. B cost the tenant exactly its own
	// half second (0.5 of a unit), not the half second before it joined.
	advance(500*time.Millisecond, 2)
	require.Equal(t, coins(1), closeLease(b))
	requireSettled(1, 2, 500_000_000)

	// 2.0s: lease C joins in the same block. No time has passed: nothing changes
	// but the rate.
	c := s.lease(t, tenant)
	requireSettled(2, 2, 500_000_000)

	// 2.9s: C leaves. A and C accrued 2 × 0.9 = 1.8, plus the carried 0.5: 2 are
	// paid, 0.3 is carried. C's 0.9s is billed although it never completed a
	// whole second.
	advance(900*time.Millisecond, 2)
	require.Equal(t, coins(2), closeLease(c))
	requireSettled(1, 4, 300_000_000)

	// 3.0s: a withdrawal finds 0.3 + 0.1 = 0.4: nothing is payable, but the
	// clock still moves and the fraction is kept.
	advance(100*time.Millisecond, 1)
	resp := s.withdraw(t, 0, nil)
	require.True(t, resp.TotalAmounts.IsZero())
	require.Zero(t, resp.WithdrawalCount)
	requireSettled(1, 4, 400_000_000)

	// 3.6s: 0.4 + 0.6 completes the fifth unit exactly. In total A ran 3.6s,
	// B 0.5s and C 0.9s: 5.0 units, all paid, nothing carried.
	advance(600*time.Millisecond, 1)
	require.Equal(t, coins(1), s.withdraw(t, 0, nil).TotalAmounts)
	requireSettled(1, 5, 0)
	s.requireInvariants(t)

	// 3.85s: A leaves and takes the stream with it. The quarter unit accrued
	// since the last settlement is less than one unit: it is the only thing a
	// stream ever drops, once, when it ends.
	advance(250*time.Millisecond, 1)
	require.True(t, closeLease(a).IsZero())
	_, exists := s.stream(t, tenant, testDenom)
	require.False(t, exists)
	require.Equal(t, int64(5), exact/nanosPerUnit)
	require.Equal(t, sdkmath.NewInt(5), s.payout(testDenom))
	require.Equal(t, sdkmath.NewInt(1_000_000-5), s.credit(tenant, testDenom))
	s.requireInvariants(t)
}

// =============================================================================
// Auto-Close Timing Edge Cases
// =============================================================================

// TestAdversarial_AutoCloseAtExactExhaustionPoint tests the freeze decision
// around the point where the accrued amount equals the credit (GTE check
// boundary): one second before it the stream stays healthy, at it and after it
// the stream freezes and the EndBlocker closes the lease.
func TestAdversarial_AutoCloseAtExactExhaustionPoint(t *testing.T) {
	// Credit of exactly one hour at 1/s (also the reservation of the lease).
	const funded = 3600

	tests := []struct {
		name       string
		elapsed    time.Duration
		wantPaid   int64
		wantFrozen bool
	}{
		{name: "one second before the credit runs out", elapsed: 3599 * time.Second, wantPaid: 3599},
		{name: "just short of the last second", elapsed: 3600*time.Second - time.Nanosecond, wantPaid: 3599},
		{name: "accrued equals the credit exactly (GTE)", elapsed: 3600 * time.Second, wantPaid: 3600, wantFrozen: true},
		{name: "one second after the credit ran out", elapsed: 3601 * time.Second, wantPaid: 3600, wantFrozen: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newStreamFixture(t, testDenom)
			tenant := s.TestAccs[0]
			s.fundTenant(t, tenant, testDenom, funded)
			uuid := s.lease(t, tenant)

			s.advance(tc.elapsed)
			s.Ctx = s.Ctx.WithEventManager(sdk.NewEventManager())
			resp := s.withdraw(t, 0, nil)
			require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(tc.wantPaid))), resp.TotalAmounts)
			require.Equal(t, sdkmath.NewInt(funded-tc.wantPaid).String(), s.credit(tenant, testDenom).String())

			state, exists := s.stream(t, tenant, testDenom)
			require.True(t, exists)
			require.Equal(t, tc.wantFrozen, state.IsExhausted())
			wantEvents := 0
			if tc.wantFrozen {
				wantEvents = 1
			}
			require.Equal(t, wantEvents, countEvents(s.Ctx, types.EventTypeStreamExhausted))
			s.requireInvariants(t)

			// The EndBlocker closes the lease if and only if its stream froze.
			require.NoError(t, s.App.BillingKeeper.EndBlocker(s.Ctx))
			lease, err := s.App.BillingKeeper.GetLease(s.Ctx, uuid)
			require.NoError(t, err)
			if tc.wantFrozen {
				require.Equal(t, types.LEASE_STATE_CLOSED, lease.State)
				require.Equal(t, types.ClosureReasonCreditExhausted, lease.ClosureReason)
				require.NotNil(t, lease.ClosedAt)
				require.Equal(t, s.Ctx.BlockTime(), *lease.ClosedAt)
			} else {
				require.Equal(t, types.LEASE_STATE_ACTIVE, lease.State)
			}
			require.Equal(t, wantEvents, countEvents(s.Ctx, types.EventTypeLeaseAutoClose))
			s.requireInvariants(t)
		})
	}
}

// TestAdversarial_StreamSettledInTheFuture tests a stream whose last_settled_at
// is ahead of the block time (data corruption: the handlers never produce it).
// Such a stream is rejected at genesis; if it exists anyway it must not pay a
// negative or bogus amount, must not move further into the future, and must
// resume normal billing once block time catches up.
func TestAdversarial_StreamSettledInTheFuture(t *testing.T) {
	s := newStreamFixture(t, testDenom)
	k := s.App.BillingKeeper
	tenant := s.TestAccs[0]
	s.fundTenant(t, tenant, testDenom, 1_000_000)
	uuid := s.lease(t, tenant)
	now := s.Ctx.BlockTime()

	// Corrupt the stream: settled one hour in the future.
	key := keeper.NewStreamKey(s.provider.Uuid, tenant, testDenom)
	state, err := k.Streams.Get(s.Ctx, key)
	require.NoError(t, err)
	future := now.Add(time.Hour)
	state.LastSettledAt = future
	require.NoError(t, k.Streams.Set(s.Ctx, key, state))

	// Genesis import refuses the corrupted state.
	gs := k.ExportGenesis(s.Ctx)
	err = gs.ValidateWithBlockTime(now)
	require.ErrorIs(t, err, types.ErrInvalidLease)
	require.ErrorContains(t, err, "in the future")
	scratch, _ := s.Ctx.CacheContext()
	require.ErrorIs(t, k.InitGenesis(scratch, gs), types.ErrInvalidLease)

	// Until block time catches up, settling pays nothing and leaves the stream alone.
	s.advance(30 * time.Minute)
	require.True(t, s.withdraw(t, 0, nil).TotalAmounts.IsZero())
	state, _ = s.stream(t, tenant, testDenom)
	require.Equal(t, future, state.LastSettledAt)
	require.False(t, state.IsExhausted())
	require.Equal(t, sdkmath.NewInt(1_000_000), s.credit(tenant, testDenom))

	// Closing the lease in that window pays nothing either, and does not fail.
	second := s.lease(t, tenant)
	resp, err := s.msgServer.CloseLease(s.Ctx, &types.MsgCloseLease{Sender: tenant.String(), LeaseUuids: []string{second}})
	require.NoError(t, err)
	require.True(t, resp.TotalSettledAmounts.IsZero())

	// Once block time passes it, the stream bills from last_settled_at only.
	s.advance(30*time.Minute + 10*time.Second)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(10))), s.withdraw(t, 0, nil).TotalAmounts)
	lease, err := k.GetLease(s.Ctx, uuid)
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_ACTIVE, lease.State)
	s.requireInvariants(t)
}

// TestAdversarial_NonActiveLeasesAreNeverBilled tests that a lease that is not
// ACTIVE (pending, rejected, cancelled, expired or closed) has no stream, is
// never charged, and is never picked up by the EndBlocker's auto-close.
func TestAdversarial_NonActiveLeasesAreNeverBilled(t *testing.T) {
	s := newStreamFixture(t, testDenom)
	k := s.App.BillingKeeper
	tenant := s.TestAccs[0]
	s.fundTenant(t, tenant, testDenom, 1_000_000)

	create := func() string {
		resp, err := s.msgServer.CreateLease(s.Ctx, &types.MsgCreateLease{
			Tenant: tenant.String(),
			Items:  []types.LeaseItemInput{{SkuUuid: s.sku.Uuid, Quantity: 1}},
		})
		require.NoError(t, err)
		return resp.LeaseUuid
	}

	rejected := create()
	_, err := s.msgServer.RejectLease(s.Ctx, &types.MsgRejectLease{Sender: s.providerAddr.String(), LeaseUuids: []string{rejected}})
	require.NoError(t, err)

	cancelled := create()
	_, err = s.msgServer.CancelLease(s.Ctx, &types.MsgCancelLease{Tenant: tenant.String(), LeaseUuids: []string{cancelled}})
	require.NoError(t, err)

	// Closed in the block it was acknowledged in: it never ran for a second.
	closed := s.lease(t, tenant)
	_, err = s.msgServer.CloseLease(s.Ctx, &types.MsgCloseLease{Sender: tenant.String(), LeaseUuids: []string{closed}})
	require.NoError(t, err)

	expired := create()
	// Past the pending timeout (default 1800s): the EndBlocker expires it.
	s.advance(1801 * time.Second)
	require.NoError(t, k.EndBlocker(s.Ctx))

	pending := create()

	wantStates := map[string]types.LeaseState{
		pending:   types.LEASE_STATE_PENDING,
		rejected:  types.LEASE_STATE_REJECTED,
		cancelled: types.LEASE_STATE_REJECTED, // a cancellation is recorded as a rejection
		closed:    types.LEASE_STATE_CLOSED,
		expired:   types.LEASE_STATE_EXPIRED,
	}
	requireStates := func() {
		t.Helper()
		for uuid, want := range wantStates {
			lease, err := k.GetLease(s.Ctx, uuid)
			require.NoError(t, err)
			require.Equal(t, want, lease.State, "lease %s", uuid)
		}
	}
	requireStates()

	// None of them has a stream.
	streams, err := k.GetAllStreams(s.Ctx)
	require.NoError(t, err)
	require.Empty(t, streams)

	// Time passes (short of the pending timeout), the provider withdraws and
	// the EndBlocker runs: nobody is charged and nothing changes state.
	s.advance(1000 * time.Second)
	s.Ctx = s.Ctx.WithEventManager(sdk.NewEventManager())
	resp := s.withdraw(t, 0, nil)
	require.True(t, resp.TotalAmounts.IsZero())
	require.Zero(t, resp.WithdrawalCount)
	require.NoError(t, k.EndBlocker(s.Ctx))
	require.Zero(t, countEvents(s.Ctx, types.EventTypeLeaseAutoClose))
	require.Zero(t, countEvents(s.Ctx, "transfer"))

	requireStates()
	require.True(t, s.payout(testDenom).IsZero())
	require.Equal(t, sdkmath.NewInt(1_000_000), s.credit(tenant, testDenom))
	s.requireInvariants(t)
}
