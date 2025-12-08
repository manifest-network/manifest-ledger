package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	sdkmath "cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/testutil/testdata"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"

	"github.com/manifest-network/manifest-ledger/app"
	"github.com/manifest-network/manifest-ledger/app/apptesting"
	appparams "github.com/manifest-network/manifest-ledger/app/params"
	"github.com/manifest-network/manifest-ledger/x/sku/types"
)

type testFixture struct {
	App         *app.ManifestApp
	EncodingCfg moduletestutil.TestEncodingConfig
	Ctx         sdk.Context
	QueryHelper *baseapp.QueryServiceTestHelper
	TestAccs    []sdk.AccAddress
}

func initFixture(t *testing.T) *testFixture {
	t.Helper()

	s := testFixture{}

	appparams.SetAddressPrefixes()

	encCfg := moduletestutil.MakeTestEncodingConfig()

	s.Ctx, s.App = app.Setup(t)
	s.QueryHelper = &baseapp.QueryServiceTestHelper{
		GRPCQueryRouter: s.App.GRPCQueryRouter(),
		Ctx:             s.Ctx,
	}
	s.TestAccs = apptesting.CreateRandomAccounts(5)

	s.EncodingCfg = encCfg

	// Initialize default params
	err := s.App.SKUKeeper.SetParams(s.Ctx, types.DefaultParams())
	require.NoError(t, err)

	return &s
}

func TestInitGenesis(t *testing.T) {
	_, _, providerAddr := testdata.KeyTestPubAddr()
	_, _, payoutAddr := testdata.KeyTestPubAddr()
	f := initFixture(t)

	k := f.App.SKUKeeper

	basePrice := sdk.NewCoin("umfx", sdkmath.NewInt(100))

	genesisState := &types.GenesisState{
		Params: types.DefaultParams(),
		Providers: []types.Provider{
			{
				Id:            1,
				Address:       providerAddr.String(),
				PayoutAddress: payoutAddr.String(),
				MetaHash:      []byte("provider1hash"),
				Active:        true,
			},
		},
		NextProviderId: 2,
		Skus: []types.SKU{
			{
				Id:         1,
				ProviderId: 1,
				Name:       "SKU 1",
				Unit:       types.Unit_UNIT_PER_HOUR,
				BasePrice:  basePrice,
				MetaHash:   []byte("hash1"),
				Active:     true,
			},
			{
				Id:         2,
				ProviderId: 1,
				Name:       "SKU 2",
				Unit:       types.Unit_UNIT_PER_DAY,
				BasePrice:  basePrice,
				MetaHash:   []byte("hash2"),
				Active:     false,
			},
		},
		NextSkuId: 3,
	}

	err := k.InitGenesis(f.Ctx, genesisState)
	require.NoError(t, err)

	provider1, err := k.GetProvider(f.Ctx, 1)
	require.NoError(t, err)
	require.Equal(t, providerAddr.String(), provider1.Address)
	require.True(t, provider1.Active)

	sku1, err := k.GetSKU(f.Ctx, 1)
	require.NoError(t, err)
	require.Equal(t, uint64(1), sku1.ProviderId)
	require.Equal(t, "SKU 1", sku1.Name)
	require.True(t, sku1.Active)

	sku2, err := k.GetSKU(f.Ctx, 2)
	require.NoError(t, err)
	require.Equal(t, uint64(1), sku2.ProviderId)
	require.Equal(t, "SKU 2", sku2.Name)
	require.False(t, sku2.Active)
}

func TestExportGenesis(t *testing.T) {
	_, _, providerAddr := testdata.KeyTestPubAddr()
	_, _, payoutAddr := testdata.KeyTestPubAddr()
	f := initFixture(t)

	k := f.App.SKUKeeper

	basePrice := sdk.NewCoin("umfx", sdkmath.NewInt(100))

	provider := types.Provider{
		Id:            1,
		Address:       providerAddr.String(),
		PayoutAddress: payoutAddr.String(),
		MetaHash:      []byte("providerhash"),
		Active:        true,
	}

	err := k.SetProvider(f.Ctx, provider)
	require.NoError(t, err)

	err = k.NextProviderID.Set(f.Ctx, 2)
	require.NoError(t, err)

	sku := types.SKU{
		Id:         1,
		ProviderId: 1,
		Name:       "Test SKU",
		Unit:       types.Unit_UNIT_PER_HOUR,
		BasePrice:  basePrice,
		MetaHash:   []byte("testhash"),
		Active:     true,
	}

	err = k.SetSKU(f.Ctx, sku)
	require.NoError(t, err)

	err = k.NextSKUID.Set(f.Ctx, 2)
	require.NoError(t, err)

	genState := k.ExportGenesis(f.Ctx)

	require.NotNil(t, genState)
	require.Len(t, genState.Providers, 1)
	require.Equal(t, uint64(2), genState.NextProviderId)
	require.Equal(t, providerAddr.String(), genState.Providers[0].Address)
	require.Len(t, genState.Skus, 1)
	require.Equal(t, uint64(2), genState.NextSkuId)
	require.Equal(t, uint64(1), genState.Skus[0].ProviderId)
}

func TestGetProvider(t *testing.T) {
	_, _, providerAddr := testdata.KeyTestPubAddr()
	_, _, payoutAddr := testdata.KeyTestPubAddr()
	f := initFixture(t)

	k := f.App.SKUKeeper

	provider := types.Provider{
		Id:            1,
		Address:       providerAddr.String(),
		PayoutAddress: payoutAddr.String(),
		Active:        true,
	}

	err := k.SetProvider(f.Ctx, provider)
	require.NoError(t, err)

	retrieved, err := k.GetProvider(f.Ctx, 1)
	require.NoError(t, err)
	require.Equal(t, provider.Id, retrieved.Id)
	require.Equal(t, provider.Address, retrieved.Address)
	require.Equal(t, provider.PayoutAddress, retrieved.PayoutAddress)

	_, err = k.GetProvider(f.Ctx, 999)
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrProviderNotFound)
}

func TestGetAllProviders(t *testing.T) {
	f := initFixture(t)

	k := f.App.SKUKeeper

	providers := []types.Provider{
		{
			Id:            1,
			Address:       f.TestAccs[0].String(),
			PayoutAddress: f.TestAccs[1].String(),
			Active:        true,
		},
		{
			Id:            2,
			Address:       f.TestAccs[2].String(),
			PayoutAddress: f.TestAccs[3].String(),
			Active:        true,
		},
		{
			Id:            3,
			Address:       f.TestAccs[4].String(),
			PayoutAddress: f.TestAccs[0].String(),
			Active:        false,
		},
	}

	for _, provider := range providers {
		err := k.SetProvider(f.Ctx, provider)
		require.NoError(t, err)
	}

	allProviders, err := k.GetAllProviders(f.Ctx)
	require.NoError(t, err)
	require.Len(t, allProviders, 3)
}

func TestGetSKU(t *testing.T) {
	_, _, providerAddr := testdata.KeyTestPubAddr()
	_, _, payoutAddr := testdata.KeyTestPubAddr()
	f := initFixture(t)

	k := f.App.SKUKeeper

	basePrice := sdk.NewCoin("umfx", sdkmath.NewInt(100))

	// Create provider first
	provider := types.Provider{
		Id:            1,
		Address:       providerAddr.String(),
		PayoutAddress: payoutAddr.String(),
		Active:        true,
	}
	err := k.SetProvider(f.Ctx, provider)
	require.NoError(t, err)

	sku := types.SKU{
		Id:         1,
		ProviderId: 1,
		Name:       "Test SKU",
		Unit:       types.Unit_UNIT_PER_HOUR,
		BasePrice:  basePrice,
		Active:     true,
	}

	err = k.SetSKU(f.Ctx, sku)
	require.NoError(t, err)

	retrieved, err := k.GetSKU(f.Ctx, 1)
	require.NoError(t, err)
	require.Equal(t, sku.Id, retrieved.Id)
	require.Equal(t, sku.ProviderId, retrieved.ProviderId)
	require.Equal(t, sku.Name, retrieved.Name)

	_, err = k.GetSKU(f.Ctx, 999)
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrSKUNotFound)
}

func TestGetAllSKUs(t *testing.T) {
	f := initFixture(t)

	k := f.App.SKUKeeper

	basePrice := sdk.NewCoin("umfx", sdkmath.NewInt(100))

	// Create providers first
	provider1 := types.Provider{
		Id:            1,
		Address:       f.TestAccs[0].String(),
		PayoutAddress: f.TestAccs[1].String(),
		Active:        true,
	}
	provider2 := types.Provider{
		Id:            2,
		Address:       f.TestAccs[2].String(),
		PayoutAddress: f.TestAccs[3].String(),
		Active:        true,
	}

	err := k.SetProvider(f.Ctx, provider1)
	require.NoError(t, err)
	err = k.SetProvider(f.Ctx, provider2)
	require.NoError(t, err)

	skus := []types.SKU{
		{
			Id:         1,
			ProviderId: 1,
			Name:       "SKU 1",
			Unit:       types.Unit_UNIT_PER_HOUR,
			BasePrice:  basePrice,
			Active:     true,
		},
		{
			Id:         2,
			ProviderId: 2,
			Name:       "SKU 2",
			Unit:       types.Unit_UNIT_PER_DAY,
			BasePrice:  basePrice,
			Active:     true,
		},
		{
			Id:         3,
			ProviderId: 1,
			Name:       "SKU 3",
			Unit:       types.Unit_UNIT_PER_DAY,
			BasePrice:  basePrice,
			Active:     false,
		},
	}

	for _, sku := range skus {
		err := k.SetSKU(f.Ctx, sku)
		require.NoError(t, err)
	}

	allSKUs, err := k.GetAllSKUs(f.Ctx)
	require.NoError(t, err)
	require.Len(t, allSKUs, 3)
}

func TestGetSKUsByProviderID(t *testing.T) {
	f := initFixture(t)

	k := f.App.SKUKeeper

	basePrice := sdk.NewCoin("umfx", sdkmath.NewInt(100))

	// Create providers first
	provider1 := types.Provider{
		Id:            1,
		Address:       f.TestAccs[0].String(),
		PayoutAddress: f.TestAccs[1].String(),
		Active:        true,
	}
	provider2 := types.Provider{
		Id:            2,
		Address:       f.TestAccs[2].String(),
		PayoutAddress: f.TestAccs[3].String(),
		Active:        true,
	}

	err := k.SetProvider(f.Ctx, provider1)
	require.NoError(t, err)
	err = k.SetProvider(f.Ctx, provider2)
	require.NoError(t, err)

	skus := []types.SKU{
		{
			Id:         1,
			ProviderId: 1,
			Name:       "SKU 1",
			Unit:       types.Unit_UNIT_PER_HOUR,
			BasePrice:  basePrice,
			Active:     true,
		},
		{
			Id:         2,
			ProviderId: 2,
			Name:       "SKU 2",
			Unit:       types.Unit_UNIT_PER_DAY,
			BasePrice:  basePrice,
			Active:     true,
		},
		{
			Id:         3,
			ProviderId: 1,
			Name:       "SKU 3",
			Unit:       types.Unit_UNIT_PER_DAY,
			BasePrice:  basePrice,
			Active:     false,
		},
	}

	for _, sku := range skus {
		err := k.SetSKU(f.Ctx, sku)
		require.NoError(t, err)
	}

	provider1SKUs, err := k.GetSKUsByProviderID(f.Ctx, 1)
	require.NoError(t, err)
	require.Len(t, provider1SKUs, 2)

	provider2SKUs, err := k.GetSKUsByProviderID(f.Ctx, 2)
	require.NoError(t, err)
	require.Len(t, provider2SKUs, 1)

	provider3SKUs, err := k.GetSKUsByProviderID(f.Ctx, 3)
	require.NoError(t, err)
	require.Len(t, provider3SKUs, 0)
}

func TestGetNextProviderID(t *testing.T) {
	_, _, authority := testdata.KeyTestPubAddr()
	f := initFixture(t)

	k := f.App.SKUKeeper
	k.SetAuthority(authority.String())

	err := k.NextProviderID.Set(f.Ctx, 1)
	require.NoError(t, err)

	id1, err := k.GetNextProviderID(f.Ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(1), id1)

	id2, err := k.GetNextProviderID(f.Ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(2), id2)

	id3, err := k.GetNextProviderID(f.Ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(3), id3)
}

func TestGetNextSKUID(t *testing.T) {
	_, _, authority := testdata.KeyTestPubAddr()
	f := initFixture(t)

	k := f.App.SKUKeeper
	k.SetAuthority(authority.String())

	err := k.NextSKUID.Set(f.Ctx, 1)
	require.NoError(t, err)

	id1, err := k.GetNextSKUID(f.Ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(1), id1)

	id2, err := k.GetNextSKUID(f.Ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(2), id2)

	id3, err := k.GetNextSKUID(f.Ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(3), id3)
}

func TestInitGenesisWithParams(t *testing.T) {
	f := initFixture(t)

	k := f.App.SKUKeeper

	basePrice := sdk.NewCoin("umfx", sdkmath.NewInt(100))

	genesisState := &types.GenesisState{
		Params: types.Params{
			AllowedList: []string{f.TestAccs[0].String(), f.TestAccs[1].String()},
		},
		Providers: []types.Provider{
			{
				Id:            1,
				Address:       f.TestAccs[2].String(),
				PayoutAddress: f.TestAccs[3].String(),
				Active:        true,
			},
		},
		NextProviderId: 2,
		Skus: []types.SKU{
			{
				Id:         1,
				ProviderId: 1,
				Name:       "SKU 1",
				Unit:       types.Unit_UNIT_PER_HOUR,
				BasePrice:  basePrice,
				Active:     true,
			},
		},
		NextSkuId: 2,
	}

	err := k.InitGenesis(f.Ctx, genesisState)
	require.NoError(t, err)

	// Verify params were initialized
	params, err := k.GetParams(f.Ctx)
	require.NoError(t, err)
	require.Len(t, params.AllowedList, 2)
	require.True(t, params.IsAllowed(f.TestAccs[0].String()))
	require.True(t, params.IsAllowed(f.TestAccs[1].String()))
	require.False(t, params.IsAllowed(f.TestAccs[2].String()))
}

func TestExportGenesisWithParams(t *testing.T) {
	f := initFixture(t)

	k := f.App.SKUKeeper

	// Set params with allowed list
	params := types.Params{
		AllowedList: []string{f.TestAccs[0].String(), f.TestAccs[1].String()},
	}
	err := k.SetParams(f.Ctx, params)
	require.NoError(t, err)

	err = k.NextProviderID.Set(f.Ctx, 1)
	require.NoError(t, err)

	err = k.NextSKUID.Set(f.Ctx, 1)
	require.NoError(t, err)

	genState := k.ExportGenesis(f.Ctx)

	require.NotNil(t, genState)
	require.Len(t, genState.Params.AllowedList, 2)
	require.Contains(t, genState.Params.AllowedList, f.TestAccs[0].String())
	require.Contains(t, genState.Params.AllowedList, f.TestAccs[1].String())
}

func TestGetParams(t *testing.T) {
	f := initFixture(t)

	k := f.App.SKUKeeper

	// Set params
	params := types.Params{
		AllowedList: []string{f.TestAccs[0].String(), f.TestAccs[1].String()},
	}
	err := k.SetParams(f.Ctx, params)
	require.NoError(t, err)

	// Get params
	gotParams, err := k.GetParams(f.Ctx)
	require.NoError(t, err)
	require.Len(t, gotParams.AllowedList, 2)
}

func TestSKUsByProviderIDPagination(t *testing.T) {
	f := initFixture(t)

	k := f.App.SKUKeeper

	basePrice := sdk.NewCoin("umfx", sdkmath.NewInt(100))

	// Create providers
	provider1 := types.Provider{
		Id:            1,
		Address:       f.TestAccs[0].String(),
		PayoutAddress: f.TestAccs[1].String(),
		Active:        true,
	}
	provider2 := types.Provider{
		Id:            2,
		Address:       f.TestAccs[2].String(),
		PayoutAddress: f.TestAccs[3].String(),
		Active:        true,
	}

	err := k.SetProvider(f.Ctx, provider1)
	require.NoError(t, err)
	err = k.SetProvider(f.Ctx, provider2)
	require.NoError(t, err)

	// Create multiple SKUs for different providers
	for i := uint64(1); i <= 5; i++ {
		sku := types.SKU{
			Id:         i,
			ProviderId: 1,
			Name:       "SKU " + string(rune('0'+i)),
			Unit:       types.Unit_UNIT_PER_HOUR,
			BasePrice:  basePrice,
			Active:     true,
		}
		err := k.SKUs.Set(f.Ctx, i, sku)
		require.NoError(t, err)
	}

	for i := uint64(6); i <= 8; i++ {
		sku := types.SKU{
			Id:         i,
			ProviderId: 2,
			Name:       "SKU " + string(rune('0'+i)),
			Unit:       types.Unit_UNIT_PER_DAY,
			BasePrice:  basePrice,
			Active:     true,
		}
		err := k.SKUs.Set(f.Ctx, i, sku)
		require.NoError(t, err)
	}

	// Test GetSKUsByProviderID
	skus, err := k.GetSKUsByProviderID(f.Ctx, 1)
	require.NoError(t, err)
	require.Len(t, skus, 5)

	skus, err = k.GetSKUsByProviderID(f.Ctx, 2)
	require.NoError(t, err)
	require.Len(t, skus, 3)

	skus, err = k.GetSKUsByProviderID(f.Ctx, 999)
	require.NoError(t, err)
	require.Len(t, skus, 0)
}
