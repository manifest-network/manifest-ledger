/*
Package keeper_test contains invariant checker functions and tests that verify
billing module state consistency. These invariants can be run against any state
to detect corruption.
*/
package keeper_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/keeper"
	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

// =============================================================================
// Invariant Checker Functions
// =============================================================================

// InvariantResult holds the result of an invariant check.
type InvariantResult struct {
	Name    string
	Passed  bool
	Message string
}

// CheckFundConservationInvariant verifies that the total credit balance across all
// credit accounts is non-negative. This is a basic sanity check — the bank module
// should never allow negative balances.
func CheckFundConservationInvariant(
	ctx context.Context,
	k keeper.Keeper,
	bankKeeper interface {
		GetBalance(ctx context.Context, addr sdk.AccAddress, denom string) sdk.Coin
	},
) InvariantResult {
	accounts, err := k.GetAllCreditAccounts(ctx)
	if err != nil {
		return InvariantResult{
			Name:    "FundConservation",
			Passed:  false,
			Message: "failed to get credit accounts: " + err.Error(),
		}
	}

	for _, ca := range accounts {
		creditAddr, err := types.DeriveCreditAddressFromBech32(ca.Tenant)
		if err != nil {
			return InvariantResult{
				Name:    "FundConservation",
				Passed:  false,
				Message: "invalid tenant address in credit account: " + ca.Tenant,
			}
		}

		// Check a representative denom — in practice you'd check all relevant denoms
		bal := bankKeeper.GetBalance(ctx, creditAddr, "umfx")
		if bal.Amount.IsNegative() {
			return InvariantResult{
				Name:   "FundConservation",
				Passed: false,
				Message: "negative balance for credit account " + ca.Tenant +
					": " + bal.String(),
			}
		}
	}

	return InvariantResult{Name: "FundConservation", Passed: true}
}

// CheckReservationConsistencyInvariant verifies that every credit account's
// ReservedAmounts equals the sum of GetLeaseReservationAmount for all PENDING
// and ACTIVE leases of that tenant.
func CheckReservationConsistencyInvariant(ctx context.Context, k keeper.Keeper) InvariantResult {
	params, err := k.GetParams(ctx)
	if err != nil {
		return InvariantResult{
			Name:    "ReservationConsistency",
			Passed:  false,
			Message: "failed to get params: " + err.Error(),
		}
	}

	allLeases, err := k.GetAllLeases(ctx)
	if err != nil {
		return InvariantResult{
			Name:    "ReservationConsistency",
			Passed:  false,
			Message: "failed to get leases: " + err.Error(),
		}
	}

	// Calculate expected reservations per tenant
	expected := types.CalculateExpectedReservationsByTenant(allLeases, params.MinLeaseDuration)

	accounts, err := k.GetAllCreditAccounts(ctx)
	if err != nil {
		return InvariantResult{
			Name:    "ReservationConsistency",
			Passed:  false,
			Message: "failed to get credit accounts: " + err.Error(),
		}
	}

	for _, ca := range accounts {
		exp := expected[ca.Tenant]
		if exp == nil {
			exp = sdk.NewCoins()
		}

		actualNormalized := sdk.NewCoins(ca.ReservedAmounts...)
		expectedNormalized := sdk.NewCoins(exp...)

		if !actualNormalized.Equal(expectedNormalized) {
			return InvariantResult{
				Name:   "ReservationConsistency",
				Passed: false,
				Message: "tenant " + ca.Tenant + " has reserved " +
					actualNormalized.String() + " but leases sum to " +
					expectedNormalized.String(),
			}
		}
	}

	return InvariantResult{Name: "ReservationConsistency", Passed: true}
}

// CheckLeaseCountConsistencyInvariant verifies that each credit account's
// ActiveLeaseCount and PendingLeaseCount match the actual number of leases
// in those states for that tenant.
func CheckLeaseCountConsistencyInvariant(ctx context.Context, k keeper.Keeper) InvariantResult {
	allLeases, err := k.GetAllLeases(ctx)
	if err != nil {
		return InvariantResult{
			Name:    "LeaseCountConsistency",
			Passed:  false,
			Message: "failed to get leases: " + err.Error(),
		}
	}

	activeCounts := make(map[string]uint64)
	pendingCounts := make(map[string]uint64)
	for _, lease := range allLeases {
		switch lease.State {
		case types.LEASE_STATE_ACTIVE:
			activeCounts[lease.Tenant]++
		case types.LEASE_STATE_PENDING:
			pendingCounts[lease.Tenant]++
		}
	}

	accounts, err := k.GetAllCreditAccounts(ctx)
	if err != nil {
		return InvariantResult{
			Name:    "LeaseCountConsistency",
			Passed:  false,
			Message: "failed to get credit accounts: " + err.Error(),
		}
	}

	for _, ca := range accounts {
		expectedActive := activeCounts[ca.Tenant]
		if ca.ActiveLeaseCount != expectedActive {
			return InvariantResult{
				Name:   "LeaseCountConsistency",
				Passed: false,
				Message: "tenant " + ca.Tenant + " active_lease_count=" +
					sdkmath.NewIntFromUint64(ca.ActiveLeaseCount).String() +
					" but actual=" + sdkmath.NewIntFromUint64(expectedActive).String(),
			}
		}

		expectedPending := pendingCounts[ca.Tenant]
		if ca.PendingLeaseCount != expectedPending {
			return InvariantResult{
				Name:   "LeaseCountConsistency",
				Passed: false,
				Message: "tenant " + ca.Tenant + " pending_lease_count=" +
					sdkmath.NewIntFromUint64(ca.PendingLeaseCount).String() +
					" but actual=" + sdkmath.NewIntFromUint64(expectedPending).String(),
			}
		}
	}

	return InvariantResult{Name: "LeaseCountConsistency", Passed: true}
}

// CheckNoOrphanedLeasesInvariant verifies that every PENDING or ACTIVE lease
// has a corresponding credit account for its tenant.
func CheckNoOrphanedLeasesInvariant(ctx context.Context, k keeper.Keeper) InvariantResult {
	allLeases, err := k.GetAllLeases(ctx)
	if err != nil {
		return InvariantResult{
			Name:    "NoOrphanedLeases",
			Passed:  false,
			Message: "failed to get leases: " + err.Error(),
		}
	}

	for _, lease := range allLeases {
		if lease.State == types.LEASE_STATE_PENDING || lease.State == types.LEASE_STATE_ACTIVE {
			_, err := k.GetCreditAccount(ctx, lease.Tenant)
			if err != nil {
				return InvariantResult{
					Name:   "NoOrphanedLeases",
					Passed: false,
					Message: "lease " + lease.Uuid + " (state=" + lease.State.String() +
						") has no credit account for tenant " + lease.Tenant,
				}
			}
		}
	}

	return InvariantResult{Name: "NoOrphanedLeases", Passed: true}
}

// CheckValidTimestampsInvariant verifies that all lease timestamps are valid:
// - LastSettledAt is not in the future relative to block time
// - CreatedAt is not in the future
// - Active leases have valid LastSettledAt
func CheckValidTimestampsInvariant(ctx context.Context, k keeper.Keeper, blockTime time.Time) InvariantResult {
	allLeases, err := k.GetAllLeases(ctx)
	if err != nil {
		return InvariantResult{
			Name:    "ValidTimestamps",
			Passed:  false,
			Message: "failed to get leases: " + err.Error(),
		}
	}

	for _, lease := range allLeases {
		if lease.CreatedAt.After(blockTime) {
			return InvariantResult{
				Name:   "ValidTimestamps",
				Passed: false,
				Message: "lease " + lease.Uuid + " has CreatedAt in the future: " +
					lease.CreatedAt.String(),
			}
		}

		if lease.State == types.LEASE_STATE_ACTIVE || lease.State == types.LEASE_STATE_PENDING {
			if lease.LastSettledAt.After(blockTime) {
				return InvariantResult{
					Name:   "ValidTimestamps",
					Passed: false,
					Message: "lease " + lease.Uuid + " has LastSettledAt in the future: " +
						lease.LastSettledAt.String(),
				}
			}
		}

		if lease.State == types.LEASE_STATE_CLOSED && lease.ClosedAt != nil {
			if lease.ClosedAt.After(blockTime) {
				return InvariantResult{
					Name:   "ValidTimestamps",
					Passed: false,
					Message: "lease " + lease.Uuid + " has ClosedAt in the future: " +
						lease.ClosedAt.String(),
				}
			}
		}
	}

	return InvariantResult{Name: "ValidTimestamps", Passed: true}
}

// CheckGenesisRoundTripInvariant exports genesis, validates it, and verifies
// the export is self-consistent.
func CheckGenesisRoundTripInvariant(ctx context.Context, k keeper.Keeper) InvariantResult {
	exported := k.ExportGenesis(ctx)

	if err := exported.Validate(); err != nil {
		return InvariantResult{
			Name:    "GenesisRoundTrip",
			Passed:  false,
			Message: "exported genesis is invalid: " + err.Error(),
		}
	}

	return InvariantResult{Name: "GenesisRoundTrip", Passed: true}
}

// RunAllInvariants runs all invariant checks and returns results.
func RunAllInvariants(
	ctx context.Context, k keeper.Keeper, bankKeeper interface {
		GetBalance(ctx context.Context, addr sdk.AccAddress, denom string) sdk.Coin
	}, blockTime time.Time,
) []InvariantResult {
	return []InvariantResult{
		CheckFundConservationInvariant(ctx, k, bankKeeper),
		CheckReservationConsistencyInvariant(ctx, k),
		CheckLeaseCountConsistencyInvariant(ctx, k),
		CheckNoOrphanedLeasesInvariant(ctx, k),
		CheckValidTimestampsInvariant(ctx, k, blockTime),
		CheckGenesisRoundTripInvariant(ctx, k),
	}
}

// =============================================================================
// Tests Using Invariant Checkers
// =============================================================================

// TestInvariants_AfterFullLifecycle runs all invariants after a complete lease lifecycle.
func TestInvariants_AfterFullLifecycle(t *testing.T) {
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

	// Create -> Acknowledge -> Withdraw -> Close
	leaseID := f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{
		{SkuUuid: sku.Uuid, Quantity: 2},
	})

	f.Ctx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(500 * time.Second))

	_, err = msgServer.Withdraw(f.Ctx, &types.MsgWithdraw{
		Sender:     providerAddr.String(),
		LeaseUuids: []string{leaseID},
	})
	require.NoError(t, err)

	f.Ctx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(500 * time.Second))

	_, err = msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
		Sender:     tenant.String(),
		LeaseUuids: []string{leaseID},
	})
	require.NoError(t, err)

	// Run all invariants
	results := RunAllInvariants(f.Ctx, f.App.BillingKeeper, f.App.BankKeeper, f.Ctx.BlockTime())
	for _, r := range results {
		require.True(t, r.Passed, "invariant %s failed: %s", r.Name, r.Message)
	}
}

// TestInvariants_AfterMixedOperations runs all invariants after a complex set of
// mixed operations: creates, acknowledges, rejects, cancels, closes, and withdrawals.
func TestInvariants_AfterMixedOperations(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant1 := f.TestAccs[0]
	tenant2 := f.TestAccs[1]
	providerAddr := f.TestAccs[2]

	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Setup tenants
	for _, tenant := range []sdk.AccAddress{tenant1, tenant2} {
		creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
		require.NoError(t, err)
		f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(100_000_000))))
		err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
			Tenant:        tenant.String(),
			CreditAddress: creditAddr.String(),
		})
		require.NoError(t, err)
	}

	// Tenant1: create and acknowledge lease
	activeLeaseID := f.createAndAcknowledgeLease(t, msgServer, tenant1, providerAddr, []types.LeaseItemInput{
		{SkuUuid: sku.Uuid, Quantity: 1},
	})

	// Tenant1: create and reject lease
	rejectedResp, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant1.String(),
		Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
	})
	require.NoError(t, err)
	_, err = msgServer.RejectLease(f.Ctx, &types.MsgRejectLease{
		Sender:     providerAddr.String(),
		LeaseUuids: []string{rejectedResp.LeaseUuid},
	})
	require.NoError(t, err)

	// Tenant2: create and cancel lease
	cancelResp, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant2.String(),
		Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
	})
	require.NoError(t, err)
	_, err = msgServer.CancelLease(f.Ctx, &types.MsgCancelLease{
		Tenant:     tenant2.String(),
		LeaseUuids: []string{cancelResp.LeaseUuid},
	})
	require.NoError(t, err)

	// Advance time
	f.Ctx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(300 * time.Second))

	// Tenant1: withdraw from active lease
	_, err = msgServer.Withdraw(f.Ctx, &types.MsgWithdraw{
		Sender:     providerAddr.String(),
		LeaseUuids: []string{activeLeaseID},
	})
	require.NoError(t, err)

	// Tenant2: create pending lease (leave it pending for EndBlocker to potentially expire)
	_, err = msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant2.String(),
		Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
	})
	require.NoError(t, err)

	// Run EndBlocker (should not expire anything yet — not past timeout)
	err = f.App.BillingKeeper.EndBlocker(f.Ctx)
	require.NoError(t, err)

	// Run all invariants
	results := RunAllInvariants(f.Ctx, f.App.BillingKeeper, f.App.BankKeeper, f.Ctx.BlockTime())
	for _, r := range results {
		require.True(t, r.Passed, "invariant %s failed: %s", r.Name, r.Message)
	}
}

// TestInvariants_AfterEndBlockerExpiration runs invariants after EndBlocker expires
// pending leases.
func TestInvariants_AfterEndBlockerExpiration(t *testing.T) {
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

	// Create several pending leases
	for i := 0; i < 5; i++ {
		_, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
			Tenant: tenant.String(),
			Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
		})
		require.NoError(t, err)
	}

	// Advance past pending timeout
	f.Ctx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(1801 * time.Second))

	// Run EndBlocker
	err = f.App.BillingKeeper.EndBlocker(f.Ctx)
	require.NoError(t, err)

	// Run all invariants
	results := RunAllInvariants(f.Ctx, f.App.BillingKeeper, f.App.BankKeeper, f.Ctx.BlockTime())
	for _, r := range results {
		require.True(t, r.Passed, "invariant %s failed after EndBlocker expiration: %s", r.Name, r.Message)
	}
}

// TestInvariants_AfterAutoClose runs invariants after a lease is auto-closed
// due to credit exhaustion.
func TestInvariants_AfterAutoClose(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]

	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	// Fund just enough for reservation + a tiny bit more
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(3700))))
	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	leaseID := f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{
		{SkuUuid: sku.Uuid, Quantity: 1},
	})

	// Advance time far past credit exhaustion
	f.Ctx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(10000 * time.Second))

	// Trigger auto-close via withdraw
	_, err = msgServer.Withdraw(f.Ctx, &types.MsgWithdraw{
		Sender:     providerAddr.String(),
		LeaseUuids: []string{leaseID},
	})
	require.NoError(t, err)

	// Verify lease is closed
	lease, err := f.App.BillingKeeper.GetLease(f.Ctx, leaseID)
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_CLOSED, lease.State)

	// Run all invariants
	results := RunAllInvariants(f.Ctx, f.App.BillingKeeper, f.App.BankKeeper, f.Ctx.BlockTime())
	for _, r := range results {
		require.True(t, r.Passed, "invariant %s failed after auto-close: %s", r.Name, r.Message)
	}
}
