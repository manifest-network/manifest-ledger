/*
Package keeper contains explicit security tests for the billing module.

Security Test Coverage:
- Authorization checks: only authorized parties can perform actions
- Overflow protection: calculations handle extreme values safely
- Cross-tenant isolation: users cannot access other users' resources
- Input validation: malformed inputs are properly rejected
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
		Sender:    attacker.String(),
		LeaseUuid: leaseID,
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
		Sender:    attacker.String(),
		LeaseUuid: leaseID,
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
		Sender:    attacker.String(),
		LeaseUuid: createResp.LeaseUuid,
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
		Sender:    attacker.String(),
		LeaseUuid: createResp.LeaseUuid,
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
	// Should return zero amounts due to overflow being silently handled
	require.True(t, result.AccruedAmounts.IsZero())
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
		Sender:    tenant2.String(),
		LeaseUuid: leaseID,
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

	// Tenant2 cannot cancel tenant1's PENDING lease
	_, err = msgServer.CancelLease(f.Ctx, &types.MsgCancelLease{
		Tenant:    tenant2.String(),
		LeaseUuid: createResp.LeaseUuid,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unauthorized")
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
				Sender:    tenant.String(),
				LeaseUuid: tc.leaseID,
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
		Sender:    providerAddr.String(),
		LeaseUuid: leaseID,
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
		Sender:    tenant.String(),
		LeaseUuid: leaseID,
	})
	require.NoError(t, err)

	// Try to close again (should fail)
	_, err = msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
		Sender:    tenant.String(),
		LeaseUuid: leaseID,
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
		Sender:    tenant.String(),
		LeaseUuid: leaseID,
	})
	require.NoError(t, err)

	// Try to withdraw from closed lease (should fail)
	_, err = msgServer.Withdraw(f.Ctx, &types.MsgWithdraw{
		Sender:    providerAddr.String(),
		LeaseUuid: leaseID,
	})
	require.Error(t, err)
}
