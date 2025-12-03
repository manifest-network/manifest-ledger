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
	s.TestAccs = apptesting.CreateRandomAccounts(3)

	s.EncodingCfg = encCfg

	// Initialize default params
	err := s.App.SKUKeeper.SetParams(s.Ctx, types.DefaultParams())
	require.NoError(t, err)

	return &s
}

func TestInitGenesis(t *testing.T) {
	f := initFixture(t)

	k := f.App.SKUKeeper

	basePrice := sdk.NewCoin("umfx", sdkmath.NewInt(100))

	genesisState := &types.GenesisState{
		Params: types.DefaultParams(),
		Skus: []types.SKU{
			{
				Id:        1,
				Provider:  "provider1",
				Name:      "SKU 1",
				Unit:      types.Unit_UNIT_PER_HOUR,
				BasePrice: basePrice,
				MetaHash:  []byte("hash1"),
				Active:    true,
			},
			{
				Id:        2,
				Provider:  "provider2",
				Name:      "SKU 2",
				Unit:      types.Unit_UNIT_PER_DAY,
				BasePrice: basePrice,
				MetaHash:  []byte("hash2"),
				Active:    false,
			},
		},
		NextId: 3,
	}

	err := k.InitGenesis(f.Ctx, genesisState)
	require.NoError(t, err)

	sku1, err := k.GetSKU(f.Ctx, 1)
	require.NoError(t, err)
	require.Equal(t, "provider1", sku1.Provider)
	require.Equal(t, "SKU 1", sku1.Name)
	require.True(t, sku1.Active)

	sku2, err := k.GetSKU(f.Ctx, 2)
	require.NoError(t, err)
	require.Equal(t, "provider2", sku2.Provider)
	require.Equal(t, "SKU 2", sku2.Name)
	require.False(t, sku2.Active)
}

func TestExportGenesis(t *testing.T) {
	f := initFixture(t)

	k := f.App.SKUKeeper

	basePrice := sdk.NewCoin("umfx", sdkmath.NewInt(100))

	sku := types.SKU{
		Id:        1,
		Provider:  "provider1",
		Name:      "Test SKU",
		Unit:      types.Unit_UNIT_PER_HOUR,
		BasePrice: basePrice,
		MetaHash:  []byte("testhash"),
		Active:    true,
	}

	err := k.SetSKU(f.Ctx, sku)
	require.NoError(t, err)

	err = k.NextID.Set(f.Ctx, 2)
	require.NoError(t, err)

	genState := k.ExportGenesis(f.Ctx)

	require.NotNil(t, genState)
	require.Len(t, genState.Skus, 1)
	require.Equal(t, uint64(2), genState.NextId)
	require.Equal(t, "provider1", genState.Skus[0].Provider)
}

func TestGetSKU(t *testing.T) {
	f := initFixture(t)

	k := f.App.SKUKeeper

	basePrice := sdk.NewCoin("umfx", sdkmath.NewInt(100))

	sku := types.SKU{
		Id:        1,
		Provider:  "provider1",
		Name:      "Test SKU",
		Unit:      types.Unit_UNIT_PER_HOUR,
		BasePrice: basePrice,
		Active:    true,
	}

	err := k.SetSKU(f.Ctx, sku)
	require.NoError(t, err)

	retrieved, err := k.GetSKU(f.Ctx, 1)
	require.NoError(t, err)
	require.Equal(t, sku.Id, retrieved.Id)
	require.Equal(t, sku.Provider, retrieved.Provider)
	require.Equal(t, sku.Name, retrieved.Name)

	_, err = k.GetSKU(f.Ctx, 999)
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrSKUNotFound)
}

func TestDeleteSKU(t *testing.T) {
	f := initFixture(t)

	k := f.App.SKUKeeper

	basePrice := sdk.NewCoin("umfx", sdkmath.NewInt(100))

	sku := types.SKU{
		Id:        1,
		Provider:  "provider1",
		Name:      "Test SKU",
		Unit:      types.Unit_UNIT_PER_HOUR,
		BasePrice: basePrice,
		Active:    true,
	}

	err := k.SetSKU(f.Ctx, sku)
	require.NoError(t, err)

	_, err = k.GetSKU(f.Ctx, 1)
	require.NoError(t, err)

	err = k.DeleteSKU(f.Ctx, 1)
	require.NoError(t, err)

	_, err = k.GetSKU(f.Ctx, 1)
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrSKUNotFound)
}

func TestGetAllSKUs(t *testing.T) {
	f := initFixture(t)

	k := f.App.SKUKeeper

	basePrice := sdk.NewCoin("umfx", sdkmath.NewInt(100))

	skus := []types.SKU{
		{
			Id:        1,
			Provider:  "provider1",
			Name:      "SKU 1",
			Unit:      types.Unit_UNIT_PER_HOUR,
			BasePrice: basePrice,
			Active:    true,
		},
		{
			Id:        2,
			Provider:  "provider2",
			Name:      "SKU 2",
			Unit:      types.Unit_UNIT_PER_DAY,
			BasePrice: basePrice,
			Active:    true,
		},
		{
			Id:        3,
			Provider:  "provider1",
			Name:      "SKU 3",
			Unit:      types.Unit_UNIT_PER_MONTH,
			BasePrice: basePrice,
			Active:    false,
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

func TestGetSKUsByProvider(t *testing.T) {
	f := initFixture(t)

	k := f.App.SKUKeeper

	basePrice := sdk.NewCoin("umfx", sdkmath.NewInt(100))

	skus := []types.SKU{
		{
			Id:        1,
			Provider:  "provider1",
			Name:      "SKU 1",
			Unit:      types.Unit_UNIT_PER_HOUR,
			BasePrice: basePrice,
			Active:    true,
		},
		{
			Id:        2,
			Provider:  "provider2",
			Name:      "SKU 2",
			Unit:      types.Unit_UNIT_PER_DAY,
			BasePrice: basePrice,
			Active:    true,
		},
		{
			Id:        3,
			Provider:  "provider1",
			Name:      "SKU 3",
			Unit:      types.Unit_UNIT_PER_MONTH,
			BasePrice: basePrice,
			Active:    false,
		},
	}

	for _, sku := range skus {
		err := k.SetSKU(f.Ctx, sku)
		require.NoError(t, err)
	}

	provider1SKUs, err := k.GetSKUsByProvider(f.Ctx, "provider1")
	require.NoError(t, err)
	require.Len(t, provider1SKUs, 2)

	provider2SKUs, err := k.GetSKUsByProvider(f.Ctx, "provider2")
	require.NoError(t, err)
	require.Len(t, provider2SKUs, 1)

	provider3SKUs, err := k.GetSKUsByProvider(f.Ctx, "provider3")
	require.NoError(t, err)
	require.Len(t, provider3SKUs, 0)
}

func TestGetNextID(t *testing.T) {
	_, _, authority := testdata.KeyTestPubAddr()
	f := initFixture(t)

	k := f.App.SKUKeeper
	k.SetAuthority(authority.String())

	err := k.NextID.Set(f.Ctx, 1)
	require.NoError(t, err)

	id1, err := k.GetNextID(f.Ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(1), id1)

	id2, err := k.GetNextID(f.Ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(2), id2)

	id3, err := k.GetNextID(f.Ctx)
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
		Skus: []types.SKU{
			{
				Id:        1,
				Provider:  "provider1",
				Name:      "SKU 1",
				Unit:      types.Unit_UNIT_PER_HOUR,
				BasePrice: basePrice,
				Active:    true,
			},
		},
		NextId: 2,
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

	err = k.NextID.Set(f.Ctx, 1)
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

func TestSKUsByProviderPagination(t *testing.T) {
	f := initFixture(t)

	k := f.App.SKUKeeper

	basePrice := sdk.NewCoin("umfx", sdkmath.NewInt(100))

	// Create multiple SKUs for different providers
	for i := uint64(1); i <= 5; i++ {
		sku := types.SKU{
			Id:        i,
			Provider:  "provider1",
			Name:      "SKU " + string(rune('0'+i)),
			Unit:      types.Unit_UNIT_PER_HOUR,
			BasePrice: basePrice,
			Active:    true,
		}
		err := k.SKUs.Set(f.Ctx, i, sku)
		require.NoError(t, err)
	}

	for i := uint64(6); i <= 8; i++ {
		sku := types.SKU{
			Id:        i,
			Provider:  "provider2",
			Name:      "SKU " + string(rune('0'+i)),
			Unit:      types.Unit_UNIT_PER_DAY,
			BasePrice: basePrice,
			Active:    true,
		}
		err := k.SKUs.Set(f.Ctx, i, sku)
		require.NoError(t, err)
	}

	// Test GetSKUsByProvider
	skus, err := k.GetSKUsByProvider(f.Ctx, "provider1")
	require.NoError(t, err)
	require.Len(t, skus, 5)

	skus, err = k.GetSKUsByProvider(f.Ctx, "provider2")
	require.NoError(t, err)
	require.Len(t, skus, 3)

	skus, err = k.GetSKUsByProvider(f.Ctx, "nonexistent")
	require.NoError(t, err)
	require.Len(t, skus, 0)
}
