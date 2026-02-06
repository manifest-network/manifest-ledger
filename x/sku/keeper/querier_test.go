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
		{Uuid: testProvider1UUID, Address: f.TestAccs[0].String(), PayoutAddress: f.TestAccs[1].String(), Active: true},
		{Uuid: testProvider2UUID, Address: f.TestAccs[2].String(), PayoutAddress: f.TestAccs[3].String(), Active: false},
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

	skuUUID := "01912345-6789-7abc-8def-0123456789b1"

	// Test not found
	_, err := q.SKU(f.Ctx, &types.QuerySKURequest{Uuid: "01912345-6789-7abc-8def-999999999999"})
	require.Error(t, err)

	// Create a provider first
	provider := types.Provider{
		Uuid:          testProvider1UUID,
		Address:       f.TestAccs[0].String(),
		PayoutAddress: f.TestAccs[1].String(),
		Active:        true,
	}
	err = k.SetProvider(f.Ctx, provider)
	require.NoError(t, err)

	// Create a SKU
	sku := types.SKU{
		Uuid:         skuUUID,
		ProviderUuid: testProvider1UUID,
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

	// Create provider first
	provider := types.Provider{
		Uuid:          testProvider1UUID,
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
			ProviderUuid: testProvider1UUID,
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

	// Create provider first
	provider := types.Provider{
		Uuid:          testProvider1UUID,
		Address:       f.TestAccs[0].String(),
		PayoutAddress: f.TestAccs[1].String(),
		Active:        true,
	}
	err := k.SetProvider(f.Ctx, provider)
	require.NoError(t, err)

	// Create mix of active and inactive SKUs
	skus := []types.SKU{
		{Uuid: "01912345-6789-7abc-8def-0123456789b1", ProviderUuid: testProvider1UUID, Name: "SKU 1", Unit: types.Unit_UNIT_PER_HOUR, BasePrice: basePrice, Active: true},
		{Uuid: "01912345-6789-7abc-8def-0123456789b2", ProviderUuid: testProvider1UUID, Name: "SKU 2", Unit: types.Unit_UNIT_PER_HOUR, BasePrice: basePrice, Active: false},
		{Uuid: "01912345-6789-7abc-8def-0123456789b3", ProviderUuid: testProvider1UUID, Name: "SKU 3", Unit: types.Unit_UNIT_PER_HOUR, BasePrice: basePrice, Active: true},
		{Uuid: "01912345-6789-7abc-8def-0123456789b4", ProviderUuid: testProvider1UUID, Name: "SKU 4", Unit: types.Unit_UNIT_PER_HOUR, BasePrice: basePrice, Active: false},
		{Uuid: "01912345-6789-7abc-8def-0123456789b5", ProviderUuid: testProvider1UUID, Name: "SKU 5", Unit: types.Unit_UNIT_PER_HOUR, BasePrice: basePrice, Active: true},
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

	// Create providers
	providers := []types.Provider{
		{Uuid: testProvider1UUID, Address: f.TestAccs[0].String(), PayoutAddress: f.TestAccs[1].String(), Active: true},
		{Uuid: testProvider2UUID, Address: f.TestAccs[2].String(), PayoutAddress: f.TestAccs[3].String(), Active: true},
	}
	for _, provider := range providers {
		err := k.SetProvider(f.Ctx, provider)
		require.NoError(t, err)
	}

	// Create SKUs for different providers
	skus := []types.SKU{
		{Uuid: "01912345-6789-7abc-8def-0123456789b1", ProviderUuid: testProvider1UUID, Name: "P1 SKU 1", Unit: types.Unit_UNIT_PER_HOUR, BasePrice: basePrice, Active: true},
		{Uuid: "01912345-6789-7abc-8def-0123456789b2", ProviderUuid: testProvider1UUID, Name: "P1 SKU 2", Unit: types.Unit_UNIT_PER_HOUR, BasePrice: basePrice, Active: false},
		{Uuid: "01912345-6789-7abc-8def-0123456789b3", ProviderUuid: testProvider2UUID, Name: "P2 SKU 1", Unit: types.Unit_UNIT_PER_DAY, BasePrice: basePrice, Active: true},
		{Uuid: "01912345-6789-7abc-8def-0123456789b4", ProviderUuid: testProvider1UUID, Name: "P1 SKU 3", Unit: types.Unit_UNIT_PER_HOUR, BasePrice: basePrice, Active: true},
	}

	for _, sku := range skus {
		err := k.SetSKU(f.Ctx, sku)
		require.NoError(t, err)
	}

	// Query provider 1 (all)
	res, err := q.SKUsByProvider(f.Ctx, &types.QuerySKUsByProviderRequest{ProviderUuid: testProvider1UUID})
	require.NoError(t, err)
	require.Len(t, res.Skus, 3)

	// Query provider 1 (active only)
	res, err = q.SKUsByProvider(f.Ctx, &types.QuerySKUsByProviderRequest{ProviderUuid: testProvider1UUID, ActiveOnly: true})
	require.NoError(t, err)
	require.Len(t, res.Skus, 2)
	for _, sku := range res.Skus {
		require.True(t, sku.Active)
		require.Equal(t, testProvider1UUID, sku.ProviderUuid)
	}

	// Query provider 2
	res, err = q.SKUsByProvider(f.Ctx, &types.QuerySKUsByProviderRequest{ProviderUuid: testProvider2UUID})
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

	// Create provider
	provider := types.Provider{
		Uuid:          testProvider1UUID,
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
			ProviderUuid: testProvider1UUID,
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
		ProviderUuid: testProvider1UUID,
		Pagination:   &query.PageRequest{Limit: 2},
	})
	require.NoError(t, err)
	require.Len(t, res1.Skus, 2)
	require.NotEmpty(t, res1.Pagination.NextKey)

	// Query second page
	res2, err := q.SKUsByProvider(f.Ctx, &types.QuerySKUsByProviderRequest{
		ProviderUuid: testProvider1UUID,
		Pagination:   &query.PageRequest{Key: res1.Pagination.NextKey, Limit: 2},
	})
	require.NoError(t, err)
	require.Len(t, res2.Skus, 2)

	// Query third page
	res3, err := q.SKUsByProvider(f.Ctx, &types.QuerySKUsByProviderRequest{
		ProviderUuid: testProvider1UUID,
		Pagination:   &query.PageRequest{Key: res2.Pagination.NextKey, Limit: 2},
	})
	require.NoError(t, err)
	require.Len(t, res3.Skus, 1)
}

func TestQuerierProviderByAddress(t *testing.T) {
	f := initFixture(t)
	k := f.App.SKUKeeper
	q := keeper.NewQuerier(k)

	addr1 := f.TestAccs[0]
	addr2 := f.TestAccs[1]
	payoutAddr := f.TestAccs[2]

	t.Run("empty result when no providers exist", func(t *testing.T) {
		resp, err := q.ProviderByAddress(f.Ctx, &types.QueryProviderByAddressRequest{
			Address: addr1.String(),
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Empty(t, resp.Providers)
	})

	t.Run("returns providers for address", func(t *testing.T) {
		// Create two providers with the same address (different UUIDs)
		provider1 := types.Provider{
			Uuid:          testProvider1UUID,
			Address:       addr1.String(),
			PayoutAddress: payoutAddr.String(),
			Active:        true,
		}
		provider2 := types.Provider{
			Uuid:          testProvider2UUID,
			Address:       addr1.String(),
			PayoutAddress: payoutAddr.String(),
			Active:        false,
		}
		// Provider with different address
		provider3 := types.Provider{
			Uuid:          "01912345-6789-7abc-8def-0123456789a3",
			Address:       addr2.String(),
			PayoutAddress: payoutAddr.String(),
			Active:        true,
		}

		require.NoError(t, k.SetProvider(f.Ctx, provider1))
		require.NoError(t, k.SetProvider(f.Ctx, provider2))
		require.NoError(t, k.SetProvider(f.Ctx, provider3))

		// Query for addr1 - should return 2 providers
		resp, err := q.ProviderByAddress(f.Ctx, &types.QueryProviderByAddressRequest{
			Address: addr1.String(),
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Len(t, resp.Providers, 2)
		for _, p := range resp.Providers {
			require.Equal(t, addr1.String(), p.Address)
		}

		// Query for addr2 - should return 1 provider
		resp, err = q.ProviderByAddress(f.Ctx, &types.QueryProviderByAddressRequest{
			Address: addr2.String(),
		})
		require.NoError(t, err)
		require.Len(t, resp.Providers, 1)
		require.Equal(t, addr2.String(), resp.Providers[0].Address)
	})

	t.Run("pagination works", func(t *testing.T) {
		// Query addr1 with pagination limit
		resp, err := q.ProviderByAddress(f.Ctx, &types.QueryProviderByAddressRequest{
			Address:    addr1.String(),
			Pagination: &query.PageRequest{Limit: 1},
		})
		require.NoError(t, err)
		require.Len(t, resp.Providers, 1)
		require.NotNil(t, resp.Pagination)
		require.NotEmpty(t, resp.Pagination.NextKey)

		// Query next page
		resp2, err := q.ProviderByAddress(f.Ctx, &types.QueryProviderByAddressRequest{
			Address:    addr1.String(),
			Pagination: &query.PageRequest{Key: resp.Pagination.NextKey, Limit: 1},
		})
		require.NoError(t, err)
		require.Len(t, resp2.Providers, 1)
	})

	t.Run("error cases", func(t *testing.T) {
		// Nil request
		_, err := q.ProviderByAddress(f.Ctx, nil)
		require.Error(t, err)

		// Empty address
		_, err = q.ProviderByAddress(f.Ctx, &types.QueryProviderByAddressRequest{
			Address: "",
		})
		require.Error(t, err)

		// Invalid address
		_, err = q.ProviderByAddress(f.Ctx, &types.QueryProviderByAddressRequest{
			Address: "invalid-address",
		})
		require.Error(t, err)
	})
}

func TestQuerierProvidersReverse(t *testing.T) {
	f := initFixture(t)
	k := f.App.SKUKeeper
	q := keeper.NewQuerier(k)

	// Create 3 active providers
	for i := 1; i <= 3; i++ {
		provider := types.Provider{
			Uuid:          "01912345-6789-7abc-8def-0123456789a" + string(rune('0'+i)),
			Address:       f.TestAccs[i%5].String(),
			PayoutAddress: f.TestAccs[(i+1)%5].String(),
			Active:        true,
		}
		err := k.SetProvider(f.Ctx, provider)
		require.NoError(t, err)
	}

	respFwd, err := q.Providers(f.Ctx, &types.QueryProvidersRequest{ActiveOnly: true})
	require.NoError(t, err)
	require.Len(t, respFwd.Providers, 3)

	respRev, err := q.Providers(f.Ctx, &types.QueryProvidersRequest{
		ActiveOnly: true,
		Pagination: &query.PageRequest{Reverse: true},
	})
	require.NoError(t, err)
	require.Len(t, respRev.Providers, 3)

	for i := range respFwd.Providers {
		require.Equal(t, respFwd.Providers[i].Uuid, respRev.Providers[len(respRev.Providers)-1-i].Uuid)
	}
}

func TestQuerierSKUsReverse(t *testing.T) {
	f := initFixture(t)
	k := f.App.SKUKeeper
	q := keeper.NewQuerier(k)

	basePrice := sdk.NewCoin("umfx", sdkmath.NewInt(100))

	// Create provider
	provider := types.Provider{
		Uuid:          testProvider1UUID,
		Address:       f.TestAccs[0].String(),
		PayoutAddress: f.TestAccs[1].String(),
		Active:        true,
	}
	err := k.SetProvider(f.Ctx, provider)
	require.NoError(t, err)

	// Create 3 active SKUs
	for i := 1; i <= 3; i++ {
		skuUUID := "01912345-6789-7abc-8def-0123456789b" + string(rune('0'+i))
		sku := types.SKU{
			Uuid:         skuUUID,
			ProviderUuid: testProvider1UUID,
			Name:         "SKU",
			Unit:         types.Unit_UNIT_PER_HOUR,
			BasePrice:    basePrice,
			Active:       true,
		}
		err := k.SetSKU(f.Ctx, sku)
		require.NoError(t, err)
	}

	respFwd, err := q.SKUs(f.Ctx, &types.QuerySKUsRequest{ActiveOnly: true})
	require.NoError(t, err)
	require.Len(t, respFwd.Skus, 3)

	respRev, err := q.SKUs(f.Ctx, &types.QuerySKUsRequest{
		ActiveOnly: true,
		Pagination: &query.PageRequest{Reverse: true},
	})
	require.NoError(t, err)
	require.Len(t, respRev.Skus, 3)

	for i := range respFwd.Skus {
		require.Equal(t, respFwd.Skus[i].Uuid, respRev.Skus[len(respRev.Skus)-1-i].Uuid)
	}
}

func TestQuerierSKUsByProviderReverse(t *testing.T) {
	f := initFixture(t)
	k := f.App.SKUKeeper
	q := keeper.NewQuerier(k)

	basePrice := sdk.NewCoin("umfx", sdkmath.NewInt(100))

	// Create provider
	provider := types.Provider{
		Uuid:          testProvider1UUID,
		Address:       f.TestAccs[0].String(),
		PayoutAddress: f.TestAccs[1].String(),
		Active:        true,
	}
	err := k.SetProvider(f.Ctx, provider)
	require.NoError(t, err)

	// Create 3 active SKUs + 1 inactive
	skus := []types.SKU{
		{Uuid: "01912345-6789-7abc-8def-0123456789b1", ProviderUuid: testProvider1UUID, Name: "SKU 1", Unit: types.Unit_UNIT_PER_HOUR, BasePrice: basePrice, Active: true},
		{Uuid: "01912345-6789-7abc-8def-0123456789b2", ProviderUuid: testProvider1UUID, Name: "SKU 2", Unit: types.Unit_UNIT_PER_HOUR, BasePrice: basePrice, Active: false},
		{Uuid: "01912345-6789-7abc-8def-0123456789b3", ProviderUuid: testProvider1UUID, Name: "SKU 3", Unit: types.Unit_UNIT_PER_HOUR, BasePrice: basePrice, Active: true},
		{Uuid: "01912345-6789-7abc-8def-0123456789b4", ProviderUuid: testProvider1UUID, Name: "SKU 4", Unit: types.Unit_UNIT_PER_HOUR, BasePrice: basePrice, Active: true},
	}
	for _, sku := range skus {
		err := k.SetSKU(f.Ctx, sku)
		require.NoError(t, err)
	}

	t.Run("reverse active only", func(t *testing.T) {
		respFwd, err := q.SKUsByProvider(f.Ctx, &types.QuerySKUsByProviderRequest{
			ProviderUuid: testProvider1UUID,
			ActiveOnly:   true,
		})
		require.NoError(t, err)
		require.Len(t, respFwd.Skus, 3)

		respRev, err := q.SKUsByProvider(f.Ctx, &types.QuerySKUsByProviderRequest{
			ProviderUuid: testProvider1UUID,
			ActiveOnly:   true,
			Pagination:   &query.PageRequest{Reverse: true},
		})
		require.NoError(t, err)
		require.Len(t, respRev.Skus, 3)

		for i := range respFwd.Skus {
			require.Equal(t, respFwd.Skus[i].Uuid, respRev.Skus[len(respRev.Skus)-1-i].Uuid)
		}
	})

	t.Run("reverse all", func(t *testing.T) {
		respFwd, err := q.SKUsByProvider(f.Ctx, &types.QuerySKUsByProviderRequest{
			ProviderUuid: testProvider1UUID,
		})
		require.NoError(t, err)
		require.Len(t, respFwd.Skus, 4)

		respRev, err := q.SKUsByProvider(f.Ctx, &types.QuerySKUsByProviderRequest{
			ProviderUuid: testProvider1UUID,
			Pagination:   &query.PageRequest{Reverse: true},
		})
		require.NoError(t, err)
		require.Len(t, respRev.Skus, 4)

		for i := range respFwd.Skus {
			require.Equal(t, respFwd.Skus[i].Uuid, respRev.Skus[len(respRev.Skus)-1-i].Uuid)
		}
	})

	t.Run("reverse all with limit and cursor", func(t *testing.T) {
		// 4 SKUs total, page through in reverse with limit=2
		page1, err := q.SKUsByProvider(f.Ctx, &types.QuerySKUsByProviderRequest{
			ProviderUuid: testProvider1UUID,
			Pagination:   &query.PageRequest{Reverse: true, Limit: 2},
		})
		require.NoError(t, err)
		require.Len(t, page1.Skus, 2)
		require.NotEmpty(t, page1.Pagination.NextKey)

		page2, err := q.SKUsByProvider(f.Ctx, &types.QuerySKUsByProviderRequest{
			ProviderUuid: testProvider1UUID,
			Pagination:   &query.PageRequest{Reverse: true, Key: page1.Pagination.NextKey, Limit: 2},
		})
		require.NoError(t, err)
		require.Len(t, page2.Skus, 2)
		require.Empty(t, page2.Pagination.NextKey, "last page should have no next cursor")

		// Combine pages and verify they match the full reverse set
		respFwd, err := q.SKUsByProvider(f.Ctx, &types.QuerySKUsByProviderRequest{
			ProviderUuid: testProvider1UUID,
		})
		require.NoError(t, err)

		allRev := append(page1.Skus, page2.Skus...)
		require.Len(t, allRev, len(respFwd.Skus))
		for i := range respFwd.Skus {
			require.Equal(t, respFwd.Skus[i].Uuid, allRev[len(allRev)-1-i].Uuid)
		}
	})
}

func TestQuerierProviderByAddressReverse(t *testing.T) {
	f := initFixture(t)
	k := f.App.SKUKeeper
	q := keeper.NewQuerier(k)

	addr := f.TestAccs[0]
	payoutAddr := f.TestAccs[2]

	// Create 3 providers with the same address
	for i := 1; i <= 3; i++ {
		provider := types.Provider{
			Uuid:          "01912345-6789-7abc-8def-0123456789a" + string(rune('0'+i)),
			Address:       addr.String(),
			PayoutAddress: payoutAddr.String(),
			Active:        true,
		}
		require.NoError(t, k.SetProvider(f.Ctx, provider))
	}

	respFwd, err := q.ProviderByAddress(f.Ctx, &types.QueryProviderByAddressRequest{
		Address: addr.String(),
	})
	require.NoError(t, err)
	require.Len(t, respFwd.Providers, 3)

	respRev, err := q.ProviderByAddress(f.Ctx, &types.QueryProviderByAddressRequest{
		Address:    addr.String(),
		Pagination: &query.PageRequest{Reverse: true},
	})
	require.NoError(t, err)
	require.Len(t, respRev.Providers, 3)

	for i := range respFwd.Providers {
		require.Equal(t, respFwd.Providers[i].Uuid, respRev.Providers[len(respRev.Providers)-1-i].Uuid)
	}
}

// TestQueryErrorCasesComprehensive tests additional error cases across SKU queries
// including invalid UUID formats and non-existent resources.
func TestQueryErrorCasesComprehensive(t *testing.T) {
	f := initFixture(t)
	k := f.App.SKUKeeper
	q := keeper.NewQuerier(k)

	t.Run("Provider query with invalid UUID format", func(t *testing.T) {
		// Not a valid UUIDv7 format
		_, err := q.Provider(f.Ctx, &types.QueryProviderRequest{
			Uuid: "not-a-valid-uuid",
		})
		require.Error(t, err)

		// Too short
		_, err = q.Provider(f.Ctx, &types.QueryProviderRequest{
			Uuid: "12345",
		})
		require.Error(t, err)

		// Empty UUID
		_, err = q.Provider(f.Ctx, &types.QueryProviderRequest{
			Uuid: "",
		})
		require.Error(t, err)
	})

	t.Run("SKU query with invalid UUID format", func(t *testing.T) {
		_, err := q.SKU(f.Ctx, &types.QuerySKURequest{
			Uuid: "invalid-uuid-format",
		})
		require.Error(t, err)

		_, err = q.SKU(f.Ctx, &types.QuerySKURequest{
			Uuid: "",
		})
		require.Error(t, err)
	})

	t.Run("SKUsByProvider with empty provider_uuid", func(t *testing.T) {
		// Empty provider_uuid should error
		_, err := q.SKUsByProvider(f.Ctx, &types.QuerySKUsByProviderRequest{
			ProviderUuid: "",
		})
		require.Error(t, err)

		// Invalid UUID format returns empty results (no validation on format)
		resp, err := q.SKUsByProvider(f.Ctx, &types.QuerySKUsByProviderRequest{
			ProviderUuid: "bad-uuid",
		})
		require.NoError(t, err)
		require.Empty(t, resp.Skus, "invalid UUID should return empty results")
	})

	t.Run("non-existent resources return appropriate errors", func(t *testing.T) {
		// Non-existent provider (valid UUID format but doesn't exist)
		nonExistentUUID := "01912345-6789-7abc-8def-999999999999"
		_, err := q.Provider(f.Ctx, &types.QueryProviderRequest{
			Uuid: nonExistentUUID,
		})
		require.Error(t, err, "should error for non-existent provider")

		// Non-existent SKU
		_, err = q.SKU(f.Ctx, &types.QuerySKURequest{
			Uuid: nonExistentUUID,
		})
		require.Error(t, err, "should error for non-existent SKU")
	})

	t.Run("queries with valid but empty results", func(t *testing.T) {
		// Provider with no SKUs (valid UUID format)
		validProviderUUID := "01912345-6789-7abc-8def-000000000001"
		resp, err := q.SKUsByProvider(f.Ctx, &types.QuerySKUsByProviderRequest{
			ProviderUuid: validProviderUUID,
		})
		require.NoError(t, err, "should not error for provider with no SKUs")
		require.Empty(t, resp.Skus, "should return empty list")

		// Address with no providers
		validAddr := f.TestAccs[4].String()
		resp2, err := q.ProviderByAddress(f.Ctx, &types.QueryProviderByAddressRequest{
			Address: validAddr,
		})
		require.NoError(t, err, "should not error for address with no providers")
		require.Empty(t, resp2.Providers, "should return empty list")
	})
}
