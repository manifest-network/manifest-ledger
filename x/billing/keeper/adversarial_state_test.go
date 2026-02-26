/*
Package keeper_test contains adversarial tests targeting state corruption in the billing module.

These tests attempt to corrupt internal state: orphaned records, reservation drift,
lease count inconsistencies, and genesis round-trip fidelity.
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
// Reservation Consistency
// =============================================================================

// TestAdversarial_ReservationMatchesAfterCreateRejectCycle verifies that creating
// a lease then rejecting it returns reserved amounts to exactly zero.
func TestAdversarial_ReservationMatchesAfterCreateRejectCycle(t *testing.T) {
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

	// Create a lease (reserves credit)
	createResp, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
	})
	require.NoError(t, err)

	// Check reservation is non-zero
	ca, err := f.App.BillingKeeper.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)
	require.False(t, ca.ReservedAmounts.IsZero(), "reservation should be set after lease creation")

	// Reject the lease (should release reservation)
	_, err = msgServer.RejectLease(f.Ctx, &types.MsgRejectLease{
		Sender:     providerAddr.String(),
		LeaseUuids: []string{createResp.LeaseUuid},
	})
	require.NoError(t, err)

	// Verify reservation is back to zero
	ca, err = f.App.BillingKeeper.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)
	require.True(t, ca.ReservedAmounts.IsZero(),
		"reservation should be zero after reject, got: %s", ca.ReservedAmounts)
	require.Equal(t, uint64(0), ca.PendingLeaseCount)
	require.Equal(t, uint64(0), ca.ActiveLeaseCount)
}

// TestAdversarial_ReservationMatchesAfterCreateCancelCycle is the same but for tenant cancellation.
func TestAdversarial_ReservationMatchesAfterCreateCancelCycle(t *testing.T) {
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

	createResp, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
	})
	require.NoError(t, err)

	// Cancel the lease
	_, err = msgServer.CancelLease(f.Ctx, &types.MsgCancelLease{
		Tenant:     tenant.String(),
		LeaseUuids: []string{createResp.LeaseUuid},
	})
	require.NoError(t, err)

	ca, err := f.App.BillingKeeper.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)
	require.True(t, ca.ReservedAmounts.IsZero(),
		"reservation should be zero after cancel, got: %s", ca.ReservedAmounts)
}

// TestAdversarial_ReservationMatchesAfterFullLifecycle tests the full lifecycle:
// create -> acknowledge -> close. Reservation must be zero at the end.
func TestAdversarial_ReservationMatchesAfterFullLifecycle(t *testing.T) {
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

	// Reservation should still be held (ACTIVE leases maintain reservations)
	ca, err := f.App.BillingKeeper.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)
	require.False(t, ca.ReservedAmounts.IsZero(), "active lease should maintain reservation")

	f.Ctx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(100 * time.Second))

	// Close the lease
	_, err = msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
		Sender:     tenant.String(),
		LeaseUuids: []string{leaseID},
	})
	require.NoError(t, err)

	// Reservation must be zero
	ca, err = f.App.BillingKeeper.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)
	require.True(t, ca.ReservedAmounts.IsZero(),
		"reservation should be zero after close, got: %s", ca.ReservedAmounts)
	require.Equal(t, uint64(0), ca.ActiveLeaseCount)
	require.Equal(t, uint64(0), ca.PendingLeaseCount)
}

// TestAdversarial_MultipleLeaseReservationAccumulation tests that creating multiple
// leases accumulates reservations correctly and closing them all returns to zero.
func TestAdversarial_MultipleLeaseReservationAccumulation(t *testing.T) {
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

	// Create 5 leases
	var leaseIDs []string
	for i := 0; i < 5; i++ {
		leaseID := f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{
			{SkuUuid: sku.Uuid, Quantity: uint64(i + 1)}, //nolint:gosec // test code, i is [0,4]
		})
		leaseIDs = append(leaseIDs, leaseID)
	}

	// Verify accumulated reservation
	ca, err := f.App.BillingKeeper.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)
	require.Equal(t, uint64(5), ca.ActiveLeaseCount)
	require.False(t, ca.ReservedAmounts.IsZero())

	f.Ctx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(100 * time.Second))

	// Close all leases
	_, err = msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
		Sender:     tenant.String(),
		LeaseUuids: leaseIDs,
	})
	require.NoError(t, err)

	// Everything must be clean
	ca, err = f.App.BillingKeeper.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)
	require.True(t, ca.ReservedAmounts.IsZero(),
		"reservation should be zero after closing all leases, got: %s", ca.ReservedAmounts)
	require.Equal(t, uint64(0), ca.ActiveLeaseCount)
}

// =============================================================================
// Lease Count Consistency
// =============================================================================

// TestAdversarial_LeaseCountAfterExpiration tests that the EndBlocker properly
// decrements pending lease count when leases expire.
func TestAdversarial_LeaseCountAfterExpiration(t *testing.T) {
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

	// Create 3 pending leases
	for i := 0; i < 3; i++ {
		_, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
			Tenant: tenant.String(),
			Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
		})
		require.NoError(t, err)
	}

	ca, err := f.App.BillingKeeper.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)
	require.Equal(t, uint64(3), ca.PendingLeaseCount)

	// Advance past pending timeout (default 1800 seconds)
	f.Ctx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(1801 * time.Second))

	// Run EndBlocker to expire pending leases
	err = f.App.BillingKeeper.EndBlocker(f.Ctx)
	require.NoError(t, err)

	// Verify counts are decremented
	ca, err = f.App.BillingKeeper.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)
	require.Equal(t, uint64(0), ca.PendingLeaseCount,
		"pending lease count should be 0 after expiration, got %d", ca.PendingLeaseCount)

	// Verify reservations released
	require.True(t, ca.ReservedAmounts.IsZero(),
		"reservations should be released after expiration, got: %s", ca.ReservedAmounts)
}

// =============================================================================
// Genesis Round-Trip
// =============================================================================

// TestAdversarial_GenesisExportImportRoundTrip tests that exporting genesis and
// re-importing it produces identical state. This is critical for chain upgrades.
func TestAdversarial_GenesisExportImportRoundTrip(t *testing.T) {
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

	// Create various lease states
	// 1. Active lease
	activeLeaseID := f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{
		{SkuUuid: sku.Uuid, Quantity: 2},
	})

	// 2. Pending lease
	pendingResp, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
	})
	require.NoError(t, err)

	// 3. Closed lease
	closedLeaseID := f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{
		{SkuUuid: sku.Uuid, Quantity: 1},
	})
	f.Ctx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(100 * time.Second))
	_, err = msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
		Sender:     tenant.String(),
		LeaseUuids: []string{closedLeaseID},
	})
	require.NoError(t, err)

	// Export genesis
	exported := f.App.BillingKeeper.ExportGenesis(f.Ctx)
	require.NotNil(t, exported)

	// Validate the exported genesis
	err = exported.Validate()
	require.NoError(t, err, "exported genesis should be valid")

	// Validate with block time
	err = exported.ValidateWithBlockTime(f.Ctx.BlockTime())
	require.NoError(t, err, "exported genesis should pass block time validation")

	// Verify lease states in export
	leasesByUUID := make(map[string]types.Lease)
	for _, l := range exported.Leases {
		leasesByUUID[l.Uuid] = l
	}

	require.Equal(t, types.LEASE_STATE_ACTIVE, leasesByUUID[activeLeaseID].State)
	require.Equal(t, types.LEASE_STATE_PENDING, leasesByUUID[pendingResp.LeaseUuid].State)
	require.Equal(t, types.LEASE_STATE_CLOSED, leasesByUUID[closedLeaseID].State)

	// Verify credit accounts in export
	require.Len(t, exported.CreditAccounts, 1)
	require.Equal(t, tenant.String(), exported.CreditAccounts[0].Tenant)
}

// TestAdversarial_GenesisValidationRejectsMismatchedReservations tests that genesis
// validation catches reservation/lease mismatches.
func TestAdversarial_GenesisValidationRejectsMismatchedReservations(t *testing.T) {
	f := initFixture(t)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]

	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)

	// Create a genesis with an active lease but WRONG reserved amounts
	gs := &types.GenesisState{
		Params: types.DefaultParams(),
		Leases: []types.Lease{
			{
				Uuid:                       testLeaseUUID1,
				Tenant:                     tenant.String(),
				ProviderUuid:               provider.Uuid,
				Items:                      []types.LeaseItem{{SkuUuid: sku.Uuid, Quantity: 1, LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(100))}},
				State:                      types.LEASE_STATE_ACTIVE,
				CreatedAt:                  f.Ctx.BlockTime(),
				LastSettledAt:              f.Ctx.BlockTime(),
				MinLeaseDurationAtCreation: 3600,
			},
		},
		CreditAccounts: []types.CreditAccount{
			{
				Tenant:           tenant.String(),
				CreditAddress:    creditAddr.String(),
				ActiveLeaseCount: 1,
				// ReservedAmounts intentionally WRONG (should be 100 * 1 * 3600 = 360000)
				ReservedAmounts: sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(999))),
			},
		},
	}

	err = gs.Validate()
	require.Error(t, err, "genesis with mismatched reservations should be rejected")
	require.Contains(t, err.Error(), "reserved_amounts")
}

// TestAdversarial_GenesisValidationRejectsMismatchedCounts tests that genesis
// validation catches active/pending lease count mismatches.
func TestAdversarial_GenesisValidationRejectsMismatchedCounts(t *testing.T) {
	f := initFixture(t)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]

	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)

	gs := &types.GenesisState{
		Params: types.DefaultParams(),
		Leases: []types.Lease{
			{
				Uuid:                       testLeaseUUID1,
				Tenant:                     tenant.String(),
				ProviderUuid:               provider.Uuid,
				Items:                      []types.LeaseItem{{SkuUuid: sku.Uuid, Quantity: 1, LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(100))}},
				State:                      types.LEASE_STATE_ACTIVE,
				CreatedAt:                  f.Ctx.BlockTime(),
				LastSettledAt:              f.Ctx.BlockTime(),
				MinLeaseDurationAtCreation: 3600,
			},
		},
		CreditAccounts: []types.CreditAccount{
			{
				Tenant:           tenant.String(),
				CreditAddress:    creditAddr.String(),
				ActiveLeaseCount: 5, // WRONG: should be 1
				ReservedAmounts:  sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(360000))),
			},
		},
	}

	err = gs.Validate()
	require.Error(t, err, "genesis with wrong active_lease_count should be rejected")
	require.Contains(t, err.Error(), "active_lease_count")
}

// TestAdversarial_GenesisRejectsDuplicateLeaseUUID tests that genesis rejects
// duplicate lease UUIDs.
func TestAdversarial_GenesisRejectsDuplicateLeaseUUID(t *testing.T) {
	f := initFixture(t)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]

	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	closedAt := f.Ctx.BlockTime()

	gs := &types.GenesisState{
		Params: types.DefaultParams(),
		Leases: []types.Lease{
			{
				Uuid:          testLeaseUUID1,
				Tenant:        tenant.String(),
				ProviderUuid:  provider.Uuid,
				Items:         []types.LeaseItem{{SkuUuid: sku.Uuid, Quantity: 1, LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(100))}},
				State:         types.LEASE_STATE_CLOSED,
				CreatedAt:     f.Ctx.BlockTime(),
				LastSettledAt: f.Ctx.BlockTime(),
				ClosedAt:      &closedAt,
			},
			{
				Uuid:          testLeaseUUID1, // DUPLICATE UUID
				Tenant:        tenant.String(),
				ProviderUuid:  provider.Uuid,
				Items:         []types.LeaseItem{{SkuUuid: sku.Uuid, Quantity: 1, LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(100))}},
				State:         types.LEASE_STATE_CLOSED,
				CreatedAt:     f.Ctx.BlockTime(),
				LastSettledAt: f.Ctx.BlockTime(),
				ClosedAt:      &closedAt,
			},
		},
	}

	err := gs.Validate()
	require.Error(t, err, "genesis with duplicate lease UUIDs should be rejected")
	require.Contains(t, err.Error(), "duplicate lease uuid")
}

// TestAdversarial_GenesisRejectsTenantWithLeaseButNoCreditAccount tests that genesis
// rejects a tenant that has active/pending lease reservations but no credit account.
func TestAdversarial_GenesisRejectsTenantWithLeaseButNoCreditAccount(t *testing.T) {
	f := initFixture(t)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]

	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	gs := &types.GenesisState{
		Params: types.DefaultParams(),
		Leases: []types.Lease{
			{
				Uuid:                       testLeaseUUID1,
				Tenant:                     tenant.String(),
				ProviderUuid:               provider.Uuid,
				Items:                      []types.LeaseItem{{SkuUuid: sku.Uuid, Quantity: 1, LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(100))}},
				State:                      types.LEASE_STATE_ACTIVE,
				CreatedAt:                  f.Ctx.BlockTime(),
				LastSettledAt:              f.Ctx.BlockTime(),
				MinLeaseDurationAtCreation: 3600,
			},
		},
		CreditAccounts: []types.CreditAccount{}, // NO credit account
	}

	err := gs.Validate()
	require.Error(t, err, "genesis should reject active lease with no credit account")
	require.Contains(t, err.Error(), "no credit account")
}

// =============================================================================
// SKU Index Consistency
// =============================================================================

// TestAdversarial_LeaseBySKUIndexConsistencyAfterClose tests that the LeaseBySKU
// index still correctly references leases after they are closed. The index should
// NOT be cleaned up on close (by design - allows querying historical leases by SKU).
func TestAdversarial_LeaseBySKUIndexConsistencyAfterClose(t *testing.T) {
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

	// Verify SKU index has the lease
	leases, err := f.App.BillingKeeper.GetLeasesBySKU(f.Ctx, sku.Uuid)
	require.NoError(t, err)
	require.Len(t, leases, 1)
	require.Equal(t, leaseID, leases[0].Uuid)

	// Close the lease
	f.Ctx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(100 * time.Second))
	_, err = msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
		Sender:     tenant.String(),
		LeaseUuids: []string{leaseID},
	})
	require.NoError(t, err)

	// SKU index should still reference the lease (historical)
	leases, err = f.App.BillingKeeper.GetLeasesBySKU(f.Ctx, sku.Uuid)
	require.NoError(t, err)
	require.Len(t, leases, 1)
	require.Equal(t, leaseID, leases[0].Uuid)
	require.Equal(t, types.LEASE_STATE_CLOSED, leases[0].State)
}

// =============================================================================
// Credit Address Derivation Consistency
// =============================================================================

// TestAdversarial_CreditAddressDerivationDeterminism tests that credit address
// derivation is truly deterministic — same input always produces same output.
func TestAdversarial_CreditAddressDerivationDeterminism(t *testing.T) {
	tenant := sdk.AccAddress([]byte("test-tenant-address1"))

	addr1 := types.DeriveCreditAddress(tenant)
	addr2 := types.DeriveCreditAddress(tenant)
	addr3 := types.DeriveCreditAddress(tenant)

	require.Equal(t, addr1, addr2)
	require.Equal(t, addr2, addr3)

	// Different tenant must produce different address
	tenant2 := sdk.AccAddress([]byte("test-tenant-address2"))
	addr4 := types.DeriveCreditAddress(tenant2)
	require.NotEqual(t, addr1, addr4, "different tenants must have different credit addresses")
}

// TestAdversarial_CreditAddressIndexConsistency tests that the credit address
// reverse index stays consistent through multiple SetCreditAccount calls.
func TestAdversarial_CreditAddressIndexConsistency(t *testing.T) {
	f := initFixture(t)

	tenant := f.TestAccs[0]
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)

	// Set credit account
	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Set it again (should be idempotent)
	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Verify it can still be retrieved
	ca, err := f.App.BillingKeeper.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)
	require.Equal(t, creditAddr.String(), ca.CreditAddress)
}
