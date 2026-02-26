// Package interchaintest contains end-to-end tests for the billing module.
// This file contains credit reservation tests (overbooking prevention).
//
// Run with: go test -v ./interchaintest -run TestBillingReservation -timeout 45m
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
)

// TestBillingReservation runs credit reservation e2e tests.
// Tests: overbooking prevention, reservation release on close/reject/cancel.
func TestBillingReservation(t *testing.T) {
	ctx, tc, cleanup := setupBillingTest(t, "billing-reservation-test")
	t.Cleanup(cleanup)

	// Set globals from context
	testPWRDenom = tc.pwrDenom
	testProviderUUID = tc.providerUUID
	testSKUUUID = tc.skuUUID
	testSKUUUID2 = tc.skuUUID2

	t.Run("OverbookingPrevention", func(t *testing.T) {
		testOverbookingPrevention(t, ctx, tc)
	})

	t.Run("ReservationReleaseOnClose", func(t *testing.T) {
		testReservationReleaseOnClose(t, ctx, tc)
	})

	t.Run("ReservationReleaseOnReject", func(t *testing.T) {
		testReservationReleaseOnReject(t, ctx, tc)
	})

	t.Run("ReservationReleaseOnCancel", func(t *testing.T) {
		testReservationReleaseOnCancel(t, ctx, tc)
	})

	t.Run("AvailableBalancesQuery", func(t *testing.T) {
		testAvailableBalancesQuery(t, ctx, tc)
	})
}

// testOverbookingPrevention verifies that the credit reservation system prevents
// creating more leases than available credit can support.
func testOverbookingPrevention(t *testing.T, ctx context.Context, tc *billingTestContext) {
	t.Log("=== Testing Overbooking Prevention ===")

	// Create a new tenant for this test to have isolated credit
	users := interchaintest.GetAndFundTestUsers(t, ctx, "overbooking-tenant", DefaultGenesisAmt, tc.chain)
	tenant := users[0]

	// SKU rate: 3600000 upwr per hour = 1000 per second
	// min_lease_duration: 3600 seconds (default)
	// Reservation per lease: 1000 * 3600 = 3,600,000 upwr

	// Fund tenant with enough for exactly 2 leases (7,200,000) but not 3 (10,800,000)
	// We'll fund 8,000,000 to be safe above 2 but below 3
	fundAmount := int64(8_000_000)

	// Send PWR to tenant
	err := tc.chain.GetNode().SendFunds(ctx, tc.authority.KeyName(), ibc.WalletAmount{
		Address: tenant.FormattedAddress(),
		Denom:   tc.pwrDenom,
		Amount:  sdkmath.NewInt(fundAmount),
	})
	require.NoError(t, err)
	require.NoError(t, testutil.WaitForBlocks(ctx, 2, tc.chain))

	// Fund credit account
	creditFundAmount := fmt.Sprintf("%d%s", fundAmount, tc.pwrDenom)
	res, err := helpers.BillingFundCredit(ctx, tc.chain, tenant, tenant.FormattedAddress(), creditFundAmount)
	require.NoError(t, err)
	txRes, err := tc.chain.GetTransaction(res.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), txRes.Code, "fund credit should succeed: %s", txRes.RawLog)
	t.Logf("Funded credit account with %s", creditFundAmount)

	// Query initial credit account state
	creditRes, err := helpers.BillingQueryCreditAccount(ctx, tc.chain, tenant.FormattedAddress())
	require.NoError(t, err)
	t.Logf("Initial balance: %s, reserved: %s, available: %s",
		creditRes.Balances, creditRes.CreditAccount.ReservedAmounts, creditRes.AvailableBalances)

	// Verify initial state - all balance should be available
	require.Equal(t, creditRes.Balances.AmountOf(tc.pwrDenom), creditRes.AvailableBalances.AmountOf(tc.pwrDenom),
		"initially all balance should be available")
	require.True(t, creditRes.CreditAccount.ReservedAmounts.IsZero(),
		"initially no reservations")

	// Create first lease - should succeed
	items := []string{fmt.Sprintf("%s:1", tc.skuUUID)}
	res, err = helpers.BillingCreateLease(ctx, tc.chain, tenant, items)
	require.NoError(t, err)
	txRes, err = tc.chain.GetTransaction(res.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), txRes.Code, "first lease creation should succeed: %s", txRes.RawLog)

	leaseID1, err := helpers.GetLeaseIDFromTxHash(ctx, tc.chain, res.TxHash)
	require.NoError(t, err)
	t.Logf("Created lease 1: %s", leaseID1)

	// Query credit account - should show reservation
	creditRes, err = helpers.BillingQueryCreditAccount(ctx, tc.chain, tenant.FormattedAddress())
	require.NoError(t, err)
	t.Logf("After lease 1 - balance: %s, reserved: %s, available: %s",
		creditRes.Balances, creditRes.CreditAccount.ReservedAmounts, creditRes.AvailableBalances)

	reserved1 := creditRes.CreditAccount.ReservedAmounts.AmountOf(tc.pwrDenom)
	require.True(t, reserved1.GT(sdkmath.ZeroInt()), "should have reservation after first lease")

	// Create second lease - should succeed
	res, err = helpers.BillingCreateLease(ctx, tc.chain, tenant, items)
	require.NoError(t, err)
	txRes, err = tc.chain.GetTransaction(res.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), txRes.Code, "second lease creation should succeed: %s", txRes.RawLog)

	leaseID2, err := helpers.GetLeaseIDFromTxHash(ctx, tc.chain, res.TxHash)
	require.NoError(t, err)
	t.Logf("Created lease 2: %s", leaseID2)

	// Query credit account - should show increased reservation
	creditRes, err = helpers.BillingQueryCreditAccount(ctx, tc.chain, tenant.FormattedAddress())
	require.NoError(t, err)
	t.Logf("After lease 2 - balance: %s, reserved: %s, available: %s",
		creditRes.Balances, creditRes.CreditAccount.ReservedAmounts, creditRes.AvailableBalances)

	reserved2 := creditRes.CreditAccount.ReservedAmounts.AmountOf(tc.pwrDenom)
	require.True(t, reserved2.GT(reserved1), "reservation should increase after second lease")

	// Create third lease - should FAIL due to insufficient available credit
	res, err = helpers.BillingCreateLease(ctx, tc.chain, tenant, items)
	require.NoError(t, err)
	txRes, err = tc.chain.GetTransaction(res.TxHash)
	require.NoError(t, err)
	require.NotEqual(t, uint32(0), txRes.Code, "third lease creation should fail")
	require.Contains(t, txRes.RawLog, "insufficient", "error should mention insufficient credit")
	t.Logf("Third lease correctly rejected: %s", txRes.RawLog)

	// Verify reservation unchanged after failed attempt
	creditRes, err = helpers.BillingQueryCreditAccount(ctx, tc.chain, tenant.FormattedAddress())
	require.NoError(t, err)
	require.Equal(t, reserved2, creditRes.CreditAccount.ReservedAmounts.AmountOf(tc.pwrDenom),
		"reservation should be unchanged after failed lease creation")

	// Cleanup: cancel the pending leases
	_, err = helpers.BillingCancelLease(ctx, tc.chain, tenant, leaseID1)
	require.NoError(t, err)
	_, err = helpers.BillingCancelLease(ctx, tc.chain, tenant, leaseID2)
	require.NoError(t, err)
	require.NoError(t, testutil.WaitForBlocks(ctx, 2, tc.chain))
}

// testReservationReleaseOnClose verifies that closing an active lease releases its reservation.
func testReservationReleaseOnClose(t *testing.T, ctx context.Context, tc *billingTestContext) {
	t.Log("=== Testing Reservation Release On Close ===")

	// Create a new tenant for this test
	users := interchaintest.GetAndFundTestUsers(t, ctx, "close-release-tenant", DefaultGenesisAmt, tc.chain)
	tenant := users[0]

	// Fund with enough for 2 leases but not 3
	fundAmount := int64(8_000_000)

	err := tc.chain.GetNode().SendFunds(ctx, tc.authority.KeyName(), ibc.WalletAmount{
		Address: tenant.FormattedAddress(),
		Denom:   tc.pwrDenom,
		Amount:  sdkmath.NewInt(fundAmount),
	})
	require.NoError(t, err)
	require.NoError(t, testutil.WaitForBlocks(ctx, 2, tc.chain))

	creditFundAmount := fmt.Sprintf("%d%s", fundAmount, tc.pwrDenom)
	res, err := helpers.BillingFundCredit(ctx, tc.chain, tenant, tenant.FormattedAddress(), creditFundAmount)
	require.NoError(t, err)
	txRes, err := tc.chain.GetTransaction(res.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), txRes.Code)

	// Create and acknowledge 2 leases to make them ACTIVE
	items := []string{fmt.Sprintf("%s:1", tc.skuUUID)}

	leaseID1, err := helpers.BillingCreateAndAcknowledgeLease(ctx, tc.chain, tenant, tc.providerWallet, items)
	require.NoError(t, err)
	t.Logf("Created and acknowledged lease 1: %s", leaseID1)

	leaseID2, err := helpers.BillingCreateAndAcknowledgeLease(ctx, tc.chain, tenant, tc.providerWallet, items)
	require.NoError(t, err)
	t.Logf("Created and acknowledged lease 2: %s", leaseID2)

	// Query credit - should show reservations for 2 leases
	creditRes, err := helpers.BillingQueryCreditAccount(ctx, tc.chain, tenant.FormattedAddress())
	require.NoError(t, err)
	reservedBefore := creditRes.CreditAccount.ReservedAmounts.AmountOf(tc.pwrDenom)
	availableBefore := creditRes.AvailableBalances.AmountOf(tc.pwrDenom)
	t.Logf("Before close - reserved: %s, available: %s", reservedBefore, availableBefore)

	// Third lease should fail
	res, err = helpers.BillingCreateLease(ctx, tc.chain, tenant, items)
	require.NoError(t, err)
	txRes, err = tc.chain.GetTransaction(res.TxHash)
	require.NoError(t, err)
	require.NotEqual(t, uint32(0), txRes.Code, "third lease should fail before close")

	// Close lease 1
	res, err = helpers.BillingCloseLease(ctx, tc.chain, tenant, leaseID1)
	require.NoError(t, err)
	txRes, err = tc.chain.GetTransaction(res.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), txRes.Code, "close lease should succeed: %s", txRes.RawLog)
	t.Logf("Closed lease 1: %s", leaseID1)

	require.NoError(t, testutil.WaitForBlocks(ctx, 2, tc.chain))

	// Query credit - reservation should be reduced
	creditRes, err = helpers.BillingQueryCreditAccount(ctx, tc.chain, tenant.FormattedAddress())
	require.NoError(t, err)
	reservedAfter := creditRes.CreditAccount.ReservedAmounts.AmountOf(tc.pwrDenom)
	availableAfter := creditRes.AvailableBalances.AmountOf(tc.pwrDenom)
	t.Logf("After close - reserved: %s, available: %s", reservedAfter, availableAfter)

	require.True(t, reservedAfter.LT(reservedBefore), "reservation should decrease after close")
	require.True(t, availableAfter.GT(availableBefore), "available should increase after close")

	// Now third lease should succeed (after closing first)
	res, err = helpers.BillingCreateLease(ctx, tc.chain, tenant, items)
	require.NoError(t, err)
	txRes, err = tc.chain.GetTransaction(res.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), txRes.Code, "third lease should succeed after close: %s", txRes.RawLog)

	leaseID3, err := helpers.GetLeaseIDFromTxHash(ctx, tc.chain, res.TxHash)
	require.NoError(t, err)
	t.Logf("Created lease 3 after closing lease 1: %s", leaseID3)

	// Cleanup
	_, _ = helpers.BillingCloseLease(ctx, tc.chain, tenant, leaseID2)
	_, _ = helpers.BillingCancelLease(ctx, tc.chain, tenant, leaseID3)
	require.NoError(t, testutil.WaitForBlocks(ctx, 2, tc.chain))
}

// testReservationReleaseOnReject verifies that rejecting a pending lease releases its reservation.
func testReservationReleaseOnReject(t *testing.T, ctx context.Context, tc *billingTestContext) {
	t.Log("=== Testing Reservation Release On Reject ===")

	// Create a new tenant for this test
	users := interchaintest.GetAndFundTestUsers(t, ctx, "reject-release-tenant", DefaultGenesisAmt, tc.chain)
	tenant := users[0]

	// Fund with enough for 2 leases but not 3
	fundAmount := int64(8_000_000)

	err := tc.chain.GetNode().SendFunds(ctx, tc.authority.KeyName(), ibc.WalletAmount{
		Address: tenant.FormattedAddress(),
		Denom:   tc.pwrDenom,
		Amount:  sdkmath.NewInt(fundAmount),
	})
	require.NoError(t, err)
	require.NoError(t, testutil.WaitForBlocks(ctx, 2, tc.chain))

	creditFundAmount := fmt.Sprintf("%d%s", fundAmount, tc.pwrDenom)
	res, err := helpers.BillingFundCredit(ctx, tc.chain, tenant, tenant.FormattedAddress(), creditFundAmount)
	require.NoError(t, err)
	txRes, err := tc.chain.GetTransaction(res.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), txRes.Code)

	// Create 2 pending leases (don't acknowledge)
	items := []string{fmt.Sprintf("%s:1", tc.skuUUID)}

	res, err = helpers.BillingCreateLease(ctx, tc.chain, tenant, items)
	require.NoError(t, err)
	txRes, err = tc.chain.GetTransaction(res.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), txRes.Code)
	leaseID1, err := helpers.GetLeaseIDFromTxHash(ctx, tc.chain, res.TxHash)
	require.NoError(t, err)
	t.Logf("Created pending lease 1: %s", leaseID1)

	res, err = helpers.BillingCreateLease(ctx, tc.chain, tenant, items)
	require.NoError(t, err)
	txRes, err = tc.chain.GetTransaction(res.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), txRes.Code)
	leaseID2, err := helpers.GetLeaseIDFromTxHash(ctx, tc.chain, res.TxHash)
	require.NoError(t, err)
	t.Logf("Created pending lease 2: %s", leaseID2)

	// Query credit - should show reservations
	creditRes, err := helpers.BillingQueryCreditAccount(ctx, tc.chain, tenant.FormattedAddress())
	require.NoError(t, err)
	reservedBefore := creditRes.CreditAccount.ReservedAmounts.AmountOf(tc.pwrDenom)
	t.Logf("Before reject - reserved: %s", reservedBefore)

	// Third lease should fail
	res, err = helpers.BillingCreateLease(ctx, tc.chain, tenant, items)
	require.NoError(t, err)
	txRes, err = tc.chain.GetTransaction(res.TxHash)
	require.NoError(t, err)
	require.NotEqual(t, uint32(0), txRes.Code, "third lease should fail before reject")

	// Provider rejects lease 1
	res, err = helpers.BillingRejectLease(ctx, tc.chain, tc.providerWallet, leaseID1, "Test rejection")
	require.NoError(t, err)
	txRes, err = tc.chain.GetTransaction(res.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), txRes.Code, "reject should succeed: %s", txRes.RawLog)
	t.Logf("Rejected lease 1: %s", leaseID1)

	require.NoError(t, testutil.WaitForBlocks(ctx, 2, tc.chain))

	// Query credit - reservation should be reduced
	creditRes, err = helpers.BillingQueryCreditAccount(ctx, tc.chain, tenant.FormattedAddress())
	require.NoError(t, err)
	reservedAfter := creditRes.CreditAccount.ReservedAmounts.AmountOf(tc.pwrDenom)
	t.Logf("After reject - reserved: %s", reservedAfter)

	require.True(t, reservedAfter.LT(reservedBefore), "reservation should decrease after reject")

	// Now third lease should succeed
	res, err = helpers.BillingCreateLease(ctx, tc.chain, tenant, items)
	require.NoError(t, err)
	txRes, err = tc.chain.GetTransaction(res.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), txRes.Code, "third lease should succeed after reject: %s", txRes.RawLog)

	leaseID3, err := helpers.GetLeaseIDFromTxHash(ctx, tc.chain, res.TxHash)
	require.NoError(t, err)
	t.Logf("Created lease 3 after rejecting lease 1: %s", leaseID3)

	// Cleanup
	_, _ = helpers.BillingRejectLease(ctx, tc.chain, tc.providerWallet, leaseID2, "cleanup")
	_, _ = helpers.BillingRejectLease(ctx, tc.chain, tc.providerWallet, leaseID3, "cleanup")
	require.NoError(t, testutil.WaitForBlocks(ctx, 2, tc.chain))
}

// testReservationReleaseOnCancel verifies that cancelling a pending lease releases its reservation.
func testReservationReleaseOnCancel(t *testing.T, ctx context.Context, tc *billingTestContext) {
	t.Log("=== Testing Reservation Release On Cancel ===")

	// Create a new tenant for this test
	users := interchaintest.GetAndFundTestUsers(t, ctx, "cancel-release-tenant", DefaultGenesisAmt, tc.chain)
	tenant := users[0]

	// Fund with enough for 2 leases but not 3
	fundAmount := int64(8_000_000)

	err := tc.chain.GetNode().SendFunds(ctx, tc.authority.KeyName(), ibc.WalletAmount{
		Address: tenant.FormattedAddress(),
		Denom:   tc.pwrDenom,
		Amount:  sdkmath.NewInt(fundAmount),
	})
	require.NoError(t, err)
	require.NoError(t, testutil.WaitForBlocks(ctx, 2, tc.chain))

	creditFundAmount := fmt.Sprintf("%d%s", fundAmount, tc.pwrDenom)
	res, err := helpers.BillingFundCredit(ctx, tc.chain, tenant, tenant.FormattedAddress(), creditFundAmount)
	require.NoError(t, err)
	txRes, err := tc.chain.GetTransaction(res.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), txRes.Code)

	// Create 2 pending leases
	items := []string{fmt.Sprintf("%s:1", tc.skuUUID)}

	res, err = helpers.BillingCreateLease(ctx, tc.chain, tenant, items)
	require.NoError(t, err)
	txRes, err = tc.chain.GetTransaction(res.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), txRes.Code)
	leaseID1, err := helpers.GetLeaseIDFromTxHash(ctx, tc.chain, res.TxHash)
	require.NoError(t, err)
	t.Logf("Created pending lease 1: %s", leaseID1)

	res, err = helpers.BillingCreateLease(ctx, tc.chain, tenant, items)
	require.NoError(t, err)
	txRes, err = tc.chain.GetTransaction(res.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), txRes.Code)
	leaseID2, err := helpers.GetLeaseIDFromTxHash(ctx, tc.chain, res.TxHash)
	require.NoError(t, err)
	t.Logf("Created pending lease 2: %s", leaseID2)

	// Query credit - should show reservations
	creditRes, err := helpers.BillingQueryCreditAccount(ctx, tc.chain, tenant.FormattedAddress())
	require.NoError(t, err)
	reservedBefore := creditRes.CreditAccount.ReservedAmounts.AmountOf(tc.pwrDenom)
	t.Logf("Before cancel - reserved: %s", reservedBefore)

	// Third lease should fail
	res, err = helpers.BillingCreateLease(ctx, tc.chain, tenant, items)
	require.NoError(t, err)
	txRes, err = tc.chain.GetTransaction(res.TxHash)
	require.NoError(t, err)
	require.NotEqual(t, uint32(0), txRes.Code, "third lease should fail before cancel")

	// Tenant cancels lease 1
	res, err = helpers.BillingCancelLease(ctx, tc.chain, tenant, leaseID1)
	require.NoError(t, err)
	txRes, err = tc.chain.GetTransaction(res.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), txRes.Code, "cancel should succeed: %s", txRes.RawLog)
	t.Logf("Cancelled lease 1: %s", leaseID1)

	require.NoError(t, testutil.WaitForBlocks(ctx, 2, tc.chain))

	// Query credit - reservation should be reduced
	creditRes, err = helpers.BillingQueryCreditAccount(ctx, tc.chain, tenant.FormattedAddress())
	require.NoError(t, err)
	reservedAfter := creditRes.CreditAccount.ReservedAmounts.AmountOf(tc.pwrDenom)
	t.Logf("After cancel - reserved: %s", reservedAfter)

	require.True(t, reservedAfter.LT(reservedBefore), "reservation should decrease after cancel")

	// Now third lease should succeed
	res, err = helpers.BillingCreateLease(ctx, tc.chain, tenant, items)
	require.NoError(t, err)
	txRes, err = tc.chain.GetTransaction(res.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), txRes.Code, "third lease should succeed after cancel: %s", txRes.RawLog)

	leaseID3, err := helpers.GetLeaseIDFromTxHash(ctx, tc.chain, res.TxHash)
	require.NoError(t, err)
	t.Logf("Created lease 3 after cancelling lease 1: %s", leaseID3)

	// Cleanup
	_, _ = helpers.BillingCancelLease(ctx, tc.chain, tenant, leaseID2)
	_, _ = helpers.BillingCancelLease(ctx, tc.chain, tenant, leaseID3)
	require.NoError(t, testutil.WaitForBlocks(ctx, 2, tc.chain))
}

// testAvailableBalancesQuery verifies the credit account query returns correct available_balances.
func testAvailableBalancesQuery(t *testing.T, ctx context.Context, tc *billingTestContext) {
	t.Log("=== Testing Available Balances Query ===")

	// Create a new tenant for this test
	users := interchaintest.GetAndFundTestUsers(t, ctx, "available-query-tenant", DefaultGenesisAmt, tc.chain)
	tenant := users[0]

	fundAmount := int64(10_000_000)

	err := tc.chain.GetNode().SendFunds(ctx, tc.authority.KeyName(), ibc.WalletAmount{
		Address: tenant.FormattedAddress(),
		Denom:   tc.pwrDenom,
		Amount:  sdkmath.NewInt(fundAmount),
	})
	require.NoError(t, err)
	require.NoError(t, testutil.WaitForBlocks(ctx, 2, tc.chain))

	creditFundAmount := fmt.Sprintf("%d%s", fundAmount, tc.pwrDenom)
	res, err := helpers.BillingFundCredit(ctx, tc.chain, tenant, tenant.FormattedAddress(), creditFundAmount)
	require.NoError(t, err)
	txRes, err := tc.chain.GetTransaction(res.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), txRes.Code)

	// Query initial state - all balance should be available
	creditRes, err := helpers.BillingQueryCreditAccount(ctx, tc.chain, tenant.FormattedAddress())
	require.NoError(t, err)

	balance := creditRes.Balances.AmountOf(tc.pwrDenom)
	available := creditRes.AvailableBalances.AmountOf(tc.pwrDenom)
	reserved := creditRes.CreditAccount.ReservedAmounts.AmountOf(tc.pwrDenom)

	t.Logf("Initial - balance: %s, available: %s, reserved: %s", balance, available, reserved)

	require.Equal(t, balance, available, "initially all balance should be available")
	require.True(t, reserved.IsZero(), "initially no reservations")

	// Create a lease
	items := []string{fmt.Sprintf("%s:1", tc.skuUUID)}
	res, err = helpers.BillingCreateLease(ctx, tc.chain, tenant, items)
	require.NoError(t, err)
	txRes, err = tc.chain.GetTransaction(res.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), txRes.Code)

	leaseID, err := helpers.GetLeaseIDFromTxHash(ctx, tc.chain, res.TxHash)
	require.NoError(t, err)
	t.Logf("Created lease: %s", leaseID)

	// Query after lease creation
	creditRes, err = helpers.BillingQueryCreditAccount(ctx, tc.chain, tenant.FormattedAddress())
	require.NoError(t, err)

	balanceAfter := creditRes.Balances.AmountOf(tc.pwrDenom)
	availableAfter := creditRes.AvailableBalances.AmountOf(tc.pwrDenom)
	reservedAfter := creditRes.CreditAccount.ReservedAmounts.AmountOf(tc.pwrDenom)

	t.Logf("After lease - balance: %s, available: %s, reserved: %s", balanceAfter, availableAfter, reservedAfter)

	// Verify: balance unchanged, available = balance - reserved
	require.Equal(t, balance, balanceAfter, "balance should be unchanged")
	require.True(t, reservedAfter.GT(sdkmath.ZeroInt()), "should have reservation")
	require.Equal(t, balanceAfter.Sub(reservedAfter), availableAfter,
		"available should equal balance minus reserved")

	// Cancel lease
	res, err = helpers.BillingCancelLease(ctx, tc.chain, tenant, leaseID)
	require.NoError(t, err)
	txRes, err = tc.chain.GetTransaction(res.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), txRes.Code)

	require.NoError(t, testutil.WaitForBlocks(ctx, 2, tc.chain))

	// Query after cancellation - should be back to initial state
	creditRes, err = helpers.BillingQueryCreditAccount(ctx, tc.chain, tenant.FormattedAddress())
	require.NoError(t, err)

	balanceFinal := creditRes.Balances.AmountOf(tc.pwrDenom)
	availableFinal := creditRes.AvailableBalances.AmountOf(tc.pwrDenom)
	reservedFinal := creditRes.CreditAccount.ReservedAmounts.AmountOf(tc.pwrDenom)

	t.Logf("After cancel - balance: %s, available: %s, reserved: %s", balanceFinal, availableFinal, reservedFinal)

	require.Equal(t, balance, balanceFinal, "balance should be unchanged")
	require.Equal(t, balanceFinal, availableFinal, "all balance should be available after cancel")
	require.True(t, reservedFinal.IsZero(), "no reservations after cancel")
}
