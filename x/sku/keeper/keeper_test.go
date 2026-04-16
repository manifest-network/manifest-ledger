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

const (
	testProviderUUID  = "01912345-6789-7abc-8def-0123456789ab"
	testProvider1UUID = "01912345-6789-7abc-8def-0123456789a1"
	testProvider2UUID = "01912345-6789-7abc-8def-0123456789a2"
	testProvider3UUID = "01912345-6789-7abc-8def-0123456789f1"
	testProvider4UUID = "01912345-6789-7abc-8def-0123456789f2"
	testSKU1UUID      = "01912345-6789-7abc-8def-0123456789ac"
	testSKU2UUID      = "01912345-6789-7abc-8def-0123456789ad"
	testSKU3UUID      = "01912345-6789-7abc-8def-0123456789f3"
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
				Uuid:          testProviderUUID,
				Address:       providerAddr.String(),
				PayoutAddress: payoutAddr.String(),
				MetaHash:      []byte("provider1hash"),
				Active:        true,
			},
		},
		Skus: []types.SKU{
			{
				Uuid:         testSKU1UUID,
				ProviderUuid: testProviderUUID,
				Name:         "SKU 1",
				Unit:         types.Unit_UNIT_PER_HOUR,
				BasePrice:    basePrice,
				MetaHash:     []byte("hash1"),
				Active:       true,
			},
			{
				Uuid:         testSKU2UUID,
				ProviderUuid: testProviderUUID,
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

	provider1, err := k.GetProvider(f.Ctx, testProviderUUID)
	require.NoError(t, err)
	require.Equal(t, providerAddr.String(), provider1.Address)
	require.True(t, provider1.Active)

	sku1, err := k.GetSKU(f.Ctx, testSKU1UUID)
	require.NoError(t, err)
	require.Equal(t, testProviderUUID, sku1.ProviderUuid)
	require.Equal(t, "SKU 1", sku1.Name)
	require.True(t, sku1.Active)

	sku2, err := k.GetSKU(f.Ctx, testSKU2UUID)
	require.NoError(t, err)
	require.Equal(t, testProviderUUID, sku2.ProviderUuid)
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
		Uuid:          testProviderUUID,
		Address:       providerAddr.String(),
		PayoutAddress: payoutAddr.String(),
		MetaHash:      []byte("providerhash"),
		Active:        true,
	}

	err := k.SetProvider(f.Ctx, provider)
	require.NoError(t, err)

	sku := types.SKU{
		Uuid:         testSKU1UUID,
		ProviderUuid: testProviderUUID,
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
	require.Equal(t, testProviderUUID, genState.Skus[0].ProviderUuid)
}

// TestGenesisSequenceRoundTrip verifies that UUID generation sequences survive
// a genesis export/import cycle, preventing UUID collisions after chain restart.
func TestGenesisSequenceRoundTrip(t *testing.T) {
	f := initFixture(t)

	k := f.App.SKUKeeper
	providerAddr := f.TestAccs[0]
	payoutAddr := f.TestAccs[1]

	// Create 2 providers and 3 SKUs via the UUID generation path
	providerUUID1, err := k.GenerateProviderUUID(f.Ctx)
	require.NoError(t, err)
	err = k.SetProvider(f.Ctx, types.Provider{
		Uuid: providerUUID1, Address: providerAddr.String(),
		PayoutAddress: payoutAddr.String(), Active: true,
	})
	require.NoError(t, err)

	providerUUID2, err := k.GenerateProviderUUID(f.Ctx)
	require.NoError(t, err)
	err = k.SetProvider(f.Ctx, types.Provider{
		Uuid: providerUUID2, Address: payoutAddr.String(),
		PayoutAddress: providerAddr.String(), Active: true,
	})
	require.NoError(t, err)

	var skuUUIDs []string
	for range 3 {
		skuUUID, err := k.GenerateSKUUUID(f.Ctx)
		require.NoError(t, err)
		err = k.SetSKU(f.Ctx, types.SKU{
			Uuid: skuUUID, ProviderUuid: providerUUID1,
			Name: "Test SKU", Unit: types.Unit_UNIT_PER_HOUR,
			BasePrice: sdk.NewCoin("umfx", sdkmath.NewInt(3600)), Active: true,
		})
		require.NoError(t, err)
		skuUUIDs = append(skuUUIDs, skuUUID)
	}

	// Export genesis
	exportedGenesis := k.ExportGenesis(f.Ctx)
	require.Equal(t, uint64(2), exportedGenesis.ProviderSequence)
	require.Equal(t, uint64(3), exportedGenesis.SkuSequence)

	// Verify exported genesis passes validation (same path as chain restart)
	require.NoError(t, exportedGenesis.Validate())

	// Import into fresh fixture
	f2 := initFixture(t)
	k2 := f2.App.SKUKeeper
	err = k2.InitGenesis(f2.Ctx, exportedGenesis)
	require.NoError(t, err)

	// Verify sequences were restored
	provSeq, err := k2.ProviderSequence.Peek(f2.Ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(2), provSeq)

	skuSeq, err := k2.SKUSequence.Peek(f2.Ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(3), skuSeq)

	// Create new entities post-import and verify no UUID collision
	newProviderUUID, err := k2.GenerateProviderUUID(f2.Ctx)
	require.NoError(t, err)
	require.NotEqual(t, providerUUID1, newProviderUUID)
	require.NotEqual(t, providerUUID2, newProviderUUID)

	newSKUUUID, err := k2.GenerateSKUUUID(f2.Ctx)
	require.NoError(t, err)
	for _, existing := range skuUUIDs {
		require.NotEqual(t, existing, newSKUUUID,
			"post-import SKU UUID should not collide with pre-import UUIDs")
	}
}

func TestGetProvider(t *testing.T) {
	_, _, providerAddr := testdata.KeyTestPubAddr()
	_, _, payoutAddr := testdata.KeyTestPubAddr()
	f := initFixture(t)

	k := f.App.SKUKeeper

	provider := types.Provider{
		Uuid:          testProviderUUID,
		Address:       providerAddr.String(),
		PayoutAddress: payoutAddr.String(),
		Active:        true,
	}

	err := k.SetProvider(f.Ctx, provider)
	require.NoError(t, err)

	retrieved, err := k.GetProvider(f.Ctx, testProviderUUID)
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
			Uuid:          testProvider1UUID,
			Address:       f.TestAccs[0].String(),
			PayoutAddress: f.TestAccs[1].String(),
			Active:        true,
		},
		{
			Uuid:          testProvider2UUID,
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

	// Create provider first
	provider := types.Provider{
		Uuid:          testProviderUUID,
		Address:       providerAddr.String(),
		PayoutAddress: payoutAddr.String(),
		Active:        true,
	}
	err := k.SetProvider(f.Ctx, provider)
	require.NoError(t, err)

	sku := types.SKU{
		Uuid:         testSKU1UUID,
		ProviderUuid: testProviderUUID,
		Name:         "Test SKU",
		Unit:         types.Unit_UNIT_PER_HOUR,
		BasePrice:    basePrice,
		Active:       true,
	}

	err = k.SetSKU(f.Ctx, sku)
	require.NoError(t, err)

	retrieved, err := k.GetSKU(f.Ctx, testSKU1UUID)
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

	// Create providers first
	provider1 := types.Provider{
		Uuid:          testProvider1UUID,
		Address:       f.TestAccs[0].String(),
		PayoutAddress: f.TestAccs[1].String(),
		Active:        true,
	}
	provider2 := types.Provider{
		Uuid:          testProvider2UUID,
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
			ProviderUuid: testProvider1UUID,
			Name:         "SKU 1",
			Unit:         types.Unit_UNIT_PER_HOUR,
			BasePrice:    basePrice,
			Active:       true,
		},
		{
			Uuid:         "01912345-6789-7abc-8def-0123456789b2",
			ProviderUuid: testProvider2UUID,
			Name:         "SKU 2",
			Unit:         types.Unit_UNIT_PER_DAY,
			BasePrice:    basePrice,
			Active:       true,
		},
		{
			Uuid:         "01912345-6789-7abc-8def-0123456789b3",
			ProviderUuid: testProvider1UUID,
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

	// Create providers first
	provider1 := types.Provider{
		Uuid:          testProvider1UUID,
		Address:       f.TestAccs[0].String(),
		PayoutAddress: f.TestAccs[1].String(),
		Active:        true,
	}
	provider2 := types.Provider{
		Uuid:          testProvider2UUID,
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
			ProviderUuid: testProvider1UUID,
			Name:         "SKU 1",
			Unit:         types.Unit_UNIT_PER_HOUR,
			BasePrice:    basePrice,
			Active:       true,
		},
		{
			Uuid:         "01912345-6789-7abc-8def-0123456789b2",
			ProviderUuid: testProvider2UUID,
			Name:         "SKU 2",
			Unit:         types.Unit_UNIT_PER_DAY,
			BasePrice:    basePrice,
			Active:       true,
		},
		{
			Uuid:         "01912345-6789-7abc-8def-0123456789b3",
			ProviderUuid: testProvider1UUID,
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

	provider1SKUs, err := k.GetSKUsByProviderUUID(f.Ctx, testProvider1UUID)
	require.NoError(t, err)
	require.Len(t, provider1SKUs, 2)

	provider2SKUs, err := k.GetSKUsByProviderUUID(f.Ctx, testProvider2UUID)
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

	genesisState := &types.GenesisState{
		Params: types.Params{
			AllowedList: []string{f.TestAccs[0].String(), f.TestAccs[1].String()},
		},
		Providers: []types.Provider{
			{
				Uuid:          testProviderUUID,
				Address:       f.TestAccs[2].String(),
				PayoutAddress: f.TestAccs[3].String(),
				Active:        true,
			},
		},
		Skus: []types.SKU{
			{
				Uuid:         testSKU1UUID,
				ProviderUuid: testProviderUUID,
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

	// Create providers
	provider1 := types.Provider{
		Uuid:          testProvider1UUID,
		Address:       f.TestAccs[0].String(),
		PayoutAddress: f.TestAccs[1].String(),
		Active:        true,
	}
	provider2 := types.Provider{
		Uuid:          testProvider2UUID,
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
			ProviderUuid: testProvider1UUID,
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
			ProviderUuid: testProvider2UUID,
			Name:         "SKU " + string(rune('0'+i)),
			Unit:         types.Unit_UNIT_PER_DAY,
			BasePrice:    basePrice,
			Active:       true,
		}
		err := k.SKUs.Set(f.Ctx, skuUUID, sku)
		require.NoError(t, err)
	}

	// Test GetSKUsByProviderUUID
	skus, err := k.GetSKUsByProviderUUID(f.Ctx, testProvider1UUID)
	require.NoError(t, err)
	require.Len(t, skus, 5)

	skus, err = k.GetSKUsByProviderUUID(f.Ctx, testProvider2UUID)
	require.NoError(t, err)
	require.Len(t, skus, 3)

	skus, err = k.GetSKUsByProviderUUID(f.Ctx, "01912345-6789-7abc-8def-999999999999")
	require.NoError(t, err)
	require.Len(t, skus, 0)
}
