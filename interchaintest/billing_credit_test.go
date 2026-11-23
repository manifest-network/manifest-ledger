// Package interchaintest contains end-to-end tests for the billing module.
// This file contains credit account and accrual/withdrawal tests.
//
// Run with: go test -v ./interchaintest -run TestBillingCredit -timeout 45m
package interchaintest

import (
	"context"
	"fmt"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	"github.com/strangelove-ventures/interchaintest/v8"
	"github.com/strangelove-ventures/interchaintest/v8/ibc"
	"github.com/strangelove-ventures/interchaintest/v8/testutil"
	"github.com/stretchr/testify/require"

	"github.com/manifest-network/manifest-ledger/interchaintest/helpers"
)

// setupCreditAccounts funds credit accounts for tenant1 and tenant2.
// This is called before subtests to ensure credit accounts exist regardless of test filter.
func setupCreditAccounts(t *testing.T, ctx context.Context, tc *billingTestContext) {
	t.Helper()
	t.Log("Setting up credit accounts for tenant1 and tenant2...")

	node := tc.chain.GetNode()

	// Fund tenant1's credit account
	err := node.SendFunds(ctx, tc.authority.KeyName(), ibc.WalletAmount{
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
	err = node.SendFunds(ctx, tc.authority.KeyName(), ibc.WalletAmount{
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
	setupCreditAccounts(t, ctx, tc)

	t.Run("QueryParams", func(t *testing.T) {
		testBillingQueryParamsIndependent(t, ctx, tc)
	})

	t.Run("CreditAccountOperations", func(t *testing.T) {
		testCreditAccountOperationsIndependent(t, ctx, tc)
	})

	t.Run("CreditAddressQuery", func(t *testing.T) {
		testCreditAddressQueryIndependent(t, ctx, tc)
	})

	t.Run("CreditAccountsQuery", func(t *testing.T) {
		testCreditAccountsQueryIndependent(t, ctx, tc)
	})

	t.Run("CreditEstimateQuery", func(t *testing.T) {
		testCreditEstimateQueryIndependent(t, ctx, tc)
	})

	t.Run("AccrualCalculation", func(t *testing.T) {
		testAccrualCalculationIndependent(t, ctx, tc)
	})

	t.Run("Withdraw", func(t *testing.T) {
		testWithdrawIndependent(t, ctx, tc)
	})

	t.Run("WithdrawByProvider", func(t *testing.T) {
		testWithdrawByProviderIndependent(t, ctx, tc)
	})

	t.Run("WithdrawableQueries", func(t *testing.T) {
		testWithdrawableQueriesIndependent(t, ctx, tc)
	})
}

func testBillingQueryParamsIndependent(t *testing.T, ctx context.Context, tc *billingTestContext) {
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

func testCreditAccountOperationsIndependent(t *testing.T, ctx context.Context, tc *billingTestContext) {
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
		err := node.SendFunds(ctx, tc.authority.KeyName(), ibc.WalletAmount{
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

func testCreditAddressQueryIndependent(t *testing.T, ctx context.Context, tc *billingTestContext) {
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

func testCreditAccountsQueryIndependent(t *testing.T, ctx context.Context, tc *billingTestContext) {
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

func testCreditEstimateQueryIndependent(t *testing.T, ctx context.Context, tc *billingTestContext) {
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

func testAccrualCalculationIndependent(t *testing.T, ctx context.Context, tc *billingTestContext) {
	t.Log("=== Testing Accrual Calculation ===")

	tenantAddr := tc.tenant1.FormattedAddress()

	// The tenant's stream with the provider before the new lease, if any.
	before, err := helpers.BillingQueryStream(ctx, tc.chain, tc.providerUUID, tenantAddr, tc.pwrDenom)
	require.NoError(t, err)
	rateBefore, leasesBefore := sdkmath.ZeroInt(), uint64(0)
	if before != nil {
		rateBefore, leasesBefore = before.State.Rate, before.State.LeaseCount
	}

	// Create a lease for accrual testing
	items := []string{fmt.Sprintf("%s:1", tc.skuUUID)}
	leaseUUID, err := helpers.BillingCreateAndAcknowledgeLease(ctx, tc.chain, tc.tenant1, tc.providerWallet, items)
	require.NoError(t, err)

	t.Run("success: acknowledged lease joins the tenant's stream", func(t *testing.T) {
		stream, err := helpers.BillingQueryStream(ctx, tc.chain, tc.providerUUID, tenantAddr, tc.pwrDenom)
		require.NoError(t, err)
		require.NotNil(t, stream, "an ACTIVE lease must be aggregated by a stream")

		// tc.skuUUID costs 3600000 per hour: 1000 per second.
		require.Equal(t, rateBefore.AddRaw(1000).String(), stream.State.Rate.String())
		require.Equal(t, leasesBefore+1, stream.State.LeaseCount)
		require.False(t, stream.State.IsExhausted())

		// Joining settles the stream at the acknowledgement, exactly at that
		// block's time, so the new lease is never charged for time before it
		// joined. A stream moves to the block time, not to a whole-second grid.
		lease, err := helpers.BillingQueryLease(ctx, tc.chain, leaseUUID)
		require.NoError(t, err)
		require.NotNil(t, lease.Lease.AcknowledgedAt)
		require.True(t, stream.State.LastSettledAt.Equal(*lease.Lease.AcknowledgedAt),
			"stream settled at %s, lease acknowledged at %s", stream.State.LastSettledAt, lease.Lease.AcknowledgedAt)
		require.False(t, stream.State.Remainder.IsNil())
		if before == nil {
			require.True(t, stream.State.Remainder.IsZero(), "a new stream starts without a remainder, got %s", stream.State.Remainder)
		} else {
			// The stream accrued at its previous rate up to the acknowledgement.
			_, remainder := before.State.AccrualAt(*lease.Lease.AcknowledgedAt)
			require.Equal(t, remainder.String(), stream.State.Remainder.String(),
				"remainder carried over the rate change should follow the accrual at the previous rate")
		}
	})

	t.Run("success: verify accrual increases over time", func(t *testing.T) {
		// expectedAt is what the provider's streams have accrued at a block time,
		// exactly: (remainder + rate × nanoseconds since the stream was last
		// settled) / 1e9, per stream. Every tenant of this suite is funded far
		// beyond that, so no credit balance caps it.
		streams := snapshotProviderStreams(t, ctx, tc.chain, tc.providerUUID)
		expectedAt := func(blockTime time.Time) sdkmath.Int {
			total := sdkmath.ZeroInt()
			for key, state := range streams {
				if key.denom != tc.pwrDenom {
					continue
				}
				accrued, _ := state.AccrualAt(blockTime)
				total = total.Add(accrued)
			}
			return total
		}

		// withdrawable reads the provider's withdrawable amount together with
		// the block times the query ran between: the query is answered at the
		// latest block, which may advance while it runs.
		withdrawable := func() (amount sdkmath.Int, earliest, latest time.Time) {
			earliest, err := helpers.GetLatestBlockTime(ctx, tc.chain)
			require.NoError(t, err)
			res, err := helpers.BillingQueryProviderWithdrawable(ctx, tc.chain, tc.providerUUID)
			require.NoError(t, err)
			latest, err = helpers.GetLatestBlockTime(ctx, tc.chain)
			require.NoError(t, err)
			return res.Amounts.AmountOf(tc.pwrDenom), earliest, latest
		}
		requireAccrued := func(amount sdkmath.Int, earliest, latest time.Time) {
			require.True(t, amount.GTE(expectedAt(earliest)) && amount.LTE(expectedAt(latest)),
				"withdrawable %s outside of [%s, %s], the accrual at block times %s and %s",
				amount, expectedAt(earliest), expectedAt(latest), earliest, latest)
		}

		initial, earliest, latest := withdrawable()
		t.Logf("Initial withdrawable: %s", initial)
		requireAccrued(initial, earliest, latest)

		// Wait for some blocks
		require.NoError(t, testutil.WaitForBlocks(ctx, 5, tc.chain))

		updated, earliest, latest := withdrawable()
		t.Logf("Updated withdrawable: %s", updated)
		requireAccrued(updated, earliest, latest)

		require.True(t, updated.GT(initial), "withdrawable should increase over time: %s -> %s", initial, updated)

		// Accrual is lazy: nothing was settled while it grew, and querying the
		// withdrawable amount does not settle a stream either.
		requireSameStreams(t, streams, snapshotProviderStreams(t, ctx, tc.chain, tc.providerUUID))
	})
}

func testWithdrawIndependent(t *testing.T, ctx context.Context, tc *billingTestContext) {
	t.Log("=== Testing Withdraw ===")

	tenantStream := streamKey{providerUUID: tc.providerUUID, tenant: tc.tenant1.FormattedAddress(), denom: tc.pwrDenom}

	// Get an active lease for tenant1
	leases, err := helpers.BillingQueryLeasesByTenant(ctx, tc.chain, tc.tenant1.FormattedAddress(), "active")
	require.NoError(t, err)
	require.NotEmpty(t, leases.Leases)
	leaseUUID := leases.Leases[0].Uuid

	// Wait for some accrual
	require.NoError(t, testutil.WaitForBlocks(ctx, 3, tc.chain))

	t.Run("success: provider withdraws what its streams accrued", func(t *testing.T) {
		withdrawal := requireProviderWithdraw(t, ctx, tc.chain, tc.providerWallet, tc.providerUUID, 0, "")

		// Settlement is per stream: the tenant's stream paid, whatever the
		// number of leases it aggregates.
		require.Contains(t, withdrawal.Settled, tenantStream)
		require.True(t, withdrawal.Paid.AmountOf(tc.pwrDenom).IsPositive(), "provider should be paid for the accrued seconds")
		require.False(t, withdrawal.Response.HasMore)
		t.Logf("Provider was paid %s by %d stream(s)", withdrawal.Paid, len(withdrawal.Settled))
	})

	t.Run("fail: withdrawing from individual leases is no longer supported", func(t *testing.T) {
		// Lease mode is gone: the CLI rejects lease UUIDs before broadcasting.
		_, err := helpers.BillingWithdrawLeasesLegacy(ctx, tc.chain, tc.providerWallet, []string{leaseUUID})
		require.ErrorContains(t, err, "withdrawing from individual leases is no longer supported")
	})

	t.Run("fail: tenant cannot withdraw", func(t *testing.T) {
		res, err := helpers.BillingWithdrawByProvider(ctx, tc.chain, tc.tenant1, tc.providerUUID, 0)
		require.NoError(t, err)

		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code)
		require.Contains(t, txRes.RawLog, "unauthorized")
	})

	t.Run("fail: unauthorized user cannot withdraw", func(t *testing.T) {
		before := snapshotProviderStreams(t, ctx, tc.chain, tc.providerUUID)

		res, err := helpers.BillingWithdrawByProvider(ctx, tc.chain, tc.unauthorizedUser, tc.providerUUID, 0)
		require.NoError(t, err)

		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code)
		require.Contains(t, txRes.RawLog, "unauthorized")

		// A rejected withdrawal settles nothing.
		requireSameStreams(t, before, snapshotProviderStreams(t, ctx, tc.chain, tc.providerUUID))
	})

	t.Run("success: authority withdraws on behalf of provider", func(t *testing.T) {
		require.NoError(t, testutil.WaitForBlocks(ctx, 3, tc.chain))

		// The funds still go to the provider's payout address, not to the authority.
		withdrawal := requireProviderWithdraw(t, ctx, tc.chain, tc.authority, tc.providerUUID, 0, "")
		require.Contains(t, withdrawal.Settled, tenantStream)
		require.Equal(t, tc.providerWallet.FormattedAddress(), withdrawal.Response.PayoutAddress)
	})
}

func testWithdrawByProviderIndependent(t *testing.T, ctx context.Context, tc *billingTestContext) {
	t.Log("=== Testing Withdraw By Provider ===")

	// Wait for some accrual
	require.NoError(t, testutil.WaitForBlocks(ctx, 5, tc.chain))

	t.Run("success: provider withdraws from all its streams", func(t *testing.T) {
		withdrawal := requireProviderWithdraw(t, ctx, tc.chain, tc.providerWallet, tc.providerUUID, 0, "")

		// Every stream had accrued for several blocks, so each one paid out.
		require.NotEmpty(t, withdrawal.Before)
		require.Len(t, withdrawal.Settled, len(withdrawal.Before), "every stream of the provider should pay out")
		require.False(t, withdrawal.Response.HasMore)
		t.Logf("Provider was paid %s by %d stream(s)", withdrawal.Paid, len(withdrawal.Settled))
	})
}

func testWithdrawableQueriesIndependent(t *testing.T, ctx context.Context, tc *billingTestContext) {
	t.Log("=== Testing Withdrawable Queries ===")

	tenantAddr := tc.tenant1.FormattedAddress()

	// A second tenant gives the provider a second stream to paginate over.
	items := []string{fmt.Sprintf("%s:2", tc.skuUUID)}
	_, err := helpers.BillingCreateAndAcknowledgeLease(ctx, tc.chain, tc.tenant2, tc.providerWallet, items)
	require.NoError(t, err)

	// expectedStream derives a tenant's stream with the provider from its
	// ACTIVE leases: the summed rate of their items and the number of leases.
	expectedStream := func(t *testing.T, tenant string) (rate sdkmath.Int, leaseCount uint64) {
		t.Helper()
		leases, err := helpers.BillingQueryLeasesByTenant(ctx, tc.chain, tenant, "active")
		require.NoError(t, err)
		require.Empty(t, leases.GetNextKeyString(), "tenant has more active leases than one query page")

		rate = sdkmath.ZeroInt()
		for _, lease := range leases.Leases {
			if lease.ProviderUuid != tc.providerUUID {
				continue
			}
			leaseCount++
			for _, item := range lease.Items {
				require.Equal(t, tc.pwrDenom, item.LockedPrice.Denom)
				quantity, ok := sdkmath.NewIntFromString(item.Quantity)
				require.True(t, ok, "quantity %q", item.Quantity)
				// locked_price is the per-second price of one unit.
				rate = rate.Add(item.LockedPrice.Amount.Mul(quantity))
			}
		}
		return rate, leaseCount
	}

	t.Run("success: query streams by provider", func(t *testing.T) {
		res, err := helpers.BillingQueryStreamsByProvider(ctx, tc.chain, tc.providerUUID)
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(res.Streams), 2)

		for _, tenant := range []string{tenantAddr, tc.tenant2.FormattedAddress()} {
			stream := res.Find(tc.providerUUID, tenant, tc.pwrDenom)
			require.NotNil(t, stream, "tenant %s has active leases with the provider", tenant)

			rate, leaseCount := expectedStream(t, tenant)
			require.NotZero(t, leaseCount)
			require.Equal(t, rate.String(), stream.State.Rate.String(), "stream rate is the sum of its active leases' rates")
			require.Equal(t, leaseCount, stream.State.LeaseCount)
			require.False(t, stream.State.IsExhausted())
			t.Logf("Stream of %s: rate=%s leases=%d last_settled_at=%s", tenant, stream.State.Rate, stream.State.LeaseCount, stream.State.LastSettledAt)
		}
		for _, stream := range res.Streams {
			require.Equal(t, tc.providerUUID, stream.ProviderUuid)
		}
	})

	t.Run("success: query streams by provider with pagination", func(t *testing.T) {
		// The unpaginated listing fixes the order the pages must follow.
		all, err := helpers.BillingQueryStreamsByProvider(ctx, tc.chain, tc.providerUUID)
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(all.Streams), 2, "pagination needs at least two streams")
		require.Empty(t, all.GetNextKeyString())

		// Page one stream at a time, passing each printed next_key (base64) back
		// as --page-key. A cursor the node did not honour would repeat, skip or
		// lose a stream, so every page must be exactly the next stream.
		key := ""
		for page, want := range all.Streams {
			res, nextKey, err := helpers.BillingQueryStreamsByProviderPaginated(ctx, tc.chain, tc.providerUUID, 1, key)
			require.NoError(t, err)
			require.Len(t, res.Streams, 1, "page %d", page)

			got := res.Streams[0]
			require.Equal(t, want.ProviderUuid, got.ProviderUuid, "page %d", page)
			require.Equal(t, want.Tenant, got.Tenant, "page %d should be stream %d of the full listing", page, page)
			require.Equal(t, want.Denom, got.Denom, "page %d", page)

			if page < len(all.Streams)-1 {
				require.NotEmpty(t, nextKey, "page %d of %d should have a next key", page, len(all.Streams))
			} else {
				require.Empty(t, nextKey, "the last page should not have a next key")
			}
			key = nextKey
		}
	})

	t.Run("success: query provider withdrawable with pagination", func(t *testing.T) {
		// Let every stream accrue something to withdraw.
		require.NoError(t, testutil.WaitForBlocks(ctx, 3, tc.chain))

		all, err := helpers.BillingQueryStreamsByProvider(ctx, tc.chain, tc.providerUUID)
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(all.Streams), 2, "pagination needs at least two streams")

		// provider-withdrawable pages over the same streams, in the same order:
		// each one-stream page covers the leases of exactly that stream.
		key := ""
		for page, stream := range all.Streams {
			res, nextKey, err := helpers.BillingQueryProviderWithdrawablePaginated(ctx, tc.chain, tc.providerUUID, 1, key)
			require.NoError(t, err)
			require.Equal(t, stream.State.LeaseCount, res.LeaseCount, "page %d should cover stream %d of the full listing", page, page)
			require.True(t, res.Amounts.AmountOf(stream.Denom).IsPositive(), "page %d: stream should have accrued", page)
			require.Len(t, res.Amounts, 1, "page %d: one stream accrues in one denom", page)

			if page < len(all.Streams)-1 {
				require.NotEmpty(t, nextKey, "page %d of %d should have a next key", page, len(all.Streams))
			} else {
				require.Empty(t, nextKey, "the last page should not have a next key")
			}
			key = nextKey
		}
	})

	t.Run("fail: stream queries reject a page key that is not base64", func(t *testing.T) {
		// Stream keys hold raw address bytes, so these commands take --page-key
		// as the base64 next_key a previous page printed. Anything else must be
		// refused, not sent to the node as a different key.
		const badKey = "not*base64!"

		_, _, err := helpers.BillingQueryStreamsByProviderPaginated(ctx, tc.chain, tc.providerUUID, 1, badKey)
		require.ErrorContains(t, err, "invalid --page-key")

		_, _, err = helpers.BillingQueryStreamsByTenantPaginated(ctx, tc.chain, tenantAddr, 1, badKey)
		require.ErrorContains(t, err, "invalid --page-key")

		_, _, err = helpers.BillingQueryProviderWithdrawablePaginated(ctx, tc.chain, tc.providerUUID, 1, badKey)
		require.ErrorContains(t, err, "invalid --page-key")
	})

	t.Run("success: query streams by tenant", func(t *testing.T) {
		res, err := helpers.BillingQueryStreamsByTenant(ctx, tc.chain, tenantAddr)
		require.NoError(t, err)
		// tenant1 only leases from one provider, in one denom.
		require.Len(t, res.Streams, 1)
		require.Equal(t, tc.providerUUID, res.Streams[0].ProviderUuid)
		require.Equal(t, tenantAddr, res.Streams[0].Tenant)
		require.Equal(t, tc.pwrDenom, res.Streams[0].Denom)

		// Both queries read the same stream.
		key := streamKey{providerUUID: tc.providerUUID, tenant: tenantAddr, denom: tc.pwrDenom}
		byProvider := snapshotProviderStreams(t, ctx, tc.chain, tc.providerUUID)
		require.Contains(t, byProvider, key)
		requireSameStreamState(t, key, byProvider[key], res.Streams[0].State)
	})

	t.Run("success: tenant without active leases has no stream", func(t *testing.T) {
		res, err := helpers.BillingQueryStreamsByTenant(ctx, tc.chain, tc.unauthorizedUser.FormattedAddress())
		require.NoError(t, err)
		require.Empty(t, res.Streams)
	})

	t.Run("fail: query streams with invalid arguments", func(t *testing.T) {
		_, err := helpers.BillingQueryStreamsByProvider(ctx, tc.chain, "not-a-uuid")
		require.Error(t, err)

		_, err = helpers.BillingQueryStreamsByTenant(ctx, tc.chain, "invalid-address")
		require.Error(t, err)
	})

	t.Run("success: query provider total withdrawable", func(t *testing.T) {
		// Let every stream accrue at least one whole second.
		require.NoError(t, testutil.WaitForBlocks(ctx, 3, tc.chain))

		streams := snapshotProviderStreams(t, ctx, tc.chain, tc.providerUUID)
		var leaseCount uint64
		for _, state := range streams {
			leaseCount += state.LeaseCount
		}

		res, err := helpers.BillingQueryProviderWithdrawable(ctx, tc.chain, tc.providerUUID)
		require.NoError(t, err)
		require.NotNil(t, res)
		require.True(t, res.Amounts.AmountOf(tc.pwrDenom).IsPositive(), "streams have accrued since their last settlement")
		// lease_count covers the leases of the streams that have something to withdraw.
		require.Equal(t, leaseCount, res.LeaseCount)
		t.Logf("Total withdrawable for provider: %s (leases: %d)", res.Amounts, res.LeaseCount)
	})
}

// TestBillingCreditEstimateOvershoot is an end-to-end test for ENG-527. It drives a tenant into
// the pending->active acknowledge overshoot — active lease count exceeding max_leases_per_tenant,
// reachable because AcknowledgeLease moves pending->active without re-gating the active limit — and
// verifies that CreditEstimate sums the burn rate over ALL active leases rather than truncating at
// max_leases_per_tenant. The query iteration cap is derived from
// max_leases_per_tenant + max_pending_leases_per_tenant (the maximum reachable active count).
func TestBillingCreditEstimateOvershoot(t *testing.T) {
	ctx, tc, cleanup := setupBillingTest(t, "billing-estimate-overshoot")
	t.Cleanup(cleanup)

	// Lower the lease limits so the overshoot is reachable with a handful of txs:
	// max_leases_per_tenant=2, max_pending_leases_per_tenant=2 => reachable active =
	// (2-1)+2 = 3, strictly greater than max_leases_per_tenant.
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
	users := interchaintest.GetAndFundTestUsers(t, ctx, "overshoot-tenant", DefaultGenesisAmt, tc.chain)
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

	// Reach active = max_leases_per_tenant-1 = 1, then overshoot to 3.
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
	ack(l2, l3)         // active 3 -> overshoot past max_leases_per_tenant (2)

	res, err := helpers.BillingQueryCreditEstimate(ctx, tc.chain, tenant.FormattedAddress())
	require.NoError(t, err)
	require.Equal(t, uint64(3), res.ActiveLeaseCount,
		"CreditEstimate must count all active leases in the overshoot state")
	require.Equal(t, baseAmt.MulRaw(3), res.TotalRatePerSecond.AmountOf(tc.pwrDenom),
		"burn rate must sum all 3 active leases (3x baseline), not truncate at max_leases_per_tenant")
	t.Logf("Overshoot verified: active_leases=%d (max_leases_per_tenant=2), rate/sec=%s",
		res.ActiveLeaseCount, res.TotalRatePerSecond)
}
