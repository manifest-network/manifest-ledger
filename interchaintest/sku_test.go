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

const (
	testProvider = "test-provider"
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

	t.Run("CreateSKU", func(t *testing.T) {
		testSKUCreate(t, ctx, chain, authority, user1)
	})

	t.Run("QuerySKU", func(t *testing.T) {
		testSKUQuery(t, ctx, chain, authority)
	})

	t.Run("UpdateSKU", func(t *testing.T) {
		testSKUUpdate(t, ctx, chain, authority, user1)
	})

	t.Run("DeleteSKU", func(t *testing.T) {
		testSKUDelete(t, ctx, chain, authority, user1)
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

func testSKUCreate(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, authority, user1 ibc.Wallet) {
	t.Log("=== Testing SKU Create ===")

	provider := testProvider
	name := "Compute Small"
	unit := 1 // UNIT_PER_HOUR
	basePrice := "100umfx"

	t.Run("success: authority creates SKU", func(t *testing.T) {
		res, err := helpers.SKUCreateSKU(ctx, chain, authority, provider, name, unit, basePrice, "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "tx should succeed: %s", txRes.RawLog)

		// Verify SKU was created by querying it
		skuID, err := helpers.GetSKUIDFromTxHash(ctx, chain, res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint64(1), skuID, "first SKU should have ID 1")

		skuRes, err := helpers.SKUQuerySKU(ctx, chain, skuID)
		require.NoError(t, err)
		require.Equal(t, provider, skuRes.Sku.Provider)
		require.Equal(t, name, skuRes.Sku.Name)
		require.True(t, skuRes.Sku.Active)
	})

	t.Run("success: authority creates SKU with meta-hash", func(t *testing.T) {
		res, err := helpers.SKUCreateSKU(ctx, chain, authority, provider, "Compute Medium", 2, "200umfx", "deadbeef")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "tx should succeed: %s", txRes.RawLog)
	})

	t.Run("fail: unauthorized user creates SKU", func(t *testing.T) {
		res, err := helpers.SKUCreateSKU(ctx, chain, user1, provider, "Unauthorized SKU", unit, basePrice, "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "tx should fail")
		require.Contains(t, txRes.RawLog, "unauthorized")
	})
}

func testSKUQuery(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, _ ibc.Wallet) {
	t.Log("=== Testing SKU Query ===")

	t.Run("success: query existing SKU", func(t *testing.T) {
		res, err := helpers.SKUQuerySKU(ctx, chain, 1)
		require.NoError(t, err)
		require.Equal(t, uint64(1), res.Sku.Id)
		require.Equal(t, testProvider, res.Sku.Provider)
	})

	t.Run("success: query all SKUs", func(t *testing.T) {
		res, err := helpers.SKUQuerySKUs(ctx, chain)
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(res.Skus), 2, "should have at least 2 SKUs")
	})
}

func testSKUUpdate(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, authority, user1 ibc.Wallet) {
	t.Log("=== Testing SKU Update ===")

	provider := testProvider

	t.Run("success: authority updates SKU", func(t *testing.T) {
		newName := "Compute Small Updated"
		res, err := helpers.SKUUpdateSKU(ctx, chain, authority, provider, 1, newName, 1, "150umfx", true, "cafebabe")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "tx should succeed: %s", txRes.RawLog)

		// Verify update
		skuRes, err := helpers.SKUQuerySKU(ctx, chain, 1)
		require.NoError(t, err)
		require.Equal(t, newName, skuRes.Sku.Name)
		require.Equal(t, "150", skuRes.Sku.BasePrice.Amount.String())
	})

	t.Run("success: authority deactivates SKU", func(t *testing.T) {
		res, err := helpers.SKUUpdateSKU(ctx, chain, authority, provider, 1, "Compute Small Updated", 1, "150umfx", false, "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "tx should succeed: %s", txRes.RawLog)

		// Verify deactivation
		skuRes, err := helpers.SKUQuerySKU(ctx, chain, 1)
		require.NoError(t, err)
		require.False(t, skuRes.Sku.Active)

		// Reactivate for other tests
		_, err = helpers.SKUUpdateSKU(ctx, chain, authority, provider, 1, "Compute Small Updated", 1, "150umfx", true, "")
		require.NoError(t, err)
	})

	t.Run("fail: unauthorized user updates SKU", func(t *testing.T) {
		res, err := helpers.SKUUpdateSKU(ctx, chain, user1, provider, 1, "Hacked Name", 1, "100umfx", true, "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "tx should fail")
		require.Contains(t, txRes.RawLog, "unauthorized")
	})

	t.Run("fail: update with wrong provider", func(t *testing.T) {
		res, err := helpers.SKUUpdateSKU(ctx, chain, authority, "wrong-provider", 1, "Name", 1, "100umfx", true, "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "tx should fail")
		require.Contains(t, txRes.RawLog, "provider mismatch")
	})

	t.Run("fail: update non-existent SKU", func(t *testing.T) {
		res, err := helpers.SKUUpdateSKU(ctx, chain, authority, provider, 999, "Name", 1, "100umfx", true, "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "tx should fail")
		require.Contains(t, txRes.RawLog, "not found")
	})
}

func testSKUDelete(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, authority, user1 ibc.Wallet) {
	t.Log("=== Testing SKU Delete ===")

	provider := testProvider

	// Create a SKU specifically for deletion
	t.Run("setup: create SKU for deletion", func(t *testing.T) {
		res, err := helpers.SKUCreateSKU(ctx, chain, authority, provider, "To Be Deleted", 1, "50umfx", "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code)
	})

	t.Run("fail: unauthorized user deletes SKU", func(t *testing.T) {
		res, err := helpers.SKUDeleteSKU(ctx, chain, user1, provider, 3)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "tx should fail")
		require.Contains(t, txRes.RawLog, "unauthorized")
	})

	t.Run("fail: delete with wrong provider", func(t *testing.T) {
		res, err := helpers.SKUDeleteSKU(ctx, chain, authority, "wrong-provider", 3)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "tx should fail")
		require.Contains(t, txRes.RawLog, "provider mismatch")
	})

	t.Run("success: authority deletes SKU", func(t *testing.T) {
		res, err := helpers.SKUDeleteSKU(ctx, chain, authority, provider, 3)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "tx should succeed: %s", txRes.RawLog)
	})

	t.Run("fail: delete non-existent SKU", func(t *testing.T) {
		res, err := helpers.SKUDeleteSKU(ctx, chain, authority, provider, 999)
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

	provider := "allowed-list-provider"

	// Add user1 to allowed list
	t.Run("setup: add user1 to allowed list", func(t *testing.T) {
		res, err := helpers.SKUUpdateParams(ctx, chain, authority, user1.FormattedAddress())
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code)
	})

	t.Run("success: allowed user creates SKU", func(t *testing.T) {
		res, err := helpers.SKUCreateSKU(ctx, chain, user1, provider, "Allowed User SKU", 1, "100umfx", "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "tx should succeed: %s", txRes.RawLog)
	})

	t.Run("fail: non-allowed user creates SKU", func(t *testing.T) {
		res, err := helpers.SKUCreateSKU(ctx, chain, user2, provider, "Non-Allowed SKU", 1, "100umfx", "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "tx should fail")
		require.Contains(t, txRes.RawLog, "unauthorized")
	})

	// Get the SKU ID created by user1
	var allowedSKUID uint64
	t.Run("setup: get SKU ID", func(t *testing.T) {
		res, err := helpers.SKUQuerySKUsByProvider(ctx, chain, provider)
		require.NoError(t, err)
		require.NotEmpty(t, res.Skus)
		allowedSKUID = res.Skus[0].Id
	})

	t.Run("success: allowed user updates SKU", func(t *testing.T) {
		res, err := helpers.SKUUpdateSKU(ctx, chain, user1, provider, allowedSKUID, "Updated by Allowed", 2, "200umfx", true, "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "tx should succeed: %s", txRes.RawLog)
	})

	t.Run("fail: non-allowed user updates SKU", func(t *testing.T) {
		res, err := helpers.SKUUpdateSKU(ctx, chain, user2, provider, allowedSKUID, "Hacked", 1, "100umfx", true, "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "tx should fail")
		require.Contains(t, txRes.RawLog, "unauthorized")
	})

	t.Run("success: allowed user deletes SKU", func(t *testing.T) {
		res, err := helpers.SKUDeleteSKU(ctx, chain, user1, provider, allowedSKUID)
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

	provider := "query-test-provider"

	// Create multiple SKUs for the same provider
	t.Run("setup: create SKUs for provider", func(t *testing.T) {
		for i := 1; i <= 3; i++ {
			res, err := helpers.SKUCreateSKU(ctx, chain, authority, provider, "Query Test SKU "+string(rune('0'+i)), i, "100umfx", "")
			require.NoError(t, err)

			txRes, err := chain.GetTransaction(res.TxHash)
			require.NoError(t, err)
			require.Equal(t, uint32(0), txRes.Code)
		}
	})

	t.Run("success: query SKUs by provider", func(t *testing.T) {
		res, err := helpers.SKUQuerySKUsByProvider(ctx, chain, provider)
		require.NoError(t, err)
		require.Len(t, res.Skus, 3, "should have 3 SKUs for this provider")

		for _, sku := range res.Skus {
			require.Equal(t, provider, sku.Provider)
		}
	})

	t.Run("success: query SKUs by non-existent provider returns empty", func(t *testing.T) {
		res, err := helpers.SKUQuerySKUsByProvider(ctx, chain, "non-existent-provider")
		require.NoError(t, err)
		require.Empty(t, res.Skus)
	})
}

func testSKUPagination(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, authority ibc.Wallet) {
	t.Log("=== Testing SKU Pagination ===")

	paginationProvider := "pagination-provider"

	// Create multiple SKUs for pagination testing
	t.Run("setup: create SKUs for pagination", func(t *testing.T) {
		for i := 1; i <= 5; i++ {
			res, err := helpers.SKUCreateSKU(ctx, chain, authority, paginationProvider, "Pagination SKU "+string(rune('0'+i)), 1, "100umfx", "")
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
		res1, nextKey, err := helpers.SKUQuerySKUsByProviderPaginated(ctx, chain, paginationProvider, 2, "")
		require.NoError(t, err)
		require.Len(t, res1.Skus, 2, "first page should have 2 SKUs")
		require.NotNil(t, res1.Pagination, "pagination info should be present")
		require.NotEmpty(t, nextKey, "next key should be present for more pages")

		for _, sku := range res1.Skus {
			require.Equal(t, paginationProvider, sku.Provider)
		}

		// Second page using next key
		res2, nextKey, err := helpers.SKUQuerySKUsByProviderPaginated(ctx, chain, paginationProvider, 2, nextKey)
		require.NoError(t, err)
		require.Len(t, res2.Skus, 2, "second page should have 2 SKUs")

		for _, sku := range res2.Skus {
			require.Equal(t, paginationProvider, sku.Provider)
		}

		// Third page - should have 1 remaining SKU
		res3, _, err := helpers.SKUQuerySKUsByProviderPaginated(ctx, chain, paginationProvider, 2, nextKey)
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
		res, _, err := helpers.SKUQuerySKUsByProviderPaginated(ctx, chain, paginationProvider, 100, "")
		require.NoError(t, err)
		require.Len(t, res.Skus, 5, "should return all 5 SKUs")
	})
}
