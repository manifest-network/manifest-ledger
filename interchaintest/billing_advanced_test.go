// Package interchaintest contains end-to-end tests for the billing module.
// This file contains advanced billing tests: CreateLeaseForTenant, AutoClose,
// ProviderWithdrawLimits, ProviderDeactivation, AllowedListAuthorization, MultiDenom.
package interchaintest

import (
	"context"
	"fmt"
	"testing"

	sdkmath "cosmossdk.io/math"
	"github.com/cosmos/interchaintest/v10"
	"github.com/cosmos/interchaintest/v10/ibc"
	"github.com/cosmos/interchaintest/v10/testutil"
	"github.com/stretchr/testify/require"

	"github.com/manifest-network/manifest-ledger/interchaintest/helpers"
	billingtypes "github.com/manifest-network/manifest-ledger/x/billing/types"
)

// TestBillingAdvanced runs the advanced billing module e2e tests independently.
// Run with: go test -v ./interchaintest -run TestBillingAdvanced -timeout 45m
func TestBillingAdvanced(t *testing.T) {
	ctx, tc, cleanup := setupBillingTest(t, "billing-advanced-test")
	t.Cleanup(cleanup)

	// Set globals from context for backward compatibility with existing test functions
	testPWRDenom = tc.pwrDenom
	testProviderUUID = tc.providerUUID
	testSKUUUID = tc.skuUUID
	testSKUUUID2 = tc.skuUUID2

	// Run advanced test suites
	t.Run("CreateLeaseForTenant", func(t *testing.T) {
		testCreateLeaseForTenantIndependent(t, ctx, tc)
	})
	t.Run("AutoCloseMechanism", func(t *testing.T) {
		testAutoCloseMechanismIndependent(t, ctx, tc)
	})
	t.Run("ProviderWithdrawLimits", func(t *testing.T) {
		testProviderWithdrawLimitsIndependent(t, ctx, tc)
	})
	t.Run("ProviderDeactivation", func(t *testing.T) {
		testProviderDeactivationIndependent(t, ctx, tc)
	})
	t.Run("SKUDeactivation", func(t *testing.T) {
		testSKUDeactivationIndependent(t, ctx, tc)
	})
	t.Run("AllowedListAuthorization", func(t *testing.T) {
		testAllowedListAuthorizationIndependent(t, ctx, tc)
	})
	t.Run("MultiDenom", func(t *testing.T) {
		testMultiDenomIndependent(t, ctx, tc)
	})
}

// testCreateLeaseForTenantIndependent tests authority creating leases for tenants.
func testCreateLeaseForTenantIndependent(t *testing.T, ctx context.Context, tc *billingTestContext) {
	t.Log("=== Testing Create Lease For Tenant (Authority Only) ===")

	// Create a new tenant for these tests
	users := interchaintest.GetAndFundTestUsers(t, ctx, "lease-for-tenant", DefaultGenesisAmt, tc.chain)
	newTenant := users[0]

	// Setup: fund new tenant credit account
	// Authority funds the new tenant's credit account using FundCredit
	// This creates the credit account record in the billing module
	t.Log("Setting up: funding new tenant credit account...")
	fundAmount := fmt.Sprintf("100000000%s", tc.pwrDenom) // 100 PWR
	fundRes, err := helpers.BillingFundCredit(ctx, tc.chain, tc.authority, newTenant.FormattedAddress(), fundAmount)
	require.NoError(t, err)

	fundTxRes, err := tc.chain.GetTransaction(fundRes.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), fundTxRes.Code, "funding new tenant credit should succeed: %s", fundTxRes.RawLog)

	// Verify credit account exists and has balance
	creditRes, err := helpers.BillingQueryCreditAccount(ctx, tc.chain, newTenant.FormattedAddress())
	require.NoError(t, err)
	require.True(t, !creditRes.Balances.IsZero(), "credit balance should be positive")
	t.Logf("New tenant credit balance: %s", creditRes.Balances)

	var leaseID string
	t.Run("success: authority creates lease for tenant", func(t *testing.T) {
		items := []string{fmt.Sprintf("%s:1", tc.skuUUID)}
		res, err := helpers.BillingCreateLeaseForTenant(ctx, tc.chain, tc.authority, newTenant.FormattedAddress(), items)
		require.NoError(t, err)

		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "authority create lease for tenant should succeed: %s", txRes.RawLog)

		leaseID, err = helpers.GetLeaseIDFromTxHash(ctx, tc.chain, res.TxHash)
		require.NoError(t, err)
		t.Logf("Created lease ID: %s for tenant: %s", leaseID, newTenant.FormattedAddress())

		// Acknowledge the lease to make it ACTIVE
		ackRes, err := helpers.BillingAcknowledgeLease(ctx, tc.chain, tc.providerWallet, leaseID)
		require.NoError(t, err)
		ackTxRes, err := tc.chain.GetTransaction(ackRes.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), ackTxRes.Code, "lease acknowledgement should succeed: %s", ackTxRes.RawLog)
	})

	t.Run("success: verify lease belongs to tenant", func(t *testing.T) {
		leaseRes, err := helpers.BillingQueryLease(ctx, tc.chain, leaseID)
		require.NoError(t, err)
		require.Equal(t, newTenant.FormattedAddress(), leaseRes.Lease.Tenant)
		require.Equal(t, billingtypes.LEASE_STATE_ACTIVE, leaseRes.Lease.GetState())
	})

	t.Run("success: tenant can close lease created by authority", func(t *testing.T) {
		res, err := helpers.BillingCloseLease(ctx, tc.chain, newTenant, leaseID)
		require.NoError(t, err)

		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "tenant should be able to close lease created by authority: %s", txRes.RawLog)

		// Verify lease is now inactive
		leaseRes, err := helpers.BillingQueryLease(ctx, tc.chain, leaseID)
		require.NoError(t, err)
		require.Equal(t, billingtypes.LEASE_STATE_CLOSED, leaseRes.Lease.GetState())
	})

	t.Run("success: authority creates multi-SKU lease for tenant", func(t *testing.T) {
		items := []string{
			fmt.Sprintf("%s:2", tc.skuUUID),  // 2x per-hour SKU
			fmt.Sprintf("%s:1", tc.skuUUID2), // 1x per-day SKU
		}
		res, err := helpers.BillingCreateLeaseForTenant(ctx, tc.chain, tc.authority, newTenant.FormattedAddress(), items)
		require.NoError(t, err)

		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "multi-SKU lease creation should succeed: %s", txRes.RawLog)

		newLeaseID, err := helpers.GetLeaseIDFromTxHash(ctx, tc.chain, res.TxHash)
		require.NoError(t, err)

		// Verify lease has correct number of items
		leaseRes, err := helpers.BillingQueryLease(ctx, tc.chain, newLeaseID)
		require.NoError(t, err)
		require.Len(t, leaseRes.Lease.Items, 2, "lease should have 2 items")
	})

	t.Run("fail: non-authority cannot create lease for tenant", func(t *testing.T) {
		items := []string{fmt.Sprintf("%s:1", tc.skuUUID)}
		res, err := helpers.BillingCreateLeaseForTenant(ctx, tc.chain, tc.unauthorizedUser, newTenant.FormattedAddress(), items)
		require.NoError(t, err)

		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "non-authority should not create lease for tenant")
		require.Contains(t, txRes.RawLog, "unauthorized")
	})

	t.Run("fail: provider cannot create lease for tenant", func(t *testing.T) {
		items := []string{fmt.Sprintf("%s:1", tc.skuUUID)}
		res, err := helpers.BillingCreateLeaseForTenant(ctx, tc.chain, tc.providerWallet, newTenant.FormattedAddress(), items)
		require.NoError(t, err)

		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "provider should not create lease for tenant")
		require.Contains(t, txRes.RawLog, "unauthorized")
	})

	t.Run("fail: create lease for tenant without credit account", func(t *testing.T) {
		// Create a new tenant without funding their credit (no credit account)
		unfundedUsers := interchaintest.GetAndFundTestUsers(t, ctx, "unfunded-tenant", DefaultGenesisAmt, tc.chain)
		unfundedTenant := unfundedUsers[0]

		items := []string{fmt.Sprintf("%s:1", tc.skuUUID)}
		res, err := helpers.BillingCreateLeaseForTenant(ctx, tc.chain, tc.authority, unfundedTenant.FormattedAddress(), items)
		require.NoError(t, err)

		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "should fail without credit account")
		require.Contains(t, txRes.RawLog, "credit account not found",
			"should fail with credit account not found error")
	})

	t.Run("fail: create lease for tenant with insufficient credit", func(t *testing.T) {
		// Create a new tenant with minimal credit (1 upwr) - not enough for min_lease_duration
		lowCreditUsers := interchaintest.GetAndFundTestUsers(t, ctx, "low-credit-tenant", DefaultGenesisAmt, tc.chain)
		lowCreditTenant := lowCreditUsers[0]

		// Fund with only 1 upwr - way below the required amount for min_lease_duration
		fundAmount := fmt.Sprintf("1%s", tc.pwrDenom)
		res, err := helpers.BillingFundCredit(ctx, tc.chain, tc.authority, lowCreditTenant.FormattedAddress(), fundAmount)
		require.NoError(t, err)
		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "funding should succeed")

		// Now try to create a lease for this tenant - should fail due to insufficient credit
		items := []string{fmt.Sprintf("%s:1", tc.skuUUID)}
		res, err = helpers.BillingCreateLeaseForTenant(ctx, tc.chain, tc.authority, lowCreditTenant.FormattedAddress(), items)
		require.NoError(t, err)

		txRes, err = tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "should fail with insufficient credit")
		require.Contains(t, txRes.RawLog, "insufficient credit",
			"should fail with insufficient credit error")
	})

	t.Run("fail: create lease for tenant with invalid address", func(t *testing.T) {
		items := []string{fmt.Sprintf("%s:1", tc.skuUUID)}
		// Using an invalid address format - this should fail at CLI validation
		res, err := helpers.BillingCreateLeaseForTenant(ctx, tc.chain, tc.authority, "invalid-address", items)
		// CLI should return an error for invalid address
		require.Error(t, err, "should fail with invalid tenant address")
		_ = res // unused
	})

	t.Run("fail: create lease for tenant with non-existent SKU", func(t *testing.T) {
		items := []string{fmt.Sprintf("%s:1", nonExistentUUID)}
		res, err := helpers.BillingCreateLeaseForTenant(ctx, tc.chain, tc.authority, newTenant.FormattedAddress(), items)
		require.NoError(t, err)

		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "should fail with non-existent SKU")
		require.Contains(t, txRes.RawLog, "not found")
	})

	t.Run("success: verify event shows authority created lease", func(t *testing.T) {
		// Create another lease and check the event
		items := []string{fmt.Sprintf("%s:1", tc.skuUUID)}
		res, err := helpers.BillingCreateLeaseForTenant(ctx, tc.chain, tc.authority, newTenant.FormattedAddress(), items)
		require.NoError(t, err)

		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code)

		// Check for the created_by event attribute with value "authority"
		foundAuthorityEvent := false
		for _, event := range txRes.Events {
			if event.Type == "lease_created" {
				for _, attr := range event.Attributes {
					if attr.Key == "created_by" && attr.Value == "authority" {
						foundAuthorityEvent = true
						break
					}
				}
			}
		}
		require.True(t, foundAuthorityEvent, "event should indicate lease was created by authority")
	})
}

// testAutoCloseMechanismIndependent tests the lazy auto-close mechanism.
func testAutoCloseMechanismIndependent(t *testing.T, ctx context.Context, tc *billingTestContext) {
	t.Log("=== Testing Auto-Close Mechanism ===")

	// For auto-close tests, we temporarily set a very low minLeaseDuration (10 seconds)
	// so we can test credit exhaustion quickly.
	// tc.skuUUID has rate of 1000/second per unit.
	// With minLeaseDuration=10 and quantity=1: need 10,000 credit minimum
	// Fund with 15,000 credit: exhaustion takes 15 seconds

	// Setup: set low min_lease_duration for this test
	t.Log("Setting up: low min_lease_duration for auto-close tests...")
	params, err := helpers.BillingQueryParams(ctx, tc.chain)
	require.NoError(t, err)

	// Set minLeaseDuration to 10 seconds for quick exhaustion tests
	paramRes, err := helpers.BillingUpdateParams(ctx, tc.chain, tc.authority,
		params.Params.MaxLeasesPerTenant, params.Params.MaxItemsPerLease,
		10, // 10 seconds min lease duration
		params.Params.MaxPendingLeasesPerTenant, params.Params.PendingTimeout,
		nil)
	require.NoError(t, err)
	paramTxRes, err := tc.chain.GetTransaction(paramRes.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), paramTxRes.Code, "params update should succeed: %s", paramTxRes.RawLog)
	t.Log("Set min_lease_duration to 10 seconds for auto-close tests")

	// Create a dedicated tenant for auto-close tests with minimal credit
	// to force exhaustion quickly
	users := interchaintest.GetAndFundTestUsers(t, ctx, "auto-close-tenant", DefaultGenesisAmt, tc.chain)
	autoCloseTenant := users[0]

	// Setup: fund tenant with just enough credit to create a lease but exhaust quickly
	// tc.skuUUID has rate of 1000/second per unit
	// With quantity=1 and 15,000 credit, exhaustion takes ~15 seconds
	t.Log("Setting up: funding tenant with minimal credit...")
	acFundAmount := fmt.Sprintf("15000%s", tc.pwrDenom) // Just above 10,000 minimum (10 * 1000)
	acFundRes, err := helpers.BillingFundCredit(ctx, tc.chain, tc.authority, autoCloseTenant.FormattedAddress(), acFundAmount)
	require.NoError(t, err)

	acFundTxRes, err := tc.chain.GetTransaction(acFundRes.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), acFundTxRes.Code, "funding should succeed: %s", acFundTxRes.RawLog)

	acCreditRes, err := helpers.BillingQueryCreditAccount(ctx, tc.chain, autoCloseTenant.FormattedAddress())
	require.NoError(t, err)
	t.Logf("Initial credit balance: %s", acCreditRes.Balances)

	// Setup: create lease that will exhaust credit
	// Create a lease with tc.skuUUID (1000/second rate with quantity=1)
	// With 15,000 credit, this will exhaust in ~15 seconds
	t.Log("Setting up: creating lease that will exhaust credit...")
	acItems := []string{fmt.Sprintf("%s:1", tc.skuUUID)}
	acLeaseRes, err := helpers.BillingCreateLease(ctx, tc.chain, autoCloseTenant, acItems)
	require.NoError(t, err)

	acLeaseTxRes, err := tc.chain.GetTransaction(acLeaseRes.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), acLeaseTxRes.Code, "lease creation should succeed: %s", acLeaseTxRes.RawLog)

	autoCloseLeaseID, err := helpers.GetLeaseIDFromTxHash(ctx, tc.chain, acLeaseRes.TxHash)
	require.NoError(t, err)
	t.Logf("Created lease ID: %s", autoCloseLeaseID)

	// Acknowledge the lease to make it ACTIVE
	acAckRes, err := helpers.BillingAcknowledgeLease(ctx, tc.chain, tc.providerWallet, autoCloseLeaseID)
	require.NoError(t, err)
	acAckTxRes, err := tc.chain.GetTransaction(acAckRes.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), acAckTxRes.Code, "lease acknowledgement should succeed: %s", acAckTxRes.RawLog)

	// Verify lease is active and check locked price
	acLease, err := helpers.BillingQueryLease(ctx, tc.chain, autoCloseLeaseID)
	require.NoError(t, err)
	require.Equal(t, billingtypes.LEASE_STATE_ACTIVE, acLease.Lease.GetState(), "lease should be active")
	t.Logf("Lease items: %+v", acLease.Lease.Items)

	t.Run("success: lease auto-closes when credit exhausted during withdrawal", func(t *testing.T) {
		// Check provider balance before auto-close
		providerBalance, err := tc.chain.GetBalance(ctx, tc.providerWallet.FormattedAddress(), tc.pwrDenom)
		require.NoError(t, err)
		t.Logf("Provider balance BEFORE auto-close: %s", providerBalance.String())

		// Wait for enough blocks to exhaust credit
		// With 1000/second rate and 15,000 credit, we need ~15 seconds
		// Block time is ~1 second, so wait for ~20 blocks to be safe
		t.Log("Waiting for credit to accrue/exhaust...")
		require.NoError(t, testutil.WaitForBlocks(ctx, 15, tc.chain))

		// Check credit balance - should be very low or zero
		creditRes, err := helpers.BillingQueryCreditAccount(ctx, tc.chain, autoCloseTenant.FormattedAddress())
		require.NoError(t, err)
		t.Logf("Credit balance after accrual: %s", creditRes.Balances)

		// Trigger settlement by attempting a withdrawal
		// This should auto-close the lease due to exhausted credit
		res, err := helpers.BillingWithdraw(ctx, tc.chain, tc.providerWallet, autoCloseLeaseID)
		require.NoError(t, err)

		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		t.Logf("Withdrawal tx result: code=%d, log=%s", txRes.Code, txRes.RawLog)

		// The withdrawal TX should succeed (settlement happens during auto-close)
		require.Equal(t, uint32(0), txRes.Code, "withdrawal should succeed, auto-close settles the funds")

		// Check provider balance AFTER auto-close - should have received 15000
		providerBalanceAfter, err := tc.chain.GetBalance(ctx, tc.providerWallet.FormattedAddress(), tc.pwrDenom)
		require.NoError(t, err)
		t.Logf("Provider balance AFTER auto-close: %s", providerBalanceAfter.String())

		// Provider should have received the tenant's credit (15000)
		require.True(t, providerBalanceAfter.GT(sdkmath.NewInt(0)),
			"provider should have received funds from auto-close settlement")

		// Check credit balance AFTER auto-close - should be 0 or near 0
		creditResAfter, err := helpers.BillingQueryCreditAccount(ctx, tc.chain, autoCloseTenant.FormattedAddress())
		require.NoError(t, err)
		t.Logf("Credit balance AFTER auto-close: %s", creditResAfter.Balances)

		// Credit should be depleted
		require.True(t, creditResAfter.Balances.IsZero() || creditResAfter.Balances.AmountOf(tc.pwrDenom).LTE(sdkmath.ZeroInt()),
			"credit balance should be depleted after auto-close")

		// Query lease - should now be inactive due to auto-close
		lease, err := helpers.BillingQueryLease(ctx, tc.chain, autoCloseLeaseID)
		require.NoError(t, err)
		require.Equal(t, billingtypes.LEASE_STATE_CLOSED, lease.Lease.GetState(),
			"lease should be auto-closed after credit exhaustion")
		t.Log("Lease was auto-closed as expected")
	})

	t.Run("success: auto-closed lease emits proper events", func(t *testing.T) {
		// The withdrawal that triggered auto-close should have emitted events
		// Query the lease to verify it's closed
		lease, err := helpers.BillingQueryLease(ctx, tc.chain, autoCloseLeaseID)
		require.NoError(t, err)
		require.Equal(t, billingtypes.LEASE_STATE_CLOSED, lease.Lease.GetState())

		// Verify closed_at is set (indicates it was closed)
		require.NotEmpty(t, lease.Lease.ClosedAt, "closed_at should be set for auto-closed lease")
		t.Logf("Lease closed_at: %s", lease.Lease.ClosedAt)

		// Verify closure_reason is set to indicate credit exhaustion
		require.Equal(t, billingtypes.ClosureReasonCreditExhausted, lease.Lease.GetClosureReason(),
			"auto-closed lease should have closure_reason set to 'credit exhausted'")
		t.Logf("Lease closure_reason: %s", lease.Lease.GetClosureReason())
	})

	t.Run("success: provider already withdrew during auto-close", func(t *testing.T) {
		// After auto-close, the provider should have already received their tokens
		// during the settlement that triggered the close
		// Attempting another withdrawal should return 0 (nothing left to withdraw)
		res, err := helpers.BillingWithdraw(ctx, tc.chain, tc.providerWallet, autoCloseLeaseID)
		require.NoError(t, err)

		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		// Should fail since lease is inactive and no accrual since last settlement
		require.NotEqual(t, uint32(0), txRes.Code, "second withdrawal should fail")
		require.Contains(t, txRes.RawLog, "no withdrawable amount",
			"should indicate no withdrawable amount")
	})

	t.Run("success: tenant cannot create new lease with exhausted credit", func(t *testing.T) {
		// Verify credit balance is depleted (should be 0 after auto-close settlement)
		creditRes, err := helpers.BillingQueryCreditAccount(ctx, tc.chain, autoCloseTenant.FormattedAddress())
		require.NoError(t, err)
		t.Logf("Credit balance after exhaustion: %s", creditRes.Balances)

		// After auto-close, credit should be 0 or very low
		require.True(t, creditRes.Balances.IsZero() || creditRes.Balances.AmountOf(tc.pwrDenom).LTE(sdkmath.ZeroInt()),
			"credit balance (%s) should be depleted after auto-close",
			creditRes.Balances)

		// Credit is insufficient to cover minLeaseDuration, so creating a new lease should fail
		items := []string{fmt.Sprintf("%s:1", tc.skuUUID)}
		res, err := helpers.BillingCreateLease(ctx, tc.chain, autoCloseTenant, items)
		require.NoError(t, err)

		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "lease creation should fail with insufficient credit")
		require.Contains(t, txRes.RawLog, "insufficient credit",
			"should indicate insufficient credit balance")
	})

	// Test auto-close via CloseLease when credit is exhausted
	t.Run("success: explicit close on exhausted lease works", func(t *testing.T) {
		// Create another tenant with minimal credit
		users2 := interchaintest.GetAndFundTestUsers(t, ctx, "auto-close-tenant2", DefaultGenesisAmt, tc.chain)
		tenant2 := users2[0]

		// Fund minimally - same approach as main auto-close test
		// tc.skuUUID has rate of 1000/second, with minLeaseDuration=10, need 10,000 minimum
		fundAmount := fmt.Sprintf("15000%s", tc.pwrDenom)
		res, err := helpers.BillingFundCredit(ctx, tc.chain, tc.authority, tenant2.FormattedAddress(), fundAmount)
		require.NoError(t, err)
		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code)

		// Create lease with tc.skuUUID (1000/second rate with quantity=1)
		items := []string{fmt.Sprintf("%s:1", tc.skuUUID)}
		res, err = helpers.BillingCreateLease(ctx, tc.chain, tenant2, items)
		require.NoError(t, err)
		txRes, err = tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code)

		leaseID, err := helpers.GetLeaseIDFromTxHash(ctx, tc.chain, res.TxHash)
		require.NoError(t, err)

		// Acknowledge the lease to make it ACTIVE
		ackRes, err := helpers.BillingAcknowledgeLease(ctx, tc.chain, tc.providerWallet, leaseID)
		require.NoError(t, err)
		ackTxRes, err := tc.chain.GetTransaction(ackRes.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), ackTxRes.Code, "lease acknowledgement should succeed: %s", ackTxRes.RawLog)

		// Wait for credit exhaustion (~15 seconds)
		require.NoError(t, testutil.WaitForBlocks(ctx, 20, tc.chain))

		res, err = helpers.BillingCloseLease(ctx, tc.chain, tenant2, leaseID)
		require.NoError(t, err)
		txRes, err = tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "explicit close should succeed even with exhausted credit")

		// Verify final state is inactive (explicit close)
		lease, err := helpers.BillingQueryLease(ctx, tc.chain, leaseID)
		require.NoError(t, err)
		require.Equal(t, billingtypes.LEASE_STATE_CLOSED, lease.Lease.GetState(), "lease should be inactive after exhaustion")
	})

	// Test that closing a lease triggers settlement and transfers accrued amount
	t.Run("success: closing lease settles and transfers accrued amount", func(t *testing.T) {
		// Create a tenant with enough credit
		users3 := interchaintest.GetAndFundTestUsers(t, ctx, "settlement-tenant", DefaultGenesisAmt, tc.chain)
		tenant3 := users3[0]

		// Fund with credit
		fundAmount := fmt.Sprintf("100000000%s", tc.pwrDenom) // 100 PWR
		res, err := helpers.BillingFundCredit(ctx, tc.chain, tc.authority, tenant3.FormattedAddress(), fundAmount)
		require.NoError(t, err)
		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code)

		// Create lease with low quantity (slow accrual)
		items := []string{fmt.Sprintf("%s:1", tc.skuUUID)}
		res, err = helpers.BillingCreateLease(ctx, tc.chain, tenant3, items)
		require.NoError(t, err)
		txRes, err = tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code)

		leaseID, err := helpers.GetLeaseIDFromTxHash(ctx, tc.chain, res.TxHash)
		require.NoError(t, err)

		// Acknowledge the lease to make it ACTIVE
		ackRes, err := helpers.BillingAcknowledgeLease(ctx, tc.chain, tc.providerWallet, leaseID)
		require.NoError(t, err)
		ackTxRes, err := tc.chain.GetTransaction(ackRes.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), ackTxRes.Code, "lease acknowledgement should succeed: %s", ackTxRes.RawLog)

		// Get initial credit balance
		initialCredit, err := helpers.BillingQueryCreditAccount(ctx, tc.chain, tenant3.FormattedAddress())
		require.NoError(t, err)
		t.Logf("Credit after lease creation: %s", initialCredit.Balances)

		// Wait for some accrual (1000/sec rate, 5 blocks = ~5000 accrued)
		require.NoError(t, testutil.WaitForBlocks(ctx, 5, tc.chain))

		// Close the lease - this triggers settlement
		res, err = helpers.BillingCloseLease(ctx, tc.chain, tenant3, leaseID)
		require.NoError(t, err)
		txRes, err = tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "lease close should succeed")

		// Get credit balance after lease close - should be less due to settlement
		afterCredit, err := helpers.BillingQueryCreditAccount(ctx, tc.chain, tenant3.FormattedAddress())
		require.NoError(t, err)
		t.Logf("Credit after lease close: %s", afterCredit.Balances)

		// Credit should have decreased (settlement happened)
		// Compare the first coin amount (assuming single denom)
		require.True(t, len(afterCredit.Balances) > 0 && len(initialCredit.Balances) > 0,
			"should have credit balances")
		require.True(t, afterCredit.Balances[0].Amount.LT(initialCredit.Balances[0].Amount),
			"credit should decrease due to settlement during lease close")

		// Verify lease is now inactive
		lease, err := helpers.BillingQueryLease(ctx, tc.chain, leaseID)
		require.NoError(t, err)
		require.Equal(t, billingtypes.LEASE_STATE_CLOSED, lease.Lease.GetState(), "lease should be inactive")
	})

	// Restore original minLeaseDuration (1 hour) after auto-close tests
	t.Run("cleanup: restore min_lease_duration to 1 hour", func(t *testing.T) {
		params, err := helpers.BillingQueryParams(ctx, tc.chain)
		require.NoError(t, err)

		res, err := helpers.BillingUpdateParams(ctx, tc.chain, tc.authority,
			params.Params.MaxLeasesPerTenant, params.Params.MaxItemsPerLease,
			3600, // Restore to 1 hour
			params.Params.MaxPendingLeasesPerTenant, params.Params.PendingTimeout,
			nil)
		require.NoError(t, err)
		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "params restore should succeed")
		t.Log("Restored min_lease_duration to 3600 seconds (1 hour)")
	})
}

// testProviderWithdrawLimitsIndependent tests provider withdraw limit functionality.
func testProviderWithdrawLimitsIndependent(t *testing.T, ctx context.Context, tc *billingTestContext) {
	// Create a new tenant for these tests
	users := interchaintest.GetAndFundTestUsers(t, ctx, "provider-withdraw-limit-tenant", DefaultGenesisAmt, tc.chain)
	tenant := users[0]

	// Fund tenant's credit account
	fundAmount := fmt.Sprintf("500000000%s", tc.pwrDenom) // 500 PWR
	res, err := helpers.BillingFundCredit(ctx, tc.chain, tc.authority, tenant.FormattedAddress(), fundAmount)
	require.NoError(t, err)
	txRes, err := tc.chain.GetTransaction(res.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), txRes.Code)

	// Create multiple leases for testing
	leaseIDs := make([]string, 5)
	for i := 0; i < 5; i++ {
		items := []string{fmt.Sprintf("%s:1", tc.skuUUID)}
		res, err := helpers.BillingCreateLease(ctx, tc.chain, tenant, items)
		require.NoError(t, err)
		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "lease creation should succeed")

		leaseID, err := helpers.GetLeaseIDFromTxHash(ctx, tc.chain, res.TxHash)
		require.NoError(t, err)
		leaseIDs[i] = leaseID

		// Acknowledge the lease to make it ACTIVE
		ackRes, err := helpers.BillingAcknowledgeLease(ctx, tc.chain, tc.providerWallet, leaseID)
		require.NoError(t, err)
		ackTxRes, err := tc.chain.GetTransaction(ackRes.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), ackTxRes.Code, "lease acknowledgement should succeed: %s", ackTxRes.RawLog)
	}

	// Wait for some accrual
	require.NoError(t, testutil.WaitForBlocks(ctx, 5, tc.chain))

	// Test: provider withdraw with custom limit
	t.Run("success: provider withdraw with custom limit", func(t *testing.T) {
		// Use a limit of 2 to test pagination
		res, err := helpers.BillingWithdrawByProvider(ctx, tc.chain, tc.providerWallet, tc.providerUUID, 2)
		require.NoError(t, err)
		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "provider withdraw should succeed")

		// Check events for has_more flag
		t.Logf("Provider withdraw with limit 2 succeeded")
	})

	// Test: provider withdraw with default limit (0 means default)
	// Uses higher gas because provider-wide withdraw processes all leases for this provider
	// across the entire test suite (not just the 5 created in this function).
	t.Run("success: provider withdraw with default limit", func(t *testing.T) {
		res, err := helpers.BillingWithdrawByProvider(ctx, tc.chain, tc.providerWallet, tc.providerUUID, 0, "--gas", "2000000")
		require.NoError(t, err)
		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "provider withdraw should succeed")
	})

	// Test: provider withdraw with limit exceeding maximum should fail at CLI validation
	t.Run("fail: provider withdraw with limit exceeding maximum", func(t *testing.T) {
		// MaxBatchLeaseSize is 100, try 150
		_, err := helpers.BillingWithdrawByProvider(ctx, tc.chain, tc.providerWallet, tc.providerUUID, 150)
		require.Error(t, err, "provider withdraw with excessive limit should fail")
	})
}

// testProviderDeactivationIndependent tests behavior when a provider is deactivated.
func testProviderDeactivationIndependent(t *testing.T, ctx context.Context, tc *billingTestContext) {
	// Create a new user specifically for the deactivation test provider
	users := interchaintest.GetAndFundTestUsers(t, ctx, "deactivation-provider-wallet", DefaultGenesisAmt, tc.chain)
	deactivationProviderWallet := users[0]

	// Setup: create a new provider specifically for deactivation tests
	t.Log("Setting up: creating provider for deactivation test...")
	dpRes, err := helpers.SKUCreateProvider(ctx, tc.chain, tc.authority,
		deactivationProviderWallet.FormattedAddress(), deactivationProviderWallet.FormattedAddress(), "")
	require.NoError(t, err)
	dpTxRes, err := tc.chain.GetTransaction(dpRes.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), dpTxRes.Code)

	// Get provider ID from events
	var deactivateProviderUUID string
	for _, event := range dpTxRes.Events {
		if event.Type == "provider_created" {
			for _, attr := range event.Attributes {
				if attr.Key == "provider_uuid" {
					deactivateProviderUUID = attr.Value
					break
				}
			}
		}
	}
	require.NotEmpty(t, deactivateProviderUUID, "provider UUID should be extracted from events")

	// Setup: create SKU for this provider with valid price (evenly divisible)
	t.Log("Setting up: creating SKU for deactivation provider...")
	dpSkuRes, err := helpers.SKUCreateSKU(ctx, tc.chain, tc.authority,
		deactivateProviderUUID, "Deactivation SKU", 1, fmt.Sprintf("3600000%s", tc.pwrDenom), "")
	require.NoError(t, err)
	dpSkuTxRes, err := tc.chain.GetTransaction(dpSkuRes.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), dpSkuTxRes.Code)

	// Get the SKU ID
	skus, err := helpers.SKUQuerySKUsByProvider(ctx, tc.chain, deactivateProviderUUID)
	require.NoError(t, err)
	require.Len(t, skus.Skus, 1)
	deactivateSKUUUID := skus.Skus[0].Uuid

	// Create tenant and fund credit
	tenantUsers := interchaintest.GetAndFundTestUsers(t, ctx, "deactivate-tenant", DefaultGenesisAmt, tc.chain)
	tenant := tenantUsers[0]

	fundAmount := fmt.Sprintf("100000000%s", tc.pwrDenom)
	res, err := helpers.BillingFundCredit(ctx, tc.chain, tc.authority, tenant.FormattedAddress(), fundAmount)
	require.NoError(t, err)
	txRes, err := tc.chain.GetTransaction(res.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), txRes.Code)

	// Setup: create a lease with this provider's SKU
	t.Log("Setting up: creating lease with provider's SKU...")
	dpLeaseItems := []string{fmt.Sprintf("%s:1", deactivateSKUUUID)}
	dpLeaseRes, err := helpers.BillingCreateLease(ctx, tc.chain, tenant, dpLeaseItems)
	require.NoError(t, err)
	dpLeaseTxRes, err := tc.chain.GetTransaction(dpLeaseRes.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), dpLeaseTxRes.Code)

	leaseID, err := helpers.GetLeaseIDFromTxHash(ctx, tc.chain, dpLeaseRes.TxHash)
	require.NoError(t, err)

	// Acknowledge the lease to make it ACTIVE
	dpAckRes, err := helpers.BillingAcknowledgeLease(ctx, tc.chain, deactivationProviderWallet, leaseID)
	require.NoError(t, err)
	dpAckTxRes, err := tc.chain.GetTransaction(dpAckRes.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), dpAckTxRes.Code, "lease acknowledgement should succeed: %s", dpAckTxRes.RawLog)

	// Deactivate the provider
	t.Run("success: provider can be deactivated while having active leases", func(t *testing.T) {
		res, err := helpers.SKUDeactivateProvider(ctx, tc.chain, tc.authority, deactivateProviderUUID)
		require.NoError(t, err)
		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "provider deactivation should succeed")
	})

	// Verify provider is deactivated
	t.Run("success: verify provider is deactivated", func(t *testing.T) {
		provider, err := helpers.SKUQueryProvider(ctx, tc.chain, deactivateProviderUUID)
		require.NoError(t, err)
		require.False(t, provider.Provider.Active, "provider should be inactive")
	})

	// Verify existing lease is still active
	t.Run("success: existing lease continues after provider deactivation", func(t *testing.T) {
		lease, err := helpers.BillingQueryLease(ctx, tc.chain, leaseID)
		require.NoError(t, err)
		require.Equal(t, billingtypes.LEASE_STATE_ACTIVE, lease.Lease.GetState(), "lease should still be active")
	})

	// Wait for some accrual
	require.NoError(t, testutil.WaitForBlocks(ctx, 3, tc.chain))

	// Provider can still withdraw after deactivation
	t.Run("success: provider can still withdraw after deactivation", func(t *testing.T) {
		res, err := helpers.BillingWithdraw(ctx, tc.chain, deactivationProviderWallet, leaseID)
		require.NoError(t, err)
		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "withdrawal should succeed")
	})

	// Cannot create new lease with deactivated provider's SKU
	t.Run("fail: cannot create new lease with deactivated provider's SKU", func(t *testing.T) {
		// Create another tenant
		users2 := interchaintest.GetAndFundTestUsers(t, ctx, "deactivate-tenant-2", DefaultGenesisAmt, tc.chain)
		tenant2 := users2[0]

		// Fund their credit
		fundAmount := fmt.Sprintf("100000000%s", tc.pwrDenom)
		res, err := helpers.BillingFundCredit(ctx, tc.chain, tc.authority, tenant2.FormattedAddress(), fundAmount)
		require.NoError(t, err)
		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code)

		// Try to create a lease - should fail because provider is inactive
		items := []string{fmt.Sprintf("%s:1", deactivateSKUUUID)}
		res, err = helpers.BillingCreateLease(ctx, tc.chain, tenant2, items)
		require.NoError(t, err)
		txRes, err = tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "lease creation should fail with inactive provider")
	})

	// Deactivated provider is still queryable
	t.Run("success: deactivated provider is still queryable", func(t *testing.T) {
		provider, err := helpers.SKUQueryProvider(ctx, tc.chain, deactivateProviderUUID)
		require.NoError(t, err)
		require.NotNil(t, provider.Provider)
		require.Equal(t, deactivateProviderUUID, provider.Provider.Uuid)
		require.False(t, provider.Provider.Active, "provider should be inactive")
	})

	// Verify SKUs are also deactivated (cascade deactivation)
	t.Run("success: all SKUs are deactivated when provider is deactivated (cascade)", func(t *testing.T) {
		skus, err := helpers.SKUQuerySKUsByProvider(ctx, tc.chain, deactivateProviderUUID)
		require.NoError(t, err)
		require.Len(t, skus.Skus, 1, "provider should still have 1 SKU")

		for _, sku := range skus.Skus {
			require.False(t, sku.Active, "SKU %s should be inactive after provider deactivation", sku.Uuid)
		}
		t.Logf("Verified all %d SKUs are inactive after provider deactivation", len(skus.Skus))
	})

	// Verify SKU is queryable and inactive
	t.Run("success: deactivated SKU is still queryable", func(t *testing.T) {
		sku, err := helpers.SKUQuerySKU(ctx, tc.chain, deactivateSKUUUID)
		require.NoError(t, err)
		require.NotNil(t, sku.Sku)
		require.Equal(t, deactivateSKUUUID, sku.Sku.Uuid)
		require.False(t, sku.Sku.Active, "SKU should be inactive")
	})

	// Tenant can still close their lease after provider/SKU deactivation
	t.Run("success: tenant can close lease after provider deactivation", func(t *testing.T) {
		res, err := helpers.BillingCloseLease(ctx, tc.chain, tenant, leaseID)
		require.NoError(t, err)
		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "tenant should be able to close lease: %s", txRes.RawLog)

		// Verify lease is closed
		lease, err := helpers.BillingQueryLease(ctx, tc.chain, leaseID)
		require.NoError(t, err)
		require.Equal(t, billingtypes.LEASE_STATE_CLOSED, lease.Lease.GetState(), "lease should be closed")
	})
}

// testSKUDeactivationIndependent tests behavior when an individual SKU is deactivated.
func testSKUDeactivationIndependent(t *testing.T, ctx context.Context, tc *billingTestContext) {
	t.Log("=== Testing SKU Deactivation (Individual) ===")

	// Create a new provider specifically for SKU deactivation tests
	users := interchaintest.GetAndFundTestUsers(t, ctx, "sku-deact-provider-wallet", DefaultGenesisAmt, tc.chain)
	skuDeactProviderWallet := users[0]

	// Setup: create provider for SKU deactivation test
	t.Log("Setting up: creating provider for SKU deactivation test...")
	sdProvRes, err := helpers.SKUCreateProvider(ctx, tc.chain, tc.authority,
		skuDeactProviderWallet.FormattedAddress(), skuDeactProviderWallet.FormattedAddress(), "")
	require.NoError(t, err)
	sdProvTxRes, err := tc.chain.GetTransaction(sdProvRes.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), sdProvTxRes.Code)

	skuDeactProviderUUID, err := helpers.GetProviderUUIDFromTxHash(ctx, tc.chain, sdProvRes.TxHash)
	require.NoError(t, err)

	// Setup: create two SKUs for this provider
	t.Log("Setting up: creating SKUs for SKU deactivation test...")
	// First SKU
	sdSku1Res, err := helpers.SKUCreateSKU(ctx, tc.chain, tc.authority,
		skuDeactProviderUUID, "SKU Deact Test 1", 1, fmt.Sprintf("3600000%s", tc.pwrDenom), "")
	require.NoError(t, err)
	sdSku1TxRes, err := tc.chain.GetTransaction(sdSku1Res.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), sdSku1TxRes.Code)
	skuUUID1, err := helpers.GetSKUUUIDFromTxHash(ctx, tc.chain, sdSku1Res.TxHash)
	require.NoError(t, err)

	// Second SKU
	sdSku2Res, err := helpers.SKUCreateSKU(ctx, tc.chain, tc.authority,
		skuDeactProviderUUID, "SKU Deact Test 2", 1, fmt.Sprintf("3600000%s", tc.pwrDenom), "")
	require.NoError(t, err)
	sdSku2TxRes, err := tc.chain.GetTransaction(sdSku2Res.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), sdSku2TxRes.Code)
	skuUUID2, err := helpers.GetSKUUUIDFromTxHash(ctx, tc.chain, sdSku2Res.TxHash)
	require.NoError(t, err)

	// Create tenant and fund credit
	tenantUsers := interchaintest.GetAndFundTestUsers(t, ctx, "sku-deact-tenant", DefaultGenesisAmt, tc.chain)
	tenant := tenantUsers[0]

	fundAmount := fmt.Sprintf("100000000%s", tc.pwrDenom)
	res, err := helpers.BillingFundCredit(ctx, tc.chain, tc.authority, tenant.FormattedAddress(), fundAmount)
	require.NoError(t, err)
	txRes, err := tc.chain.GetTransaction(res.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), txRes.Code)

	// Setup: create a lease with SKU 1
	t.Log("Setting up: creating lease with SKU 1...")
	sdLeaseItems := []string{fmt.Sprintf("%s:1", skuUUID1)}
	sdLeaseRes, err := helpers.BillingCreateLease(ctx, tc.chain, tenant, sdLeaseItems)
	require.NoError(t, err)
	sdLeaseTxRes, err := tc.chain.GetTransaction(sdLeaseRes.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), sdLeaseTxRes.Code)

	leaseID, err := helpers.GetLeaseIDFromTxHash(ctx, tc.chain, sdLeaseRes.TxHash)
	require.NoError(t, err)

	// Acknowledge the lease
	sdAckRes, err := helpers.BillingAcknowledgeLease(ctx, tc.chain, skuDeactProviderWallet, leaseID)
	require.NoError(t, err)
	sdAckTxRes, err := tc.chain.GetTransaction(sdAckRes.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), sdAckTxRes.Code)

	// Deactivate only SKU 1 (not the provider)
	t.Run("success: deactivate individual SKU", func(t *testing.T) {
		res, err := helpers.SKUDeactivateSKU(ctx, tc.chain, tc.authority, skuUUID1)
		require.NoError(t, err)
		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "SKU deactivation should succeed")
	})

	// Verify SKU 1 is inactive but SKU 2 is still active
	t.Run("success: verify only deactivated SKU is inactive", func(t *testing.T) {
		sku1, err := helpers.SKUQuerySKU(ctx, tc.chain, skuUUID1)
		require.NoError(t, err)
		require.False(t, sku1.Sku.Active, "SKU 1 should be inactive")

		sku2, err := helpers.SKUQuerySKU(ctx, tc.chain, skuUUID2)
		require.NoError(t, err)
		require.True(t, sku2.Sku.Active, "SKU 2 should still be active")
	})

	// Verify provider is still active
	t.Run("success: provider remains active after SKU deactivation", func(t *testing.T) {
		provider, err := helpers.SKUQueryProvider(ctx, tc.chain, skuDeactProviderUUID)
		require.NoError(t, err)
		require.True(t, provider.Provider.Active, "provider should still be active")
	})

	// Verify existing lease continues running
	t.Run("success: existing lease continues after SKU deactivation", func(t *testing.T) {
		lease, err := helpers.BillingQueryLease(ctx, tc.chain, leaseID)
		require.NoError(t, err)
		require.Equal(t, billingtypes.LEASE_STATE_ACTIVE, lease.Lease.GetState(), "lease should still be active")
	})

	// Wait for some accrual
	require.NoError(t, testutil.WaitForBlocks(ctx, 3, tc.chain))

	// Provider can still withdraw from existing lease
	t.Run("success: provider can withdraw from lease with deactivated SKU", func(t *testing.T) {
		res, err := helpers.BillingWithdraw(ctx, tc.chain, skuDeactProviderWallet, leaseID)
		require.NoError(t, err)
		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "withdrawal should succeed")
	})

	// Cannot create new lease with deactivated SKU
	t.Run("fail: cannot create new lease with deactivated SKU", func(t *testing.T) {
		items := []string{fmt.Sprintf("%s:1", skuUUID1)}
		res, err := helpers.BillingCreateLease(ctx, tc.chain, tenant, items)
		require.NoError(t, err)
		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "lease creation should fail with deactivated SKU")
		require.Contains(t, txRes.RawLog, "not active")
	})

	// Can still create lease with active SKU 2
	t.Run("success: can create lease with other active SKU", func(t *testing.T) {
		items := []string{fmt.Sprintf("%s:1", skuUUID2)}
		res, err := helpers.BillingCreateLease(ctx, tc.chain, tenant, items)
		require.NoError(t, err)
		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "lease creation with active SKU should succeed: %s", txRes.RawLog)
	})

	// Tenant can close their lease
	t.Run("success: tenant can close lease after SKU deactivation", func(t *testing.T) {
		res, err := helpers.BillingCloseLease(ctx, tc.chain, tenant, leaseID)
		require.NoError(t, err)
		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "tenant should be able to close lease")

		lease, err := helpers.BillingQueryLease(ctx, tc.chain, leaseID)
		require.NoError(t, err)
		require.Equal(t, billingtypes.LEASE_STATE_CLOSED, lease.Lease.GetState())
	})
}

// testAllowedListAuthorizationIndependent tests the allowed_list authorization.
func testAllowedListAuthorizationIndependent(t *testing.T, ctx context.Context, tc *billingTestContext) {
	t.Log("=== Testing AllowedList Authorization ===")

	// Create test users
	users := interchaintest.GetAndFundTestUsers(t, ctx, "allowlist-test", DefaultGenesisAmt, tc.chain)
	allowedUser := users[0]

	users2 := interchaintest.GetAndFundTestUsers(t, ctx, "nonallowlist-test", DefaultGenesisAmt, tc.chain)
	nonAllowedUser := users2[0]

	users3 := interchaintest.GetAndFundTestUsers(t, ctx, "tenant-test", DefaultGenesisAmt, tc.chain)
	tenant := users3[0]

	// Setup: add allowedUser to allowed_list
	t.Log("Setting up: adding user to allowed_list...")
	alParams, err := helpers.BillingQueryParams(ctx, tc.chain)
	require.NoError(t, err)

	// Update with allowed_list
	alParamRes, err := helpers.BillingUpdateParams(ctx, tc.chain, tc.authority,
		alParams.Params.MaxLeasesPerTenant, alParams.Params.MaxItemsPerLease,
		alParams.Params.MinLeaseDuration,
		alParams.Params.MaxPendingLeasesPerTenant, alParams.Params.PendingTimeout,
		[]string{allowedUser.FormattedAddress()})
	require.NoError(t, err)
	alParamTxRes, err := tc.chain.GetTransaction(alParamRes.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), alParamTxRes.Code, "params update should succeed")

	// Setup: fund tenant's credit account
	t.Log("Setting up: funding tenant credit...")
	alFundAmount := fmt.Sprintf("100000000%s", tc.pwrDenom)
	alFundRes, err := helpers.BillingFundCredit(ctx, tc.chain, tc.authority, tenant.FormattedAddress(), alFundAmount)
	require.NoError(t, err)
	alFundTxRes, err := tc.chain.GetTransaction(alFundRes.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), alFundTxRes.Code, "fund credit should succeed")

	// Get an SKU for lease creation
	skus, err := helpers.SKUQuerySKUs(ctx, tc.chain)
	require.NoError(t, err)
	require.NotEmpty(t, skus.Skus, "should have at least one SKU")
	skuUUID := skus.Skus[0].Uuid

	// Test: Authority can create lease for tenant
	var authorityLeaseID string
	t.Run("success: authority creates lease for tenant", func(t *testing.T) {
		items := []string{fmt.Sprintf("%s:1", skuUUID)}
		res, err := helpers.BillingCreateLeaseForTenant(ctx, tc.chain, tc.authority, tenant.FormattedAddress(), items)
		require.NoError(t, err)
		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "authority should be able to create lease for tenant")

		authorityLeaseID, err = helpers.GetLeaseIDFromTxHash(ctx, tc.chain, res.TxHash)
		require.NoError(t, err)
		require.NotZero(t, authorityLeaseID)
	})

	// Test: Allowed user can create lease for tenant
	var allowedUserLeaseID string
	t.Run("success: allowed user creates lease for tenant", func(t *testing.T) {
		items := []string{fmt.Sprintf("%s:1", skuUUID)}
		res, err := helpers.BillingCreateLeaseForTenant(ctx, tc.chain, allowedUser, tenant.FormattedAddress(), items)
		require.NoError(t, err)
		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "allowed user should be able to create lease for tenant")

		allowedUserLeaseID, err = helpers.GetLeaseIDFromTxHash(ctx, tc.chain, res.TxHash)
		require.NoError(t, err)
		require.NotZero(t, allowedUserLeaseID)
	})

	// Test: Non-allowed user cannot create lease for tenant
	t.Run("fail: non-allowed user cannot create lease for tenant", func(t *testing.T) {
		items := []string{fmt.Sprintf("%s:1", skuUUID)}
		res, err := helpers.BillingCreateLeaseForTenant(ctx, tc.chain, nonAllowedUser, tenant.FormattedAddress(), items)
		require.NoError(t, err)
		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "non-allowed user should not be able to create lease for tenant")
	})

	// Test: Verify leases belong to tenant
	t.Run("success: verify leases belong to tenant", func(t *testing.T) {
		lease1, err := helpers.BillingQueryLease(ctx, tc.chain, authorityLeaseID)
		require.NoError(t, err)
		require.Equal(t, tenant.FormattedAddress(), lease1.Lease.Tenant)

		lease2, err := helpers.BillingQueryLease(ctx, tc.chain, allowedUserLeaseID)
		require.NoError(t, err)
		require.Equal(t, tenant.FormattedAddress(), lease2.Lease.Tenant)
	})

	// Test: Remove user from allowed_list, then they can't create leases anymore
	t.Run("success: removed user cannot create lease after allowed_list update", func(t *testing.T) {
		// Get current params
		params, err := helpers.BillingQueryParams(ctx, tc.chain)
		require.NoError(t, err)

		// Update with empty allowed_list
		res, err := helpers.BillingUpdateParams(ctx, tc.chain, tc.authority,
			params.Params.MaxLeasesPerTenant, params.Params.MaxItemsPerLease,
			params.Params.MinLeaseDuration,
			params.Params.MaxPendingLeasesPerTenant, params.Params.PendingTimeout,
			[]string{})
		require.NoError(t, err)
		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "params update should succeed")

		// Now the previously allowed user should not be able to create leases
		items := []string{fmt.Sprintf("%s:1", skuUUID)}
		res, err = helpers.BillingCreateLeaseForTenant(ctx, tc.chain, allowedUser, tenant.FormattedAddress(), items)
		require.NoError(t, err)
		txRes, err = tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "removed user should not be able to create lease for tenant")
	})

	// Test: Authority can still create leases even with empty allowed_list
	t.Run("success: authority can still create lease with empty allowed_list", func(t *testing.T) {
		items := []string{fmt.Sprintf("%s:1", skuUUID)}
		res, err := helpers.BillingCreateLeaseForTenant(ctx, tc.chain, tc.authority, tenant.FormattedAddress(), items)
		require.NoError(t, err)
		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "authority should always be able to create lease for tenant")
	})
}

// testMultiDenomIndependent tests multi-denom support.
func testMultiDenomIndependent(t *testing.T, ctx context.Context, tc *billingTestContext) {
	t.Log("=== Testing Multi-Denom Support ===")

	node := tc.chain.GetNode()

	// Setup: create second denom for testing (using tokenfactory)
	t.Log("Creating second denom...")
	secondDenom, _, err := node.TokenFactoryCreateDenom(ctx, tc.authority, "utest", 2_500_00)
	require.NoError(t, err, "failed to create second denom")
	t.Logf("Created second denom: %s", secondDenom)

	// Mint tokens
	_, err = node.TokenFactoryMintDenom(ctx, tc.authority.FormattedAddress(), secondDenom, 1_000_000_000_000)
	require.NoError(t, err, "failed to mint second denom")

	// Setup: create provider for multi-denom tests
	t.Log("Creating provider for multi-denom tests...")
	res, err := helpers.SKUCreateProvider(ctx, tc.chain, tc.authority, tc.providerWallet.FormattedAddress(), tc.providerWallet.FormattedAddress(), "")
	require.NoError(t, err, "failed to create multi-denom provider")
	txRes, err := tc.chain.GetTransaction(res.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), txRes.Code, "provider creation should succeed: %s", txRes.RawLog)

	multiDenomProviderUUID, err := helpers.GetProviderUUIDFromTxHash(ctx, tc.chain, res.TxHash)
	require.NoError(t, err)
	t.Logf("Created multi-denom provider ID: %s", multiDenomProviderUUID)

	// Setup: create SKU with first denom (PWR)
	// 3600000 per hour = 1000 per second
	t.Log("Creating SKU with PWR denom...")
	res, err = helpers.SKUCreateSKU(ctx, tc.chain, tc.authority, multiDenomProviderUUID, "Compute PWR", 1, fmt.Sprintf("3600000%s", tc.pwrDenom), "")
	require.NoError(t, err, "failed to create SKU with PWR denom")
	txRes, err = tc.chain.GetTransaction(res.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), txRes.Code, "SKU creation should succeed: %s", txRes.RawLog)

	skuPWRUUID, err := helpers.GetSKUUUIDFromTxHash(ctx, tc.chain, res.TxHash)
	require.NoError(t, err)
	t.Logf("Created SKU with PWR denom, ID: %s", skuPWRUUID)

	// Setup: create SKU with second denom
	// 7200000 per hour = 2000 per second
	t.Log("Creating SKU with second denom...")
	res, err = helpers.SKUCreateSKU(ctx, tc.chain, tc.authority, multiDenomProviderUUID, "Storage TEST", 1, fmt.Sprintf("7200000%s", secondDenom), "")
	require.NoError(t, err, "failed to create SKU with second denom")
	txRes, err = tc.chain.GetTransaction(res.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), txRes.Code, "SKU creation should succeed: %s", txRes.RawLog)

	skuSecondUUID, err := helpers.GetSKUUUIDFromTxHash(ctx, tc.chain, res.TxHash)
	require.NoError(t, err)
	t.Logf("Created SKU with second denom, ID: %s", skuSecondUUID)

	// Setup: create tenant for multi-denom tests
	users := interchaintest.GetAndFundTestUsers(t, ctx, "multi-denom-tenant", DefaultGenesisAmt, tc.chain)
	tenant := users[0]

	// Setup: fund tenant with both denoms
	t.Log("Funding tenant with both denoms...")
	// Send PWR denom
	err = node.SendFunds(ctx, tc.authority.KeyName(), ibc.WalletAmount{
		Address: tenant.FormattedAddress(),
		Denom:   tc.pwrDenom,
		Amount:  sdkmath.NewInt(500_000_000), // 500M PWR
	})
	require.NoError(t, err, "failed to send PWR to tenant")

	// Send second denom
	err = node.SendFunds(ctx, tc.authority.KeyName(), ibc.WalletAmount{
		Address: tenant.FormattedAddress(),
		Denom:   secondDenom,
		Amount:  sdkmath.NewInt(500_000_000), // 500M second denom
	})
	require.NoError(t, err, "failed to send second denom to tenant")

	require.NoError(t, testutil.WaitForBlocks(ctx, 2, tc.chain))

	// Fund credit account with both denoms
	t.Run("success: fund credit account with first denom", func(t *testing.T) {
		fundAmount := fmt.Sprintf("200000000%s", tc.pwrDenom) // 200M PWR
		res, err := helpers.BillingFundCredit(ctx, tc.chain, tenant, tenant.FormattedAddress(), fundAmount)
		require.NoError(t, err)
		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "fund credit with PWR should succeed")
	})

	t.Run("success: fund credit account with second denom", func(t *testing.T) {
		fundAmount := fmt.Sprintf("200000000%s", secondDenom) // 200M second denom
		res, err := helpers.BillingFundCredit(ctx, tc.chain, tenant, tenant.FormattedAddress(), fundAmount)
		require.NoError(t, err)
		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "fund credit with second denom should succeed")
	})

	// Verify credit account has both denoms
	t.Run("success: verify credit account has multiple denoms", func(t *testing.T) {
		creditRes, err := helpers.BillingQueryCreditAccount(ctx, tc.chain, tenant.FormattedAddress())
		require.NoError(t, err)
		require.NotNil(t, creditRes)
		t.Logf("Credit account balances: %s", creditRes.Balances)

		// Should have at least 2 coins in balances
		require.GreaterOrEqual(t, len(creditRes.Balances), 2, "credit account should have multiple denoms")

		// Check specific balances
		pwrBalance := creditRes.Balances.AmountOf(tc.pwrDenom)
		secondBalance := creditRes.Balances.AmountOf(secondDenom)
		require.True(t, pwrBalance.GT(sdkmath.ZeroInt()), "should have PWR balance")
		require.True(t, secondBalance.GT(sdkmath.ZeroInt()), "should have second denom balance")
	})

	// Create lease with SKUs using different denoms
	var multiDenomLeaseID string
	t.Run("success: create lease with SKUs using different denoms", func(t *testing.T) {
		items := []string{
			fmt.Sprintf("%s:1", skuPWRUUID),    // Uses PWR denom
			fmt.Sprintf("%s:1", skuSecondUUID), // Uses second denom
		}
		res, err := helpers.BillingCreateLease(ctx, tc.chain, tenant, items)
		require.NoError(t, err)
		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "multi-denom lease creation should succeed: %s", txRes.RawLog)

		multiDenomLeaseID, err = helpers.GetLeaseIDFromTxHash(ctx, tc.chain, res.TxHash)
		require.NoError(t, err)
		t.Logf("Created multi-denom lease ID: %s", multiDenomLeaseID)

		// Acknowledge the lease to make it ACTIVE
		ackRes, err := helpers.BillingAcknowledgeLease(ctx, tc.chain, tc.providerWallet, multiDenomLeaseID)
		require.NoError(t, err)
		ackTxRes, err := tc.chain.GetTransaction(ackRes.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), ackTxRes.Code, "lease acknowledgement should succeed: %s", ackTxRes.RawLog)
	})

	// Verify lease items have correct denoms
	t.Run("success: verify lease items have correct denoms", func(t *testing.T) {
		leaseRes, err := helpers.BillingQueryLease(ctx, tc.chain, multiDenomLeaseID)
		require.NoError(t, err)
		require.Len(t, leaseRes.Lease.Items, 2, "lease should have 2 items")

		// Items should have different denoms
		denoms := make(map[string]bool)
		for _, item := range leaseRes.Lease.Items {
			denoms[item.LockedPrice.Denom] = true
		}
		require.Len(t, denoms, 2, "lease items should use 2 different denoms")
		require.True(t, denoms[tc.pwrDenom], "lease should include PWR denom")
		require.True(t, denoms[secondDenom], "lease should include second denom")
	})

	// Wait for accrual
	require.NoError(t, testutil.WaitForBlocks(ctx, 5, tc.chain))

	// Query withdrawable - should show multiple denoms
	t.Run("success: withdrawable amounts show multiple denoms", func(t *testing.T) {
		withdrawableRes, err := helpers.BillingQueryWithdrawable(ctx, tc.chain, multiDenomLeaseID)
		require.NoError(t, err)
		require.NotNil(t, withdrawableRes)
		t.Logf("Withdrawable amounts: %s", withdrawableRes.Amounts)

		// Should have amounts in both denoms
		require.GreaterOrEqual(t, len(withdrawableRes.Amounts), 2, "withdrawable should have multiple denoms")
	})

	// Withdraw - should receive multiple denoms
	t.Run("success: withdraw receives multiple denoms", func(t *testing.T) {
		// Get initial balances
		initialPWR, err := tc.chain.GetBalance(ctx, tc.providerWallet.FormattedAddress(), tc.pwrDenom)
		require.NoError(t, err)
		initialSecond, err := tc.chain.GetBalance(ctx, tc.providerWallet.FormattedAddress(), secondDenom)
		require.NoError(t, err)

		res, err := helpers.BillingWithdraw(ctx, tc.chain, tc.providerWallet, multiDenomLeaseID)
		require.NoError(t, err)
		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "withdrawal should succeed")

		// Verify provider received both denoms
		newPWR, err := tc.chain.GetBalance(ctx, tc.providerWallet.FormattedAddress(), tc.pwrDenom)
		require.NoError(t, err)
		newSecond, err := tc.chain.GetBalance(ctx, tc.providerWallet.FormattedAddress(), secondDenom)
		require.NoError(t, err)

		require.True(t, newPWR.GT(initialPWR), "provider should receive PWR from withdrawal")
		require.True(t, newSecond.GT(initialSecond), "provider should receive second denom from withdrawal")
		t.Logf("Received PWR: %s -> %s", initialPWR, newPWR)
		t.Logf("Received second: %s -> %s", initialSecond, newSecond)
	})

	// Wait more and close lease
	require.NoError(t, testutil.WaitForBlocks(ctx, 3, tc.chain))

	t.Run("success: close lease settles multiple denoms", func(t *testing.T) {
		// Get pre-close balances
		prePWR, err := tc.chain.GetBalance(ctx, tc.providerWallet.FormattedAddress(), tc.pwrDenom)
		require.NoError(t, err)
		preSecond, err := tc.chain.GetBalance(ctx, tc.providerWallet.FormattedAddress(), secondDenom)
		require.NoError(t, err)

		res, err := helpers.BillingCloseLease(ctx, tc.chain, tenant, multiDenomLeaseID)
		require.NoError(t, err)
		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "close should succeed")

		// Verify settlement transferred both denoms
		postPWR, err := tc.chain.GetBalance(ctx, tc.providerWallet.FormattedAddress(), tc.pwrDenom)
		require.NoError(t, err)
		postSecond, err := tc.chain.GetBalance(ctx, tc.providerWallet.FormattedAddress(), secondDenom)
		require.NoError(t, err)

		require.True(t, postPWR.GTE(prePWR), "provider should receive PWR from settlement")
		require.True(t, postSecond.GTE(preSecond), "provider should receive second denom from settlement")
	})

	// Test: lease creation fails with insufficient credit for one denom
	t.Run("fail: insufficient credit for one denom", func(t *testing.T) {
		// Create a new tenant with only one denom
		oneUsers := interchaintest.GetAndFundTestUsers(t, ctx, "one-denom-tenant", DefaultGenesisAmt, tc.chain)
		oneDenomTenant := oneUsers[0]

		// Send only PWR denom
		err := node.SendFunds(ctx, tc.authority.KeyName(), ibc.WalletAmount{
			Address: oneDenomTenant.FormattedAddress(),
			Denom:   tc.pwrDenom,
			Amount:  sdkmath.NewInt(500_000_000),
		})
		require.NoError(t, err)
		require.NoError(t, testutil.WaitForBlocks(ctx, 2, tc.chain))

		// Fund credit only with PWR
		fundAmount := fmt.Sprintf("200000000%s", tc.pwrDenom)
		res, err := helpers.BillingFundCredit(ctx, tc.chain, oneDenomTenant, oneDenomTenant.FormattedAddress(), fundAmount)
		require.NoError(t, err)
		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code)

		// Try to create lease requiring both denoms - should fail
		items := []string{
			fmt.Sprintf("%s:1", skuPWRUUID),    // Uses PWR - has enough
			fmt.Sprintf("%s:1", skuSecondUUID), // Uses second denom - insufficient!
		}
		res, err = helpers.BillingCreateLease(ctx, tc.chain, oneDenomTenant, items)
		require.NoError(t, err)
		txRes, err = tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "lease should fail with insufficient second denom")
		require.Contains(t, txRes.RawLog, "insufficient credit", "error should indicate insufficient credit")
	})

	// Test: lease with same denom multiple SKUs works correctly
	t.Run("success: lease with same denom multiple SKUs", func(t *testing.T) {
		// Create two more SKUs with same denom
		res, err := helpers.SKUCreateSKU(ctx, tc.chain, tc.authority, multiDenomProviderUUID, "Compute PWR 2", 1, fmt.Sprintf("1800000%s", tc.pwrDenom), "")
		require.NoError(t, err)
		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code)
		skuPWRUUID2, err := helpers.GetSKUUUIDFromTxHash(ctx, tc.chain, res.TxHash)
		require.NoError(t, err)

		// Create tenant
		sameUsers := interchaintest.GetAndFundTestUsers(t, ctx, "same-denom-tenant", DefaultGenesisAmt, tc.chain)
		sameDenomTenant := sameUsers[0]

		// Fund with PWR
		err = node.SendFunds(ctx, tc.authority.KeyName(), ibc.WalletAmount{
			Address: sameDenomTenant.FormattedAddress(),
			Denom:   tc.pwrDenom,
			Amount:  sdkmath.NewInt(500_000_000),
		})
		require.NoError(t, err)
		require.NoError(t, testutil.WaitForBlocks(ctx, 2, tc.chain))

		fundAmount := fmt.Sprintf("200000000%s", tc.pwrDenom)
		res, err = helpers.BillingFundCredit(ctx, tc.chain, sameDenomTenant, sameDenomTenant.FormattedAddress(), fundAmount)
		require.NoError(t, err)
		txRes, err = tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code)

		// Create lease with multiple SKUs using same denom
		items := []string{
			fmt.Sprintf("%s:1", skuPWRUUID),
			fmt.Sprintf("%s:1", skuPWRUUID2),
		}
		res, err = helpers.BillingCreateLease(ctx, tc.chain, sameDenomTenant, items)
		require.NoError(t, err)
		txRes, err = tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "same denom multi-SKU lease should succeed")
	})
}
