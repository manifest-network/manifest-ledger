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
	providerUUID := "01912345-6789-7abc-8def-0123456789ab"
	sku1UUID := "01912345-6789-7abc-8def-0123456789ac"
	sku2UUID := "01912345-6789-7abc-8def-0123456789ad"

	genesisState := &types.GenesisState{
		Params: types.DefaultParams(),
		Providers: []types.Provider{
			{
				Uuid:          providerUUID,
				Address:       providerAddr.String(),
				PayoutAddress: payoutAddr.String(),
				MetaHash:      []byte("provider1hash"),
				Active:        true,
			},
		},
		Skus: []types.SKU{
			{
				Uuid:         sku1UUID,
				ProviderUuid: providerUUID,
				Name:         "SKU 1",
				Unit:         types.Unit_UNIT_PER_HOUR,
				BasePrice:    basePrice,
				MetaHash:     []byte("hash1"),
				Active:       true,
			},
			{
				Uuid:         sku2UUID,
				ProviderUuid: providerUUID,
				Name:         "SKU 2",
				Unit:         types.Unit_UNIT_PER_DAY,
				BasePrice:    basePrice,
				MetaHash:     []byte("hash2"),
				Active:       false,
			},
		},
	}

	err := k.InitGenesis(f.Ctx, genesisState)
	require.NoError(t, err)

	provider1, err := k.GetProvider(f.Ctx, providerUUID)
	require.NoError(t, err)
	require.Equal(t, providerAddr.String(), provider1.Address)
	require.True(t, provider1.Active)

	sku1, err := k.GetSKU(f.Ctx, sku1UUID)
	require.NoError(t, err)
	require.Equal(t, providerUUID, sku1.ProviderUuid)
	require.Equal(t, "SKU 1", sku1.Name)
	require.True(t, sku1.Active)

	sku2, err := k.GetSKU(f.Ctx, sku2UUID)
	require.NoError(t, err)
	require.Equal(t, providerUUID, sku2.ProviderUuid)
	require.Equal(t, "SKU 2", sku2.Name)
	require.False(t, sku2.Active)
}

func TestExportGenesis(t *testing.T) {
	_, _, providerAddr := testdata.KeyTestPubAddr()
	_, _, payoutAddr := testdata.KeyTestPubAddr()
	f := initFixture(t)

	k := f.App.SKUKeeper

	basePrice := sdk.NewCoin("umfx", sdkmath.NewInt(100))
	providerUUID := "01912345-6789-7abc-8def-0123456789ab"
	skuUUID := "01912345-6789-7abc-8def-0123456789ac"

	provider := types.Provider{
		Uuid:          providerUUID,
		Address:       providerAddr.String(),
		PayoutAddress: payoutAddr.String(),
		MetaHash:      []byte("providerhash"),
		Active:        true,
	}

	err := k.SetProvider(f.Ctx, provider)
	require.NoError(t, err)

	sku := types.SKU{
		Uuid:         skuUUID,
		ProviderUuid: providerUUID,
		Name:         "Test SKU",
		Unit:         types.Unit_UNIT_PER_HOUR,
		BasePrice:    basePrice,
		MetaHash:     []byte("testhash"),
		Active:       true,
	}

	err = k.SetSKU(f.Ctx, sku)
	require.NoError(t, err)

	genState := k.ExportGenesis(f.Ctx)

	require.NotNil(t, genState)
	require.Len(t, genState.Providers, 1)
	require.Equal(t, providerAddr.String(), genState.Providers[0].Address)
	require.Len(t, genState.Skus, 1)
	require.Equal(t, providerUUID, genState.Skus[0].ProviderUuid)
}

func TestGetProvider(t *testing.T) {
	_, _, providerAddr := testdata.KeyTestPubAddr()
	_, _, payoutAddr := testdata.KeyTestPubAddr()
	f := initFixture(t)

	k := f.App.SKUKeeper
	providerUUID := "01912345-6789-7abc-8def-0123456789ab"

	provider := types.Provider{
		Uuid:          providerUUID,
		Address:       providerAddr.String(),
		PayoutAddress: payoutAddr.String(),
		Active:        true,
	}

	err := k.SetProvider(f.Ctx, provider)
	require.NoError(t, err)

	retrieved, err := k.GetProvider(f.Ctx, providerUUID)
	require.NoError(t, err)
	require.Equal(t, provider.Uuid, retrieved.Uuid)
	require.Equal(t, provider.Address, retrieved.Address)
	require.Equal(t, provider.PayoutAddress, retrieved.PayoutAddress)

	_, err = k.GetProvider(f.Ctx, "01912345-6789-7abc-8def-999999999999")
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrProviderNotFound)
}

func TestGetAllProviders(t *testing.T) {
	f := initFixture(t)

	k := f.App.SKUKeeper

	providers := []types.Provider{
		{
			Uuid:          "01912345-6789-7abc-8def-0123456789a1",
			Address:       f.TestAccs[0].String(),
			PayoutAddress: f.TestAccs[1].String(),
			Active:        true,
		},
		{
			Uuid:          "01912345-6789-7abc-8def-0123456789a2",
			Address:       f.TestAccs[2].String(),
			PayoutAddress: f.TestAccs[3].String(),
			Active:        true,
		},
		{
			Uuid:          "01912345-6789-7abc-8def-0123456789a3",
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
	providerUUID := "01912345-6789-7abc-8def-0123456789ab"
	skuUUID := "01912345-6789-7abc-8def-0123456789ac"

	// Create provider first
	provider := types.Provider{
		Uuid:          providerUUID,
		Address:       providerAddr.String(),
		PayoutAddress: payoutAddr.String(),
		Active:        true,
	}
	err := k.SetProvider(f.Ctx, provider)
	require.NoError(t, err)

	sku := types.SKU{
		Uuid:         skuUUID,
		ProviderUuid: providerUUID,
		Name:         "Test SKU",
		Unit:         types.Unit_UNIT_PER_HOUR,
		BasePrice:    basePrice,
		Active:       true,
	}

	err = k.SetSKU(f.Ctx, sku)
	require.NoError(t, err)

	retrieved, err := k.GetSKU(f.Ctx, skuUUID)
	require.NoError(t, err)
	require.Equal(t, sku.Uuid, retrieved.Uuid)
	require.Equal(t, sku.ProviderUuid, retrieved.ProviderUuid)
	require.Equal(t, sku.Name, retrieved.Name)

	_, err = k.GetSKU(f.Ctx, "01912345-6789-7abc-8def-999999999999")
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrSKUNotFound)
}

func TestGetAllSKUs(t *testing.T) {
	f := initFixture(t)

	k := f.App.SKUKeeper

	basePrice := sdk.NewCoin("umfx", sdkmath.NewInt(100))
	provider1UUID := "01912345-6789-7abc-8def-0123456789a1"
	provider2UUID := "01912345-6789-7abc-8def-0123456789a2"

	// Create providers first
	provider1 := types.Provider{
		Uuid:          provider1UUID,
		Address:       f.TestAccs[0].String(),
		PayoutAddress: f.TestAccs[1].String(),
		Active:        true,
	}
	provider2 := types.Provider{
		Uuid:          provider2UUID,
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
			Uuid:         "01912345-6789-7abc-8def-0123456789b1",
			ProviderUuid: provider1UUID,
			Name:         "SKU 1",
			Unit:         types.Unit_UNIT_PER_HOUR,
			BasePrice:    basePrice,
			Active:       true,
		},
		{
			Uuid:         "01912345-6789-7abc-8def-0123456789b2",
			ProviderUuid: provider2UUID,
			Name:         "SKU 2",
			Unit:         types.Unit_UNIT_PER_DAY,
			BasePrice:    basePrice,
			Active:       true,
		},
		{
			Uuid:         "01912345-6789-7abc-8def-0123456789b3",
			ProviderUuid: provider1UUID,
			Name:         "SKU 3",
			Unit:         types.Unit_UNIT_PER_DAY,
			BasePrice:    basePrice,
			Active:       false,
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

func TestGetSKUsByProviderUUID(t *testing.T) {
	f := initFixture(t)

	k := f.App.SKUKeeper

	basePrice := sdk.NewCoin("umfx", sdkmath.NewInt(100))
	provider1UUID := "01912345-6789-7abc-8def-0123456789a1"
	provider2UUID := "01912345-6789-7abc-8def-0123456789a2"

	// Create providers first
	provider1 := types.Provider{
		Uuid:          provider1UUID,
		Address:       f.TestAccs[0].String(),
		PayoutAddress: f.TestAccs[1].String(),
		Active:        true,
	}
	provider2 := types.Provider{
		Uuid:          provider2UUID,
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
			Uuid:         "01912345-6789-7abc-8def-0123456789b1",
			ProviderUuid: provider1UUID,
			Name:         "SKU 1",
			Unit:         types.Unit_UNIT_PER_HOUR,
			BasePrice:    basePrice,
			Active:       true,
		},
		{
			Uuid:         "01912345-6789-7abc-8def-0123456789b2",
			ProviderUuid: provider2UUID,
			Name:         "SKU 2",
			Unit:         types.Unit_UNIT_PER_DAY,
			BasePrice:    basePrice,
			Active:       true,
		},
		{
			Uuid:         "01912345-6789-7abc-8def-0123456789b3",
			ProviderUuid: provider1UUID,
			Name:         "SKU 3",
			Unit:         types.Unit_UNIT_PER_DAY,
			BasePrice:    basePrice,
			Active:       false,
		},
	}

	for _, sku := range skus {
		err := k.SetSKU(f.Ctx, sku)
		require.NoError(t, err)
	}

	provider1SKUs, err := k.GetSKUsByProviderUUID(f.Ctx, provider1UUID)
	require.NoError(t, err)
	require.Len(t, provider1SKUs, 2)

	provider2SKUs, err := k.GetSKUsByProviderUUID(f.Ctx, provider2UUID)
	require.NoError(t, err)
	require.Len(t, provider2SKUs, 1)

	provider3SKUs, err := k.GetSKUsByProviderUUID(f.Ctx, "01912345-6789-7abc-8def-0123456789a3")
	require.NoError(t, err)
	require.Len(t, provider3SKUs, 0)
}

func TestInitGenesisWithParams(t *testing.T) {
	f := initFixture(t)

	k := f.App.SKUKeeper

	basePrice := sdk.NewCoin("umfx", sdkmath.NewInt(100))
	providerUUID := "01912345-6789-7abc-8def-0123456789ab"
	skuUUID := "01912345-6789-7abc-8def-0123456789ac"

	genesisState := &types.GenesisState{
		Params: types.Params{
			AllowedList: []string{f.TestAccs[0].String(), f.TestAccs[1].String()},
		},
		Providers: []types.Provider{
			{
				Uuid:          providerUUID,
				Address:       f.TestAccs[2].String(),
				PayoutAddress: f.TestAccs[3].String(),
				Active:        true,
			},
		},
		Skus: []types.SKU{
			{
				Uuid:         skuUUID,
				ProviderUuid: providerUUID,
				Name:         "SKU 1",
				Unit:         types.Unit_UNIT_PER_HOUR,
				BasePrice:    basePrice,
				Active:       true,
			},
		},
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

func TestSKUsByProviderUUIDPagination(t *testing.T) {
	f := initFixture(t)

	k := f.App.SKUKeeper

	basePrice := sdk.NewCoin("umfx", sdkmath.NewInt(100))
	provider1UUID := "01912345-6789-7abc-8def-0123456789a1"
	provider2UUID := "01912345-6789-7abc-8def-0123456789a2"

	// Create providers
	provider1 := types.Provider{
		Uuid:          provider1UUID,
		Address:       f.TestAccs[0].String(),
		PayoutAddress: f.TestAccs[1].String(),
		Active:        true,
	}
	provider2 := types.Provider{
		Uuid:          provider2UUID,
		Address:       f.TestAccs[2].String(),
		PayoutAddress: f.TestAccs[3].String(),
		Active:        true,
	}

	err := k.SetProvider(f.Ctx, provider1)
	require.NoError(t, err)
	err = k.SetProvider(f.Ctx, provider2)
	require.NoError(t, err)

	// Create multiple SKUs for different providers
	for i := 1; i <= 5; i++ {
		skuUUID := "01912345-6789-7abc-8def-0123456789b" + string(rune('0'+i))
		sku := types.SKU{
			Uuid:         skuUUID,
			ProviderUuid: provider1UUID,
			Name:         "SKU " + string(rune('0'+i)),
			Unit:         types.Unit_UNIT_PER_HOUR,
			BasePrice:    basePrice,
			Active:       true,
		}
		err := k.SKUs.Set(f.Ctx, skuUUID, sku)
		require.NoError(t, err)
	}

	for i := 6; i <= 8; i++ {
		skuUUID := "01912345-6789-7abc-8def-0123456789b" + string(rune('0'+i))
		sku := types.SKU{
			Uuid:         skuUUID,
			ProviderUuid: provider2UUID,
			Name:         "SKU " + string(rune('0'+i)),
			Unit:         types.Unit_UNIT_PER_DAY,
			BasePrice:    basePrice,
			Active:       true,
		}
		err := k.SKUs.Set(f.Ctx, skuUUID, sku)
		require.NoError(t, err)
	}

	// Test GetSKUsByProviderUUID
	skus, err := k.GetSKUsByProviderUUID(f.Ctx, provider1UUID)
	require.NoError(t, err)
	require.Len(t, skus, 5)

	skus, err = k.GetSKUsByProviderUUID(f.Ctx, provider2UUID)
	require.NoError(t, err)
	require.Len(t, skus, 3)

	skus, err = k.GetSKUsByProviderUUID(f.Ctx, "01912345-6789-7abc-8def-999999999999")
	require.NoError(t, err)
	require.Len(t, skus, 0)
}
