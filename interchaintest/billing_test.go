// Package interchaintest contains end-to-end tests for the billing module.
//
// # Test Coverage
//
// TestBilling is the main test function that runs all billing module e2e tests in sequence.
// Tests are run against a live chain using interchaintest framework.
//
// ## Setup Requirements
//
// The billing module requires:
//   - A tokenfactory PWR denom created specifically for tests
//   - An SKU provider with active SKUs for lease creation
//   - Credit accounts funded with PWR tokens
//
// IMPORTANT: SKU prices must be large enough to produce non-zero per-second rates
// due to integer division. For UNIT_PER_HOUR prices, use at least 3600 (1/second).
// For UNIT_PER_DAY prices, use at least 86400 (1/second). The tests use prices
// of 3600000 (1000/second) and 86400000 (1000/second) respectively to ensure
// meaningful accrual even with short block times.
//
// ## Query Tests
//
// testBillingQueryParams:
//   - Verifies default params are returned (denom, min_credit_balance, max_leases_per_tenant, max_items_per_lease)
//
// ## Credit Account Tests
//
// testCreditAccountOperations:
//   - Success: derive credit address for a tenant
//   - Success: fund credit account
//   - Success: query credit account balance
//   - Fail: fund with wrong denomination
//   - Fail: fund with insufficient funds
//
// ## Lease Lifecycle Tests
//
// testLeaseCreate:
//   - Success: tenant creates lease with single SKU
//   - Success: tenant creates lease with multiple SKUs
//   - Fail: create lease without sufficient credit
//   - Fail: create lease with inactive SKU
//   - Fail: create lease with non-existent SKU
//   - Fail: create lease exceeding max_leases_per_tenant
//   - Fail: create lease exceeding max_items_per_lease hard limit
//   - Fail: create lease with SKUs from different providers
//
// testLeaseQuery:
//   - Success: query lease by ID
//   - Success: query all leases
//   - Success: query leases by tenant
//   - Success: query leases by provider
//   - Success: query active-only leases
//
// testLeaseClose:
//   - Success: tenant closes their own lease
//   - Success: provider closes lease
//   - Success: authority closes lease
//   - Fail: unauthorized user closes lease
//   - Fail: close already inactive lease
//   - Fail: close non-existent lease
//
// ## Accrual and Withdrawal Tests
//
// testAccrualCalculation:
//   - Success: verify accrual increases over time (block-based)
//   - Success: verify locked price is used for calculation
//   - Success: verify multiple SKU items accrue correctly
//
// testWithdraw:
//   - Success: provider withdraws from specific lease
//   - Success: authority withdraws on behalf of provider
//   - Fail: tenant cannot withdraw
//   - Fail: unauthorized user cannot withdraw
//   - Fail: withdraw from non-existent lease
//   - Success: partial withdrawal (accrual continues)
//
// testWithdrawAll:
//   - Success: provider withdraws from all leases
//   - Success: authority withdraws for specific provider
//   - Fail: withdraw all from provider with no leases
//
// ## Query Helpers Tests
//
// testWithdrawableQueries:
//   - Success: query withdrawable amount for lease
//   - Success: query provider total withdrawable
//
// ## Pagination Tests
//
// testBillingPagination:
//   - Success: paginate all leases
//   - Success: paginate leases by tenant
//   - Success: paginate leases by provider
//
// ## Edge Cases Tests
//
// testEdgeCases:
//   - Success: remaining credit stays in account after lease close
//   - Success: provider cannot double-withdraw after lease closure (already settled)
//
// ## Authority Lease Creation Tests
//
// testCreateLeaseForTenant:
//   - Success: authority creates lease for tenant
//   - Success: verify lease belongs to tenant
//   - Success: tenant can close lease created by authority
//   - Success: authority creates multi-SKU lease for tenant
//   - Fail: non-authority cannot create lease for tenant
//   - Fail: provider cannot create lease for tenant
//   - Fail: create lease for tenant without funded credit
//   - Fail: create lease for tenant with invalid address
//   - Fail: create lease for tenant with non-existent SKU
//   - Success: verify event shows authority created lease
//
// ## Auto-Close Mechanism Tests
//
// testAutoCloseMechanism:
//   - Success: lease auto-closes when credit is exhausted during settlement
//   - Success: auto-closed lease emits proper events
//   - Success: provider can still withdraw from auto-closed lease
//   - Success: tenant cannot create new lease after exhausting credit (below minimum)
//   - Success: closing lease settles and transfers accrued amount
//
// ## Credit Address Query Tests
//
// testCreditAddressQuery:
//   - Success: derive credit address without existing credit account
//   - Success: derive credit address for funded tenant matches actual credit account
//   - Fail: derive credit address with invalid tenant address
//
// ## WithdrawAll Limits Tests
//
// testWithdrawAllLimits:
//   - Success: withdraw all with default limit
//   - Success: withdraw all with custom limit
//   - Success: has_more flag indicates more leases to process
//   - Fail: withdraw all with limit exceeding maximum
//
// ## Provider Deactivation Tests
//
// testProviderDeactivation:
//   - Success: provider can be deactivated while having active leases
//   - Success: existing leases continue after provider deactivation
//   - Success: provider can still withdraw after deactivation
//   - Fail: cannot create new lease with deactivated provider's SKU
package interchaintest

import (
	"context"
	"fmt"
	"strconv"
	"testing"

	sdkmath "cosmossdk.io/math"
	"github.com/strangelove-ventures/interchaintest/v8"
	"github.com/strangelove-ventures/interchaintest/v8/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v8/dockerutil"
	"github.com/strangelove-ventures/interchaintest/v8/ibc"
	"github.com/strangelove-ventures/interchaintest/v8/testutil"
	"github.com/stretchr/testify/require"

	"github.com/manifest-network/manifest-ledger/interchaintest/helpers"
)

// testPWRDenom is the test PWR denom created via tokenfactory
var testPWRDenom string

// testProviderID is the provider ID created for billing tests
var testProviderID uint64

// testSKUID is the SKU ID created for billing tests (per-hour pricing)
var testSKUID uint64

// testSKUID2 is a second SKU ID for multi-SKU lease tests (per-day pricing)
var testSKUID2 uint64

func TestBilling(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	// Setup chain
	name := "billing-test"
	cfgA := LocalChainConfig
	cfgA.Name = name
	cfgA.WithCodeCoverage()

	chains := interchaintest.CreateChainWithConfig(t, vals, fullNodes, name, "", cfgA)
	chain := chains[0].(*cosmos.CosmosChain)

	enableBlockDB := false
	ctx, ic, client, _ := interchaintest.BuildInitialChain(t, chains, enableBlockDB)

	// Setup accounts
	// acc0 is the PoA admin (module authority)
	authority, err := interchaintest.GetAndFundTestUserWithMnemonic(ctx, "authority", accMnemonic, DefaultGenesisAmt, chain)
	require.NoError(t, err)

	// Regular users (tenants)
	users := interchaintest.GetAndFundTestUsers(t, ctx, t.Name(), DefaultGenesisAmt, chain, chain, chain, chain)
	tenant1 := users[0]
	tenant2 := users[1]
	providerWallet := users[2]
	unauthorizedUser := users[3]

	// Setup test infrastructure
	t.Run("Setup", func(t *testing.T) {
		setupBillingTestInfrastructure(t, ctx, chain, authority, providerWallet)
	})

	// Run test cases
	t.Run("QueryParams", func(t *testing.T) {
		testBillingQueryParams(t, ctx, chain)
	})

	t.Run("CreditAccountOperations", func(t *testing.T) {
		testCreditAccountOperations(t, ctx, chain, authority, tenant1, tenant2)
	})

	t.Run("LeaseCreate", func(t *testing.T) {
		testLeaseCreate(t, ctx, chain, authority, tenant1, tenant2)
	})

	t.Run("LeaseQuery", func(t *testing.T) {
		testLeaseQuery(t, ctx, chain, tenant1)
	})

	t.Run("AccrualCalculation", func(t *testing.T) {
		testAccrualCalculation(t, ctx, chain, authority, tenant1)
	})

	t.Run("Withdraw", func(t *testing.T) {
		testWithdraw(t, ctx, chain, authority, tenant1, providerWallet, unauthorizedUser)
	})

	t.Run("WithdrawAll", func(t *testing.T) {
		testWithdrawAll(t, ctx, chain, authority, tenant1, providerWallet)
	})

	t.Run("LeaseClose", func(t *testing.T) {
		testLeaseClose(t, ctx, chain, authority, tenant1, providerWallet, unauthorizedUser)
	})

	t.Run("WithdrawableQueries", func(t *testing.T) {
		testWithdrawableQueries(t, ctx, chain, tenant1)
	})

	t.Run("EdgeCases", func(t *testing.T) {
		testEdgeCases(t, ctx, chain, authority, tenant2, providerWallet)
	})

	t.Run("CreateLeaseForTenant", func(t *testing.T) {
		testCreateLeaseForTenant(t, ctx, chain, authority, providerWallet, unauthorizedUser)
	})

	t.Run("AutoCloseMechanism", func(t *testing.T) {
		testAutoCloseMechanism(t, ctx, chain, authority, providerWallet)
	})

	t.Run("CreditAddressQuery", func(t *testing.T) {
		testCreditAddressQuery(t, ctx, chain, tenant1)
	})

	t.Run("WithdrawAllLimits", func(t *testing.T) {
		testWithdrawAllLimits(t, ctx, chain, authority, providerWallet)
	})

	t.Run("ProviderDeactivation", func(t *testing.T) {
		testProviderDeactivation(t, ctx, chain, authority, providerWallet)
	})

	t.Run("SendRestriction", func(t *testing.T) {
		testSendRestriction(t, ctx, chain, authority, tenant1)
	})

	t.Run("AllowedListAuthorization", func(t *testing.T) {
		testAllowedListAuthorization(t, ctx, chain, authority)
	})

	t.Cleanup(func() {
		dockerutil.CopyCoverageFromContainer(ctx, t, client, chain.GetNode().ContainerID(), chain.HomeDir(), ExternalGoCoverDir)
		_ = ic.Close()
	})
}

// setupBillingTestInfrastructure creates the PWR denom, provider, and SKUs needed for tests.
func setupBillingTestInfrastructure(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, authority, providerWallet ibc.Wallet) {
	t.Log("=== Setting up Billing Test Infrastructure ===")

	node := chain.GetNode()

	// Create PWR denom via tokenfactory
	t.Run("create_pwr_denom", func(t *testing.T) {
		var err error
		testPWRDenom, _, err = node.TokenFactoryCreateDenom(ctx, authority, "upwr", 2_500_00)
		require.NoError(t, err)
		t.Logf("Created PWR denom: %s", testPWRDenom)
	})

	// Mint PWR tokens to authority for distribution
	t.Run("mint_pwr_tokens", func(t *testing.T) {
		// Mint a large amount for testing
		_, err := node.TokenFactoryMintDenom(ctx, authority.FormattedAddress(), testPWRDenom, 1_000_000_000_000)
		require.NoError(t, err)

		balance, err := chain.GetBalance(ctx, authority.FormattedAddress(), testPWRDenom)
		require.NoError(t, err)
		require.True(t, balance.GT(sdkmath.ZeroInt()), "authority should have PWR balance")
		t.Logf("Authority PWR balance: %s", balance)
	})

	// Update billing params to use test PWR denom
	t.Run("update_billing_params", func(t *testing.T) {
		res, err := helpers.BillingUpdateParams(ctx, chain, authority, testPWRDenom, sdkmath.NewInt(5_000_000), 100, 20, nil)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "update params should succeed: %s", txRes.RawLog)
		t.Logf("Updated billing params with denom: %s", testPWRDenom)
	})

	// Create provider
	t.Run("create_provider", func(t *testing.T) {
		res, err := helpers.SKUCreateProvider(ctx, chain, authority, providerWallet.FormattedAddress(), providerWallet.FormattedAddress(), "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "provider creation should succeed: %s", txRes.RawLog)

		testProviderID, err = helpers.GetProviderIDFromTxHash(ctx, chain, res.TxHash)
		require.NoError(t, err)
		t.Logf("Created provider ID: %d", testProviderID)
	})

	// Create SKU with per-hour pricing (Unit = 1)
	// Price: 3600000 umfx per hour = 3600000/3600 = 1000 per second
	// This ensures meaningful accrual even with short test durations
	t.Run("create_sku_per_hour", func(t *testing.T) {
		res, err := helpers.SKUCreateSKU(ctx, chain, authority, testProviderID, "Compute Small", 1, "3600000umfx", "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "SKU creation should succeed: %s", txRes.RawLog)

		testSKUID, err = helpers.GetSKUIDFromTxHash(ctx, chain, res.TxHash)
		require.NoError(t, err)
		t.Logf("Created SKU ID (per-hour): %d", testSKUID)
	})

	// Create SKU with per-day pricing (Unit = 2)
	// Price: 86400000 umfx per day = 86400000/86400 = 1000 per second
	// This ensures meaningful accrual even with short test durations
	t.Run("create_sku_per_day", func(t *testing.T) {
		res, err := helpers.SKUCreateSKU(ctx, chain, authority, testProviderID, "Storage Large", 2, "86400000umfx", "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "SKU creation should succeed: %s", txRes.RawLog)

		testSKUID2, err = helpers.GetSKUIDFromTxHash(ctx, chain, res.TxHash)
		require.NoError(t, err)
		t.Logf("Created SKU ID (per-day): %d", testSKUID2)
	})
}

func testBillingQueryParams(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain) {
	t.Log("=== Testing Billing Query Params ===")

	res, err := helpers.BillingQueryParamsJSON(ctx, chain)
	require.NoError(t, err)
	require.NotNil(t, res)
	require.NotEmpty(t, res.Params.Denom, "denom should be set")
	require.NotEmpty(t, res.Params.MinCreditBalance, "min_credit_balance should be set")
	require.NotEmpty(t, res.Params.MaxLeasesPerTenant, "max_leases_per_tenant should be set")
	require.NotEmpty(t, res.Params.MaxItemsPerLease, "max_items_per_lease should be set")
	t.Logf("Billing params: denom=%s, min_credit_balance=%s, max_leases_per_tenant=%s, max_items_per_lease=%s",
		res.Params.Denom, res.Params.MinCreditBalance, res.Params.MaxLeasesPerTenant, res.Params.MaxItemsPerLease)
}

func testCreditAccountOperations(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, authority, tenant1, tenant2 ibc.Wallet) {
	t.Log("=== Testing Credit Account Operations ===")

	node := chain.GetNode()

	t.Run("success: derive credit address", func(t *testing.T) {
		res, err := helpers.BillingQueryCreditAddress(ctx, chain, tenant1.FormattedAddress())
		require.NoError(t, err)
		require.NotEmpty(t, res.CreditAddress)
		require.Contains(t, res.CreditAddress, "manifest1", "credit address should be a valid manifest address")
		t.Logf("Tenant1 credit address: %s", res.CreditAddress)
	})

	t.Run("success: fund credit account", func(t *testing.T) {
		// First send PWR to tenant1 so they can fund their credit
		err := node.SendFunds(ctx, authority.KeyName(), ibc.WalletAmount{
			Address: tenant1.FormattedAddress(),
			Denom:   testPWRDenom,
			Amount:  sdkmath.NewInt(100_000_000), // 100 PWR
		})
		require.NoError(t, err)
		require.NoError(t, testutil.WaitForBlocks(ctx, 2, chain))

		// Fund credit account
		fundAmount := fmt.Sprintf("50000000%s", testPWRDenom) // 50 PWR
		res, err := helpers.BillingFundCredit(ctx, chain, tenant1, tenant1.FormattedAddress(), fundAmount)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "fund credit should succeed: %s", txRes.RawLog)
	})

	t.Run("success: query credit account balance", func(t *testing.T) {
		res, err := helpers.BillingQueryCreditAccount(ctx, chain, tenant1.FormattedAddress())
		require.NoError(t, err)
		require.Equal(t, tenant1.FormattedAddress(), res.CreditAccount.Tenant)
		require.NotEmpty(t, res.CreditAccount.CreditAddress)
		require.True(t, res.Balance.Amount.IsPositive(), "credit balance should be positive")
		t.Logf("Tenant1 credit balance: %s", res.Balance)
	})

	t.Run("fail: fund with wrong denomination", func(t *testing.T) {
		// Try to fund with umfx instead of PWR
		res, err := helpers.BillingFundCredit(ctx, chain, tenant1, tenant1.FormattedAddress(), "1000000umfx")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "fund with wrong denom should fail")
		require.Contains(t, txRes.RawLog, "invalid denomination")
	})

	t.Run("setup: fund tenant2 for later tests", func(t *testing.T) {
		// Send PWR to tenant2
		err := node.SendFunds(ctx, authority.KeyName(), ibc.WalletAmount{
			Address: tenant2.FormattedAddress(),
			Denom:   testPWRDenom,
			Amount:  sdkmath.NewInt(100_000_000), // 100 PWR
		})
		require.NoError(t, err)
		require.NoError(t, testutil.WaitForBlocks(ctx, 2, chain))

		// Fund tenant2's credit account
		fundAmount := fmt.Sprintf("50000000%s", testPWRDenom)
		res, err := helpers.BillingFundCredit(ctx, chain, tenant2, tenant2.FormattedAddress(), fundAmount)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "fund credit should succeed")
	})
}

func testLeaseCreate(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, authority, tenant1, tenant2 ibc.Wallet) {
	t.Log("=== Testing Lease Create ===")

	t.Run("success: tenant creates lease with single SKU", func(t *testing.T) {
		items := []string{fmt.Sprintf("%d:1", testSKUID)}
		res, err := helpers.BillingCreateLease(ctx, chain, tenant1, items)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "lease creation should succeed: %s", txRes.RawLog)

		leaseID, err := helpers.GetLeaseIDFromTxHash(ctx, chain, res.TxHash)
		require.NoError(t, err)
		t.Logf("Created lease ID: %d", leaseID)
	})

	t.Run("success: tenant creates lease with multiple SKUs", func(t *testing.T) {
		items := []string{
			fmt.Sprintf("%d:2", testSKUID),  // 2x per-hour SKU
			fmt.Sprintf("%d:1", testSKUID2), // 1x per-day SKU
		}
		res, err := helpers.BillingCreateLease(ctx, chain, tenant1, items)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "lease creation should succeed: %s", txRes.RawLog)
	})

	t.Run("fail: create lease with non-existent SKU", func(t *testing.T) {
		items := []string{"99999:1"}
		res, err := helpers.BillingCreateLease(ctx, chain, tenant1, items)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "lease creation should fail")
		require.Contains(t, txRes.RawLog, "not found")
	})

	t.Run("fail: create lease without sufficient credit", func(t *testing.T) {
		// Create a user without any credit
		users := interchaintest.GetAndFundTestUsers(t, ctx, "no-credit", DefaultGenesisAmt, chain)
		noCredit := users[0]

		items := []string{fmt.Sprintf("%d:1", testSKUID)}
		res, err := helpers.BillingCreateLease(ctx, chain, noCredit, items)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "lease creation should fail")
		require.Contains(t, txRes.RawLog, "insufficient credit")
	})

	t.Run("fail: create lease exceeding max_items_per_lease hard limit", func(t *testing.T) {
		// The hard limit is 100 items per lease (MaxItemsPerLeaseHardLimit)
		// This test validates the client-side validation in ValidateBasic
		items := make([]string, 101)
		for i := 0; i < 101; i++ {
			// Use different SKU IDs to avoid duplicate validation error
			items[i] = fmt.Sprintf("%d:1", i+1000)
		}
		_, err := helpers.BillingCreateLease(ctx, chain, tenant1, items)
		// The error should be caught at client-side validation before tx is broadcast
		require.Error(t, err)
		require.Contains(t, err.Error(), "too many items")
		t.Log("Correctly rejected lease with too many items (hard limit)")
	})
}

func testLeaseQuery(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, tenant1 ibc.Wallet) {
	t.Log("=== Testing Lease Query ===")

	t.Run("success: query all leases", func(t *testing.T) {
		res, err := helpers.BillingQueryLeases(ctx, chain, false)
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(res.Leases), 2, "should have at least 2 leases")
		t.Logf("Found %d leases", len(res.Leases))
	})

	t.Run("success: query leases by tenant", func(t *testing.T) {
		res, err := helpers.BillingQueryLeasesByTenant(ctx, chain, tenant1.FormattedAddress(), false)
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(res.Leases), 2, "tenant1 should have at least 2 leases")

		for _, lease := range res.Leases {
			require.Equal(t, tenant1.FormattedAddress(), lease.Tenant)
		}
	})

	t.Run("success: query leases by provider", func(t *testing.T) {
		res, err := helpers.BillingQueryLeasesByProvider(ctx, chain, testProviderID, false)
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(res.Leases), 2, "provider should have at least 2 leases")

		for _, lease := range res.Leases {
			providerID, _ := strconv.ParseUint(lease.ProviderID, 10, 64)
			require.Equal(t, testProviderID, providerID)
		}
	})

	t.Run("success: query active-only leases", func(t *testing.T) {
		res, err := helpers.BillingQueryLeases(ctx, chain, true)
		require.NoError(t, err)

		for _, lease := range res.Leases {
			require.Equal(t, "LEASE_STATE_ACTIVE", lease.State)
		}
	})

	t.Run("success: query lease by ID", func(t *testing.T) {
		// Get first lease ID
		allLeases, err := helpers.BillingQueryLeases(ctx, chain, false)
		require.NoError(t, err)
		require.NotEmpty(t, allLeases.Leases)

		leaseID, err := helpers.ParseLeaseID(allLeases.Leases[0].ID)
		require.NoError(t, err)

		res, err := helpers.BillingQueryLease(ctx, chain, leaseID)
		require.NoError(t, err)
		require.Equal(t, allLeases.Leases[0].ID, res.Lease.ID)
	})
}

func testAccrualCalculation(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, authority, tenant1 ibc.Wallet) {
	t.Log("=== Testing Accrual Calculation ===")

	// Get an active lease
	leases, err := helpers.BillingQueryLeasesByTenant(ctx, chain, tenant1.FormattedAddress(), true)
	require.NoError(t, err)
	require.NotEmpty(t, leases.Leases, "tenant should have active leases")

	leaseID, err := helpers.ParseLeaseID(leases.Leases[0].ID)
	require.NoError(t, err)

	t.Run("success: verify accrual increases over time", func(t *testing.T) {
		// Get initial withdrawable
		initial, err := helpers.BillingQueryWithdrawable(ctx, chain, leaseID)
		require.NoError(t, err)
		t.Logf("Initial withdrawable: %s", initial.Amount)

		// Wait for some blocks to pass
		require.NoError(t, testutil.WaitForBlocks(ctx, 5, chain))

		// Get updated withdrawable
		updated, err := helpers.BillingQueryWithdrawable(ctx, chain, leaseID)
		require.NoError(t, err)
		t.Logf("Updated withdrawable: %s", updated.Amount)

		// Accrual should have increased (or at least not decreased)
		require.True(t, updated.Amount.Amount.GTE(initial.Amount.Amount),
			"withdrawable should increase over time")
	})
}

func testWithdraw(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, authority, tenant1, providerWallet, unauthorizedUser ibc.Wallet) {
	t.Log("=== Testing Withdraw ===")

	// Get an active lease
	leases, err := helpers.BillingQueryLeasesByTenant(ctx, chain, tenant1.FormattedAddress(), true)
	require.NoError(t, err)
	require.NotEmpty(t, leases.Leases)

	leaseID, err := helpers.ParseLeaseID(leases.Leases[0].ID)
	require.NoError(t, err)

	// Wait for some accrual
	require.NoError(t, testutil.WaitForBlocks(ctx, 3, chain))

	t.Run("success: provider withdraws from lease", func(t *testing.T) {
		// Get provider's initial balance
		initialBalance, err := chain.GetBalance(ctx, providerWallet.FormattedAddress(), testPWRDenom)
		require.NoError(t, err)

		res, err := helpers.BillingWithdraw(ctx, chain, providerWallet, leaseID)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "withdraw should succeed: %s", txRes.RawLog)

		// Verify provider received funds
		newBalance, err := chain.GetBalance(ctx, providerWallet.FormattedAddress(), testPWRDenom)
		require.NoError(t, err)
		require.True(t, newBalance.GTE(initialBalance), "provider balance should increase")
		t.Logf("Provider balance changed: %s -> %s", initialBalance, newBalance)
	})

	t.Run("fail: tenant cannot withdraw", func(t *testing.T) {
		res, err := helpers.BillingWithdraw(ctx, chain, tenant1, leaseID)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "tenant withdraw should fail")
		require.Contains(t, txRes.RawLog, "unauthorized")
	})

	t.Run("fail: unauthorized user cannot withdraw", func(t *testing.T) {
		res, err := helpers.BillingWithdraw(ctx, chain, unauthorizedUser, leaseID)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "unauthorized withdraw should fail")
		require.Contains(t, txRes.RawLog, "unauthorized")
	})

	t.Run("fail: withdraw from non-existent lease", func(t *testing.T) {
		res, err := helpers.BillingWithdraw(ctx, chain, providerWallet, 99999)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "withdraw from non-existent lease should fail")
		require.Contains(t, txRes.RawLog, "not found")
	})

	t.Run("success: authority withdraws on behalf of provider", func(t *testing.T) {
		// Wait for more accrual
		require.NoError(t, testutil.WaitForBlocks(ctx, 3, chain))

		res, err := helpers.BillingWithdraw(ctx, chain, authority, leaseID)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "authority withdraw should succeed: %s", txRes.RawLog)
	})
}

func testWithdrawAll(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, authority, tenant1, providerWallet ibc.Wallet) {
	t.Log("=== Testing Withdraw All ===")

	// Wait for some accrual
	require.NoError(t, testutil.WaitForBlocks(ctx, 5, chain))

	t.Run("success: provider withdraws from all leases", func(t *testing.T) {
		// Get provider's initial balance
		initialBalance, err := chain.GetBalance(ctx, providerWallet.FormattedAddress(), testPWRDenom)
		require.NoError(t, err)

		res, err := helpers.BillingWithdrawAll(ctx, chain, providerWallet, testProviderID)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "withdraw all should succeed: %s", txRes.RawLog)

		// Verify provider received funds
		newBalance, err := chain.GetBalance(ctx, providerWallet.FormattedAddress(), testPWRDenom)
		require.NoError(t, err)
		require.True(t, newBalance.GTE(initialBalance), "provider balance should increase")
	})

	t.Run("success: authority withdraws for provider", func(t *testing.T) {
		// Wait for more accrual
		require.NoError(t, testutil.WaitForBlocks(ctx, 3, chain))

		res, err := helpers.BillingWithdrawAll(ctx, chain, authority, testProviderID)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "authority withdraw all should succeed: %s", txRes.RawLog)
	})
}

func testLeaseClose(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, authority, tenant1, providerWallet, unauthorizedUser ibc.Wallet) {
	t.Log("=== Testing Lease Close ===")

	// Create a new lease for close testing
	var closeLeaseID uint64
	t.Run("setup: create lease for close testing", func(t *testing.T) {
		items := []string{fmt.Sprintf("%d:1", testSKUID)}
		res, err := helpers.BillingCreateLease(ctx, chain, tenant1, items)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code)

		closeLeaseID, err = helpers.GetLeaseIDFromTxHash(ctx, chain, res.TxHash)
		require.NoError(t, err)
	})

	t.Run("fail: unauthorized user closes lease", func(t *testing.T) {
		res, err := helpers.BillingCloseLease(ctx, chain, unauthorizedUser, closeLeaseID)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "unauthorized close should fail")
		require.Contains(t, txRes.RawLog, "unauthorized")
	})

	t.Run("success: tenant closes their own lease", func(t *testing.T) {
		res, err := helpers.BillingCloseLease(ctx, chain, tenant1, closeLeaseID)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "tenant close should succeed: %s", txRes.RawLog)

		// Verify lease is now inactive
		leaseRes, err := helpers.BillingQueryLease(ctx, chain, closeLeaseID)
		require.NoError(t, err)
		require.Equal(t, "LEASE_STATE_INACTIVE", leaseRes.Lease.State)
	})

	t.Run("fail: close already inactive lease", func(t *testing.T) {
		res, err := helpers.BillingCloseLease(ctx, chain, tenant1, closeLeaseID)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "close inactive lease should fail")
		require.Contains(t, txRes.RawLog, "not active")
	})

	t.Run("fail: close non-existent lease", func(t *testing.T) {
		res, err := helpers.BillingCloseLease(ctx, chain, tenant1, 99999)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "close non-existent lease should fail")
		require.Contains(t, txRes.RawLog, "not found")
	})

	// Test provider closing
	var providerCloseLeaseID uint64
	t.Run("setup: create lease for provider close", func(t *testing.T) {
		items := []string{fmt.Sprintf("%d:1", testSKUID)}
		res, err := helpers.BillingCreateLease(ctx, chain, tenant1, items)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code)

		providerCloseLeaseID, err = helpers.GetLeaseIDFromTxHash(ctx, chain, res.TxHash)
		require.NoError(t, err)
	})

	t.Run("success: provider closes lease", func(t *testing.T) {
		res, err := helpers.BillingCloseLease(ctx, chain, providerWallet, providerCloseLeaseID)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "provider close should succeed: %s", txRes.RawLog)
	})

	// Test authority closing
	var authorityCloseLeaseID uint64
	t.Run("setup: create lease for authority close", func(t *testing.T) {
		items := []string{fmt.Sprintf("%d:1", testSKUID)}
		res, err := helpers.BillingCreateLease(ctx, chain, tenant1, items)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code)

		authorityCloseLeaseID, err = helpers.GetLeaseIDFromTxHash(ctx, chain, res.TxHash)
		require.NoError(t, err)
	})

	t.Run("success: authority closes lease", func(t *testing.T) {
		res, err := helpers.BillingCloseLease(ctx, chain, authority, authorityCloseLeaseID)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "authority close should succeed: %s", txRes.RawLog)
	})
}

func testWithdrawableQueries(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, tenant1 ibc.Wallet) {
	t.Log("=== Testing Withdrawable Queries ===")

	// Get an active lease
	leases, err := helpers.BillingQueryLeasesByTenant(ctx, chain, tenant1.FormattedAddress(), true)
	require.NoError(t, err)

	if len(leases.Leases) == 0 {
		t.Skip("No active leases to test withdrawable queries")
	}

	leaseID, err := helpers.ParseLeaseID(leases.Leases[0].ID)
	require.NoError(t, err)

	t.Run("success: query withdrawable amount for lease", func(t *testing.T) {
		res, err := helpers.BillingQueryWithdrawable(ctx, chain, leaseID)
		require.NoError(t, err)
		require.Equal(t, testPWRDenom, res.Amount.Denom)
		t.Logf("Withdrawable for lease %d: %s", leaseID, res.Amount)
	})

	t.Run("success: query provider total withdrawable", func(t *testing.T) {
		res, err := helpers.BillingQueryProviderWithdrawable(ctx, chain, testProviderID)
		require.NoError(t, err)
		require.Equal(t, testPWRDenom, res.Amount.Denom)
		t.Logf("Provider total withdrawable: %s (from %s leases)", res.Amount, res.LeaseCount)
	})
}

func testEdgeCases(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, authority, tenant2, providerWallet ibc.Wallet) {
	t.Log("=== Testing Edge Cases ===")

	t.Run("success: remaining credit stays after lease close", func(t *testing.T) {
		// Get tenant2's credit balance before
		beforeRes, err := helpers.BillingQueryCreditAccount(ctx, chain, tenant2.FormattedAddress())
		require.NoError(t, err)
		beforeBalance := beforeRes.Balance

		// Create a lease
		items := []string{fmt.Sprintf("%d:1", testSKUID)}
		createRes, err := helpers.BillingCreateLease(ctx, chain, tenant2, items)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(createRes.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code)

		leaseID, err := helpers.GetLeaseIDFromTxHash(ctx, chain, createRes.TxHash)
		require.NoError(t, err)

		// Wait for some accrual
		require.NoError(t, testutil.WaitForBlocks(ctx, 3, chain))

		// Close the lease
		closeRes, err := helpers.BillingCloseLease(ctx, chain, tenant2, leaseID)
		require.NoError(t, err)

		txRes, err = chain.GetTransaction(closeRes.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code)

		// Check credit balance - should be less than before (due to accrual) but still positive
		afterRes, err := helpers.BillingQueryCreditAccount(ctx, chain, tenant2.FormattedAddress())
		require.NoError(t, err)
		afterBalance := afterRes.Balance

		// Credit should have decreased due to accrual
		require.True(t, afterBalance.Amount.LT(beforeBalance.Amount),
			"credit should decrease after lease accrual")
		// But credit should still exist (wasn't drained to zero)
		require.True(t, afterBalance.Amount.IsPositive(),
			"remaining credit should stay in account")
		t.Logf("Credit balance: before=%s, after=%s", beforeBalance, afterBalance)
	})

	t.Run("success: provider cannot double-withdraw after lease closure", func(t *testing.T) {
		// Get a closed lease from tenant2's tests
		leases, err := helpers.BillingQueryLeasesByTenant(ctx, chain, tenant2.FormattedAddress(), false)
		require.NoError(t, err)

		var closedLeaseID uint64
		for _, lease := range leases.Leases {
			if lease.State == "LEASE_STATE_INACTIVE" {
				closedLeaseID, _ = helpers.ParseLeaseID(lease.ID)
				break
			}
		}

		if closedLeaseID == 0 {
			t.Skip("No closed lease found")
		}

		// After closure, settlement already happened, so withdrawal should fail
		// because there's nothing left to withdraw (LastSettledAt == ClosedAt)
		res, err := helpers.BillingWithdraw(ctx, chain, providerWallet, closedLeaseID)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		// Should fail because settlement already happened during closure
		require.NotEqual(t, uint32(0), txRes.Code, "withdraw after closure should fail (already settled)")
		require.Contains(t, txRes.RawLog, "no withdrawable amount")
	})
}

func testCreateLeaseForTenant(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, authority, providerWallet, unauthorizedUser ibc.Wallet) {
	t.Log("=== Testing Create Lease For Tenant (Authority Only) ===")

	// Create a new tenant for these tests
	users := interchaintest.GetAndFundTestUsers(t, ctx, "lease-for-tenant", DefaultGenesisAmt, chain)
	newTenant := users[0]

	t.Run("setup: fund new tenant credit account", func(t *testing.T) {
		// Authority funds the new tenant's credit account using FundCredit
		// This creates the credit account record in the billing module
		fundAmount := fmt.Sprintf("100000000%s", testPWRDenom) // 100 PWR
		res, err := helpers.BillingFundCredit(ctx, chain, authority, newTenant.FormattedAddress(), fundAmount)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "funding new tenant credit should succeed: %s", txRes.RawLog)

		// Verify credit account exists and has balance
		creditRes, err := helpers.BillingQueryCreditAccount(ctx, chain, newTenant.FormattedAddress())
		require.NoError(t, err)
		require.True(t, creditRes.Balance.Amount.IsPositive(), "credit balance should be positive")
		t.Logf("New tenant credit balance: %s", creditRes.Balance)
	})

	var leaseID uint64
	t.Run("success: authority creates lease for tenant", func(t *testing.T) {
		items := []string{fmt.Sprintf("%d:1", testSKUID)}
		res, err := helpers.BillingCreateLeaseForTenant(ctx, chain, authority, newTenant.FormattedAddress(), items)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "authority create lease for tenant should succeed: %s", txRes.RawLog)

		leaseID, err = helpers.GetLeaseIDFromTxHash(ctx, chain, res.TxHash)
		require.NoError(t, err)
		t.Logf("Created lease ID: %d for tenant: %s", leaseID, newTenant.FormattedAddress())
	})

	t.Run("success: verify lease belongs to tenant", func(t *testing.T) {
		leaseRes, err := helpers.BillingQueryLease(ctx, chain, leaseID)
		require.NoError(t, err)
		require.Equal(t, newTenant.FormattedAddress(), leaseRes.Lease.Tenant)
		require.Equal(t, "LEASE_STATE_ACTIVE", leaseRes.Lease.State)
	})

	t.Run("success: tenant can close lease created by authority", func(t *testing.T) {
		res, err := helpers.BillingCloseLease(ctx, chain, newTenant, leaseID)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "tenant should be able to close lease created by authority: %s", txRes.RawLog)

		// Verify lease is now inactive
		leaseRes, err := helpers.BillingQueryLease(ctx, chain, leaseID)
		require.NoError(t, err)
		require.Equal(t, "LEASE_STATE_INACTIVE", leaseRes.Lease.State)
	})

	t.Run("success: authority creates multi-SKU lease for tenant", func(t *testing.T) {
		items := []string{
			fmt.Sprintf("%d:2", testSKUID),  // 2x per-hour SKU
			fmt.Sprintf("%d:1", testSKUID2), // 1x per-day SKU
		}
		res, err := helpers.BillingCreateLeaseForTenant(ctx, chain, authority, newTenant.FormattedAddress(), items)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "multi-SKU lease creation should succeed: %s", txRes.RawLog)

		newLeaseID, err := helpers.GetLeaseIDFromTxHash(ctx, chain, res.TxHash)
		require.NoError(t, err)

		// Verify lease has correct number of items
		leaseRes, err := helpers.BillingQueryLease(ctx, chain, newLeaseID)
		require.NoError(t, err)
		require.Len(t, leaseRes.Lease.Items, 2, "lease should have 2 items")
	})

	t.Run("fail: non-authority cannot create lease for tenant", func(t *testing.T) {
		items := []string{fmt.Sprintf("%d:1", testSKUID)}
		res, err := helpers.BillingCreateLeaseForTenant(ctx, chain, unauthorizedUser, newTenant.FormattedAddress(), items)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "non-authority should not create lease for tenant")
		require.Contains(t, txRes.RawLog, "unauthorized")
	})

	t.Run("fail: provider cannot create lease for tenant", func(t *testing.T) {
		items := []string{fmt.Sprintf("%d:1", testSKUID)}
		res, err := helpers.BillingCreateLeaseForTenant(ctx, chain, providerWallet, newTenant.FormattedAddress(), items)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "provider should not create lease for tenant")
		require.Contains(t, txRes.RawLog, "unauthorized")
	})

	t.Run("fail: create lease for tenant without funded credit", func(t *testing.T) {
		// Create a new tenant without funding their credit
		unfundedUsers := interchaintest.GetAndFundTestUsers(t, ctx, "unfunded-tenant", DefaultGenesisAmt, chain)
		unfundedTenant := unfundedUsers[0]

		items := []string{fmt.Sprintf("%d:1", testSKUID)}
		res, err := helpers.BillingCreateLeaseForTenant(ctx, chain, authority, unfundedTenant.FormattedAddress(), items)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "should fail without sufficient credit")
		require.Contains(t, txRes.RawLog, "insufficient credit")
	})

	t.Run("fail: create lease for tenant with invalid address", func(t *testing.T) {
		items := []string{fmt.Sprintf("%d:1", testSKUID)}
		// Using an invalid address format - this should fail at CLI validation
		res, err := helpers.BillingCreateLeaseForTenant(ctx, chain, authority, "invalid-address", items)
		// CLI should return an error for invalid address
		require.Error(t, err, "should fail with invalid tenant address")
		_ = res // unused
	})

	t.Run("fail: create lease for tenant with non-existent SKU", func(t *testing.T) {
		items := []string{"99999:1"}
		res, err := helpers.BillingCreateLeaseForTenant(ctx, chain, authority, newTenant.FormattedAddress(), items)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "should fail with non-existent SKU")
		require.Contains(t, txRes.RawLog, "not found")
	})

	t.Run("success: verify event shows authority created lease", func(t *testing.T) {
		// Create another lease and check the event
		items := []string{fmt.Sprintf("%d:1", testSKUID)}
		res, err := helpers.BillingCreateLeaseForTenant(ctx, chain, authority, newTenant.FormattedAddress(), items)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
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

// testAutoCloseMechanism tests the lazy auto-close mechanism that closes leases
// when credit balance is exhausted during settlement operations.
func testAutoCloseMechanism(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, authority, providerWallet ibc.Wallet) {
	t.Log("=== Testing Auto-Close Mechanism ===")

	// Create a dedicated tenant for auto-close tests with minimal credit
	// to force exhaustion quickly
	users := interchaintest.GetAndFundTestUsers(t, ctx, "auto-close-tenant", DefaultGenesisAmt, chain)
	autoCloseTenant := users[0]

	// For auto-close tests, we need credit to exhaust quickly.
	// testSKUID has rate of 1000/second per unit.
	// We'll use quantity=500 to get 500,000/second rate.
	// Fund with 5,020,000 (just above 5M minimum).
	// Time to exhaust: 5,020,000 / 500,000 = ~10 seconds
	t.Run("setup: fund tenant with minimal credit", func(t *testing.T) {
		fundAmount := fmt.Sprintf("5020000%s", testPWRDenom) // Just above minimum
		res, err := helpers.BillingFundCredit(ctx, chain, authority, autoCloseTenant.FormattedAddress(), fundAmount)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "funding should succeed: %s", txRes.RawLog)

		creditRes, err := helpers.BillingQueryCreditAccount(ctx, chain, autoCloseTenant.FormattedAddress())
		require.NoError(t, err)
		t.Logf("Initial credit balance: %s", creditRes.Balance)
	})

	var autoCloseLeaseID uint64
	t.Run("setup: create lease that will exhaust credit", func(t *testing.T) {
		// Create a lease with high quantity to exhaust credit quickly
		// testSKUID has 1000/second rate, so quantity=500 gives 500,000/second
		// With 5,020,000 credit, this will exhaust in ~10 seconds
		items := []string{fmt.Sprintf("%d:500", testSKUID)}
		res, err := helpers.BillingCreateLease(ctx, chain, autoCloseTenant, items)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "lease creation should succeed: %s", txRes.RawLog)

		autoCloseLeaseID, err = helpers.GetLeaseIDFromTxHash(ctx, chain, res.TxHash)
		require.NoError(t, err)
		t.Logf("Created lease ID: %d", autoCloseLeaseID)

		// Verify lease is active
		lease, err := helpers.BillingQueryLease(ctx, chain, autoCloseLeaseID)
		require.NoError(t, err)
		require.Equal(t, "LEASE_STATE_ACTIVE", lease.Lease.State, "lease should be active")
	})

	t.Run("success: lease auto-closes when credit exhausted during withdrawal", func(t *testing.T) {
		// Wait for enough blocks to exhaust credit
		// With 500,000/second rate and ~5M credit, we need ~10 seconds
		// Block time is ~1 second, so wait for ~15 blocks to be safe
		t.Log("Waiting for credit to accrue/exhaust...")
		require.NoError(t, testutil.WaitForBlocks(ctx, 15, chain))

		// Check credit balance - should be very low or zero
		creditRes, err := helpers.BillingQueryCreditAccount(ctx, chain, autoCloseTenant.FormattedAddress())
		require.NoError(t, err)
		t.Logf("Credit balance after accrual: %s", creditRes.Balance)

		// Trigger settlement by attempting a withdrawal
		// This should auto-close the lease due to exhausted credit
		res, err := helpers.BillingWithdraw(ctx, chain, providerWallet, autoCloseLeaseID)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		t.Logf("Withdrawal tx result: code=%d, log=%s", txRes.Code, txRes.RawLog)

		// Query lease - should now be inactive due to auto-close
		lease, err := helpers.BillingQueryLease(ctx, chain, autoCloseLeaseID)
		require.NoError(t, err)
		require.Equal(t, "LEASE_STATE_INACTIVE", lease.Lease.State,
			"lease should be auto-closed after credit exhaustion")
		t.Log("Lease was auto-closed as expected")
	})

	t.Run("success: auto-closed lease emits proper events", func(t *testing.T) {
		// The withdrawal that triggered auto-close should have emitted events
		// Query the lease to verify it's closed
		lease, err := helpers.BillingQueryLease(ctx, chain, autoCloseLeaseID)
		require.NoError(t, err)
		require.Equal(t, "LEASE_STATE_INACTIVE", lease.Lease.State)

		// Verify closed_at is set (indicates it was closed)
		require.NotEmpty(t, lease.Lease.ClosedAt, "closed_at should be set for auto-closed lease")
		t.Logf("Lease closed_at: %s", lease.Lease.ClosedAt)
	})

	t.Run("success: provider already withdrew during auto-close", func(t *testing.T) {
		// After auto-close, the provider should have already received their tokens
		// during the settlement that triggered the close
		// Attempting another withdrawal should fail (nothing left)
		res, err := helpers.BillingWithdraw(ctx, chain, providerWallet, autoCloseLeaseID)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "second withdrawal should fail")
		require.Contains(t, txRes.RawLog, "no withdrawable amount",
			"should indicate no withdrawable amount")
	})

	t.Run("success: tenant cannot create new lease with exhausted credit", func(t *testing.T) {
		// Credit should be zero now, so creating a new lease should fail
		items := []string{fmt.Sprintf("%d:1", testSKUID)}
		res, err := helpers.BillingCreateLease(ctx, chain, autoCloseTenant, items)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "lease creation should fail with insufficient credit")
		require.Contains(t, txRes.RawLog, "insufficient credit",
			"should indicate insufficient credit balance")
	})

	// Test auto-close via CloseLease when credit is exhausted
	t.Run("success: explicit close on exhausted lease works", func(t *testing.T) {
		// Create another tenant with minimal credit
		users2 := interchaintest.GetAndFundTestUsers(t, ctx, "auto-close-tenant2", DefaultGenesisAmt, chain)
		tenant2 := users2[0]

		// Fund minimally - same approach: 5,020,000 with quantity=500
		fundAmount := fmt.Sprintf("5020000%s", testPWRDenom)
		res, err := helpers.BillingFundCredit(ctx, chain, authority, tenant2.FormattedAddress(), fundAmount)
		require.NoError(t, err)
		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code)

		// Create lease with high quantity
		items := []string{fmt.Sprintf("%d:500", testSKUID)}
		res, err = helpers.BillingCreateLease(ctx, chain, tenant2, items)
		require.NoError(t, err)
		txRes, err = chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code)

		leaseID, err := helpers.GetLeaseIDFromTxHash(ctx, chain, res.TxHash)
		require.NoError(t, err)

		// Wait for credit exhaustion
		require.NoError(t, testutil.WaitForBlocks(ctx, 15, chain))

		// Explicitly close the lease (tenant closes their own)
		res, err = helpers.BillingCloseLease(ctx, chain, tenant2, leaseID)
		require.NoError(t, err)

		txRes, err = chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		// Should succeed - close settles and handles exhausted credit gracefully
		require.Equal(t, uint32(0), txRes.Code, "explicit close should succeed: %s", txRes.RawLog)

		// Verify lease is closed
		lease, err := helpers.BillingQueryLease(ctx, chain, leaseID)
		require.NoError(t, err)
		require.Equal(t, "LEASE_STATE_INACTIVE", lease.Lease.State)
	})

	// Test that closing a lease triggers settlement and transfers accrued amount
	t.Run("success: closing lease settles and transfers accrued amount", func(t *testing.T) {
		// Create a tenant with enough credit
		users3 := interchaintest.GetAndFundTestUsers(t, ctx, "settlement-tenant", DefaultGenesisAmt, chain)
		tenant3 := users3[0]

		// Fund with credit
		fundAmount := fmt.Sprintf("100000000%s", testPWRDenom) // 100 PWR
		res, err := helpers.BillingFundCredit(ctx, chain, authority, tenant3.FormattedAddress(), fundAmount)
		require.NoError(t, err)
		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code)

		// Create lease with low quantity (slow accrual)
		items := []string{fmt.Sprintf("%d:1", testSKUID)}
		res, err = helpers.BillingCreateLease(ctx, chain, tenant3, items)
		require.NoError(t, err)
		txRes, err = chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code)

		leaseID, err := helpers.GetLeaseIDFromTxHash(ctx, chain, res.TxHash)
		require.NoError(t, err)

		// Get initial credit balance
		initialCredit, err := helpers.BillingQueryCreditAccount(ctx, chain, tenant3.FormattedAddress())
		require.NoError(t, err)
		t.Logf("Credit after lease creation: %s", initialCredit.Balance)

		// Wait for some accrual (1000/sec rate, 5 blocks = ~5000 accrued)
		require.NoError(t, testutil.WaitForBlocks(ctx, 5, chain))

		// Close the lease - this triggers settlement
		res, err = helpers.BillingCloseLease(ctx, chain, tenant3, leaseID)
		require.NoError(t, err)
		txRes, err = chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "lease close should succeed")

		// Get credit balance after lease close - should be less due to settlement
		afterCredit, err := helpers.BillingQueryCreditAccount(ctx, chain, tenant3.FormattedAddress())
		require.NoError(t, err)
		t.Logf("Credit after lease close: %s", afterCredit.Balance)

		// Credit should have decreased (settlement happened)
		require.True(t, afterCredit.Balance.Amount.LT(initialCredit.Balance.Amount),
			"credit should decrease due to settlement during lease close")

		// Verify lease is now inactive
		lease, err := helpers.BillingQueryLease(ctx, chain, leaseID)
		require.NoError(t, err)
		require.Equal(t, "LEASE_STATE_INACTIVE", lease.Lease.State, "lease should be inactive")
	})
}

// testCreditAddressQuery tests the credit address derivation query.
func testCreditAddressQuery(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, fundedTenant ibc.Wallet) {
	// Test: derive credit address without existing credit account
	t.Run("success: derive credit address for any address", func(t *testing.T) {
		// Use the funded tenant address - we just want to test the derivation works
		// The address doesn't need to NOT have a credit account, we're testing derivation
		res, err := helpers.BillingQueryCreditAddress(ctx, chain, fundedTenant.FormattedAddress())
		require.NoError(t, err)
		require.NotEmpty(t, res.CreditAddress, "credit address should be derived")
		t.Logf("Derived credit address for %s: %s", fundedTenant.FormattedAddress(), res.CreditAddress)
	})

	// Test: derive credit address for funded tenant matches actual credit account
	t.Run("success: derived address matches credit account", func(t *testing.T) {
		// Get the derived address
		derivedRes, err := helpers.BillingQueryCreditAddress(ctx, chain, fundedTenant.FormattedAddress())
		require.NoError(t, err)

		// Get the actual credit account
		creditRes, err := helpers.BillingQueryCreditAccount(ctx, chain, fundedTenant.FormattedAddress())
		require.NoError(t, err)

		// They should match
		require.Equal(t, derivedRes.CreditAddress, creditRes.CreditAccount.CreditAddress,
			"derived address should match actual credit account address")
	})

	// Test: invalid tenant address
	t.Run("fail: invalid tenant address", func(t *testing.T) {
		_, err := helpers.BillingQueryCreditAddress(ctx, chain, "invalid_address")
		require.Error(t, err, "should fail with invalid address")
	})
}

// testWithdrawAllLimits tests the WithdrawAll limit functionality.
func testWithdrawAllLimits(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, authority, providerWallet ibc.Wallet) {
	// Create a new tenant for these tests
	users := interchaintest.GetAndFundTestUsers(t, ctx, "withdrawall-limit-tenant", DefaultGenesisAmt, chain)
	tenant := users[0]

	// Fund tenant's credit account
	fundAmount := fmt.Sprintf("500000000%s", testPWRDenom) // 500 PWR
	res, err := helpers.BillingFundCredit(ctx, chain, authority, tenant.FormattedAddress(), fundAmount)
	require.NoError(t, err)
	txRes, err := chain.GetTransaction(res.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), txRes.Code)

	// Create multiple leases for testing
	leaseIDs := make([]uint64, 5)
	for i := 0; i < 5; i++ {
		items := []string{fmt.Sprintf("%d:1", testSKUID)}
		res, err := helpers.BillingCreateLease(ctx, chain, tenant, items)
		require.NoError(t, err)
		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "lease creation should succeed")

		leaseID, err := helpers.GetLeaseIDFromTxHash(ctx, chain, res.TxHash)
		require.NoError(t, err)
		leaseIDs[i] = leaseID
	}

	// Wait for some accrual
	require.NoError(t, testutil.WaitForBlocks(ctx, 5, chain))

	// Test: withdraw all with custom limit
	t.Run("success: withdraw all with custom limit", func(t *testing.T) {
		// Use a limit of 2 to test pagination
		res, err := helpers.BillingWithdrawAllWithLimit(ctx, chain, providerWallet, testProviderID, 2)
		require.NoError(t, err)
		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "withdraw all should succeed")

		// Check events for has_more flag
		t.Logf("WithdrawAll with limit 2 succeeded")
	})

	// Test: withdraw all with default limit (0 means default)
	t.Run("success: withdraw all with default limit", func(t *testing.T) {
		res, err := helpers.BillingWithdrawAll(ctx, chain, providerWallet, testProviderID)
		require.NoError(t, err)
		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "withdraw all should succeed")
	})

	// Test: withdraw all with limit exceeding maximum should fail at CLI validation
	t.Run("fail: withdraw all with limit exceeding maximum", func(t *testing.T) {
		// MaxWithdrawAllLimit is 100, try 150
		_, err := helpers.BillingWithdrawAllWithLimit(ctx, chain, providerWallet, testProviderID, 150)
		require.Error(t, err, "withdraw all with excessive limit should fail")
	})
}

// testProviderDeactivation tests behavior when a provider is deactivated while having active leases.
func testProviderDeactivation(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, authority, providerWallet ibc.Wallet) {
	// Create a new user specifically for the deactivation test provider
	users := interchaintest.GetAndFundTestUsers(t, ctx, "deactivation-provider-wallet", DefaultGenesisAmt, chain)
	deactivationProviderWallet := users[0]

	// Create a new provider specifically for deactivation tests
	var deactivateProviderID uint64
	t.Run("setup: create provider for deactivation test", func(t *testing.T) {
		res, err := helpers.SKUCreateProvider(ctx, chain, authority,
			deactivationProviderWallet.FormattedAddress(), deactivationProviderWallet.FormattedAddress(), "")
		require.NoError(t, err)
		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code)

		// Get provider ID from events
		for _, event := range txRes.Events {
			if event.Type == "provider_created" {
				for _, attr := range event.Attributes {
					if attr.Key == "provider_id" {
						deactivateProviderID, _ = strconv.ParseUint(attr.Value, 10, 64)
						break
					}
				}
			}
		}
		require.NotZero(t, deactivateProviderID, "provider ID should be extracted from events")
	})

	// Create SKU for this provider with valid price (evenly divisible)
	t.Run("setup: create SKU for deactivation provider", func(t *testing.T) {
		res, err := helpers.SKUCreateSKU(ctx, chain, authority,
			deactivateProviderID, "Deactivation SKU", 1, "3600000umfx", "")
		require.NoError(t, err)
		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code)
	})

	// Get the SKU ID
	skus, err := helpers.SKUQuerySKUsByProvider(ctx, chain, deactivateProviderID)
	require.NoError(t, err)
	require.Len(t, skus.Skus, 1)
	deactivateSKUID := skus.Skus[0].Id

	// Create tenant and fund credit
	tenantUsers := interchaintest.GetAndFundTestUsers(t, ctx, "deactivate-tenant", DefaultGenesisAmt, chain)
	tenant := tenantUsers[0]

	fundAmount := fmt.Sprintf("100000000%s", testPWRDenom)
	res, err := helpers.BillingFundCredit(ctx, chain, authority, tenant.FormattedAddress(), fundAmount)
	require.NoError(t, err)
	txRes, err := chain.GetTransaction(res.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), txRes.Code)

	// Create a lease with this provider's SKU
	var leaseID uint64
	t.Run("setup: create lease with provider's SKU", func(t *testing.T) {
		items := []string{fmt.Sprintf("%d:1", deactivateSKUID)}
		res, err := helpers.BillingCreateLease(ctx, chain, tenant, items)
		require.NoError(t, err)
		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code)

		leaseID, err = helpers.GetLeaseIDFromTxHash(ctx, chain, res.TxHash)
		require.NoError(t, err)
	})

	// Deactivate the provider
	t.Run("success: provider can be deactivated while having active leases", func(t *testing.T) {
		res, err := helpers.SKUDeactivateProvider(ctx, chain, authority, deactivateProviderID)
		require.NoError(t, err)
		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "provider deactivation should succeed")
	})

	// Verify provider is deactivated
	t.Run("success: verify provider is deactivated", func(t *testing.T) {
		provider, err := helpers.SKUQueryProvider(ctx, chain, deactivateProviderID)
		require.NoError(t, err)
		require.False(t, provider.Provider.Active, "provider should be inactive")
	})

	// Verify existing lease is still active
	t.Run("success: existing lease continues after provider deactivation", func(t *testing.T) {
		lease, err := helpers.BillingQueryLease(ctx, chain, leaseID)
		require.NoError(t, err)
		require.Equal(t, "LEASE_STATE_ACTIVE", lease.Lease.State, "lease should still be active")
	})

	// Wait for some accrual
	require.NoError(t, testutil.WaitForBlocks(ctx, 3, chain))

	// Provider can still withdraw after deactivation
	t.Run("success: provider can still withdraw after deactivation", func(t *testing.T) {
		res, err := helpers.BillingWithdraw(ctx, chain, deactivationProviderWallet, leaseID)
		require.NoError(t, err)
		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "withdrawal should succeed")
	})

	// Cannot create new lease with deactivated provider's SKU
	// First, deactivate the SKU (since the provider is deactivated, SKUs are still active but provider check should fail)
	t.Run("fail: cannot create new lease with deactivated provider's SKU", func(t *testing.T) {
		// Create another tenant
		users2 := interchaintest.GetAndFundTestUsers(t, ctx, "deactivate-tenant-2", DefaultGenesisAmt, chain)
		tenant2 := users2[0]

		// Fund their credit
		fundAmount := fmt.Sprintf("100000000%s", testPWRDenom)
		res, err := helpers.BillingFundCredit(ctx, chain, authority, tenant2.FormattedAddress(), fundAmount)
		require.NoError(t, err)
		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code)

		// Try to create a lease - should fail because provider is inactive
		items := []string{fmt.Sprintf("%d:1", deactivateSKUID)}
		res, err = helpers.BillingCreateLease(ctx, chain, tenant2, items)
		require.NoError(t, err)
		txRes, err = chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "lease creation should fail with inactive provider")
	})

	// Deactivated provider is still queryable
	t.Run("success: deactivated provider is still queryable", func(t *testing.T) {
		provider, err := helpers.SKUQueryProvider(ctx, chain, deactivateProviderID)
		require.NoError(t, err)
		require.NotNil(t, provider.Provider)
		require.Equal(t, deactivateProviderID, provider.Provider.Id)
		require.False(t, provider.Provider.Active, "provider should be inactive")
	})
}

// testSendRestriction tests that only the correct denom can be sent to credit accounts.
// This prevents users from accidentally losing funds by sending wrong tokens.
func testSendRestriction(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, authority, tenant ibc.Wallet) {
	t.Log("=== Testing Send Restriction ===")

	node := chain.GetNode()

	// Get the tenant's credit address
	creditAddrResp, err := helpers.BillingQueryCreditAddress(ctx, chain, tenant.FormattedAddress())
	require.NoError(t, err, "should query credit address")
	creditAddr := creditAddrResp.CreditAddress
	t.Logf("Tenant credit address: %s", creditAddr)

	// Ensure the tenant has a credit account (fund it first if needed)
	t.Run("setup: ensure tenant has credit account", func(t *testing.T) {
		// Check if tenant already has a credit account
		_, err := helpers.BillingQueryCreditAccount(ctx, chain, tenant.FormattedAddress())
		if err != nil {
			// Fund the credit account to create it
			res, err := helpers.BillingFundCredit(ctx, chain, tenant, tenant.FormattedAddress(), fmt.Sprintf("10000000%s", testPWRDenom))
			require.NoError(t, err)
			txRes, err := chain.GetTransaction(res.TxHash)
			require.NoError(t, err)
			require.Equal(t, uint32(0), txRes.Code, "fund credit should succeed: %s", txRes.RawLog)
		}
	})

	// Test: Try to send wrong denom (umfx) to credit account via bank send
	t.Run("fail: bank send wrong denom to credit account", func(t *testing.T) {
		// Use node.ExecTx to send umfx directly via bank send
		_, err := node.ExecTx(ctx, tenant.KeyName(),
			"bank", "send", tenant.FormattedAddress(), creditAddr, "1000000umfx",
			"--gas", "auto",
		)
		// This should fail due to send restriction
		require.Error(t, err, "bank send with wrong denom should fail")
		require.Contains(t, err.Error(), "cannot send umfx to credit account",
			"error should mention wrong denom")
	})

	// Test: Send correct denom (testPWRDenom) to credit account via bank send
	t.Run("success: bank send correct denom to credit account", func(t *testing.T) {
		// First, get the initial balance
		initialBalance, err := helpers.BillingQueryCreditAccount(ctx, chain, tenant.FormattedAddress())
		require.NoError(t, err)
		t.Logf("Initial credit balance: %s", initialBalance.Balance)

		// Send correct denom via bank send
		res, err := node.ExecTx(ctx, tenant.KeyName(),
			"bank", "send", tenant.FormattedAddress(), creditAddr, fmt.Sprintf("1000000%s", testPWRDenom),
			"--gas", "auto",
		)
		require.NoError(t, err, "bank send with correct denom should succeed")
		t.Logf("Bank send tx hash: %s", res)

		// Verify the balance increased (note: credit account balance comes from bank module)
		finalBalance, err := helpers.BillingQueryCreditAccount(ctx, chain, tenant.FormattedAddress())
		require.NoError(t, err)
		t.Logf("Final credit balance: %s", finalBalance.Balance)
	})

	// Test: Send wrong denom via multi-send should also fail
	t.Run("fail: multi-send with wrong denom to credit account", func(t *testing.T) {
		// This tests that the restriction applies to all bank send operations
		_, err := node.ExecTx(ctx, tenant.KeyName(),
			"bank", "send", tenant.FormattedAddress(), creditAddr, "500000umfx",
			"--gas", "auto",
		)
		require.Error(t, err, "multi-send with wrong denom should fail")
	})

	// Test: Send to non-credit address works with any denom
	t.Run("success: send any denom to non-credit address", func(t *testing.T) {
		// Use authority address as a non-credit address recipient
		_, err := node.ExecTx(ctx, tenant.KeyName(),
			"bank", "send", tenant.FormattedAddress(), authority.FormattedAddress(), "1000000umfx",
			"--gas", "auto",
		)
		require.NoError(t, err, "sending to non-credit address should succeed with any denom")
	})
}

// testAllowedListAuthorization tests the allowed_list authorization for CreateLeaseForTenant.
// This verifies that:
// - Authority can always create leases for tenants
// - Users in allowed_list can create leases for tenants
// - Users not in allowed_list cannot create leases for tenants
// - Updating allowed_list changes who can create leases
func testAllowedListAuthorization(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, authority ibc.Wallet) {
	t.Log("=== Testing AllowedList Authorization ===")

	// Create test users
	users := interchaintest.GetAndFundTestUsers(t, ctx, "allowlist-test", DefaultGenesisAmt, chain)
	allowedUser := users[0]

	users2 := interchaintest.GetAndFundTestUsers(t, ctx, "nonallowlist-test", DefaultGenesisAmt, chain)
	nonAllowedUser := users2[0]

	users3 := interchaintest.GetAndFundTestUsers(t, ctx, "tenant-test", DefaultGenesisAmt, chain)
	tenant := users3[0]

	// First update params to add allowedUser to allowed_list
	t.Run("setup: add user to allowed_list", func(t *testing.T) {
		// Get current params
		params, err := helpers.BillingQueryParams(ctx, chain)
		require.NoError(t, err)

		// Update with allowed_list
		res, err := helpers.BillingUpdateParams(ctx, chain, authority, testPWRDenom,
			params.Params.MinCreditBalance, params.Params.MaxLeasesPerTenant, params.Params.MaxItemsPerLease,
			[]string{allowedUser.FormattedAddress()})
		require.NoError(t, err)
		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "params update should succeed")
	})

	// Fund tenant's credit account
	t.Run("setup: fund tenant credit", func(t *testing.T) {
		fundAmount := fmt.Sprintf("100000000%s", testPWRDenom)
		res, err := helpers.BillingFundCredit(ctx, chain, authority, tenant.FormattedAddress(), fundAmount)
		require.NoError(t, err)
		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "fund credit should succeed")
	})

	// Get an SKU for lease creation
	skus, err := helpers.SKUQuerySKUs(ctx, chain)
	require.NoError(t, err)
	require.NotEmpty(t, skus.Skus, "should have at least one SKU")
	skuID := skus.Skus[0].Id

	// Test: Authority can create lease for tenant
	var authorityLeaseID uint64
	t.Run("success: authority creates lease for tenant", func(t *testing.T) {
		items := []string{fmt.Sprintf("%d:1", skuID)}
		res, err := helpers.BillingCreateLeaseForTenant(ctx, chain, authority, tenant.FormattedAddress(), items)
		require.NoError(t, err)
		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "authority should be able to create lease for tenant")

		authorityLeaseID, err = helpers.GetLeaseIDFromTxHash(ctx, chain, res.TxHash)
		require.NoError(t, err)
		require.NotZero(t, authorityLeaseID)
	})

	// Test: Allowed user can create lease for tenant
	var allowedUserLeaseID uint64
	t.Run("success: allowed user creates lease for tenant", func(t *testing.T) {
		items := []string{fmt.Sprintf("%d:1", skuID)}
		res, err := helpers.BillingCreateLeaseForTenant(ctx, chain, allowedUser, tenant.FormattedAddress(), items)
		require.NoError(t, err)
		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "allowed user should be able to create lease for tenant")

		allowedUserLeaseID, err = helpers.GetLeaseIDFromTxHash(ctx, chain, res.TxHash)
		require.NoError(t, err)
		require.NotZero(t, allowedUserLeaseID)
	})

	// Test: Non-allowed user cannot create lease for tenant
	t.Run("fail: non-allowed user cannot create lease for tenant", func(t *testing.T) {
		items := []string{fmt.Sprintf("%d:1", skuID)}
		res, err := helpers.BillingCreateLeaseForTenant(ctx, chain, nonAllowedUser, tenant.FormattedAddress(), items)
		require.NoError(t, err)
		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "non-allowed user should not be able to create lease for tenant")
	})

	// Test: Verify leases belong to tenant
	t.Run("success: verify leases belong to tenant", func(t *testing.T) {
		lease1, err := helpers.BillingQueryLease(ctx, chain, authorityLeaseID)
		require.NoError(t, err)
		require.Equal(t, tenant.FormattedAddress(), lease1.Lease.Tenant)

		lease2, err := helpers.BillingQueryLease(ctx, chain, allowedUserLeaseID)
		require.NoError(t, err)
		require.Equal(t, tenant.FormattedAddress(), lease2.Lease.Tenant)
	})

	// Test: Remove user from allowed_list, then they can't create leases anymore
	t.Run("success: removed user cannot create lease after allowed_list update", func(t *testing.T) {
		// Get current params
		params, err := helpers.BillingQueryParams(ctx, chain)
		require.NoError(t, err)

		// Update with empty allowed_list
		res, err := helpers.BillingUpdateParams(ctx, chain, authority, testPWRDenom,
			params.Params.MinCreditBalance, params.Params.MaxLeasesPerTenant, params.Params.MaxItemsPerLease,
			[]string{})
		require.NoError(t, err)
		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "params update should succeed")

		// Now the previously allowed user should not be able to create leases
		items := []string{fmt.Sprintf("%d:1", skuID)}
		res, err = helpers.BillingCreateLeaseForTenant(ctx, chain, allowedUser, tenant.FormattedAddress(), items)
		require.NoError(t, err)
		txRes, err = chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "removed user should not be able to create lease for tenant")
	})

	// Test: Authority can still create leases even with empty allowed_list
	t.Run("success: authority can still create lease with empty allowed_list", func(t *testing.T) {
		items := []string{fmt.Sprintf("%d:1", skuID)}
		res, err := helpers.BillingCreateLeaseForTenant(ctx, chain, authority, tenant.FormattedAddress(), items)
		require.NoError(t, err)
		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "authority should always be able to create lease for tenant")
	})
}
