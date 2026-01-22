// Package interchaintest contains end-to-end tests for the billing module.
// This file contains lease lifecycle tests (create, query, close, reject, cancel).
//
// Run with: go test -v ./interchaintest -run TestBillingLease -timeout 45m
package interchaintest

import (
	"context"
	"fmt"
	"testing"

	"github.com/strangelove-ventures/interchaintest/v8"
	"github.com/strangelove-ventures/interchaintest/v8/ibc"
	"github.com/strangelove-ventures/interchaintest/v8/testutil"
	"github.com/stretchr/testify/require"

	"github.com/manifest-network/manifest-ledger/interchaintest/helpers"
	billingtypes "github.com/manifest-network/manifest-ledger/x/billing/types"
)

// TestBillingLease runs lease lifecycle tests independently.
// Tests: create, query, close, reject, cancel operations.
func TestBillingLease(t *testing.T) {
	ctx, tc, cleanup := setupBillingTest(t, "billing-lease-test")
	t.Cleanup(cleanup)

	// Set globals from context for existing test functions
	testPWRDenom = tc.pwrDenom
	testProviderUUID = tc.providerUUID
	testSKUUUID = tc.skuUUID
	testSKUUUID2 = tc.skuUUID2

	// Fund tenant1 credit account for lease tests
	fundTenantCredit(t, ctx, tc, tc.tenant1, 100_000_000)

	// Run lease lifecycle tests
	t.Run("Create", func(t *testing.T) {
		testLeaseCreateIndependent(t, ctx, tc)
	})

	t.Run("Query", func(t *testing.T) {
		testLeaseQueryIndependent(t, ctx, tc)
	})

	t.Run("Close", func(t *testing.T) {
		testLeaseCloseIndependent(t, ctx, tc)
	})

	t.Run("RejectAndCancel", func(t *testing.T) {
		testLeaseRejectAndCancelIndependent(t, ctx, tc)
	})
}

// testLeaseCreateIndependent tests lease creation scenarios.
func testLeaseCreateIndependent(t *testing.T, ctx context.Context, tc *billingTestContext) {
	t.Log("=== Testing Lease Create ===")

	t.Run("success: tenant creates lease with single SKU", func(t *testing.T) {
		items := []string{fmt.Sprintf("%s:1", tc.skuUUID)}
		res, err := helpers.BillingCreateLease(ctx, tc.chain, tc.tenant1, items)
		require.NoError(t, err)

		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "lease creation should succeed: %s", txRes.RawLog)

		leaseID, err := helpers.GetLeaseIDFromTxHash(ctx, tc.chain, res.TxHash)
		require.NoError(t, err)
		t.Logf("Created lease ID: %s", leaseID)

		// Acknowledge the lease to make it ACTIVE
		ackRes, err := helpers.BillingAcknowledgeLease(ctx, tc.chain, tc.providerWallet, leaseID)
		require.NoError(t, err)
		ackTxRes, err := tc.chain.GetTransaction(ackRes.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), ackTxRes.Code, "lease acknowledgement should succeed")
	})

	t.Run("success: tenant creates lease with multiple SKUs", func(t *testing.T) {
		items := []string{
			fmt.Sprintf("%s:2", tc.skuUUID),  // 2x per-hour SKU
			fmt.Sprintf("%s:1", tc.skuUUID2), // 1x per-day SKU
		}
		res, err := helpers.BillingCreateLease(ctx, tc.chain, tc.tenant1, items)
		require.NoError(t, err)

		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "lease creation should succeed")

		leaseID, err := helpers.GetLeaseIDFromTxHash(ctx, tc.chain, res.TxHash)
		require.NoError(t, err)

		// Acknowledge the lease
		ackRes, err := helpers.BillingAcknowledgeLease(ctx, tc.chain, tc.providerWallet, leaseID)
		require.NoError(t, err)
		ackTxRes, err := tc.chain.GetTransaction(ackRes.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), ackTxRes.Code, "lease acknowledgement should succeed")
	})

	t.Run("success: tenant creates lease with meta_hash", func(t *testing.T) {
		// Create a SHA-256 hash as meta_hash (hex-encoded, 64 chars = 32 bytes)
		metaHash := "a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8091a2b3c4d5e6f708192a3b4c5d6"

		items := []string{fmt.Sprintf("%s:1", tc.skuUUID)}
		res, err := helpers.BillingCreateLeaseWithMetaHash(ctx, tc.chain, tc.tenant1, items, metaHash)
		require.NoError(t, err)

		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "lease creation with meta_hash should succeed: %s", txRes.RawLog)

		leaseID, err := helpers.GetLeaseIDFromTxHash(ctx, tc.chain, res.TxHash)
		require.NoError(t, err)
		t.Logf("Created lease with meta_hash, ID: %s", leaseID)

		// Query the lease and verify meta_hash is stored
		leaseRes, err := helpers.BillingQueryLease(ctx, tc.chain, leaseID)
		require.NoError(t, err)
		require.NotEmpty(t, leaseRes.Lease.MetaHash, "meta_hash should be stored on the lease")

		// Acknowledge the lease to make it ACTIVE
		ackRes, err := helpers.BillingAcknowledgeLease(ctx, tc.chain, tc.providerWallet, leaseID)
		require.NoError(t, err)
		ackTxRes, err := tc.chain.GetTransaction(ackRes.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), ackTxRes.Code, "lease acknowledgement should succeed")
	})

	t.Run("fail: create lease with non-existent SKU", func(t *testing.T) {
		items := []string{fmt.Sprintf("%s:1", nonExistentUUID)}
		res, err := helpers.BillingCreateLease(ctx, tc.chain, tc.tenant1, items)
		require.NoError(t, err)

		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "lease creation should fail")
		require.Contains(t, txRes.RawLog, "not found")
	})

	t.Run("fail: create lease without credit account", func(t *testing.T) {
		users := interchaintest.GetAndFundTestUsers(t, ctx, "no-credit-account", DefaultGenesisAmt, tc.chain)
		noCreditAccount := users[0]

		items := []string{fmt.Sprintf("%s:1", tc.skuUUID)}
		res, err := helpers.BillingCreateLease(ctx, tc.chain, noCreditAccount, items)
		require.NoError(t, err)

		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "lease creation should fail")
		require.Contains(t, txRes.RawLog, "credit account not found")
	})

	t.Run("fail: create lease with insufficient credit", func(t *testing.T) {
		users := interchaintest.GetAndFundTestUsers(t, ctx, "low-credit", DefaultGenesisAmt, tc.chain)
		lowCredit := users[0]

		// Fund with only 1 upwr
		fundAmount := fmt.Sprintf("1%s", tc.pwrDenom)
		res, err := helpers.BillingFundCredit(ctx, tc.chain, tc.authority, lowCredit.FormattedAddress(), fundAmount)
		require.NoError(t, err)
		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code)

		items := []string{fmt.Sprintf("%s:1", tc.skuUUID)}
		res, err = helpers.BillingCreateLease(ctx, tc.chain, lowCredit, items)
		require.NoError(t, err)

		txRes, err = tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "lease creation should fail")
		require.Contains(t, txRes.RawLog, "insufficient credit")
	})

	t.Run("fail: create lease exceeding max_items_per_lease hard limit", func(t *testing.T) {
		items := make([]string, 101)
		for i := 0; i < 101; i++ {
			items[i] = fmt.Sprintf("01912345-6789-7abc-8def-%012d:1", i)
		}
		_, err := helpers.BillingCreateLease(ctx, tc.chain, tc.tenant1, items)
		require.Error(t, err)
		require.Contains(t, err.Error(), "too many items")
	})
}

// testLeaseQueryIndependent tests lease query operations.
func testLeaseQueryIndependent(t *testing.T, ctx context.Context, tc *billingTestContext) {
	t.Log("=== Testing Lease Query ===")

	t.Run("success: query all leases", func(t *testing.T) {
		res, err := helpers.BillingQueryLeases(ctx, tc.chain, "")
		require.NoError(t, err)
		require.NotEmpty(t, res.Leases, "should have leases from previous tests")
		t.Logf("Found %d leases", len(res.Leases))
	})

	t.Run("success: query leases by tenant", func(t *testing.T) {
		res, err := helpers.BillingQueryLeasesByTenant(ctx, tc.chain, tc.tenant1.FormattedAddress(), "")
		require.NoError(t, err)
		require.NotEmpty(t, res.Leases)
		for _, lease := range res.Leases {
			require.Equal(t, tc.tenant1.FormattedAddress(), lease.Tenant)
		}
	})

	t.Run("success: query leases by provider", func(t *testing.T) {
		res, err := helpers.BillingQueryLeasesByProvider(ctx, tc.chain, tc.providerUUID, "")
		require.NoError(t, err)
		require.NotEmpty(t, res.Leases)
		for _, lease := range res.Leases {
			require.Equal(t, tc.providerUUID, lease.ProviderUuid)
		}
	})

	t.Run("success: query leases by state filter", func(t *testing.T) {
		res, err := helpers.BillingQueryLeases(ctx, tc.chain, "active")
		require.NoError(t, err)
		for _, lease := range res.Leases {
			require.Equal(t, billingtypes.LEASE_STATE_ACTIVE, lease.GetState())
		}
	})
}

// testLeaseCloseIndependent tests lease close operations.
func testLeaseCloseIndependent(t *testing.T, ctx context.Context, tc *billingTestContext) {
	t.Log("=== Testing Lease Close ===")

	// Create a fresh lease for close testing
	items := []string{fmt.Sprintf("%s:1", tc.skuUUID)}
	leaseUUID, err := helpers.BillingCreateAndAcknowledgeLease(ctx, tc.chain, tc.tenant1, tc.providerWallet, items)
	require.NoError(t, err)
	require.NotEmpty(t, leaseUUID)

	t.Run("success: tenant closes their own lease", func(t *testing.T) {
		res, err := helpers.BillingCloseLease(ctx, tc.chain, tc.tenant1, leaseUUID)
		require.NoError(t, err)

		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "lease close should succeed")

		// Verify lease is closed
		leaseRes, err := helpers.BillingQueryLease(ctx, tc.chain, leaseUUID)
		require.NoError(t, err)
		require.Equal(t, billingtypes.LEASE_STATE_CLOSED, leaseRes.Lease.GetState())
	})

	t.Run("fail: close already closed lease", func(t *testing.T) {
		res, err := helpers.BillingCloseLease(ctx, tc.chain, tc.tenant1, leaseUUID)
		require.NoError(t, err)

		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code)
	})

	t.Run("success: provider closes lease", func(t *testing.T) {
		// Create another lease
		leaseUUID2, err := helpers.BillingCreateAndAcknowledgeLease(ctx, tc.chain, tc.tenant1, tc.providerWallet, items)
		require.NoError(t, err)

		res, err := helpers.BillingCloseLease(ctx, tc.chain, tc.providerWallet, leaseUUID2)
		require.NoError(t, err)

		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "provider should be able to close lease")
	})

	t.Run("fail: unauthorized user closes lease", func(t *testing.T) {
		// Create another lease
		leaseUUID3, err := helpers.BillingCreateAndAcknowledgeLease(ctx, tc.chain, tc.tenant1, tc.providerWallet, items)
		require.NoError(t, err)

		res, err := helpers.BillingCloseLease(ctx, tc.chain, tc.unauthorizedUser, leaseUUID3)
		require.NoError(t, err)

		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "unauthorized close should fail")
	})

	t.Run("success: close lease with reason", func(t *testing.T) {
		// Create another lease for this test
		leaseUUID4, err := helpers.BillingCreateAndAcknowledgeLease(ctx, tc.chain, tc.tenant1, tc.providerWallet, items)
		require.NoError(t, err)

		closureReason := "service no longer needed"
		res, err := helpers.BillingCloseLeaseWithReason(ctx, tc.chain, tc.tenant1, leaseUUID4, closureReason)
		require.NoError(t, err)

		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "lease close with reason should succeed: %s", txRes.RawLog)

		// Verify lease is closed and has the closure reason
		leaseRes, err := helpers.BillingQueryLease(ctx, tc.chain, leaseUUID4)
		require.NoError(t, err)
		require.Equal(t, billingtypes.LEASE_STATE_CLOSED, leaseRes.Lease.GetState())
		require.Equal(t, closureReason, leaseRes.Lease.GetClosureReason(), "closure reason should be stored on lease")
		t.Logf("Lease %s closed with reason: %s", leaseUUID4, leaseRes.Lease.GetClosureReason())
	})

	t.Run("success: close lease without reason has empty closure_reason", func(t *testing.T) {
		// Create another lease for this test
		leaseUUID5, err := helpers.BillingCreateAndAcknowledgeLease(ctx, tc.chain, tc.tenant1, tc.providerWallet, items)
		require.NoError(t, err)

		// Close without reason
		res, err := helpers.BillingCloseLease(ctx, tc.chain, tc.tenant1, leaseUUID5)
		require.NoError(t, err)

		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "lease close should succeed")

		// Verify lease is closed and closure_reason is empty
		leaseRes, err := helpers.BillingQueryLease(ctx, tc.chain, leaseUUID5)
		require.NoError(t, err)
		require.Equal(t, billingtypes.LEASE_STATE_CLOSED, leaseRes.Lease.GetState())
		require.Empty(t, leaseRes.Lease.GetClosureReason(), "closure reason should be empty when not provided")
	})

	t.Run("success: provider closes lease with reason", func(t *testing.T) {
		// Create another lease for this test
		leaseUUID6, err := helpers.BillingCreateAndAcknowledgeLease(ctx, tc.chain, tc.tenant1, tc.providerWallet, items)
		require.NoError(t, err)

		closureReason := "provider maintenance"
		res, err := helpers.BillingCloseLeaseWithReason(ctx, tc.chain, tc.providerWallet, leaseUUID6, closureReason)
		require.NoError(t, err)

		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "provider close with reason should succeed")

		// Verify closure reason is stored
		leaseRes, err := helpers.BillingQueryLease(ctx, tc.chain, leaseUUID6)
		require.NoError(t, err)
		require.Equal(t, billingtypes.LEASE_STATE_CLOSED, leaseRes.Lease.GetState())
		require.Equal(t, closureReason, leaseRes.Lease.GetClosureReason())
	})
}

// testLeaseRejectAndCancelIndependent tests lease reject and cancel operations.
func testLeaseRejectAndCancelIndependent(t *testing.T, ctx context.Context, tc *billingTestContext) {
	t.Log("=== Testing Lease Reject and Cancel ===")

	node := tc.chain.GetNode()

	// Create a fresh tenant for this test
	rejectUsers := interchaintest.GetAndFundTestUsers(t, ctx, "reject-cancel-tenant", DefaultGenesisAmt, tc.chain)
	rejectTenant := rejectUsers[0]

	// Fund tenant with PWR tokens
	err := node.SendFunds(ctx, tc.authority.KeyName(), ibc.WalletAmount{
		Address: rejectTenant.FormattedAddress(),
		Denom:   tc.pwrDenom,
		Amount:  DefaultGenesisAmt,
	})
	require.NoError(t, err)
	require.NoError(t, testutil.WaitForBlocks(ctx, 2, tc.chain))

	fundAmount := fmt.Sprintf("5000000%s", tc.pwrDenom) // 5 PWR (tenant only has 10 PWR)
	res, err := helpers.BillingFundCredit(ctx, tc.chain, rejectTenant, rejectTenant.FormattedAddress(), fundAmount)
	require.NoError(t, err)
	txRes, err := tc.chain.GetTransaction(res.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), txRes.Code)

	t.Run("success: provider rejects pending lease", func(t *testing.T) {
		// Create a pending lease (not acknowledged)
		items := []string{fmt.Sprintf("%s:1", tc.skuUUID)}
		createRes, err := helpers.BillingCreateLease(ctx, tc.chain, rejectTenant, items)
		require.NoError(t, err)

		txRes, err := tc.chain.GetTransaction(createRes.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code)

		leaseUUID, err := helpers.GetLeaseIDFromTxHash(ctx, tc.chain, createRes.TxHash)
		require.NoError(t, err)

		// Provider rejects the lease
		rejectRes, err := helpers.BillingRejectLease(ctx, tc.chain, tc.providerWallet, leaseUUID, "capacity exceeded")
		require.NoError(t, err)

		rejectTxRes, err := tc.chain.GetTransaction(rejectRes.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), rejectTxRes.Code, "reject should succeed")

		// Verify lease is rejected
		leaseRes, err := helpers.BillingQueryLease(ctx, tc.chain, leaseUUID)
		require.NoError(t, err)
		require.Equal(t, billingtypes.LEASE_STATE_REJECTED, leaseRes.Lease.GetState())
		require.Equal(t, "capacity exceeded", leaseRes.Lease.RejectionReason)
	})

	t.Run("success: tenant cancels pending lease", func(t *testing.T) {
		// Create a pending lease
		items := []string{fmt.Sprintf("%s:1", tc.skuUUID)}
		createRes, err := helpers.BillingCreateLease(ctx, tc.chain, rejectTenant, items)
		require.NoError(t, err)

		txRes, err := tc.chain.GetTransaction(createRes.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code)

		leaseUUID, err := helpers.GetLeaseIDFromTxHash(ctx, tc.chain, createRes.TxHash)
		require.NoError(t, err)

		// Tenant cancels their own pending lease
		cancelRes, err := helpers.BillingCancelLease(ctx, tc.chain, rejectTenant, leaseUUID)
		require.NoError(t, err)

		cancelTxRes, err := tc.chain.GetTransaction(cancelRes.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), cancelTxRes.Code, "cancel should succeed")

		// Verify lease is rejected (cancelled state is REJECTED with reason)
		leaseRes, err := helpers.BillingQueryLease(ctx, tc.chain, leaseUUID)
		require.NoError(t, err)
		require.Equal(t, billingtypes.LEASE_STATE_REJECTED, leaseRes.Lease.GetState())
	})

	t.Run("fail: tenant cannot cancel active lease", func(t *testing.T) {
		// Create and acknowledge a lease
		items := []string{fmt.Sprintf("%s:1", tc.skuUUID)}
		leaseUUID, err := helpers.BillingCreateAndAcknowledgeLease(ctx, tc.chain, rejectTenant, tc.providerWallet, items)
		require.NoError(t, err)

		// Try to cancel active lease
		cancelRes, err := helpers.BillingCancelLease(ctx, tc.chain, rejectTenant, leaseUUID)
		require.NoError(t, err)

		cancelTxRes, err := tc.chain.GetTransaction(cancelRes.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), cancelTxRes.Code, "cancel should fail for active lease")
	})
}
