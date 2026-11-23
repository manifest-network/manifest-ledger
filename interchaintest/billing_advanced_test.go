// Package interchaintest contains end-to-end tests for the billing module.
// This file contains advanced billing tests: CreateLeaseForTenant, AutoClose,
// ProviderWithdrawLimits, ProviderDeactivation, AllowedListAuthorization, MultiDenom.
package interchaintest

import (
	"context"
	"encoding/base64"
	"fmt"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/strangelove-ventures/interchaintest/v8"
	"github.com/strangelove-ventures/interchaintest/v8/ibc"
	"github.com/strangelove-ventures/interchaintest/v8/testutil"
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
//
// Settlement is lazy and per stream: nothing happens when a tenant's credit
// runs out until a settlement touches its stream. A withdrawal that finds the
// credit exhausted pays the provider what is left and freezes the stream; the
// EndBlocker of that same block then closes the stream's leases. An explicit
// close of such a lease settles and closes it within the transaction instead.
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

	// The tenant's credit is consumed creditSeconds after its stream was last
	// settled: 15,000 credit at 1,000 per second.
	const (
		autoCloseCredit      = int64(15_000)
		autoCloseRate        = int64(1_000)
		autoCloseMinDuration = int64(10)
		creditSeconds        = autoCloseCredit / autoCloseRate
	)
	autoCloseTenantAddr := autoCloseTenant.FormattedAddress()

	// The withdrawal that froze the tenant's stream; set by the first subtest
	// and inspected by the next ones.
	var (
		freezeTx    *sdk.TxResponse
		freezeBlock *helpers.BlockInfo
	)

	t.Run("success: withdrawal freezes an exhausted stream and the EndBlocker closes its lease", func(t *testing.T) {
		stream, err := helpers.BillingQueryStream(ctx, tc.chain, tc.providerUUID, autoCloseTenantAddr, tc.pwrDenom)
		require.NoError(t, err)
		require.NotNil(t, stream, "the acknowledged lease should have a stream")
		require.Equal(t, sdkmath.NewInt(autoCloseRate).String(), stream.State.Rate.String())
		require.Equal(t, uint64(1), stream.State.LeaseCount)
		require.False(t, stream.State.IsExhausted())

		// Wait until the accrual exceeds the credit. This waits on block time,
		// which is what accrual is computed from, rather than on a number of
		// blocks, so it does not depend on the block interval.
		t.Log("Waiting for credit to accrue/exhaust...")
		waitForBlockTime(t, ctx, tc.chain, stream.State.LastSettledAt.Add(time.Duration(creditSeconds+1)*time.Second))

		// Settlement is lazy: until something touches the stream, the credit is
		// still in the tenant's account and the lease is still ACTIVE.
		creditRes, err := helpers.BillingQueryCreditAccount(ctx, tc.chain, autoCloseTenantAddr)
		require.NoError(t, err)
		t.Logf("Credit balance after accrual: %s", creditRes.Balances)
		require.Equal(t, sdkmath.NewInt(autoCloseCredit).String(), creditRes.Balances.AmountOf(tc.pwrDenom).String(),
			"nothing is settled before the stream is touched")
		require.Equal(t, uint64(1), creditRes.CreditAccount.ActiveLeaseCount)
		require.Equal(t, sdkmath.NewInt(autoCloseRate*autoCloseMinDuration).String(),
			creditRes.CreditAccount.ReservedAmounts.AmountOf(tc.pwrDenom).String(),
			"the active lease reserves rate × min_lease_duration")
		lease, err := helpers.BillingQueryLease(ctx, tc.chain, autoCloseLeaseID)
		require.NoError(t, err)
		require.Equal(t, billingtypes.LEASE_STATE_ACTIVE, lease.Lease.GetState(), "an untouched exhausted lease stays active")

		providerBalance, err := tc.chain.GetBalance(ctx, tc.providerWallet.FormattedAddress(), tc.pwrDenom)
		require.NoError(t, err)
		t.Logf("Provider balance BEFORE the withdrawal: %s", providerBalance.String())

		// Trigger settlement with a withdrawal. It pays the provider what is
		// left and freezes the stream; it does not close the lease itself.
		res, err := helpers.BillingWithdrawByProvider(ctx, tc.chain, tc.providerWallet, tc.providerUUID, 0)
		require.NoError(t, err)

		freezeTx, err = tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		t.Logf("Withdrawal tx result: height=%d, code=%d, log=%s", freezeTx.Height, freezeTx.Code, freezeTx.RawLog)
		require.Equal(t, uint32(0), freezeTx.Code, "withdrawal should succeed and settle the remaining credit")

		// The tenant's stream paid the provider exactly the whole credit.
		settled, ok := helpers.FindEvent(freezeTx.Events, billingtypes.EventTypeStreamSettled, billingtypes.AttributeKeyTenant, autoCloseTenantAddr)
		require.True(t, ok, "the withdrawal should settle the tenant's stream")
		require.Equal(t, sdk.NewInt64Coin(tc.pwrDenom, autoCloseCredit).String(), settled[billingtypes.AttributeKeyAmount],
			"an exhausted stream pays the tenant's remaining credit, not rate × seconds")
		require.Equal(t, tc.providerUUID, settled[billingtypes.AttributeKeyProviderUUID])
		require.Equal(t, tc.providerWallet.FormattedAddress(), settled[billingtypes.AttributeKeyPayoutAddress])

		batches := helpers.FilterEvents(freezeTx.Events, billingtypes.EventTypeBatchWithdraw)
		require.Len(t, batches, 1)
		batchAmount, err := sdk.ParseCoinsNormalized(batches[0][billingtypes.AttributeKeyAmount])
		require.NoError(t, err)

		providerBalanceAfter, err := tc.chain.GetBalance(ctx, tc.providerWallet.FormattedAddress(), tc.pwrDenom)
		require.NoError(t, err)
		t.Logf("Provider balance AFTER the withdrawal: %s", providerBalanceAfter.String())
		require.Equal(t, batchAmount.AmountOf(tc.pwrDenom).String(), providerBalanceAfter.Sub(providerBalance).String(),
			"provider should receive exactly what the withdrawal reports")
		require.True(t, batchAmount.AmountOf(tc.pwrDenom).GTE(sdkmath.NewInt(autoCloseCredit)),
			"the withdrawal total includes the tenant's whole credit")

		// The credit is fully consumed.
		creditResAfter, err := helpers.BillingQueryCreditAccount(ctx, tc.chain, autoCloseTenantAddr)
		require.NoError(t, err)
		t.Logf("Credit balance AFTER the withdrawal: %s", creditResAfter.Balances)
		require.True(t, creditResAfter.Balances.AmountOf(tc.pwrDenom).IsZero(),
			"credit balance should be depleted, got %s", creditResAfter.Balances)

		// The EndBlocker of the withdrawal's own block closed the lease.
		freezeBlock, err = helpers.GetBlockInfo(ctx, tc.chain, freezeTx.Height)
		require.NoError(t, err)
		closedLease := waitForLeaseState(t, ctx, tc.chain, autoCloseLeaseID, billingtypes.LEASE_STATE_CLOSED, 5)
		require.NotNil(t, closedLease.Lease.ClosedAt, "closed_at should be set for auto-closed lease")
		require.True(t, closedLease.Lease.ClosedAt.Equal(freezeBlock.Time),
			"closed_at %s should be the time of the block that froze the stream (%s)", closedLease.Lease.ClosedAt, freezeBlock.Time)
		t.Log("Lease was auto-closed as expected")
	})

	t.Run("success: stream freeze and lease auto-close emit proper events", func(t *testing.T) {
		require.NotNil(t, freezeTx, "the withdrawal subtest must run first")
		require.NotNil(t, freezeBlock, "the withdrawal subtest must run first")

		// The withdrawal transaction reports the freeze...
		exhausted, ok := helpers.FindEvent(freezeTx.Events, billingtypes.EventTypeStreamExhausted, billingtypes.AttributeKeyTenant, autoCloseTenantAddr)
		require.True(t, ok, "the withdrawal should emit stream_exhausted for the tenant's stream")
		require.Equal(t, tc.providerUUID, exhausted[billingtypes.AttributeKeyProviderUUID])
		require.Equal(t, tc.pwrDenom, exhausted[billingtypes.AttributeKeyDenom])

		batches := helpers.FilterEvents(freezeTx.Events, billingtypes.EventTypeBatchWithdraw)
		require.Len(t, batches, 1)
		require.Equal(t, "1", batches[0][billingtypes.AttributeKeyExhaustedCount])

		// ...but closes nothing: lease_auto_closed is a block event, emitted by
		// the EndBlocker of the same block.
		require.Empty(t, helpers.FilterEvents(freezeTx.Events, billingtypes.EventTypeLeaseAutoClose),
			"the withdrawal transaction must not close leases itself")
		require.Empty(t, helpers.FilterEvents(freezeTx.Events, billingtypes.EventTypeLeaseClosed))

		autoClosed, ok := helpers.FindEvent(freezeBlock.Events, billingtypes.EventTypeLeaseAutoClose, billingtypes.AttributeKeyLeaseUUID, autoCloseLeaseID)
		require.True(t, ok, "block %d should emit lease_auto_closed for lease %s", freezeTx.Height, autoCloseLeaseID)
		require.Equal(t, autoCloseTenantAddr, autoClosed[billingtypes.AttributeKeyTenant])
		require.Equal(t, tc.providerUUID, autoClosed[billingtypes.AttributeKeyProviderUUID])
		require.Equal(t, "credit_exhausted", autoClosed[billingtypes.AttributeKeyReason])

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

	t.Run("success: auto-close releases the stream, the reservation and the lease count", func(t *testing.T) {
		// A stream exists only while it has an ACTIVE lease.
		stream, err := helpers.BillingQueryStream(ctx, tc.chain, tc.providerUUID, autoCloseTenantAddr, tc.pwrDenom)
		require.NoError(t, err)
		require.Nil(t, stream, "the drained stream should be deleted")

		creditRes, err := helpers.BillingQueryCreditAccount(ctx, tc.chain, autoCloseTenantAddr)
		require.NoError(t, err)
		require.Zero(t, creditRes.CreditAccount.ActiveLeaseCount)
		require.True(t, creditRes.CreditAccount.ReservedAmounts.IsZero(),
			"the closed lease's reservation should be released, got %s", creditRes.CreditAccount.ReservedAmounts)

		active, err := helpers.BillingQueryLeasesByTenant(ctx, tc.chain, autoCloseTenantAddr, "active")
		require.NoError(t, err)
		require.Empty(t, active.Leases)
	})

	t.Run("success: provider already withdrew when the stream froze", func(t *testing.T) {
		// The provider received the tenant's whole credit in the settlement that
		// froze the stream. Another withdrawal succeeds, since the provider may
		// have other streams, but takes nothing more from this tenant.
		res, err := helpers.BillingWithdrawByProvider(ctx, tc.chain, tc.providerWallet, tc.providerUUID, 0)
		require.NoError(t, err)

		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "a withdrawal with nothing accrued is not an error: %s", txRes.RawLog)

		_, paidAgain := helpers.FindEvent(txRes.Events, billingtypes.EventTypeStreamSettled, billingtypes.AttributeKeyTenant, autoCloseTenantAddr)
		require.False(t, paidAgain, "the closed lease's tenant must not be charged again")

		creditRes, err := helpers.BillingQueryCreditAccount(ctx, tc.chain, autoCloseTenantAddr)
		require.NoError(t, err)
		require.True(t, creditRes.Balances.AmountOf(tc.pwrDenom).IsZero())
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

		// Wait until the accrual exceeds the credit (15 seconds after the stream
		// was last settled), leaving an unpaid remainder.
		stream, err := helpers.BillingQueryStream(ctx, tc.chain, tc.providerUUID, tenant2.FormattedAddress(), tc.pwrDenom)
		require.NoError(t, err)
		require.NotNil(t, stream)
		waitForBlockTime(t, ctx, tc.chain, stream.State.LastSettledAt.Add(time.Duration(creditSeconds+1)*time.Second))

		providerBalance, err := tc.chain.GetBalance(ctx, tc.providerWallet.FormattedAddress(), tc.pwrDenom)
		require.NoError(t, err)

		res, err = helpers.BillingCloseLease(ctx, tc.chain, tenant2, leaseID)
		require.NoError(t, err)
		txRes, err = tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "explicit close should succeed even with exhausted credit")

		// The close is synchronous: unlike a withdrawal, it closes the lease in
		// the transaction, after settling the stream for the last time.
		lease, err := helpers.BillingQueryLease(ctx, tc.chain, leaseID)
		require.NoError(t, err)
		require.Equal(t, billingtypes.LEASE_STATE_CLOSED, lease.Lease.GetState(), "lease should be inactive after exhaustion")

		// The credit could not cover the accrual, so the closure is attributed
		// to credit exhaustion, not to the tenant.
		require.Equal(t, billingtypes.ClosureReasonCreditExhausted, lease.Lease.GetClosureReason())
		closed, ok := helpers.FindEvent(txRes.Events, billingtypes.EventTypeLeaseClosed, billingtypes.AttributeKeyLeaseUUID, leaseID)
		require.True(t, ok, "the close should emit lease_closed")
		require.Equal(t, "credit_exhaustion", closed[billingtypes.AttributeKeyClosedBy])
		require.Empty(t, helpers.FilterEvents(txRes.Events, billingtypes.EventTypeLeaseAutoClose))

		// The provider received the tenant's whole credit, and nothing more.
		settled, ok := helpers.FindEvent(txRes.Events, billingtypes.EventTypeStreamSettled, billingtypes.AttributeKeyTenant, tenant2.FormattedAddress())
		require.True(t, ok, "the close should settle the tenant's stream")
		require.Equal(t, sdk.NewInt64Coin(tc.pwrDenom, autoCloseCredit).String(), settled[billingtypes.AttributeKeyAmount])
		providerBalanceAfter, err := tc.chain.GetBalance(ctx, tc.providerWallet.FormattedAddress(), tc.pwrDenom)
		require.NoError(t, err)
		require.Equal(t, sdkmath.NewInt(autoCloseCredit).String(), providerBalanceAfter.Sub(providerBalance).String())

		creditRes, err := helpers.BillingQueryCreditAccount(ctx, tc.chain, tenant2.FormattedAddress())
		require.NoError(t, err)
		require.True(t, creditRes.Balances.AmountOf(tc.pwrDenom).IsZero(), "credit should be depleted, got %s", creditRes.Balances)
		require.Zero(t, creditRes.CreditAccount.ActiveLeaseCount)
		require.True(t, creditRes.CreditAccount.ReservedAmounts.IsZero())

		// The frozen stream lost its only lease, so it is deleted rather than
		// left for the EndBlocker.
		stream, err = helpers.BillingQueryStream(ctx, tc.chain, tc.providerUUID, tenant2.FormattedAddress(), tc.pwrDenom)
		require.NoError(t, err)
		require.Nil(t, stream)
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

		// The lease was the only one of its stream, which started at the
		// acknowledgement without a remainder, so the close charged exactly the
		// rate (1000/second) for the time the lease was active, to the
		// nanosecond: rate × active nanoseconds / 1e9, fractions of a second
		// included.
		require.NotNil(t, lease.Lease.AcknowledgedAt)
		require.NotNil(t, lease.Lease.ClosedAt)
		active := lease.Lease.ClosedAt.Sub(*lease.Lease.AcknowledgedAt)
		require.Positive(t, active)
		expectedCharge, _ := helpers.StreamStateJSON{
			Rate: sdkmath.NewInt(1000), LastSettledAt: *lease.Lease.AcknowledgedAt, Remainder: sdkmath.ZeroInt(),
		}.AccrualAt(*lease.Lease.ClosedAt)
		charged := initialCredit.Balances.AmountOf(tc.pwrDenom).Sub(afterCredit.Balances.AmountOf(tc.pwrDenom))
		require.Equal(t, expectedCharge.String(), charged.String(),
			"close should charge the rate for exactly the %s the lease was active", active)
		// The tenant chose to close and its credit covered the charge.
		require.Empty(t, lease.Lease.GetClosureReason())
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
	// The withdraw limit counts streams, and a tenant has one stream per
	// provider and denom however many leases it holds. Paginating a withdrawal
	// therefore takes several tenants: one with three leases (a single stream
	// aggregating them) and two with one lease each.
	const multiLeaseCount = 3
	tenants := interchaintest.GetAndFundTestUsers(t, ctx, "provider-withdraw-limit-tenant", DefaultGenesisAmt, tc.chain, tc.chain, tc.chain)
	leasesPerTenant := []int{multiLeaseCount, 1, 1}

	for i, tenant := range tenants {
		// Fund tenant's credit account
		fundAmount := fmt.Sprintf("500000000%s", tc.pwrDenom) // 500 PWR
		res, err := helpers.BillingFundCredit(ctx, tc.chain, tc.authority, tenant.FormattedAddress(), fundAmount)
		require.NoError(t, err)
		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code)

		for range leasesPerTenant[i] {
			items := []string{fmt.Sprintf("%s:1", tc.skuUUID)}
			_, err := helpers.BillingCreateAndAcknowledgeLease(ctx, tc.chain, tenant, tc.providerWallet, items)
			require.NoError(t, err)
		}
	}

	multiLeaseStream := streamKey{providerUUID: tc.providerUUID, tenant: tenants[0].FormattedAddress(), denom: tc.pwrDenom}

	t.Run("success: leases of one tenant share a stream", func(t *testing.T) {
		streams := snapshotProviderStreams(t, ctx, tc.chain, tc.providerUUID)
		require.GreaterOrEqual(t, len(streams), len(tenants), "each tenant should have a stream with the provider")

		for i, tenant := range tenants {
			key := streamKey{providerUUID: tc.providerUUID, tenant: tenant.FormattedAddress(), denom: tc.pwrDenom}
			stream, ok := streams[key]
			require.True(t, ok, "tenant %d should have a stream", i)
			require.Equal(t, uint64(leasesPerTenant[i]), stream.LeaseCount)
			// tc.skuUUID costs 1000 per second.
			require.Equal(t, sdkmath.NewInt(1000*int64(leasesPerTenant[i])).String(), stream.Rate.String())
		}
	})

	// Wait for some accrual
	require.NoError(t, testutil.WaitForBlocks(ctx, 5, tc.chain))

	// Test: provider withdraw with custom limit
	t.Run("success: provider withdraw paginates over streams", func(t *testing.T) {
		streamCount := len(snapshotProviderStreams(t, ctx, tc.chain, tc.providerUUID))
		require.GreaterOrEqual(t, streamCount, 3)

		// A limit of 2 settles two streams, whatever the number of leases they
		// aggregate, and reports that more remain.
		first := requireProviderWithdraw(t, ctx, tc.chain, tc.providerWallet, tc.providerUUID, 2, "", "--gas", "2000000")
		require.Len(t, first.Settled, 2, "limit should count streams, not leases")
		require.Equal(t, uint64(2), first.Response.WithdrawalCount)
		require.True(t, first.Response.HasMore, "%d streams remain", streamCount-2)
		require.NotEmpty(t, first.Response.NextKey)

		// Continue from the cursor until the provider has no more streams. Each
		// page settles the streams behind the previous one, none of them twice.
		seen := make(map[streamKey]struct{})
		for _, key := range first.Settled {
			seen[key] = struct{}{}
		}
		page := first
		for pages := 1; page.Response.HasMore; pages++ {
			require.Less(t, pages, streamCount, "pagination did not terminate")

			cursor := base64.StdEncoding.EncodeToString(page.Response.NextKey)
			page = requireProviderWithdraw(t, ctx, tc.chain, tc.providerWallet, tc.providerUUID, 2, cursor, "--gas", "2000000")
			require.NotEmpty(t, page.Settled, "a page behind the cursor should settle streams")
			require.LessOrEqual(t, len(page.Settled), 2)
			for _, key := range page.Settled {
				require.NotContains(t, seen, key, "stream settled by two pages of the same pass")
				seen[key] = struct{}{}
			}
		}
		require.Len(t, seen, streamCount, "the pages together should settle every stream once")
		require.Empty(t, page.Response.NextKey)
	})

	// Test: provider withdraw with default limit (0 means default)
	t.Run("success: provider withdraw with default limit", func(t *testing.T) {
		require.NoError(t, testutil.WaitForBlocks(ctx, 3, tc.chain))

		withdrawal := requireProviderWithdraw(t, ctx, tc.chain, tc.providerWallet, tc.providerUUID, 0, "", "--gas", "2000000")
		require.False(t, withdrawal.Response.HasMore, "the default limit (50) covers all of the provider's streams")
		require.Len(t, withdrawal.Settled, len(withdrawal.Before), "every stream should be settled")

		// The three leases of one tenant were paid by a single settlement, at
		// three times the rate of one lease: exactly
		// (remainder + 3000 × elapsed nanoseconds) / 1e9, as requireProviderWithdraw
		// verified for every settled stream.
		require.Contains(t, withdrawal.Settled, multiLeaseStream)
		before, after := withdrawal.Before[multiLeaseStream], withdrawal.After[multiLeaseStream]
		require.Equal(t, sdkmath.NewInt(multiLeaseCount*1000).String(), before.Rate.String())
		expected, remainder := before.AccrualAt(after.LastSettledAt)
		require.True(t, expected.IsPositive())
		require.Equal(t, expected.String(), withdrawal.Amounts[multiLeaseStream].String())
		require.Equal(t, remainder.String(), after.Remainder.String())
		settled, ok := helpers.FindEvent(withdrawal.Tx.Events, billingtypes.EventTypeStreamSettled,
			billingtypes.AttributeKeyTenant, multiLeaseStream.tenant)
		require.True(t, ok)
		require.Equal(t, sdk.NewCoin(tc.pwrDenom, expected).String(), settled[billingtypes.AttributeKeyAmount])
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
		// The stream of the lease created before the deactivation keeps accruing
		// and paying the deactivated provider.
		withdrawal := requireProviderWithdraw(t, ctx, tc.chain, deactivationProviderWallet, deactivateProviderUUID, 0, "")
		require.Equal(t, []streamKey{{providerUUID: deactivateProviderUUID, tenant: tenant.FormattedAddress(), denom: tc.pwrDenom}},
			withdrawal.Settled, "the tenant's stream should pay the deactivated provider")
		require.True(t, withdrawal.Paid.AmountOf(tc.pwrDenom).IsPositive())
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
		// The lease keeps its locked price, so its stream keeps accruing at the
		// rate of the SKU deactivated since.
		withdrawal := requireProviderWithdraw(t, ctx, tc.chain, skuDeactProviderWallet, skuDeactProviderUUID, 0, "")
		key := streamKey{providerUUID: skuDeactProviderUUID, tenant: tenant.FormattedAddress(), denom: tc.pwrDenom}
		require.Equal(t, []streamKey{key}, withdrawal.Settled, "the tenant's stream should pay the provider")
		require.Equal(t, "1000", withdrawal.Before[key].Rate.String())
		require.Equal(t, uint64(1), withdrawal.Before[key].LeaseCount)
		require.True(t, withdrawal.Paid.AmountOf(tc.pwrDenom).IsPositive())
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

	pwrStream := streamKey{providerUUID: multiDenomProviderUUID, tenant: tenant.FormattedAddress(), denom: tc.pwrDenom}
	secondStream := streamKey{providerUUID: multiDenomProviderUUID, tenant: tenant.FormattedAddress(), denom: secondDenom}

	// A lease priced in two denoms belongs to two streams, one per denom
	t.Run("success: multi-denom lease has one stream per denom", func(t *testing.T) {
		streams := snapshotProviderStreams(t, ctx, tc.chain, multiDenomProviderUUID)
		require.Len(t, streams, 2, "one stream per denom the lease is priced in")

		// 3600000 PWR per hour = 1000 per second; 7200000 of the second denom
		// per hour = 2000 per second.
		require.Contains(t, streams, pwrStream)
		require.Equal(t, "1000", streams[pwrStream].Rate.String())
		require.Equal(t, uint64(1), streams[pwrStream].LeaseCount)
		require.Contains(t, streams, secondStream)
		require.Equal(t, "2000", streams[secondStream].Rate.String())
		require.Equal(t, uint64(1), streams[secondStream].LeaseCount)

		// Both streams were created by the same acknowledgement, so they start at
		// the same block time without a remainder.
		require.True(t, streams[pwrStream].LastSettledAt.Equal(streams[secondStream].LastSettledAt))
		require.True(t, streams[pwrStream].Remainder.IsZero())
		require.True(t, streams[secondStream].Remainder.IsZero())

		// The tenant sees the same two streams.
		byTenant, err := helpers.BillingQueryStreamsByTenant(ctx, tc.chain, tenant.FormattedAddress())
		require.NoError(t, err)
		require.Len(t, byTenant.Streams, 2)
		require.NotNil(t, byTenant.Find(multiDenomProviderUUID, tenant.FormattedAddress(), tc.pwrDenom))
		require.NotNil(t, byTenant.Find(multiDenomProviderUUID, tenant.FormattedAddress(), secondDenom))

		// Paging the tenant's streams one at a time, with the printed next_key
		// (base64) as --page-key, returns them in the same order.
		first, nextKey, err := helpers.BillingQueryStreamsByTenantPaginated(ctx, tc.chain, tenant.FormattedAddress(), 1, "")
		require.NoError(t, err)
		require.Len(t, first.Streams, 1)
		require.Equal(t, byTenant.Streams[0].Denom, first.Streams[0].Denom)
		require.NotEmpty(t, nextKey, "a second stream remains")
		second, nextKey, err := helpers.BillingQueryStreamsByTenantPaginated(ctx, tc.chain, tenant.FormattedAddress(), 1, nextKey)
		require.NoError(t, err)
		require.Len(t, second.Streams, 1)
		require.Equal(t, byTenant.Streams[1].Denom, second.Streams[0].Denom, "the cursor should lead to the tenant's other stream")
		require.Equal(t, multiDenomProviderUUID, second.Streams[0].ProviderUuid)
		require.Empty(t, nextKey, "the last page should not have a next key")
	})

	// Query withdrawable - should show multiple denoms
	t.Run("success: withdrawable amounts show multiple denoms", func(t *testing.T) {
		withdrawableRes, err := helpers.BillingQueryProviderWithdrawable(ctx, tc.chain, multiDenomProviderUUID)
		require.NoError(t, err)
		require.NotNil(t, withdrawableRes)
		t.Logf("Withdrawable amounts: %s", withdrawableRes.Amounts)

		// Should have amounts in both denoms
		require.Len(t, withdrawableRes.Amounts, 2, "withdrawable should have multiple denoms")
		pwrAmount := withdrawableRes.Amounts.AmountOf(tc.pwrDenom)
		secondAmount := withdrawableRes.Amounts.AmountOf(secondDenom)
		require.True(t, pwrAmount.IsPositive(), "should have accrued PWR")
		// Both streams accrue over the same time, at 1000 and 2000 per second,
		// each rounded down to a whole unit: floor(2x) is 2·floor(x) or one more.
		require.True(t, secondAmount.Equal(pwrAmount.MulRaw(2)) || secondAmount.Equal(pwrAmount.MulRaw(2).AddRaw(1)),
			"the second denom accrues at twice the rate over the same time: %s vs %s", secondAmount, pwrAmount)
	})

	// Withdraw - should receive multiple denoms
	t.Run("success: withdraw receives multiple denoms", func(t *testing.T) {
		// The provider-wide withdrawal settles both streams and pays each in its
		// own denom; requireProviderWithdraw checks the payout address received
		// each stream's exact accrual.
		withdrawal := requireProviderWithdraw(t, ctx, tc.chain, tc.providerWallet, multiDenomProviderUUID, 0, "")
		require.ElementsMatch(t, []streamKey{pwrStream, secondStream}, withdrawal.Settled)

		paidPWR := withdrawal.Paid.AmountOf(tc.pwrDenom)
		paidSecond := withdrawal.Paid.AmountOf(secondDenom)
		require.True(t, paidPWR.IsPositive(), "provider should receive PWR from withdrawal")
		// Same start, same block, no remainder carried in: twice the rate pays
		// twice as much, up to the unit each stream rounds down.
		require.True(t, paidSecond.Equal(paidPWR.MulRaw(2)) || paidSecond.Equal(paidPWR.MulRaw(2).AddRaw(1)),
			"provider should receive twice as much of the second denom: %s vs %s", paidSecond, paidPWR)
		require.Equal(t, uint64(2), withdrawal.Response.WithdrawalCount, "one payout per stream")
		t.Logf("Received PWR: %s, second: %s", paidPWR, paidSecond)
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
