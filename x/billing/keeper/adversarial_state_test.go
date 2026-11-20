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

	"cosmossdk.io/collections"
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
	requireInvariants(t, f)
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
	// Quantities 1..5 at 1/s, reserved for an hour each
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(15*3600))), ca.ReservedAmounts)

	// The stream accumulates the same leases: its rate is the sum of theirs
	streamKey := keeper.NewStreamKey(provider.Uuid, tenant, testDenom)
	state, err := f.App.BillingKeeper.Streams.Get(f.Ctx, streamKey)
	require.NoError(t, err)
	require.Equal(t, sdkmath.NewInt(15), state.Rate)
	require.Equal(t, uint64(5), state.LeaseCount)
	requireInvariants(t, f)

	f.Ctx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(100 * time.Second))

	// Close all leases
	resp, err := msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
		Sender:     tenant.String(),
		LeaseUuids: leaseIDs,
	})
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(1500))), resp.TotalSettledAmounts)

	// Everything must be clean
	ca, err = f.App.BillingKeeper.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)
	require.True(t, ca.ReservedAmounts.IsZero(),
		"reservation should be zero after closing all leases, got: %s", ca.ReservedAmounts)
	require.Equal(t, uint64(0), ca.ActiveLeaseCount)
	_, err = f.App.BillingKeeper.Streams.Get(f.Ctx, streamKey)
	require.ErrorIs(t, err, collections.ErrNotFound, "a stream without leases is deleted")
	requireInvariants(t, f)
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
// re-importing it produces identical state, including the settlement streams
// and everything derived from them. This is critical for chain upgrades.
func TestAdversarial_GenesisExportImportRoundTrip(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	poorTenant := f.TestAccs[2]

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

	// A second tenant whose stream is frozen for exhausted credit and still
	// waiting for the EndBlocker when the state is exported.
	poorCreditAddr := types.DeriveCreditAddress(poorTenant)
	f.fundAccount(t, poorCreditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(3600))))
	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        poorTenant.String(),
		CreditAddress: poorCreditAddr.String(),
	})
	require.NoError(t, err)
	frozenLeaseID := f.createAndAcknowledgeLease(t, msgServer, poorTenant, providerAddr, []types.LeaseItemInput{
		{SkuUuid: sku.Uuid, Quantity: 1},
	})
	f.Ctx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(2 * time.Hour))
	_, err = msgServer.Withdraw(f.Ctx, &types.MsgWithdraw{
		Sender:       providerAddr.String(),
		ProviderUuid: provider.Uuid,
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
	// 100.5s at 3/s accrue 301.5: the close pays 301 and leaves half a unit in
	// the stream's remainder, which must survive the export and the import.
	f.Ctx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(100*time.Second + 500*time.Millisecond))
	closeResp, err := msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
		Sender:     tenant.String(),
		LeaseUuids: []string{closedLeaseID},
	})
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(301))), closeResp.TotalSettledAmounts)

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
	require.Equal(t, types.LEASE_STATE_ACTIVE, leasesByUUID[frozenLeaseID].State)
	for _, l := range exported.Leases {
		require.Nil(t, l.LastSettledAt, "the deprecated field is never exported") //nolint:staticcheck // asserting the deprecated field stays unset
	}

	// Verify credit accounts in export
	require.Len(t, exported.CreditAccounts, 2)

	// Verify streams in export: one per tenant, built from the ACTIVE leases only
	// (the pending and the closed lease contribute nothing).
	require.Len(t, exported.Streams, 2)
	streamsByTenant := make(map[string]types.StreamState)
	for _, stream := range exported.Streams {
		require.Equal(t, provider.Uuid, stream.ProviderUuid)
		require.Equal(t, testDenom, stream.Denom)
		streamsByTenant[stream.Tenant] = stream.State
	}
	healthy := streamsByTenant[tenant.String()]
	require.Equal(t, sdkmath.NewInt(2), healthy.Rate)
	require.Equal(t, uint64(1), healthy.LeaseCount)
	require.False(t, healthy.IsExhausted())
	require.Equal(t, f.Ctx.BlockTime(), healthy.LastSettledAt, "settled by the close of the other lease")
	require.Equal(t, sdkmath.NewInt(500_000_000), healthy.Remainder, "the half unit the close could not pay")
	frozen := streamsByTenant[poorTenant.String()]
	require.Equal(t, sdkmath.NewInt(1), frozen.Rate)
	require.Equal(t, uint64(1), frozen.LeaseCount)
	require.True(t, frozen.IsExhausted())
	require.False(t, frozen.Remainder.IsNil(), "an exported remainder is zero, never unset")
	require.True(t, frozen.Remainder.IsZero())

	// Re-import into a fresh chain that knows the same provider and SKU.
	f2 := initFixture(t)
	f2.Ctx = f2.Ctx.WithBlockTime(f.Ctx.BlockTime())
	require.NoError(t, f2.App.SKUKeeper.SetProvider(f2.Ctx, provider))
	require.NoError(t, f2.App.SKUKeeper.SetSKU(f2.Ctx, sku))
	require.NoError(t, f2.App.BillingKeeper.InitGenesis(f2.Ctx, exported))

	// Exporting again yields the same genesis, byte for byte.
	want, err := exported.Marshal()
	require.NoError(t, err)
	got, err := f2.App.BillingKeeper.ExportGenesis(f2.Ctx).Marshal()
	require.NoError(t, err)
	require.Equal(t, want, got, "export -> import -> export must be the identity")

	// What is derived from the streams was rebuilt: membership and reverse
	// lookup (checked by the stream invariant) and the drain queue.
	requireInvariants(t, f2)
	queued, err := f2.App.BillingKeeper.ExhaustedStreams.Has(f2.Ctx, keeper.NewStreamKey(provider.Uuid, poorTenant, testDenom))
	require.NoError(t, err)
	require.True(t, queued, "a frozen stream must be queued for the drain after import")

	// The imported chain carries on where the exported one stopped: its
	// EndBlocker closes the frozen stream's lease, and the healthy stream
	// keeps billing from its last settlement, remainder included.
	f2.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(1_000_000))))
	f2.Ctx = f2.Ctx.WithBlockTime(f2.Ctx.BlockTime().Add(50*time.Second + 250*time.Millisecond))
	require.NoError(t, f2.App.BillingKeeper.EndBlocker(f2.Ctx))
	frozenLease, err := f2.App.BillingKeeper.GetLease(f2.Ctx, frozenLeaseID)
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_CLOSED, frozenLease.State)
	require.Equal(t, types.ClosureReasonCreditExhausted, frozenLease.ClosureReason)

	resp, err := keeper.NewMsgServerImpl(f2.App.BillingKeeper).Withdraw(f2.Ctx, &types.MsgWithdraw{
		Sender:       providerAddr.String(),
		ProviderUuid: provider.Uuid,
	})
	require.NoError(t, err)
	// 2/s for 50.25s accrue 100.5; with the imported half unit that is exactly
	// 101. Without the remainder the provider would get 100.
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(101))), resp.TotalAmounts)
	imported, err := f2.App.BillingKeeper.Streams.Get(f2.Ctx, keeper.NewStreamKey(provider.Uuid, tenant, testDenom))
	require.NoError(t, err)
	require.True(t, imported.Remainder.IsZero())
	requireInvariants(t, f2)
}

// TestAdversarial_GenesisRejectsStreamsThatDoNotMatchLeases tests that a
// genesis whose streams disagree with its ACTIVE leases is rejected, both by
// Validate and by InitGenesis (which does not run Validate). Any such mismatch
// would misprice every later settlement.
func TestAdversarial_GenesisRejectsStreamsThatDoNotMatchLeases(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	otherTenant := f.TestAccs[2]

	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	creditAddr := types.DeriveCreditAddress(tenant)
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(1_000_000))))
	require.NoError(t, f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	}))
	for range 2 {
		f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{
			{SkuUuid: sku.Uuid, Quantity: 1},
		})
	}

	// A valid genesis to corrupt: two ACTIVE leases and their single stream.
	valid := func() *types.GenesisState {
		gs := f.App.BillingKeeper.ExportGenesis(f.Ctx)
		require.NoError(t, gs.Validate())
		require.Len(t, gs.Streams, 1)
		return gs
	}
	future := f.Ctx.BlockTime().Add(time.Hour)

	tests := []struct {
		name    string
		corrupt func(gs *types.GenesisState)
		wantErr string
		// onlyWithBlockTime marks corruptions that only ValidateWithBlockTime
		// (run by InitGenesis) can see.
		onlyWithBlockTime bool
		// notEnforcedOnInit marks corruptions that Validate rejects but that
		// leave a consistent state when imported anyway.
		notEnforcedOnInit bool
	}{
		{
			name:    "active leases without their stream",
			corrupt: func(gs *types.GenesisState) { gs.Streams = nil },
			wantErr: "missing",
		},
		{
			name: "stream rate lower than the leases imply",
			corrupt: func(gs *types.GenesisState) {
				gs.Streams[0].State.Rate = sdkmath.NewInt(1)
			},
			wantErr: "rate",
		},
		{
			name: "stream rate higher than the leases imply",
			corrupt: func(gs *types.GenesisState) {
				gs.Streams[0].State.Rate = sdkmath.NewInt(1_000_000)
			},
			wantErr: "rate",
		},
		{
			name: "stream lease count off by one",
			corrupt: func(gs *types.GenesisState) {
				gs.Streams[0].State.LeaseCount = 3
			},
			wantErr: "lease_count",
		},
		{
			name: "stream for a tenant without active leases",
			corrupt: func(gs *types.GenesisState) {
				extra := gs.Streams[0]
				extra.Tenant = otherTenant.String()
				gs.Streams = append(gs.Streams, extra)
			},
			wantErr: "no active lease",
		},
		{
			name: "stream in a denom no lease is priced in",
			corrupt: func(gs *types.GenesisState) {
				extra := gs.Streams[0]
				extra.Denom = testDenom2
				gs.Streams = append(gs.Streams, extra)
			},
			wantErr: "no active lease",
		},
		{
			name: "duplicate stream",
			corrupt: func(gs *types.GenesisState) {
				gs.Streams = append(gs.Streams, gs.Streams[0])
			},
			wantErr:           "duplicate stream",
			notEnforcedOnInit: true,
		},
		{
			name: "stream left behind by leases that are all closed",
			corrupt: func(gs *types.GenesisState) {
				closedAt := f.Ctx.BlockTime()
				for i := range gs.Leases {
					gs.Leases[i].State = types.LEASE_STATE_CLOSED
					gs.Leases[i].ClosedAt = &closedAt
				}
				gs.CreditAccounts[0].ActiveLeaseCount = 0
				gs.CreditAccounts[0].ReservedAmounts = nil
			},
			wantErr: "no active lease",
		},
		{
			name: "negative remainder",
			corrupt: func(gs *types.GenesisState) {
				gs.Streams[0].State.Remainder = sdkmath.NewInt(-1)
			},
			wantErr: "remainder",
		},
		{
			name: "remainder of a whole unit",
			corrupt: func(gs *types.GenesisState) {
				gs.Streams[0].State.Remainder = sdkmath.NewInt(1_000_000_000)
			},
			wantErr: "remainder",
		},
		{
			name: "stream settled in the future",
			corrupt: func(gs *types.GenesisState) {
				gs.Streams[0].State.LastSettledAt = future
			},
			wantErr:           "in the future",
			onlyWithBlockTime: true,
		},
		{
			name: "stream exhausted in the future",
			corrupt: func(gs *types.GenesisState) {
				gs.Streams[0].State.ExhaustedAt = &future
			},
			wantErr:           "in the future",
			onlyWithBlockTime: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gs := valid()
			tc.corrupt(gs)

			if tc.onlyWithBlockTime {
				err := gs.ValidateWithBlockTime(f.Ctx.BlockTime())
				require.ErrorIs(t, err, types.ErrInvalidLease)
				require.ErrorContains(t, err, tc.wantErr)
			} else {
				err := gs.Validate()
				require.ErrorIs(t, err, types.ErrInvalidLease)
				require.ErrorContains(t, err, tc.wantErr)
			}

			if tc.notEnforcedOnInit {
				return
			}
			// InitChain does not run Validate, so InitGenesis must refuse the
			// same state on its own. Import into a scratch context.
			ctx, _ := f.Ctx.CacheContext()
			require.NoError(t, f.App.BillingKeeper.Streams.Clear(ctx, nil))
			require.NoError(t, f.App.BillingKeeper.StreamsByTenant.Clear(ctx, nil))
			require.NoError(t, f.App.BillingKeeper.StreamLeases.Clear(ctx, nil))
			require.NoError(t, f.App.BillingKeeper.ExhaustedStreams.Clear(ctx, nil))
			require.ErrorIs(t, f.App.BillingKeeper.InitGenesis(ctx, gs), types.ErrInvalidLease)
		})
	}

	// The uncorrupted genesis imports cleanly through the same procedure.
	ctx, _ := f.Ctx.CacheContext()
	require.NoError(t, f.App.BillingKeeper.Streams.Clear(ctx, nil))
	require.NoError(t, f.App.BillingKeeper.StreamsByTenant.Clear(ctx, nil))
	require.NoError(t, f.App.BillingKeeper.StreamLeases.Clear(ctx, nil))
	require.NoError(t, f.App.BillingKeeper.ExhaustedStreams.Clear(ctx, nil))
	require.NoError(t, f.App.BillingKeeper.InitGenesis(ctx, valid()))
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
				Uuid:         testLeaseUUID1,
				Tenant:       tenant.String(),
				ProviderUuid: provider.Uuid,
				Items:        []types.LeaseItem{{SkuUuid: sku.Uuid, Quantity: 1, LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(100))}},
				State:        types.LEASE_STATE_CLOSED,
				CreatedAt:    f.Ctx.BlockTime(),
				ClosedAt:     &closedAt,
			},
			{
				Uuid:         testLeaseUUID1, // DUPLICATE UUID
				Tenant:       tenant.String(),
				ProviderUuid: provider.Uuid,
				Items:        []types.LeaseItem{{SkuUuid: sku.Uuid, Quantity: 1, LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(100))}},
				State:        types.LEASE_STATE_CLOSED,
				CreatedAt:    f.Ctx.BlockTime(),
				ClosedAt:     &closedAt,
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

	// The stream membership index, unlike the SKU index, is not historical: a
	// closed lease is no longer a member of any stream.
	members, err := f.App.BillingKeeper.StreamLeases.Has(f.Ctx,
		collections.Join(keeper.NewStreamKey(provider.Uuid, tenant, testDenom), leaseID))
	require.NoError(t, err)
	require.False(t, members)
	requireInvariants(t, f)
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
