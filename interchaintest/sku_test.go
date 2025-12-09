// Package interchaintest contains end-to-end tests for the SKU module.
//
// # Test Coverage
//
// TestSKU is the main test function that runs all SKU module e2e tests in sequence.
// Tests are run against a live chain using interchaintest framework.
//
// ## Query Tests
//
// testSKUQueryParams:
//   - Verifies default params are returned (empty allowed list)
//
// ## Provider Tests
//
// testProviderCreate:
//   - Success: authority creates provider
//   - Success: authority creates provider with meta-hash
//   - Fail: unauthorized user creates provider
//
// testProviderQuery:
//   - Success: query existing provider by ID
//   - Success: query all providers
//
// testProviderUpdate:
//   - Success: authority updates provider (payout address)
//   - Fail: unauthorized user updates provider
//   - Fail: update non-existent provider
//
// testProviderDeactivate:
//   - Fail: unauthorized user deactivates provider
//   - Success: authority deactivates provider (soft delete, remains queryable)
//   - Fail: deactivate already inactive provider
//   - Fail: deactivate non-existent provider
//
// ## SKU Tests
//
// testSKUCreate:
//   - Success: authority creates SKU
//   - Success: authority creates SKU with meta-hash
//   - Fail: unauthorized user creates SKU
//   - Fail: create SKU with non-existent provider
//   - Fail: create SKU with zero provider_id
//   - Fail: create SKU with inactive provider
//
// testSKUQuery:
//   - Success: query existing SKU by ID
//   - Success: query all SKUs
//
// testSKUUpdate:
//   - Success: authority updates SKU (name, price)
//   - Success: authority deactivates SKU via update (active=false)
//   - Fail: unauthorized user updates SKU
//   - Fail: update with wrong provider_id (mismatch)
//   - Fail: update non-existent SKU
//   - Fail: update SKU with zero provider_id
//
// testSKUDeactivate:
//   - Fail: unauthorized user deactivates SKU
//   - Success: authority deactivates SKU (soft delete, remains queryable)
//   - Fail: deactivate already inactive SKU
//   - Fail: deactivate non-existent SKU
//
// ## Params Tests
//
// testSKUUpdateParams:
//   - Fail: unauthorized user updates params
//   - Success: authority updates params with allowed list
//   - Success: authority clears allowed list
//
// ## Allowed List Tests
//
// testSKUAllowedListOperations:
//   - Success: allowed user creates SKU
//   - Fail: non-allowed user creates SKU
//   - Success: allowed user updates SKU
//   - Fail: non-allowed user updates SKU
//   - Success: allowed user deactivates SKU
//   - Fail: allowed user cannot update params (only authority can)
//
// ## Query by Provider Tests
//
// testSKUQueryByProvider:
//   - Success: query SKUs filtered by provider ID
//   - Success: query SKUs for non-existent provider returns empty list
//
// ## Pagination Tests
//
// testSKUPagination:
//   - Success: paginate all SKUs with limit and page-key
//   - Success: paginate SKUs by provider with limit and page-key
//   - Success: verify no duplicates across pages
//   - Success: large limit returns all results
package interchaintest

import (
	"context"
	"testing"

	"github.com/strangelove-ventures/interchaintest/v8"
	"github.com/strangelove-ventures/interchaintest/v8/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v8/dockerutil"
	"github.com/strangelove-ventures/interchaintest/v8/ibc"
	"github.com/stretchr/testify/require"

	"github.com/manifest-network/manifest-ledger/interchaintest/helpers"
)

func TestSKU(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	// Setup chain
	name := "sku-test"
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

	// Regular users
	users := interchaintest.GetAndFundTestUsers(t, ctx, t.Name(), DefaultGenesisAmt, chain, chain)
	user1 := users[0]
	user2 := users[1]

	// Run test cases
	t.Run("QueryParams", func(t *testing.T) {
		testSKUQueryParams(t, ctx, chain)
	})

	// Provider tests
	var providerID uint64
	t.Run("CreateProvider", func(t *testing.T) {
		providerID = testProviderCreate(t, ctx, chain, authority, user1)
	})

	t.Run("QueryProvider", func(t *testing.T) {
		testProviderQuery(t, ctx, chain, providerID)
	})

	t.Run("UpdateProvider", func(t *testing.T) {
		testProviderUpdate(t, ctx, chain, authority, user1, providerID)
	})

	// SKU tests
	var skuID uint64
	t.Run("CreateSKU", func(t *testing.T) {
		skuID = testSKUCreate(t, ctx, chain, authority, user1, providerID)
	})

	t.Run("QuerySKU", func(t *testing.T) {
		testSKUQuery(t, ctx, chain, skuID, providerID)
	})

	t.Run("UpdateSKU", func(t *testing.T) {
		testSKUUpdate(t, ctx, chain, authority, user1, skuID, providerID)
	})

	t.Run("DeactivateSKU", func(t *testing.T) {
		testSKUDeactivate(t, ctx, chain, authority, user1, providerID)
	})

	t.Run("DeactivateProvider", func(t *testing.T) {
		testProviderDeactivate(t, ctx, chain, authority, user1)
	})

	t.Run("UpdateParams", func(t *testing.T) {
		testSKUUpdateParams(t, ctx, chain, authority, user1)
	})

	t.Run("AllowedListOperations", func(t *testing.T) {
		testSKUAllowedListOperations(t, ctx, chain, authority, user1, user2)
	})

	t.Run("QuerySKUsByProvider", func(t *testing.T) {
		testSKUQueryByProvider(t, ctx, chain, authority)
	})

	t.Run("Pagination", func(t *testing.T) {
		testSKUPagination(t, ctx, chain, authority)
	})

	t.Cleanup(func() {
		dockerutil.CopyCoverageFromContainer(ctx, t, client, chain.GetNode().ContainerID(), chain.HomeDir(), ExternalGoCoverDir)
		_ = ic.Close()
	})
}

func testSKUQueryParams(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain) {
	t.Log("=== Testing SKU Query Params ===")

	res, err := helpers.SKUQueryParams(ctx, chain)
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Empty(t, res.Params.AllowedList, "default allowed list should be empty")
}

func testProviderCreate(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, authority, user1 ibc.Wallet) uint64 {
	t.Log("=== Testing Provider Create ===")

	address := authority.FormattedAddress()
	payoutAddress := authority.FormattedAddress()

	var providerID uint64

	t.Run("success: authority creates provider", func(t *testing.T) {
		res, err := helpers.SKUCreateProvider(ctx, chain, authority, address, payoutAddress, "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "tx should succeed: %s", txRes.RawLog)

		// Verify provider was created by querying it
		providerID, err = helpers.GetProviderIDFromTxHash(ctx, chain, res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint64(1), providerID, "first provider should have ID 1")

		providerRes, err := helpers.SKUQueryProvider(ctx, chain, providerID)
		require.NoError(t, err)
		require.Equal(t, address, providerRes.Provider.Address)
		require.True(t, providerRes.Provider.Active)
	})

	t.Run("success: authority creates provider with meta-hash", func(t *testing.T) {
		res, err := helpers.SKUCreateProvider(ctx, chain, authority, address, payoutAddress, "deadbeef")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "tx should succeed: %s", txRes.RawLog)
	})

	t.Run("fail: unauthorized user creates provider", func(t *testing.T) {
		res, err := helpers.SKUCreateProvider(ctx, chain, user1, address, payoutAddress, "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "tx should fail")
		require.Contains(t, txRes.RawLog, "unauthorized")
	})

	return providerID
}

func testProviderQuery(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, providerID uint64) {
	t.Log("=== Testing Provider Query ===")

	t.Run("success: query existing provider", func(t *testing.T) {
		res, err := helpers.SKUQueryProvider(ctx, chain, providerID)
		require.NoError(t, err)
		require.Equal(t, providerID, res.Provider.Id)
	})

	t.Run("success: query all providers", func(t *testing.T) {
		res, err := helpers.SKUQueryProviders(ctx, chain)
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(res.Providers), 1, "should have at least 1 provider")
	})
}

func testProviderUpdate(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, authority, user1 ibc.Wallet, providerID uint64) {
	t.Log("=== Testing Provider Update ===")

	address := authority.FormattedAddress()
	newPayoutAddress := user1.FormattedAddress()

	t.Run("success: authority updates provider", func(t *testing.T) {
		res, err := helpers.SKUUpdateProvider(ctx, chain, authority, providerID, address, newPayoutAddress, true, "cafebabe")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "tx should succeed: %s", txRes.RawLog)

		// Verify update
		providerRes, err := helpers.SKUQueryProvider(ctx, chain, providerID)
		require.NoError(t, err)
		require.Equal(t, newPayoutAddress, providerRes.Provider.PayoutAddress)
	})

	t.Run("fail: unauthorized user updates provider", func(t *testing.T) {
		res, err := helpers.SKUUpdateProvider(ctx, chain, user1, providerID, address, newPayoutAddress, true, "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "tx should fail")
		require.Contains(t, txRes.RawLog, "unauthorized")
	})

	t.Run("fail: update non-existent provider", func(t *testing.T) {
		res, err := helpers.SKUUpdateProvider(ctx, chain, authority, 999, address, newPayoutAddress, true, "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "tx should fail")
		require.Contains(t, txRes.RawLog, "not found")
	})
}

func testProviderDeactivate(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, authority, user1 ibc.Wallet) {
	t.Log("=== Testing Provider Deactivate ===")

	address := authority.FormattedAddress()
	payoutAddress := authority.FormattedAddress()

	// Create a provider specifically for deactivation
	var providerIDToDeactivate uint64
	t.Run("setup: create provider for deactivation", func(t *testing.T) {
		res, err := helpers.SKUCreateProvider(ctx, chain, authority, address, payoutAddress, "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code)

		providerIDToDeactivate, err = helpers.GetProviderIDFromTxHash(ctx, chain, res.TxHash)
		require.NoError(t, err)
	})

	t.Run("fail: unauthorized user deactivates provider", func(t *testing.T) {
		res, err := helpers.SKUDeactivateProvider(ctx, chain, user1, providerIDToDeactivate)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "tx should fail")
		require.Contains(t, txRes.RawLog, "unauthorized")
	})

	t.Run("success: authority deactivates provider", func(t *testing.T) {
		res, err := helpers.SKUDeactivateProvider(ctx, chain, authority, providerIDToDeactivate)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "tx should succeed: %s", txRes.RawLog)

		// Verify provider is still queryable but inactive
		providerRes, err := helpers.SKUQueryProvider(ctx, chain, providerIDToDeactivate)
		require.NoError(t, err)
		require.False(t, providerRes.Provider.Active, "provider should be inactive after deactivation")
	})

	t.Run("fail: deactivate already inactive provider", func(t *testing.T) {
		res, err := helpers.SKUDeactivateProvider(ctx, chain, authority, providerIDToDeactivate)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "tx should fail")
		require.Contains(t, txRes.RawLog, "already inactive")
	})

	t.Run("fail: deactivate non-existent provider", func(t *testing.T) {
		res, err := helpers.SKUDeactivateProvider(ctx, chain, authority, 999)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "tx should fail")
		require.Contains(t, txRes.RawLog, "not found")
	})
}

func testSKUCreate(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, authority, user1 ibc.Wallet, providerID uint64) uint64 {
	t.Log("=== Testing SKU Create ===")

	name := "Compute Small"
	unit := 1 // UNIT_PER_HOUR
	// Price must be >= 3600 for UNIT_PER_HOUR to have non-zero per-second rate
	basePrice := "3600umfx"

	var skuID uint64

	t.Run("success: authority creates SKU", func(t *testing.T) {
		res, err := helpers.SKUCreateSKU(ctx, chain, authority, providerID, name, unit, basePrice, "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "tx should succeed: %s", txRes.RawLog)

		// Verify SKU was created by querying it
		skuID, err = helpers.GetSKUIDFromTxHash(ctx, chain, res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint64(1), skuID, "first SKU should have ID 1")

		skuRes, err := helpers.SKUQuerySKU(ctx, chain, skuID)
		require.NoError(t, err)
		require.Equal(t, providerID, skuRes.Sku.ProviderId)
		require.Equal(t, name, skuRes.Sku.Name)
		require.True(t, skuRes.Sku.Active)
	})

	t.Run("success: authority creates SKU with meta-hash", func(t *testing.T) {
		// Price must be >= 86400 for UNIT_PER_DAY to have non-zero per-second rate
		res, err := helpers.SKUCreateSKU(ctx, chain, authority, providerID, "Compute Medium", 2, "86400umfx", "deadbeef")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "tx should succeed: %s", txRes.RawLog)
	})

	t.Run("fail: unauthorized user creates SKU", func(t *testing.T) {
		res, err := helpers.SKUCreateSKU(ctx, chain, user1, providerID, "Unauthorized SKU", unit, basePrice, "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "tx should fail")
		require.Contains(t, txRes.RawLog, "unauthorized")
	})

	t.Run("fail: create SKU with non-existent provider", func(t *testing.T) {
		res, err := helpers.SKUCreateSKU(ctx, chain, authority, 999, "Bad Provider SKU", unit, basePrice, "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "tx should fail")
		require.Contains(t, txRes.RawLog, "not found")
	})

	t.Run("fail: create SKU with zero provider_id", func(t *testing.T) {
		// CLI validation fails before tx is broadcast, so we expect an error from the CLI
		_, err := helpers.SKUCreateSKU(ctx, chain, authority, 0, "Zero Provider SKU", unit, basePrice, "")
		require.Error(t, err)
		require.Contains(t, err.Error(), "provider_id cannot be zero")
	})

	t.Run("fail: create SKU with inactive provider", func(t *testing.T) {
		// First create a provider, then deactivate it
		createRes, err := helpers.SKUCreateProvider(ctx, chain, authority, authority.FormattedAddress(), authority.FormattedAddress(), "")
		require.NoError(t, err)
		createTxRes, err := chain.GetTransaction(createRes.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), createTxRes.Code, "provider creation should succeed")

		inactiveProviderID, err := helpers.GetProviderIDFromTxHash(ctx, chain, createRes.TxHash)
		require.NoError(t, err)

		// Deactivate the provider
		deactivateRes, err := helpers.SKUDeactivateProvider(ctx, chain, authority, inactiveProviderID)
		require.NoError(t, err)
		deactivateTxRes, err := chain.GetTransaction(deactivateRes.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), deactivateTxRes.Code, "provider deactivation should succeed")

		// Now try to create a SKU for the inactive provider
		res, err := helpers.SKUCreateSKU(ctx, chain, authority, inactiveProviderID, "Inactive Provider SKU", unit, basePrice, "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "tx should fail")
		require.Contains(t, txRes.RawLog, "not active")
	})

	return skuID
}

func testSKUQuery(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, skuID, providerID uint64) {
	t.Log("=== Testing SKU Query ===")

	t.Run("success: query existing SKU", func(t *testing.T) {
		res, err := helpers.SKUQuerySKU(ctx, chain, skuID)
		require.NoError(t, err)
		require.Equal(t, skuID, res.Sku.Id)
		require.Equal(t, providerID, res.Sku.ProviderId)
	})

	t.Run("success: query all SKUs", func(t *testing.T) {
		res, err := helpers.SKUQuerySKUs(ctx, chain)
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(res.Skus), 2, "should have at least 2 SKUs")
	})
}

func testSKUUpdate(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, authority, user1 ibc.Wallet, skuID, providerID uint64) {
	t.Log("=== Testing SKU Update ===")

	// Price must be >= 3600 for UNIT_PER_HOUR to have non-zero per-second rate
	validPrice := "3600umfx"
	updatedPrice := "7200umfx"

	t.Run("success: authority updates SKU", func(t *testing.T) {
		newName := "Compute Small Updated"
		res, err := helpers.SKUUpdateSKU(ctx, chain, authority, skuID, providerID, newName, 1, updatedPrice, true, "cafebabe")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "tx should succeed: %s", txRes.RawLog)

		// Verify update
		skuRes, err := helpers.SKUQuerySKU(ctx, chain, skuID)
		require.NoError(t, err)
		require.Equal(t, newName, skuRes.Sku.Name)
		require.Equal(t, "7200", skuRes.Sku.BasePrice.Amount.String())
	})

	t.Run("success: authority deactivates SKU via update", func(t *testing.T) {
		res, err := helpers.SKUUpdateSKU(ctx, chain, authority, skuID, providerID, "Compute Small Updated", 1, updatedPrice, false, "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "tx should succeed: %s", txRes.RawLog)

		// Verify deactivation
		skuRes, err := helpers.SKUQuerySKU(ctx, chain, skuID)
		require.NoError(t, err)
		require.False(t, skuRes.Sku.Active)

		// Reactivate for other tests
		_, err = helpers.SKUUpdateSKU(ctx, chain, authority, skuID, providerID, "Compute Small Updated", 1, updatedPrice, true, "")
		require.NoError(t, err)
	})

	t.Run("fail: unauthorized user updates SKU", func(t *testing.T) {
		res, err := helpers.SKUUpdateSKU(ctx, chain, user1, skuID, providerID, "Hacked Name", 1, validPrice, true, "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "tx should fail")
		require.Contains(t, txRes.RawLog, "unauthorized")
	})

	t.Run("fail: update with wrong provider_id", func(t *testing.T) {
		res, err := helpers.SKUUpdateSKU(ctx, chain, authority, skuID, 999, "Name", 1, validPrice, true, "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "tx should fail")
		require.Contains(t, txRes.RawLog, "provider_id mismatch")
	})

	t.Run("fail: update non-existent SKU", func(t *testing.T) {
		res, err := helpers.SKUUpdateSKU(ctx, chain, authority, 999, providerID, "Name", 1, validPrice, true, "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "tx should fail")
		require.Contains(t, txRes.RawLog, "not found")
	})

	t.Run("fail: update SKU with zero provider_id", func(t *testing.T) {
		// CLI validation fails before tx is broadcast, so we expect an error from the CLI
		_, err := helpers.SKUUpdateSKU(ctx, chain, authority, skuID, 0, "Name", 1, validPrice, true, "")
		require.Error(t, err)
		require.Contains(t, err.Error(), "provider_id cannot be zero")
	})
}

func testSKUDeactivate(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, authority, user1 ibc.Wallet, providerID uint64) {
	t.Log("=== Testing SKU Deactivate ===")

	// Create a SKU specifically for deactivation
	// Price must be >= 3600 for UNIT_PER_HOUR to have non-zero per-second rate
	var skuIDToDeactivate uint64
	t.Run("setup: create SKU for deactivation", func(t *testing.T) {
		res, err := helpers.SKUCreateSKU(ctx, chain, authority, providerID, "To Be Deactivated", 1, "3600umfx", "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code)

		skuIDToDeactivate, err = helpers.GetSKUIDFromTxHash(ctx, chain, res.TxHash)
		require.NoError(t, err)
	})

	t.Run("fail: unauthorized user deactivates SKU", func(t *testing.T) {
		res, err := helpers.SKUDeactivateSKU(ctx, chain, user1, skuIDToDeactivate)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "tx should fail")
		require.Contains(t, txRes.RawLog, "unauthorized")
	})

	t.Run("success: authority deactivates SKU", func(t *testing.T) {
		res, err := helpers.SKUDeactivateSKU(ctx, chain, authority, skuIDToDeactivate)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "tx should succeed: %s", txRes.RawLog)

		// Verify SKU is still queryable but inactive
		skuRes, err := helpers.SKUQuerySKU(ctx, chain, skuIDToDeactivate)
		require.NoError(t, err)
		require.False(t, skuRes.Sku.Active, "SKU should be inactive after deactivation")
	})

	t.Run("fail: deactivate already inactive SKU", func(t *testing.T) {
		res, err := helpers.SKUDeactivateSKU(ctx, chain, authority, skuIDToDeactivate)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "tx should fail")
		require.Contains(t, txRes.RawLog, "already inactive")
	})

	t.Run("fail: deactivate non-existent SKU", func(t *testing.T) {
		res, err := helpers.SKUDeactivateSKU(ctx, chain, authority, 999)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "tx should fail")
		require.Contains(t, txRes.RawLog, "not found")
	})
}

func testSKUUpdateParams(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, authority, user1 ibc.Wallet) {
	t.Log("=== Testing SKU Update Params ===")

	t.Run("fail: unauthorized user updates params", func(t *testing.T) {
		res, err := helpers.SKUUpdateParams(ctx, chain, user1, user1.FormattedAddress())
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "tx should fail")
		require.Contains(t, txRes.RawLog, "unauthorized")
	})

	t.Run("success: authority updates params with allowed list", func(t *testing.T) {
		res, err := helpers.SKUUpdateParams(ctx, chain, authority, user1.FormattedAddress())
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "tx should succeed: %s", txRes.RawLog)

		// Verify params updated
		paramsRes, err := helpers.SKUQueryParams(ctx, chain)
		require.NoError(t, err)
		require.Len(t, paramsRes.Params.AllowedList, 1)
		require.Equal(t, user1.FormattedAddress(), paramsRes.Params.AllowedList[0])
	})

	t.Run("success: authority clears allowed list", func(t *testing.T) {
		res, err := helpers.SKUUpdateParams(ctx, chain, authority, "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "tx should succeed: %s", txRes.RawLog)

		// Verify params cleared
		paramsRes, err := helpers.SKUQueryParams(ctx, chain)
		require.NoError(t, err)
		require.Empty(t, paramsRes.Params.AllowedList)
	})
}

func testSKUAllowedListOperations(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, authority, user1, user2 ibc.Wallet) {
	t.Log("=== Testing SKU Allowed List Operations ===")

	// Create a provider for testing
	var allowedProviderID uint64
	t.Run("setup: create provider for allowed list tests", func(t *testing.T) {
		res, err := helpers.SKUCreateProvider(ctx, chain, authority, authority.FormattedAddress(), authority.FormattedAddress(), "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code)

		allowedProviderID, err = helpers.GetProviderIDFromTxHash(ctx, chain, res.TxHash)
		require.NoError(t, err)
	})

	// Add user1 to allowed list
	t.Run("setup: add user1 to allowed list", func(t *testing.T) {
		res, err := helpers.SKUUpdateParams(ctx, chain, authority, user1.FormattedAddress())
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code)
	})

	// Price must be >= 3600 for UNIT_PER_HOUR to have non-zero per-second rate
	validPrice := "3600umfx"

	var allowedSKUID uint64
	t.Run("success: allowed user creates SKU", func(t *testing.T) {
		res, err := helpers.SKUCreateSKU(ctx, chain, user1, allowedProviderID, "Allowed User SKU", 1, validPrice, "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "tx should succeed: %s", txRes.RawLog)

		allowedSKUID, err = helpers.GetSKUIDFromTxHash(ctx, chain, res.TxHash)
		require.NoError(t, err)
	})

	t.Run("fail: non-allowed user creates SKU", func(t *testing.T) {
		res, err := helpers.SKUCreateSKU(ctx, chain, user2, allowedProviderID, "Non-Allowed SKU", 1, validPrice, "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "tx should fail")
		require.Contains(t, txRes.RawLog, "unauthorized")
	})

	t.Run("success: allowed user updates SKU", func(t *testing.T) {
		// Price must be >= 86400 for UNIT_PER_DAY to have non-zero per-second rate
		res, err := helpers.SKUUpdateSKU(ctx, chain, user1, allowedSKUID, allowedProviderID, "Updated by Allowed", 2, "86400umfx", true, "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "tx should succeed: %s", txRes.RawLog)
	})

	t.Run("fail: non-allowed user updates SKU", func(t *testing.T) {
		res, err := helpers.SKUUpdateSKU(ctx, chain, user2, allowedSKUID, allowedProviderID, "Hacked", 1, validPrice, true, "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "tx should fail")
		require.Contains(t, txRes.RawLog, "unauthorized")
	})

	t.Run("success: allowed user deactivates SKU", func(t *testing.T) {
		res, err := helpers.SKUDeactivateSKU(ctx, chain, user1, allowedSKUID)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "tx should succeed: %s", txRes.RawLog)
	})

	t.Run("fail: allowed user cannot update params", func(t *testing.T) {
		// Even allowed users cannot update params - only authority can
		res, err := helpers.SKUUpdateParams(ctx, chain, user1, user2.FormattedAddress())
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "tx should fail")
		require.Contains(t, txRes.RawLog, "unauthorized")
	})

	// Cleanup: clear allowed list
	t.Run("cleanup: clear allowed list", func(t *testing.T) {
		res, err := helpers.SKUUpdateParams(ctx, chain, authority, "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code)
	})
}

func testSKUQueryByProvider(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, authority ibc.Wallet) {
	t.Log("=== Testing SKU Query By Provider ===")

	// Create a new provider specifically for this test
	var queryProviderID uint64
	t.Run("setup: create provider for query test", func(t *testing.T) {
		res, err := helpers.SKUCreateProvider(ctx, chain, authority, authority.FormattedAddress(), authority.FormattedAddress(), "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code)

		queryProviderID, err = helpers.GetProviderIDFromTxHash(ctx, chain, res.TxHash)
		require.NoError(t, err)
	})

	// Create multiple SKUs for the provider
	// Create 3 SKUs for the provider using valid units (1=UNIT_PER_HOUR, 2=UNIT_PER_DAY)
	// Price must be >= 86400 for UNIT_PER_DAY to have non-zero per-second rate
	t.Run("setup: create SKUs for provider", func(t *testing.T) {
		units := []int{1, 2, 1} // UNIT_PER_HOUR, UNIT_PER_DAY, UNIT_PER_HOUR
		for i := 0; i < 3; i++ {
			res, err := helpers.SKUCreateSKU(ctx, chain, authority, queryProviderID, "Query Test SKU "+string(rune('1'+i)), units[i], "86400umfx", "")
			require.NoError(t, err)

			txRes, err := chain.GetTransaction(res.TxHash)
			require.NoError(t, err)
			require.Equal(t, uint32(0), txRes.Code)
		}
	})

	t.Run("success: query SKUs by provider", func(t *testing.T) {
		res, err := helpers.SKUQuerySKUsByProvider(ctx, chain, queryProviderID)
		require.NoError(t, err)
		require.Len(t, res.Skus, 3, "should have 3 SKUs for this provider")

		for _, sku := range res.Skus {
			require.Equal(t, queryProviderID, sku.ProviderId)
		}
	})

	t.Run("success: query SKUs by non-existent provider returns empty", func(t *testing.T) {
		res, err := helpers.SKUQuerySKUsByProvider(ctx, chain, 99999)
		require.NoError(t, err)
		require.Empty(t, res.Skus)
	})
}

func testSKUPagination(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, authority ibc.Wallet) {
	t.Log("=== Testing SKU Pagination ===")

	// Create a new provider for pagination testing
	var paginationProviderID uint64
	t.Run("setup: create provider for pagination test", func(t *testing.T) {
		res, err := helpers.SKUCreateProvider(ctx, chain, authority, authority.FormattedAddress(), authority.FormattedAddress(), "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code)

		paginationProviderID, err = helpers.GetProviderIDFromTxHash(ctx, chain, res.TxHash)
		require.NoError(t, err)
	})

	// Create multiple SKUs for pagination testing
	// Price must be >= 3600 for UNIT_PER_HOUR to have non-zero per-second rate
	t.Run("setup: create SKUs for pagination", func(t *testing.T) {
		for i := 1; i <= 5; i++ {
			res, err := helpers.SKUCreateSKU(ctx, chain, authority, paginationProviderID, "Pagination SKU "+string(rune('0'+i)), 1, "3600umfx", "")
			require.NoError(t, err)

			txRes, err := chain.GetTransaction(res.TxHash)
			require.NoError(t, err)
			require.Equal(t, uint32(0), txRes.Code)
		}
	})

	t.Run("success: paginate all SKUs", func(t *testing.T) {
		// First page with limit 2
		res1, nextKey, err := helpers.SKUQuerySKUsPaginated(ctx, chain, 2, "")
		require.NoError(t, err)
		require.Len(t, res1.Skus, 2, "first page should have 2 SKUs")
		require.NotNil(t, res1.Pagination, "pagination info should be present")
		require.NotEmpty(t, nextKey, "next key should be present for more pages")

		// Second page using next key
		res2, _, err := helpers.SKUQuerySKUsPaginated(ctx, chain, 2, nextKey)
		require.NoError(t, err)
		require.Len(t, res2.Skus, 2, "second page should have 2 SKUs")

		// Verify no duplicates between pages
		page1IDs := make(map[uint64]bool)
		for _, sku := range res1.Skus {
			page1IDs[sku.Id] = true
		}
		for _, sku := range res2.Skus {
			require.False(t, page1IDs[sku.Id], "SKU %d should not appear in both pages", sku.Id)
		}
	})

	t.Run("success: paginate SKUs by provider", func(t *testing.T) {
		// First page with limit 2
		res1, nextKey, err := helpers.SKUQuerySKUsByProviderPaginated(ctx, chain, paginationProviderID, 2, "")
		require.NoError(t, err)
		require.Len(t, res1.Skus, 2, "first page should have 2 SKUs")
		require.NotNil(t, res1.Pagination, "pagination info should be present")
		require.NotEmpty(t, nextKey, "next key should be present for more pages")

		for _, sku := range res1.Skus {
			require.Equal(t, paginationProviderID, sku.ProviderId)
		}

		// Second page using next key
		res2, nextKey, err := helpers.SKUQuerySKUsByProviderPaginated(ctx, chain, paginationProviderID, 2, nextKey)
		require.NoError(t, err)
		require.Len(t, res2.Skus, 2, "second page should have 2 SKUs")

		for _, sku := range res2.Skus {
			require.Equal(t, paginationProviderID, sku.ProviderId)
		}

		// Third page - should have 1 remaining SKU
		res3, _, err := helpers.SKUQuerySKUsByProviderPaginated(ctx, chain, paginationProviderID, 2, nextKey)
		require.NoError(t, err)
		require.Len(t, res3.Skus, 1, "third page should have 1 SKU")

		// Verify no duplicates across all pages
		allIDs := make(map[uint64]bool)
		for _, sku := range res1.Skus {
			require.False(t, allIDs[sku.Id], "duplicate SKU ID found")
			allIDs[sku.Id] = true
		}
		for _, sku := range res2.Skus {
			require.False(t, allIDs[sku.Id], "duplicate SKU ID found")
			allIDs[sku.Id] = true
		}
		for _, sku := range res3.Skus {
			require.False(t, allIDs[sku.Id], "duplicate SKU ID found")
			allIDs[sku.Id] = true
		}
		require.Len(t, allIDs, 5, "should have collected all 5 SKUs across pages")
	})

	t.Run("success: large limit returns all results", func(t *testing.T) {
		res, _, err := helpers.SKUQuerySKUsByProviderPaginated(ctx, chain, paginationProviderID, 100, "")
		require.NoError(t, err)
		require.Len(t, res.Skus, 5, "should return all 5 SKUs")
	})
}
