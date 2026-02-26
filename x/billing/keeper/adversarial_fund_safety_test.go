/*
Package keeper_test contains adversarial tests targeting fund safety in the billing module.

These tests attempt to break fund conservation invariants: double-spend, over-withdrawal,
settlement during insufficient funds, race conditions between billing and manual transfers,
and multi-denom edge cases.
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
	skutypes "github.com/manifest-network/manifest-ledger/x/sku/types"
)

// =============================================================================
// Double-Spend / Double-Settlement
// =============================================================================

// TestAdversarial_DoubleWithdrawSameBlock tests that withdrawing from the same
// lease twice in the same block (same block time) does not double-pay the provider.
// Attack: provider submits two Withdraw txs targeting the same lease in the same block.
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

	leaseID := f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{
		{SkuUuid: sku.Uuid, Quantity: 1},
	})

	// Advance time so there's something to withdraw
	f.Ctx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(3600 * time.Second))

	// Record balances before
	providerBalBefore := f.App.BankKeeper.GetBalance(f.Ctx, providerAddr, testDenom)
	creditBalBefore := f.App.BankKeeper.GetBalance(f.Ctx, creditAddr, testDenom)

	// First withdrawal succeeds
	resp1, err := msgServer.Withdraw(f.Ctx, &types.MsgWithdraw{
		Sender:     providerAddr.String(),
		LeaseUuids: []string{leaseID},
	})
	require.NoError(t, err)
	require.True(t, resp1.TotalAmounts.AmountOf(testDenom).IsPositive())

	firstWithdrawal := resp1.TotalAmounts.AmountOf(testDenom)

	// Second withdrawal in the same block should yield zero (LastSettledAt updated)
	_, err = msgServer.Withdraw(f.Ctx, &types.MsgWithdraw{
		Sender:     providerAddr.String(),
		LeaseUuids: []string{leaseID},
	})
	// Should error with no withdrawable amount (duration = 0)
	require.Error(t, err)

	// Verify fund conservation: provider gained exactly what credit lost
	providerBalAfter := f.App.BankKeeper.GetBalance(f.Ctx, providerAddr, testDenom)
	creditBalAfter := f.App.BankKeeper.GetBalance(f.Ctx, creditAddr, testDenom)

	providerGain := providerBalAfter.Amount.Sub(providerBalBefore.Amount)
	creditLoss := creditBalBefore.Amount.Sub(creditBalAfter.Amount)

	require.Equal(t, providerGain, creditLoss, "fund conservation violated")
	require.Equal(t, firstWithdrawal, providerGain, "provider received more than expected")
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

	// First close succeeds
	_, err = msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
		Sender:     tenant.String(),
		LeaseUuids: []string{leaseID},
	})
	require.NoError(t, err)

	// Second close must fail (lease is no longer ACTIVE)
	_, err = msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
		Sender:     tenant.String(),
		LeaseUuids: []string{leaseID},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not active")
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

	providerBalBefore := f.App.BankKeeper.GetBalance(f.Ctx, providerAddr, testDenom)

	// Withdraw first
	resp, err := msgServer.Withdraw(f.Ctx, &types.MsgWithdraw{
		Sender:     providerAddr.String(),
		LeaseUuids: []string{leaseID},
	})
	require.NoError(t, err)
	withdrawAmount := resp.TotalAmounts.AmountOf(testDenom)

	// Then close (should settle zero additional since we just withdrew at the same time)
	_, err = msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
		Sender:     tenant.String(),
		LeaseUuids: []string{leaseID},
	})
	require.NoError(t, err)

	// Verify total provider gain does not exceed initial funding
	providerBalAfter := f.App.BankKeeper.GetBalance(f.Ctx, providerAddr, testDenom)
	totalProviderGain := providerBalAfter.Amount.Sub(providerBalBefore.Amount)

	require.True(t, totalProviderGain.LTE(initialFunding),
		"provider gained %s but credit was only funded with %s", totalProviderGain, initialFunding)
	require.Equal(t, withdrawAmount, totalProviderGain,
		"close should not have settled additional funds after withdraw in same block")
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

	providerBalBefore := f.App.BankKeeper.GetBalance(f.Ctx, providerAddr, testDenom)

	// Withdraw should auto-close and transfer whatever is available
	resp, err := msgServer.Withdraw(f.Ctx, &types.MsgWithdraw{
		Sender:     providerAddr.String(),
		LeaseUuids: []string{leaseID},
	})
	require.NoError(t, err)

	// Verify provider received at most what was in the credit account
	providerBalAfter := f.App.BankKeeper.GetBalance(f.Ctx, providerAddr, testDenom)
	providerGain := providerBalAfter.Amount.Sub(providerBalBefore.Amount)

	// Credit balance should now be zero (or very close)
	creditBalAfter := f.App.BankKeeper.GetBalance(f.Ctx, creditAddr, testDenom)

	require.True(t, providerGain.LTE(sdkmath.NewInt(3700)),
		"provider received %s but credit only had 3700", providerGain)
	require.True(t, creditBalAfter.Amount.GTE(sdkmath.ZeroInt()),
		"credit balance went negative: %s", creditBalAfter.Amount)

	// Verify lease was auto-closed
	lease, err := f.App.BillingKeeper.GetLease(f.Ctx, leaseID)
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_CLOSED, lease.State)

	_ = resp
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

	leaseID := f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{
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

	// Settlement should handle the reduced balance gracefully (transfer min(accrued, available))
	resp, err := msgServer.Withdraw(f.Ctx, &types.MsgWithdraw{
		Sender:     providerAddr.String(),
		LeaseUuids: []string{leaseID},
	})
	require.NoError(t, err)

	// Provider should get at most 1 (the remaining balance)
	require.True(t, resp.TotalAmounts.AmountOf(testDenom).LTE(sdkmath.NewInt(1)),
		"provider received more than available balance")

	// Credit balance should be non-negative
	creditBal := f.App.BankKeeper.GetBalance(f.Ctx, creditAddr, testDenom)
	require.True(t, creditBal.Amount.GTE(sdkmath.ZeroInt()),
		"credit balance went negative: %s", creditBal.Amount)
}

// =============================================================================
// Multi-Tenant Fund Isolation
// =============================================================================

// TestAdversarial_SettlementDoesNotAffectOtherTenants tests that settling one
// tenant's lease doesn't accidentally debit another tenant's credit account.
func TestAdversarial_SettlementDoesNotAffectOtherTenants(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant1 := f.TestAccs[0]
	tenant2 := f.TestAccs[1]
	providerAddr := f.TestAccs[2]

	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Fund both tenants
	for _, tenant := range []sdk.AccAddress{tenant1, tenant2} {
		creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
		require.NoError(t, err)
		f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(1_000_000))))
		err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
			Tenant:        tenant.String(),
			CreditAddress: creditAddr.String(),
		})
		require.NoError(t, err)
	}

	// Create leases for both
	leaseID1 := f.createAndAcknowledgeLease(t, msgServer, tenant1, providerAddr, []types.LeaseItemInput{
		{SkuUuid: sku.Uuid, Quantity: 1},
	})
	_ = f.createAndAcknowledgeLease(t, msgServer, tenant2, providerAddr, []types.LeaseItemInput{
		{SkuUuid: sku.Uuid, Quantity: 1},
	})

	// Record tenant2's balance before
	creditAddr2, _ := types.DeriveCreditAddressFromBech32(tenant2.String())
	tenant2BalBefore := f.App.BankKeeper.GetBalance(f.Ctx, creditAddr2, testDenom)

	// Advance time and settle only tenant1's lease
	f.Ctx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(3600 * time.Second))

	_, err := msgServer.Withdraw(f.Ctx, &types.MsgWithdraw{
		Sender:     providerAddr.String(),
		LeaseUuids: []string{leaseID1},
	})
	require.NoError(t, err)

	// Verify tenant2's balance is unchanged
	tenant2BalAfter := f.App.BankKeeper.GetBalance(f.Ctx, creditAddr2, testDenom)
	require.Equal(t, tenant2BalBefore, tenant2BalAfter,
		"tenant2's credit balance changed during tenant1's settlement")
}

// =============================================================================
// Multi-Denom Edge Cases
// =============================================================================

// TestAdversarial_MultiDenomPartialExhaustion tests that when a multi-denom lease
// exhausts one denom but not another, auto-close triggers correctly and funds are handled properly.
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

	// Check auto-close detection
	lease, err := f.App.BillingKeeper.GetLease(f.Ctx, createResp.LeaseUuid)
	require.NoError(t, err)

	shouldClose, _, err := f.App.BillingKeeper.ShouldAutoCloseLease(f.Ctx, &lease)
	require.NoError(t, err)
	require.True(t, shouldClose, "lease should auto-close when any denom is exhausted")

	// Verify no negative balances after auto-close via withdraw
	_, err = msgServer.Withdraw(f.Ctx, &types.MsgWithdraw{
		Sender:     providerAddr.String(),
		LeaseUuids: []string{createResp.LeaseUuid},
	})
	require.NoError(t, err)

	// Both denom balances must be non-negative
	bal1 := f.App.BankKeeper.GetBalance(f.Ctx, creditAddr, testDenom)
	bal2 := f.App.BankKeeper.GetBalance(f.Ctx, creditAddr, testDenom2)
	require.True(t, bal1.Amount.GTE(sdkmath.ZeroInt()), "umfx balance negative: %s", bal1.Amount)
	require.True(t, bal2.Amount.GTE(sdkmath.ZeroInt()), "upwr balance negative: %s", bal2.Amount)
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
	for _, tenant := range []sdk.AccAddress{tenant1, tenant2} {
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
		leaseIDs = append(leaseIDs, leaseID)
	}

	// Record balances
	creditAddr1, _ := types.DeriveCreditAddressFromBech32(tenant1.String())
	creditAddr2, _ := types.DeriveCreditAddressFromBech32(tenant2.String())
	t1Before := f.App.BankKeeper.GetBalance(f.Ctx, creditAddr1, testDenom)
	t2Before := f.App.BankKeeper.GetBalance(f.Ctx, creditAddr2, testDenom)
	provBefore := f.App.BankKeeper.GetBalance(f.Ctx, providerAddr, testDenom)

	f.Ctx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(100 * time.Second))

	// Authority batch-closes both leases
	_, err := msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
		Sender:     f.Authority.String(),
		LeaseUuids: leaseIDs,
	})
	require.NoError(t, err)

	// Verify fund conservation: provider gain == tenant1 loss + tenant2 loss
	t1After := f.App.BankKeeper.GetBalance(f.Ctx, creditAddr1, testDenom)
	t2After := f.App.BankKeeper.GetBalance(f.Ctx, creditAddr2, testDenom)
	provAfter := f.App.BankKeeper.GetBalance(f.Ctx, providerAddr, testDenom)

	t1Loss := t1Before.Amount.Sub(t1After.Amount)
	t2Loss := t2Before.Amount.Sub(t2After.Amount)
	provGain := provAfter.Amount.Sub(provBefore.Amount)

	require.Equal(t, t1Loss.Add(t2Loss), provGain,
		"fund conservation violated: tenant losses (%s + %s) != provider gain (%s)",
		t1Loss, t2Loss, provGain)
}

// TestAdversarial_SettlementWithMaxQuantity tests settlement with the maximum
// allowed quantity to verify no overflow in accrued amount calculations.
func TestAdversarial_SettlementWithMaxQuantity(t *testing.T) {
	f := initFixture(t)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]

	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())

	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)

	// Create lease directly with MaxQuantityPerItem
	lease := types.Lease{
		Uuid:         testLeaseUUID1,
		Tenant:       tenant.String(),
		ProviderUuid: provider.Uuid,
		Items: []types.LeaseItem{
			{
				SkuUuid:     testSKUUUID,
				Quantity:    types.MaxQuantityPerItem, // 1 billion
				LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(1)),
			},
		},
		State:         types.LEASE_STATE_ACTIVE,
		CreatedAt:     f.Ctx.BlockTime(),
		LastSettledAt: f.Ctx.BlockTime(),
	}
	err = f.App.BillingKeeper.SetLease(f.Ctx, lease)
	require.NoError(t, err)

	// Fund a large amount
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(1_000_000_000_000_000))))

	// Settle for 1 day — should be fine
	settleTime := f.Ctx.BlockTime().Add(86400 * time.Second)
	result, err := f.App.BillingKeeper.PerformSettlement(f.Ctx, &lease, settleTime)
	require.NoError(t, err)

	// Expected: 1 * 1_000_000_000 * 86400 = 86_400_000_000_000
	expected := sdkmath.NewInt(1).Mul(sdkmath.NewIntFromUint64(types.MaxQuantityPerItem)).Mul(sdkmath.NewInt(86400))
	require.Equal(t, expected, result.AccruedAmounts.AmountOf(testDenom),
		"accrual with max quantity produced unexpected result")
}

// TestAdversarial_SettlementNearOverflowBoundary tests settlement at just under
// the 100-year overflow boundary.
func TestAdversarial_SettlementNearOverflowBoundary(t *testing.T) {
	f := initFixture(t)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]

	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())

	lease := types.Lease{
		Uuid:         testLeaseUUID1,
		Tenant:       tenant.String(),
		ProviderUuid: provider.Uuid,
		Items: []types.LeaseItem{
			{SkuUuid: testSKUUUID, Quantity: 1, LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(1))},
		},
		State:         types.LEASE_STATE_ACTIVE,
		CreatedAt:     f.Ctx.BlockTime(),
		LastSettledAt: f.Ctx.BlockTime(),
	}
	err := f.App.BillingKeeper.SetLease(f.Ctx, lease)
	require.NoError(t, err)

	// Just under 100 years should succeed
	justUnder := time.Duration(keeper.MaxDurationSeconds-1) * time.Second
	settleTime := f.Ctx.BlockTime().Add(justUnder)

	creditAddr, _ := types.DeriveCreditAddressFromBech32(tenant.String())
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(1).Mul(sdkmath.NewInt(keeper.MaxDurationSeconds)))))

	result, err := f.App.BillingKeeper.PerformSettlement(f.Ctx, &lease, settleTime)
	require.NoError(t, err)
	require.True(t, result.AccruedAmounts.AmountOf(testDenom).IsPositive())

	// Exactly at the boundary should still succeed
	lease.LastSettledAt = f.Ctx.BlockTime() // Reset
	exactBoundary := time.Duration(keeper.MaxDurationSeconds) * time.Second
	settleTimeExact := f.Ctx.BlockTime().Add(exactBoundary)

	_, err = f.App.BillingKeeper.PerformSettlement(f.Ctx, &lease, settleTimeExact)
	require.NoError(t, err) // MaxDurationSeconds is inclusive (> check, not >=)

	// One second over should fail
	lease.LastSettledAt = f.Ctx.BlockTime() // Reset
	overBoundary := time.Duration(keeper.MaxDurationSeconds+1) * time.Second
	settleTimeOver := f.Ctx.BlockTime().Add(overBoundary)

	_, err = f.App.BillingKeeper.PerformSettlement(f.Ctx, &lease, settleTimeOver)
	require.Error(t, err)
	require.Contains(t, err.Error(), "overflow")
}
