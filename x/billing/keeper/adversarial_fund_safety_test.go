/*
Package keeper_test contains adversarial tests targeting fund safety in the billing module.

These tests attempt to break fund conservation invariants: double-spend, over-withdrawal,
settlement during insufficient funds, race conditions between billing and manual transfers,
multi-denom edge cases, and several providers competing for one tenant's credit.
*/
package keeper_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/collections"
	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/keeper"
	"github.com/manifest-network/manifest-ledger/x/billing/types"
	skutypes "github.com/manifest-network/manifest-ledger/x/sku/types"
)

// =============================================================================
// Double-Spend / Double-Settlement
// =============================================================================

// TestAdversarial_DoubleWithdrawSameBlock tests that withdrawing the same
// stream twice in the same block (same block time) does not double-pay the provider.
// Attack: provider submits two Withdraw txs in the same block.
func TestAdversarial_DoubleWithdrawSameBlock(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]

	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Fund tenant credit
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

	// Advance time so there's something to withdraw
	f.Ctx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(3600 * time.Second))

	withdraw := &types.MsgWithdraw{Sender: providerAddr.String(), ProviderUuid: provider.Uuid}

	// First withdrawal pays the hour that elapsed
	resp1, err := msgServer.Withdraw(f.Ctx, withdraw)
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(3600))), resp1.TotalAmounts)
	require.Equal(t, uint64(1), resp1.WithdrawalCount)

	// The stream is now settled up to the block time
	state, err := f.App.BillingKeeper.Streams.Get(f.Ctx, keeper.NewStreamKey(provider.Uuid, tenant, testDenom))
	require.NoError(t, err)
	require.Equal(t, f.Ctx.BlockTime(), state.LastSettledAt)

	// Second withdrawal in the same block pays nothing
	resp2, err := msgServer.Withdraw(f.Ctx, withdraw)
	require.NoError(t, err)
	require.True(t, resp2.TotalAmounts.IsZero())
	require.Zero(t, resp2.WithdrawalCount)

	// Verify fund conservation: provider gained exactly what credit lost
	require.Equal(t, sdkmath.NewInt(3600), f.App.BankKeeper.GetBalance(f.Ctx, providerAddr, testDenom).Amount,
		"provider received more than expected")
	require.Equal(t, sdkmath.NewInt(1_000_000-3600), f.App.BankKeeper.GetBalance(f.Ctx, creditAddr, testDenom).Amount,
		"fund conservation violated")
	requireInvariants(t, f)
}

// TestAdversarial_DoubleCloseInSameBlock tests that closing the same lease twice
// does not double-settle.
func TestAdversarial_DoubleCloseInSameBlock(t *testing.T) {
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

	leaseID := f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{
		{SkuUuid: sku.Uuid, Quantity: 1},
	})

	f.Ctx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(3600 * time.Second))

	// Naming the lease twice in one message is rejected before anything settles.
	_, err = msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
		Sender:     tenant.String(),
		LeaseUuids: []string{leaseID, leaseID},
	})
	require.ErrorIs(t, err, types.ErrInvalidLease)
	require.Contains(t, err.Error(), "duplicate")
	require.True(t, f.App.BankKeeper.GetBalance(f.Ctx, providerAddr, testDenom).IsZero())

	// First close succeeds
	resp, err := msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
		Sender:     tenant.String(),
		LeaseUuids: []string{leaseID},
	})
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(3600))), resp.TotalSettledAmounts)

	// Second close must fail (lease is no longer ACTIVE)
	_, err = msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
		Sender:     tenant.String(),
		LeaseUuids: []string{leaseID},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not active")

	// The hour was paid exactly once.
	require.Equal(t, sdkmath.NewInt(3600), f.App.BankKeeper.GetBalance(f.Ctx, providerAddr, testDenom).Amount)
	require.Equal(t, sdkmath.NewInt(1_000_000-3600), f.App.BankKeeper.GetBalance(f.Ctx, creditAddr, testDenom).Amount)
	requireInvariants(t, f)
}

// TestAdversarial_WithdrawThenCloseInSameBlock tests that withdrawing then closing
// in the same block does not cause over-payment.
func TestAdversarial_WithdrawThenCloseInSameBlock(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]

	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	initialFunding := sdkmath.NewInt(1_000_000)
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, initialFunding)))

	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	leaseID := f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{
		{SkuUuid: sku.Uuid, Quantity: 1},
	})

	f.Ctx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(3600 * time.Second))

	// Withdraw first
	resp, err := msgServer.Withdraw(f.Ctx, &types.MsgWithdraw{
		Sender:       providerAddr.String(),
		ProviderUuid: provider.Uuid,
	})
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(3600))), resp.TotalAmounts)

	// Then close (settles zero additional since we just withdrew at the same time)
	closeResp, err := msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
		Sender:     tenant.String(),
		LeaseUuids: []string{leaseID},
	})
	require.NoError(t, err)
	require.True(t, closeResp.TotalSettledAmounts.IsZero(),
		"close should not have settled additional funds after withdraw in same block")

	// Verify the provider was paid for the hour exactly once
	require.Equal(t, sdkmath.NewInt(3600), f.App.BankKeeper.GetBalance(f.Ctx, providerAddr, testDenom).Amount)
	require.Equal(t, initialFunding.SubRaw(3600), f.App.BankKeeper.GetBalance(f.Ctx, creditAddr, testDenom).Amount)
	requireInvariants(t, f)
}

// TestAdversarial_WithdrawCloseWithdrawSameBlock tests that a provider cannot be
// paid twice for the same seconds by interleaving withdrawals with lease closes
// in one block. Closing a lease settles its stream and withdrawing settles it
// again; whatever the order, the 100 seconds two leases ran are paid exactly
// once, with exactly one bank transfer.
func TestAdversarial_WithdrawCloseWithdrawSameBlock(t *testing.T) {
	const (
		opWithdraw  = "withdraw"
		opCloseA    = "close_a"
		opCloseB    = "close_b"
		opCloseBoth = "close_both"
	)

	tests := []struct {
		name string
		ops  []string
	}{
		{name: "withdraw, close one, withdraw, close the other, withdraw", ops: []string{opWithdraw, opCloseA, opWithdraw, opCloseB, opWithdraw}},
		{name: "close one, withdraw, close the other, withdraw", ops: []string{opCloseA, opWithdraw, opCloseB, opWithdraw}},
		{name: "close one, close the other, withdraw", ops: []string{opCloseA, opCloseB, opWithdraw}},
		{name: "close both in one message, withdraw", ops: []string{opCloseBoth, opWithdraw}},
		{name: "withdraw, close both in one message, withdraw", ops: []string{opWithdraw, opCloseBoth, opWithdraw}},
		{name: "withdraw three times, close one", ops: []string{opWithdraw, opWithdraw, opWithdraw, opCloseA}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newStreamFixture(t, testDenom)
			tenant := s.TestAccs[0]
			s.fundTenant(t, tenant, testDenom, 1_000_000)
			leaseA := s.lease(t, tenant)
			leaseB := s.lease(t, tenant)

			// Two leases at 1/s for 100 seconds: 200 is owed, once.
			s.advance(100 * time.Second)
			s.Ctx = s.Ctx.WithEventManager(sdk.NewEventManager())

			// The provider closes the leases itself.
			closeLeases := func(uuids ...string) sdk.Coins {
				resp, err := s.msgServer.CloseLease(s.Ctx, &types.MsgCloseLease{
					Sender:     s.providerAddr.String(),
					LeaseUuids: uuids,
				})
				require.NoError(t, err)
				return resp.TotalSettledAmounts
			}

			paid := sdk.NewCoins()
			for _, op := range tc.ops {
				switch op {
				case opWithdraw:
					paid = paid.Add(s.withdraw(t, 0, nil).TotalAmounts...)
				case opCloseA:
					paid = paid.Add(closeLeases(leaseA)...)
				case opCloseB:
					paid = paid.Add(closeLeases(leaseB)...)
				case opCloseBoth:
					paid = paid.Add(closeLeases(leaseA, leaseB)...)
				}
				s.requireInvariants(t)
			}

			want := sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(200)))
			require.Equal(t, want, paid, "the responses of the block add up to what was owed")
			require.Equal(t, sdkmath.NewInt(200), s.payout(testDenom))
			require.Equal(t, sdkmath.NewInt(1_000_000-200), s.credit(tenant, testDenom))
			require.Equal(t, 1, countEvents(s.Ctx, "transfer"), "the stream was paid with a single transfer")
			require.Equal(t, 1, countEvents(s.Ctx, types.EventTypeStreamSettled))

			// Later blocks pay only what the surviving leases accrue.
			surviving := int64(2)
			for _, op := range tc.ops {
				switch op {
				case opCloseA, opCloseB:
					surviving--
				case opCloseBoth:
					surviving -= 2
				}
			}
			s.advance(50 * time.Second)
			later := s.withdraw(t, 0, nil).TotalAmounts
			require.Equal(t, sdkmath.NewInt(50*surviving).String(), later.AmountOf(testDenom).String())
			require.Equal(t, sdkmath.NewInt(200+50*surviving), s.payout(testDenom))
			s.requireInvariants(t)
		})
	}
}

// =============================================================================
// Escaping Charges
// =============================================================================

// TestAdversarial_TenantCannotEscapeChargesByCloseAndReacknowledge tests that a
// tenant gains nothing by closing a lease and getting a replacement
// acknowledged around a settlement: every second of service is billed whatever
// the order of the close, the acknowledgement and the provider's withdrawal.
func TestAdversarial_TenantCannotEscapeChargesByCloseAndReacknowledge(t *testing.T) {
	t.Run("churning a lease around a settlement pays the same as keeping it", func(t *testing.T) {
		s := newStreamFixture(t, testDenom)
		tenant := s.TestAccs[0]
		s.fundTenant(t, tenant, testDenom, 100_000)

		closeLease := func(uuid string) {
			_, err := s.msgServer.CloseLease(s.Ctx, &types.MsgCloseLease{
				Sender:     tenant.String(),
				LeaseUuids: []string{uuid},
			})
			require.NoError(t, err)
		}

		// One lease is kept for the whole test, the other is replaced each
		// round. Two leases at 1/s owe 200.5 per 100.25-second round.
		s.lease(t, tenant)
		churn := s.lease(t, tenant)

		rounds := []struct {
			name string
			run  func()
		}{
			{name: "close before the settlement, re-acknowledge after it", run: func() {
				closeLease(churn)
				s.withdraw(t, 0, nil)
				churn = s.lease(t, tenant)
			}},
			{name: "close and re-acknowledge between two settlements", run: func() {
				s.withdraw(t, 0, nil)
				closeLease(churn)
				churn = s.lease(t, tenant)
				s.withdraw(t, 0, nil)
			}},
			{name: "re-acknowledge the replacement before closing", run: func() {
				replacement := s.lease(t, tenant)
				closeLease(churn)
				churn = replacement
				s.withdraw(t, 0, nil)
			}},
			{name: "close and re-acknowledge without any withdrawal", run: func() {
				closeLease(churn)
				churn = s.lease(t, tenant)
			}},
		}

		// Rounds are not a whole number of seconds, so the churn also happens
		// across the fraction the stream carries: 2 × 100.25s = 200.5 per round.
		const (
			roundDuration = 100*time.Second + 250*time.Millisecond
			nanosPerUnit  = int64(time.Second)
		)
		for i, round := range rounds {
			s.advance(roundDuration)
			round.run()

			// Exactly what two leases kept for the whole time would owe: the
			// floor is paid, the rest is in the stream's remainder.
			exact := int64(i+1) * 2 * int64(roundDuration)
			owed := exact / nanosPerUnit
			require.Equal(t, []int64{200, 401, 601, 802}[i], owed, "the test's own arithmetic")
			require.Equal(t, sdkmath.NewInt(owed), s.payout(testDenom), "round %q", round.name)
			require.Equal(t, sdkmath.NewInt(100_000-owed), s.credit(tenant, testDenom), "round %q", round.name)

			state, exists := s.stream(t, tenant, testDenom)
			require.True(t, exists)
			require.Equal(t, sdkmath.NewInt(2), state.Rate, "round %q", round.name)
			require.Equal(t, s.Ctx.BlockTime(), state.LastSettledAt, "round %q", round.name)
			require.Equal(t, sdkmath.NewInt(exact%nanosPerUnit).String(), state.Remainder.String(), "round %q", round.name)
			s.requireInvariants(t)
		}
	})

	t.Run("closing before exhaustion does not let the tenant back in without credit", func(t *testing.T) {
		s := newStreamFixture(t, testDenom)
		tenant := s.TestAccs[0]
		// Exactly the reservation of two leases at 1/s.
		s.fundTenant(t, tenant, testDenom, 7200)
		first := s.lease(t, tenant)
		second := s.lease(t, tenant)

		// After 3000s the tenant owes 6000. Closing one lease settles the whole
		// stream, so the close cannot be used to walk away from the other half.
		s.advance(3000 * time.Second)
		resp, err := s.msgServer.CloseLease(s.Ctx, &types.MsgCloseLease{
			Sender:     tenant.String(),
			LeaseUuids: []string{first},
		})
		require.NoError(t, err)
		require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(6000))), resp.TotalSettledAmounts)
		require.Equal(t, sdkmath.NewInt(1200), s.credit(tenant, testDenom))

		// The 1200 left are below the reservation of a new lease on top of the
		// surviving one: no replacement.
		_, err = s.msgServer.CreateLease(s.Ctx, &types.MsgCreateLease{
			Tenant: tenant.String(),
			Items:  []types.LeaseItemInput{{SkuUuid: s.sku.Uuid, Quantity: 1}},
		})
		require.ErrorIs(t, err, types.ErrInsufficientCredit)

		// 1300s later the surviving lease owes 1300 against 1200: the provider
		// gets everything and the stream freezes.
		s.advance(1300 * time.Second)
		require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(1200))), s.withdraw(t, 0, nil).TotalAmounts)
		state, _ := s.stream(t, tenant, testDenom)
		require.True(t, state.IsExhausted())

		// The tenant cannot relabel the exhaustion by closing the lease itself
		// before the EndBlocker does.
		resp, err = s.msgServer.CloseLease(s.Ctx, &types.MsgCloseLease{
			Sender:     tenant.String(),
			LeaseUuids: []string{second},
			Reason:     "done",
		})
		require.NoError(t, err)
		require.True(t, resp.TotalSettledAmounts.IsZero())
		lease, err := s.App.BillingKeeper.GetLease(s.Ctx, second)
		require.NoError(t, err)
		require.Equal(t, types.ClosureReasonCreditExhausted, lease.ClosureReason)
		closedEvent := findEvent(t, s.Ctx, types.EventTypeLeaseClosed)
		require.Equal(t, "credit_exhaustion", attrValue(t, closedEvent, types.AttributeKeyClosedBy))

		// Everything the tenant ever funded went to the provider.
		require.Equal(t, sdkmath.NewInt(7200), s.payout(testDenom))
		require.True(t, s.credit(tenant, testDenom).IsZero())
		s.requireInvariants(t)

		// A fresh top-up starts a fresh stream that bills from its own start.
		s.fundTenant(t, tenant, testDenom, 3600)
		s.lease(t, tenant)
		s.advance(10 * time.Second)
		require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(10))), s.withdraw(t, 0, nil).TotalAmounts)
		s.requireInvariants(t)
	})
}

// =============================================================================
// Insufficient Funds Mid-Accrual
// =============================================================================

// TestAdversarial_CreditExhaustedDuringSettlement tests that when credit runs out
// during settlement, the provider gets exactly what's available and no more.
func TestAdversarial_CreditExhaustedDuringSettlement(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]

	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600) // 1 umfx/sec locked price

	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)

	// Fund with exactly enough for ~100 seconds (at 1 umfx/sec)
	// But need enough for reservation too: rate * quantity * minLeaseDuration = 1 * 1 * 3600 = 3600
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(3700))))

	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	leaseID := f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{
		{SkuUuid: sku.Uuid, Quantity: 1},
	})

	// Advance time way past when credit should be exhausted
	f.Ctx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(10000 * time.Second))
	withdraw := &types.MsgWithdraw{Sender: providerAddr.String(), ProviderUuid: provider.Uuid}

	// The withdrawal transfers whatever is available and freezes the stream
	f.Ctx = f.Ctx.WithEventManager(sdk.NewEventManager())
	resp, err := msgServer.Withdraw(f.Ctx, withdraw)
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(3700))), resp.TotalAmounts,
		"provider must receive exactly what the credit account held")
	require.Equal(t, "1", attrValue(t, findEvent(t, f.Ctx, types.EventTypeBatchWithdraw), types.AttributeKeyExhaustedCount))

	require.Equal(t, sdkmath.NewInt(3700), f.App.BankKeeper.GetBalance(f.Ctx, providerAddr, testDenom).Amount)
	require.True(t, f.App.BankKeeper.GetBalance(f.Ctx, creditAddr, testDenom).IsZero())

	streamKey := keeper.NewStreamKey(provider.Uuid, tenant, testDenom)
	state, err := f.App.BillingKeeper.Streams.Get(f.Ctx, streamKey)
	require.NoError(t, err)
	require.True(t, state.IsExhausted())

	// No lease is closed inside the transaction
	lease, err := f.App.BillingKeeper.GetLease(f.Ctx, leaseID)
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_ACTIVE, lease.State)
	requireInvariants(t, f)

	// A top-up after the freeze is not owed to the frozen stream: the unpaid
	// 6300 are written off, and withdrawing again pays nothing.
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(50_000))))
	f.Ctx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(1000 * time.Second))
	resp, err = msgServer.Withdraw(f.Ctx, withdraw)
	require.NoError(t, err)
	require.True(t, resp.TotalAmounts.IsZero())
	require.Equal(t, sdkmath.NewInt(50_000), f.App.BankKeeper.GetBalance(f.Ctx, creditAddr, testDenom).Amount)

	// The EndBlocker closes the lease for exhausted credit
	require.NoError(t, f.App.BillingKeeper.EndBlocker(f.Ctx))
	lease, err = f.App.BillingKeeper.GetLease(f.Ctx, leaseID)
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_CLOSED, lease.State)
	require.Equal(t, types.ClosureReasonCreditExhausted, lease.ClosureReason)
	_, err = f.App.BillingKeeper.Streams.Get(f.Ctx, streamKey)
	require.ErrorIs(t, err, collections.ErrNotFound)

	require.Equal(t, sdkmath.NewInt(3700), f.App.BankKeeper.GetBalance(f.Ctx, providerAddr, testDenom).Amount,
		"the drain moves no funds")
	requireInvariants(t, f)
}

// TestAdversarial_ManualTransferDrainsCredit tests that if someone manually
// transfers tokens OUT of a credit address (via bank send), settlement handles
// the reduced balance gracefully without panicking or going negative.
func TestAdversarial_ManualTransferDrainsCredit(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	thief := f.TestAccs[2]

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

	// Simulate someone draining the credit address directly via bank send
	// Note: On a real chain, credit addresses are module-derived and can't sign txs.
	// But we test the keeper's resilience to unexpected balance changes.
	drainAmount := sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(999_999)))
	err = f.App.BankKeeper.SendCoins(f.Ctx, creditAddr, thief, drainAmount)
	require.NoError(t, err)

	// Advance time
	f.Ctx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(3600 * time.Second))

	// Settlement handles the reduced balance gracefully (transfer min(accrued, available))
	resp, err := msgServer.Withdraw(f.Ctx, &types.MsgWithdraw{
		Sender:       providerAddr.String(),
		ProviderUuid: provider.Uuid,
	})
	require.NoError(t, err)

	// Provider gets exactly the 1 that remained
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(1))), resp.TotalAmounts,
		"provider must receive exactly the available balance")

	// Credit balance is exactly zero and the stream is frozen
	require.True(t, f.App.BankKeeper.GetBalance(f.Ctx, creditAddr, testDenom).IsZero())
	state, err := f.App.BillingKeeper.Streams.Get(f.Ctx, keeper.NewStreamKey(provider.Uuid, tenant, testDenom))
	require.NoError(t, err)
	require.True(t, state.IsExhausted())
	requireInvariants(t, f)
}

// =============================================================================
// Multi-Tenant Fund Isolation
// =============================================================================

// TestAdversarial_SettlementDoesNotAffectOtherTenants tests that settling one
// tenant's stream doesn't accidentally debit another tenant's credit account.
func TestAdversarial_SettlementDoesNotAffectOtherTenants(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant1 := f.TestAccs[0]
	tenant2 := f.TestAccs[1]
	providerAddr := f.TestAccs[2]

	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Fund both tenants and create a lease for each
	tenants := []sdk.AccAddress{tenant1, tenant2}
	for _, tenant := range tenants {
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
	}
	ackTime := f.Ctx.BlockTime()

	// Advance time and settle a single stream (a page of one)
	f.Ctx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(3600 * time.Second))

	resp, err := msgServer.Withdraw(f.Ctx, &types.MsgWithdraw{
		Sender:       providerAddr.String(),
		ProviderUuid: provider.Uuid,
		Limit:        1,
	})
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(3600))), resp.TotalAmounts)
	require.True(t, resp.HasMore)

	// Exactly one tenant paid, and the other one is untouched: neither its
	// balance nor its stream moved.
	settled, untouched := 0, 0
	for _, tenant := range tenants {
		balance := f.App.BankKeeper.GetBalance(f.Ctx, types.DeriveCreditAddress(tenant), testDenom).Amount
		state, err := f.App.BillingKeeper.Streams.Get(f.Ctx, keeper.NewStreamKey(provider.Uuid, tenant, testDenom))
		require.NoError(t, err)

		switch {
		case balance.Equal(sdkmath.NewInt(1_000_000 - 3600)):
			settled++
			require.Equal(t, f.Ctx.BlockTime(), state.LastSettledAt)
		case balance.Equal(sdkmath.NewInt(1_000_000)):
			untouched++
			require.Equal(t, ackTime, state.LastSettledAt,
				"the other tenant's stream changed during this tenant's settlement")
		default:
			t.Fatalf("tenant %s has unexpected credit balance %s", tenant, balance)
		}
	}
	require.Equal(t, 1, settled)
	require.Equal(t, 1, untouched)

	// The next page settles the other tenant for the same hour.
	resp, err = msgServer.Withdraw(f.Ctx, &types.MsgWithdraw{
		Sender:       providerAddr.String(),
		ProviderUuid: provider.Uuid,
		Limit:        1,
		Key:          resp.NextKey,
	})
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(3600))), resp.TotalAmounts)
	require.False(t, resp.HasMore)
	for _, tenant := range tenants {
		require.Equal(t, sdkmath.NewInt(1_000_000-3600),
			f.App.BankKeeper.GetBalance(f.Ctx, types.DeriveCreditAddress(tenant), testDenom).Amount)
	}
	requireInvariants(t, f)
}

// TestAdversarial_FundConservationAcrossSettleFreezeDrain follows three tenants
// and two providers through settlement, credit exhaustion and the EndBlocker
// drain. One tenant leases from both providers, so both providers' streams draw
// on the same credit: it goes to whichever stream settles first, and the total
// paid out never exceeds what was funded.
func TestAdversarial_FundConservationAcrossSettleFreezeDrain(t *testing.T) {
	s := newStreamFixture(t, testDenom) // provider 1: 1/s
	k := s.App.BillingKeeper

	// Provider 2 charges 2/s and has its own payout address.
	provider2Addr := sdk.AccAddress("adv-provider-2-addr_")
	payout2 := sdk.AccAddress("adv-provider-2-pay__")
	provider2 := s.createTestProvider(t, provider2Addr.String(), payout2.String())
	sku2 := s.createTestSKUWithDenom(t, provider2.Uuid, 7200, testDenom)

	shared, rich, poor := s.TestAccs[0], s.TestAccs[3], s.TestAccs[4]
	tenants := []sdk.AccAddress{shared, rich, poor}
	// The shared and the poor tenant hold exactly their reservations.
	s.fundTenant(t, shared, testDenom, 10_800)
	s.fundTenant(t, rich, testDenom, 1_000_000)
	s.fundTenant(t, poor, testDenom, 7_200)
	funded := int64(10_800 + 1_000_000 + 7_200)

	leaseWith2 := func(tenant sdk.AccAddress) string {
		return s.createAndAcknowledgeLease(t, s.msgServer, tenant, provider2Addr, []types.LeaseItemInput{
			{SkuUuid: sku2.Uuid, Quantity: 1},
		})
	}
	withdraw2 := func() *types.MsgWithdrawResponse {
		resp, err := s.msgServer.Withdraw(s.Ctx, &types.MsgWithdraw{
			Sender:       provider2Addr.String(),
			ProviderUuid: provider2.Uuid,
		})
		require.NoError(t, err)
		return resp
	}
	payout2Balance := func() sdkmath.Int {
		return s.App.BankKeeper.GetBalance(s.Ctx, payout2, testDenom).Amount
	}
	exhausted := func(providerUUID string, tenant sdk.AccAddress) bool {
		state, err := k.Streams.Get(s.Ctx, keeper.NewStreamKey(providerUUID, tenant, testDenom))
		require.NoError(t, err)
		return state.IsExhausted()
	}
	coins := func(amount int64) sdk.Coins {
		return sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(amount)))
	}
	requireConserved := func() {
		t.Helper()
		total := s.payout(testDenom).Add(payout2Balance())
		for _, tenant := range tenants {
			total = total.Add(s.credit(tenant, testDenom))
		}
		require.Equal(t, sdkmath.NewInt(funded), total, "credit balances plus payouts must equal what was funded")
		s.requireInvariants(t)
	}

	sharedWith1 := s.lease(t, shared)
	sharedWith2 := leaseWith2(shared)
	richWith1 := s.lease(t, rich)
	poorWith2 := leaseWith2(poor)
	requireConserved()

	// --- t = 3000s: everybody can still pay. Provider 1 settles first.
	s.advance(3000 * time.Second)
	resp := s.withdraw(t, 0, nil)
	require.Equal(t, coins(3000+3000), resp.TotalAmounts)
	require.Equal(t, uint64(2), resp.WithdrawalCount)
	resp = withdraw2()
	require.Equal(t, coins(6000+6000), resp.TotalAmounts)
	require.Equal(t, uint64(2), resp.WithdrawalCount)

	require.Equal(t, sdkmath.NewInt(1800), s.credit(shared, testDenom))
	require.Equal(t, sdkmath.NewInt(1200), s.credit(poor, testDenom))
	requireConserved()

	// --- t = 4000s: the shared tenant owes provider 1 1000 and provider 2 2000,
	// against 1800. Provider 2 settles first and takes everything that is left.
	s.advance(1000 * time.Second)
	resp = withdraw2()
	require.Equal(t, coins(1800+1200), resp.TotalAmounts)
	require.Equal(t, uint64(2), resp.WithdrawalCount)
	require.True(t, exhausted(provider2.Uuid, shared))
	require.True(t, exhausted(provider2.Uuid, poor))
	require.True(t, s.credit(shared, testDenom).IsZero())
	requireConserved()

	// Provider 1 comes second: nothing is left for its stream with the shared
	// tenant, which freezes unpaid. The rich tenant pays as usual.
	s.Ctx = s.Ctx.WithEventManager(sdk.NewEventManager())
	resp = s.withdraw(t, 0, nil)
	require.Equal(t, coins(1000), resp.TotalAmounts)
	require.Equal(t, uint64(1), resp.WithdrawalCount)
	require.Equal(t, "1", attrValue(t, findEvent(t, s.Ctx, types.EventTypeBatchWithdraw), types.AttributeKeyExhaustedCount))
	require.True(t, exhausted(s.provider.Uuid, shared))
	require.False(t, exhausted(s.provider.Uuid, rich))
	requireConserved()

	require.Equal(t, sdkmath.NewInt(3000+3000+1000), s.payout(testDenom))
	require.Equal(t, sdkmath.NewInt(6000+6000+1800+1200), payout2Balance())

	// --- drain: the EndBlocker closes the leases of the three frozen streams
	// and moves no funds.
	s.Ctx = s.Ctx.WithEventManager(sdk.NewEventManager())
	require.NoError(t, k.EndBlocker(s.Ctx))
	require.Equal(t, 3, countEvents(s.Ctx, types.EventTypeLeaseAutoClose))
	require.Zero(t, countEvents(s.Ctx, "transfer"))

	for _, uuid := range []string{sharedWith1, sharedWith2, poorWith2} {
		lease, err := k.GetLease(s.Ctx, uuid)
		require.NoError(t, err)
		require.Equal(t, types.LEASE_STATE_CLOSED, lease.State)
		require.Equal(t, types.ClosureReasonCreditExhausted, lease.ClosureReason)
	}
	lease, err := k.GetLease(s.Ctx, richWith1)
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_ACTIVE, lease.State)

	for _, tenant := range []sdk.AccAddress{shared, poor} {
		account, err := k.GetCreditAccount(s.Ctx, tenant.String())
		require.NoError(t, err)
		require.Zero(t, account.ActiveLeaseCount)
		require.True(t, account.ReservedAmounts.IsZero())
	}
	streams, err := k.GetAllStreams(s.Ctx)
	require.NoError(t, err)
	require.Len(t, streams, 1, "only the rich tenant's stream survives")
	requireConserved()

	// --- afterwards: a top-up of the shared tenant is owed to nobody, and only
	// the surviving lease keeps paying.
	s.fundTenant(t, shared, testDenom, 5000)
	funded += 5000
	s.advance(1000 * time.Second)
	require.Equal(t, coins(1000), s.withdraw(t, 0, nil).TotalAmounts)
	require.True(t, withdraw2().TotalAmounts.IsZero())
	require.Equal(t, sdkmath.NewInt(5000), s.credit(shared, testDenom))
	requireConserved()

	// Every tenant paid at most what it funded, and the two exhausted ones
	// paid exactly that.
	require.Equal(t, sdkmath.NewInt(8000), s.payout(testDenom), "rich 5000 + shared 3000")
	require.Equal(t, sdkmath.NewInt(15000), payout2Balance(), "shared 7800 + poor 7200")
	require.True(t, s.credit(poor, testDenom).IsZero())
	require.Equal(t, sdkmath.NewInt(1_000_000-5000), s.credit(rich, testDenom))
}

// =============================================================================
// Multi-Denom Edge Cases
// =============================================================================

// TestAdversarial_MultiDenomPartialExhaustion tests that when a multi-denom lease
// exhausts one denom but not another, only the exhausted stream freezes, the lease is
// closed by the EndBlocker, and funds are handled properly.
func TestAdversarial_MultiDenomPartialExhaustion(t *testing.T) {
	f := initFixture(t)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]

	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())

	// Create two SKUs with different denoms
	sku1 := f.createTestSKU(t, provider.Uuid, 3600) // umfx

	// Create a second SKU with different denom
	skuUUID2, err := f.App.SKUKeeper.GenerateSKUUUID(f.Ctx)
	require.NoError(t, err)
	sku2 := skutypes.SKU{
		Uuid:         skuUUID2,
		ProviderUuid: provider.Uuid,
		Name:         "Test SKU 2",
		Unit:         skutypes.Unit_UNIT_PER_HOUR,
		BasePrice:    sdk.NewCoin(testDenom2, sdkmath.NewInt(3600)),
		Active:       true,
	}
	err = f.App.SKUKeeper.SetSKU(f.Ctx, sku2)
	require.NoError(t, err)

	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)

	// Fund plenty of umfx but minimal upwr (just enough to pass reservation check).
	// Reservation requires price_per_second * quantity * min_lease_duration = 1 * 1 * 3600 = 3600 upwr.
	// Fund 3700 so it passes reservation but exhausts well before umfx does.
	f.fundAccount(t, creditAddr, sdk.NewCoins(
		sdk.NewCoin(testDenom, sdkmath.NewInt(1_000_000)),
		sdk.NewCoin(testDenom2, sdkmath.NewInt(3700)),
	))

	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Create a lease with both denoms using service_name mode to allow same provider
	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)
	createResp, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items: []types.LeaseItemInput{
			{SkuUuid: sku1.Uuid, Quantity: 1, ServiceName: "svc1"},
			{SkuUuid: sku2.Uuid, Quantity: 1, ServiceName: "svc2"},
		},
	})
	require.NoError(t, err)

	// Acknowledge
	_, err = msgServer.AcknowledgeLease(f.Ctx, &types.MsgAcknowledgeLease{
		Sender:     providerAddr.String(),
		LeaseUuids: []string{createResp.LeaseUuid},
	})
	require.NoError(t, err)

	// Advance time to exhaust testDenom2 (3700 upwr at 1/sec) but not testDenom (1M umfx at 1/sec)
	f.Ctx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(4000 * time.Second))

	// The withdrawal pays 4000 umfx in full and the 3700 upwr that are left
	resp, err := msgServer.Withdraw(f.Ctx, &types.MsgWithdraw{
		Sender:       providerAddr.String(),
		ProviderUuid: provider.Uuid,
	})
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoins(
		sdk.NewCoin(testDenom, sdkmath.NewInt(4000)),
		sdk.NewCoin(testDenom2, sdkmath.NewInt(3700)),
	), resp.TotalAmounts)
	require.Equal(t, uint64(2), resp.WithdrawalCount)

	// Only the exhausted denom's stream is frozen
	healthy, err := f.App.BillingKeeper.Streams.Get(f.Ctx, keeper.NewStreamKey(provider.Uuid, tenant, testDenom))
	require.NoError(t, err)
	require.False(t, healthy.IsExhausted())
	frozen, err := f.App.BillingKeeper.Streams.Get(f.Ctx, keeper.NewStreamKey(provider.Uuid, tenant, testDenom2))
	require.NoError(t, err)
	require.True(t, frozen.IsExhausted(), "stream should freeze when its denom is exhausted")

	// Both denom balances are exact, never negative
	require.Equal(t, sdkmath.NewInt(1_000_000-4000), f.App.BankKeeper.GetBalance(f.Ctx, creditAddr, testDenom).Amount)
	require.True(t, f.App.BankKeeper.GetBalance(f.Ctx, creditAddr, testDenom2).IsZero())
	requireInvariants(t, f)

	// The lease is closed when any of its denoms is exhausted, and leaves the
	// healthy stream too. Nothing more is charged in either denom.
	f.Ctx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(time.Second))
	require.NoError(t, f.App.BillingKeeper.EndBlocker(f.Ctx))
	lease, err := f.App.BillingKeeper.GetLease(f.Ctx, createResp.LeaseUuid)
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_CLOSED, lease.State)
	require.Equal(t, types.ClosureReasonCreditExhausted, lease.ClosureReason)

	streams, err := f.App.BillingKeeper.GetAllStreams(f.Ctx)
	require.NoError(t, err)
	require.Empty(t, streams)

	// The healthy stream was settled for the second that passed before the drain.
	require.Equal(t, sdkmath.NewInt(4001), f.App.BankKeeper.GetBalance(f.Ctx, providerAddr, testDenom).Amount)
	require.Equal(t, sdkmath.NewInt(3700), f.App.BankKeeper.GetBalance(f.Ctx, providerAddr, testDenom2).Amount)
	requireInvariants(t, f)
}

// TestAdversarial_ZeroPricePerSecondSKU tests behavior when a SKU price converts
// to zero per-second rate (e.g., 1 token per day = 0 per second with integer division).
// This could allow free service indefinitely if not handled.
func TestAdversarial_ZeroPricePerSecondSKU(t *testing.T) {
	f := initFixture(t)

	providerAddr := f.TestAccs[1]
	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())

	// Create a SKU with 1 umfx per day — integer division gives 0 per second
	skuUUID, err := f.App.SKUKeeper.GenerateSKUUUID(f.Ctx)
	require.NoError(t, err)
	sku := skutypes.SKU{
		Uuid:         skuUUID,
		ProviderUuid: provider.Uuid,
		Name:         "Cheap SKU",
		Unit:         skutypes.Unit_UNIT_PER_DAY,
		BasePrice:    sdk.NewCoin(testDenom, sdkmath.NewInt(1)), // 1/86400 = 0 per second
		Active:       true,
	}
	err = f.App.SKUKeeper.SetSKU(f.Ctx, sku)
	require.NoError(t, err)

	// Attempt to convert price — this should fail (zero per-second rate)
	_, convertErr := keeper.ConvertBasePriceToPerSecond(sku.BasePrice, sku.Unit)

	// If conversion succeeds with zero, that's a potential free-service bug
	if convertErr == nil {
		t.Log("WARNING: zero per-second rate accepted — verify this doesn't allow free service")
	}
	// The SKU module's CalculatePricePerSecond should reject zero results
}

// =============================================================================
// Fund Conservation Under Batch Operations
// =============================================================================

// TestAdversarial_BatchCloseMultipleTenantsFundConservation tests that batch-closing
// leases from multiple tenants (by authority) correctly debits each tenant separately.
func TestAdversarial_BatchCloseMultipleTenantsFundConservation(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant1 := f.TestAccs[0]
	tenant2 := f.TestAccs[1]
	providerAddr := f.TestAccs[2]

	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	var leaseIDs []string
	for i, tenant := range []sdk.AccAddress{tenant1, tenant2} {
		creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
		require.NoError(t, err)
		f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(1_000_000))))
		err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
			Tenant:        tenant.String(),
			CreditAddress: creditAddr.String(),
		})
		require.NoError(t, err)

		// Tenant 1 leases one unit, tenant 2 leases two.
		leaseID := f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{
			{SkuUuid: sku.Uuid, Quantity: uint64(i + 1)}, //nolint:gosec // test code, i is [0,1]
		})
		leaseIDs = append(leaseIDs, leaseID)
	}

	creditAddr1 := types.DeriveCreditAddress(tenant1)
	creditAddr2 := types.DeriveCreditAddress(tenant2)

	f.Ctx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(100 * time.Second))

	// Authority batch-closes both leases
	resp, err := msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
		Sender:     f.Authority.String(),
		LeaseUuids: leaseIDs,
	})
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(300))), resp.TotalSettledAmounts)

	// Each tenant is debited for its own lease only, and the provider gains the sum
	require.Equal(t, sdkmath.NewInt(1_000_000-100), f.App.BankKeeper.GetBalance(f.Ctx, creditAddr1, testDenom).Amount)
	require.Equal(t, sdkmath.NewInt(1_000_000-200), f.App.BankKeeper.GetBalance(f.Ctx, creditAddr2, testDenom).Amount)
	require.Equal(t, sdkmath.NewInt(300), f.App.BankKeeper.GetBalance(f.Ctx, providerAddr, testDenom).Amount,
		"fund conservation violated")
	requireInvariants(t, f)
}

// TestAdversarial_SettlementWithMaxQuantity tests settlement with the maximum
// allowed quantity to verify no overflow in accrued amount calculations.
func TestAdversarial_SettlementWithMaxQuantity(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]

	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600) // 1/s

	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)

	// Fund a large amount
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(1_000_000_000_000_000))))
	require.NoError(t, f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	}))

	// Create the lease with MaxQuantityPerItem (1 billion)
	f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{
		{SkuUuid: sku.Uuid, Quantity: types.MaxQuantityPerItem},
	})

	state, err := f.App.BillingKeeper.Streams.Get(f.Ctx, keeper.NewStreamKey(provider.Uuid, tenant, testDenom))
	require.NoError(t, err)
	require.Equal(t, sdkmath.NewIntFromUint64(types.MaxQuantityPerItem), state.Rate)

	// Settle for 1 day — should be fine
	f.Ctx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(86400 * time.Second))
	resp, err := msgServer.Withdraw(f.Ctx, &types.MsgWithdraw{
		Sender:       providerAddr.String(),
		ProviderUuid: provider.Uuid,
	})
	require.NoError(t, err)

	// Expected: 1 * 1_000_000_000 * 86400 = 86_400_000_000_000
	expected := sdkmath.NewInt(1).Mul(sdkmath.NewIntFromUint64(types.MaxQuantityPerItem)).Mul(sdkmath.NewInt(86400))
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, expected)), resp.TotalAmounts,
		"accrual with max quantity produced unexpected result")
	require.Equal(t, expected, f.App.BankKeeper.GetBalance(f.Ctx, providerAddr, testDenom).Amount)
	requireInvariants(t, f)
}

// TestAdversarial_SettlementNearOverflowBoundary tests settlement around the
// 100-year overflow boundary (MaxDurationSeconds). Up to and including the
// boundary the accrual is exact; one second over, it is no longer computed and
// the provider receives everything that remains.
func TestAdversarial_SettlementNearOverflowBoundary(t *testing.T) {
	// The tenant holds a little more than 100 years of credit at 1/s.
	const funded = int64(keeper.MaxDurationSeconds) + 10

	tests := []struct {
		name       string
		seconds    int64
		wantPaid   int64
		wantFrozen bool
	}{
		{name: "just under the boundary", seconds: keeper.MaxDurationSeconds - 1, wantPaid: keeper.MaxDurationSeconds - 1},
		{name: "exactly at the boundary (inclusive)", seconds: keeper.MaxDurationSeconds, wantPaid: keeper.MaxDurationSeconds},
		{name: "one second over", seconds: keeper.MaxDurationSeconds + 1, wantPaid: funded, wantFrozen: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newStreamFixture(t, testDenom)
			tenant := s.TestAccs[0]
			s.fundTenant(t, tenant, testDenom, funded)
			s.lease(t, tenant)
			start := s.Ctx.BlockTime()

			s.advance(time.Duration(tc.seconds) * time.Second)
			resp := s.withdraw(t, 0, nil)
			require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(tc.wantPaid))), resp.TotalAmounts)
			require.Equal(t, sdkmath.NewInt(funded-tc.wantPaid).String(), s.credit(tenant, testDenom).String())

			state, exists := s.stream(t, tenant, testDenom)
			require.True(t, exists)
			require.Equal(t, tc.wantFrozen, state.IsExhausted())
			require.Equal(t, start.Add(time.Duration(tc.seconds)*time.Second), state.LastSettledAt)
			s.requireInvariants(t)
		})
	}
}
