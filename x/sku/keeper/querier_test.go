package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"

	"github.com/manifest-network/manifest-ledger/x/sku/keeper"
	"github.com/manifest-network/manifest-ledger/x/sku/types"
)

func TestQuerierParams(t *testing.T) {
	f := initFixture(t)
	k := f.App.SKUKeeper
	q := keeper.NewQuerier(k)

	// Test with default params
	res, err := q.Params(f.Ctx, &types.QueryParamsRequest{})
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Empty(t, res.Params.AllowedList)

	// Set params with allowed list
	params := types.Params{AllowedList: []string{f.TestAccs[0].String()}}
	err = k.SetParams(f.Ctx, params)
	require.NoError(t, err)

	// Query again
	res, err = q.Params(f.Ctx, &types.QueryParamsRequest{})
	require.NoError(t, err)
	require.Len(t, res.Params.AllowedList, 1)
	require.Equal(t, f.TestAccs[0].String(), res.Params.AllowedList[0])

	// Test nil request
	_, err = q.Params(f.Ctx, nil)
	require.Error(t, err)
}

func TestQuerierSKU(t *testing.T) {
	f := initFixture(t)
	k := f.App.SKUKeeper
	q := keeper.NewQuerier(k)

	basePrice := sdk.NewCoin("umfx", sdkmath.NewInt(100))

	// Test not found
	_, err := q.SKU(f.Ctx, &types.QuerySKURequest{Id: 999})
	require.Error(t, err)

	// Create a SKU
	sku := types.SKU{
		Id:        1,
		Provider:  "provider1",
		Name:      "Test SKU",
		Unit:      types.Unit_UNIT_PER_HOUR,
		BasePrice: basePrice,
		MetaHash:  []byte("hash"),
		Active:    true,
	}
	err = k.SetSKU(f.Ctx, sku)
	require.NoError(t, err)

	// Query the SKU
	res, err := q.SKU(f.Ctx, &types.QuerySKURequest{Id: 1})
	require.NoError(t, err)
	require.Equal(t, sku.Name, res.Sku.Name)
	require.Equal(t, sku.Provider, res.Sku.Provider)
	require.Equal(t, sku.Unit, res.Sku.Unit)
	require.True(t, res.Sku.Active)

	// Test nil request
	_, err = q.SKU(f.Ctx, nil)
	require.Error(t, err)
}

func TestQuerierSKUsPagination(t *testing.T) {
	f := initFixture(t)
	k := f.App.SKUKeeper
	q := keeper.NewQuerier(k)

	basePrice := sdk.NewCoin("umfx", sdkmath.NewInt(100))

	// Create 5 SKUs
	for i := uint64(1); i <= 5; i++ {
		sku := types.SKU{
			Id:        i,
			Provider:  "test-provider",
			Name:      "SKU",
			Unit:      types.Unit_UNIT_PER_HOUR,
			BasePrice: basePrice,
			Active:    true,
		}
		err := k.SKUs.Set(f.Ctx, i, sku)
		require.NoError(t, err)
	}

	// Query first page
	res1, err := q.SKUs(f.Ctx, &types.QuerySKUsRequest{
		Pagination: &query.PageRequest{Limit: 2},
	})
	require.NoError(t, err)
	require.Len(t, res1.Skus, 2)
	require.NotNil(t, res1.Pagination)
	require.NotEmpty(t, res1.Pagination.NextKey)

	t.Logf("First page SKU IDs: %d, %d", res1.Skus[0].Id, res1.Skus[1].Id)
	t.Logf("NextKey: %x", res1.Pagination.NextKey)

	// Query second page using next key
	res2, err := q.SKUs(f.Ctx, &types.QuerySKUsRequest{
		Pagination: &query.PageRequest{Key: res1.Pagination.NextKey, Limit: 2},
	})
	require.NoError(t, err)
	require.Len(t, res2.Skus, 2, "second page should have 2 SKUs")

	t.Logf("Second page SKU IDs: %d, %d", res2.Skus[0].Id, res2.Skus[1].Id)

	// Query third page
	res3, err := q.SKUs(f.Ctx, &types.QuerySKUsRequest{
		Pagination: &query.PageRequest{Key: res2.Pagination.NextKey, Limit: 2},
	})
	require.NoError(t, err)
	require.Len(t, res3.Skus, 1, "third page should have 1 SKU")

	t.Logf("Third page SKU IDs: %d", res3.Skus[0].Id)

	// Test nil request
	_, err = q.SKUs(f.Ctx, nil)
	require.Error(t, err)
}

func TestQuerierSKUsActiveOnly(t *testing.T) {
	f := initFixture(t)
	k := f.App.SKUKeeper
	q := keeper.NewQuerier(k)

	basePrice := sdk.NewCoin("umfx", sdkmath.NewInt(100))

	// Create mix of active and inactive SKUs
	skus := []types.SKU{
		{Id: 1, Provider: "p1", Name: "SKU 1", Unit: types.Unit_UNIT_PER_HOUR, BasePrice: basePrice, Active: true},
		{Id: 2, Provider: "p1", Name: "SKU 2", Unit: types.Unit_UNIT_PER_HOUR, BasePrice: basePrice, Active: false},
		{Id: 3, Provider: "p1", Name: "SKU 3", Unit: types.Unit_UNIT_PER_HOUR, BasePrice: basePrice, Active: true},
		{Id: 4, Provider: "p1", Name: "SKU 4", Unit: types.Unit_UNIT_PER_HOUR, BasePrice: basePrice, Active: false},
		{Id: 5, Provider: "p1", Name: "SKU 5", Unit: types.Unit_UNIT_PER_HOUR, BasePrice: basePrice, Active: true},
	}

	for _, sku := range skus {
		err := k.SetSKU(f.Ctx, sku)
		require.NoError(t, err)
	}

	// Query all SKUs (no filter)
	res, err := q.SKUs(f.Ctx, &types.QuerySKUsRequest{})
	require.NoError(t, err)
	require.Len(t, res.Skus, 5)

	// Query active only
	res, err = q.SKUs(f.Ctx, &types.QuerySKUsRequest{ActiveOnly: true})
	require.NoError(t, err)
	require.Len(t, res.Skus, 3)

	// Verify all returned SKUs are active
	for _, sku := range res.Skus {
		require.True(t, sku.Active, "expected only active SKUs")
	}
}

func TestQuerierSKUsByProvider(t *testing.T) {
	f := initFixture(t)
	k := f.App.SKUKeeper
	q := keeper.NewQuerier(k)

	basePrice := sdk.NewCoin("umfx", sdkmath.NewInt(100))

	// Create SKUs for different providers
	skus := []types.SKU{
		{Id: 1, Provider: "provider1", Name: "P1 SKU 1", Unit: types.Unit_UNIT_PER_HOUR, BasePrice: basePrice, Active: true},
		{Id: 2, Provider: "provider1", Name: "P1 SKU 2", Unit: types.Unit_UNIT_PER_HOUR, BasePrice: basePrice, Active: false},
		{Id: 3, Provider: "provider2", Name: "P2 SKU 1", Unit: types.Unit_UNIT_PER_DAY, BasePrice: basePrice, Active: true},
		{Id: 4, Provider: "provider1", Name: "P1 SKU 3", Unit: types.Unit_UNIT_PER_HOUR, BasePrice: basePrice, Active: true},
	}

	for _, sku := range skus {
		err := k.SetSKU(f.Ctx, sku)
		require.NoError(t, err)
	}

	// Query provider1 (all)
	res, err := q.SKUsByProvider(f.Ctx, &types.QuerySKUsByProviderRequest{Provider: "provider1"})
	require.NoError(t, err)
	require.Len(t, res.Skus, 3)

	// Query provider1 (active only)
	res, err = q.SKUsByProvider(f.Ctx, &types.QuerySKUsByProviderRequest{Provider: "provider1", ActiveOnly: true})
	require.NoError(t, err)
	require.Len(t, res.Skus, 2)
	for _, sku := range res.Skus {
		require.True(t, sku.Active)
		require.Equal(t, "provider1", sku.Provider)
	}

	// Query provider2
	res, err = q.SKUsByProvider(f.Ctx, &types.QuerySKUsByProviderRequest{Provider: "provider2"})
	require.NoError(t, err)
	require.Len(t, res.Skus, 1)

	// Query non-existent provider
	res, err = q.SKUsByProvider(f.Ctx, &types.QuerySKUsByProviderRequest{Provider: "provider3"})
	require.NoError(t, err)
	require.Len(t, res.Skus, 0)

	// Test empty provider
	_, err = q.SKUsByProvider(f.Ctx, &types.QuerySKUsByProviderRequest{Provider: ""})
	require.Error(t, err)

	// Test nil request
	_, err = q.SKUsByProvider(f.Ctx, nil)
	require.Error(t, err)
}

func TestQuerierSKUsByProviderPagination(t *testing.T) {
	f := initFixture(t)
	k := f.App.SKUKeeper
	q := keeper.NewQuerier(k)

	basePrice := sdk.NewCoin("umfx", sdkmath.NewInt(100))

	// Create 5 SKUs for the same provider
	for i := uint64(1); i <= 5; i++ {
		sku := types.SKU{
			Id:        i,
			Provider:  "test-provider",
			Name:      "SKU",
			Unit:      types.Unit_UNIT_PER_HOUR,
			BasePrice: basePrice,
			Active:    true,
		}
		err := k.SetSKU(f.Ctx, sku)
		require.NoError(t, err)
	}

	// Query first page
	res1, err := q.SKUsByProvider(f.Ctx, &types.QuerySKUsByProviderRequest{
		Provider:   "test-provider",
		Pagination: &query.PageRequest{Limit: 2},
	})
	require.NoError(t, err)
	require.Len(t, res1.Skus, 2)
	require.NotEmpty(t, res1.Pagination.NextKey)

	// Query second page
	res2, err := q.SKUsByProvider(f.Ctx, &types.QuerySKUsByProviderRequest{
		Provider:   "test-provider",
		Pagination: &query.PageRequest{Key: res1.Pagination.NextKey, Limit: 2},
	})
	require.NoError(t, err)
	require.Len(t, res2.Skus, 2)

	// Query third page
	res3, err := q.SKUsByProvider(f.Ctx, &types.QuerySKUsByProviderRequest{
		Provider:   "test-provider",
		Pagination: &query.PageRequest{Key: res2.Pagination.NextKey, Limit: 2},
	})
	require.NoError(t, err)
	require.Len(t, res3.Skus, 1)
}
