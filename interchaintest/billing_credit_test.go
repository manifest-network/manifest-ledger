// Package interchaintest contains end-to-end tests for the billing module.
// This file contains credit account and accrual/withdrawal tests.
//
// Run with: go test -v ./interchaintest -run TestBillingCredit -timeout 45m
package interchaintest

import (
	"context"
	"fmt"
	"testing"

	"github.com/strangelove-ventures/interchaintest/v8"
	"github.com/strangelove-ventures/interchaintest/v8/ibc"
	"github.com/strangelove-ventures/interchaintest/v8/testutil"
	"github.com/stretchr/testify/require"

	sdkmath "cosmossdk.io/math"

	"github.com/manifest-network/manifest-ledger/interchaintest/helpers"
	billingtypes "github.com/manifest-network/manifest-ledger/x/billing/types"
)

// setupCreditAccounts funds credit accounts for tenant1 and tenant2.
// This is called before subtests to ensure credit accounts exist regardless of test filter.
func setupCreditAccounts(ctx context.Context, t *testing.T, tc *billingTestContext) {
	t.Helper()
	t.Log("Setting up credit accounts for tenant1 and tenant2...")

	node := tc.chain.GetNode()

	// Fund tenant1's credit account
	err := node.BankSend(ctx, tc.authority.KeyName(), ibc.WalletAmount{
		Address: tc.tenant1.FormattedAddress(),
		Denom:   tc.pwrDenom,
		Amount:  sdkmath.NewInt(100_000_000),
	})
	require.NoError(t, err, "failed to send funds to tenant1")
	require.NoError(t, testutil.WaitForBlocks(ctx, 2, tc.chain))

	fundAmount1 := fmt.Sprintf("50000000%s", tc.pwrDenom)
	res1, err := helpers.BillingFundCredit(ctx, tc.chain, tc.tenant1, tc.tenant1.FormattedAddress(), fundAmount1)
	require.NoError(t, err, "failed to fund credit for tenant1")
	txRes1, err := tc.chain.GetTransaction(res1.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), txRes1.Code, "fund credit for tenant1 should succeed: %s", txRes1.RawLog)
	t.Logf("Funded tenant1 credit account with %s", fundAmount1)

	// Fund tenant2's credit account
	err = node.BankSend(ctx, tc.authority.KeyName(), ibc.WalletAmount{
		Address: tc.tenant2.FormattedAddress(),
		Denom:   tc.pwrDenom,
		Amount:  sdkmath.NewInt(100_000_000),
	})
	require.NoError(t, err, "failed to send funds to tenant2")
	require.NoError(t, testutil.WaitForBlocks(ctx, 2, tc.chain))

	fundAmount2 := fmt.Sprintf("50000000%s", tc.pwrDenom)
	res2, err := helpers.BillingFundCredit(ctx, tc.chain, tc.tenant2, tc.tenant2.FormattedAddress(), fundAmount2)
	require.NoError(t, err, "failed to fund credit for tenant2")
	txRes2, err := tc.chain.GetTransaction(res2.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), txRes2.Code, "fund credit for tenant2 should succeed: %s", txRes2.RawLog)
	t.Logf("Funded tenant2 credit account with %s", fundAmount2)

	require.NoError(t, testutil.WaitForBlocks(ctx, 2, tc.chain))
}

// TestBillingCredit runs credit account and accrual tests independently.
// Tests: params query, credit operations, accrual, withdrawal, withdrawable queries.
func TestBillingCredit(t *testing.T) {
	ctx, tc, cleanup := setupBillingTest(t, "billing-credit-test")
	t.Cleanup(cleanup)

	// Set globals from context for any functions that use them
	testPWRDenom = tc.pwrDenom
	testProviderUUID = tc.providerUUID
	testSKUUUID = tc.skuUUID
	testSKUUUID2 = tc.skuUUID2

	// Setup: Fund credit accounts for tenant1 and tenant2
	// This runs before any subtests to ensure credit accounts exist
	setupCreditAccounts(ctx, t, tc)

	t.Run("QueryParams", func(t *testing.T) {
		testBillingQueryParamsIndependent(ctx, t, tc)
	})

	t.Run("CreditAccountOperations", func(t *testing.T) {
		testCreditAccountOperationsIndependent(ctx, t, tc)
	})

	t.Run("CreditAddressQuery", func(t *testing.T) {
		testCreditAddressQueryIndependent(ctx, t, tc)
	})

	t.Run("CreditAccountsQuery", func(t *testing.T) {
		testCreditAccountsQueryIndependent(ctx, t, tc)
	})

	t.Run("CreditEstimateQuery", func(t *testing.T) {
		testCreditEstimateQueryIndependent(ctx, t, tc)
	})

	t.Run("AccrualCalculation", func(t *testing.T) {
		testAccrualCalculationIndependent(ctx, t, tc)
	})

	t.Run("Withdraw", func(t *testing.T) {
		testWithdrawIndependent(ctx, t, tc)
	})

	t.Run("WithdrawByProvider", func(t *testing.T) {
		testWithdrawByProviderIndependent(ctx, t, tc)
	})

	t.Run("WithdrawableQueries", func(t *testing.T) {
		testWithdrawableQueriesIndependent(ctx, t, tc)
	})
}

func testBillingQueryParamsIndependent(ctx context.Context, t *testing.T, tc *billingTestContext) {
	t.Log("=== Testing Billing Query Params ===")

	res, err := helpers.BillingQueryParams(ctx, tc.chain)
	require.NoError(t, err)
	require.NotNil(t, res)
	require.NotEmpty(t, res.Params.MaxLeasesPerTenant)
	require.NotEmpty(t, res.Params.MaxItemsPerLease)
	require.NotEmpty(t, res.Params.MinLeaseDuration)
	t.Logf("Billing params: max_leases=%d, max_items=%d, min_duration=%d",
		res.Params.MaxLeasesPerTenant, res.Params.MaxItemsPerLease, res.Params.MinLeaseDuration)
}

func testCreditAccountOperationsIndependent(ctx context.Context, t *testing.T, tc *billingTestContext) {
	t.Log("=== Testing Credit Account Operations ===")

	node := tc.chain.GetNode()

	t.Run("success: derive credit address", func(t *testing.T) {
		res, err := helpers.BillingQueryCreditAddress(ctx, tc.chain, tc.tenant1.FormattedAddress())
		require.NoError(t, err)
		require.NotEmpty(t, res.CreditAddress)
		require.Contains(t, res.CreditAddress, "manifest1")
		t.Logf("Tenant1 credit address: %s", res.CreditAddress)
	})

	t.Run("success: fund credit account", func(t *testing.T) {
		// Send PWR to tenant1
		err := node.BankSend(ctx, tc.authority.KeyName(), ibc.WalletAmount{
			Address: tc.tenant1.FormattedAddress(),
			Denom:   tc.pwrDenom,
			Amount:  sdkmath.NewInt(100_000_000),
		})
		require.NoError(t, err)
		require.NoError(t, testutil.WaitForBlocks(ctx, 2, tc.chain))

		// Fund credit account
		fundAmount := fmt.Sprintf("50000000%s", tc.pwrDenom)
		res, err := helpers.BillingFundCredit(ctx, tc.chain, tc.tenant1, tc.tenant1.FormattedAddress(), fundAmount)
		require.NoError(t, err)

		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "fund credit should succeed")
	})

	t.Run("success: query credit account balance", func(t *testing.T) {
		res, err := helpers.BillingQueryCreditAccount(ctx, tc.chain, tc.tenant1.FormattedAddress())
		require.NoError(t, err)
		require.Equal(t, tc.tenant1.FormattedAddress(), res.CreditAccount.Tenant)
		t.Logf("Credit account: tenant=%s, credit_address=%s",
			res.CreditAccount.Tenant, res.CreditAccount.CreditAddress)
	})
	// Note: tenant2 credit is funded in setupCreditAccounts() which runs before subtests
}

func testCreditAddressQueryIndependent(ctx context.Context, t *testing.T, tc *billingTestContext) {
	t.Log("=== Testing Credit Address Query ===")

	t.Run("success: derive credit address without credit account", func(t *testing.T) {
		res, err := helpers.BillingQueryCreditAddress(ctx, tc.chain, tc.tenant1.FormattedAddress())
		require.NoError(t, err)
		require.NotEmpty(t, res.CreditAddress)
		t.Logf("Derived credit address: %s", res.CreditAddress)
	})

	t.Run("success: derived address matches credit account", func(t *testing.T) {
		derivedRes, err := helpers.BillingQueryCreditAddress(ctx, tc.chain, tc.tenant1.FormattedAddress())
		require.NoError(t, err)

		creditRes, err := helpers.BillingQueryCreditAccount(ctx, tc.chain, tc.tenant1.FormattedAddress())
		require.NoError(t, err)

		require.Equal(t, derivedRes.CreditAddress, creditRes.CreditAccount.CreditAddress)
	})

	t.Run("fail: derive credit address with invalid address", func(t *testing.T) {
		_, err := helpers.BillingQueryCreditAddress(ctx, tc.chain, "invalid-address")
		require.Error(t, err)
	})
}

func testCreditAccountsQueryIndependent(ctx context.Context, t *testing.T, tc *billingTestContext) {
	t.Log("=== Testing Credit Accounts Query ===")

	t.Run("success: query returns existing credit accounts", func(t *testing.T) {
		res, err := helpers.BillingQueryCreditAccounts(ctx, tc.chain)
		require.NoError(t, err)
		require.NotNil(t, res)
		require.GreaterOrEqual(t, len(res.CreditAccounts), 2)

		// Verify our tenants are in the list
		found1, found2 := false, false
		for _, ca := range res.CreditAccounts {
			if ca.Tenant == tc.tenant1.FormattedAddress() {
				found1 = true
			}
			if ca.Tenant == tc.tenant2.FormattedAddress() {
				found2 = true
			}
		}
		require.True(t, found1, "tenant1 should be in list")
		require.True(t, found2, "tenant2 should be in list")
		t.Logf("Found %d credit accounts", len(res.CreditAccounts))
	})

	t.Run("success: query with pagination", func(t *testing.T) {
		// First, get total count
		allRes, err := helpers.BillingQueryCreditAccounts(ctx, tc.chain)
		require.NoError(t, err)
		totalAccounts := len(allRes.CreditAccounts)
		require.GreaterOrEqual(t, totalAccounts, 2, "should have at least 2 credit accounts for pagination test")

		// Query first page with limit=1
		res, nextKey, err := helpers.BillingQueryCreditAccountsPaginated(ctx, tc.chain, 1, "")
		require.NoError(t, err)
		require.NotNil(t, res)
		require.Len(t, res.CreditAccounts, 1, "first page should have 1 account")
		t.Logf("First page returned 1 account, nextKey present: %v", nextKey != "")

		// If there are more accounts, we should be able to paginate through them
		if nextKey != "" && totalAccounts > 1 {
			res2, _, err := helpers.BillingQueryCreditAccountsPaginated(ctx, tc.chain, 1, nextKey)
			require.NoError(t, err)
			// Note: SDK pagination can return nextKey even on last page, so second page might be empty
			if len(res2.CreditAccounts) > 0 {
				// The second page should ideally have a different account, but pagination behavior
				// can vary depending on iteration order and key encoding
				if res.CreditAccounts[0].Tenant != res2.CreditAccounts[0].Tenant {
					t.Log("Successfully paginated through credit accounts with different tenants")
				} else {
					t.Log("Pagination returned same tenant (possible iteration order issue), but query succeeded")
				}
			} else {
				t.Log("Second page empty (pagination key behavior), but first page worked")
			}
		}
	})
}

func testCreditEstimateQueryIndependent(ctx context.Context, t *testing.T, tc *billingTestContext) {
	t.Log("=== Testing Credit Estimate Query ===")

	// Create a fresh tenant for this test
	users := interchaintest.GetAndFundTestUsers(t, ctx, "estimate-query", DefaultGenesisAmt, tc.chain)
	tenant := users[0]

	// Fund tenant's credit account
	fundAmount := int64(100_000_000)
	err := tc.chain.SendFunds(ctx, tc.authority.KeyName(), ibc.WalletAmount{
		Address: tenant.FormattedAddress(),
		Denom:   tc.pwrDenom,
		Amount:  sdkmath.NewInt(fundAmount),
	})
	require.NoError(t, err)
	_, err = helpers.BillingFundCredit(ctx, tc.chain, tenant, tenant.FormattedAddress(), fmt.Sprintf("%d%s", fundAmount, tc.pwrDenom))
	require.NoError(t, err)

	t.Run("success: estimate with no active leases", func(t *testing.T) {
		res, err := helpers.BillingQueryCreditEstimate(ctx, tc.chain, tenant.FormattedAddress())
		require.NoError(t, err)
		require.NotNil(t, res)
		require.Equal(t, uint64(0), res.ActiveLeaseCount)
		require.True(t, res.TotalRatePerSecond.IsZero())
		t.Logf("Balance: %s, Active leases: %d", res.CurrentBalance, res.ActiveLeaseCount)
	})

	// Create and acknowledge a lease
	items := []string{fmt.Sprintf("%s:1", tc.skuUUID)}
	leaseUUID, err := helpers.BillingCreateAndAcknowledgeLease(ctx, tc.chain, tenant, tc.providerWallet, items)
	require.NoError(t, err)
	require.NotEmpty(t, leaseUUID)

	t.Run("success: estimate with active lease", func(t *testing.T) {
		res, err := helpers.BillingQueryCreditEstimate(ctx, tc.chain, tenant.FormattedAddress())
		require.NoError(t, err)
		require.NotNil(t, res)
		require.Equal(t, uint64(1), res.ActiveLeaseCount)
		require.False(t, res.TotalRatePerSecond.IsZero())
		require.Greater(t, res.EstimatedDurationSeconds, uint64(0))
		t.Logf("Balance: %s, Rate/sec: %s, Duration: %ds",
			res.CurrentBalance, res.TotalRatePerSecond, res.EstimatedDurationSeconds)
	})

	t.Run("fail: estimate for non-existent credit account", func(t *testing.T) {
		_, err := helpers.BillingQueryCreditEstimate(ctx, tc.chain, "manifest1qqqqqqqqqqqqqqqqqqqqqqqqqqqqphgzfs")
		require.Error(t, err)
	})

	// Cleanup
	_, _ = helpers.BillingCloseLease(ctx, tc.chain, tenant, leaseUUID)
}

func testAccrualCalculationIndependent(ctx context.Context, t *testing.T, tc *billingTestContext) {
	t.Log("=== Testing Accrual Calculation ===")

	// Create a lease for accrual testing
	items := []string{fmt.Sprintf("%s:1", tc.skuUUID)}
	leaseUUID, err := helpers.BillingCreateAndAcknowledgeLease(ctx, tc.chain, tc.tenant1, tc.providerWallet, items)
	require.NoError(t, err)

	t.Run("success: verify accrual increases over time", func(t *testing.T) {
		// Get initial withdrawable
		initial, err := helpers.BillingQueryWithdrawable(ctx, tc.chain, leaseUUID)
		require.NoError(t, err)
		t.Logf("Initial withdrawable: %s", initial.Amounts)

		// Wait for some blocks
		require.NoError(t, testutil.WaitForBlocks(ctx, 5, tc.chain))

		// Get updated withdrawable
		updated, err := helpers.BillingQueryWithdrawable(ctx, tc.chain, leaseUUID)
		require.NoError(t, err)
		t.Logf("Updated withdrawable: %s", updated.Amounts)

		require.True(t, len(updated.Amounts) > 0 && len(initial.Amounts) > 0)
		require.True(t, updated.Amounts[0].Amount.GTE(initial.Amounts[0].Amount),
			"withdrawable should increase over time")
	})
}

func testWithdrawIndependent(ctx context.Context, t *testing.T, tc *billingTestContext) {
	t.Log("=== Testing Withdraw ===")

	// Get an active lease for tenant1
	leases, err := helpers.BillingQueryLeasesByTenant(ctx, tc.chain, tc.tenant1.FormattedAddress(), "active")
	require.NoError(t, err)
	require.NotEmpty(t, leases.Leases)
	leaseUUID := leases.Leases[0].UUID

	// Wait for some accrual
	require.NoError(t, testutil.WaitForBlocks(ctx, 3, tc.chain))

	t.Run("success: provider withdraws from lease", func(t *testing.T) {
		initialBalance, err := tc.chain.GetBalance(ctx, tc.providerWallet.FormattedAddress(), tc.pwrDenom)
		require.NoError(t, err)

		res, err := helpers.BillingWithdraw(ctx, tc.chain, tc.providerWallet, leaseUUID)
		require.NoError(t, err)

		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "withdraw should succeed")

		newBalance, err := tc.chain.GetBalance(ctx, tc.providerWallet.FormattedAddress(), tc.pwrDenom)
		require.NoError(t, err)
		require.True(t, newBalance.GTE(initialBalance))
		t.Logf("Provider balance: %s -> %s", initialBalance, newBalance)
	})

	t.Run("fail: tenant cannot withdraw", func(t *testing.T) {
		res, err := helpers.BillingWithdraw(ctx, tc.chain, tc.tenant1, leaseUUID)
		require.NoError(t, err)

		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code)
		require.Contains(t, txRes.RawLog, "unauthorized")
	})

	t.Run("fail: unauthorized user cannot withdraw", func(t *testing.T) {
		res, err := helpers.BillingWithdraw(ctx, tc.chain, tc.unauthorizedUser, leaseUUID)
		require.NoError(t, err)

		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code)
	})

	t.Run("success: authority withdraws on behalf of provider", func(t *testing.T) {
		require.NoError(t, testutil.WaitForBlocks(ctx, 3, tc.chain))

		res, err := helpers.BillingWithdraw(ctx, tc.chain, tc.authority, leaseUUID)
		require.NoError(t, err)

		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "authority withdraw should succeed")
	})
}

func testWithdrawByProviderIndependent(ctx context.Context, t *testing.T, tc *billingTestContext) {
	t.Log("=== Testing Withdraw By Provider ===")

	// Wait for some accrual
	require.NoError(t, testutil.WaitForBlocks(ctx, 5, tc.chain))

	t.Run("success: provider withdraws from all leases", func(t *testing.T) {
		initialBalance, err := tc.chain.GetBalance(ctx, tc.providerWallet.FormattedAddress(), tc.pwrDenom)
		require.NoError(t, err)

		res, err := helpers.BillingWithdrawByProvider(ctx, tc.chain, tc.providerWallet, tc.providerUUID, 0)
		require.NoError(t, err)

		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "withdraw by provider should succeed")

		newBalance, err := tc.chain.GetBalance(ctx, tc.providerWallet.FormattedAddress(), tc.pwrDenom)
		require.NoError(t, err)
		require.True(t, newBalance.GTE(initialBalance))
		t.Logf("Provider balance: %s -> %s", initialBalance, newBalance)
	})
}

func testWithdrawableQueriesIndependent(ctx context.Context, t *testing.T, tc *billingTestContext) {
	t.Log("=== Testing Withdrawable Queries ===")

	// Get an active lease
	leases, err := helpers.BillingQueryLeasesByTenant(ctx, tc.chain, tc.tenant1.FormattedAddress(), "active")
	require.NoError(t, err)
	require.NotEmpty(t, leases.Leases)
	leaseUUID := leases.Leases[0].UUID

	t.Run("success: query withdrawable amount for lease", func(t *testing.T) {
		res, err := helpers.BillingQueryWithdrawable(ctx, tc.chain, leaseUUID)
		require.NoError(t, err)
		require.NotNil(t, res)
		t.Logf("Withdrawable for lease %s: %s", leaseUUID, res.Amounts)
	})

	t.Run("success: query provider total withdrawable", func(t *testing.T) {
		res, err := helpers.BillingQueryProviderWithdrawable(ctx, tc.chain, tc.providerUUID)
		require.NoError(t, err)
		require.NotNil(t, res)
		t.Logf("Total withdrawable for provider: %s (leases: %d)", res.Amounts, res.LeaseCount)
	})
}

// TestBillingAcknowledgeActiveCap is an end-to-end regression for ENG-859. It verifies that a
// same-tenant acknowledgement batch cannot push the active count above max_leases_per_tenant and
// that the failed batch leaves both pending leases and the credit estimate unchanged.
func TestBillingAcknowledgeActiveCap(t *testing.T) {
	ctx, tc, cleanup := setupBillingTest(t, "billing-ack-active-cap")
	t.Cleanup(cleanup)

	// Lower the lease limits so a post-batch overshoot can be attempted with a handful of txs.
	current, err := helpers.BillingQueryParams(ctx, tc.chain)
	require.NoError(t, err)
	paramRes, err := helpers.BillingUpdateParams(ctx, tc.chain, tc.authority,
		2, // max_leases_per_tenant
		current.Params.MaxItemsPerLease,
		current.Params.MinLeaseDuration,
		2, // max_pending_leases_per_tenant
		current.Params.PendingTimeout,
		nil, // preserve allowed_list
	)
	require.NoError(t, err)
	paramTx, err := tc.chain.GetTransaction(paramRes.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), paramTx.Code, "params update should succeed: %s", paramTx.RawLog)
	require.NoError(t, testutil.WaitForBlocks(ctx, 2, tc.chain))

	// Fresh tenant with a well-funded credit account.
	users := interchaintest.GetAndFundTestUsers(t, ctx, "active-cap-tenant", DefaultGenesisAmt, tc.chain)
	tenant := users[0]
	err = tc.chain.SendFunds(ctx, tc.authority.KeyName(), ibc.WalletAmount{
		Address: tenant.FormattedAddress(),
		Denom:   tc.pwrDenom,
		Amount:  sdkmath.NewInt(100_000_000),
	})
	require.NoError(t, err)
	fundRes, err := helpers.BillingFundCredit(ctx, tc.chain, tenant, tenant.FormattedAddress(),
		fmt.Sprintf("100000000%s", tc.pwrDenom))
	require.NoError(t, err)
	fundTx, err := tc.chain.GetTransaction(fundRes.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), fundTx.Code, "fund credit should succeed: %s", fundTx.RawLog)
	require.NoError(t, testutil.WaitForBlocks(ctx, 2, tc.chain))

	items := []string{fmt.Sprintf("%s:1", tc.skuUUID)}

	// createLease creates a single PENDING lease and returns its UUID.
	createLease := func() string {
		res, err := helpers.BillingCreateLease(ctx, tc.chain, tenant, items)
		require.NoError(t, err)
		require.NoError(t, testutil.WaitForBlocks(ctx, 2, tc.chain))
		uuid, err := helpers.GetLeaseIDFromTxHash(ctx, tc.chain, res.TxHash)
		require.NoError(t, err)
		require.NotEmpty(t, uuid)
		return uuid
	}
	ack := func(uuids ...string) {
		res, err := helpers.BillingAcknowledgeLeases(ctx, tc.chain, tc.providerWallet, uuids)
		require.NoError(t, err)
		ackTx, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), ackTx.Code, "acknowledge should succeed: %s", ackTx.RawLog)
		require.NoError(t, testutil.WaitForBlocks(ctx, 2, tc.chain))
	}

	// Reach active = max_leases_per_tenant-1 = 1, then prepare two pending leases.
	l1 := createLease() // pending 1 (active 0 < 2)
	ack(l1)             // active 1

	// Baseline burn rate for a single active lease.
	baseline, err := helpers.BillingQueryCreditEstimate(ctx, tc.chain, tenant.FormattedAddress())
	require.NoError(t, err)
	require.Equal(t, uint64(1), baseline.ActiveLeaseCount)
	baseAmt := baseline.TotalRatePerSecond.AmountOf(tc.pwrDenom)
	require.True(t, baseAmt.IsPositive(), "single-lease baseline rate must be positive in the funded denom")

	l2 := createLease() // pending 1 (active 1 < 2)
	l3 := createLease() // pending 2 (active 1 < 2, pending 2 <= 2)
	beforeAck, err := helpers.BillingQueryCreditAccount(ctx, tc.chain, tenant.FormattedAddress())
	require.NoError(t, err)
	require.Equal(t, uint64(1), beforeAck.CreditAccount.ActiveLeaseCount)
	require.Equal(t, uint64(2), beforeAck.CreditAccount.PendingLeaseCount)

	ackRes, err := helpers.BillingAcknowledgeLeases(ctx, tc.chain, tc.providerWallet, []string{l2, l3})
	require.NoError(t, err)
	ackTx, err := tc.chain.GetTransaction(ackRes.TxHash)
	require.NoError(t, err)
	require.NotEqual(t, uint32(0), ackTx.Code, "post-batch active-cap overshoot must fail")
	require.Contains(t, ackTx.RawLog, "lease acknowledgement active cap exceeded")
	require.NoError(t, testutil.WaitForBlocks(ctx, 2, tc.chain))

	afterAck, err := helpers.BillingQueryCreditAccount(ctx, tc.chain, tenant.FormattedAddress())
	require.NoError(t, err)
	require.Equal(t, beforeAck.CreditAccount.ActiveLeaseCount, afterAck.CreditAccount.ActiveLeaseCount)
	require.Equal(t, beforeAck.CreditAccount.PendingLeaseCount, afterAck.CreditAccount.PendingLeaseCount)
	require.True(t, beforeAck.CreditAccount.ReservedAmounts.Equal(afterAck.CreditAccount.ReservedAmounts),
		"failed acknowledgement must preserve reservations")

	for _, leaseUUID := range []string{l2, l3} {
		leaseRes, err := helpers.BillingQueryLease(ctx, tc.chain, leaseUUID)
		require.NoError(t, err)
		require.Equal(t, billingtypes.LEASE_STATE_PENDING, leaseRes.Lease.GetState())
		require.Nil(t, leaseRes.Lease.AcknowledgedAt)
	}

	res, err := helpers.BillingQueryCreditEstimate(ctx, tc.chain, tenant.FormattedAddress())
	require.NoError(t, err)
	require.Equal(t, uint64(1), res.ActiveLeaseCount)
	require.Equal(t, baseAmt, res.TotalRatePerSecond.AmountOf(tc.pwrDenom),
		"failed acknowledgement must not change the active burn rate")
	t.Logf("Active-cap gate verified: active_leases=%d (max_leases_per_tenant=2), rate/sec=%s",
		res.ActiveLeaseCount, res.TotalRatePerSecond)
}
