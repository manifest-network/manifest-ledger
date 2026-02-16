/*
Package keeper contains unit tests for the settlement helper functions.

Test Coverage:
- LeaseItemsToWithPrice: conversion of lease items to accrual format
- CalculateTransferAmounts: minimum calculation between accrued and available
- PerformSettlement: full settlement flow with transfer
- PerformSettlementSilent: settlement with silent overflow handling
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
// LeaseItemsToWithPrice Tests
// ============================================================================

func TestLeaseItemsToWithPrice(t *testing.T) {
	tests := []struct {
		name     string
		items    []types.LeaseItem
		expected []keeper.LeaseItemWithPrice
	}{
		{
			name:     "empty items",
			items:    []types.LeaseItem{},
			expected: []keeper.LeaseItemWithPrice{},
		},
		{
			name: "single item",
			items: []types.LeaseItem{
				{SkuUuid: "sku-1", Quantity: 2, LockedPrice: sdk.NewCoin("upwr", sdkmath.NewInt(100))},
			},
			expected: []keeper.LeaseItemWithPrice{
				{SkuUUID: "sku-1", Quantity: 2, LockedPricePerSecond: sdk.NewCoin("upwr", sdkmath.NewInt(100))},
			},
		},
		{
			name: "multiple items",
			items: []types.LeaseItem{
				{SkuUuid: "sku-1", Quantity: 2, LockedPrice: sdk.NewCoin("upwr", sdkmath.NewInt(100))},
				{SkuUuid: "sku-2", Quantity: 5, LockedPrice: sdk.NewCoin("umfx", sdkmath.NewInt(200))},
				{SkuUuid: "sku-3", Quantity: 1, LockedPrice: sdk.NewCoin("upwr", sdkmath.NewInt(50))},
			},
			expected: []keeper.LeaseItemWithPrice{
				{SkuUUID: "sku-1", Quantity: 2, LockedPricePerSecond: sdk.NewCoin("upwr", sdkmath.NewInt(100))},
				{SkuUUID: "sku-2", Quantity: 5, LockedPricePerSecond: sdk.NewCoin("umfx", sdkmath.NewInt(200))},
				{SkuUUID: "sku-3", Quantity: 1, LockedPricePerSecond: sdk.NewCoin("upwr", sdkmath.NewInt(50))},
			},
		},
		{
			name: "preserves zero quantity",
			items: []types.LeaseItem{
				{SkuUuid: "sku-1", Quantity: 0, LockedPrice: sdk.NewCoin("upwr", sdkmath.NewInt(100))},
			},
			expected: []keeper.LeaseItemWithPrice{
				{SkuUUID: "sku-1", Quantity: 0, LockedPricePerSecond: sdk.NewCoin("upwr", sdkmath.NewInt(100))},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := keeper.LeaseItemsToWithPrice(tc.items)

			require.Len(t, result, len(tc.expected))
			for i, item := range result {
				require.Equal(t, tc.expected[i].SkuUUID, item.SkuUUID)
				require.Equal(t, tc.expected[i].Quantity, item.Quantity)
				require.True(t, tc.expected[i].LockedPricePerSecond.Equal(item.LockedPricePerSecond),
					"item %d: expected %s, got %s", i, tc.expected[i].LockedPricePerSecond, item.LockedPricePerSecond)
			}
		})
	}
}

// ============================================================================
// CalculateTransferAmounts Tests
// ============================================================================

func TestCalculateTransferAmounts(t *testing.T) {
	tests := []struct {
		name      string
		accrued   sdk.Coins
		available sdk.Coins
		expected  sdk.Coins
	}{
		{
			name:      "accrued equals available",
			accrued:   sdk.NewCoins(sdk.NewCoin("upwr", sdkmath.NewInt(100))),
			available: sdk.NewCoins(sdk.NewCoin("upwr", sdkmath.NewInt(100))),
			expected:  sdk.NewCoins(sdk.NewCoin("upwr", sdkmath.NewInt(100))),
		},
		{
			name:      "accrued less than available",
			accrued:   sdk.NewCoins(sdk.NewCoin("upwr", sdkmath.NewInt(50))),
			available: sdk.NewCoins(sdk.NewCoin("upwr", sdkmath.NewInt(100))),
			expected:  sdk.NewCoins(sdk.NewCoin("upwr", sdkmath.NewInt(50))),
		},
		{
			name:      "accrued greater than available (capped)",
			accrued:   sdk.NewCoins(sdk.NewCoin("upwr", sdkmath.NewInt(200))),
			available: sdk.NewCoins(sdk.NewCoin("upwr", sdkmath.NewInt(100))),
			expected:  sdk.NewCoins(sdk.NewCoin("upwr", sdkmath.NewInt(100))),
		},
		{
			name:      "zero available returns empty",
			accrued:   sdk.NewCoins(sdk.NewCoin("upwr", sdkmath.NewInt(100))),
			available: sdk.NewCoins(),
			expected:  sdk.NewCoins(),
		},
		{
			name:      "zero accrued returns empty",
			accrued:   sdk.NewCoins(),
			available: sdk.NewCoins(sdk.NewCoin("upwr", sdkmath.NewInt(100))),
			expected:  sdk.NewCoins(),
		},
		{
			name:      "both empty returns empty",
			accrued:   sdk.NewCoins(),
			available: sdk.NewCoins(),
			expected:  sdk.NewCoins(),
		},
		{
			name: "multi-denom: all accrued available",
			accrued: sdk.NewCoins(
				sdk.NewCoin("upwr", sdkmath.NewInt(100)),
				sdk.NewCoin("umfx", sdkmath.NewInt(50)),
			),
			available: sdk.NewCoins(
				sdk.NewCoin("upwr", sdkmath.NewInt(200)),
				sdk.NewCoin("umfx", sdkmath.NewInt(100)),
			),
			expected: sdk.NewCoins(
				sdk.NewCoin("umfx", sdkmath.NewInt(50)),
				sdk.NewCoin("upwr", sdkmath.NewInt(100)),
			),
		},
		{
			name: "multi-denom: partial availability",
			accrued: sdk.NewCoins(
				sdk.NewCoin("upwr", sdkmath.NewInt(100)),
				sdk.NewCoin("umfx", sdkmath.NewInt(200)),
			),
			available: sdk.NewCoins(
				sdk.NewCoin("upwr", sdkmath.NewInt(80)),
				sdk.NewCoin("umfx", sdkmath.NewInt(50)),
			),
			expected: sdk.NewCoins(
				sdk.NewCoin("umfx", sdkmath.NewInt(50)),
				sdk.NewCoin("upwr", sdkmath.NewInt(80)),
			),
		},
		{
			name: "multi-denom: one denom missing from available",
			accrued: sdk.NewCoins(
				sdk.NewCoin("upwr", sdkmath.NewInt(100)),
				sdk.NewCoin("umfx", sdkmath.NewInt(50)),
			),
			available: sdk.NewCoins(
				sdk.NewCoin("upwr", sdkmath.NewInt(100)),
			),
			expected: sdk.NewCoins(
				sdk.NewCoin("upwr", sdkmath.NewInt(100)),
			),
		},
		{
			name: "multi-denom: extra denom in available ignored",
			accrued: sdk.NewCoins(
				sdk.NewCoin("upwr", sdkmath.NewInt(100)),
			),
			available: sdk.NewCoins(
				sdk.NewCoin("upwr", sdkmath.NewInt(100)),
				sdk.NewCoin("umfx", sdkmath.NewInt(500)),
			),
			expected: sdk.NewCoins(
				sdk.NewCoin("upwr", sdkmath.NewInt(100)),
			),
		},
		{
			name:      "large amounts",
			accrued:   sdk.NewCoins(sdk.NewCoin("upwr", sdkmath.NewInt(1_000_000_000_000))),
			available: sdk.NewCoins(sdk.NewCoin("upwr", sdkmath.NewInt(500_000_000_000))),
			expected:  sdk.NewCoins(sdk.NewCoin("upwr", sdkmath.NewInt(500_000_000_000))),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := keeper.CalculateTransferAmounts(tc.accrued, tc.available)
			require.True(t, tc.expected.Equal(result),
				"expected %s, got %s", tc.expected, result)
		})
	}
}

// ============================================================================
// PerformSettlement Integration Tests
// ============================================================================

func TestPerformSettlement(t *testing.T) {
	f := initFixture(t)

	k := f.App.BillingKeeper
	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600) // 1 per second

	// Fund tenant credit account
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(10000))))

	// Create active lease
	now := f.Ctx.BlockTime()
	lease := types.Lease{
		Uuid:         "01912345-6789-7abc-8def-0123456789ab",
		Tenant:       tenant.String(),
		ProviderUuid: provider.Uuid,
		Items: []types.LeaseItem{
			{SkuUuid: sku.Uuid, Quantity: 1, LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(1))},
		},
		State:         types.LEASE_STATE_ACTIVE,
		CreatedAt:     now,
		LastSettledAt: now,
	}
	err = k.SetLease(f.Ctx, lease)
	require.NoError(t, err)

	tests := []struct {
		name           string
		settleTime     time.Time
		expectedAccrue int64
		expectErr      bool
	}{
		{
			name:           "zero duration (same time)",
			settleTime:     now,
			expectedAccrue: 0,
			expectErr:      false,
		},
		{
			name:           "negative duration (past time)",
			settleTime:     now.Add(-time.Hour),
			expectedAccrue: 0,
			expectErr:      false,
		},
		{
			name:           "100 seconds accrual",
			settleTime:     now.Add(100 * time.Second),
			expectedAccrue: 100, // 1 per second * 1 quantity * 100 seconds
			expectErr:      false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := k.PerformSettlement(f.Ctx, &lease, tc.settleTime)

			if tc.expectErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, result)

			if tc.expectedAccrue == 0 {
				require.True(t, result.AccruedAmounts.IsZero())
				require.True(t, result.TransferAmounts.IsZero())
			} else {
				require.Equal(t, tc.expectedAccrue, result.AccruedAmounts.AmountOf(testDenom).Int64())
			}
		})
	}
}

func TestPerformSettlement_InsufficientCredit(t *testing.T) {
	f := initFixture(t)

	k := f.App.BillingKeeper
	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600) // 1 per second

	// Fund tenant credit account with only 50 tokens
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(50))))

	// Create active lease
	now := f.Ctx.BlockTime()
	lease := types.Lease{
		Uuid:         "01912345-6789-7abc-8def-0123456789ab",
		Tenant:       tenant.String(),
		ProviderUuid: provider.Uuid,
		Items: []types.LeaseItem{
			{SkuUuid: sku.Uuid, Quantity: 1, LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(1))},
		},
		State:         types.LEASE_STATE_ACTIVE,
		CreatedAt:     now,
		LastSettledAt: now,
	}
	err = k.SetLease(f.Ctx, lease)
	require.NoError(t, err)

	// Try to settle for 100 seconds (100 tokens accrued, only 50 available)
	result, err := k.PerformSettlement(f.Ctx, &lease, now.Add(100*time.Second))
	require.NoError(t, err)

	// Should accrue 100 but transfer only 50
	require.Equal(t, int64(100), result.AccruedAmounts.AmountOf(testDenom).Int64())
	require.Equal(t, int64(50), result.TransferAmounts.AmountOf(testDenom).Int64())
	require.True(t, result.CreditBalanceAfter.IsZero())
}

func TestPerformSettlement_ProviderNotFound(t *testing.T) {
	f := initFixture(t)

	k := f.App.BillingKeeper
	tenant := f.TestAccs[0]

	// Fund tenant credit account
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(10000))))

	// Create lease with non-existent provider
	now := f.Ctx.BlockTime()
	lease := types.Lease{
		Uuid:         "01912345-6789-7abc-8def-0123456789ab",
		Tenant:       tenant.String(),
		ProviderUuid: "01912345-6789-7abc-8def-nonexistent",
		Items: []types.LeaseItem{
			{SkuUuid: "01912345-6789-7abc-8def-0123456789ae", Quantity: 1, LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(1))},
		},
		State:         types.LEASE_STATE_ACTIVE,
		CreatedAt:     now,
		LastSettledAt: now,
	}

	// Settlement should fail due to missing provider
	_, err = k.PerformSettlement(f.Ctx, &lease, now.Add(100*time.Second))
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

// ============================================================================
// PerformSettlementSilent Tests
// ============================================================================

func TestPerformSettlementSilent_OverflowHandling(t *testing.T) {
	f := initFixture(t)

	k := f.App.BillingKeeper
	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]

	// Create provider
	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())

	// Fund tenant credit account
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(10000))))

	// Create lease with very long duration that would cause overflow
	now := f.Ctx.BlockTime()
	lease := types.Lease{
		Uuid:         "01912345-6789-7abc-8def-0123456789ab",
		Tenant:       tenant.String(),
		ProviderUuid: provider.Uuid,
		Items: []types.LeaseItem{
			{SkuUuid: "01912345-6789-7abc-8def-0123456789ae", Quantity: 1, LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(1))},
		},
		State:         types.LEASE_STATE_ACTIVE,
		CreatedAt:     now,
		LastSettledAt: now,
	}
	err = k.SetLease(f.Ctx, lease)
	require.NoError(t, err)

	// Try to settle for 101+ years (exceeds MaxDurationSeconds)
	veryFarFuture := now.Add(101 * 365 * 24 * time.Hour)

	// PerformSettlementSilent should NOT error on overflow
	result, err := k.PerformSettlementSilent(f.Ctx, &lease, veryFarFuture)
	require.NoError(t, err)
	require.NotNil(t, result)

	// On overflow, all remaining credit should be transferred to the provider
	// (accrued amount would far exceed balance, so provider gets everything)
	require.Equal(t, int64(10000), result.AccruedAmounts.AmountOf(testDenom).Int64())
	require.Equal(t, int64(10000), result.TransferAmounts.AmountOf(testDenom).Int64())
	require.True(t, result.CreditBalanceAfter.IsZero())
}

func TestPerformSettlementSilent_NormalOperation(t *testing.T) {
	f := initFixture(t)

	k := f.App.BillingKeeper
	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Fund tenant credit account
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(10000))))

	now := f.Ctx.BlockTime()
	lease := types.Lease{
		Uuid:         "01912345-6789-7abc-8def-0123456789ab",
		Tenant:       tenant.String(),
		ProviderUuid: provider.Uuid,
		Items: []types.LeaseItem{
			{SkuUuid: sku.Uuid, Quantity: 1, LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(1))},
		},
		State:         types.LEASE_STATE_ACTIVE,
		CreatedAt:     now,
		LastSettledAt: now,
	}
	err = k.SetLease(f.Ctx, lease)
	require.NoError(t, err)

	// Normal settlement should work the same as PerformSettlement
	result, err := k.PerformSettlementSilent(f.Ctx, &lease, now.Add(100*time.Second))
	require.NoError(t, err)
	require.Equal(t, int64(100), result.AccruedAmounts.AmountOf(testDenom).Int64())
	require.Equal(t, int64(100), result.TransferAmounts.AmountOf(testDenom).Int64())
}
