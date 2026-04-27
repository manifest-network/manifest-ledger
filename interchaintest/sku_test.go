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
//   - Fail: create SKU with non-evenly divisible price (exact division required)
//
// testSKUQuery:
//   - Success: query existing SKU by ID
//   - Success: query all SKUs
//
// testSKUUpdate:
//   - Success: authority updates SKU (name, price)
//   - Fail: cannot deactivate SKU via update (must use DeactivateSKU)
//   - Fail: unauthorized user updates SKU
//   - Fail: update with wrong provider_id (mismatch)
//   - Fail: update non-existent SKU
//   - Fail: update SKU with zero provider_id
//   - Fail: update SKU with non-evenly divisible price (exact division required)
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
//
// ## Multi-Denom Tests
//
// testSKUMultiDenom:
//   - Success: create SKU with umfx denom
//   - Success: create SKU with upwr denom
//   - Success: create SKU with custom utest denom
//   - Success: update SKU to different denom
//   - Success: query all SKUs returns multiple denoms
//   - Success: verify SKU IDs are correct
//
// ## Provider Full Fields Tests
//
// testProviderFullFields:
//   - Success: create provider with api_url and pending_timeout
//   - Success: update provider api_url
//   - Success: update provider pending_timeout
//   - Success: create provider with default pending_timeout
//   - Success: create provider without api_url
//   - Success: update preserves existing values when not provided
//   - Success: create provider with minimum and maximum pending_timeout
//
// ## Provider Pagination Tests
//
// testProviderPagination:
//   - Success: paginate through all providers
//   - Success: large limit returns all results
//   - Success: verify no duplicates across pages
//
// ## SKU Validation Tests
//
// testSKUValidation:
//   - Fail: create SKU with UNIT_UNSPECIFIED
//   - Fail: create SKU with empty name
//   - Fail: update SKU with UNIT_UNSPECIFIED
//   - Fail: update SKU with empty name
//
// ## Invalid UUID Tests
//
// testSKUInvalidUUID:
//   - Fail: update provider with invalid uuid format
//   - Fail: deactivate provider with invalid uuid format
//   - Fail: create SKU with invalid provider_uuid format
//   - Fail: update SKU with invalid uuid format
//   - Fail: update SKU with invalid provider_uuid format
//   - Fail: deactivate SKU with invalid uuid format
//   - Fail: query provider with invalid uuid format
//   - Fail: query SKU with invalid uuid format
//
// ## Empty Params Tests
//
// testSKUEmptyParams:
//   - Fail: create provider with empty address
//   - Fail: create provider with empty payout_address
//   - Fail: update provider with empty uuid/address/payout_address
//   - Fail: deactivate provider with empty uuid
//   - Fail: create SKU with empty provider_uuid
//   - Fail: update SKU with empty uuid/provider_uuid
//   - Fail: deactivate SKU with empty uuid
//   - Fail: query provider/SKU with empty uuid
package interchaintest

import (
	"context"
	"fmt"
	"testing"

	"github.com/cosmos/interchaintest/v10"
	"github.com/cosmos/interchaintest/v10/chain/cosmos"
	"github.com/cosmos/interchaintest/v10/dockerutil"
	"github.com/cosmos/interchaintest/v10/ibc"
	"github.com/stretchr/testify/require"

	"github.com/manifest-network/manifest-ledger/interchaintest/helpers"
	skutypes "github.com/manifest-network/manifest-ledger/x/sku/types"
)

// Test constants for SKU prices
const (
	testPriceHourly = "3600umfx"  // Minimum valid price for UNIT_PER_HOUR
	testPriceDaily  = "86400umfx" // Minimum valid price for UNIT_PER_DAY
)

// nonExistentUUID is a valid UUIDv7 format that doesn't exist in the store.
// Used for testing "not found" error cases where we need to pass CLI validation.
const nonExistentUUID = "01912345-6789-7abc-8def-0123456789ab"

// validMetaHashHex is a valid hex-encoded meta-hash for testing updates.
const validMetaHashHex = "deadbeefcafe1234"

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
	var providerUUID string
	t.Run("CreateProvider", func(t *testing.T) {
		providerUUID = testProviderCreate(t, ctx, chain, authority, user1)
	})

	t.Run("QueryProvider", func(t *testing.T) {
		testProviderQuery(t, ctx, chain, providerUUID)
	})

	t.Run("UpdateProvider", func(t *testing.T) {
		testProviderUpdate(t, ctx, chain, authority, user1, providerUUID)
	})

	// SKU tests
	var skuUUID string
	t.Run("CreateSKU", func(t *testing.T) {
		skuUUID = testSKUCreate(t, ctx, chain, authority, user1, providerUUID)
	})

	t.Run("QuerySKU", func(t *testing.T) {
		testSKUQuery(t, ctx, chain, skuUUID, providerUUID)
	})

	t.Run("UpdateSKU", func(t *testing.T) {
		testSKUUpdate(t, ctx, chain, authority, user1, skuUUID, providerUUID)
	})

	t.Run("DeactivateSKU", func(t *testing.T) {
		testSKUDeactivate(t, ctx, chain, authority, user1, providerUUID)
	})

	t.Run("DeactivateProvider", func(t *testing.T) {
		testProviderDeactivate(t, ctx, chain, authority, user1)
	})

	t.Run("DeactivateProviderPagination", func(t *testing.T) {
		testProviderDeactivatePagination(t, ctx, chain, authority)
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

	t.Run("MultiDenom", func(t *testing.T) {
		testSKUMultiDenom(t, ctx, chain, authority)
	})

	t.Run("ProviderFullFields", func(t *testing.T) {
		testProviderFullFields(t, ctx, chain, authority)
	})

	t.Run("UnitPerDay", func(t *testing.T) {
		testSKUUnitPerDay(t, ctx, chain, authority)
	})

	t.Run("ProviderPagination", func(t *testing.T) {
		testProviderPagination(t, ctx, chain, authority)
	})

	t.Run("SKUValidation", func(t *testing.T) {
		testSKUValidation(t, ctx, chain, authority)
	})

	t.Run("InvalidUUID", func(t *testing.T) {
		testSKUInvalidUUID(t, ctx, chain, authority)
	})

	t.Run("EmptyParams", func(t *testing.T) {
		testSKUEmptyParams(t, ctx, chain, authority)
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

func testProviderCreate(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, authority, user1 ibc.Wallet) string {
	t.Log("=== Testing Provider Create ===")

	address := authority.FormattedAddress()
	payoutAddress := authority.FormattedAddress()

	var providerUUID string

	t.Run("success: authority creates provider", func(t *testing.T) {
		res, err := helpers.SKUCreateProvider(ctx, chain, authority, address, payoutAddress, "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "tx should succeed: %s", txRes.RawLog)

		// Verify provider was created by querying it
		providerUUID, err = helpers.GetProviderUUIDFromTxHash(ctx, chain, res.TxHash)
		require.NoError(t, err)
		require.NotEmpty(t, providerUUID, "provider UUID should not be empty")

		providerRes, err := helpers.SKUQueryProvider(ctx, chain, providerUUID)
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

	return providerUUID
}

func testProviderQuery(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, providerUUID string) {
	t.Log("=== Testing Provider Query ===")

	t.Run("success: query existing provider", func(t *testing.T) {
		res, err := helpers.SKUQueryProvider(ctx, chain, providerUUID)
		require.NoError(t, err)
		require.Equal(t, providerUUID, res.Provider.Uuid)
	})

	t.Run("success: query all providers", func(t *testing.T) {
		res, err := helpers.SKUQueryProviders(ctx, chain)
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(res.Providers), 1, "should have at least 1 provider")
	})
}

func testProviderUpdate(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, authority, user1 ibc.Wallet, providerUUID string) {
	t.Log("=== Testing Provider Update ===")

	address := authority.FormattedAddress()
	newPayoutAddress := user1.FormattedAddress()

	t.Run("success: authority updates provider", func(t *testing.T) {
		res, err := helpers.SKUUpdateProvider(ctx, chain, authority, providerUUID, address, newPayoutAddress, true, "cafebabe")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "tx should succeed: %s", txRes.RawLog)

		// Verify update
		providerRes, err := helpers.SKUQueryProvider(ctx, chain, providerUUID)
		require.NoError(t, err)
		require.Equal(t, newPayoutAddress, providerRes.Provider.PayoutAddress)
	})

	t.Run("fail: unauthorized user updates provider", func(t *testing.T) {
		res, err := helpers.SKUUpdateProvider(ctx, chain, user1, providerUUID, address, newPayoutAddress, true, "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "tx should fail")
		require.Contains(t, txRes.RawLog, "unauthorized")
	})

	t.Run("fail: update non-existent provider", func(t *testing.T) {
		res, err := helpers.SKUUpdateProvider(ctx, chain, authority, nonExistentUUID, address, newPayoutAddress, true, "")
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
	var providerUUIDToDeactivate string
	t.Run("setup: create provider for deactivation", func(t *testing.T) {
		res, err := helpers.SKUCreateProvider(ctx, chain, authority, address, payoutAddress, "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code)

		providerUUIDToDeactivate, err = helpers.GetProviderUUIDFromTxHash(ctx, chain, res.TxHash)
		require.NoError(t, err)
	})

	t.Run("fail: unauthorized user deactivates provider", func(t *testing.T) {
		res, err := helpers.SKUDeactivateProvider(ctx, chain, user1, providerUUIDToDeactivate)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "tx should fail")
		require.Contains(t, txRes.RawLog, "unauthorized")
	})

	t.Run("success: authority deactivates provider", func(t *testing.T) {
		res, err := helpers.SKUDeactivateProvider(ctx, chain, authority, providerUUIDToDeactivate)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "tx should succeed: %s", txRes.RawLog)

		// Verify provider is still queryable but inactive
		providerRes, err := helpers.SKUQueryProvider(ctx, chain, providerUUIDToDeactivate)
		require.NoError(t, err)
		require.False(t, providerRes.Provider.Active, "provider should be inactive after deactivation")
	})

	t.Run("fail: deactivate already inactive provider", func(t *testing.T) {
		res, err := helpers.SKUDeactivateProvider(ctx, chain, authority, providerUUIDToDeactivate)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "tx should fail")
		require.Contains(t, txRes.RawLog, "already inactive")
	})

	t.Run("fail: deactivate non-existent provider", func(t *testing.T) {
		res, err := helpers.SKUDeactivateProvider(ctx, chain, authority, nonExistentUUID)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "tx should fail")
		require.Contains(t, txRes.RawLog, "not found")
	})
}

// testProviderDeactivatePagination tests that SKU deactivation is paginated
// to prevent gas exhaustion when a provider has many SKUs.
func testProviderDeactivatePagination(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, authority ibc.Wallet) {
	t.Log("=== Testing Provider Deactivate Pagination ===")

	address := authority.FormattedAddress()
	payoutAddress := authority.FormattedAddress()

	// Create a provider specifically for pagination testing
	var providerUUID string
	t.Run("setup: create provider", func(t *testing.T) {
		res, err := helpers.SKUCreateProvider(ctx, chain, authority, address, payoutAddress, "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "tx should succeed: %s", txRes.RawLog)

		providerUUID, err = helpers.GetProviderUUIDFromTxHash(ctx, chain, res.TxHash)
		require.NoError(t, err)
		t.Logf("Created provider: %s", providerUUID)
	})

	// Create 10 SKUs for this provider
	const totalSKUs = 10
	const testLimit = 3 // Small limit to test pagination
	var skuUUIDs []string

	t.Run("setup: create multiple SKUs", func(t *testing.T) {
		for i := 0; i < totalSKUs; i++ {
			name := fmt.Sprintf("Pagination Test SKU %d", i)
			res, err := helpers.SKUCreateSKU(ctx, chain, authority, providerUUID, name, 1, testPriceHourly, "")
			require.NoError(t, err)

			txRes, err := chain.GetTransaction(res.TxHash)
			require.NoError(t, err)
			require.Equal(t, uint32(0), txRes.Code, "tx should succeed: %s", txRes.RawLog)

			skuUUID, err := helpers.GetSKUUUIDFromTxHash(ctx, chain, res.TxHash)
			require.NoError(t, err)
			skuUUIDs = append(skuUUIDs, skuUUID)
		}
		t.Logf("Created %d SKUs", len(skuUUIDs))
	})

	// Verify all SKUs are active
	t.Run("verify: all SKUs initially active", func(t *testing.T) {
		skusRes, err := helpers.SKUQuerySKUsByProvider(ctx, chain, providerUUID)
		require.NoError(t, err)
		require.Len(t, skusRes.Skus, totalSKUs)

		activeCount := 0
		for _, sku := range skusRes.Skus {
			if sku.Active {
				activeCount++
			}
		}
		require.Equal(t, totalSKUs, activeCount, "all SKUs should be active initially")
	})

	// First deactivation call with limit - should deactivate provider + first batch of SKUs
	t.Run("success: first deactivation call with limit", func(t *testing.T) {
		res, err := helpers.SKUDeactivateProvider(ctx, chain, authority, providerUUID, "--limit", fmt.Sprintf("%d", testLimit))
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "tx should succeed: %s", txRes.RawLog)

		// Verify provider is now inactive
		providerRes, err := helpers.SKUQueryProvider(ctx, chain, providerUUID)
		require.NoError(t, err)
		require.False(t, providerRes.Provider.Active, "provider should be inactive after first call")

		// Verify some SKUs are still active (not all deactivated yet)
		skusRes, err := helpers.SKUQuerySKUsByProvider(ctx, chain, providerUUID)
		require.NoError(t, err)

		activeCount := 0
		for _, sku := range skusRes.Skus {
			if sku.Active {
				activeCount++
			}
		}
		expectedActive := totalSKUs - testLimit
		require.Equal(t, expectedActive, activeCount, "should have %d active SKUs after first call (deactivated %d)", expectedActive, testLimit)
		t.Logf("After first call: %d SKUs still active", activeCount)
	})

	// Continue calling until all SKUs are deactivated
	t.Run("success: continue deactivation until complete", func(t *testing.T) {
		maxCalls := 10 // Safety limit to prevent infinite loop
		for i := 0; i < maxCalls; i++ {
			// Query current state
			skusRes, err := helpers.SKUQuerySKUsByProvider(ctx, chain, providerUUID)
			require.NoError(t, err)

			activeCount := 0
			for _, sku := range skusRes.Skus {
				if sku.Active {
					activeCount++
				}
			}

			if activeCount == 0 {
				t.Logf("All SKUs deactivated after %d additional calls", i)
				break
			}

			// Call deactivate again
			res, err := helpers.SKUDeactivateProvider(ctx, chain, authority, providerUUID, "--limit", fmt.Sprintf("%d", testLimit))
			require.NoError(t, err)

			txRes, err := chain.GetTransaction(res.TxHash)
			require.NoError(t, err)
			require.Equal(t, uint32(0), txRes.Code, "tx should succeed: %s", txRes.RawLog)

			t.Logf("Call %d: deactivated more SKUs", i+1)
		}
	})

	// Verify all SKUs are now inactive
	t.Run("verify: all SKUs deactivated", func(t *testing.T) {
		skusRes, err := helpers.SKUQuerySKUsByProvider(ctx, chain, providerUUID)
		require.NoError(t, err)
		require.Len(t, skusRes.Skus, totalSKUs)

		for _, sku := range skusRes.Skus {
			require.False(t, sku.Active, "SKU %s should be inactive", sku.Uuid)
		}
		t.Log("All SKUs confirmed inactive")
	})

	// Calling deactivate again should fail since provider and all SKUs are already inactive
	t.Run("fail: deactivate when already fully deactivated", func(t *testing.T) {
		res, err := helpers.SKUDeactivateProvider(ctx, chain, authority, providerUUID, "--limit", fmt.Sprintf("%d", testLimit))
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "tx should fail when nothing to deactivate")
		require.Contains(t, txRes.RawLog, "already inactive")
	})
}

func testSKUCreate(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, authority, user1 ibc.Wallet, providerUUID string) string {
	t.Log("=== Testing SKU Create ===")

	name := "Compute Small"
	unit := 1 // UNIT_PER_HOUR
	// Price must be >= 3600 for UNIT_PER_HOUR to have non-zero per-second rate
	basePrice := testPriceHourly

	var skuUUID string

	t.Run("success: authority creates SKU", func(t *testing.T) {
		res, err := helpers.SKUCreateSKU(ctx, chain, authority, providerUUID, name, unit, basePrice, "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "tx should succeed: %s", txRes.RawLog)

		// Verify SKU was created by querying it
		skuUUID, err = helpers.GetSKUUUIDFromTxHash(ctx, chain, res.TxHash)
		require.NoError(t, err)
		require.NotEmpty(t, skuUUID, "SKU UUID should not be empty")

		skuRes, err := helpers.SKUQuerySKU(ctx, chain, skuUUID)
		require.NoError(t, err)
		require.Equal(t, providerUUID, skuRes.Sku.ProviderUuid)
		require.Equal(t, name, skuRes.Sku.Name)
		require.True(t, skuRes.Sku.Active)
	})

	t.Run("success: authority creates SKU with meta-hash", func(t *testing.T) {
		// Price must be >= 86400 for UNIT_PER_DAY to have non-zero per-second rate
		res, err := helpers.SKUCreateSKU(ctx, chain, authority, providerUUID, "Compute Medium", 2, "86400umfx", "deadbeef")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "tx should succeed: %s", txRes.RawLog)
	})

	t.Run("fail: unauthorized user creates SKU", func(t *testing.T) {
		res, err := helpers.SKUCreateSKU(ctx, chain, user1, providerUUID, "Unauthorized SKU", unit, basePrice, "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "tx should fail")
		require.Contains(t, txRes.RawLog, "unauthorized")
	})

	t.Run("fail: create SKU with non-existent provider", func(t *testing.T) {
		res, err := helpers.SKUCreateSKU(ctx, chain, authority, nonExistentUUID, "Bad Provider SKU", unit, basePrice, "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "tx should fail")
		require.Contains(t, txRes.RawLog, "not found")
	})

	t.Run("fail: create SKU with empty provider_uuid", func(t *testing.T) {
		// CLI validation fails before tx is broadcast, so we expect an error from the CLI
		_, err := helpers.SKUCreateSKU(ctx, chain, authority, "", "Empty Provider SKU", unit, basePrice, "")
		require.Error(t, err)
		require.Contains(t, err.Error(), "provider_uuid")
	})

	t.Run("fail: create SKU with inactive provider", func(t *testing.T) {
		// First create a provider, then deactivate it
		createRes, err := helpers.SKUCreateProvider(ctx, chain, authority, authority.FormattedAddress(), authority.FormattedAddress(), "")
		require.NoError(t, err)
		createTxRes, err := chain.GetTransaction(createRes.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), createTxRes.Code, "provider creation should succeed")

		inactiveProviderUUID, err := helpers.GetProviderUUIDFromTxHash(ctx, chain, createRes.TxHash)
		require.NoError(t, err)

		// Deactivate the provider
		deactivateRes, err := helpers.SKUDeactivateProvider(ctx, chain, authority, inactiveProviderUUID)
		require.NoError(t, err)
		deactivateTxRes, err := chain.GetTransaction(deactivateRes.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), deactivateTxRes.Code, "provider deactivation should succeed")

		// Now try to create a SKU for the inactive provider
		res, err := helpers.SKUCreateSKU(ctx, chain, authority, inactiveProviderUUID, "Inactive Provider SKU", unit, basePrice, "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "tx should fail")
		require.Contains(t, txRes.RawLog, "not active")
	})

	t.Run("fail: create SKU with non-evenly divisible price", func(t *testing.T) {
		// 3601 is not evenly divisible by 3600 (seconds in an hour)
		// This should fail CLI validation with "not evenly divisible" error
		_, err := helpers.SKUCreateSKU(ctx, chain, authority, providerUUID, "Non-Divisible SKU", unit, "3601umfx", "")
		require.Error(t, err)
		require.Contains(t, err.Error(), "not evenly divisible")
	})

	t.Run("fail: create SKU with non-evenly divisible per-day price", func(t *testing.T) {
		// 86401 is not evenly divisible by 86400 (seconds in a day)
		// This should fail CLI validation with "not evenly divisible" error
		_, err := helpers.SKUCreateSKU(ctx, chain, authority, providerUUID, "Non-Divisible SKU", 2, "86401umfx", "")
		require.Error(t, err)
		require.Contains(t, err.Error(), "not evenly divisible")
	})

	return skuUUID
}

func testSKUQuery(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, skuUUID, providerUUID string) {
	t.Log("=== Testing SKU Query ===")

	t.Run("success: query existing SKU", func(t *testing.T) {
		res, err := helpers.SKUQuerySKU(ctx, chain, skuUUID)
		require.NoError(t, err)
		require.Equal(t, skuUUID, res.Sku.Uuid)
		require.Equal(t, providerUUID, res.Sku.ProviderUuid)
	})

	t.Run("success: query all SKUs", func(t *testing.T) {
		res, err := helpers.SKUQuerySKUs(ctx, chain)
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(res.Skus), 2, "should have at least 2 SKUs")
	})
}

func testSKUUpdate(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, authority, user1 ibc.Wallet, skuUUID, providerUUID string) {
	t.Log("=== Testing SKU Update ===")

	// Price must be >= 3600 for UNIT_PER_HOUR to have non-zero per-second rate
	validPrice := testPriceHourly
	updatedPrice := "7200umfx"

	t.Run("success: authority updates SKU", func(t *testing.T) {
		newName := "Compute Small Updated"
		res, err := helpers.SKUUpdateSKU(ctx, chain, authority, skuUUID, providerUUID, newName, 1, updatedPrice, true, "cafebabe")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "tx should succeed: %s", txRes.RawLog)

		// Verify update
		skuRes, err := helpers.SKUQuerySKU(ctx, chain, skuUUID)
		require.NoError(t, err)
		require.Equal(t, newName, skuRes.Sku.Name)
		require.Equal(t, "7200", skuRes.Sku.BasePrice.Amount.String())
	})

	t.Run("fail: cannot deactivate SKU via update", func(t *testing.T) {
		// Deactivation via UpdateSKU is forbidden - must use DeactivateSKU instead
		res, err := helpers.SKUUpdateSKU(ctx, chain, authority, skuUUID, providerUUID, "Compute Small Updated", 1, updatedPrice, false, "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "tx should fail")
		require.Contains(t, txRes.RawLog, "cannot deactivate SKU via UpdateSKU")
	})

	t.Run("fail: unauthorized user updates SKU", func(t *testing.T) {
		res, err := helpers.SKUUpdateSKU(ctx, chain, user1, skuUUID, providerUUID, "Hacked Name", 1, validPrice, true, "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "tx should fail")
		require.Contains(t, txRes.RawLog, "unauthorized")
	})

	t.Run("fail: update with wrong provider_uuid", func(t *testing.T) {
		res, err := helpers.SKUUpdateSKU(ctx, chain, authority, skuUUID, nonExistentUUID, "Name", 1, validPrice, true, "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "tx should fail")
		require.Contains(t, txRes.RawLog, "provider_uuid mismatch")
	})

	t.Run("fail: update non-existent SKU", func(t *testing.T) {
		res, err := helpers.SKUUpdateSKU(ctx, chain, authority, nonExistentUUID, providerUUID, "Name", 1, validPrice, true, "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "tx should fail")
		require.Contains(t, txRes.RawLog, "not found")
	})

	t.Run("fail: update SKU with empty provider_uuid", func(t *testing.T) {
		// CLI validation fails before tx is broadcast, so we expect an error from the CLI
		_, err := helpers.SKUUpdateSKU(ctx, chain, authority, skuUUID, "", "Name", 1, validPrice, true, "")
		require.Error(t, err)
		require.Contains(t, err.Error(), "provider_uuid")
	})

	t.Run("fail: update SKU with non-evenly divisible price", func(t *testing.T) {
		// 3601 is not evenly divisible by 3600 (seconds in an hour)
		// This should fail CLI validation with "not evenly divisible" error
		_, err := helpers.SKUUpdateSKU(ctx, chain, authority, skuUUID, providerUUID, "Updated Name", 1, "3601umfx", true, "")
		require.Error(t, err)
		require.Contains(t, err.Error(), "not evenly divisible")
	})

	t.Run("fail: update SKU with non-evenly divisible per-day price", func(t *testing.T) {
		// 86401 is not evenly divisible by 86400 (seconds in a day)
		// This should fail CLI validation with "not evenly divisible" error
		_, err := helpers.SKUUpdateSKU(ctx, chain, authority, skuUUID, providerUUID, "Updated Name", 2, "86401umfx", true, "")
		require.Error(t, err)
		require.Contains(t, err.Error(), "not evenly divisible")
	})
}

func testSKUDeactivate(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, authority, user1 ibc.Wallet, providerUUID string) {
	t.Log("=== Testing SKU Deactivate ===")

	// Create a SKU specifically for deactivation
	// Price must be >= 3600 for UNIT_PER_HOUR to have non-zero per-second rate
	var skuUUIDToDeactivate string
	t.Run("setup: create SKU for deactivation", func(t *testing.T) {
		res, err := helpers.SKUCreateSKU(ctx, chain, authority, providerUUID, "To Be Deactivated", 1, testPriceHourly, "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code)

		skuUUIDToDeactivate, err = helpers.GetSKUUUIDFromTxHash(ctx, chain, res.TxHash)
		require.NoError(t, err)
	})

	t.Run("fail: unauthorized user deactivates SKU", func(t *testing.T) {
		res, err := helpers.SKUDeactivateSKU(ctx, chain, user1, skuUUIDToDeactivate)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "tx should fail")
		require.Contains(t, txRes.RawLog, "unauthorized")
	})

	t.Run("success: authority deactivates SKU", func(t *testing.T) {
		res, err := helpers.SKUDeactivateSKU(ctx, chain, authority, skuUUIDToDeactivate)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "tx should succeed: %s", txRes.RawLog)

		// Verify SKU is still queryable but inactive
		skuRes, err := helpers.SKUQuerySKU(ctx, chain, skuUUIDToDeactivate)
		require.NoError(t, err)
		require.False(t, skuRes.Sku.Active, "SKU should be inactive after deactivation")
	})

	t.Run("fail: deactivate already inactive SKU", func(t *testing.T) {
		res, err := helpers.SKUDeactivateSKU(ctx, chain, authority, skuUUIDToDeactivate)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "tx should fail")
		require.Contains(t, txRes.RawLog, "already inactive")
	})

	t.Run("fail: deactivate non-existent SKU", func(t *testing.T) {
		res, err := helpers.SKUDeactivateSKU(ctx, chain, authority, nonExistentUUID)
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
	var allowedProviderUUID string
	t.Run("setup: create provider for allowed list tests", func(t *testing.T) {
		res, err := helpers.SKUCreateProvider(ctx, chain, authority, authority.FormattedAddress(), authority.FormattedAddress(), "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code)

		allowedProviderUUID, err = helpers.GetProviderUUIDFromTxHash(ctx, chain, res.TxHash)
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
	validPrice := testPriceHourly

	var allowedSKUUUID string
	t.Run("success: allowed user creates SKU", func(t *testing.T) {
		res, err := helpers.SKUCreateSKU(ctx, chain, user1, allowedProviderUUID, "Allowed User SKU", 1, validPrice, "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "tx should succeed: %s", txRes.RawLog)

		allowedSKUUUID, err = helpers.GetSKUUUIDFromTxHash(ctx, chain, res.TxHash)
		require.NoError(t, err)
	})

	t.Run("fail: non-allowed user creates SKU", func(t *testing.T) {
		res, err := helpers.SKUCreateSKU(ctx, chain, user2, allowedProviderUUID, "Non-Allowed SKU", 1, validPrice, "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "tx should fail")
		require.Contains(t, txRes.RawLog, "unauthorized")
	})

	t.Run("success: allowed user updates SKU", func(t *testing.T) {
		// Price must be >= 86400 for UNIT_PER_DAY to have non-zero per-second rate
		res, err := helpers.SKUUpdateSKU(ctx, chain, user1, allowedSKUUUID, allowedProviderUUID, "Updated by Allowed", 2, "86400umfx", true, "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "tx should succeed: %s", txRes.RawLog)
	})

	t.Run("fail: non-allowed user updates SKU", func(t *testing.T) {
		res, err := helpers.SKUUpdateSKU(ctx, chain, user2, allowedSKUUUID, allowedProviderUUID, "Hacked", 1, validPrice, true, "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "tx should fail")
		require.Contains(t, txRes.RawLog, "unauthorized")
	})

	t.Run("success: allowed user deactivates SKU", func(t *testing.T) {
		res, err := helpers.SKUDeactivateSKU(ctx, chain, user1, allowedSKUUUID)
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
	var queryProviderUUID string
	t.Run("setup: create provider for query test", func(t *testing.T) {
		res, err := helpers.SKUCreateProvider(ctx, chain, authority, authority.FormattedAddress(), authority.FormattedAddress(), "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code)

		queryProviderUUID, err = helpers.GetProviderUUIDFromTxHash(ctx, chain, res.TxHash)
		require.NoError(t, err)
	})

	// Create multiple SKUs for the provider
	// Create 3 SKUs for the provider using valid units (1=UNIT_PER_HOUR, 2=UNIT_PER_DAY)
	// Price must be >= 86400 for UNIT_PER_DAY to have non-zero per-second rate
	t.Run("setup: create SKUs for provider", func(t *testing.T) {
		units := []int{1, 2, 1} // UNIT_PER_HOUR, UNIT_PER_DAY, UNIT_PER_HOUR
		for i := 0; i < 3; i++ {
			res, err := helpers.SKUCreateSKU(ctx, chain, authority, queryProviderUUID, "Query Test SKU "+string(rune('1'+i)), units[i], "86400umfx", "")
			require.NoError(t, err)

			txRes, err := chain.GetTransaction(res.TxHash)
			require.NoError(t, err)
			require.Equal(t, uint32(0), txRes.Code)
		}
	})

	t.Run("success: query SKUs by provider", func(t *testing.T) {
		res, err := helpers.SKUQuerySKUsByProvider(ctx, chain, queryProviderUUID)
		require.NoError(t, err)
		require.Len(t, res.Skus, 3, "should have 3 SKUs for this provider")

		for _, sku := range res.Skus {
			require.Equal(t, queryProviderUUID, sku.ProviderUuid)
		}
	})

	t.Run("success: query SKUs by non-existent provider returns empty", func(t *testing.T) {
		res, err := helpers.SKUQuerySKUsByProvider(ctx, chain, nonExistentUUID)
		require.NoError(t, err)
		require.Empty(t, res.Skus)
	})
}

func testSKUPagination(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, authority ibc.Wallet) {
	t.Log("=== Testing SKU Pagination ===")

	// Create a new provider for pagination testing
	var paginationProviderUUID string
	t.Run("setup: create provider for pagination test", func(t *testing.T) {
		res, err := helpers.SKUCreateProvider(ctx, chain, authority, authority.FormattedAddress(), authority.FormattedAddress(), "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code)

		paginationProviderUUID, err = helpers.GetProviderUUIDFromTxHash(ctx, chain, res.TxHash)
		require.NoError(t, err)
	})

	// Create multiple SKUs for pagination testing
	// Price must be >= 3600 for UNIT_PER_HOUR to have non-zero per-second rate
	t.Run("setup: create SKUs for pagination", func(t *testing.T) {
		for i := 1; i <= 5; i++ {
			res, err := helpers.SKUCreateSKU(ctx, chain, authority, paginationProviderUUID, "Pagination SKU "+string(rune('0'+i)), 1, testPriceHourly, "")
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
		page1UUIDs := make(map[string]bool)
		for _, sku := range res1.Skus {
			page1UUIDs[sku.Uuid] = true
		}
		for _, sku := range res2.Skus {
			require.False(t, page1UUIDs[sku.Uuid], "SKU %s should not appear in both pages", sku.Uuid)
		}
	})

	t.Run("success: paginate SKUs by provider", func(t *testing.T) {
		// First page with limit 2
		res1, nextKey, err := helpers.SKUQuerySKUsByProviderPaginated(ctx, chain, paginationProviderUUID, 2, "")
		require.NoError(t, err)
		require.Len(t, res1.Skus, 2, "first page should have 2 SKUs")
		require.NotNil(t, res1.Pagination, "pagination info should be present")
		require.NotEmpty(t, nextKey, "next key should be present for more pages")

		for _, sku := range res1.Skus {
			require.Equal(t, paginationProviderUUID, sku.ProviderUuid)
		}

		// Second page using next key
		res2, nextKey, err := helpers.SKUQuerySKUsByProviderPaginated(ctx, chain, paginationProviderUUID, 2, nextKey)
		require.NoError(t, err)
		require.Len(t, res2.Skus, 2, "second page should have 2 SKUs")

		for _, sku := range res2.Skus {
			require.Equal(t, paginationProviderUUID, sku.ProviderUuid)
		}

		// Third page - should have 1 remaining SKU
		res3, _, err := helpers.SKUQuerySKUsByProviderPaginated(ctx, chain, paginationProviderUUID, 2, nextKey)
		require.NoError(t, err)
		require.Len(t, res3.Skus, 1, "third page should have 1 SKU")

		// Verify no duplicates across all pages
		allUUIDs := make(map[string]bool)
		for _, sku := range res1.Skus {
			require.False(t, allUUIDs[sku.Uuid], "duplicate SKU UUID found")
			allUUIDs[sku.Uuid] = true
		}
		for _, sku := range res2.Skus {
			require.False(t, allUUIDs[sku.Uuid], "duplicate SKU UUID found")
			allUUIDs[sku.Uuid] = true
		}
		for _, sku := range res3.Skus {
			require.False(t, allUUIDs[sku.Uuid], "duplicate SKU UUID found")
			allUUIDs[sku.Uuid] = true
		}
		require.Len(t, allUUIDs, 5, "should have collected all 5 SKUs across pages")
	})

	t.Run("success: large limit returns all results", func(t *testing.T) {
		res, _, err := helpers.SKUQuerySKUsByProviderPaginated(ctx, chain, paginationProviderUUID, 100, "")
		require.NoError(t, err)
		require.Len(t, res.Skus, 5, "should return all 5 SKUs")
	})
}

// testSKUMultiDenom tests that SKUs can be created with different denoms
func testSKUMultiDenom(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, authority ibc.Wallet) {
	t.Log("=== Testing SKU Multi-Denom Support ===")

	// Create a new provider for multi-denom testing
	var multiDenomProviderUUID string
	t.Run("setup: create provider for multi-denom tests", func(t *testing.T) {
		res, err := helpers.SKUCreateProvider(ctx, chain, authority, authority.FormattedAddress(), authority.FormattedAddress(), "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code)

		multiDenomProviderUUID, err = helpers.GetProviderUUIDFromTxHash(ctx, chain, res.TxHash)
		require.NoError(t, err)
	})

	// Define test denoms
	denom1 := "umfx"
	denom2 := "upwr"
	denom3 := "utest"

	var sku1UUID, sku2UUID, sku3UUID string

	t.Run("success: create SKU with umfx denom", func(t *testing.T) {
		// Price must be >= 3600 for UNIT_PER_HOUR
		res, err := helpers.SKUCreateSKU(ctx, chain, authority, multiDenomProviderUUID, "Compute MFX", 1, "3600"+denom1, "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "tx should succeed: %s", txRes.RawLog)

		sku1UUID, err = helpers.GetSKUUUIDFromTxHash(ctx, chain, res.TxHash)
		require.NoError(t, err)

		// Verify SKU has correct denom
		skuRes, err := helpers.SKUQuerySKU(ctx, chain, sku1UUID)
		require.NoError(t, err)
		require.Equal(t, denom1, skuRes.Sku.BasePrice.Denom, "SKU should have umfx denom")
		require.Equal(t, "3600", skuRes.Sku.BasePrice.Amount.String())
	})

	t.Run("success: create SKU with upwr denom", func(t *testing.T) {
		// Price must be >= 86400 for UNIT_PER_DAY
		res, err := helpers.SKUCreateSKU(ctx, chain, authority, multiDenomProviderUUID, "Storage PWR", 2, "86400"+denom2, "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "tx should succeed: %s", txRes.RawLog)

		sku2UUID, err = helpers.GetSKUUUIDFromTxHash(ctx, chain, res.TxHash)
		require.NoError(t, err)

		// Verify SKU has correct denom
		skuRes, err := helpers.SKUQuerySKU(ctx, chain, sku2UUID)
		require.NoError(t, err)
		require.Equal(t, denom2, skuRes.Sku.BasePrice.Denom, "SKU should have upwr denom")
		require.Equal(t, "86400", skuRes.Sku.BasePrice.Amount.String())
	})

	t.Run("success: create SKU with custom utest denom", func(t *testing.T) {
		res, err := helpers.SKUCreateSKU(ctx, chain, authority, multiDenomProviderUUID, "Network TEST", 1, "7200"+denom3, "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "tx should succeed: %s", txRes.RawLog)

		sku3UUID, err = helpers.GetSKUUUIDFromTxHash(ctx, chain, res.TxHash)
		require.NoError(t, err)

		// Verify SKU has correct denom
		skuRes, err := helpers.SKUQuerySKU(ctx, chain, sku3UUID)
		require.NoError(t, err)
		require.Equal(t, denom3, skuRes.Sku.BasePrice.Denom, "SKU should have utest denom")
		require.Equal(t, "7200", skuRes.Sku.BasePrice.Amount.String())
	})

	t.Run("success: update SKU to different denom", func(t *testing.T) {
		// Update SKU 1 from umfx to upwr
		res, err := helpers.SKUUpdateSKU(ctx, chain, authority, sku1UUID, multiDenomProviderUUID, "Compute PWR Updated", 1, "3600"+denom2, true, "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "tx should succeed: %s", txRes.RawLog)

		// Verify SKU now has the new denom
		skuRes, err := helpers.SKUQuerySKU(ctx, chain, sku1UUID)
		require.NoError(t, err)
		require.Equal(t, denom2, skuRes.Sku.BasePrice.Denom, "SKU should now have upwr denom")
		require.Equal(t, "Compute PWR Updated", skuRes.Sku.Name)
	})

	t.Run("success: query all SKUs returns multiple denoms", func(t *testing.T) {
		res, err := helpers.SKUQuerySKUsByProvider(ctx, chain, multiDenomProviderUUID)
		require.NoError(t, err)
		require.Len(t, res.Skus, 3, "should have 3 SKUs for this provider")

		// Collect denoms
		denoms := make(map[string]bool)
		for _, sku := range res.Skus {
			denoms[sku.BasePrice.Denom] = true
		}

		// After update, we should have 2 upwr and 1 utest
		require.True(t, denoms[denom2], "should have upwr denom")
		require.True(t, denoms[denom3], "should have utest denom")
	})

	t.Run("success: verify SKU UUIDs are correct", func(t *testing.T) {
		// Verify all three SKUs exist
		_, err := helpers.SKUQuerySKU(ctx, chain, sku1UUID)
		require.NoError(t, err)

		_, err = helpers.SKUQuerySKU(ctx, chain, sku2UUID)
		require.NoError(t, err)

		_, err = helpers.SKUQuerySKU(ctx, chain, sku3UUID)
		require.NoError(t, err)
	})
}

// testProviderFullFields tests provider creation and update with api_url field.
func testProviderFullFields(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, authority ibc.Wallet) {
	t.Log("=== Testing Provider Full Fields (api_url) ===")

	address := authority.FormattedAddress()
	payoutAddress := authority.FormattedAddress()

	// Test values
	apiURL := "https://api.provider.example.com"

	var providerUUID string

	t.Run("success: create provider with api_url", func(t *testing.T) {
		res, err := helpers.SKUCreateProviderFull(ctx, chain, authority, address, payoutAddress, "", apiURL)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "tx should succeed: %s", txRes.RawLog)

		providerUUID, err = helpers.GetProviderUUIDFromTxHash(ctx, chain, res.TxHash)
		require.NoError(t, err)

		// Verify provider was created with all fields
		providerRes, err := helpers.SKUQueryProvider(ctx, chain, providerUUID)
		require.NoError(t, err)
		require.Equal(t, address, providerRes.Provider.Address)
		require.Equal(t, payoutAddress, providerRes.Provider.PayoutAddress)
		require.Equal(t, apiURL, providerRes.Provider.ApiUrl)
		require.True(t, providerRes.Provider.Active)
	})

	t.Run("success: update provider api_url", func(t *testing.T) {
		newAPIURL := "https://api.updated-provider.example.com"
		res, err := helpers.SKUUpdateProviderFull(ctx, chain, authority, providerUUID, address, payoutAddress, true, "", newAPIURL)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "tx should succeed: %s", txRes.RawLog)

		// Verify api_url was updated
		providerRes, err := helpers.SKUQueryProvider(ctx, chain, providerUUID)
		require.NoError(t, err)
		require.Equal(t, newAPIURL, providerRes.Provider.ApiUrl)
	})

	t.Run("success: create provider without api_url", func(t *testing.T) {
		// Create provider without api_url (empty string)
		res, err := helpers.SKUCreateProviderFull(ctx, chain, authority, address, payoutAddress, "", "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "tx should succeed: %s", txRes.RawLog)

		newProviderUUID, err := helpers.GetProviderUUIDFromTxHash(ctx, chain, res.TxHash)
		require.NoError(t, err)

		// Verify provider was created with empty api_url
		providerRes, err := helpers.SKUQueryProvider(ctx, chain, newProviderUUID)
		require.NoError(t, err)
		require.Empty(t, providerRes.Provider.ApiUrl)
	})

	t.Run("success: update preserves existing values when not provided", func(t *testing.T) {
		// First create a provider with all fields
		res, err := helpers.SKUCreateProviderFull(ctx, chain, authority, address, payoutAddress, "", apiURL)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code)

		preserveProviderUUID, err := helpers.GetProviderUUIDFromTxHash(ctx, chain, res.TxHash)
		require.NoError(t, err)

		// Update using the basic update function (without new field values)
		// This should preserve existing api_url
		res, err = helpers.SKUUpdateProvider(ctx, chain, authority, preserveProviderUUID, address, payoutAddress, true, validMetaHashHex)
		require.NoError(t, err)

		txRes, err = chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "tx should succeed: %s", txRes.RawLog)

		// Verify existing field values are preserved
		providerRes, err := helpers.SKUQueryProvider(ctx, chain, preserveProviderUUID)
		require.NoError(t, err)
		require.Equal(t, apiURL, providerRes.Provider.ApiUrl, "api_url should be preserved")
	})
}

// testSKUUnitPerDay tests SKU creation and updates with UNIT_PER_DAY pricing model.
func testSKUUnitPerDay(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, authority ibc.Wallet) {
	t.Log("=== Testing SKU UNIT_PER_DAY Pricing Model ===")

	// Create a provider for UNIT_PER_DAY testing
	var dailyProviderUUID string
	t.Run("setup: create provider for daily pricing tests", func(t *testing.T) {
		res, err := helpers.SKUCreateProvider(ctx, chain, authority, authority.FormattedAddress(), authority.FormattedAddress(), "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code)

		dailyProviderUUID, err = helpers.GetProviderUUIDFromTxHash(ctx, chain, res.TxHash)
		require.NoError(t, err)
	})

	var dailySKUUUID string

	t.Run("success: create SKU with UNIT_PER_DAY", func(t *testing.T) {
		// Unit 2 = UNIT_PER_DAY, price must be >= 86400 for non-zero per-second rate
		res, err := helpers.SKUCreateSKU(ctx, chain, authority, dailyProviderUUID, "Daily Storage", 2, testPriceDaily, "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "tx should succeed: %s", txRes.RawLog)

		dailySKUUUID, err = helpers.GetSKUUUIDFromTxHash(ctx, chain, res.TxHash)
		require.NoError(t, err)

		// Verify SKU was created with correct unit
		skuRes, err := helpers.SKUQuerySKU(ctx, chain, dailySKUUUID)
		require.NoError(t, err)
		require.Equal(t, dailyProviderUUID, skuRes.Sku.ProviderUuid)
		require.Equal(t, "Daily Storage", skuRes.Sku.Name)
		require.Equal(t, skutypes.Unit_UNIT_PER_DAY, skuRes.Sku.Unit, "SKU should have UNIT_PER_DAY")
		require.Equal(t, "86400", skuRes.Sku.BasePrice.Amount.String())
		require.True(t, skuRes.Sku.Active)
	})

	t.Run("success: create SKU with higher daily price", func(t *testing.T) {
		// 172800 = 2 * 86400 (2 umfx per second)
		res, err := helpers.SKUCreateSKU(ctx, chain, authority, dailyProviderUUID, "Premium Daily Storage", 2, "172800umfx", "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "tx should succeed: %s", txRes.RawLog)

		skuUUID, err := helpers.GetSKUUUIDFromTxHash(ctx, chain, res.TxHash)
		require.NoError(t, err)

		skuRes, err := helpers.SKUQuerySKU(ctx, chain, skuUUID)
		require.NoError(t, err)
		require.Equal(t, skutypes.Unit_UNIT_PER_DAY, skuRes.Sku.Unit)
		require.Equal(t, "172800", skuRes.Sku.BasePrice.Amount.String())
	})

	t.Run("fail: create UNIT_PER_DAY SKU with price too low", func(t *testing.T) {
		// 86399 is less than 86400 (seconds in a day), so per-second rate would be 0
		_, err := helpers.SKUCreateSKU(ctx, chain, authority, dailyProviderUUID, "Too Cheap Daily", 2, "86399umfx", "")
		require.Error(t, err, "should fail with price that results in zero per-second rate")
		require.Contains(t, err.Error(), "zero")
	})

	t.Run("fail: create UNIT_PER_DAY SKU with non-divisible price", func(t *testing.T) {
		// 86401 is not evenly divisible by 86400
		_, err := helpers.SKUCreateSKU(ctx, chain, authority, dailyProviderUUID, "Non-Divisible Daily", 2, "86401umfx", "")
		require.Error(t, err, "should fail with non-evenly divisible price")
		require.Contains(t, err.Error(), "divisible")
	})

	t.Run("success: update SKU from UNIT_PER_HOUR to UNIT_PER_DAY", func(t *testing.T) {
		// First create an hourly SKU
		res, err := helpers.SKUCreateSKU(ctx, chain, authority, dailyProviderUUID, "Hourly To Daily", 1, testPriceHourly, "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code)

		hourlyUUID, err := helpers.GetSKUUUIDFromTxHash(ctx, chain, res.TxHash)
		require.NoError(t, err)

		// Verify it's UNIT_PER_HOUR
		skuRes, err := helpers.SKUQuerySKU(ctx, chain, hourlyUUID)
		require.NoError(t, err)
		require.Equal(t, skutypes.Unit_UNIT_PER_HOUR, skuRes.Sku.Unit)

		// Update to UNIT_PER_DAY
		updateRes, err := helpers.SKUUpdateSKU(ctx, chain, authority, hourlyUUID, dailyProviderUUID, "Now Daily", 2, testPriceDaily, true, "")
		require.NoError(t, err)

		updateTxRes, err := chain.GetTransaction(updateRes.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), updateTxRes.Code, "tx should succeed: %s", updateTxRes.RawLog)

		// Verify it's now UNIT_PER_DAY
		skuRes, err = helpers.SKUQuerySKU(ctx, chain, hourlyUUID)
		require.NoError(t, err)
		require.Equal(t, skutypes.Unit_UNIT_PER_DAY, skuRes.Sku.Unit)
		require.Equal(t, "Now Daily", skuRes.Sku.Name)
		require.Equal(t, "86400", skuRes.Sku.BasePrice.Amount.String())
	})

	t.Run("success: update daily SKU price", func(t *testing.T) {
		// Update the daily SKU to a higher price (259200 = 3 * 86400 = 3 umfx/second)
		updateRes, err := helpers.SKUUpdateSKU(ctx, chain, authority, dailySKUUUID, dailyProviderUUID, "Daily Storage Updated", 2, "259200umfx", true, "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(updateRes.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "tx should succeed: %s", txRes.RawLog)

		skuRes, err := helpers.SKUQuerySKU(ctx, chain, dailySKUUUID)
		require.NoError(t, err)
		require.Equal(t, skutypes.Unit_UNIT_PER_DAY, skuRes.Sku.Unit)
		require.Equal(t, "Daily Storage Updated", skuRes.Sku.Name)
		require.Equal(t, "259200", skuRes.Sku.BasePrice.Amount.String())
	})

	t.Run("success: query SKUs includes daily priced SKUs", func(t *testing.T) {
		res, err := helpers.SKUQuerySKUsByProvider(ctx, chain, dailyProviderUUID)
		require.NoError(t, err)

		// Count SKUs by unit type
		hourlyCount := 0
		dailyCount := 0
		for _, sku := range res.Skus {
			switch sku.Unit {
			case skutypes.Unit_UNIT_PER_HOUR:
				hourlyCount++
			case skutypes.Unit_UNIT_PER_DAY:
				dailyCount++
			}
		}

		require.GreaterOrEqual(t, dailyCount, 2, "should have at least 2 daily SKUs")
		t.Logf("Provider has %d hourly SKUs and %d daily SKUs", hourlyCount, dailyCount)
	})
}

// testProviderPagination tests provider query pagination.
func testProviderPagination(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, authority ibc.Wallet) {
	t.Log("=== Testing Provider Pagination ===")

	// Create multiple providers to test pagination
	var providerUUIDs []string
	for i := 0; i < 5; i++ {
		res, err := helpers.SKUCreateProvider(ctx, chain, authority, authority.FormattedAddress(), authority.FormattedAddress(), "")
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "provider creation should succeed")

		uuid, err := helpers.GetProviderUUIDFromTxHash(ctx, chain, res.TxHash)
		require.NoError(t, err)
		providerUUIDs = append(providerUUIDs, uuid)
	}

	t.Run("success: paginate through all providers", func(t *testing.T) {
		// Query first page (limit 2)
		res1, nextKey, err := helpers.SKUQueryProvidersPaginated(ctx, chain, 2, "")
		require.NoError(t, err)
		require.Len(t, res1.Providers, 2, "first page should have 2 providers")
		require.NotEmpty(t, nextKey, "should have next key for more pages")

		// Query second page
		res2, _, err := helpers.SKUQueryProvidersPaginated(ctx, chain, 2, nextKey)
		require.NoError(t, err)
		require.NotEmpty(t, res2.Providers, "second page should have providers")

		t.Logf("Page 1: %d providers, Page 2: %d providers", len(res1.Providers), len(res2.Providers))
	})

	t.Run("success: large limit returns all results", func(t *testing.T) {
		res, nextKey, err := helpers.SKUQueryProvidersPaginated(ctx, chain, 100, "")
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(res.Providers), 5, "should return all providers")
		require.Empty(t, nextKey, "no next key when all results fit in page")
	})

	t.Run("success: verify no duplicates across pages", func(t *testing.T) {
		seen := make(map[string]bool)
		var nextKey string
		var err error

		for {
			var res *helpers.ProvidersResponseJSON
			res, nextKey, err = helpers.SKUQueryProvidersPaginated(ctx, chain, 2, nextKey)
			require.NoError(t, err)

			for _, provider := range res.Providers {
				require.False(t, seen[provider.Uuid], "duplicate provider found: %s", provider.Uuid)
				seen[provider.Uuid] = true
			}

			if nextKey == "" {
				break
			}
		}

		t.Logf("Verified %d unique providers across pages", len(seen))
	})

	// Cleanup: deactivate test providers
	for _, uuid := range providerUUIDs {
		_, _ = helpers.SKUDeactivateProvider(ctx, chain, authority, uuid)
	}
}

// testSKUValidation tests SKU validation edge cases.
func testSKUValidation(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, authority ibc.Wallet) {
	t.Log("=== Testing SKU Validation ===")

	// Create a provider for testing
	res, err := helpers.SKUCreateProvider(ctx, chain, authority, authority.FormattedAddress(), authority.FormattedAddress(), "")
	require.NoError(t, err)
	txRes, err := chain.GetTransaction(res.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), txRes.Code, "provider creation should succeed")

	providerUUID, err := helpers.GetProviderUUIDFromTxHash(ctx, chain, res.TxHash)
	require.NoError(t, err)

	t.Run("fail: create SKU with UNIT_UNSPECIFIED", func(t *testing.T) {
		// Unit 0 is UNIT_UNSPECIFIED which is invalid - CLI validates before broadcast
		_, err := helpers.SKUCreateSKU(ctx, chain, authority, providerUUID, "Invalid Unit SKU", 0, testPriceHourly, "")
		require.Error(t, err, "CLI should reject UNIT_UNSPECIFIED")
		require.Contains(t, err.Error(), "unit")

		t.Log("Correctly rejected SKU with UNIT_UNSPECIFIED")
	})

	t.Run("fail: create SKU with empty name", func(t *testing.T) {
		// Empty name should be rejected - CLI validates before broadcast
		_, err := helpers.SKUCreateSKU(ctx, chain, authority, providerUUID, "", 1, testPriceHourly, "")
		require.Error(t, err, "CLI should reject empty name")
		require.Contains(t, err.Error(), "name")

		t.Log("Correctly rejected SKU with empty name")
	})

	t.Run("fail: update SKU with UNIT_UNSPECIFIED", func(t *testing.T) {
		// First create a valid SKU
		createRes, err := helpers.SKUCreateSKU(ctx, chain, authority, providerUUID, "Valid SKU", 1, testPriceHourly, "")
		require.NoError(t, err)
		createTxRes, err := chain.GetTransaction(createRes.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), createTxRes.Code, "SKU creation should succeed")

		skuUUID, err := helpers.GetSKUUUIDFromTxHash(ctx, chain, createRes.TxHash)
		require.NoError(t, err)

		// Try to update with UNIT_UNSPECIFIED - CLI validates before broadcast
		_, err = helpers.SKUUpdateSKU(ctx, chain, authority, skuUUID, providerUUID, "Updated SKU", 0, testPriceHourly, true, "")
		require.Error(t, err, "CLI should reject UNIT_UNSPECIFIED")
		require.Contains(t, err.Error(), "unit")

		t.Log("Correctly rejected SKU update with UNIT_UNSPECIFIED")
	})

	t.Run("fail: update SKU with empty name", func(t *testing.T) {
		// First create a valid SKU
		createRes, err := helpers.SKUCreateSKU(ctx, chain, authority, providerUUID, "Another Valid SKU", 1, testPriceHourly, "")
		require.NoError(t, err)
		createTxRes, err := chain.GetTransaction(createRes.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), createTxRes.Code, "SKU creation should succeed")

		skuUUID, err := helpers.GetSKUUUIDFromTxHash(ctx, chain, createRes.TxHash)
		require.NoError(t, err)

		// Try to update with empty name - CLI validates before broadcast
		_, err = helpers.SKUUpdateSKU(ctx, chain, authority, skuUUID, providerUUID, "", 1, testPriceHourly, true, "")
		require.Error(t, err, "CLI should reject empty name")
		require.Contains(t, err.Error(), "name")

		t.Log("Correctly rejected SKU update with empty name")
	})

	// Cleanup: deactivate the test provider
	_, _ = helpers.SKUDeactivateProvider(ctx, chain, authority, providerUUID)
}

// testSKUInvalidUUID tests that invalid UUID formats are rejected.
func testSKUInvalidUUID(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, authority ibc.Wallet) {
	t.Log("=== Testing Invalid UUID Format Rejection ===")

	// Invalid UUID formats to test
	invalidUUIDs := []struct {
		uuid string
		desc string
	}{
		{"not-a-uuid", "plain string"},
		{"12345", "numeric string"},
		{"01234567-89ab-cdef-0123-456789abcdef", "UUIDv4 format (not v7)"},
		{"01912345-6789-7abc-8def-0123456789a", "too short"},
		{"01912345-6789-7abc-8def-0123456789abcd", "too long"},
		{"01912345-6789-7abc-8def-0123456789ag", "invalid character"},
	}

	t.Run("fail: update provider with invalid uuid", func(t *testing.T) {
		for _, tc := range invalidUUIDs {
			res, err := helpers.SKUUpdateProvider(ctx, chain, authority, tc.uuid, authority.FormattedAddress(), authority.FormattedAddress(), true, "")
			// CLI validation should reject invalid UUIDs
			if err != nil {
				require.Contains(t, err.Error(), "uuid", "invalid uuid (%s) should be rejected: %s", tc.desc, tc.uuid)
			} else {
				txRes, err := chain.GetTransaction(res.TxHash)
				require.NoError(t, err)
				require.NotEqual(t, uint32(0), txRes.Code, "invalid uuid (%s) should fail: %s", tc.desc, tc.uuid)
			}
		}
		t.Log("Correctly rejected update provider with invalid UUIDs")
	})

	t.Run("fail: deactivate provider with invalid uuid", func(t *testing.T) {
		for _, tc := range invalidUUIDs {
			res, err := helpers.SKUDeactivateProvider(ctx, chain, authority, tc.uuid)
			if err != nil {
				require.Contains(t, err.Error(), "uuid", "invalid uuid (%s) should be rejected: %s", tc.desc, tc.uuid)
			} else {
				txRes, err := chain.GetTransaction(res.TxHash)
				require.NoError(t, err)
				require.NotEqual(t, uint32(0), txRes.Code, "invalid uuid (%s) should fail: %s", tc.desc, tc.uuid)
			}
		}
		t.Log("Correctly rejected deactivate provider with invalid UUIDs")
	})

	t.Run("fail: create SKU with invalid provider_uuid", func(t *testing.T) {
		for _, tc := range invalidUUIDs {
			res, err := helpers.SKUCreateSKU(ctx, chain, authority, tc.uuid, "Test SKU", 1, testPriceHourly, "")
			if err != nil {
				require.Contains(t, err.Error(), "uuid", "invalid provider_uuid (%s) should be rejected: %s", tc.desc, tc.uuid)
			} else {
				txRes, err := chain.GetTransaction(res.TxHash)
				require.NoError(t, err)
				require.NotEqual(t, uint32(0), txRes.Code, "invalid provider_uuid (%s) should fail: %s", tc.desc, tc.uuid)
			}
		}
		t.Log("Correctly rejected create SKU with invalid provider_uuid")
	})

	t.Run("fail: update SKU with invalid uuid", func(t *testing.T) {
		for _, tc := range invalidUUIDs {
			// Use a valid provider UUID but invalid SKU UUID
			res, err := helpers.SKUUpdateSKU(ctx, chain, authority, tc.uuid, nonExistentUUID, "Test SKU", 1, testPriceHourly, true, "")
			if err != nil {
				require.Contains(t, err.Error(), "uuid", "invalid uuid (%s) should be rejected: %s", tc.desc, tc.uuid)
			} else {
				txRes, err := chain.GetTransaction(res.TxHash)
				require.NoError(t, err)
				require.NotEqual(t, uint32(0), txRes.Code, "invalid uuid (%s) should fail: %s", tc.desc, tc.uuid)
			}
		}
		t.Log("Correctly rejected update SKU with invalid uuid")
	})

	t.Run("fail: update SKU with invalid provider_uuid", func(t *testing.T) {
		for _, tc := range invalidUUIDs {
			// Use a valid SKU UUID but invalid provider UUID
			res, err := helpers.SKUUpdateSKU(ctx, chain, authority, nonExistentUUID, tc.uuid, "Test SKU", 1, testPriceHourly, true, "")
			if err != nil {
				require.Contains(t, err.Error(), "uuid", "invalid provider_uuid (%s) should be rejected: %s", tc.desc, tc.uuid)
			} else {
				txRes, err := chain.GetTransaction(res.TxHash)
				require.NoError(t, err)
				require.NotEqual(t, uint32(0), txRes.Code, "invalid provider_uuid (%s) should fail: %s", tc.desc, tc.uuid)
			}
		}
		t.Log("Correctly rejected update SKU with invalid provider_uuid")
	})

	t.Run("fail: deactivate SKU with invalid uuid", func(t *testing.T) {
		for _, tc := range invalidUUIDs {
			res, err := helpers.SKUDeactivateSKU(ctx, chain, authority, tc.uuid)
			if err != nil {
				require.Contains(t, err.Error(), "uuid", "invalid uuid (%s) should be rejected: %s", tc.desc, tc.uuid)
			} else {
				txRes, err := chain.GetTransaction(res.TxHash)
				require.NoError(t, err)
				require.NotEqual(t, uint32(0), txRes.Code, "invalid uuid (%s) should fail: %s", tc.desc, tc.uuid)
			}
		}
		t.Log("Correctly rejected deactivate SKU with invalid uuid")
	})

	t.Run("fail: query provider with invalid uuid", func(t *testing.T) {
		for _, tc := range invalidUUIDs {
			_, err := helpers.SKUQueryProvider(ctx, chain, tc.uuid)
			require.Error(t, err, "query provider with invalid uuid (%s) should fail: %s", tc.desc, tc.uuid)
		}
		t.Log("Correctly rejected query provider with invalid UUIDs")
	})

	t.Run("fail: query SKU with invalid uuid", func(t *testing.T) {
		for _, tc := range invalidUUIDs {
			_, err := helpers.SKUQuerySKU(ctx, chain, tc.uuid)
			require.Error(t, err, "query SKU with invalid uuid (%s) should fail: %s", tc.desc, tc.uuid)
		}
		t.Log("Correctly rejected query SKU with invalid UUIDs")
	})
}

// testSKUEmptyParams tests that empty string parameters are rejected.
func testSKUEmptyParams(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, authority ibc.Wallet) {
	t.Log("=== Testing Empty String Parameter Rejection ===")

	t.Run("fail: create provider with empty address", func(t *testing.T) {
		// Empty address should be rejected
		_, err := helpers.SKUCreateProvider(ctx, chain, authority, "", authority.FormattedAddress(), "")
		require.Error(t, err, "create provider with empty address should fail")
		t.Log("Correctly rejected create provider with empty address")
	})

	t.Run("fail: create provider with empty payout_address", func(t *testing.T) {
		// Empty payout address should be rejected
		_, err := helpers.SKUCreateProvider(ctx, chain, authority, authority.FormattedAddress(), "", "")
		require.Error(t, err, "create provider with empty payout_address should fail")
		t.Log("Correctly rejected create provider with empty payout_address")
	})

	// Create a provider for update tests
	createRes, err := helpers.SKUCreateProvider(ctx, chain, authority, authority.FormattedAddress(), authority.FormattedAddress(), "")
	require.NoError(t, err)
	createTxRes, err := chain.GetTransaction(createRes.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), createTxRes.Code, "provider creation should succeed")

	providerUUID, err := helpers.GetProviderUUIDFromTxHash(ctx, chain, createRes.TxHash)
	require.NoError(t, err)

	t.Run("fail: update provider with empty uuid", func(t *testing.T) {
		// Empty uuid should be rejected
		_, err := helpers.SKUUpdateProvider(ctx, chain, authority, "", authority.FormattedAddress(), authority.FormattedAddress(), true, "")
		require.Error(t, err, "update provider with empty uuid should fail")
		t.Log("Correctly rejected update provider with empty uuid")
	})

	t.Run("fail: update provider with empty address", func(t *testing.T) {
		// Empty address should be rejected
		_, err := helpers.SKUUpdateProvider(ctx, chain, authority, providerUUID, "", authority.FormattedAddress(), true, "")
		require.Error(t, err, "update provider with empty address should fail")
		t.Log("Correctly rejected update provider with empty address")
	})

	t.Run("fail: update provider with empty payout_address", func(t *testing.T) {
		// Empty payout address should be rejected
		_, err := helpers.SKUUpdateProvider(ctx, chain, authority, providerUUID, authority.FormattedAddress(), "", true, "")
		require.Error(t, err, "update provider with empty payout_address should fail")
		t.Log("Correctly rejected update provider with empty payout_address")
	})

	t.Run("fail: deactivate provider with empty uuid", func(t *testing.T) {
		// Empty uuid should be rejected
		_, err := helpers.SKUDeactivateProvider(ctx, chain, authority, "")
		require.Error(t, err, "deactivate provider with empty uuid should fail")
		t.Log("Correctly rejected deactivate provider with empty uuid")
	})

	t.Run("fail: create SKU with empty provider_uuid", func(t *testing.T) {
		// Empty provider_uuid should be rejected (test may already exist)
		_, err := helpers.SKUCreateSKU(ctx, chain, authority, "", "Test SKU", 1, testPriceHourly, "")
		require.Error(t, err, "create SKU with empty provider_uuid should fail")
		t.Log("Correctly rejected create SKU with empty provider_uuid")
	})

	// Create a SKU for update tests
	skuRes, err := helpers.SKUCreateSKU(ctx, chain, authority, providerUUID, "Test SKU for Empty Params", 1, testPriceHourly, "")
	require.NoError(t, err)
	skuTxRes, err := chain.GetTransaction(skuRes.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), skuTxRes.Code, "SKU creation should succeed")

	skuUUID, err := helpers.GetSKUUUIDFromTxHash(ctx, chain, skuRes.TxHash)
	require.NoError(t, err)

	t.Run("fail: update SKU with empty uuid", func(t *testing.T) {
		// Empty uuid should be rejected
		_, err := helpers.SKUUpdateSKU(ctx, chain, authority, "", providerUUID, "Updated SKU", 1, testPriceHourly, true, "")
		require.Error(t, err, "update SKU with empty uuid should fail")
		t.Log("Correctly rejected update SKU with empty uuid")
	})

	t.Run("fail: update SKU with empty provider_uuid", func(t *testing.T) {
		// Empty provider_uuid should be rejected
		_, err := helpers.SKUUpdateSKU(ctx, chain, authority, skuUUID, "", "Updated SKU", 1, testPriceHourly, true, "")
		require.Error(t, err, "update SKU with empty provider_uuid should fail")
		t.Log("Correctly rejected update SKU with empty provider_uuid")
	})

	t.Run("fail: deactivate SKU with empty uuid", func(t *testing.T) {
		// Empty uuid should be rejected
		_, err := helpers.SKUDeactivateSKU(ctx, chain, authority, "")
		require.Error(t, err, "deactivate SKU with empty uuid should fail")
		t.Log("Correctly rejected deactivate SKU with empty uuid")
	})

	t.Run("fail: query provider with empty uuid", func(t *testing.T) {
		// Empty uuid should be rejected
		_, err := helpers.SKUQueryProvider(ctx, chain, "")
		require.Error(t, err, "query provider with empty uuid should fail")
		t.Log("Correctly rejected query provider with empty uuid")
	})

	t.Run("fail: query SKU with empty uuid", func(t *testing.T) {
		// Empty uuid should be rejected
		_, err := helpers.SKUQuerySKU(ctx, chain, "")
		require.Error(t, err, "query SKU with empty uuid should fail")
		t.Log("Correctly rejected query SKU with empty uuid")
	})

	t.Run("fail: query SKUs by provider with empty uuid", func(t *testing.T) {
		// Empty provider_uuid should be rejected or return empty
		res, err := helpers.SKUQuerySKUsByProvider(ctx, chain, "")
		if err == nil {
			require.Empty(t, res.Skus, "query SKUs by empty provider should return empty")
		}
		t.Log("Handled query SKUs by empty provider uuid")
	})

	// Cleanup
	_, _ = helpers.SKUDeactivateProvider(ctx, chain, authority, providerUUID)
}
