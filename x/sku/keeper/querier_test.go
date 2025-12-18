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

func TestQuerierProvider(t *testing.T) {
	f := initFixture(t)
	k := f.App.SKUKeeper
	q := keeper.NewQuerier(k)

	providerUUID := "01912345-6789-7abc-8def-0123456789ab"

	// Test not found
	_, err := q.Provider(f.Ctx, &types.QueryProviderRequest{Uuid: "01912345-6789-7abc-8def-999999999999"})
	require.Error(t, err)

	// Create a provider
	provider := types.Provider{
		Uuid:          providerUUID,
		Address:       f.TestAccs[0].String(),
		PayoutAddress: f.TestAccs[1].String(),
		MetaHash:      []byte("hash"),
		Active:        true,
	}
	err = k.SetProvider(f.Ctx, provider)
	require.NoError(t, err)

	// Query the provider
	res, err := q.Provider(f.Ctx, &types.QueryProviderRequest{Uuid: providerUUID})
	require.NoError(t, err)
	require.Equal(t, provider.Address, res.Provider.Address)
	require.Equal(t, provider.PayoutAddress, res.Provider.PayoutAddress)
	require.True(t, res.Provider.Active)

	// Test nil request
	_, err = q.Provider(f.Ctx, nil)
	require.Error(t, err)
}

func TestQuerierProviders(t *testing.T) {
	f := initFixture(t)
	k := f.App.SKUKeeper
	q := keeper.NewQuerier(k)

	// Create providers
	providers := []types.Provider{
		{Uuid: "01912345-6789-7abc-8def-0123456789a1", Address: f.TestAccs[0].String(), PayoutAddress: f.TestAccs[1].String(), Active: true},
		{Uuid: "01912345-6789-7abc-8def-0123456789a2", Address: f.TestAccs[2].String(), PayoutAddress: f.TestAccs[3].String(), Active: false},
		{Uuid: "01912345-6789-7abc-8def-0123456789a3", Address: f.TestAccs[4].String(), PayoutAddress: f.TestAccs[0].String(), Active: true},
	}

	for _, provider := range providers {
		err := k.SetProvider(f.Ctx, provider)
		require.NoError(t, err)
	}

	// Query all providers
	res, err := q.Providers(f.Ctx, &types.QueryProvidersRequest{})
	require.NoError(t, err)
	require.Len(t, res.Providers, 3)

	// Query active only
	res, err = q.Providers(f.Ctx, &types.QueryProvidersRequest{ActiveOnly: true})
	require.NoError(t, err)
	require.Len(t, res.Providers, 2)

	// Verify all returned providers are active
	for _, provider := range res.Providers {
		require.True(t, provider.Active, "expected only active providers")
	}

	// Test nil request
	_, err = q.Providers(f.Ctx, nil)
	require.Error(t, err)
}

func TestQuerierProvidersPagination(t *testing.T) {
	f := initFixture(t)
	k := f.App.SKUKeeper
	q := keeper.NewQuerier(k)

	// Create 5 providers
	for i := 1; i <= 5; i++ {
		provider := types.Provider{
			Uuid:          "01912345-6789-7abc-8def-0123456789a" + string(rune('0'+i)),
			Address:       f.TestAccs[i%5].String(),
			PayoutAddress: f.TestAccs[(i+1)%5].String(),
			Active:        true,
		}
		err := k.SetProvider(f.Ctx, provider)
		require.NoError(t, err)
	}

	// Query first page
	res1, err := q.Providers(f.Ctx, &types.QueryProvidersRequest{
		Pagination: &query.PageRequest{Limit: 2},
	})
	require.NoError(t, err)
	require.Len(t, res1.Providers, 2)
	require.NotNil(t, res1.Pagination)
	require.NotEmpty(t, res1.Pagination.NextKey)

	// Query second page using next key
	res2, err := q.Providers(f.Ctx, &types.QueryProvidersRequest{
		Pagination: &query.PageRequest{Key: res1.Pagination.NextKey, Limit: 2},
	})
	require.NoError(t, err)
	require.Len(t, res2.Providers, 2)

	// Query third page
	res3, err := q.Providers(f.Ctx, &types.QueryProvidersRequest{
		Pagination: &query.PageRequest{Key: res2.Pagination.NextKey, Limit: 2},
	})
	require.NoError(t, err)
	require.Len(t, res3.Providers, 1)
}

func TestQuerierSKU(t *testing.T) {
	f := initFixture(t)
	k := f.App.SKUKeeper
	q := keeper.NewQuerier(k)

	basePrice := sdk.NewCoin("umfx", sdkmath.NewInt(100))

	providerUUID := "01912345-6789-7abc-8def-0123456789a1"
	skuUUID := "01912345-6789-7abc-8def-0123456789b1"

	// Test not found
	_, err := q.SKU(f.Ctx, &types.QuerySKURequest{Uuid: "01912345-6789-7abc-8def-999999999999"})
	require.Error(t, err)

	// Create a provider first
	provider := types.Provider{
		Uuid:          providerUUID,
		Address:       f.TestAccs[0].String(),
		PayoutAddress: f.TestAccs[1].String(),
		Active:        true,
	}
	err = k.SetProvider(f.Ctx, provider)
	require.NoError(t, err)

	// Create a SKU
	sku := types.SKU{
		Uuid:         skuUUID,
		ProviderUuid: providerUUID,
		Name:         "Test SKU",
		Unit:         types.Unit_UNIT_PER_HOUR,
		BasePrice:    basePrice,
		MetaHash:     []byte("hash"),
		Active:       true,
	}
	err = k.SetSKU(f.Ctx, sku)
	require.NoError(t, err)

	// Query the SKU
	res, err := q.SKU(f.Ctx, &types.QuerySKURequest{Uuid: skuUUID})
	require.NoError(t, err)
	require.Equal(t, sku.Name, res.Sku.Name)
	require.Equal(t, sku.ProviderUuid, res.Sku.ProviderUuid)
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

	providerUUID := "01912345-6789-7abc-8def-0123456789a1"

	// Create provider first
	provider := types.Provider{
		Uuid:          providerUUID,
		Address:       f.TestAccs[0].String(),
		PayoutAddress: f.TestAccs[1].String(),
		Active:        true,
	}
	err := k.SetProvider(f.Ctx, provider)
	require.NoError(t, err)

	// Create 5 SKUs
	for i := 1; i <= 5; i++ {
		skuUUID := "01912345-6789-7abc-8def-0123456789b" + string(rune('0'+i))
		sku := types.SKU{
			Uuid:         skuUUID,
			ProviderUuid: providerUUID,
			Name:         "SKU",
			Unit:         types.Unit_UNIT_PER_HOUR,
			BasePrice:    basePrice,
			Active:       true,
		}
		err := k.SetSKU(f.Ctx, sku)
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

	t.Logf("First page SKU UUIDs: %s, %s", res1.Skus[0].Uuid, res1.Skus[1].Uuid)
	t.Logf("NextKey: %x", res1.Pagination.NextKey)

	// Query second page using next key
	res2, err := q.SKUs(f.Ctx, &types.QuerySKUsRequest{
		Pagination: &query.PageRequest{Key: res1.Pagination.NextKey, Limit: 2},
	})
	require.NoError(t, err)
	require.Len(t, res2.Skus, 2, "second page should have 2 SKUs")

	t.Logf("Second page SKU UUIDs: %s, %s", res2.Skus[0].Uuid, res2.Skus[1].Uuid)

	// Query third page
	res3, err := q.SKUs(f.Ctx, &types.QuerySKUsRequest{
		Pagination: &query.PageRequest{Key: res2.Pagination.NextKey, Limit: 2},
	})
	require.NoError(t, err)
	require.Len(t, res3.Skus, 1, "third page should have 1 SKU")

	t.Logf("Third page SKU UUIDs: %s", res3.Skus[0].Uuid)

	// Test nil request
	_, err = q.SKUs(f.Ctx, nil)
	require.Error(t, err)
}

func TestQuerierSKUsActiveOnly(t *testing.T) {
	f := initFixture(t)
	k := f.App.SKUKeeper
	q := keeper.NewQuerier(k)

	basePrice := sdk.NewCoin("umfx", sdkmath.NewInt(100))

	providerUUID := "01912345-6789-7abc-8def-0123456789a1"

	// Create provider first
	provider := types.Provider{
		Uuid:          providerUUID,
		Address:       f.TestAccs[0].String(),
		PayoutAddress: f.TestAccs[1].String(),
		Active:        true,
	}
	err := k.SetProvider(f.Ctx, provider)
	require.NoError(t, err)

	// Create mix of active and inactive SKUs
	skus := []types.SKU{
		{Uuid: "01912345-6789-7abc-8def-0123456789b1", ProviderUuid: providerUUID, Name: "SKU 1", Unit: types.Unit_UNIT_PER_HOUR, BasePrice: basePrice, Active: true},
		{Uuid: "01912345-6789-7abc-8def-0123456789b2", ProviderUuid: providerUUID, Name: "SKU 2", Unit: types.Unit_UNIT_PER_HOUR, BasePrice: basePrice, Active: false},
		{Uuid: "01912345-6789-7abc-8def-0123456789b3", ProviderUuid: providerUUID, Name: "SKU 3", Unit: types.Unit_UNIT_PER_HOUR, BasePrice: basePrice, Active: true},
		{Uuid: "01912345-6789-7abc-8def-0123456789b4", ProviderUuid: providerUUID, Name: "SKU 4", Unit: types.Unit_UNIT_PER_HOUR, BasePrice: basePrice, Active: false},
		{Uuid: "01912345-6789-7abc-8def-0123456789b5", ProviderUuid: providerUUID, Name: "SKU 5", Unit: types.Unit_UNIT_PER_HOUR, BasePrice: basePrice, Active: true},
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

	provider1UUID := "01912345-6789-7abc-8def-0123456789a1"
	provider2UUID := "01912345-6789-7abc-8def-0123456789a2"

	// Create providers
	providers := []types.Provider{
		{Uuid: provider1UUID, Address: f.TestAccs[0].String(), PayoutAddress: f.TestAccs[1].String(), Active: true},
		{Uuid: provider2UUID, Address: f.TestAccs[2].String(), PayoutAddress: f.TestAccs[3].String(), Active: true},
	}
	for _, provider := range providers {
		err := k.SetProvider(f.Ctx, provider)
		require.NoError(t, err)
	}

	// Create SKUs for different providers
	skus := []types.SKU{
		{Uuid: "01912345-6789-7abc-8def-0123456789b1", ProviderUuid: provider1UUID, Name: "P1 SKU 1", Unit: types.Unit_UNIT_PER_HOUR, BasePrice: basePrice, Active: true},
		{Uuid: "01912345-6789-7abc-8def-0123456789b2", ProviderUuid: provider1UUID, Name: "P1 SKU 2", Unit: types.Unit_UNIT_PER_HOUR, BasePrice: basePrice, Active: false},
		{Uuid: "01912345-6789-7abc-8def-0123456789b3", ProviderUuid: provider2UUID, Name: "P2 SKU 1", Unit: types.Unit_UNIT_PER_DAY, BasePrice: basePrice, Active: true},
		{Uuid: "01912345-6789-7abc-8def-0123456789b4", ProviderUuid: provider1UUID, Name: "P1 SKU 3", Unit: types.Unit_UNIT_PER_HOUR, BasePrice: basePrice, Active: true},
	}

	for _, sku := range skus {
		err := k.SetSKU(f.Ctx, sku)
		require.NoError(t, err)
	}

	// Query provider 1 (all)
	res, err := q.SKUsByProvider(f.Ctx, &types.QuerySKUsByProviderRequest{ProviderUuid: provider1UUID})
	require.NoError(t, err)
	require.Len(t, res.Skus, 3)

	// Query provider 1 (active only)
	res, err = q.SKUsByProvider(f.Ctx, &types.QuerySKUsByProviderRequest{ProviderUuid: provider1UUID, ActiveOnly: true})
	require.NoError(t, err)
	require.Len(t, res.Skus, 2)
	for _, sku := range res.Skus {
		require.True(t, sku.Active)
		require.Equal(t, provider1UUID, sku.ProviderUuid)
	}

	// Query provider 2
	res, err = q.SKUsByProvider(f.Ctx, &types.QuerySKUsByProviderRequest{ProviderUuid: provider2UUID})
	require.NoError(t, err)
	require.Len(t, res.Skus, 1)

	// Query non-existent provider
	res, err = q.SKUsByProvider(f.Ctx, &types.QuerySKUsByProviderRequest{ProviderUuid: "01912345-6789-7abc-8def-999999999999"})
	require.NoError(t, err)
	require.Len(t, res.Skus, 0)

	// Test empty provider_uuid
	_, err = q.SKUsByProvider(f.Ctx, &types.QuerySKUsByProviderRequest{ProviderUuid: ""})
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

	providerUUID := "01912345-6789-7abc-8def-0123456789a1"

	// Create provider
	provider := types.Provider{
		Uuid:          providerUUID,
		Address:       f.TestAccs[0].String(),
		PayoutAddress: f.TestAccs[1].String(),
		Active:        true,
	}
	err := k.SetProvider(f.Ctx, provider)
	require.NoError(t, err)

	// Create 5 SKUs for the same provider
	for i := 1; i <= 5; i++ {
		skuUUID := "01912345-6789-7abc-8def-0123456789b" + string(rune('0'+i))
		sku := types.SKU{
			Uuid:         skuUUID,
			ProviderUuid: providerUUID,
			Name:         "SKU",
			Unit:         types.Unit_UNIT_PER_HOUR,
			BasePrice:    basePrice,
			Active:       true,
		}
		err := k.SetSKU(f.Ctx, sku)
		require.NoError(t, err)
	}

	// Query first page
	res1, err := q.SKUsByProvider(f.Ctx, &types.QuerySKUsByProviderRequest{
		ProviderUuid: providerUUID,
		Pagination:   &query.PageRequest{Limit: 2},
	})
	require.NoError(t, err)
	require.Len(t, res1.Skus, 2)
	require.NotEmpty(t, res1.Pagination.NextKey)

	// Query second page
	res2, err := q.SKUsByProvider(f.Ctx, &types.QuerySKUsByProviderRequest{
		ProviderUuid: providerUUID,
		Pagination:   &query.PageRequest{Key: res1.Pagination.NextKey, Limit: 2},
	})
	require.NoError(t, err)
	require.Len(t, res2.Skus, 2)

	// Query third page
	res3, err := q.SKUsByProvider(f.Ctx, &types.QuerySKUsByProviderRequest{
		ProviderUuid: providerUUID,
		Pagination:   &query.PageRequest{Key: res2.Pagination.NextKey, Limit: 2},
	})
	require.NoError(t, err)
	require.Len(t, res3.Skus, 1)
}
