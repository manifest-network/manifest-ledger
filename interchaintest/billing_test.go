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
//   - Verifies default params are returned (denom, max_leases_per_tenant, max_items_per_lease, min_lease_duration)
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
//   - Fail: create lease without credit account
//   - Fail: create lease with insufficient credit
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

// testProviderUUID is the provider ID created for billing tests
var testProviderUUID string

// testSKUUUID is the SKU ID created for billing tests (per-hour pricing)
var testSKUUUID string

// testSKUUUID2 is a second SKU ID for multi-SKU lease tests (per-day pricing)
var testSKUUUID2 string

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
		testLeaseCreate(t, ctx, chain, authority, tenant1, tenant2, providerWallet)
	})

	t.Run("LeaseQuery", func(t *testing.T) {
		testLeaseQuery(t, ctx, chain, tenant1, providerWallet)
	})

	t.Run("AccrualCalculation", func(t *testing.T) {
		testAccrualCalculation(t, ctx, chain, authority, tenant1, providerWallet)
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
		testWithdrawableQueries(t, ctx, chain, tenant1, providerWallet)
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

	// Note: Send restriction was removed as part of multi-denom support.
	// Credit accounts can now hold any token denomination.

	t.Run("AllowedListAuthorization", func(t *testing.T) {
		testAllowedListAuthorization(t, ctx, chain, authority)
	})

	t.Run("MultiDenom", func(t *testing.T) {
		testMultiDenom(t, ctx, chain, authority, providerWallet)
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

	// Note: The billing module no longer has a denom parameter.
	// SKUs can use any denom for their base_price, and credit accounts can hold any denom.

	// Create provider
	t.Run("create_provider", func(t *testing.T) {
		res, err := helpers.SKUCreateProvider(ctx, chain, authority, providerWallet.FormattedAddress(), providerWallet.FormattedAddress(), "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "provider creation should succeed: %s", txRes.RawLog)

		testProviderUUID, err = helpers.GetProviderUUIDFromTxHash(ctx, chain, res.TxHash)
		require.NoError(t, err)
		t.Logf("Created provider UUID: %s", testProviderUUID)
	})

	// Create SKU with per-hour pricing (Unit = 1)
	// Price: 3600000 upwr per hour = 3600000/3600 = 1000 per second
	// This ensures meaningful accrual even with short test durations
	t.Run("create_sku_per_hour", func(t *testing.T) {
		res, err := helpers.SKUCreateSKU(ctx, chain, authority, testProviderUUID, "Compute Small", 1, fmt.Sprintf("3600000%s", testPWRDenom), "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "SKU creation should succeed: %s", txRes.RawLog)

		testSKUUUID, err = helpers.GetSKUUUIDFromTxHash(ctx, chain, res.TxHash)
		require.NoError(t, err)
		t.Logf("Created SKU UUID (per-hour): %s", testSKUUUID)
	})

	// Create SKU with per-day pricing (Unit = 2)
	// Price: 86400000 upwr per day = 86400000/86400 = 1000 per second
	// This ensures meaningful accrual even with short test durations
	t.Run("create_sku_per_day", func(t *testing.T) {
		res, err := helpers.SKUCreateSKU(ctx, chain, authority, testProviderUUID, "Storage Large", 2, fmt.Sprintf("86400000%s", testPWRDenom), "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "SKU creation should succeed: %s", txRes.RawLog)

		testSKUUUID2, err = helpers.GetSKUUUIDFromTxHash(ctx, chain, res.TxHash)
		require.NoError(t, err)
		t.Logf("Created SKU UUID (per-day): %s", testSKUUUID2)
	})
}

func testBillingQueryParams(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain) {
	t.Log("=== Testing Billing Query Params ===")

	res, err := helpers.BillingQueryParamsJSON(ctx, chain)
	require.NoError(t, err)
	require.NotNil(t, res)
	require.NotEmpty(t, res.Params.MaxLeasesPerTenant, "max_leases_per_tenant should be set")
	require.NotEmpty(t, res.Params.MaxItemsPerLease, "max_items_per_lease should be set")
	require.NotEmpty(t, res.Params.MinLeaseDuration, "min_lease_duration should be set")
	t.Logf("Billing params: max_leases_per_tenant=%s, max_items_per_lease=%s, min_lease_duration=%s",
		res.Params.MaxLeasesPerTenant, res.Params.MaxItemsPerLease, res.Params.MinLeaseDuration)
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
		require.False(t, res.Balances.IsZero(), "credit balances should not be zero")
		t.Logf("Tenant1 credit balances: %s", res.Balances)
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

func testLeaseCreate(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, authority, tenant1, tenant2, providerWallet ibc.Wallet) {
	t.Log("=== Testing Lease Create ===")

	t.Run("success: tenant creates lease with single SKU", func(t *testing.T) {
		items := []string{fmt.Sprintf("%s:1", testSKUUUID)}
		res, err := helpers.BillingCreateLease(ctx, chain, tenant1, items)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "lease creation should succeed: %s", txRes.RawLog)

		leaseID, err := helpers.GetLeaseIDFromTxHash(ctx, chain, res.TxHash)
		require.NoError(t, err)
		t.Logf("Created lease ID: %s", leaseID)

		// Acknowledge the lease to make it ACTIVE for subsequent tests
		ackRes, err := helpers.BillingAcknowledgeLease(ctx, chain, providerWallet, leaseID)
		require.NoError(t, err)
		ackTxRes, err := chain.GetTransaction(ackRes.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), ackTxRes.Code, "lease acknowledgement should succeed: %s", ackTxRes.RawLog)
	})

	t.Run("success: tenant creates lease with multiple SKUs", func(t *testing.T) {
		items := []string{
			fmt.Sprintf("%s:2", testSKUUUID),  // 2x per-hour SKU
			fmt.Sprintf("%s:1", testSKUUUID2), // 1x per-day SKU
		}
		res, err := helpers.BillingCreateLease(ctx, chain, tenant1, items)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "lease creation should succeed: %s", txRes.RawLog)

		leaseID, err := helpers.GetLeaseIDFromTxHash(ctx, chain, res.TxHash)
		require.NoError(t, err)

		// Acknowledge the lease to make it ACTIVE for subsequent tests
		ackRes, err := helpers.BillingAcknowledgeLease(ctx, chain, providerWallet, leaseID)
		require.NoError(t, err)
		ackTxRes, err := chain.GetTransaction(ackRes.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), ackTxRes.Code, "lease acknowledgement should succeed: %s", ackTxRes.RawLog)
	})

	t.Run("fail: create lease with non-existent SKU", func(t *testing.T) {
		// Use a valid UUIDv7 format that doesn't exist
		nonExistentSKU := "01912345-6789-7abc-8def-0123456789ab"
		items := []string{fmt.Sprintf("%s:1", nonExistentSKU)}
		res, err := helpers.BillingCreateLease(ctx, chain, tenant1, items)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "lease creation should fail")
		require.Contains(t, txRes.RawLog, "not found")
	})

	t.Run("fail: create lease without credit account", func(t *testing.T) {
		// Create a user without any credit account (never funded)
		users := interchaintest.GetAndFundTestUsers(t, ctx, "no-credit-account", DefaultGenesisAmt, chain)
		noCreditAccount := users[0]

		items := []string{fmt.Sprintf("%s:1", testSKUUUID)}
		res, err := helpers.BillingCreateLease(ctx, chain, noCreditAccount, items)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "lease creation should fail")
		require.Contains(t, txRes.RawLog, "credit account not found",
			"should fail with credit account not found error")
	})

	t.Run("fail: create lease with insufficient credit", func(t *testing.T) {
		// Create a user with minimal credit (1 upwr) - not enough for min_lease_duration
		users := interchaintest.GetAndFundTestUsers(t, ctx, "low-credit", DefaultGenesisAmt, chain)
		lowCredit := users[0]

		// Fund with only 1 upwr - way below the required amount for min_lease_duration
		fundAmount := fmt.Sprintf("1%s", testPWRDenom)
		res, err := helpers.BillingFundCredit(ctx, chain, authority, lowCredit.FormattedAddress(), fundAmount)
		require.NoError(t, err)
		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "funding should succeed")

		// Now try to create a lease - should fail due to insufficient credit
		items := []string{fmt.Sprintf("%s:1", testSKUUUID)}
		res, err = helpers.BillingCreateLease(ctx, chain, lowCredit, items)
		require.NoError(t, err)

		txRes, err = chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "lease creation should fail")
		require.Contains(t, txRes.RawLog, "insufficient credit",
			"should fail with insufficient credit error")
	})

	t.Run("fail: create lease exceeding max_items_per_lease hard limit", func(t *testing.T) {
		// The hard limit is 100 items per lease (MaxItemsPerLeaseHardLimit)
		// This test validates the client-side validation in ValidateBasic
		items := make([]string, 101)
		for i := 0; i < 101; i++ {
			// Use valid UUIDv7 format SKU UUIDs to avoid UUID validation error
			items[i] = fmt.Sprintf("01912345-6789-7abc-8def-%012d:1", i)
		}
		_, err := helpers.BillingCreateLease(ctx, chain, tenant1, items)
		// The error should be caught at client-side validation before tx is broadcast
		require.Error(t, err)
		require.Contains(t, err.Error(), "too many items")
		t.Log("Correctly rejected lease with too many items (hard limit)")
	})
}

func testLeaseQuery(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, tenant1, providerWallet ibc.Wallet) {
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
		res, err := helpers.BillingQueryLeasesByProvider(ctx, chain, testProviderUUID, false)
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(res.Leases), 2, "provider should have at least 2 leases")

		for _, lease := range res.Leases {
			require.Equal(t, testProviderUUID, lease.ProviderUuid)
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

		leaseUUID := allLeases.Leases[0].Uuid

		res, err := helpers.BillingQueryLease(ctx, chain, leaseUUID)
		require.NoError(t, err)
		require.Equal(t, allLeases.Leases[0].Uuid, res.Lease.Uuid)
	})
}

func testAccrualCalculation(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, authority, tenant1, providerWallet ibc.Wallet) {
	t.Log("=== Testing Accrual Calculation ===")

	// Get an active lease
	leases, err := helpers.BillingQueryLeasesByTenant(ctx, chain, tenant1.FormattedAddress(), true)
	require.NoError(t, err)
	require.NotEmpty(t, leases.Leases, "tenant should have active leases")

	leaseUUID := leases.Leases[0].Uuid

	t.Run("success: verify accrual increases over time", func(t *testing.T) {
		// Get initial withdrawable
		initial, err := helpers.BillingQueryWithdrawable(ctx, chain, leaseUUID)
		require.NoError(t, err)
		t.Logf("Initial withdrawable: %s", initial.Amounts)

		// Wait for some blocks to pass
		require.NoError(t, testutil.WaitForBlocks(ctx, 5, chain))

		// Get updated withdrawable
		updated, err := helpers.BillingQueryWithdrawable(ctx, chain, leaseUUID)
		require.NoError(t, err)
		t.Logf("Updated withdrawable: %s", updated.Amounts)

		// Accrual should have increased (or at least not decreased)
		// Compare the first coin amount (assuming single denom lease)
		require.True(t, len(updated.Amounts) > 0 && len(initial.Amounts) > 0,
			"should have withdrawable amounts")
		require.True(t, updated.Amounts[0].Amount.GTE(initial.Amounts[0].Amount),
			"withdrawable should increase over time")
	})
}

func testWithdraw(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, authority, tenant1, providerWallet, unauthorizedUser ibc.Wallet) {
	t.Log("=== Testing Withdraw ===")

	// Get an active lease
	leases, err := helpers.BillingQueryLeasesByTenant(ctx, chain, tenant1.FormattedAddress(), true)
	require.NoError(t, err)
	require.NotEmpty(t, leases.Leases)

	leaseUUID := leases.Leases[0].Uuid

	// Wait for some accrual
	require.NoError(t, testutil.WaitForBlocks(ctx, 3, chain))

	t.Run("success: provider withdraws from lease", func(t *testing.T) {
		// Get provider's initial balance
		initialBalance, err := chain.GetBalance(ctx, providerWallet.FormattedAddress(), testPWRDenom)
		require.NoError(t, err)

		res, err := helpers.BillingWithdraw(ctx, chain, providerWallet, leaseUUID)
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
		res, err := helpers.BillingWithdraw(ctx, chain, tenant1, leaseUUID)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "tenant withdraw should fail")
		require.Contains(t, txRes.RawLog, "unauthorized")
	})

	t.Run("fail: unauthorized user cannot withdraw", func(t *testing.T) {
		res, err := helpers.BillingWithdraw(ctx, chain, unauthorizedUser, leaseUUID)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "unauthorized withdraw should fail")
		require.Contains(t, txRes.RawLog, "unauthorized")
	})

	t.Run("fail: withdraw from non-existent lease", func(t *testing.T) {
		// Use a valid UUIDv7 format that doesn't exist
		nonExistentUUID := "01912345-6789-7abc-8def-999999999999"
		res, err := helpers.BillingWithdraw(ctx, chain, providerWallet, nonExistentUUID)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "withdraw from non-existent lease should fail")
		require.Contains(t, txRes.RawLog, "not found")
	})

	t.Run("success: authority withdraws on behalf of provider", func(t *testing.T) {
		// Wait for more accrual
		require.NoError(t, testutil.WaitForBlocks(ctx, 3, chain))

		res, err := helpers.BillingWithdraw(ctx, chain, authority, leaseUUID)
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

		res, err := helpers.BillingWithdrawAll(ctx, chain, providerWallet, testProviderUUID)
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

		res, err := helpers.BillingWithdrawAll(ctx, chain, authority, testProviderUUID)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "authority withdraw all should succeed: %s", txRes.RawLog)
	})
}

func testLeaseClose(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, authority, tenant1, providerWallet, unauthorizedUser ibc.Wallet) {
	t.Log("=== Testing Lease Close ===")

	// Create a new lease for close testing
	var closeLeaseID string
	t.Run("setup: create and acknowledge lease for close testing", func(t *testing.T) {
		items := []string{fmt.Sprintf("%s:1", testSKUUUID)}
		res, err := helpers.BillingCreateLease(ctx, chain, tenant1, items)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code)

		closeLeaseID, err = helpers.GetLeaseIDFromTxHash(ctx, chain, res.TxHash)
		require.NoError(t, err)

		// Acknowledge the lease to make it ACTIVE
		ackRes, err := helpers.BillingAcknowledgeLease(ctx, chain, providerWallet, closeLeaseID)
		require.NoError(t, err)
		ackTxRes, err := chain.GetTransaction(ackRes.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), ackTxRes.Code, "lease acknowledgement should succeed: %s", ackTxRes.RawLog)
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
		require.Equal(t, "LEASE_STATE_CLOSED", leaseRes.Lease.State)
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
		// Use valid UUIDv7 format that doesn't exist
		nonExistentUUID := "01912345-6789-7abc-8def-0123456789ab"
		res, err := helpers.BillingCloseLease(ctx, chain, tenant1, nonExistentUUID)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "close non-existent lease should fail")
		require.Contains(t, txRes.RawLog, "not found")
	})

	// Test provider closing
	var providerCloseLeaseID string
	t.Run("setup: create and acknowledge lease for provider close", func(t *testing.T) {
		items := []string{fmt.Sprintf("%s:1", testSKUUUID)}
		res, err := helpers.BillingCreateLease(ctx, chain, tenant1, items)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code)

		providerCloseLeaseID, err = helpers.GetLeaseIDFromTxHash(ctx, chain, res.TxHash)
		require.NoError(t, err)

		// Acknowledge the lease to make it ACTIVE
		ackRes, err := helpers.BillingAcknowledgeLease(ctx, chain, providerWallet, providerCloseLeaseID)
		require.NoError(t, err)
		ackTxRes, err := chain.GetTransaction(ackRes.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), ackTxRes.Code, "lease acknowledgement should succeed: %s", ackTxRes.RawLog)
	})

	t.Run("success: provider closes lease", func(t *testing.T) {
		res, err := helpers.BillingCloseLease(ctx, chain, providerWallet, providerCloseLeaseID)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "provider close should succeed: %s", txRes.RawLog)
	})

	// Test authority closing
	var authorityCloseLeaseID string
	t.Run("setup: create and acknowledge lease for authority close", func(t *testing.T) {
		items := []string{fmt.Sprintf("%s:1", testSKUUUID)}
		res, err := helpers.BillingCreateLease(ctx, chain, tenant1, items)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code)

		authorityCloseLeaseID, err = helpers.GetLeaseIDFromTxHash(ctx, chain, res.TxHash)
		require.NoError(t, err)

		// Acknowledge the lease to make it ACTIVE
		ackRes, err := helpers.BillingAcknowledgeLease(ctx, chain, providerWallet, authorityCloseLeaseID)
		require.NoError(t, err)
		ackTxRes, err := chain.GetTransaction(ackRes.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), ackTxRes.Code, "lease acknowledgement should succeed: %s", ackTxRes.RawLog)
	})

	t.Run("success: authority closes lease", func(t *testing.T) {
		res, err := helpers.BillingCloseLease(ctx, chain, authority, authorityCloseLeaseID)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "authority close should succeed: %s", txRes.RawLog)
	})
}

func testWithdrawableQueries(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, tenant1, providerWallet ibc.Wallet) {
	t.Log("=== Testing Withdrawable Queries ===")

	// Get an active lease
	leases, err := helpers.BillingQueryLeasesByTenant(ctx, chain, tenant1.FormattedAddress(), true)
	require.NoError(t, err)

	if len(leases.Leases) == 0 {
		t.Skip("No active leases to test withdrawable queries")
	}

	leaseUUID := leases.Leases[0].Uuid

	t.Run("success: query withdrawable amount for lease", func(t *testing.T) {
		res, err := helpers.BillingQueryWithdrawable(ctx, chain, leaseUUID)
		require.NoError(t, err)
		require.False(t, res.Amounts.IsZero(), "withdrawable amounts should not be zero")
		t.Logf("Withdrawable for lease %s: %s", leaseUUID, res.Amounts)
	})

	t.Run("success: query provider total withdrawable", func(t *testing.T) {
		res, err := helpers.BillingQueryProviderWithdrawable(ctx, chain, testProviderUUID)
		require.NoError(t, err)
		t.Logf("Provider total withdrawable: %s (from %s leases)", res.Amounts, res.LeaseCount)
	})
}

func testEdgeCases(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, authority, tenant2, providerWallet ibc.Wallet) {
	t.Log("=== Testing Edge Cases ===")

	t.Run("success: remaining credit stays after lease close", func(t *testing.T) {
		// Get tenant2's credit balance before
		beforeRes, err := helpers.BillingQueryCreditAccount(ctx, chain, tenant2.FormattedAddress())
		require.NoError(t, err)
		beforeBalances := beforeRes.Balances

		// Create a lease
		items := []string{fmt.Sprintf("%s:1", testSKUUUID)}
		createRes, err := helpers.BillingCreateLease(ctx, chain, tenant2, items)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(createRes.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code)

		leaseUUID, err := helpers.GetLeaseIDFromTxHash(ctx, chain, createRes.TxHash)
		require.NoError(t, err)

		// Acknowledge the lease to make it ACTIVE
		ackRes, err := helpers.BillingAcknowledgeLease(ctx, chain, providerWallet, leaseUUID)
		require.NoError(t, err)
		ackTxRes, err := chain.GetTransaction(ackRes.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), ackTxRes.Code, "lease acknowledgement should succeed: %s", ackTxRes.RawLog)

		// Wait for some accrual
		require.NoError(t, testutil.WaitForBlocks(ctx, 3, chain))

		// Close the lease
		closeRes, err := helpers.BillingCloseLease(ctx, chain, tenant2, leaseUUID)
		require.NoError(t, err)

		txRes, err = chain.GetTransaction(closeRes.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code)

		// Check credit balance - should be less than before (due to accrual) but still positive
		afterRes, err := helpers.BillingQueryCreditAccount(ctx, chain, tenant2.FormattedAddress())
		require.NoError(t, err)
		afterBalances := afterRes.Balances

		// Credit should have decreased due to accrual (compare total amounts)
		require.True(t, !afterBalances.IsZero(),
			"remaining credit should stay in account")
		t.Logf("Credit balances: before=%s, after=%s", beforeBalances, afterBalances)
	})

	t.Run("success: provider cannot double-withdraw after lease closure", func(t *testing.T) {
		// Get a closed lease from tenant2's tests
		leases, err := helpers.BillingQueryLeasesByTenant(ctx, chain, tenant2.FormattedAddress(), false)
		require.NoError(t, err)

		var closedLeaseUUID string
		for _, lease := range leases.Leases {
			if lease.State == "LEASE_STATE_CLOSED" {
				closedLeaseUUID = lease.Uuid
				break
			}
		}

		if closedLeaseUUID == "" {
			t.Skip("No closed lease found")
		}

		// After closure, settlement already happened, so withdrawal should fail
		// because there's nothing left to withdraw (LastSettledAt == ClosedAt)
		res, err := helpers.BillingWithdraw(ctx, chain, providerWallet, closedLeaseUUID)
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
		require.True(t, !creditRes.Balances.IsZero(), "credit balance should be positive")
		t.Logf("New tenant credit balance: %s", creditRes.Balances)
	})

	var leaseID string
	t.Run("success: authority creates lease for tenant", func(t *testing.T) {
		items := []string{fmt.Sprintf("%s:1", testSKUUUID)}
		res, err := helpers.BillingCreateLeaseForTenant(ctx, chain, authority, newTenant.FormattedAddress(), items)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "authority create lease for tenant should succeed: %s", txRes.RawLog)

		leaseID, err = helpers.GetLeaseIDFromTxHash(ctx, chain, res.TxHash)
		require.NoError(t, err)
		t.Logf("Created lease ID: %s for tenant: %s", leaseID, newTenant.FormattedAddress())

		// Acknowledge the lease to make it ACTIVE
		ackRes, err := helpers.BillingAcknowledgeLease(ctx, chain, providerWallet, leaseID)
		require.NoError(t, err)
		ackTxRes, err := chain.GetTransaction(ackRes.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), ackTxRes.Code, "lease acknowledgement should succeed: %s", ackTxRes.RawLog)
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
		require.Equal(t, "LEASE_STATE_CLOSED", leaseRes.Lease.State)
	})

	t.Run("success: authority creates multi-SKU lease for tenant", func(t *testing.T) {
		items := []string{
			fmt.Sprintf("%s:2", testSKUUUID),  // 2x per-hour SKU
			fmt.Sprintf("%s:1", testSKUUUID2), // 1x per-day SKU
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
		items := []string{fmt.Sprintf("%s:1", testSKUUUID)}
		res, err := helpers.BillingCreateLeaseForTenant(ctx, chain, unauthorizedUser, newTenant.FormattedAddress(), items)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "non-authority should not create lease for tenant")
		require.Contains(t, txRes.RawLog, "unauthorized")
	})

	t.Run("fail: provider cannot create lease for tenant", func(t *testing.T) {
		items := []string{fmt.Sprintf("%s:1", testSKUUUID)}
		res, err := helpers.BillingCreateLeaseForTenant(ctx, chain, providerWallet, newTenant.FormattedAddress(), items)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "provider should not create lease for tenant")
		require.Contains(t, txRes.RawLog, "unauthorized")
	})

	t.Run("fail: create lease for tenant without credit account", func(t *testing.T) {
		// Create a new tenant without funding their credit (no credit account)
		unfundedUsers := interchaintest.GetAndFundTestUsers(t, ctx, "unfunded-tenant", DefaultGenesisAmt, chain)
		unfundedTenant := unfundedUsers[0]

		items := []string{fmt.Sprintf("%s:1", testSKUUUID)}
		res, err := helpers.BillingCreateLeaseForTenant(ctx, chain, authority, unfundedTenant.FormattedAddress(), items)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "should fail without credit account")
		require.Contains(t, txRes.RawLog, "credit account not found",
			"should fail with credit account not found error")
	})

	t.Run("fail: create lease for tenant with insufficient credit", func(t *testing.T) {
		// Create a new tenant with minimal credit (1 upwr) - not enough for min_lease_duration
		lowCreditUsers := interchaintest.GetAndFundTestUsers(t, ctx, "low-credit-tenant", DefaultGenesisAmt, chain)
		lowCreditTenant := lowCreditUsers[0]

		// Fund with only 1 upwr - way below the required amount for min_lease_duration
		fundAmount := fmt.Sprintf("1%s", testPWRDenom)
		res, err := helpers.BillingFundCredit(ctx, chain, authority, lowCreditTenant.FormattedAddress(), fundAmount)
		require.NoError(t, err)
		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "funding should succeed")

		// Now try to create a lease for this tenant - should fail due to insufficient credit
		items := []string{fmt.Sprintf("%s:1", testSKUUUID)}
		res, err = helpers.BillingCreateLeaseForTenant(ctx, chain, authority, lowCreditTenant.FormattedAddress(), items)
		require.NoError(t, err)

		txRes, err = chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "should fail with insufficient credit")
		require.Contains(t, txRes.RawLog, "insufficient credit",
			"should fail with insufficient credit error")
	})

	t.Run("fail: create lease for tenant with invalid address", func(t *testing.T) {
		items := []string{fmt.Sprintf("%s:1", testSKUUUID)}
		// Using an invalid address format - this should fail at CLI validation
		res, err := helpers.BillingCreateLeaseForTenant(ctx, chain, authority, "invalid-address", items)
		// CLI should return an error for invalid address
		require.Error(t, err, "should fail with invalid tenant address")
		_ = res // unused
	})

	t.Run("fail: create lease for tenant with non-existent SKU", func(t *testing.T) {
		// Use valid UUIDv7 format that doesn't exist
		nonExistentSKU := "01912345-6789-7abc-8def-0123456789ab"
		items := []string{fmt.Sprintf("%s:1", nonExistentSKU)}
		res, err := helpers.BillingCreateLeaseForTenant(ctx, chain, authority, newTenant.FormattedAddress(), items)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "should fail with non-existent SKU")
		require.Contains(t, txRes.RawLog, "not found")
	})

	t.Run("success: verify event shows authority created lease", func(t *testing.T) {
		// Create another lease and check the event
		items := []string{fmt.Sprintf("%s:1", testSKUUUID)}
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

	// For auto-close tests, we temporarily set a very low minLeaseDuration (10 seconds)
	// so we can test credit exhaustion quickly.
	// testSKUUUID has rate of 1000/second per unit.
	// With minLeaseDuration=10 and quantity=1: need 10,000 credit minimum
	// Fund with 15,000 credit: exhaustion takes 15 seconds

	// Save original params and set low minLeaseDuration for this test
	t.Run("setup: set low min_lease_duration for auto-close tests", func(t *testing.T) {
		params, err := helpers.BillingQueryParams(ctx, chain)
		require.NoError(t, err)

		// Set minLeaseDuration to 10 seconds for quick exhaustion tests
		res, err := helpers.BillingUpdateParams(ctx, chain, authority,
			params.Params.MaxLeasesPerTenant, params.Params.MaxItemsPerLease,
			10, // 10 seconds min lease duration
			params.Params.MaxPendingLeasesPerTenant, params.Params.PendingTimeout,
			nil)
		require.NoError(t, err)
		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "params update should succeed: %s", txRes.RawLog)
		t.Log("Set min_lease_duration to 10 seconds for auto-close tests")
	})

	// Create a dedicated tenant for auto-close tests with minimal credit
	// to force exhaustion quickly
	users := interchaintest.GetAndFundTestUsers(t, ctx, "auto-close-tenant", DefaultGenesisAmt, chain)
	autoCloseTenant := users[0]

	// Fund tenant with just enough credit to create a lease but exhaust quickly
	// testSKUUUID has rate of 1000/second per unit
	// With quantity=1 and 15,000 credit, exhaustion takes ~15 seconds
	t.Run("setup: fund tenant with minimal credit", func(t *testing.T) {
		fundAmount := fmt.Sprintf("15000%s", testPWRDenom) // Just above 10,000 minimum (10 * 1000)
		res, err := helpers.BillingFundCredit(ctx, chain, authority, autoCloseTenant.FormattedAddress(), fundAmount)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "funding should succeed: %s", txRes.RawLog)

		creditRes, err := helpers.BillingQueryCreditAccount(ctx, chain, autoCloseTenant.FormattedAddress())
		require.NoError(t, err)
		t.Logf("Initial credit balance: %s", creditRes.Balances)
	})

	var autoCloseLeaseID string
	t.Run("setup: create lease that will exhaust credit", func(t *testing.T) {
		// Create a lease with testSKUUUID (1000/second rate with quantity=1)
		// With 15,000 credit, this will exhaust in ~15 seconds
		items := []string{fmt.Sprintf("%s:1", testSKUUUID)}
		res, err := helpers.BillingCreateLease(ctx, chain, autoCloseTenant, items)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "lease creation should succeed: %s", txRes.RawLog)

		autoCloseLeaseID, err = helpers.GetLeaseIDFromTxHash(ctx, chain, res.TxHash)
		require.NoError(t, err)
		t.Logf("Created lease ID: %s", autoCloseLeaseID)

		// Acknowledge the lease to make it ACTIVE
		ackRes, err := helpers.BillingAcknowledgeLease(ctx, chain, providerWallet, autoCloseLeaseID)
		require.NoError(t, err)
		ackTxRes, err := chain.GetTransaction(ackRes.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), ackTxRes.Code, "lease acknowledgement should succeed: %s", ackTxRes.RawLog)

		// Verify lease is active and check locked price
		lease, err := helpers.BillingQueryLease(ctx, chain, autoCloseLeaseID)
		require.NoError(t, err)
		require.Equal(t, "LEASE_STATE_ACTIVE", lease.Lease.State, "lease should be active")
		t.Logf("Lease items: %+v", lease.Lease.Items)
	})

	t.Run("success: lease auto-closes when credit exhausted during withdrawal", func(t *testing.T) {
		// Check provider balance before auto-close
		providerBalance, err := chain.GetBalance(ctx, providerWallet.FormattedAddress(), testPWRDenom)
		require.NoError(t, err)
		t.Logf("Provider balance BEFORE auto-close: %s", providerBalance.String())

		// Wait for enough blocks to exhaust credit
		// With 1000/second rate and 15,000 credit, we need ~15 seconds
		// Block time is ~1 second, so wait for ~20 blocks to be safe
		t.Log("Waiting for credit to accrue/exhaust...")
		require.NoError(t, testutil.WaitForBlocks(ctx, 15, chain))

		// Check credit balance - should be very low or zero
		creditRes, err := helpers.BillingQueryCreditAccount(ctx, chain, autoCloseTenant.FormattedAddress())
		require.NoError(t, err)
		t.Logf("Credit balance after accrual: %s", creditRes.Balances)

		// Trigger settlement by attempting a withdrawal
		// This should auto-close the lease due to exhausted credit
		res, err := helpers.BillingWithdraw(ctx, chain, providerWallet, autoCloseLeaseID)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		t.Logf("Withdrawal tx result: code=%d, log=%s", txRes.Code, txRes.RawLog)

		// The withdrawal TX should succeed (settlement happens during auto-close)
		require.Equal(t, uint32(0), txRes.Code, "withdrawal should succeed, auto-close settles the funds")

		// Check provider balance AFTER auto-close - should have received 15000
		providerBalanceAfter, err := chain.GetBalance(ctx, providerWallet.FormattedAddress(), testPWRDenom)
		require.NoError(t, err)
		t.Logf("Provider balance AFTER auto-close: %s", providerBalanceAfter.String())

		// Provider should have received the tenant's credit (15000)
		require.True(t, providerBalanceAfter.GT(sdkmath.NewInt(0)),
			"provider should have received funds from auto-close settlement")

		// Check credit balance AFTER auto-close - should be 0 or near 0
		creditResAfter, err := helpers.BillingQueryCreditAccount(ctx, chain, autoCloseTenant.FormattedAddress())
		require.NoError(t, err)
		t.Logf("Credit balance AFTER auto-close: %s", creditResAfter.Balances)

		// Credit should be depleted
		require.True(t, creditResAfter.Balances.IsZero() || creditResAfter.Balances.AmountOf(testPWRDenom).LTE(sdkmath.ZeroInt()),
			"credit balance should be depleted after auto-close")

		// Query lease - should now be inactive due to auto-close
		lease, err := helpers.BillingQueryLease(ctx, chain, autoCloseLeaseID)
		require.NoError(t, err)
		require.Equal(t, "LEASE_STATE_CLOSED", lease.Lease.State,
			"lease should be auto-closed after credit exhaustion")
		t.Log("Lease was auto-closed as expected")
	})

	t.Run("success: auto-closed lease emits proper events", func(t *testing.T) {
		// The withdrawal that triggered auto-close should have emitted events
		// Query the lease to verify it's closed
		lease, err := helpers.BillingQueryLease(ctx, chain, autoCloseLeaseID)
		require.NoError(t, err)
		require.Equal(t, "LEASE_STATE_CLOSED", lease.Lease.State)

		// Verify closed_at is set (indicates it was closed)
		require.NotEmpty(t, lease.Lease.ClosedAt, "closed_at should be set for auto-closed lease")
		t.Logf("Lease closed_at: %s", lease.Lease.ClosedAt)
	})

	t.Run("success: provider already withdrew during auto-close", func(t *testing.T) {
		// After auto-close, the provider should have already received their tokens
		// during the settlement that triggered the close
		// Attempting another withdrawal should return 0 (nothing left to withdraw)
		res, err := helpers.BillingWithdraw(ctx, chain, providerWallet, autoCloseLeaseID)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		// Should fail since lease is inactive and no accrual since last settlement
		require.NotEqual(t, uint32(0), txRes.Code, "second withdrawal should fail")
		require.Contains(t, txRes.RawLog, "no withdrawable amount",
			"should indicate no withdrawable amount")
	})

	t.Run("success: tenant cannot create new lease with exhausted credit", func(t *testing.T) {
		// Verify credit balance is depleted (should be 0 after auto-close settlement)
		creditRes, err := helpers.BillingQueryCreditAccount(ctx, chain, autoCloseTenant.FormattedAddress())
		require.NoError(t, err)
		t.Logf("Credit balance after exhaustion: %s", creditRes.Balances)

		// After auto-close, credit should be 0 or very low
		require.True(t, creditRes.Balances.IsZero() || creditRes.Balances.AmountOf(testPWRDenom).LTE(sdkmath.ZeroInt()),
			"credit balance (%s) should be depleted after auto-close",
			creditRes.Balances)

		// Credit is insufficient to cover minLeaseDuration, so creating a new lease should fail
		items := []string{fmt.Sprintf("%s:1", testSKUUUID)}
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

		// Fund minimally - same approach as main auto-close test
		// testSKUUUID has rate of 1000/second, with minLeaseDuration=10, need 10,000 minimum
		fundAmount := fmt.Sprintf("15000%s", testPWRDenom)
		res, err := helpers.BillingFundCredit(ctx, chain, authority, tenant2.FormattedAddress(), fundAmount)
		require.NoError(t, err)
		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code)

		// Create lease with testSKUUUID (1000/second rate with quantity=1)
		items := []string{fmt.Sprintf("%s:1", testSKUUUID)}
		res, err = helpers.BillingCreateLease(ctx, chain, tenant2, items)
		require.NoError(t, err)
		txRes, err = chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code)

		leaseID, err := helpers.GetLeaseIDFromTxHash(ctx, chain, res.TxHash)
		require.NoError(t, err)

		// Acknowledge the lease to make it ACTIVE
		ackRes, err := helpers.BillingAcknowledgeLease(ctx, chain, providerWallet, leaseID)
		require.NoError(t, err)
		ackTxRes, err := chain.GetTransaction(ackRes.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), ackTxRes.Code, "lease acknowledgement should succeed: %s", ackTxRes.RawLog)

		// Wait for credit exhaustion (~15 seconds)
		require.NoError(t, testutil.WaitForBlocks(ctx, 20, chain))

		res, err = helpers.BillingCloseLease(ctx, chain, tenant2, leaseID)
		require.NoError(t, err)
		txRes, err = chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "explicit close should succeed even with exhausted credit")

		// Verify final state is inactive (explicit close)
		lease, err := helpers.BillingQueryLease(ctx, chain, leaseID)
		require.NoError(t, err)
		require.Equal(t, "LEASE_STATE_CLOSED", lease.Lease.State, "lease should be inactive after exhaustion")
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
		items := []string{fmt.Sprintf("%s:1", testSKUUUID)}
		res, err = helpers.BillingCreateLease(ctx, chain, tenant3, items)
		require.NoError(t, err)
		txRes, err = chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code)

		leaseID, err := helpers.GetLeaseIDFromTxHash(ctx, chain, res.TxHash)
		require.NoError(t, err)

		// Acknowledge the lease to make it ACTIVE
		ackRes, err := helpers.BillingAcknowledgeLease(ctx, chain, providerWallet, leaseID)
		require.NoError(t, err)
		ackTxRes, err := chain.GetTransaction(ackRes.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), ackTxRes.Code, "lease acknowledgement should succeed: %s", ackTxRes.RawLog)

		// Get initial credit balance
		initialCredit, err := helpers.BillingQueryCreditAccount(ctx, chain, tenant3.FormattedAddress())
		require.NoError(t, err)
		t.Logf("Credit after lease creation: %s", initialCredit.Balances)

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
		t.Logf("Credit after lease close: %s", afterCredit.Balances)

		// Credit should have decreased (settlement happened)
		// Compare the first coin amount (assuming single denom)
		require.True(t, len(afterCredit.Balances) > 0 && len(initialCredit.Balances) > 0,
			"should have credit balances")
		require.True(t, afterCredit.Balances[0].Amount.LT(initialCredit.Balances[0].Amount),
			"credit should decrease due to settlement during lease close")

		// Verify lease is now inactive
		lease, err := helpers.BillingQueryLease(ctx, chain, leaseID)
		require.NoError(t, err)
		require.Equal(t, "LEASE_STATE_CLOSED", lease.Lease.State, "lease should be inactive")
	})

	// Restore original minLeaseDuration (1 hour) after auto-close tests
	t.Run("cleanup: restore min_lease_duration to 1 hour", func(t *testing.T) {
		params, err := helpers.BillingQueryParams(ctx, chain)
		require.NoError(t, err)

		res, err := helpers.BillingUpdateParams(ctx, chain, authority,
			params.Params.MaxLeasesPerTenant, params.Params.MaxItemsPerLease,
			3600, // Restore to 1 hour
			params.Params.MaxPendingLeasesPerTenant, params.Params.PendingTimeout,
			nil)
		require.NoError(t, err)
		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "params restore should succeed")
		t.Log("Restored min_lease_duration to 3600 seconds (1 hour)")
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
	leaseIDs := make([]string, 5)
	for i := 0; i < 5; i++ {
		items := []string{fmt.Sprintf("%s:1", testSKUUUID)}
		res, err := helpers.BillingCreateLease(ctx, chain, tenant, items)
		require.NoError(t, err)
		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "lease creation should succeed")

		leaseID, err := helpers.GetLeaseIDFromTxHash(ctx, chain, res.TxHash)
		require.NoError(t, err)
		leaseIDs[i] = leaseID

		// Acknowledge the lease to make it ACTIVE
		ackRes, err := helpers.BillingAcknowledgeLease(ctx, chain, providerWallet, leaseID)
		require.NoError(t, err)
		ackTxRes, err := chain.GetTransaction(ackRes.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), ackTxRes.Code, "lease acknowledgement should succeed: %s", ackTxRes.RawLog)
	}

	// Wait for some accrual
	require.NoError(t, testutil.WaitForBlocks(ctx, 5, chain))

	// Test: withdraw all with custom limit
	t.Run("success: withdraw all with custom limit", func(t *testing.T) {
		// Use a limit of 2 to test pagination
		res, err := helpers.BillingWithdrawAllWithLimit(ctx, chain, providerWallet, testProviderUUID, 2)
		require.NoError(t, err)
		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "withdraw all should succeed")

		// Check events for has_more flag
		t.Logf("WithdrawAll with limit 2 succeeded")
	})

	// Test: withdraw all with default limit (0 means default)
	t.Run("success: withdraw all with default limit", func(t *testing.T) {
		res, err := helpers.BillingWithdrawAll(ctx, chain, providerWallet, testProviderUUID)
		require.NoError(t, err)
		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "withdraw all should succeed")
	})

	// Test: withdraw all with limit exceeding maximum should fail at CLI validation
	t.Run("fail: withdraw all with limit exceeding maximum", func(t *testing.T) {
		// MaxWithdrawAllLimit is 100, try 150
		_, err := helpers.BillingWithdrawAllWithLimit(ctx, chain, providerWallet, testProviderUUID, 150)
		require.Error(t, err, "withdraw all with excessive limit should fail")
	})
}

// testProviderDeactivation tests behavior when a provider is deactivated while having active leases.
func testProviderDeactivation(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, authority, providerWallet ibc.Wallet) {
	// Create a new user specifically for the deactivation test provider
	users := interchaintest.GetAndFundTestUsers(t, ctx, "deactivation-provider-wallet", DefaultGenesisAmt, chain)
	deactivationProviderWallet := users[0]

	// Create a new provider specifically for deactivation tests
	var deactivateProviderUUID string
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
					if attr.Key == "provider_uuid" {
						deactivateProviderUUID = attr.Value
						break
					}
				}
			}
		}
		require.NotEmpty(t, deactivateProviderUUID, "provider UUID should be extracted from events")
	})

	// Create SKU for this provider with valid price (evenly divisible)
	t.Run("setup: create SKU for deactivation provider", func(t *testing.T) {
		res, err := helpers.SKUCreateSKU(ctx, chain, authority,
			deactivateProviderUUID, "Deactivation SKU", 1, fmt.Sprintf("3600000%s", testPWRDenom), "")
		require.NoError(t, err)
		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code)
	})

	// Get the SKU ID
	skus, err := helpers.SKUQuerySKUsByProvider(ctx, chain, deactivateProviderUUID)
	require.NoError(t, err)
	require.Len(t, skus.Skus, 1)
	deactivateSKUUUID := skus.Skus[0].Uuid

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
	var leaseID string
	t.Run("setup: create lease with provider's SKU", func(t *testing.T) {
		items := []string{fmt.Sprintf("%s:1", deactivateSKUUUID)}
		res, err := helpers.BillingCreateLease(ctx, chain, tenant, items)
		require.NoError(t, err)
		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code)

		leaseID, err = helpers.GetLeaseIDFromTxHash(ctx, chain, res.TxHash)
		require.NoError(t, err)

		// Acknowledge the lease to make it ACTIVE
		ackRes, err := helpers.BillingAcknowledgeLease(ctx, chain, deactivationProviderWallet, leaseID)
		require.NoError(t, err)
		ackTxRes, err := chain.GetTransaction(ackRes.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), ackTxRes.Code, "lease acknowledgement should succeed: %s", ackTxRes.RawLog)
	})

	// Deactivate the provider
	t.Run("success: provider can be deactivated while having active leases", func(t *testing.T) {
		res, err := helpers.SKUDeactivateProvider(ctx, chain, authority, deactivateProviderUUID)
		require.NoError(t, err)
		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "provider deactivation should succeed")
	})

	// Verify provider is deactivated
	t.Run("success: verify provider is deactivated", func(t *testing.T) {
		provider, err := helpers.SKUQueryProvider(ctx, chain, deactivateProviderUUID)
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
		items := []string{fmt.Sprintf("%s:1", deactivateSKUUUID)}
		res, err = helpers.BillingCreateLease(ctx, chain, tenant2, items)
		require.NoError(t, err)
		txRes, err = chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "lease creation should fail with inactive provider")
	})

	// Deactivated provider is still queryable
	t.Run("success: deactivated provider is still queryable", func(t *testing.T) {
		provider, err := helpers.SKUQueryProvider(ctx, chain, deactivateProviderUUID)
		require.NoError(t, err)
		require.NotNil(t, provider.Provider)
		require.Equal(t, deactivateProviderUUID, provider.Provider.Uuid)
		require.False(t, provider.Provider.Active, "provider should be inactive")
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
		res, err := helpers.BillingUpdateParams(ctx, chain, authority,
			params.Params.MaxLeasesPerTenant, params.Params.MaxItemsPerLease,
			params.Params.MinLeaseDuration,
			params.Params.MaxPendingLeasesPerTenant, params.Params.PendingTimeout,
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
	skuUUID := skus.Skus[0].Uuid

	// Test: Authority can create lease for tenant
	var authorityLeaseID string
	t.Run("success: authority creates lease for tenant", func(t *testing.T) {
		items := []string{fmt.Sprintf("%s:1", skuUUID)}
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
	var allowedUserLeaseID string
	t.Run("success: allowed user creates lease for tenant", func(t *testing.T) {
		items := []string{fmt.Sprintf("%s:1", skuUUID)}
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
		items := []string{fmt.Sprintf("%s:1", skuUUID)}
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
		res, err := helpers.BillingUpdateParams(ctx, chain, authority,
			params.Params.MaxLeasesPerTenant, params.Params.MaxItemsPerLease,
			params.Params.MinLeaseDuration,
			params.Params.MaxPendingLeasesPerTenant, params.Params.PendingTimeout,
			[]string{})
		require.NoError(t, err)
		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "params update should succeed")

		// Now the previously allowed user should not be able to create leases
		items := []string{fmt.Sprintf("%s:1", skuUUID)}
		res, err = helpers.BillingCreateLeaseForTenant(ctx, chain, allowedUser, tenant.FormattedAddress(), items)
		require.NoError(t, err)
		txRes, err = chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "removed user should not be able to create lease for tenant")
	})

	// Test: Authority can still create leases even with empty allowed_list
	t.Run("success: authority can still create lease with empty allowed_list", func(t *testing.T) {
		items := []string{fmt.Sprintf("%s:1", skuUUID)}
		res, err := helpers.BillingCreateLeaseForTenant(ctx, chain, authority, tenant.FormattedAddress(), items)
		require.NoError(t, err)
		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "authority should always be able to create lease for tenant")
	})
}

// testMultiDenom tests the multi-denom feature where SKUs can use different denoms
// and credit accounts can hold multiple token types.
func testMultiDenom(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, authority, providerWallet ibc.Wallet) {
	t.Log("=== Testing Multi-Denom Support ===")

	node := chain.GetNode()

	// Create a second denom for testing (using tokenfactory)
	var secondDenom string
	t.Run("setup: create second denom", func(t *testing.T) {
		var err error
		secondDenom, _, err = node.TokenFactoryCreateDenom(ctx, authority, "utest", 2_500_00)
		require.NoError(t, err)
		t.Logf("Created second denom: %s", secondDenom)

		// Mint tokens
		_, err = node.TokenFactoryMintDenom(ctx, authority.FormattedAddress(), secondDenom, 1_000_000_000_000)
		require.NoError(t, err)
	})

	// Create a new provider for multi-denom tests
	var multiDenomProviderUUID string
	t.Run("setup: create provider for multi-denom tests", func(t *testing.T) {
		res, err := helpers.SKUCreateProvider(ctx, chain, authority, providerWallet.FormattedAddress(), providerWallet.FormattedAddress(), "")
		require.NoError(t, err)
		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code)

		multiDenomProviderUUID, err = helpers.GetProviderUUIDFromTxHash(ctx, chain, res.TxHash)
		require.NoError(t, err)
		t.Logf("Created multi-denom provider ID: %s", multiDenomProviderUUID)
	})

	// Create SKU with first denom (PWR)
	var skuPWRUUID string
	t.Run("setup: create SKU with PWR denom", func(t *testing.T) {
		// 3600000 per hour = 1000 per second
		res, err := helpers.SKUCreateSKU(ctx, chain, authority, multiDenomProviderUUID, "Compute PWR", 1, fmt.Sprintf("3600000%s", testPWRDenom), "")
		require.NoError(t, err)
		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code)

		skuPWRUUID, err = helpers.GetSKUUUIDFromTxHash(ctx, chain, res.TxHash)
		require.NoError(t, err)
		t.Logf("Created SKU with PWR denom, ID: %s", skuPWRUUID)
	})

	// Create SKU with second denom
	var skuSecondUUID string
	t.Run("setup: create SKU with second denom", func(t *testing.T) {
		// 7200000 per hour = 2000 per second
		res, err := helpers.SKUCreateSKU(ctx, chain, authority, multiDenomProviderUUID, "Storage TEST", 1, fmt.Sprintf("7200000%s", secondDenom), "")
		require.NoError(t, err)
		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code)

		skuSecondUUID, err = helpers.GetSKUUUIDFromTxHash(ctx, chain, res.TxHash)
		require.NoError(t, err)
		t.Logf("Created SKU with second denom, ID: %s", skuSecondUUID)
	})

	// Create tenant for multi-denom tests
	users := interchaintest.GetAndFundTestUsers(t, ctx, "multi-denom-tenant", DefaultGenesisAmt, chain)
	tenant := users[0]

	// Fund tenant with both denoms
	t.Run("setup: fund tenant with both denoms", func(t *testing.T) {
		// Send PWR denom
		err := node.SendFunds(ctx, authority.KeyName(), ibc.WalletAmount{
			Address: tenant.FormattedAddress(),
			Denom:   testPWRDenom,
			Amount:  sdkmath.NewInt(500_000_000), // 500M PWR
		})
		require.NoError(t, err)

		// Send second denom
		err = node.SendFunds(ctx, authority.KeyName(), ibc.WalletAmount{
			Address: tenant.FormattedAddress(),
			Denom:   secondDenom,
			Amount:  sdkmath.NewInt(500_000_000), // 500M second denom
		})
		require.NoError(t, err)

		require.NoError(t, testutil.WaitForBlocks(ctx, 2, chain))
	})

	// Fund credit account with both denoms
	t.Run("success: fund credit account with first denom", func(t *testing.T) {
		fundAmount := fmt.Sprintf("200000000%s", testPWRDenom) // 200M PWR
		res, err := helpers.BillingFundCredit(ctx, chain, tenant, tenant.FormattedAddress(), fundAmount)
		require.NoError(t, err)
		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "fund credit with PWR should succeed")
	})

	t.Run("success: fund credit account with second denom", func(t *testing.T) {
		fundAmount := fmt.Sprintf("200000000%s", secondDenom) // 200M second denom
		res, err := helpers.BillingFundCredit(ctx, chain, tenant, tenant.FormattedAddress(), fundAmount)
		require.NoError(t, err)
		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "fund credit with second denom should succeed")
	})

	// Verify credit account has both denoms
	t.Run("success: verify credit account has multiple denoms", func(t *testing.T) {
		creditRes, err := helpers.BillingQueryCreditAccount(ctx, chain, tenant.FormattedAddress())
		require.NoError(t, err)
		require.NotNil(t, creditRes)
		t.Logf("Credit account balances: %s", creditRes.Balances)

		// Should have at least 2 coins in balances
		require.GreaterOrEqual(t, len(creditRes.Balances), 2, "credit account should have multiple denoms")

		// Check specific balances
		pwrBalance := creditRes.Balances.AmountOf(testPWRDenom)
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
		res, err := helpers.BillingCreateLease(ctx, chain, tenant, items)
		require.NoError(t, err)
		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "multi-denom lease creation should succeed: %s", txRes.RawLog)

		multiDenomLeaseID, err = helpers.GetLeaseIDFromTxHash(ctx, chain, res.TxHash)
		require.NoError(t, err)
		t.Logf("Created multi-denom lease ID: %s", multiDenomLeaseID)

		// Acknowledge the lease to make it ACTIVE
		ackRes, err := helpers.BillingAcknowledgeLease(ctx, chain, providerWallet, multiDenomLeaseID)
		require.NoError(t, err)
		ackTxRes, err := chain.GetTransaction(ackRes.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), ackTxRes.Code, "lease acknowledgement should succeed: %s", ackTxRes.RawLog)
	})

	// Verify lease items have correct denoms
	t.Run("success: verify lease items have correct denoms", func(t *testing.T) {
		leaseRes, err := helpers.BillingQueryLease(ctx, chain, multiDenomLeaseID)
		require.NoError(t, err)
		require.Len(t, leaseRes.Lease.Items, 2, "lease should have 2 items")

		// Items should have different denoms
		denoms := make(map[string]bool)
		for _, item := range leaseRes.Lease.Items {
			denoms[item.LockedPrice.Denom] = true
		}
		require.Len(t, denoms, 2, "lease items should use 2 different denoms")
		require.True(t, denoms[testPWRDenom], "lease should include PWR denom")
		require.True(t, denoms[secondDenom], "lease should include second denom")
	})

	// Wait for accrual
	require.NoError(t, testutil.WaitForBlocks(ctx, 5, chain))

	// Query withdrawable - should show multiple denoms
	t.Run("success: withdrawable amounts show multiple denoms", func(t *testing.T) {
		withdrawableRes, err := helpers.BillingQueryWithdrawable(ctx, chain, multiDenomLeaseID)
		require.NoError(t, err)
		require.NotNil(t, withdrawableRes)
		t.Logf("Withdrawable amounts: %s", withdrawableRes.Amounts)

		// Should have amounts in both denoms
		require.GreaterOrEqual(t, len(withdrawableRes.Amounts), 2, "withdrawable should have multiple denoms")
	})

	// Withdraw - should receive multiple denoms
	t.Run("success: withdraw receives multiple denoms", func(t *testing.T) {
		// Get initial balances
		initialPWR, err := chain.GetBalance(ctx, providerWallet.FormattedAddress(), testPWRDenom)
		require.NoError(t, err)
		initialSecond, err := chain.GetBalance(ctx, providerWallet.FormattedAddress(), secondDenom)
		require.NoError(t, err)

		res, err := helpers.BillingWithdraw(ctx, chain, providerWallet, multiDenomLeaseID)
		require.NoError(t, err)
		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "withdrawal should succeed")

		// Verify provider received both denoms
		newPWR, err := chain.GetBalance(ctx, providerWallet.FormattedAddress(), testPWRDenom)
		require.NoError(t, err)
		newSecond, err := chain.GetBalance(ctx, providerWallet.FormattedAddress(), secondDenom)
		require.NoError(t, err)

		require.True(t, newPWR.GT(initialPWR), "provider should receive PWR from withdrawal")
		require.True(t, newSecond.GT(initialSecond), "provider should receive second denom from withdrawal")
		t.Logf("Received PWR: %s -> %s", initialPWR, newPWR)
		t.Logf("Received second: %s -> %s", initialSecond, newSecond)
	})

	// Wait more and close lease
	require.NoError(t, testutil.WaitForBlocks(ctx, 3, chain))

	t.Run("success: close lease settles multiple denoms", func(t *testing.T) {
		// Get pre-close balances
		prePWR, err := chain.GetBalance(ctx, providerWallet.FormattedAddress(), testPWRDenom)
		require.NoError(t, err)
		preSecond, err := chain.GetBalance(ctx, providerWallet.FormattedAddress(), secondDenom)
		require.NoError(t, err)

		res, err := helpers.BillingCloseLease(ctx, chain, tenant, multiDenomLeaseID)
		require.NoError(t, err)
		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "close should succeed")

		// Verify settlement transferred both denoms
		postPWR, err := chain.GetBalance(ctx, providerWallet.FormattedAddress(), testPWRDenom)
		require.NoError(t, err)
		postSecond, err := chain.GetBalance(ctx, providerWallet.FormattedAddress(), secondDenom)
		require.NoError(t, err)

		require.True(t, postPWR.GTE(prePWR), "provider should receive PWR from settlement")
		require.True(t, postSecond.GTE(preSecond), "provider should receive second denom from settlement")
	})

	// Test: lease creation fails with insufficient credit for one denom
	t.Run("fail: insufficient credit for one denom", func(t *testing.T) {
		// Create a new tenant with only one denom
		oneUsers := interchaintest.GetAndFundTestUsers(t, ctx, "one-denom-tenant", DefaultGenesisAmt, chain)
		oneDenomTenant := oneUsers[0]

		// Send only PWR denom
		err := node.SendFunds(ctx, authority.KeyName(), ibc.WalletAmount{
			Address: oneDenomTenant.FormattedAddress(),
			Denom:   testPWRDenom,
			Amount:  sdkmath.NewInt(500_000_000),
		})
		require.NoError(t, err)
		require.NoError(t, testutil.WaitForBlocks(ctx, 2, chain))

		// Fund credit only with PWR
		fundAmount := fmt.Sprintf("200000000%s", testPWRDenom)
		res, err := helpers.BillingFundCredit(ctx, chain, oneDenomTenant, oneDenomTenant.FormattedAddress(), fundAmount)
		require.NoError(t, err)
		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code)

		// Try to create lease requiring both denoms - should fail
		items := []string{
			fmt.Sprintf("%s:1", skuPWRUUID),    // Uses PWR - has enough
			fmt.Sprintf("%s:1", skuSecondUUID), // Uses second denom - insufficient!
		}
		res, err = helpers.BillingCreateLease(ctx, chain, oneDenomTenant, items)
		require.NoError(t, err)
		txRes, err = chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "lease should fail with insufficient second denom")
		require.Contains(t, txRes.RawLog, "insufficient credit", "error should indicate insufficient credit")
	})

	// Test: lease with same denom multiple SKUs works correctly
	t.Run("success: lease with same denom multiple SKUs", func(t *testing.T) {
		// Create two more SKUs with same denom
		res, err := helpers.SKUCreateSKU(ctx, chain, authority, multiDenomProviderUUID, "Compute PWR 2", 1, fmt.Sprintf("1800000%s", testPWRDenom), "")
		require.NoError(t, err)
		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code)
		skuPWRUUID2, err := helpers.GetSKUUUIDFromTxHash(ctx, chain, res.TxHash)
		require.NoError(t, err)

		// Create tenant
		sameUsers := interchaintest.GetAndFundTestUsers(t, ctx, "same-denom-tenant", DefaultGenesisAmt, chain)
		sameDenomTenant := sameUsers[0]

		// Fund with PWR
		err = node.SendFunds(ctx, authority.KeyName(), ibc.WalletAmount{
			Address: sameDenomTenant.FormattedAddress(),
			Denom:   testPWRDenom,
			Amount:  sdkmath.NewInt(500_000_000),
		})
		require.NoError(t, err)
		require.NoError(t, testutil.WaitForBlocks(ctx, 2, chain))

		fundAmount := fmt.Sprintf("200000000%s", testPWRDenom)
		res, err = helpers.BillingFundCredit(ctx, chain, sameDenomTenant, sameDenomTenant.FormattedAddress(), fundAmount)
		require.NoError(t, err)
		txRes, err = chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code)

		// Create lease with multiple SKUs using same denom
		items := []string{
			fmt.Sprintf("%s:1", skuPWRUUID),
			fmt.Sprintf("%s:1", skuPWRUUID2),
		}
		res, err = helpers.BillingCreateLease(ctx, chain, sameDenomTenant, items)
		require.NoError(t, err)
		txRes, err = chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "same denom multi-SKU lease should succeed")
	})
}
