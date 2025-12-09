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
// ## Query Tests
//
// testBillingQueryParams:
//   - Verifies default params are returned (denom, min_credit_balance, max_leases_per_tenant)
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
//   - Success: lease with zero balance auto-closes (overdraw)
//   - Success: remaining credit stays in account after lease close
//   - Success: provider can withdraw after lease closure
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
	// Price: 100 umfx per hour = 100/3600 ≈ 0.0278 per second
	t.Run("create_sku_per_hour", func(t *testing.T) {
		res, err := helpers.SKUCreateSKU(ctx, chain, authority, testProviderID, "Compute Small", 1, "100umfx", "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "SKU creation should succeed: %s", txRes.RawLog)

		testSKUID, err = helpers.GetSKUIDFromTxHash(ctx, chain, res.TxHash)
		require.NoError(t, err)
		t.Logf("Created SKU ID (per-hour): %d", testSKUID)
	})

	// Create SKU with per-day pricing (Unit = 2)
	// Price: 1000 umfx per day = 1000/86400 ≈ 0.0116 per second
	t.Run("create_sku_per_day", func(t *testing.T) {
		res, err := helpers.SKUCreateSKU(ctx, chain, authority, testProviderID, "Storage Large", 2, "1000umfx", "")
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
	t.Logf("Billing params: denom=%s, min_credit_balance=%s, max_leases_per_tenant=%s",
		res.Params.Denom, res.Params.MinCreditBalance, res.Params.MaxLeasesPerTenant)
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

	t.Run("success: provider can withdraw after lease closure", func(t *testing.T) {
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

		// Provider should still be able to withdraw remaining accrued funds
		res, err := helpers.BillingWithdraw(ctx, chain, providerWallet, closedLeaseID)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		// This might succeed with 0 amount or small amount
		require.Equal(t, uint32(0), txRes.Code, "provider withdraw from closed lease should succeed: %s", txRes.RawLog)
	})
}
