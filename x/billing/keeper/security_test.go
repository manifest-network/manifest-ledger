/*
Package keeper contains explicit security tests for the billing module.

Security Test Coverage:
- Authorization checks: only authorized parties can perform actions
- Overflow protection: calculations handle extreme values safely
- Cross-tenant isolation: users cannot access other users' resources
- Input validation: malformed inputs are properly rejected
- Denom-spam resistance: dust tokens on credit addresses don't leak into billing paths
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

// ============================================================================
// Authorization Tests
// ============================================================================

func TestSecurity_UnauthorizedLeaseClose(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	attacker := f.TestAccs[2]

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Fund tenant credit account
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(10000))))

	// Create credit account
	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Create and acknowledge a lease
	leaseID := f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{
		{SkuUuid: sku.Uuid, Quantity: 1},
	})

	// Attacker tries to close someone else's lease
	_, err = msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
		Sender:     attacker.String(),
		LeaseUuids: []string{leaseID},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unauthorized")
}

func TestSecurity_UnauthorizedProviderWithdrawal(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	attacker := f.TestAccs[2]

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Fund tenant credit account
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(10000))))

	// Create credit account
	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Create and acknowledge a lease
	leaseID := f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{
		{SkuUuid: sku.Uuid, Quantity: 1},
	})

	// Advance time to accrue some amount
	newCtx := f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(100 * time.Second))
	f.Ctx = newCtx

	// Attacker (not the provider) tries to withdraw from the lease
	_, err = msgServer.Withdraw(f.Ctx, &types.MsgWithdraw{
		Sender:     attacker.String(),
		LeaseUuids: []string{leaseID},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unauthorized")
}

func TestSecurity_UnauthorizedLeaseAcknowledgement(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	attacker := f.TestAccs[2]

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Fund tenant credit account
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(10000))))

	// Create credit account
	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Create a pending lease
	createResp, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items: []types.LeaseItemInput{
			{SkuUuid: sku.Uuid, Quantity: 1},
		},
	})
	require.NoError(t, err)

	// Attacker (not the provider) tries to acknowledge the lease
	_, err = msgServer.AcknowledgeLease(f.Ctx, &types.MsgAcknowledgeLease{
		Sender:     attacker.String(),
		LeaseUuids: []string{createResp.LeaseUuid},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unauthorized")
}

func TestSecurity_UnauthorizedLeaseRejection(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	attacker := f.TestAccs[2]

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Fund tenant credit account
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(10000))))

	// Create credit account
	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Create a pending lease
	createResp, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items: []types.LeaseItemInput{
			{SkuUuid: sku.Uuid, Quantity: 1},
		},
	})
	require.NoError(t, err)

	// Attacker (not the provider) tries to reject the lease
	_, err = msgServer.RejectLease(f.Ctx, &types.MsgRejectLease{
		Sender:     attacker.String(),
		LeaseUuids: []string{createResp.LeaseUuid},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unauthorized")
}

// ============================================================================
// Overflow Protection Tests
// ============================================================================

func TestSecurity_AccrualOverflowProtection(t *testing.T) {
	f := initFixture(t)

	k := f.App.BillingKeeper
	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]

	// Create provider
	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())

	// Fund tenant credit account
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(1_000_000_000_000))))

	// Create lease with normal price
	now := f.Ctx.BlockTime()
	lease := types.Lease{
		Uuid:         "01912345-6789-7abc-8def-0123456789ab",
		Tenant:       tenant.String(),
		ProviderUuid: provider.Uuid,
		Items: []types.LeaseItem{
			{SkuUuid: "01912345-6789-7abc-8def-0123456789ae", Quantity: 1, LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(1000))},
		},
		State:         types.LEASE_STATE_ACTIVE,
		CreatedAt:     now,
		LastSettledAt: now,
	}
	err = k.SetLease(f.Ctx, lease)
	require.NoError(t, err)

	// Try to settle for an extremely long duration (>100 years)
	veryFarFuture := now.Add(101 * 365 * 24 * time.Hour)

	// PerformSettlement should error on overflow
	_, err = k.PerformSettlement(f.Ctx, &lease, veryFarFuture)
	require.Error(t, err)
	require.Contains(t, err.Error(), "overflow")

	// PerformSettlementSilent should NOT error (silently handles overflow)
	result, err := k.PerformSettlementSilent(f.Ctx, &lease, veryFarFuture)
	require.NoError(t, err)
	// On overflow, all remaining credit should be transferred to the provider
	require.Equal(t, int64(1_000_000_000_000), result.AccruedAmounts.AmountOf(testDenom).Int64())
	require.Equal(t, int64(1_000_000_000_000), result.TransferAmounts.AmountOf(testDenom).Int64())
}

func TestSecurity_ExtremeQuantityValues(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Fund tenant credit account with large amount
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(1_000_000_000_000_000))))

	// Create credit account
	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Try to create lease with extremely high quantity
	_, err = msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items: []types.LeaseItemInput{
			{SkuUuid: sku.Uuid, Quantity: 1<<63 - 1}, // Max int64
		},
	})
	// Should be rejected or handled safely
	require.Error(t, err)
}

// ============================================================================
// Cross-Tenant Isolation Tests
// ============================================================================

func TestSecurity_TenantCreditAccountIsolation(t *testing.T) {
	f := initFixture(t)

	tenant1 := f.TestAccs[0]
	tenant2 := f.TestAccs[1]

	// Fund tenant1's credit account
	creditAddr1, err := types.DeriveCreditAddressFromBech32(tenant1.String())
	require.NoError(t, err)
	f.fundAccount(t, creditAddr1, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(10000))))

	// Fund tenant2's credit account with different amount
	creditAddr2, err := types.DeriveCreditAddressFromBech32(tenant2.String())
	require.NoError(t, err)
	f.fundAccount(t, creditAddr2, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(5000))))

	// Verify credit addresses are different (deterministic derivation)
	require.NotEqual(t, creditAddr1.String(), creditAddr2.String())

	// Verify each tenant's balance is correct
	balance1 := f.App.BankKeeper.GetBalance(f.Ctx, creditAddr1, testDenom)
	require.Equal(t, int64(10000), balance1.Amount.Int64())

	balance2 := f.App.BankKeeper.GetBalance(f.Ctx, creditAddr2, testDenom)
	require.Equal(t, int64(5000), balance2.Amount.Int64())

	// Credit account addresses are deterministically derived from tenant addresses
	// so there's no way for one tenant to "reach" another's credit account
}

func TestSecurity_LeaseIsolation(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant1 := f.TestAccs[0]
	tenant2 := f.TestAccs[1]
	providerAddr := f.TestAccs[2]

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Fund both tenant credit accounts
	for _, tenant := range []sdk.AccAddress{tenant1, tenant2} {
		creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
		require.NoError(t, err)
		f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(10000))))

		err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
			Tenant:        tenant.String(),
			CreditAddress: creditAddr.String(),
		})
		require.NoError(t, err)
	}

	// Create an ACTIVE lease for tenant1
	leaseID := f.createAndAcknowledgeLease(t, msgServer, tenant1, providerAddr, []types.LeaseItemInput{
		{SkuUuid: sku.Uuid, Quantity: 1},
	})

	// Tenant2 cannot close tenant1's ACTIVE lease
	_, err := msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
		Sender:     tenant2.String(),
		LeaseUuids: []string{leaseID},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unauthorized")

	// Create a PENDING lease for tenant1 (to test CancelLease isolation)
	createResp, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant1.String(),
		Items: []types.LeaseItemInput{
			{SkuUuid: sku.Uuid, Quantity: 1},
		},
	})
	require.NoError(t, err)

	// Tenant2 cannot cancel tenant1's PENDING lease (both have credit accounts, but tenant2 doesn't own the lease)
	_, err = msgServer.CancelLease(f.Ctx, &types.MsgCancelLease{
		Tenant:     tenant2.String(),
		LeaseUuids: []string{createResp.LeaseUuid},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "is not the tenant")
}

// ============================================================================
// Input Validation Tests
// ============================================================================

func TestSecurity_InvalidUUIDFormat(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]

	tests := []struct {
		name    string
		leaseID string
	}{
		{"empty UUID", ""},
		{"invalid format", "not-a-uuid"},
		{"SQL injection attempt", "'; DROP TABLE leases; --"},
		{"path traversal", "../../../etc/passwd"},
		{"too long", "01912345-6789-7abc-8def-0123456789ab-extra-stuff-that-should-not-be-here"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
				Sender:     tenant.String(),
				LeaseUuids: []string{tc.leaseID},
			})
			require.Error(t, err)
		})
	}
}

func TestSecurity_InvalidProviderUUID(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]

	// Fund tenant credit account
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(10000))))

	// Create credit account
	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Try to create lease referencing SKU with invalid UUID
	_, err = msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items: []types.LeaseItemInput{
			{SkuUuid: "invalid-sku-uuid", Quantity: 1},
		},
	})
	require.Error(t, err)
}

func TestSecurity_NonexistentSKU(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]

	// Fund tenant credit account
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(10000))))

	// Create credit account
	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Try to create lease with nonexistent SKU (valid UUID format)
	_, err = msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items: []types.LeaseItemInput{
			{SkuUuid: "01912345-6789-7abc-8def-999999999999", Quantity: 1},
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestSecurity_ZeroQuantityLease(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Fund tenant credit account
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(10000))))

	// Create credit account
	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Try to create lease with zero quantity
	_, err = msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items: []types.LeaseItemInput{
			{SkuUuid: sku.Uuid, Quantity: 0},
		},
	})
	require.Error(t, err)
}

func TestSecurity_EmptyLeaseItems(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]

	// Fund tenant credit account
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(10000))))

	// Create credit account
	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Try to create lease with no items
	_, err = msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items:  []types.LeaseItemInput{},
	})
	require.Error(t, err)
}

// ============================================================================
// Provider Authorization Tests
// ============================================================================

// Note: Provider and SKU authorization tests are covered in the SKU module tests.
// The billing module relies on the SKU module for provider/SKU ownership validation.

// ============================================================================
// Lease State Transition Tests
// ============================================================================

func TestSecurity_DoubleAcknowledgement(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Fund tenant credit account
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(10000))))

	// Create credit account
	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Create and acknowledge a lease
	leaseID := f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{
		{SkuUuid: sku.Uuid, Quantity: 1},
	})

	// Try to acknowledge again (should fail)
	_, err = msgServer.AcknowledgeLease(f.Ctx, &types.MsgAcknowledgeLease{
		Sender:     providerAddr.String(),
		LeaseUuids: []string{leaseID},
	})
	require.Error(t, err)
}

func TestSecurity_CloseAlreadyClosedLease(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Fund tenant credit account
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(10000))))

	// Create credit account
	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Create and acknowledge a lease
	leaseID := f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{
		{SkuUuid: sku.Uuid, Quantity: 1},
	})

	// Close the lease
	_, err = msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
		Sender:     tenant.String(),
		LeaseUuids: []string{leaseID},
	})
	require.NoError(t, err)

	// Try to close again (should fail)
	_, err = msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
		Sender:     tenant.String(),
		LeaseUuids: []string{leaseID},
	})
	require.Error(t, err)
}

func TestSecurity_WithdrawFromClosedLease(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Fund tenant credit account
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(10000))))

	// Create credit account
	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Create and acknowledge a lease
	leaseID := f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{
		{SkuUuid: sku.Uuid, Quantity: 1},
	})

	// Advance time
	newCtx := f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(100 * time.Second))
	f.Ctx = newCtx

	// Close the lease
	_, err = msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
		Sender:     tenant.String(),
		LeaseUuids: []string{leaseID},
	})
	require.NoError(t, err)

	// Try to withdraw from closed lease (should fail - no withdrawable amount)
	_, err = msgServer.Withdraw(f.Ctx, &types.MsgWithdraw{
		Sender:     providerAddr.String(),
		LeaseUuids: []string{leaseID},
	})
	require.Error(t, err)
}

// ============================================================================
// Denom-Spam Resistance Tests
// ============================================================================

// TestSecurity_DenomSpamDoesNotAffectSettlement verifies that dust tokens
// sent to a credit address by third parties do not leak into settlement
// transfers.
func TestSecurity_DenomSpamDoesNotAffectSettlement(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]

	// Create provider and SKU (priced in umfx)
	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600) // 3600 umfx/hour

	// Fund tenant credit account with the real denom
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(1_000_000))))

	// Spam the credit address with many dust denoms (simulating attacker)
	dustDenoms := []string{"dust1", "dust2", "dust3", "dust4", "dust5"}
	for _, denom := range dustDenoms {
		f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(denom, sdkmath.NewInt(1))))
	}

	// Verify dust is actually on the credit address
	allBalances := f.App.BankKeeper.GetAllBalances(f.Ctx, creditAddr)
	require.Equal(t, len(dustDenoms)+1, len(allBalances), "credit address should have real denom + dust denoms")

	// Create credit account
	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Create and acknowledge a lease
	leaseID := f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{
		{SkuUuid: sku.Uuid, Quantity: 1},
	})

	// Advance time so there's something to settle
	f.Ctx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(100 * time.Second))

	// Withdraw (triggers settlement)
	resp, err := msgServer.Withdraw(f.Ctx, &types.MsgWithdraw{
		Sender:     providerAddr.String(),
		LeaseUuids: []string{leaseID},
	})
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Verify: provider received ONLY the lease denom, not dust
	for _, denom := range dustDenoms {
		providerBal := f.App.BankKeeper.GetBalance(f.Ctx, sdk.MustAccAddressFromBech32(provider.PayoutAddress), denom)
		require.True(t, providerBal.IsZero(), "provider should not receive dust denom %s", denom)
	}

	// Verify: dust is still on the credit address (untouched)
	for _, denom := range dustDenoms {
		creditBal := f.App.BankKeeper.GetBalance(f.Ctx, creditAddr, denom)
		require.Equal(t, sdkmath.NewInt(1), creditBal.Amount, "dust denom %s should remain on credit address", denom)
	}

	// Verify: the lease's real denom was settled correctly
	providerBal := f.App.BankKeeper.GetBalance(f.Ctx, sdk.MustAccAddressFromBech32(provider.PayoutAddress), testDenom)
	require.True(t, providerBal.IsPositive(), "provider should have received settlement in %s", testDenom)
}

// TestSecurity_DenomSpamDoesNotAffectAutoClose verifies that dust tokens
// on a credit address do not interfere with the auto-close decision.
func TestSecurity_DenomSpamDoesNotAffectAutoClose(t *testing.T) {
	f := initFixture(t)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Fund credit account with a small amount (will be exhausted quickly)
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(10))))

	// Spam dust denoms
	for _, denom := range []string{"spam1", "spam2", "spam3"} {
		f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(denom, sdkmath.NewInt(999_999_999))))
	}

	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Build a lease manually in ACTIVE state
	lease := types.Lease{
		Uuid:         testLeaseUUID1,
		Tenant:       tenant.String(),
		ProviderUuid: provider.Uuid,
		Items: []types.LeaseItem{
			{SkuUuid: sku.Uuid, Quantity: 1, LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(1))},
		},
		State:         types.LEASE_STATE_ACTIVE,
		CreatedAt:     f.Ctx.BlockTime(),
		LastSettledAt: f.Ctx.BlockTime(),
	}
	err = f.App.BillingKeeper.SetLease(f.Ctx, lease)
	require.NoError(t, err)

	// Advance time far enough to exhaust the real denom balance
	f.Ctx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(3600 * time.Second))

	// Auto-close should trigger based on the lease's denom (umfx), not be
	// confused by the large balances in spam denoms.
	shouldClose, _, err := f.App.BillingKeeper.ShouldAutoCloseLease(f.Ctx, &lease)
	require.NoError(t, err)
	require.True(t, shouldClose, "lease should auto-close: real denom exhausted despite large dust balances")
}

// TestSecurity_DenomSpamDoesNotLeakOnOverflow verifies that when accrual
// overflows (duration > ~100 years) and PerformSettlementSilent transfers
// all remaining credit, only the lease's denoms are transferred — not dust.
func TestSecurity_DenomSpamDoesNotLeakOnOverflow(t *testing.T) {
	f := initFixture(t)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Fund credit account with the real denom
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(5000))))

	// Spam dust denoms with large balances
	dustDenoms := []string{"overflow_dust1", "overflow_dust2", "overflow_dust3"}
	for _, denom := range dustDenoms {
		f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(denom, sdkmath.NewInt(999_999_999))))
	}

	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Build a lease manually in ACTIVE state
	lease := types.Lease{
		Uuid:         testLeaseUUID1,
		Tenant:       tenant.String(),
		ProviderUuid: provider.Uuid,
		Items: []types.LeaseItem{
			{SkuUuid: sku.Uuid, Quantity: 1, LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(1))},
		},
		State:         types.LEASE_STATE_ACTIVE,
		CreatedAt:     f.Ctx.BlockTime(),
		LastSettledAt: f.Ctx.BlockTime(),
	}
	err = f.App.BillingKeeper.SetLease(f.Ctx, lease)
	require.NoError(t, err)

	// Settle with a time > 100 years in the future to trigger accrual overflow.
	// PerformSettlementSilent handles overflow by transferring all remaining
	// credit rather than returning an error.
	overflowTime := f.Ctx.BlockTime().Add(101 * 365 * 24 * time.Hour)
	result, err := f.App.BillingKeeper.PerformSettlementSilent(f.Ctx, &lease, overflowTime)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify: only the lease's denom was transferred, not dust
	require.Len(t, result.TransferAmounts, 1, "overflow settlement should transfer exactly one denom")
	require.Equal(t, testDenom, result.TransferAmounts[0].Denom)
	require.Equal(t, sdkmath.NewInt(5000), result.TransferAmounts[0].Amount,
		"overflow settlement should transfer all remaining credit in the lease's denom")

	// Verify: provider received none of the dust denoms
	payoutAddr := sdk.MustAccAddressFromBech32(provider.PayoutAddress)
	for _, denom := range dustDenoms {
		bal := f.App.BankKeeper.GetBalance(f.Ctx, payoutAddr, denom)
		require.True(t, bal.IsZero(), "provider should not receive dust denom %s on overflow", denom)
	}

	// Verify: dust is still on the credit address
	for _, denom := range dustDenoms {
		bal := f.App.BankKeeper.GetBalance(f.Ctx, creditAddr, denom)
		require.Equal(t, sdkmath.NewInt(999_999_999), bal.Amount,
			"dust denom %s should remain untouched on credit address after overflow settlement", denom)
	}
}
