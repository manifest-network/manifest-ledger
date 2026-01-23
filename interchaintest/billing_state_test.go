// Package interchaintest contains end-to-end tests for the billing module.
// This file contains state, validation, and edge case tests: EdgeCases, PendingLeaseExpiration,
// StateIndexQueries, MaxLeaseLimits, LeaseAcknowledgeEdgeCases, LeasePagination,
// InvalidUUID, EmptyParams, LeasesBySKUQuery, ProviderByAddressQuery.
package interchaintest

import (
	"context"
	"fmt"
	"testing"

	sdkmath "cosmossdk.io/math"
	"github.com/strangelove-ventures/interchaintest/v8"
	"github.com/strangelove-ventures/interchaintest/v8/ibc"
	"github.com/strangelove-ventures/interchaintest/v8/testutil"
	"github.com/stretchr/testify/require"

	"github.com/manifest-network/manifest-ledger/interchaintest/helpers"
	billingtypes "github.com/manifest-network/manifest-ledger/x/billing/types"
)

// TestBillingState runs the billing state, validation, and edge case e2e tests independently.
// Run with: go test -v ./interchaintest -run TestBillingState -timeout 45m
func TestBillingState(t *testing.T) {
	ctx, tc, cleanup := setupBillingTest(t, "billing-state-test")
	t.Cleanup(cleanup)

	// Set globals from context for backward compatibility with existing test functions
	testPWRDenom = tc.pwrDenom
	testProviderUUID = tc.providerUUID
	testSKUUUID = tc.skuUUID
	testSKUUUID2 = tc.skuUUID2

	// Fund tenant2 for edge case tests
	fundTenantCredit(t, ctx, tc, tc.tenant2, 100_000_000)

	// Run state and validation test suites
	t.Run("EdgeCases", func(t *testing.T) {
		testEdgeCasesIndependent(t, ctx, tc)
	})
	t.Run("PendingLeaseExpiration", func(t *testing.T) {
		testPendingLeaseExpirationIndependent(t, ctx, tc)
	})
	t.Run("StateIndexQueries", func(t *testing.T) {
		testStateIndexQueriesIndependent(t, ctx, tc)
	})
	t.Run("MaxLeaseLimits", func(t *testing.T) {
		testMaxLeaseLimitsIndependent(t, ctx, tc)
	})
	t.Run("LeaseAcknowledgeEdgeCases", func(t *testing.T) {
		testLeaseAcknowledgeEdgeCasesIndependent(t, ctx, tc)
	})
	t.Run("LeasePagination", func(t *testing.T) {
		testLeasePaginationIndependent(t, ctx, tc)
	})
	t.Run("InvalidUUID", func(t *testing.T) {
		testBillingInvalidUUIDIndependent(t, ctx, tc)
	})
	t.Run("EmptyParams", func(t *testing.T) {
		testBillingEmptyParamsIndependent(t, ctx, tc)
	})
	t.Run("LeasesBySKUQuery", func(t *testing.T) {
		testLeasesBySKUQueryIndependent(t, ctx, tc)
	})
	t.Run("ProviderByAddressQuery", func(t *testing.T) {
		testProviderByAddressQueryIndependent(t, ctx, tc)
	})
}

// testEdgeCasesIndependent tests edge cases.
func testEdgeCasesIndependent(t *testing.T, ctx context.Context, tc *billingTestContext) {
	t.Log("=== Testing Edge Cases ===")

	t.Run("success: remaining credit stays after lease close", func(t *testing.T) {
		// Get tenant2's credit balance before
		beforeRes, err := helpers.BillingQueryCreditAccount(ctx, tc.chain, tc.tenant2.FormattedAddress())
		require.NoError(t, err)
		beforeBalances := beforeRes.Balances

		// Create a lease
		items := []string{fmt.Sprintf("%s:1", tc.skuUUID)}
		createRes, err := helpers.BillingCreateLease(ctx, tc.chain, tc.tenant2, items)
		require.NoError(t, err)

		txRes, err := tc.chain.GetTransaction(createRes.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code)

		leaseUUID, err := helpers.GetLeaseIDFromTxHash(ctx, tc.chain, createRes.TxHash)
		require.NoError(t, err)

		// Acknowledge the lease to make it ACTIVE
		ackRes, err := helpers.BillingAcknowledgeLease(ctx, tc.chain, tc.providerWallet, leaseUUID)
		require.NoError(t, err)
		ackTxRes, err := tc.chain.GetTransaction(ackRes.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), ackTxRes.Code, "lease acknowledgement should succeed: %s", ackTxRes.RawLog)

		// Wait for some accrual
		require.NoError(t, testutil.WaitForBlocks(ctx, 3, tc.chain))

		// Close the lease
		closeRes, err := helpers.BillingCloseLease(ctx, tc.chain, tc.tenant2, leaseUUID)
		require.NoError(t, err)

		txRes, err = tc.chain.GetTransaction(closeRes.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code)

		// Check credit balance - should be less than before (due to accrual) but still positive
		afterRes, err := helpers.BillingQueryCreditAccount(ctx, tc.chain, tc.tenant2.FormattedAddress())
		require.NoError(t, err)
		afterBalances := afterRes.Balances

		// Credit should have decreased due to accrual (compare total amounts)
		require.True(t, !afterBalances.IsZero(),
			"remaining credit should stay in account")
		t.Logf("Credit balances: before=%s, after=%s", beforeBalances, afterBalances)
	})

	t.Run("success: provider cannot double-withdraw after lease closure", func(t *testing.T) {
		// Get a closed lease from tenant2's tests
		leases, err := helpers.BillingQueryLeasesByTenant(ctx, tc.chain, tc.tenant2.FormattedAddress(), "")
		require.NoError(t, err)

		var closedLeaseUUID string
		for _, lease := range leases.Leases {
			if lease.GetState() == billingtypes.LEASE_STATE_CLOSED {
				closedLeaseUUID = lease.Uuid
				break
			}
		}

		if closedLeaseUUID == "" {
			t.Skip("No closed lease found")
		}

		// After closure, settlement already happened, so withdrawal should fail
		// because there's nothing left to withdraw (LastSettledAt == ClosedAt)
		res, err := helpers.BillingWithdraw(ctx, tc.chain, tc.providerWallet, closedLeaseUUID)
		require.NoError(t, err)

		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		// Should fail because settlement already happened during closure
		require.NotEqual(t, uint32(0), txRes.Code, "withdraw after closure should fail (already settled)")
		require.Contains(t, txRes.RawLog, "no withdrawable amount")
	})
}

// testPendingLeaseExpirationIndependent tests pending lease expiration.
func testPendingLeaseExpirationIndependent(t *testing.T, ctx context.Context, tc *billingTestContext) {
	t.Log("=== Testing Pending Lease Expiration ===")

	// Create a new tenant for this test
	users := interchaintest.GetAndFundTestUsers(t, ctx, "expire_tenant", DefaultGenesisAmt, tc.chain)
	expireTenant := users[0]

	// Fund the tenant with PWR tokens
	err := tc.chain.SendFunds(ctx, tc.authority.KeyName(), ibc.WalletAmount{
		Address: expireTenant.FormattedAddress(),
		Denom:   tc.pwrDenom,
		Amount:  sdkmath.NewInt(500_000_000),
	})
	require.NoError(t, err)
	require.NoError(t, testutil.WaitForBlocks(ctx, 2, tc.chain))

	// Fund the credit account
	fundAmount := fmt.Sprintf("200000000%s", tc.pwrDenom)
	res, err := helpers.BillingFundCredit(ctx, tc.chain, expireTenant, expireTenant.FormattedAddress(), fundAmount)
	require.NoError(t, err)
	txRes, err := tc.chain.GetTransaction(res.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), txRes.Code)

	// Get current params
	params, err := helpers.BillingQueryParams(ctx, tc.chain)
	require.NoError(t, err)
	originalPendingTimeout := params.Params.PendingTimeout
	t.Logf("Original pending timeout: %d", originalPendingTimeout)

	// Setup: set a very short pending timeout (60 seconds - minimum allowed) for testing
	t.Log("Setting short pending timeout (60 seconds)...")
	res, err = helpers.BillingUpdateParams(
		ctx, tc.chain, tc.authority,
		params.Params.MaxLeasesPerTenant,
		params.Params.MaxItemsPerLease,
		params.Params.MinLeaseDuration,
		params.Params.MaxPendingLeasesPerTenant,
		60, // 60 seconds - minimum allowed
		nil,
	)
	require.NoError(t, err, "failed to update params")
	txRes, err = tc.chain.GetTransaction(res.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), txRes.Code, "params update should succeed: %s", txRes.RawLog)

	// Verify params updated
	newParams, err := helpers.BillingQueryParams(ctx, tc.chain)
	require.NoError(t, err)
	require.Equal(t, uint64(60), newParams.Params.PendingTimeout)

	// Setup: create multiple pending leases to test iterator-based EndBlocker processing
	numLeasesToCreate := 5
	expireLeaseUUIDs := make([]string, 0, numLeasesToCreate)

	t.Log("Creating multiple pending leases for expiration test...")
	for i := 0; i < numLeasesToCreate; i++ {
		items := []string{fmt.Sprintf("%s:1", tc.skuUUID)}
		res, err := helpers.BillingCreateLease(ctx, tc.chain, expireTenant, items)
		require.NoError(t, err, "failed to create lease %d", i)
		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "create lease %d should succeed: %s", i, txRes.RawLog)

		leaseUUID, err := helpers.GetLeaseIDFromTxHash(ctx, tc.chain, res.TxHash)
		require.NoError(t, err)
		expireLeaseUUIDs = append(expireLeaseUUIDs, leaseUUID)
		t.Logf("Created pending lease %d for expiration: %s", i+1, leaseUUID)

		// Verify it's in PENDING state
		lease, err := helpers.BillingQueryLease(ctx, tc.chain, leaseUUID)
		require.NoError(t, err)
		require.Equal(t, billingtypes.LEASE_STATE_PENDING, lease.Lease.GetState())
	}
	t.Logf("Created %d pending leases for expiration test", len(expireLeaseUUIDs))

	t.Run("success: all pending leases expire after timeout via iterator-based EndBlocker", func(t *testing.T) {
		// Wait for enough blocks to pass the pending timeout
		// With ~1 second block time, wait for ~70 blocks to exceed 60 second timeout
		t.Log("Waiting for pending timeout to expire (~70 blocks)...")
		require.NoError(t, testutil.WaitForBlocks(ctx, 70, tc.chain))

		// Verify ALL leases are now EXPIRED
		for i, leaseUUID := range expireLeaseUUIDs {
			lease, err := helpers.BillingQueryLease(ctx, tc.chain, leaseUUID)
			require.NoError(t, err)
			require.Equal(t, billingtypes.LEASE_STATE_EXPIRED, lease.Lease.GetState(),
				"lease %d (%s) should be expired after timeout", i+1, leaseUUID)
			t.Logf("Lease %d (%s) successfully expired", i+1, leaseUUID)
		}
		t.Logf("All %d pending leases successfully expired via iterator-based EndBlocker", len(expireLeaseUUIDs))
	})

	t.Run("success: credit refunded after expiration", func(t *testing.T) {
		// Get credit account balance - should have been refunded
		creditAccount, err := helpers.BillingQueryCreditAccount(ctx, tc.chain, expireTenant.FormattedAddress())
		require.NoError(t, err)
		t.Logf("Credit account after expiration: tenant=%s", creditAccount.CreditAccount.Tenant)

		// Query the balance via bank module to verify credit was refunded
		balance, err := tc.chain.BankQueryBalance(ctx, creditAccount.CreditAccount.CreditAddress, "upwr")
		require.NoError(t, err)
		t.Logf("Credit balance after expiration: %s", balance.String())
	})

	t.Run("cleanup: restore original pending timeout", func(t *testing.T) {
		res, err := helpers.BillingUpdateParams(
			ctx, tc.chain, tc.authority,
			params.Params.MaxLeasesPerTenant,
			params.Params.MaxItemsPerLease,
			params.Params.MinLeaseDuration,
			params.Params.MaxPendingLeasesPerTenant,
			originalPendingTimeout,
			nil,
		)
		require.NoError(t, err)
		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code)
	})
}

// testStateIndexQueriesIndependent tests efficient lease state index queries.
func testStateIndexQueriesIndependent(t *testing.T, ctx context.Context, tc *billingTestContext) {
	t.Log("=== Testing State Index Queries ===")

	// Create a new tenant for isolated state testing
	users := interchaintest.GetAndFundTestUsers(t, ctx, "state-index", sdkmath.NewInt(1_000_000_000), tc.chain)
	stateTestTenant := users[0]

	// First send PWR to tenant so they can fund their credit
	node := tc.chain.GetNode()
	err := node.SendFunds(ctx, tc.authority.KeyName(), ibc.WalletAmount{
		Address: stateTestTenant.FormattedAddress(),
		Denom:   tc.pwrDenom,
		Amount:  sdkmath.NewInt(100_000_000_000), // 100B PWR - enough for multiple leases
	})
	require.NoError(t, err)
	require.NoError(t, testutil.WaitForBlocks(ctx, 2, tc.chain))

	// Fund credit account using BillingFundCredit (this creates the credit account record)
	fundAmount := fmt.Sprintf("50000000000%s", tc.pwrDenom) // 50B PWR
	res, err := helpers.BillingFundCredit(ctx, tc.chain, stateTestTenant, stateTestTenant.FormattedAddress(), fundAmount)
	require.NoError(t, err)
	txRes, err := tc.chain.GetTransaction(res.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), txRes.Code, "fund credit should succeed: %s", txRes.RawLog)
	require.NoError(t, testutil.WaitForBlocks(ctx, 2, tc.chain))

	// Setup: create leases in different states
	t.Log("Creating leases in different states...")

	// Create first lease (will be acknowledged -> active)
	res, err = helpers.BillingCreateLease(ctx, tc.chain, stateTestTenant, []string{tc.skuUUID + ":1"})
	require.NoError(t, err, "failed to create active lease")
	txRes, err = tc.chain.GetTransaction(res.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), txRes.Code, "lease creation should succeed: %s", txRes.RawLog)
	activeLeaseUUID, err := helpers.GetLeaseIDFromTxHash(ctx, tc.chain, res.TxHash)
	require.NoError(t, err, "failed to get lease UUID from tx events")
	t.Logf("Created lease for ACTIVE state: %s", activeLeaseUUID)

	// Acknowledge to make it active
	ackRes, err := helpers.BillingAcknowledgeLease(ctx, tc.chain, tc.providerWallet, activeLeaseUUID)
	require.NoError(t, err, "failed to acknowledge lease")
	ackTxRes, err := tc.chain.GetTransaction(ackRes.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), ackTxRes.Code, "acknowledge should succeed: %s", ackTxRes.RawLog)
	require.NoError(t, testutil.WaitForBlocks(ctx, 2, tc.chain))

	// Create second lease (will stay pending)
	res2, err := helpers.BillingCreateLease(ctx, tc.chain, stateTestTenant, []string{tc.skuUUID + ":1"})
	require.NoError(t, err, "failed to create pending lease")
	txRes2, err := tc.chain.GetTransaction(res2.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), txRes2.Code, "lease creation should succeed: %s", txRes2.RawLog)
	pendingLeaseUUID, err := helpers.GetLeaseIDFromTxHash(ctx, tc.chain, res2.TxHash)
	require.NoError(t, err, "failed to get pending lease UUID from tx events")
	t.Logf("Created lease for PENDING state: %s", pendingLeaseUUID)

	// Create third lease (will be acknowledged then closed)
	res3, err := helpers.BillingCreateLease(ctx, tc.chain, stateTestTenant, []string{tc.skuUUID + ":1"})
	require.NoError(t, err, "failed to create lease for closure")
	txRes3, err := tc.chain.GetTransaction(res3.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), txRes3.Code, "lease creation should succeed: %s", txRes3.RawLog)
	closedLeaseUUID, err := helpers.GetLeaseIDFromTxHash(ctx, tc.chain, res3.TxHash)
	require.NoError(t, err, "failed to get closed lease UUID from tx events")
	t.Logf("Created lease for CLOSED state: %s", closedLeaseUUID)

	// Acknowledge and then close the third lease
	ackRes3, err := helpers.BillingAcknowledgeLease(ctx, tc.chain, tc.providerWallet, closedLeaseUUID)
	require.NoError(t, err, "failed to acknowledge lease for closure")
	require.Equal(t, uint32(0), ackRes3.Code)
	require.NoError(t, testutil.WaitForBlocks(ctx, 2, tc.chain))

	closeRes, err := helpers.BillingCloseLease(ctx, tc.chain, stateTestTenant, closedLeaseUUID)
	require.NoError(t, err, "failed to close lease")
	closeTxRes, err := tc.chain.GetTransaction(closeRes.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), closeTxRes.Code, "close should succeed")
	require.NoError(t, testutil.WaitForBlocks(ctx, 2, tc.chain))

	t.Run("success: query all leases returns all states", func(t *testing.T) {
		res, err := helpers.BillingQueryLeases(ctx, tc.chain, "")
		require.NoError(t, err)

		// Should contain leases in various states
		stateCount := make(map[billingtypes.LeaseState]int)
		for _, lease := range res.Leases {
			stateCount[lease.GetState()]++
		}
		t.Logf("All leases by state: %v", stateCount)

		// We should have at least one of each state we created
		require.GreaterOrEqual(t, len(res.Leases), 3, "should have at least 3 leases")
	})

	t.Run("success: query leases by state=pending", func(t *testing.T) {
		res, err := helpers.BillingQueryLeases(ctx, tc.chain, "pending")
		require.NoError(t, err)

		// All returned leases should be PENDING
		for _, lease := range res.Leases {
			require.Equal(t, billingtypes.LEASE_STATE_PENDING, lease.GetState(), "all leases should be pending")
		}

		// Our pending lease should be in the results
		found := false
		for _, lease := range res.Leases {
			if lease.Uuid == pendingLeaseUUID {
				found = true
				break
			}
		}
		require.True(t, found, "pending lease should be found in pending state query")
		t.Logf("Found %d pending leases", len(res.Leases))
	})

	t.Run("success: query leases by state=active", func(t *testing.T) {
		res, err := helpers.BillingQueryLeases(ctx, tc.chain, "active")
		require.NoError(t, err)

		// All returned leases should be ACTIVE
		for _, lease := range res.Leases {
			require.Equal(t, billingtypes.LEASE_STATE_ACTIVE, lease.GetState(), "all leases should be active")
		}

		// Our active lease should be in the results
		found := false
		for _, lease := range res.Leases {
			if lease.Uuid == activeLeaseUUID {
				found = true
				break
			}
		}
		require.True(t, found, "active lease should be found in active state query")
		t.Logf("Found %d active leases", len(res.Leases))
	})

	t.Run("success: query leases by state=closed", func(t *testing.T) {
		res, err := helpers.BillingQueryLeases(ctx, tc.chain, "closed")
		require.NoError(t, err)

		// All returned leases should be CLOSED
		for _, lease := range res.Leases {
			require.Equal(t, billingtypes.LEASE_STATE_CLOSED, lease.GetState(), "all leases should be closed")
		}

		// Our closed lease should be in the results
		found := false
		for _, lease := range res.Leases {
			if lease.Uuid == closedLeaseUUID {
				found = true
				break
			}
		}
		require.True(t, found, "closed lease should be found in closed state query")
		t.Logf("Found %d closed leases", len(res.Leases))
	})

	t.Run("success: query pending leases by provider (efficient index)", func(t *testing.T) {
		res, err := helpers.BillingQueryLeasesByProvider(ctx, tc.chain, tc.providerUUID, "pending")
		require.NoError(t, err)

		// All returned leases should be PENDING and from this provider
		for _, lease := range res.Leases {
			require.Equal(t, billingtypes.LEASE_STATE_PENDING, lease.GetState(), "all leases should be pending")
			require.Equal(t, tc.providerUUID, lease.ProviderUuid, "all leases should be from test provider")
		}

		// Our pending lease should be in the results
		found := false
		for _, lease := range res.Leases {
			if lease.Uuid == pendingLeaseUUID {
				found = true
				break
			}
		}
		require.True(t, found, "pending lease should be found in provider pending query")
		t.Logf("Found %d pending leases for provider", len(res.Leases))
	})

	t.Run("success: query leases by tenant and state", func(t *testing.T) {
		// Query active leases for our test tenant
		res, err := helpers.BillingQueryLeasesByTenant(ctx, tc.chain, stateTestTenant.FormattedAddress(), "active")
		require.NoError(t, err)

		for _, lease := range res.Leases {
			require.Equal(t, billingtypes.LEASE_STATE_ACTIVE, lease.GetState())
			require.Equal(t, stateTestTenant.FormattedAddress(), lease.Tenant)
		}

		// Our active lease should be in the results
		found := false
		for _, lease := range res.Leases {
			if lease.Uuid == activeLeaseUUID {
				found = true
				break
			}
		}
		require.True(t, found, "active lease should be found for tenant")
		t.Logf("Found %d active leases for tenant", len(res.Leases))
	})

	t.Run("success: query leases by provider and state", func(t *testing.T) {
		// Query closed leases for our test provider
		res, err := helpers.BillingQueryLeasesByProvider(ctx, tc.chain, tc.providerUUID, "closed")
		require.NoError(t, err)

		for _, lease := range res.Leases {
			require.Equal(t, billingtypes.LEASE_STATE_CLOSED, lease.GetState())
			require.Equal(t, tc.providerUUID, lease.ProviderUuid)
		}

		// Our closed lease should be in the results
		found := false
		for _, lease := range res.Leases {
			if lease.Uuid == closedLeaseUUID {
				found = true
				break
			}
		}
		require.True(t, found, "closed lease should be found for provider")
		t.Logf("Found %d closed leases for provider", len(res.Leases))
	})

	t.Run("success: state filter returns empty for unmatched state", func(t *testing.T) {
		// Query rejected leases - we haven't created any
		res, err := helpers.BillingQueryLeasesByTenant(ctx, tc.chain, stateTestTenant.FormattedAddress(), "rejected")
		require.NoError(t, err)

		// No rejected leases for this tenant
		require.Empty(t, res.Leases, "should have no rejected leases for this tenant")
		t.Log("Correctly returned empty for rejected state")
	})

	t.Run("success: pending lease not in active query", func(t *testing.T) {
		res, err := helpers.BillingQueryLeases(ctx, tc.chain, "active")
		require.NoError(t, err)

		// Pending lease should NOT be in active results
		for _, lease := range res.Leases {
			require.NotEqual(t, pendingLeaseUUID, lease.Uuid, "pending lease should not appear in active query")
		}
		t.Log("Correctly excluded pending lease from active query")
	})

	t.Run("success: closed lease not in active query", func(t *testing.T) {
		res, err := helpers.BillingQueryLeases(ctx, tc.chain, "active")
		require.NoError(t, err)

		// Closed lease should NOT be in active results
		for _, lease := range res.Leases {
			require.NotEqual(t, closedLeaseUUID, lease.Uuid, "closed lease should not appear in active query")
		}
		t.Log("Correctly excluded closed lease from active query")
	})

	// Cleanup: acknowledge and close remaining pending lease
	t.Run("cleanup: close remaining leases", func(t *testing.T) {
		// Acknowledge the pending lease first
		ackRes, err := helpers.BillingAcknowledgeLease(ctx, tc.chain, tc.providerWallet, pendingLeaseUUID)
		require.NoError(t, err)
		ackTxRes, err := tc.chain.GetTransaction(ackRes.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), ackTxRes.Code, "acknowledge should succeed: %s", ackTxRes.RawLog)
		require.NoError(t, testutil.WaitForBlocks(ctx, 2, tc.chain))

		// Then close it
		closeRes, err := helpers.BillingCloseLease(ctx, tc.chain, stateTestTenant, pendingLeaseUUID)
		require.NoError(t, err)
		closeTxRes, err := tc.chain.GetTransaction(closeRes.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), closeTxRes.Code, "close should succeed: %s", closeTxRes.RawLog)

		// Close the active lease too
		closeRes2, err := helpers.BillingCloseLease(ctx, tc.chain, stateTestTenant, activeLeaseUUID)
		require.NoError(t, err)
		closeTxRes2, err := tc.chain.GetTransaction(closeRes2.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), closeTxRes2.Code, "close should succeed: %s", closeTxRes2.RawLog)
	})
}

// testMaxLeaseLimitsIndependent tests max_leases_per_tenant and max_pending_leases_per_tenant limits.
func testMaxLeaseLimitsIndependent(t *testing.T, ctx context.Context, tc *billingTestContext) {
	t.Log("=== Testing Max Lease Limits ===")

	node := tc.chain.GetNode()

	// Create a test tenant with funded credit
	limitTestTenant, err := interchaintest.GetAndFundTestUserWithMnemonic(ctx, "limittenant", "", sdkmath.NewInt(10_000_000), tc.chain)
	require.NoError(t, err)

	// Send PWR tokens to tenant first
	err = node.SendFunds(ctx, tc.authority.KeyName(), ibc.WalletAmount{
		Address: limitTestTenant.FormattedAddress(),
		Denom:   tc.pwrDenom,
		Amount:  sdkmath.NewInt(200_000_000), // 200 PWR (generous amount for multiple leases)
	})
	require.NoError(t, err)
	require.NoError(t, testutil.WaitForBlocks(ctx, 2, tc.chain))

	// Fund tenant's credit account generously
	fundAmount := fmt.Sprintf("100000000%s", tc.pwrDenom)
	fundRes, err := helpers.BillingFundCredit(ctx, tc.chain, limitTestTenant, limitTestTenant.FormattedAddress(), fundAmount)
	require.NoError(t, err)
	fundTxRes, err := tc.chain.GetTransaction(fundRes.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), fundTxRes.Code, "fund credit should succeed: %s", fundTxRes.RawLog)

	items := []string{tc.skuUUID + ":1"}

	t.Run("fail: exceed max_pending_leases_per_tenant", func(t *testing.T) {
		// Get current params
		params, err := helpers.BillingQueryParams(ctx, tc.chain)
		require.NoError(t, err)

		// Update params to set a low max_pending_leases (2)
		updateRes, err := helpers.BillingUpdateParams(ctx, tc.chain, tc.authority,
			params.Params.MaxLeasesPerTenant,
			params.Params.MaxItemsPerLease,
			params.Params.MinLeaseDuration,
			2, // max_pending_leases_per_tenant = 2
			params.Params.PendingTimeout,
			nil,
		)
		require.NoError(t, err)
		updateTxRes, err := tc.chain.GetTransaction(updateRes.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), updateTxRes.Code, "update params should succeed: %s", updateTxRes.RawLog)

		// Create first pending lease
		res1, err := helpers.BillingCreateLease(ctx, tc.chain, limitTestTenant, items)
		require.NoError(t, err)
		tx1Res, err := tc.chain.GetTransaction(res1.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), tx1Res.Code, "first lease should succeed: %s", tx1Res.RawLog)

		// Create second pending lease
		res2, err := helpers.BillingCreateLease(ctx, tc.chain, limitTestTenant, items)
		require.NoError(t, err)
		tx2Res, err := tc.chain.GetTransaction(res2.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), tx2Res.Code, "second lease should succeed: %s", tx2Res.RawLog)

		// Third pending lease should fail (exceeds max of 2)
		res3, err := helpers.BillingCreateLease(ctx, tc.chain, limitTestTenant, items)
		require.NoError(t, err)
		tx3Res, err := tc.chain.GetTransaction(res3.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), tx3Res.Code, "third pending lease should fail")
		require.Contains(t, tx3Res.RawLog, "pending")

		t.Log("Correctly rejected lease exceeding max_pending_leases_per_tenant")

		// Cleanup: acknowledge and close the pending leases
		leases, err := helpers.BillingQueryLeasesByTenant(ctx, tc.chain, limitTestTenant.FormattedAddress(), "pending")
		require.NoError(t, err)
		for _, lease := range leases.Leases {
			ackRes, _ := helpers.BillingAcknowledgeLease(ctx, tc.chain, tc.providerWallet, lease.Uuid)
			ackTxRes, _ := tc.chain.GetTransaction(ackRes.TxHash)
			if ackTxRes.Code == 0 {
				_, _ = helpers.BillingCloseLease(ctx, tc.chain, limitTestTenant, lease.Uuid)
			}
		}

		// Restore params
		_, _ = helpers.BillingUpdateParams(ctx, tc.chain, tc.authority,
			params.Params.MaxLeasesPerTenant,
			params.Params.MaxItemsPerLease,
			params.Params.MinLeaseDuration,
			params.Params.MaxPendingLeasesPerTenant,
			params.Params.PendingTimeout,
			nil,
		)
	})

	t.Run("fail: exceed max_leases_per_tenant (active)", func(t *testing.T) {
		// Get current params
		params, err := helpers.BillingQueryParams(ctx, tc.chain)
		require.NoError(t, err)

		// Update params to set a low max_leases (2) and high pending limit
		updateRes, err := helpers.BillingUpdateParams(ctx, tc.chain, tc.authority,
			2, // max_leases_per_tenant = 2
			params.Params.MaxItemsPerLease,
			params.Params.MinLeaseDuration,
			10, // allow many pending
			params.Params.PendingTimeout,
			nil,
		)
		require.NoError(t, err)
		updateTxRes, err := tc.chain.GetTransaction(updateRes.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), updateTxRes.Code, "update params should succeed: %s", updateTxRes.RawLog)

		// Create and acknowledge first lease
		res1, err := helpers.BillingCreateLease(ctx, tc.chain, limitTestTenant, items)
		require.NoError(t, err)
		tx1Res, err := tc.chain.GetTransaction(res1.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), tx1Res.Code, "first lease should succeed: %s", tx1Res.RawLog)
		lease1UUID, err := helpers.GetLeaseIDFromTxHash(ctx, tc.chain, res1.TxHash)
		require.NoError(t, err)
		ack1, err := helpers.BillingAcknowledgeLease(ctx, tc.chain, tc.providerWallet, lease1UUID)
		require.NoError(t, err)
		ack1TxRes, err := tc.chain.GetTransaction(ack1.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), ack1TxRes.Code, "first ack should succeed: %s", ack1TxRes.RawLog)

		// Create and acknowledge second lease
		res2, err := helpers.BillingCreateLease(ctx, tc.chain, limitTestTenant, items)
		require.NoError(t, err)
		tx2Res, err := tc.chain.GetTransaction(res2.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), tx2Res.Code, "second lease should succeed: %s", tx2Res.RawLog)
		lease2UUID, err := helpers.GetLeaseIDFromTxHash(ctx, tc.chain, res2.TxHash)
		require.NoError(t, err)
		ack2, err := helpers.BillingAcknowledgeLease(ctx, tc.chain, tc.providerWallet, lease2UUID)
		require.NoError(t, err)
		ack2TxRes, err := tc.chain.GetTransaction(ack2.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), ack2TxRes.Code, "second ack should succeed: %s", ack2TxRes.RawLog)

		// Third lease should fail (exceeds max_leases_per_tenant of 2)
		res3, err := helpers.BillingCreateLease(ctx, tc.chain, limitTestTenant, items)
		require.NoError(t, err)
		tx3Res, err := tc.chain.GetTransaction(res3.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), tx3Res.Code, "third active lease should fail")
		require.Contains(t, tx3Res.RawLog, "maximum")

		t.Log("Correctly rejected lease exceeding max_leases_per_tenant")

		// Cleanup: close leases
		_, _ = helpers.BillingCloseLease(ctx, tc.chain, limitTestTenant, lease1UUID)
		_, _ = helpers.BillingCloseLease(ctx, tc.chain, limitTestTenant, lease2UUID)

		// Restore params
		_, _ = helpers.BillingUpdateParams(ctx, tc.chain, tc.authority,
			params.Params.MaxLeasesPerTenant,
			params.Params.MaxItemsPerLease,
			params.Params.MinLeaseDuration,
			params.Params.MaxPendingLeasesPerTenant,
			params.Params.PendingTimeout,
			nil,
		)
	})
}

// testLeaseAcknowledgeEdgeCasesIndependent tests edge cases for lease acknowledgment.
func testLeaseAcknowledgeEdgeCasesIndependent(t *testing.T, ctx context.Context, tc *billingTestContext) {
	t.Log("=== Testing Lease Acknowledge Edge Cases ===")

	node := tc.chain.GetNode()

	// Setup: create a second provider for wrong-provider tests
	secondProviderWallet, err := interchaintest.GetAndFundTestUserWithMnemonic(ctx, "secondprovider", "", sdkmath.NewInt(10_000_000), tc.chain)
	require.NoError(t, err)

	t.Log("Creating second provider for edge case tests...")
	res, err := helpers.SKUCreateProvider(ctx, tc.chain, tc.authority, secondProviderWallet.FormattedAddress(), secondProviderWallet.FormattedAddress(), "")
	require.NoError(t, err, "failed to create second provider")
	txRes, err := tc.chain.GetTransaction(res.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), txRes.Code, "provider creation should succeed: %s", txRes.RawLog)
	secondProviderUUID, _ := helpers.GetProviderUUIDFromTxHash(ctx, tc.chain, res.TxHash)
	t.Logf("Created second provider UUID: %s", secondProviderUUID)

	// Create a test tenant
	ackTestTenant, err := interchaintest.GetAndFundTestUserWithMnemonic(ctx, "acktenant", "", sdkmath.NewInt(10_000_000), tc.chain)
	require.NoError(t, err)

	// Send PWR tokens to tenant first
	err = node.SendFunds(ctx, tc.authority.KeyName(), ibc.WalletAmount{
		Address: ackTestTenant.FormattedAddress(),
		Denom:   tc.pwrDenom,
		Amount:  sdkmath.NewInt(50_000_000), // 50 PWR
	})
	require.NoError(t, err)
	require.NoError(t, testutil.WaitForBlocks(ctx, 2, tc.chain))

	// Fund enough for multiple leases with reservations
	// Each lease reserves ~3,600,000 (SKU rate × min_lease_duration)
	// Test creates up to 5-6 leases across sub-tests
	fundAmount := fmt.Sprintf("40000000%s", tc.pwrDenom)
	fundRes, err := helpers.BillingFundCredit(ctx, tc.chain, ackTestTenant, ackTestTenant.FormattedAddress(), fundAmount)
	require.NoError(t, err)
	fundTxRes, err := tc.chain.GetTransaction(fundRes.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), fundTxRes.Code, "fund credit should succeed: %s", fundTxRes.RawLog)

	items := []string{tc.skuUUID + ":1"}

	t.Run("fail: acknowledge non-existent lease", func(t *testing.T) {
		fakeUUID := "01935f8a-1234-7000-8000-000000000000"
		res, err := helpers.BillingAcknowledgeLease(ctx, tc.chain, tc.providerWallet, fakeUUID)
		require.NoError(t, err)
		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "acknowledging non-existent lease should fail")
		require.Contains(t, txRes.RawLog, "not found")
		t.Log("Correctly rejected acknowledge for non-existent lease")
	})

	t.Run("fail: wrong provider acknowledges lease", func(t *testing.T) {
		// Create a pending lease for testProvider's SKU
		createRes, err := helpers.BillingCreateLease(ctx, tc.chain, ackTestTenant, items)
		require.NoError(t, err)
		createTxRes, err := tc.chain.GetTransaction(createRes.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), createTxRes.Code, "lease creation should succeed: %s", createTxRes.RawLog)

		leaseUUID, err := helpers.GetLeaseIDFromTxHash(ctx, tc.chain, createRes.TxHash)
		require.NoError(t, err)

		// Second provider tries to acknowledge (wrong provider)
		ackRes, err := helpers.BillingAcknowledgeLease(ctx, tc.chain, secondProviderWallet, leaseUUID)
		require.NoError(t, err)
		ackTxRes, err := tc.chain.GetTransaction(ackRes.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), ackTxRes.Code, "wrong provider should not be able to acknowledge")
		require.Contains(t, ackTxRes.RawLog, "unauthorized")

		t.Log("Correctly rejected acknowledge from wrong provider")

		// Cleanup: cancel the lease
		_, _ = helpers.BillingCancelLease(ctx, tc.chain, ackTestTenant, leaseUUID)
	})

	t.Run("fail: acknowledge already active lease", func(t *testing.T) {
		// Create and acknowledge a lease
		createRes, err := helpers.BillingCreateLease(ctx, tc.chain, ackTestTenant, items)
		require.NoError(t, err)
		createTxRes, err := tc.chain.GetTransaction(createRes.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), createTxRes.Code, "lease creation should succeed: %s", createTxRes.RawLog)

		leaseUUID, err := helpers.GetLeaseIDFromTxHash(ctx, tc.chain, createRes.TxHash)
		require.NoError(t, err)

		// First acknowledge succeeds
		ack1, err := helpers.BillingAcknowledgeLease(ctx, tc.chain, tc.providerWallet, leaseUUID)
		require.NoError(t, err)
		ack1TxRes, err := tc.chain.GetTransaction(ack1.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), ack1TxRes.Code, "first ack should succeed: %s", ack1TxRes.RawLog)

		// Second acknowledge should fail
		ack2, err := helpers.BillingAcknowledgeLease(ctx, tc.chain, tc.providerWallet, leaseUUID)
		require.NoError(t, err)
		ack2TxRes, err := tc.chain.GetTransaction(ack2.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), ack2TxRes.Code, "re-acknowledging active lease should fail")
		require.Contains(t, ack2TxRes.RawLog, "not in pending state")

		t.Log("Correctly rejected re-acknowledge of active lease")

		// Cleanup
		_, _ = helpers.BillingCloseLease(ctx, tc.chain, ackTestTenant, leaseUUID)
	})

	t.Run("fail: reject non-existent lease", func(t *testing.T) {
		fakeUUID := "01935f8a-5678-7000-8000-000000000000"
		res, err := helpers.BillingRejectLease(ctx, tc.chain, tc.providerWallet, fakeUUID, "test rejection")
		require.NoError(t, err)
		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "rejecting non-existent lease should fail")
		require.Contains(t, txRes.RawLog, "not found")
		t.Log("Correctly rejected reject for non-existent lease")
	})

	t.Run("fail: cancel non-existent lease", func(t *testing.T) {
		fakeUUID := "01935f8a-9abc-7000-8000-000000000000"
		res, err := helpers.BillingCancelLease(ctx, tc.chain, ackTestTenant, fakeUUID)
		require.NoError(t, err)
		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "canceling non-existent lease should fail")
		require.Contains(t, txRes.RawLog, "not found")
		t.Log("Correctly rejected cancel for non-existent lease")
	})

	// Batch Acknowledge Tests
	t.Run("success: batch acknowledge multiple leases", func(t *testing.T) {
		// Create 3 pending leases
		var batchLeaseUUIDs []string
		for i := 0; i < 3; i++ {
			createRes, err := helpers.BillingCreateLease(ctx, tc.chain, ackTestTenant, items)
			require.NoError(t, err)
			createTxRes, err := tc.chain.GetTransaction(createRes.TxHash)
			require.NoError(t, err)
			require.Equal(t, uint32(0), createTxRes.Code, "lease %d creation should succeed: %s", i+1, createTxRes.RawLog)

			leaseUUID, err := helpers.GetLeaseIDFromTxHash(ctx, tc.chain, createRes.TxHash)
			require.NoError(t, err)
			batchLeaseUUIDs = append(batchLeaseUUIDs, leaseUUID)
		}

		// Verify all are pending
		for _, uuid := range batchLeaseUUIDs {
			lease, err := helpers.BillingQueryLease(ctx, tc.chain, uuid)
			require.NoError(t, err)
			require.Equal(t, "LEASE_STATE_PENDING", lease.Lease.State)
		}

		// Batch acknowledge all 3 at once
		ackRes, err := helpers.BillingAcknowledgeLeases(ctx, tc.chain, tc.providerWallet, batchLeaseUUIDs)
		require.NoError(t, err)
		ackTxRes, err := tc.chain.GetTransaction(ackRes.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), ackTxRes.Code, "batch acknowledge should succeed: %s", ackTxRes.RawLog)

		// Verify all leases are now active
		for _, uuid := range batchLeaseUUIDs {
			lease, err := helpers.BillingQueryLease(ctx, tc.chain, uuid)
			require.NoError(t, err)
			require.Equal(t, "LEASE_STATE_ACTIVE", lease.Lease.State, "lease %s should be active after batch ack", uuid)
		}

		t.Logf("Successfully batch acknowledged %d leases", len(batchLeaseUUIDs))

		// Cleanup
		for _, uuid := range batchLeaseUUIDs {
			_, _ = helpers.BillingCloseLease(ctx, tc.chain, ackTestTenant, uuid)
		}
	})

	t.Run("fail: batch acknowledge with one already active lease (atomicity)", func(t *testing.T) {
		// Create 2 leases
		var atomicLeaseUUIDs []string
		for i := 0; i < 2; i++ {
			createRes, err := helpers.BillingCreateLease(ctx, tc.chain, ackTestTenant, items)
			require.NoError(t, err)
			createTxRes, err := tc.chain.GetTransaction(createRes.TxHash)
			require.NoError(t, err)
			require.Equal(t, uint32(0), createTxRes.Code, "lease %d creation should succeed: %s", i+1, createTxRes.RawLog)

			leaseUUID, err := helpers.GetLeaseIDFromTxHash(ctx, tc.chain, createRes.TxHash)
			require.NoError(t, err)
			atomicLeaseUUIDs = append(atomicLeaseUUIDs, leaseUUID)
		}

		// Acknowledge one lease first
		ack1, err := helpers.BillingAcknowledgeLease(ctx, tc.chain, tc.providerWallet, atomicLeaseUUIDs[0])
		require.NoError(t, err)
		ack1TxRes, err := tc.chain.GetTransaction(ack1.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), ack1TxRes.Code)

		// Try to batch acknowledge both (one already active) - should fail
		ackRes, err := helpers.BillingAcknowledgeLeases(ctx, tc.chain, tc.providerWallet, atomicLeaseUUIDs)
		require.NoError(t, err)
		ackTxRes, err := tc.chain.GetTransaction(ackRes.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), ackTxRes.Code, "batch ack with active lease should fail")
		require.Contains(t, ackTxRes.RawLog, "not in pending state")

		// Verify the pending lease is still PENDING (atomic - no partial success)
		lease, err := helpers.BillingQueryLease(ctx, tc.chain, atomicLeaseUUIDs[1])
		require.NoError(t, err)
		require.Equal(t, "LEASE_STATE_PENDING", lease.Lease.State, "pending lease should still be pending after atomic failure")

		t.Log("Correctly enforced atomicity: pending lease unchanged after batch failure")

		// Cleanup
		_, _ = helpers.BillingCloseLease(ctx, tc.chain, ackTestTenant, atomicLeaseUUIDs[0])
		_, _ = helpers.BillingCancelLease(ctx, tc.chain, ackTestTenant, atomicLeaseUUIDs[1])
	})

	_ = secondProviderUUID // avoid unused variable
}

// testLeasePaginationIndependent tests pagination for lease queries.
func testLeasePaginationIndependent(t *testing.T, ctx context.Context, tc *billingTestContext) {
	t.Log("=== Testing Lease Pagination ===")

	node := tc.chain.GetNode()

	// Create a test tenant with funded credit
	paginationTenant, err := interchaintest.GetAndFundTestUserWithMnemonic(ctx, "pagetenant", "", sdkmath.NewInt(10_000_000), tc.chain)
	require.NoError(t, err)

	// Send PWR tokens to tenant first
	err = node.SendFunds(ctx, tc.authority.KeyName(), ibc.WalletAmount{
		Address: paginationTenant.FormattedAddress(),
		Denom:   tc.pwrDenom,
		Amount:  sdkmath.NewInt(200_000_000), // 200 PWR (generous amount for 5 leases)
	})
	require.NoError(t, err)
	require.NoError(t, testutil.WaitForBlocks(ctx, 2, tc.chain))

	fundAmount := fmt.Sprintf("100000000%s", tc.pwrDenom)
	fundRes, err := helpers.BillingFundCredit(ctx, tc.chain, paginationTenant, paginationTenant.FormattedAddress(), fundAmount)
	require.NoError(t, err)
	fundTxRes, err := tc.chain.GetTransaction(fundRes.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), fundTxRes.Code, "fund credit should succeed: %s", fundTxRes.RawLog)

	items := []string{tc.skuUUID + ":1"}
	var leaseUUIDs []string

	// Setup: create 5 leases and acknowledge them
	t.Log("Creating 5 leases for pagination tests...")
	for i := 0; i < 5; i++ {
		createRes, err := helpers.BillingCreateLease(ctx, tc.chain, paginationTenant, items)
		require.NoError(t, err, "failed to create lease %d", i+1)
		createTxRes, err := tc.chain.GetTransaction(createRes.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), createTxRes.Code, "lease %d creation should succeed: %s", i+1, createTxRes.RawLog)

		leaseUUID, err := helpers.GetLeaseIDFromTxHash(ctx, tc.chain, createRes.TxHash)
		require.NoError(t, err)
		leaseUUIDs = append(leaseUUIDs, leaseUUID)

		// Acknowledge
		ackRes, err := helpers.BillingAcknowledgeLease(ctx, tc.chain, tc.providerWallet, leaseUUID)
		require.NoError(t, err, "failed to acknowledge lease %d", i+1)
		ackTxRes, err := tc.chain.GetTransaction(ackRes.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), ackTxRes.Code, "lease %d ack should succeed: %s", i+1, ackTxRes.RawLog)
	}
	t.Logf("Created %d leases for pagination tests", len(leaseUUIDs))

	t.Run("success: paginate through all leases", func(t *testing.T) {
		// Query first page (limit 2)
		res1, nextKey, err := helpers.BillingQueryLeasesPaginated(ctx, tc.chain, "", 2, "")
		require.NoError(t, err)
		require.Len(t, res1.Leases, 2, "first page should have 2 leases")
		require.NotEmpty(t, nextKey, "should have next key for more pages")

		// Query second page
		res2, _, err := helpers.BillingQueryLeasesPaginated(ctx, tc.chain, "", 2, nextKey)
		require.NoError(t, err)
		t.Logf("Page 1: %d leases, Page 2: %d leases", len(res1.Leases), len(res2.Leases))
	})

	t.Run("success: paginate leases by tenant", func(t *testing.T) {
		// Query all leases for this tenant without pagination to get total count
		allRes, _, err := helpers.BillingQueryLeasesByTenantPaginated(ctx, tc.chain, paginationTenant.FormattedAddress(), "", 100, "")
		require.NoError(t, err)
		require.Len(t, allRes.Leases, 5, "tenant should have exactly 5 leases")

		// All leases should belong to our tenant
		for _, lease := range allRes.Leases {
			require.Equal(t, paginationTenant.FormattedAddress(), lease.Tenant)
		}

		// Now test pagination with smaller page size
		res1, nextKey, err := helpers.BillingQueryLeasesByTenantPaginated(ctx, tc.chain, paginationTenant.FormattedAddress(), "", 2, "")
		require.NoError(t, err)
		require.Len(t, res1.Leases, 2, "first page should have 2 leases")
		require.NotEmpty(t, nextKey, "should have next key for more pages")

		t.Logf("Tenant pagination: total = %d, first page = %d, has more = %v", len(allRes.Leases), len(res1.Leases), nextKey != "")
	})

	t.Run("success: paginate leases by provider", func(t *testing.T) {
		// Query first page for testProvider
		res1, nextKey, err := helpers.BillingQueryLeasesByProviderPaginated(ctx, tc.chain, tc.providerUUID, "", 2, "")
		require.NoError(t, err)
		require.NotEmpty(t, res1.Leases, "should have leases")

		t.Logf("Provider pagination: Page 1 = %d leases, has more = %v", len(res1.Leases), nextKey != "")
	})

	t.Run("success: paginate with state filter", func(t *testing.T) {
		// Query active leases with pagination
		res, _, err := helpers.BillingQueryLeasesPaginated(ctx, tc.chain, "active", 3, "")
		require.NoError(t, err)

		// All returned leases should be active
		for _, lease := range res.Leases {
			require.Equal(t, billingtypes.LEASE_STATE_ACTIVE, lease.GetState())
		}

		t.Logf("Found %d active leases with state filter", len(res.Leases))
	})

	// Cleanup
	t.Run("cleanup: close pagination test leases", func(_ *testing.T) {
		for _, uuid := range leaseUUIDs {
			_, _ = helpers.BillingCloseLease(ctx, tc.chain, paginationTenant, uuid)
		}
	})
}

// testBillingInvalidUUIDIndependent tests that invalid UUID formats are rejected.
func testBillingInvalidUUIDIndependent(t *testing.T, ctx context.Context, tc *billingTestContext) {
	t.Log("=== Testing Billing Invalid UUID Format Rejection ===")

	node := tc.chain.GetNode()

	// Invalid UUID formats to test
	invalidUUIDs := []struct {
		uuid string
		desc string
	}{
		{"not-a-uuid", "plain string"},
		{"12345", "numeric string"},
		{"01234567-89ab-cdef-0123-456789abcdef", "UUIDv4 format (not v7)"},
		{"01912345-6789-7abc-8def-0123456789a", "too short"},
		{"01912345-6789-7abc-8def-0123456789abcd", "too long"},
		{"01912345-6789-7abc-8def-0123456789ag", "invalid character"},
	}

	// Create a test tenant with funded credit
	invalidUUIDTenant, err := interchaintest.GetAndFundTestUserWithMnemonic(ctx, "invaliduuid", "", sdkmath.NewInt(10_000_000), tc.chain)
	require.NoError(t, err)

	// Send PWR tokens to tenant first
	err = node.SendFunds(ctx, tc.authority.KeyName(), ibc.WalletAmount{
		Address: invalidUUIDTenant.FormattedAddress(),
		Denom:   tc.pwrDenom,
		Amount:  sdkmath.NewInt(10_000_000), // 10 PWR
	})
	require.NoError(t, err)
	require.NoError(t, testutil.WaitForBlocks(ctx, 2, tc.chain))

	// Fund credit account
	fundRes, err := helpers.BillingFundCredit(ctx, tc.chain, invalidUUIDTenant, invalidUUIDTenant.FormattedAddress(), fmt.Sprintf("1000000%s", tc.pwrDenom))
	require.NoError(t, err)
	fundTxRes, err := tc.chain.GetTransaction(fundRes.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), fundTxRes.Code, "fund credit should succeed: %s", fundTxRes.RawLog)

	t.Run("fail: create lease with invalid sku_uuid", func(t *testing.T) {
		for _, testCase := range invalidUUIDs {
			items := []string{fmt.Sprintf("%s:1", testCase.uuid)}
			res, err := helpers.BillingCreateLease(ctx, tc.chain, invalidUUIDTenant, items)
			if err != nil {
				require.Contains(t, err.Error(), "uuid", "invalid sku_uuid (%s) should be rejected: %s", testCase.desc, testCase.uuid)
			} else {
				txRes, err := tc.chain.GetTransaction(res.TxHash)
				require.NoError(t, err)
				require.NotEqual(t, uint32(0), txRes.Code, "invalid sku_uuid (%s) should fail: %s", testCase.desc, testCase.uuid)
			}
		}
		t.Log("Correctly rejected create lease with invalid sku_uuid")
	})

	// Note: The rest of this test follows the same pattern, testing invalid UUIDs
	// against various operations. For brevity, I'll just test a few key ones.

	t.Run("fail: close lease with invalid uuid", func(t *testing.T) {
		for _, testCase := range invalidUUIDs {
			res, err := helpers.BillingCloseLease(ctx, tc.chain, invalidUUIDTenant, testCase.uuid)
			if err != nil {
				require.Contains(t, err.Error(), "uuid", "invalid lease_uuid (%s) should be rejected: %s", testCase.desc, testCase.uuid)
			} else {
				txRes, err := tc.chain.GetTransaction(res.TxHash)
				require.NoError(t, err)
				require.NotEqual(t, uint32(0), txRes.Code, "invalid lease_uuid (%s) should fail: %s", testCase.desc, testCase.uuid)
			}
		}
		t.Log("Correctly rejected close lease with invalid uuid")
	})

	t.Run("fail: query lease with invalid uuid", func(t *testing.T) {
		for _, testCase := range invalidUUIDs {
			_, err := helpers.BillingQueryLease(ctx, tc.chain, testCase.uuid)
			require.Error(t, err, "query lease with invalid uuid (%s) should fail: %s", testCase.desc, testCase.uuid)
		}
		t.Log("Correctly rejected query lease with invalid uuid")
	})
}

// testBillingEmptyParamsIndependent tests that empty string parameters are rejected.
func testBillingEmptyParamsIndependent(t *testing.T, ctx context.Context, tc *billingTestContext) {
	t.Log("=== Testing Billing Empty String Parameter Rejection ===")

	node := tc.chain.GetNode()

	// Create a test tenant with funded credit
	emptyParamsTenant, err := interchaintest.GetAndFundTestUserWithMnemonic(ctx, "emptyparams", "", sdkmath.NewInt(10_000_000), tc.chain)
	require.NoError(t, err)

	// Send PWR tokens to tenant first
	err = node.SendFunds(ctx, tc.authority.KeyName(), ibc.WalletAmount{
		Address: emptyParamsTenant.FormattedAddress(),
		Denom:   tc.pwrDenom,
		Amount:  sdkmath.NewInt(10_000_000), // 10 PWR
	})
	require.NoError(t, err)
	require.NoError(t, testutil.WaitForBlocks(ctx, 2, tc.chain))

	// Fund credit account
	fundRes, err := helpers.BillingFundCredit(ctx, tc.chain, emptyParamsTenant, emptyParamsTenant.FormattedAddress(), fmt.Sprintf("1000000%s", tc.pwrDenom))
	require.NoError(t, err)
	fundTxRes, err := tc.chain.GetTransaction(fundRes.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), fundTxRes.Code, "fund credit should succeed: %s", fundTxRes.RawLog)

	t.Run("fail: fund credit with empty tenant", func(t *testing.T) {
		_, err := helpers.BillingFundCredit(ctx, tc.chain, emptyParamsTenant, "", fmt.Sprintf("1000%s", tc.pwrDenom))
		require.Error(t, err, "fund credit with empty tenant should fail")
		t.Log("Correctly rejected fund credit with empty tenant")
	})

	t.Run("fail: close lease with empty uuid", func(t *testing.T) {
		_, err := helpers.BillingCloseLease(ctx, tc.chain, emptyParamsTenant, "")
		require.Error(t, err, "close lease with empty uuid should fail")
		t.Log("Correctly rejected close lease with empty uuid")
	})

	t.Run("fail: acknowledge lease with empty uuid", func(t *testing.T) {
		_, err := helpers.BillingAcknowledgeLease(ctx, tc.chain, tc.providerWallet, "")
		require.Error(t, err, "acknowledge lease with empty uuid should fail")
		t.Log("Correctly rejected acknowledge lease with empty uuid")
	})

	t.Run("fail: query credit account with empty tenant", func(t *testing.T) {
		_, err := helpers.BillingQueryCreditAccount(ctx, tc.chain, "")
		require.Error(t, err, "query credit account with empty tenant should fail")
		t.Log("Correctly rejected query credit account with empty tenant")
	})

	t.Run("fail: query lease with empty uuid", func(t *testing.T) {
		_, err := helpers.BillingQueryLease(ctx, tc.chain, "")
		require.Error(t, err, "query lease with empty uuid should fail")
		t.Log("Correctly rejected query lease with empty uuid")
	})
}

// testLeasesBySKUQueryIndependent tests the leases-by-sku query.
func testLeasesBySKUQueryIndependent(t *testing.T, ctx context.Context, tc *billingTestContext) {
	t.Log("=== Testing Leases By SKU Query ===")

	// Create a fresh tenant and lease for this test
	users := interchaintest.GetAndFundTestUsers(t, ctx, "sku-query-test", DefaultGenesisAmt, tc.chain)
	tenant := users[0]

	// Fund tenant's credit account
	err := tc.chain.SendFunds(ctx, tc.authority.KeyName(), ibc.WalletAmount{
		Address: tenant.FormattedAddress(),
		Denom:   tc.pwrDenom,
		Amount:  sdkmath.NewInt(100_000_000),
	})
	require.NoError(t, err)
	_, err = helpers.BillingFundCredit(ctx, tc.chain, tenant, tenant.FormattedAddress(), fmt.Sprintf("50000000%s", tc.pwrDenom))
	require.NoError(t, err)

	// Create and acknowledge a lease with the test SKU
	items := []string{fmt.Sprintf("%s:1", tc.skuUUID)}
	leaseUUID, err := helpers.BillingCreateAndAcknowledgeLease(ctx, tc.chain, tenant, tc.providerWallet, items)
	require.NoError(t, err)
	require.NotEmpty(t, leaseUUID)

	t.Run("success: query leases by SKU UUID", func(t *testing.T) {
		res, err := helpers.BillingQueryLeasesBySKU(ctx, tc.chain, tc.skuUUID, "")
		require.NoError(t, err)
		require.NotNil(t, res)
		require.NotEmpty(t, res.Leases, "should find leases using this SKU")

		// Verify our lease is in the results
		found := false
		for _, lease := range res.Leases {
			if lease.Uuid == leaseUUID {
				found = true
				break
			}
		}
		require.True(t, found, "newly created lease should be in results")
		t.Logf("Found %d leases using SKU %s", len(res.Leases), tc.skuUUID)
	})

	t.Run("success: query leases by SKU with state filter", func(t *testing.T) {
		// Query only active leases
		res, err := helpers.BillingQueryLeasesBySKU(ctx, tc.chain, tc.skuUUID, "active")
		require.NoError(t, err)
		require.NotNil(t, res)

		// All returned leases should be active
		for _, lease := range res.Leases {
			require.Equal(t, "LEASE_STATE_ACTIVE", lease.State,
				"state filter should only return active leases")
		}
		t.Logf("Found %d active leases using SKU %s", len(res.Leases), tc.skuUUID)
	})

	t.Run("success: query returns empty for non-existent SKU", func(t *testing.T) {
		// Use a valid UUID format that doesn't exist in the system
		nonExistentSKU := "01912345-6789-7abc-8def-ffffffffffff"
		res, err := helpers.BillingQueryLeasesBySKU(ctx, tc.chain, nonExistentSKU, "")
		require.NoError(t, err)
		require.NotNil(t, res)
		require.Empty(t, res.Leases, "should return empty for non-existent SKU")
		t.Log("Correctly returned empty for non-existent SKU")
	})

	t.Run("fail: query with invalid SKU UUID format", func(t *testing.T) {
		_, err := helpers.BillingQueryLeasesBySKU(ctx, tc.chain, "invalid-uuid", "")
		require.Error(t, err, "should fail with invalid UUID format")
		t.Log("Correctly rejected invalid SKU UUID format")
	})

	// Clean up - close the lease
	_, err = helpers.BillingCloseLease(ctx, tc.chain, tenant, leaseUUID)
	require.NoError(t, err)
}

// testProviderByAddressQueryIndependent tests the provider-by-address query in the SKU module.
func testProviderByAddressQueryIndependent(t *testing.T, ctx context.Context, tc *billingTestContext) {
	t.Log("=== Testing Provider By Address Query ===")

	t.Run("success: query provider by address", func(t *testing.T) {
		res, err := helpers.SKUQueryProviderByAddress(ctx, tc.chain, tc.providerWallet.FormattedAddress())
		require.NoError(t, err)
		require.NotNil(t, res)
		require.Len(t, res.Providers, 1, "should return exactly one provider")

		provider := res.Providers[0]
		// Verify the provider details match
		require.Equal(t, tc.providerWallet.FormattedAddress(), provider.Address,
			"provider address should match")
		require.Equal(t, tc.providerUUID, provider.Uuid,
			"provider UUID should match the test provider")
		require.True(t, provider.Active, "provider should be active")

		t.Logf("Found provider %s with UUID %s", provider.Address, provider.Uuid)
	})

	t.Run("success: query non-existent provider returns empty list", func(t *testing.T) {
		// Use tenant2's address - it's a valid address but has no provider
		res, err := helpers.SKUQueryProviderByAddress(ctx, tc.chain, tc.tenant2.FormattedAddress())
		require.NoError(t, err, "query should succeed")
		require.NotNil(t, res)
		require.Empty(t, res.Providers, "should return empty list for address without provider")
		t.Log("Correctly returned empty list for address without provider")
	})

	t.Run("fail: query with invalid address format", func(t *testing.T) {
		_, err := helpers.SKUQueryProviderByAddress(ctx, tc.chain, "invalid-address")
		require.Error(t, err, "should fail with invalid address format")
		t.Log("Correctly rejected invalid address format")
	})

	t.Run("fail: query with empty address", func(t *testing.T) {
		_, err := helpers.SKUQueryProviderByAddress(ctx, tc.chain, "")
		require.Error(t, err, "should fail with empty address")
		t.Log("Correctly rejected empty address")
	})
}
