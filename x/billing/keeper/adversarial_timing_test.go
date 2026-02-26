/*
Package keeper_test contains adversarial tests targeting timing and ordering edge cases.

These tests attempt to exploit clock skew, same-block race conditions, timestamp
boundaries, and EndBlocker execution patterns.
*/
package keeper_test

import (
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

	leaseID := f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{
		{SkuUuid: sku.Uuid, Quantity: 1},
	})

	// First: advance time normally and withdraw
	f.Ctx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(100 * time.Second))
	_, err = msgServer.Withdraw(f.Ctx, &types.MsgWithdraw{
		Sender:     providerAddr.String(),
		LeaseUuids: []string{leaseID},
	})
	require.NoError(t, err)

	// Now: time goes BACKWARD (clock skew)
	f.Ctx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(-50 * time.Second))

	// Attempting to withdraw with backward time should yield zero (duration <= 0)
	_, err = msgServer.Withdraw(f.Ctx, &types.MsgWithdraw{
		Sender:     providerAddr.String(),
		LeaseUuids: []string{leaseID},
	})
	// Should error because no withdrawable amount
	require.Error(t, err)

	// Credit balance should not have changed
	creditBal := f.App.BankKeeper.GetBalance(f.Ctx, creditAddr, testDenom)
	require.True(t, creditBal.Amount.IsPositive(), "credit balance should be positive after backward time")
}

// TestAdversarial_SettlementWithZeroDuration tests that settlement with zero
// duration (settle at LastSettledAt) returns zero and doesn't transfer anything.
func TestAdversarial_SettlementWithZeroDuration(t *testing.T) {
	f := initFixture(t)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]

	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())

	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(1_000_000))))

	now := f.Ctx.BlockTime()
	lease := types.Lease{
		Uuid:         testLeaseUUID1,
		Tenant:       tenant.String(),
		ProviderUuid: provider.Uuid,
		Items: []types.LeaseItem{
			{SkuUuid: testSKUUUID, Quantity: 1, LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(1))},
		},
		State:         types.LEASE_STATE_ACTIVE,
		CreatedAt:     now,
		LastSettledAt: now,
	}
	err = f.App.BillingKeeper.SetLease(f.Ctx, lease)
	require.NoError(t, err)

	// Settle at the exact LastSettledAt time — duration = 0
	result, err := f.App.BillingKeeper.PerformSettlement(f.Ctx, &lease, now)
	require.NoError(t, err)
	require.True(t, result.TransferAmounts.IsZero(), "zero duration should produce zero transfer")
	require.True(t, result.AccruedAmounts.IsZero(), "zero duration should produce zero accrual")
}

// TestAdversarial_SettlementWithNegativeDuration tests settlement when settle time
// is before LastSettledAt (should return zero, not negative amounts).
func TestAdversarial_SettlementWithNegativeDuration(t *testing.T) {
	f := initFixture(t)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]

	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())

	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(1_000_000))))

	now := f.Ctx.BlockTime()
	lease := types.Lease{
		Uuid:         testLeaseUUID1,
		Tenant:       tenant.String(),
		ProviderUuid: provider.Uuid,
		Items: []types.LeaseItem{
			{SkuUuid: testSKUUUID, Quantity: 1, LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(1))},
		},
		State:         types.LEASE_STATE_ACTIVE,
		CreatedAt:     now,
		LastSettledAt: now,
	}
	err = f.App.BillingKeeper.SetLease(f.Ctx, lease)
	require.NoError(t, err)

	// Settle BEFORE LastSettledAt
	pastTime := now.Add(-100 * time.Second)
	result, err := f.App.BillingKeeper.PerformSettlement(f.Ctx, &lease, pastTime)
	require.NoError(t, err)
	require.True(t, result.TransferAmounts.IsZero(), "negative duration should produce zero transfer")
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

// =============================================================================
// Auto-Close Timing Edge Cases
// =============================================================================

// TestAdversarial_AutoCloseAtExactExhaustionPoint tests auto-close when credit
// is exactly equal to accrued amount (GTE check boundary).
func TestAdversarial_AutoCloseAtExactExhaustionPoint(t *testing.T) {
	f := initFixture(t)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]

	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)

	// Fund exactly 100 tokens. Price = 1/sec. After 100 seconds, accrued = 100 = balance.
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(100))))
	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	now := f.Ctx.BlockTime()
	lease := types.Lease{
		Uuid:         testLeaseUUID1,
		Tenant:       tenant.String(),
		ProviderUuid: provider.Uuid,
		Items: []types.LeaseItem{
			{SkuUuid: sku.Uuid, Quantity: 1, LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(1))},
		},
		State:         types.LEASE_STATE_ACTIVE,
		CreatedAt:     now,
		LastSettledAt: now,
	}
	err = f.App.BillingKeeper.SetLease(f.Ctx, lease)
	require.NoError(t, err)

	// At exactly 100 seconds, accrued (100) >= balance (100) → should auto-close
	f.Ctx = f.Ctx.WithBlockTime(now.Add(100 * time.Second))

	shouldClose, _, err := f.App.BillingKeeper.ShouldAutoCloseLease(f.Ctx, &lease)
	require.NoError(t, err)
	require.True(t, shouldClose, "lease should auto-close when accrued == balance (GTE)")

	// At 99 seconds, accrued (99) < balance (100) → should NOT auto-close
	f.Ctx = f.Ctx.WithBlockTime(now.Add(99 * time.Second))

	shouldClose, _, err = f.App.BillingKeeper.ShouldAutoCloseLease(f.Ctx, &lease)
	require.NoError(t, err)
	require.False(t, shouldClose, "lease should NOT auto-close when accrued < balance")
}

// TestAdversarial_ShouldAutoCloseWithFutureLastSettledAt tests that ShouldAutoCloseLease
// returns an error when LastSettledAt is in the future (data corruption indicator).
func TestAdversarial_ShouldAutoCloseWithFutureLastSettledAt(t *testing.T) {
	f := initFixture(t)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]

	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())

	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(1_000_000))))

	now := f.Ctx.BlockTime()
	lease := types.Lease{
		Uuid:         testLeaseUUID1,
		Tenant:       tenant.String(),
		ProviderUuid: provider.Uuid,
		Items: []types.LeaseItem{
			{SkuUuid: testSKUUUID, Quantity: 1, LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(1))},
		},
		State:         types.LEASE_STATE_ACTIVE,
		CreatedAt:     now,
		LastSettledAt: now.Add(1 * time.Hour), // Future!
	}
	err = f.App.BillingKeeper.SetLease(f.Ctx, lease)
	require.NoError(t, err)

	// This should return an error, not silently skip
	_, _, err = f.App.BillingKeeper.ShouldAutoCloseLease(f.Ctx, &lease)
	require.Error(t, err, "should detect data corruption: LastSettledAt in the future")
}

// TestAdversarial_ShouldAutoCloseOnNonActiveLease tests that ShouldAutoCloseLease
// returns false for non-ACTIVE leases and doesn't modify any state.
func TestAdversarial_ShouldAutoCloseOnNonActiveLease(t *testing.T) {
	f := initFixture(t)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]

	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())

	now := f.Ctx.BlockTime()

	for _, state := range []types.LeaseState{
		types.LEASE_STATE_PENDING,
		types.LEASE_STATE_CLOSED,
		types.LEASE_STATE_REJECTED,
		types.LEASE_STATE_EXPIRED,
	} {
		t.Run(state.String(), func(t *testing.T) {
			lease := types.Lease{
				Uuid:         testLeaseUUID1,
				Tenant:       tenant.String(),
				ProviderUuid: provider.Uuid,
				Items: []types.LeaseItem{
					{SkuUuid: testSKUUUID, Quantity: 1, LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(1))},
				},
				State:         state,
				CreatedAt:     now,
				LastSettledAt: now,
			}

			shouldClose, _, err := f.App.BillingKeeper.ShouldAutoCloseLease(f.Ctx, &lease)
			require.NoError(t, err)
			require.False(t, shouldClose, "non-ACTIVE lease should not auto-close")
		})
	}
}
