/*
Package keeper_test contains unit tests for the billing module keeper.

Test Coverage:
- Genesis state initialization and export
- Parameter management
- Credit account derivation and operations
- Credit funding
- Lease CRUD operations
- Indexed queries (by tenant, by provider)

The tests use the real ManifestApp for integration testing, ensuring
proper interaction with the bank module for credit account balances.
*/
package keeper_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	sdkmath "cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/testutil/testdata"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	"github.com/manifest-network/manifest-ledger/app"
	"github.com/manifest-network/manifest-ledger/app/apptesting"
	appparams "github.com/manifest-network/manifest-ledger/app/params"
	"github.com/manifest-network/manifest-ledger/x/billing/keeper"
	"github.com/manifest-network/manifest-ledger/x/billing/types"
	skutypes "github.com/manifest-network/manifest-ledger/x/sku/types"
)

const (
	testDenom        = "umfx"
	testDenom2       = "upwr"
	testLeaseUUID1   = "01912345-6789-7abc-8def-0123456789ab"
	testLeaseUUID2   = "01912345-6789-7abc-8def-0123456789ac"
	testProviderUUID = "01912345-6789-7abc-8def-0123456789ad"
	testSKUUUID      = "01912345-6789-7abc-8def-0123456789ae"
)

type testFixture struct {
	App         *app.ManifestApp
	EncodingCfg moduletestutil.TestEncodingConfig
	Ctx         sdk.Context
	QueryHelper *baseapp.QueryServiceTestHelper
	TestAccs    []sdk.AccAddress
	Authority   sdk.AccAddress
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

	// Use testdata to generate authority key
	_, _, authority := testdata.KeyTestPubAddr()
	s.Authority = authority

	s.EncodingCfg = encCfg

	// Initialize default params
	err := s.App.BillingKeeper.SetParams(s.Ctx, types.DefaultParams())
	require.NoError(t, err)

	// Set authority
	s.App.BillingKeeper.SetAuthority(authority.String())

	return &s
}

// fundAccount sends tokens to an account using the bank module.
func (f *testFixture) fundAccount(t *testing.T, addr sdk.AccAddress, coins sdk.Coins) {
	t.Helper()
	err := f.App.BankKeeper.MintCoins(f.Ctx, "mint", coins)
	require.NoError(t, err)
	err = f.App.BankKeeper.SendCoinsFromModuleToAccount(f.Ctx, "mint", addr, coins)
	require.NoError(t, err)
}

// createTestProvider creates a provider in the SKU module for testing.
func (f *testFixture) createTestProvider(t *testing.T, address, payoutAddress string) skutypes.Provider {
	t.Helper()
	// Generate a unique UUID for the provider
	providerUUID, err := f.App.SKUKeeper.GenerateProviderUUID(f.Ctx)
	require.NoError(t, err)

	provider := skutypes.Provider{
		Uuid:          providerUUID,
		Address:       address,
		PayoutAddress: payoutAddress,
		Active:        true,
	}
	err = f.App.SKUKeeper.SetProvider(f.Ctx, provider)
	require.NoError(t, err)
	return provider
}

// createTestSKU creates a SKU in the SKU module for testing.
func (f *testFixture) createTestSKU(t *testing.T, providerUUID string, priceAmount int64) skutypes.SKU {
	t.Helper()
	// Generate a unique UUID for the SKU
	skuUUID, err := f.App.SKUKeeper.GenerateSKUUUID(f.Ctx)
	require.NoError(t, err)

	sku := skutypes.SKU{
		Uuid:         skuUUID,
		ProviderUuid: providerUUID,
		Name:         "Test SKU",
		Unit:         skutypes.Unit_UNIT_PER_HOUR,
		BasePrice:    sdk.NewCoin("umfx", sdkmath.NewInt(priceAmount)),
		Active:       true,
	}
	err = f.App.SKUKeeper.SetSKU(f.Ctx, sku)
	require.NoError(t, err)
	return sku
}

func TestInitGenesis(t *testing.T) {
	f := initFixture(t)

	k := f.App.BillingKeeper

	// Create provider and SKU first (required for genesis validation)
	providerAddr := f.TestAccs[1]
	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600) // 3600 umfx/hour

	// Fund the credit address before importing genesis
	tenant := f.TestAccs[0]
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)

	denom := testDenom
	balance := sdk.NewCoin(denom, sdkmath.NewInt(1000000))

	// Mint and send to credit address
	f.fundAccount(t, creditAddr, sdk.NewCoins(balance))

	genesisState := &types.GenesisState{
		Params: types.DefaultParams(),
		Leases: []types.Lease{
			{
				Uuid:         "01912345-6789-7abc-8def-0123456789ab",
				Tenant:       tenant.String(),
				ProviderUuid: provider.Uuid,
				Items: []types.LeaseItem{
					{
						SkuUuid:     sku.Uuid,
						Quantity:    2,
						LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(100)),
					},
				},
				State:         types.LEASE_STATE_ACTIVE,
				CreatedAt:     f.Ctx.BlockTime(),
				LastSettledAt: f.Ctx.BlockTime(),
			},
		},
		CreditAccounts: []types.CreditAccount{
			{
				Tenant:        tenant.String(),
				CreditAddress: creditAddr.String(),
			},
		},
	}

	err = k.InitGenesis(f.Ctx, genesisState)
	require.NoError(t, err)

	// Verify lease was imported
	lease, err := k.GetLease(f.Ctx, "01912345-6789-7abc-8def-0123456789ab")
	require.NoError(t, err)
	require.Equal(t, tenant.String(), lease.Tenant)
	require.Equal(t, provider.Uuid, lease.ProviderUuid)
	require.Equal(t, types.LEASE_STATE_ACTIVE, lease.State)

	// Verify credit account was imported
	ca, err := k.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)
	require.Equal(t, creditAddr.String(), ca.CreditAddress)
}

func TestInitGenesis_InvalidProviderReference(t *testing.T) {
	f := initFixture(t)

	k := f.App.BillingKeeper

	tenant := f.TestAccs[0]
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)

	// Create genesis with a lease referencing a non-existent provider
	genesisState := &types.GenesisState{
		Params: types.DefaultParams(),
		Leases: []types.Lease{
			{
				Uuid:         "01912345-6789-7abc-8def-0123456789ab",
				Tenant:       tenant.String(),
				ProviderUuid: "01912345-6789-7abc-8def-nonexistent1", // Does not exist
				Items: []types.LeaseItem{
					{
						SkuUuid:     "01912345-6789-7abc-8def-000000000001",
						Quantity:    1,
						LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(100)),
					},
				},
				State:         types.LEASE_STATE_ACTIVE,
				CreatedAt:     f.Ctx.BlockTime(),
				LastSettledAt: f.Ctx.BlockTime(),
			},
		},
		CreditAccounts: []types.CreditAccount{
			{
				Tenant:        tenant.String(),
				CreditAddress: creditAddr.String(),
			},
		},
	}

	err = k.InitGenesis(f.Ctx, genesisState)
	require.Error(t, err)
	require.Contains(t, err.Error(), "non-existent provider")
}

func TestInitGenesis_InvalidSKUReference(t *testing.T) {
	f := initFixture(t)

	k := f.App.BillingKeeper

	// Create a valid provider but no SKU
	providerAddr := f.TestAccs[1]
	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())

	tenant := f.TestAccs[0]
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)

	// Create genesis with a lease referencing a non-existent SKU
	genesisState := &types.GenesisState{
		Params: types.DefaultParams(),
		Leases: []types.Lease{
			{
				Uuid:         "01912345-6789-7abc-8def-0123456789ab",
				Tenant:       tenant.String(),
				ProviderUuid: provider.Uuid, // Valid provider
				Items: []types.LeaseItem{
					{
						SkuUuid:     "01912345-6789-7abc-8def-nonexistent2", // Does not exist
						Quantity:    1,
						LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(100)),
					},
				},
				State:         types.LEASE_STATE_ACTIVE,
				CreatedAt:     f.Ctx.BlockTime(),
				LastSettledAt: f.Ctx.BlockTime(),
			},
		},
		CreditAccounts: []types.CreditAccount{
			{
				Tenant:        tenant.String(),
				CreditAddress: creditAddr.String(),
			},
		},
	}

	err = k.InitGenesis(f.Ctx, genesisState)
	require.Error(t, err)
	require.Contains(t, err.Error(), "non-existent SKU")
}

func TestInitGenesis_SKUProviderMismatch(t *testing.T) {
	f := initFixture(t)

	k := f.App.BillingKeeper

	// Create two providers
	provider1Addr := f.TestAccs[1]
	provider1 := f.createTestProvider(t, provider1Addr.String(), provider1Addr.String())

	provider2Addr := f.TestAccs[2]
	provider2 := f.createTestProvider(t, provider2Addr.String(), provider2Addr.String())

	// Create a SKU that belongs to provider 1
	sku := f.createTestSKU(t, provider1.Uuid, 3600)

	tenant := f.TestAccs[0]
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)

	// Create genesis with a lease that references provider 2 but uses provider 1's SKU
	genesisState := &types.GenesisState{
		Params: types.DefaultParams(),
		Leases: []types.Lease{
			{
				Uuid:         "01912345-6789-7abc-8def-0123456789ab",
				Tenant:       tenant.String(),
				ProviderUuid: provider2.Uuid, // Provider 2
				Items: []types.LeaseItem{
					{
						SkuUuid:     sku.Uuid, // But SKU belongs to Provider 1
						Quantity:    1,
						LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(100)),
					},
				},
				State:         types.LEASE_STATE_ACTIVE,
				CreatedAt:     f.Ctx.BlockTime(),
				LastSettledAt: f.Ctx.BlockTime(),
			},
		},
		CreditAccounts: []types.CreditAccount{
			{
				Tenant:        tenant.String(),
				CreditAddress: creditAddr.String(),
			},
		},
	}

	err = k.InitGenesis(f.Ctx, genesisState)
	require.Error(t, err)
	require.Contains(t, err.Error(), "belongs to provider")
	require.Contains(t, err.Error(), provider1.Uuid) // The actual provider
	require.Contains(t, err.Error(), provider2.Uuid) // The claimed provider
}

func TestExportGenesis(t *testing.T) {
	f := initFixture(t)

	k := f.App.BillingKeeper

	tenant := f.TestAccs[0]
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)

	denom := testDenom
	balance := sdk.NewCoin(denom, sdkmath.NewInt(500000))

	// Fund the credit address
	f.fundAccount(t, creditAddr, sdk.NewCoins(balance))

	// Create a credit account
	ca := types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	}
	err = k.SetCreditAccount(f.Ctx, ca)
	require.NoError(t, err)

	// Create a lease
	lease := types.Lease{
		Uuid:         "lease-1",
		Tenant:       tenant.String(),
		ProviderUuid: "provider-1",
		Items: []types.LeaseItem{
			{
				SkuUuid:     "sku-1",
				Quantity:    1,
				LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(50)),
			},
		},
		State:     types.LEASE_STATE_ACTIVE,
		CreatedAt: f.Ctx.BlockTime(),
	}
	err = k.SetLease(f.Ctx, lease)
	require.NoError(t, err)

	// Export genesis
	genState := k.ExportGenesis(f.Ctx)

	require.NotNil(t, genState)
	// Compare params fields individually (nil slice vs empty slice comparison issue)
	require.Equal(t, types.DefaultMaxLeasesPerTenant, genState.Params.MaxLeasesPerTenant)
	require.Empty(t, genState.Params.AllowedList)
	require.Len(t, genState.Leases, 1)
	require.Equal(t, "lease-1", genState.Leases[0].Uuid)
	require.Len(t, genState.CreditAccounts, 1)
	require.Equal(t, tenant.String(), genState.CreditAccounts[0].Tenant)
}

func TestGetSetParams(t *testing.T) {
	f := initFixture(t)

	k := f.App.BillingKeeper

	// Get default params
	params, err := k.GetParams(f.Ctx)
	require.NoError(t, err)
	require.Equal(t, types.DefaultMaxLeasesPerTenant, params.MaxLeasesPerTenant)

	// Set new params
	newParams := types.NewParams(
		50,
		[]string{},
		20,
		3600,
		10,
		1800,
	)
	err = k.SetParams(f.Ctx, newParams)
	require.NoError(t, err)

	// Verify new params
	gotParams, err := k.GetParams(f.Ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(50), gotParams.MaxLeasesPerTenant)
	require.Equal(t, uint64(20), gotParams.MaxItemsPerLease)
	require.Equal(t, uint64(3600), gotParams.MinLeaseDuration)
	require.Equal(t, uint64(10), gotParams.MaxPendingLeasesPerTenant)
}

func TestCreditAddressDerivation(t *testing.T) {
	f := initFixture(t)

	tenant1 := f.TestAccs[0]
	tenant2 := f.TestAccs[1]

	// Derive credit addresses
	creditAddr1, err := types.DeriveCreditAddressFromBech32(tenant1.String())
	require.NoError(t, err)
	require.NotNil(t, creditAddr1)

	creditAddr2, err := types.DeriveCreditAddressFromBech32(tenant2.String())
	require.NoError(t, err)
	require.NotNil(t, creditAddr2)

	// Different tenants should have different credit addresses
	require.NotEqual(t, creditAddr1.String(), creditAddr2.String())

	// Same tenant should always derive the same credit address
	creditAddr1Again, err := types.DeriveCreditAddressFromBech32(tenant1.String())
	require.NoError(t, err)
	require.Equal(t, creditAddr1.String(), creditAddr1Again.String())

	// Credit address should be different from tenant address
	require.NotEqual(t, tenant1.String(), creditAddr1.String())
}

func TestGetLease(t *testing.T) {
	f := initFixture(t)

	k := f.App.BillingKeeper

	tenant := f.TestAccs[0]

	// Test lease not found
	_, err := k.GetLease(f.Ctx, "lease-1")
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrLeaseNotFound)

	// Create a lease
	lease := types.Lease{
		Uuid:         "lease-1",
		Tenant:       tenant.String(),
		ProviderUuid: "provider-1",
		Items: []types.LeaseItem{
			{
				SkuUuid:     "sku-1",
				Quantity:    1,
				LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(100)),
			},
		},
		State:     types.LEASE_STATE_ACTIVE,
		CreatedAt: f.Ctx.BlockTime(),
	}
	err = k.SetLease(f.Ctx, lease)
	require.NoError(t, err)

	// Get lease
	gotLease, err := k.GetLease(f.Ctx, "lease-1")
	require.NoError(t, err)
	require.Equal(t, lease.Uuid, gotLease.Uuid)
	require.Equal(t, lease.Tenant, gotLease.Tenant)
	require.Equal(t, lease.ProviderUuid, gotLease.ProviderUuid)
	require.Equal(t, lease.State, gotLease.State)
}

func TestGetAllLeases(t *testing.T) {
	f := initFixture(t)

	k := f.App.BillingKeeper

	// Initially empty
	leases, err := k.GetAllLeases(f.Ctx)
	require.NoError(t, err)
	require.Len(t, leases, 0)

	// Add multiple leases
	for i := uint64(1); i <= 3; i++ {
		lease := types.Lease{
			Uuid:         fmt.Sprintf("lease-%d", i),
			Tenant:       f.TestAccs[i-1].String(),
			ProviderUuid: "provider-1",
			Items: []types.LeaseItem{
				{
					SkuUuid:     fmt.Sprintf("sku-%d", i),
					Quantity:    1,
					LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(100)),
				},
			},
			State:     types.LEASE_STATE_ACTIVE,
			CreatedAt: f.Ctx.BlockTime(),
		}
		err := k.SetLease(f.Ctx, lease)
		require.NoError(t, err)
	}

	// Get all leases
	leases, err = k.GetAllLeases(f.Ctx)
	require.NoError(t, err)
	require.Len(t, leases, 3)
}

func TestGetLeasesByTenant(t *testing.T) {
	f := initFixture(t)

	k := f.App.BillingKeeper

	tenant1 := f.TestAccs[0]
	tenant2 := f.TestAccs[1]

	// Create leases for tenant1
	for i := uint64(1); i <= 3; i++ {
		lease := types.Lease{
			Uuid:         fmt.Sprintf("lease-%d", i),
			Tenant:       tenant1.String(),
			ProviderUuid: "provider-1",
			Items: []types.LeaseItem{
				{
					SkuUuid:     fmt.Sprintf("sku-%d", i),
					Quantity:    1,
					LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(100)),
				},
			},
			State:     types.LEASE_STATE_ACTIVE,
			CreatedAt: f.Ctx.BlockTime(),
		}
		err := k.SetLease(f.Ctx, lease)
		require.NoError(t, err)
	}

	// Create leases for tenant2
	for i := uint64(4); i <= 5; i++ {
		lease := types.Lease{
			Uuid:         fmt.Sprintf("lease-%d", i),
			Tenant:       tenant2.String(),
			ProviderUuid: "provider-2",
			Items: []types.LeaseItem{
				{
					SkuUuid:     fmt.Sprintf("sku-%d", i),
					Quantity:    1,
					LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(100)),
				},
			},
			State:     types.LEASE_STATE_ACTIVE,
			CreatedAt: f.Ctx.BlockTime(),
		}
		err := k.SetLease(f.Ctx, lease)
		require.NoError(t, err)
	}

	// Get leases by tenant1
	tenant1Leases, err := k.GetLeasesByTenant(f.Ctx, tenant1.String())
	require.NoError(t, err)
	require.Len(t, tenant1Leases, 3)

	// Get leases by tenant2
	tenant2Leases, err := k.GetLeasesByTenant(f.Ctx, tenant2.String())
	require.NoError(t, err)
	require.Len(t, tenant2Leases, 2)

	// Get leases by unknown tenant
	unknownLeases, err := k.GetLeasesByTenant(f.Ctx, f.TestAccs[3].String())
	require.NoError(t, err)
	require.Len(t, unknownLeases, 0)
}

func TestGetLeasesByProviderID(t *testing.T) {
	f := initFixture(t)

	k := f.App.BillingKeeper

	// Create leases for provider 1
	for i := uint64(1); i <= 4; i++ {
		lease := types.Lease{
			Uuid:         fmt.Sprintf("lease-%d", i),
			Tenant:       f.TestAccs[0].String(),
			ProviderUuid: "provider-1",
			Items: []types.LeaseItem{
				{
					SkuUuid:     fmt.Sprintf("sku-%d", i),
					Quantity:    1,
					LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(100)),
				},
			},
			State:     types.LEASE_STATE_ACTIVE,
			CreatedAt: f.Ctx.BlockTime(),
		}
		err := k.SetLease(f.Ctx, lease)
		require.NoError(t, err)
	}

	// Create leases for provider 2
	for i := uint64(5); i <= 6; i++ {
		lease := types.Lease{
			Uuid:         fmt.Sprintf("lease-%d", i),
			Tenant:       f.TestAccs[1].String(),
			ProviderUuid: "provider-2",
			Items: []types.LeaseItem{
				{
					SkuUuid:     fmt.Sprintf("sku-%d", i),
					Quantity:    1,
					LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(100)),
				},
			},
			State:     types.LEASE_STATE_ACTIVE,
			CreatedAt: f.Ctx.BlockTime(),
		}
		err := k.SetLease(f.Ctx, lease)
		require.NoError(t, err)
	}

	// Get leases by provider 1
	provider1Leases, err := k.GetLeasesByProviderUUID(f.Ctx, "provider-1")
	require.NoError(t, err)
	require.Len(t, provider1Leases, 4)

	// Get leases by provider 2
	provider2Leases, err := k.GetLeasesByProviderUUID(f.Ctx, "provider-2")
	require.NoError(t, err)
	require.Len(t, provider2Leases, 2)

	// Get leases by unknown provider
	unknownLeases, err := k.GetLeasesByProviderUUID(f.Ctx, "provider-999")
	require.NoError(t, err)
	require.Len(t, unknownLeases, 0)
}

func TestCountActiveLeasesByTenant(t *testing.T) {
	f := initFixture(t)

	k := f.App.BillingKeeper

	tenant := f.TestAccs[0]

	// Initially 0
	count, err := k.CountActiveLeasesByTenant(f.Ctx, tenant.String())
	require.NoError(t, err)
	require.Equal(t, uint64(0), count)

	// Create 3 active leases and 2 inactive leases
	for i := uint64(1); i <= 3; i++ {
		lease := types.Lease{
			Uuid:         fmt.Sprintf("lease-%d", i),
			Tenant:       tenant.String(),
			ProviderUuid: "provider-1",
			Items: []types.LeaseItem{
				{
					SkuUuid:     fmt.Sprintf("sku-%d", i),
					Quantity:    1,
					LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(100)),
				},
			},
			State:     types.LEASE_STATE_ACTIVE,
			CreatedAt: f.Ctx.BlockTime(),
		}
		err := k.SetLease(f.Ctx, lease)
		require.NoError(t, err)
	}

	closedAt := f.Ctx.BlockTime()
	for i := uint64(4); i <= 5; i++ {
		lease := types.Lease{
			Uuid:         fmt.Sprintf("lease-%d", i),
			Tenant:       tenant.String(),
			ProviderUuid: "provider-1",
			Items: []types.LeaseItem{
				{
					SkuUuid:     fmt.Sprintf("sku-%d", i),
					Quantity:    1,
					LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(100)),
				},
			},
			State:     types.LEASE_STATE_CLOSED,
			CreatedAt: f.Ctx.BlockTime(),
			ClosedAt:  &closedAt,
		}
		err := k.SetLease(f.Ctx, lease)
		require.NoError(t, err)
	}

	// Count should only include active leases
	count, err = k.CountActiveLeasesByTenant(f.Ctx, tenant.String())
	require.NoError(t, err)
	require.Equal(t, uint64(3), count)
}

func TestCreditAccountOperations(t *testing.T) {
	f := initFixture(t)

	k := f.App.BillingKeeper

	tenant := f.TestAccs[0]
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)

	// Test credit account not found
	_, err = k.GetCreditAccount(f.Ctx, tenant.String())
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrCreditAccountNotFound)

	// Create credit account
	ca := types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	}
	err = k.SetCreditAccount(f.Ctx, ca)
	require.NoError(t, err)

	// Get credit account
	gotCA, err := k.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)
	require.Equal(t, ca.Tenant, gotCA.Tenant)
	require.Equal(t, ca.CreditAddress, gotCA.CreditAddress)
}

// TestActiveLeaseCountAccuracy verifies that ActiveLeaseCount tracking is accurate
// through the lease lifecycle: creation, acknowledgment, close, and batch operations.
func TestActiveLeaseCountAccuracy(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	payoutAddr := f.TestAccs[2]
	denom := testDenom

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Fund tenant's credit account
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	fundAmount := sdk.NewCoin(denom, sdkmath.NewInt(100000000))
	f.fundAccount(t, creditAddr, sdk.NewCoins(fundAmount))

	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	t.Run("initial count is zero", func(t *testing.T) {
		creditAccount, err := f.App.BillingKeeper.GetCreditAccount(f.Ctx, tenant.String())
		require.NoError(t, err)
		require.Equal(t, uint64(0), creditAccount.ActiveLeaseCount)
		require.Equal(t, uint64(0), creditAccount.PendingLeaseCount)
	})

	// Create multiple leases
	var leaseUUIDs []string
	for i := 0; i < 3; i++ {
		resp, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
			Tenant: tenant.String(),
			Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
		})
		require.NoError(t, err)
		leaseUUIDs = append(leaseUUIDs, resp.LeaseUuid)
	}

	t.Run("pending count increments on create", func(t *testing.T) {
		creditAccount, err := f.App.BillingKeeper.GetCreditAccount(f.Ctx, tenant.String())
		require.NoError(t, err)
		require.Equal(t, uint64(0), creditAccount.ActiveLeaseCount, "active count should still be 0")
		require.Equal(t, uint64(3), creditAccount.PendingLeaseCount, "pending count should be 3")
	})

	// Acknowledge all leases
	for _, uuid := range leaseUUIDs {
		_, err := msgServer.AcknowledgeLease(f.Ctx, &types.MsgAcknowledgeLease{
			Sender:     providerAddr.String(),
			LeaseUuids: []string{uuid},
		})
		require.NoError(t, err)
	}

	t.Run("active count increments on acknowledge, pending decrements", func(t *testing.T) {
		creditAccount, err := f.App.BillingKeeper.GetCreditAccount(f.Ctx, tenant.String())
		require.NoError(t, err)
		require.Equal(t, uint64(3), creditAccount.ActiveLeaseCount, "active count should be 3")
		require.Equal(t, uint64(0), creditAccount.PendingLeaseCount, "pending count should be 0")
	})

	// Close one lease
	_, err = msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
		Sender:     tenant.String(),
		LeaseUuids: []string{leaseUUIDs[0]},
	})
	require.NoError(t, err)

	t.Run("active count decrements on close", func(t *testing.T) {
		creditAccount, err := f.App.BillingKeeper.GetCreditAccount(f.Ctx, tenant.String())
		require.NoError(t, err)
		require.Equal(t, uint64(2), creditAccount.ActiveLeaseCount, "active count should be 2")
	})

	// Batch close remaining leases
	_, err = msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
		Sender:     tenant.String(),
		LeaseUuids: []string{leaseUUIDs[1], leaseUUIDs[2]},
	})
	require.NoError(t, err)

	t.Run("batch close decrements correctly", func(t *testing.T) {
		creditAccount, err := f.App.BillingKeeper.GetCreditAccount(f.Ctx, tenant.String())
		require.NoError(t, err)
		require.Equal(t, uint64(0), creditAccount.ActiveLeaseCount, "active count should be 0 after batch close")
	})

	t.Run("count never goes negative", func(t *testing.T) {
		// Manually create a credit account with count=0 and verify decrement doesn't go negative
		creditAccount, err := f.App.BillingKeeper.GetCreditAccount(f.Ctx, tenant.String())
		require.NoError(t, err)
		require.Equal(t, uint64(0), creditAccount.ActiveLeaseCount)

		// The DecrementActiveLeaseCount function has a guard against going negative
		f.App.BillingKeeper.DecrementActiveLeaseCount(&creditAccount, "non-existent-lease")
		require.Equal(t, uint64(0), creditAccount.ActiveLeaseCount, "count should not go negative")
	})
}

func TestGetAllCreditAccounts(t *testing.T) {
	f := initFixture(t)

	k := f.App.BillingKeeper

	// Initially empty
	accounts, err := k.GetAllCreditAccounts(f.Ctx)
	require.NoError(t, err)
	require.Len(t, accounts, 0)

	// Create multiple credit accounts
	for i := 0; i < 3; i++ {
		tenant := f.TestAccs[i]
		creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
		require.NoError(t, err)

		ca := types.CreditAccount{
			Tenant:        tenant.String(),
			CreditAddress: creditAddr.String(),
		}
		err = k.SetCreditAccount(f.Ctx, ca)
		require.NoError(t, err)
	}

	// Get all credit accounts
	accounts, err = k.GetAllCreditAccounts(f.Ctx)
	require.NoError(t, err)
	require.Len(t, accounts, 3)
}

func TestGetCreditBalance(t *testing.T) {
	f := initFixture(t)

	k := f.App.BillingKeeper

	tenant := f.TestAccs[0]
	denom := testDenom

	// Get credit balance when no funds
	balance, err := k.GetCreditBalance(f.Ctx, tenant.String(), denom)
	require.NoError(t, err)
	require.True(t, balance.IsZero())

	// Fund the credit address
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)

	fundAmount := sdk.NewCoin(denom, sdkmath.NewInt(5000000))
	f.fundAccount(t, creditAddr, sdk.NewCoins(fundAmount))

	// Get credit balance
	balance, err = k.GetCreditBalance(f.Ctx, tenant.String(), denom)
	require.NoError(t, err)
	require.Equal(t, fundAmount, balance)
}

func TestParamsValidation(t *testing.T) {
	tests := []struct {
		name      string
		params    types.Params
		expectErr bool
	}{
		{
			name:      "valid default params",
			params:    types.DefaultParams(),
			expectErr: false,
		},
		{
			name: "zero max leases per tenant",
			params: types.NewParams(
				0,
				[]string{},
				20,
				3600,
				10,
				1800,
			),
			expectErr: true,
		},
		{
			name: "zero max items per lease",
			params: types.NewParams(
				100,
				[]string{},
				0,
				3600,
				10,
				1800,
			),
			expectErr: true,
		},
		{
			name: "zero min lease duration",
			params: types.NewParams(
				100,
				[]string{},
				20,
				0,
				10,
				1800,
			),
			expectErr: true,
		},
		{
			name: "valid custom params",
			params: types.NewParams(
				50,
				[]string{},
				20,
				3600,
				10,
				1800,
			),
			expectErr: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.params.Validate()
			if tc.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestGenesisValidation(t *testing.T) {
	f := initFixture(t)

	tenant := f.TestAccs[0]
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)

	closedAt := f.Ctx.BlockTime()

	// Valid UUIDs for testing (use local variables that can be different from constants)
	leaseUUID1 := testLeaseUUID1
	leaseUUID2 := testLeaseUUID2
	providerUUID := testProviderUUID
	skuUUID1 := testSKUUUID
	skuUUID2 := "01912345-6789-7abc-8def-0123456789af"

	tests := []struct {
		name      string
		genesis   types.GenesisState
		expectErr bool
	}{
		{
			name:      "valid default genesis",
			genesis:   *types.DefaultGenesis(),
			expectErr: false,
		},
		{
			name: "valid genesis with lease",
			genesis: types.GenesisState{
				Params: types.DefaultParams(),
				Leases: []types.Lease{
					{
						Uuid:         leaseUUID1,
						Tenant:       tenant.String(),
						ProviderUuid: providerUUID,
						Items: []types.LeaseItem{
							{
								SkuUuid:     skuUUID1,
								Quantity:    1,
								LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(100)),
							},
						},
						State:     types.LEASE_STATE_ACTIVE,
						CreatedAt: f.Ctx.BlockTime(),
					},
				},
				CreditAccounts: []types.CreditAccount{
					{
						Tenant:           tenant.String(),
						CreditAddress:    creditAddr.String(),
						ActiveLeaseCount: 1, // 1 ACTIVE lease
						// Reservation: 100 * 1 * 3600 = 360000 (active lease needs reservation)
						ReservedAmounts: sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(360000))),
					},
				},
				LeaseSequence: 1,
			},
			expectErr: false,
		},
		{
			name: "duplicate lease uuid",
			genesis: types.GenesisState{
				Params: types.DefaultParams(),
				Leases: []types.Lease{
					{
						Uuid:         leaseUUID1,
						Tenant:       tenant.String(),
						ProviderUuid: providerUUID,
						Items: []types.LeaseItem{
							{
								SkuUuid:     skuUUID1,
								Quantity:    1,
								LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(100)),
							},
						},
						State:     types.LEASE_STATE_ACTIVE,
						CreatedAt: f.Ctx.BlockTime(),
					},
					{
						Uuid:         leaseUUID1, // Duplicate UUID
						Tenant:       tenant.String(),
						ProviderUuid: providerUUID,
						Items: []types.LeaseItem{
							{
								SkuUuid:     skuUUID2,
								Quantity:    1,
								LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(100)),
							},
						},
						State:     types.LEASE_STATE_ACTIVE,
						CreatedAt: f.Ctx.BlockTime(),
					},
				},
			},
			expectErr: true,
		},
		{
			name: "invalid lease uuid format",
			genesis: types.GenesisState{
				Params: types.DefaultParams(),
				Leases: []types.Lease{
					{
						Uuid:         "invalid-uuid",
						Tenant:       tenant.String(),
						ProviderUuid: providerUUID,
						Items: []types.LeaseItem{
							{
								SkuUuid:     skuUUID1,
								Quantity:    1,
								LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(100)),
							},
						},
						State:     types.LEASE_STATE_ACTIVE,
						CreatedAt: f.Ctx.BlockTime(),
					},
				},
			},
			expectErr: true,
		},
		{
			name: "inactive lease without closed_at",
			genesis: types.GenesisState{
				Params: types.DefaultParams(),
				Leases: []types.Lease{
					{
						Uuid:         leaseUUID1,
						Tenant:       tenant.String(),
						ProviderUuid: providerUUID,
						Items: []types.LeaseItem{
							{
								SkuUuid:     skuUUID1,
								Quantity:    1,
								LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(100)),
							},
						},
						State:     types.LEASE_STATE_CLOSED,
						CreatedAt: f.Ctx.BlockTime(),
						// Missing ClosedAt
					},
				},
			},
			expectErr: true,
		},
		{
			name: "valid inactive lease with closed_at",
			genesis: types.GenesisState{
				Params: types.DefaultParams(),
				Leases: []types.Lease{
					{
						Uuid:         leaseUUID2,
						Tenant:       tenant.String(),
						ProviderUuid: providerUUID,
						Items: []types.LeaseItem{
							{
								SkuUuid:     skuUUID1,
								Quantity:    1,
								LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(100)),
							},
						},
						State:     types.LEASE_STATE_CLOSED,
						CreatedAt: f.Ctx.BlockTime(),
						ClosedAt:  &closedAt,
					},
				},
				LeaseSequence: 1,
			},
			expectErr: false,
		},
		{
			name: "duplicate credit account",
			genesis: types.GenesisState{
				Params: types.DefaultParams(),
				CreditAccounts: []types.CreditAccount{
					{
						Tenant:        tenant.String(),
						CreditAddress: creditAddr.String(),
					},
					{
						Tenant:        tenant.String(),
						CreditAddress: creditAddr.String(),
					},
				},
			},
			expectErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.genesis.Validate()
			if tc.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestMsgValidation(t *testing.T) {
	_, _, validAddr := testdata.KeyTestPubAddr()

	// Valid UUIDs for testing
	validSkuUUID := testLeaseUUID1
	validLeaseUUID := testLeaseUUID2
	validProviderUUID := testProviderUUID

	tests := []struct {
		name      string
		msg       sdk.Msg
		expectErr bool
	}{
		{
			name: "valid MsgFundCredit",
			msg: &types.MsgFundCredit{
				Sender: validAddr.String(),
				Tenant: validAddr.String(),
				Amount: sdk.NewCoin(testDenom, sdkmath.NewInt(1000000)),
			},
			expectErr: false,
		},
		{
			name: "MsgFundCredit invalid sender",
			msg: &types.MsgFundCredit{
				Sender: "invalid",
				Tenant: validAddr.String(),
				Amount: sdk.NewCoin(testDenom, sdkmath.NewInt(1000000)),
			},
			expectErr: true,
		},
		{
			name: "MsgFundCredit invalid tenant",
			msg: &types.MsgFundCredit{
				Sender: validAddr.String(),
				Tenant: "invalid",
				Amount: sdk.NewCoin(testDenom, sdkmath.NewInt(1000000)),
			},
			expectErr: true,
		},
		{
			name: "MsgFundCredit zero amount",
			msg: &types.MsgFundCredit{
				Sender: validAddr.String(),
				Tenant: validAddr.String(),
				Amount: sdk.NewCoin(testDenom, sdkmath.ZeroInt()),
			},
			expectErr: true,
		},
		{
			name: "valid MsgCreateLease",
			msg: &types.MsgCreateLease{
				Tenant: validAddr.String(),
				Items: []types.LeaseItemInput{
					{
						SkuUuid:  validSkuUUID,
						Quantity: 1,
					},
				},
			},
			expectErr: false,
		},
		{
			name: "MsgCreateLease invalid tenant",
			msg: &types.MsgCreateLease{
				Tenant: "invalid",
				Items: []types.LeaseItemInput{
					{
						SkuUuid:  validSkuUUID,
						Quantity: 1,
					},
				},
			},
			expectErr: true,
		},
		{
			name: "MsgCreateLease empty items",
			msg: &types.MsgCreateLease{
				Tenant: validAddr.String(),
				Items:  []types.LeaseItemInput{},
			},
			expectErr: true,
		},
		{
			name: "MsgCreateLease empty sku_uuid",
			msg: &types.MsgCreateLease{
				Tenant: validAddr.String(),
				Items: []types.LeaseItemInput{
					{
						SkuUuid:  "",
						Quantity: 1,
					},
				},
			},
			expectErr: true,
		},
		{
			name: "MsgCreateLease zero quantity",
			msg: &types.MsgCreateLease{
				Tenant: validAddr.String(),
				Items: []types.LeaseItemInput{
					{
						SkuUuid:  validSkuUUID,
						Quantity: 0,
					},
				},
			},
			expectErr: true,
		},
		{
			name: "MsgCreateLease duplicate SKU",
			msg: &types.MsgCreateLease{
				Tenant: validAddr.String(),
				Items: []types.LeaseItemInput{
					{
						SkuUuid:  validSkuUUID,
						Quantity: 1,
					},
					{
						SkuUuid:  validSkuUUID,
						Quantity: 2,
					},
				},
			},
			expectErr: true,
		},
		{
			name: "valid MsgCloseLease",
			msg: &types.MsgCloseLease{
				Sender:     validAddr.String(),
				LeaseUuids: []string{validLeaseUUID},
			},
			expectErr: false,
		},
		{
			name: "MsgCloseLease empty lease_uuids",
			msg: &types.MsgCloseLease{
				Sender:     validAddr.String(),
				LeaseUuids: []string{},
			},
			expectErr: true,
		},
		{
			name: "valid MsgWithdraw with lease_uuids",
			msg: &types.MsgWithdraw{
				Sender:     validAddr.String(),
				LeaseUuids: []string{validLeaseUUID},
			},
			expectErr: false,
		},
		{
			name: "valid MsgWithdraw with provider_uuid",
			msg: &types.MsgWithdraw{
				Sender:       validAddr.String(),
				ProviderUuid: validProviderUUID,
			},
			expectErr: false,
		},
		{
			name: "MsgWithdraw neither mode specified",
			msg: &types.MsgWithdraw{
				Sender:     validAddr.String(),
				LeaseUuids: []string{},
			},
			expectErr: true,
		},
		{
			name: "MsgWithdraw both modes specified",
			msg: &types.MsgWithdraw{
				Sender:       validAddr.String(),
				LeaseUuids:   []string{validLeaseUUID},
				ProviderUuid: validProviderUUID,
			},
			expectErr: true,
		},
		{
			name: "valid MsgUpdateParams",
			msg: &types.MsgUpdateParams{
				Authority: validAddr.String(),
				Params:    types.DefaultParams(),
			},
			expectErr: false,
		},
		{
			name: "MsgUpdateParams invalid authority",
			msg: &types.MsgUpdateParams{
				Authority: "invalid",
				Params:    types.DefaultParams(),
			},
			expectErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var err error
			switch msg := tc.msg.(type) {
			case *types.MsgFundCredit:
				err = msg.ValidateBasic()
			case *types.MsgCreateLease:
				err = msg.ValidateBasic()
			case *types.MsgCloseLease:
				err = msg.ValidateBasic()
			case *types.MsgWithdraw:
				err = msg.ValidateBasic()
			case *types.MsgUpdateParams:
				err = msg.ValidateBasic()
			}
			if tc.expectErr {
				require.Error(t, err, "expected error for %s", tc.name)
			} else {
				require.NoError(t, err, "unexpected error for %s", tc.name)
			}
		})
	}
}

func TestAuthority(t *testing.T) {
	f := initFixture(t)

	k := f.App.BillingKeeper

	// Get authority
	authority := k.GetAuthority()
	require.Equal(t, f.Authority.String(), authority)

	// Set new authority
	newAuthority := f.TestAccs[0].String()
	k.SetAuthority(newAuthority)
	require.Equal(t, newAuthority, k.GetAuthority())
}

func TestBillingKeeperIntegration(t *testing.T) {
	f := initFixture(t)

	// Verify billing keeper has access to SKU and bank keepers
	require.NotNil(t, f.App.BillingKeeper)

	// Create a provider and SKU using SKU keeper
	provider := f.createTestProvider(t, f.TestAccs[0].String(), f.TestAccs[1].String())
	require.NotEmpty(t, provider.Uuid)

	sku := f.createTestSKU(t, provider.Uuid, 100)
	require.NotEmpty(t, sku.Uuid)

	// Verify we can look them up via SKU keeper
	gotProvider, err := f.App.SKUKeeper.GetProvider(f.Ctx, provider.Uuid)
	require.NoError(t, err)
	require.Equal(t, provider.Address, gotProvider.Address)

	gotSKU, err := f.App.SKUKeeper.GetSKU(f.Ctx, sku.Uuid)
	require.NoError(t, err)
	require.Equal(t, sku.Name, gotSKU.Name)

	// Register denomination for transfers
	f.App.BankKeeper.SetDenomMetaData(f.Ctx, banktypes.Metadata{
		Base:        testDenom,
		Display:     "pwr",
		Description: "Test PWR token",
	})
}

func TestShouldAutoCloseLease(t *testing.T) {
	f := initFixture(t)

	k := f.App.BillingKeeper

	// Create a provider
	provider := f.createTestProvider(t, f.TestAccs[0].String(), f.TestAccs[1].String())

	tenant := f.TestAccs[2]
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)

	// Create credit account with zero balance
	ca := types.CreditAccount{
		Tenant:           tenant.String(),
		CreditAddress:    creditAddr.String(),
		ActiveLeaseCount: 1,
	}
	err = k.SetCreditAccount(f.Ctx, ca)
	require.NoError(t, err)

	// Create an active lease with zero credit balance
	lease := types.Lease{
		Uuid:          "lease-1",
		Tenant:        tenant.String(),
		ProviderUuid:  provider.Uuid,
		Items:         []types.LeaseItem{{SkuUuid: "sku-1", Quantity: 1, LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(100))}},
		State:         types.LEASE_STATE_ACTIVE,
		CreatedAt:     f.Ctx.BlockTime(),
		LastSettledAt: f.Ctx.BlockTime(),
	}
	err = k.SetLease(f.Ctx, lease)
	require.NoError(t, err)

	// Run ShouldAutoCloseLease - should return true (lease should be closed due to zero balance)
	shouldClose, closeTime, err := k.ShouldAutoCloseLease(f.Ctx, &lease)
	require.NoError(t, err)
	require.True(t, shouldClose)
	require.Equal(t, f.Ctx.BlockTime(), closeTime)

	// Verify lease state was NOT modified (function only checks, doesn't modify)
	require.Equal(t, types.LEASE_STATE_ACTIVE, lease.State)
	require.Nil(t, lease.ClosedAt)

	// Verify credit account was NOT modified
	updatedCA, err := k.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)
	require.Equal(t, uint64(1), updatedCA.ActiveLeaseCount)
}

func TestShouldAutoCloseLease_WithBalance(t *testing.T) {
	f := initFixture(t)

	k := f.App.BillingKeeper

	// Create a provider
	provider := f.createTestProvider(t, f.TestAccs[0].String(), f.TestAccs[1].String())

	tenant := f.TestAccs[2]
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)

	// Fund the credit account
	denom := testDenom
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(denom, sdkmath.NewInt(10000000))))

	// Create credit account with balance
	ca := types.CreditAccount{
		Tenant:           tenant.String(),
		CreditAddress:    creditAddr.String(),
		ActiveLeaseCount: 1,
	}
	err = k.SetCreditAccount(f.Ctx, ca)
	require.NoError(t, err)

	// Create an active lease
	lease := types.Lease{
		Uuid:          "lease-1",
		Tenant:        tenant.String(),
		ProviderUuid:  provider.Uuid,
		Items:         []types.LeaseItem{{SkuUuid: "sku-1", Quantity: 1, LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(100))}},
		State:         types.LEASE_STATE_ACTIVE,
		CreatedAt:     f.Ctx.BlockTime(),
		LastSettledAt: f.Ctx.BlockTime(),
	}
	err = k.SetLease(f.Ctx, lease)
	require.NoError(t, err)

	// Run ShouldAutoCloseLease - should NOT close the lease (has balance)
	shouldClose, _, err := k.ShouldAutoCloseLease(f.Ctx, &lease)
	require.NoError(t, err)
	require.False(t, shouldClose)

	// Verify lease is still active
	require.Equal(t, types.LEASE_STATE_ACTIVE, lease.State)
}

func TestShouldAutoCloseLease_InactiveLease(t *testing.T) {
	f := initFixture(t)

	k := f.App.BillingKeeper

	tenant := f.TestAccs[0]
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)

	// Create credit account
	ca := types.CreditAccount{
		Tenant:           tenant.String(),
		CreditAddress:    creditAddr.String(),
		ActiveLeaseCount: 0,
	}
	err = k.SetCreditAccount(f.Ctx, ca)
	require.NoError(t, err)

	// Create an inactive lease (already closed)
	closedAt := f.Ctx.BlockTime()
	lease := types.Lease{
		Uuid:          "lease-1",
		Tenant:        tenant.String(),
		ProviderUuid:  "provider-1",
		Items:         []types.LeaseItem{{SkuUuid: "sku-1", Quantity: 1, LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(100))}},
		State:         types.LEASE_STATE_CLOSED,
		CreatedAt:     f.Ctx.BlockTime(),
		LastSettledAt: f.Ctx.BlockTime(),
		ClosedAt:      &closedAt,
	}
	err = k.SetLease(f.Ctx, lease)
	require.NoError(t, err)

	// Run ShouldAutoCloseLease - should NOT try to close inactive leases
	shouldClose, _, err := k.ShouldAutoCloseLease(f.Ctx, &lease)
	require.NoError(t, err)
	require.False(t, shouldClose)
}

// TestShouldAutoCloseLease_FutureLastSettledAt tests that ShouldAutoCloseLease returns
// an error when LastSettledAt is in the future, indicating data corruption.
func TestShouldAutoCloseLease_FutureLastSettledAt(t *testing.T) {
	f := initFixture(t)

	k := f.App.BillingKeeper

	tenant := f.TestAccs[0]
	providerUUID := "test-provider"
	skuUUID := "test-sku"

	// Create a lease with LastSettledAt in the future (simulating data corruption)
	blockTime := f.Ctx.BlockTime()
	futureTime := blockTime.Add(time.Hour) // 1 hour in the future

	lease := types.Lease{
		Uuid:         "test-future-lease",
		Tenant:       tenant.String(),
		ProviderUuid: providerUUID,
		Items: []types.LeaseItem{
			{
				SkuUuid:     skuUUID,
				Quantity:    1,
				LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(3600)),
			},
		},
		State:         types.LEASE_STATE_ACTIVE,
		CreatedAt:     blockTime,
		LastSettledAt: futureTime, // Future timestamp - data corruption
	}
	err := k.SetLease(f.Ctx, lease)
	require.NoError(t, err)

	// Run ShouldAutoCloseLease - should return an error due to future LastSettledAt
	shouldClose, _, err := k.ShouldAutoCloseLease(f.Ctx, &lease)
	require.Error(t, err, "should return error when LastSettledAt is in the future")
	require.Contains(t, err.Error(), "in the future")
	require.Contains(t, err.Error(), lease.Uuid)
	require.False(t, shouldClose, "shouldClose should be false when error is returned")
}

func TestGetLeasesByState(t *testing.T) {
	f := initFixture(t)

	k := f.App.BillingKeeper

	tenant := f.TestAccs[0]

	// Create leases with different states
	states := []types.LeaseState{
		types.LEASE_STATE_PENDING,
		types.LEASE_STATE_PENDING,
		types.LEASE_STATE_ACTIVE,
		types.LEASE_STATE_ACTIVE,
		types.LEASE_STATE_ACTIVE,
		types.LEASE_STATE_CLOSED,
		types.LEASE_STATE_REJECTED,
		types.LEASE_STATE_EXPIRED,
	}

	for i, state := range states {
		lease := types.Lease{
			Uuid:         fmt.Sprintf("lease-%d", i),
			Tenant:       tenant.String(),
			ProviderUuid: "provider-1",
			Items: []types.LeaseItem{
				{
					SkuUuid:     fmt.Sprintf("sku-%d", i),
					Quantity:    1,
					LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(100)),
				},
			},
			State:         state,
			CreatedAt:     f.Ctx.BlockTime(),
			LastSettledAt: f.Ctx.BlockTime(),
		}
		err := k.SetLease(f.Ctx, lease)
		require.NoError(t, err)
	}

	// Test GetLeasesByState for each state
	pendingLeases, err := k.GetLeasesByState(f.Ctx, types.LEASE_STATE_PENDING)
	require.NoError(t, err)
	require.Len(t, pendingLeases, 2)
	for _, l := range pendingLeases {
		require.Equal(t, types.LEASE_STATE_PENDING, l.State)
	}

	activeLeases, err := k.GetLeasesByState(f.Ctx, types.LEASE_STATE_ACTIVE)
	require.NoError(t, err)
	require.Len(t, activeLeases, 3)
	for _, l := range activeLeases {
		require.Equal(t, types.LEASE_STATE_ACTIVE, l.State)
	}

	closedLeases, err := k.GetLeasesByState(f.Ctx, types.LEASE_STATE_CLOSED)
	require.NoError(t, err)
	require.Len(t, closedLeases, 1)

	rejectedLeases, err := k.GetLeasesByState(f.Ctx, types.LEASE_STATE_REJECTED)
	require.NoError(t, err)
	require.Len(t, rejectedLeases, 1)

	expiredLeases, err := k.GetLeasesByState(f.Ctx, types.LEASE_STATE_EXPIRED)
	require.NoError(t, err)
	require.Len(t, expiredLeases, 1)

	// Test GetPendingLeases helper
	pendingLeases2, err := k.GetPendingLeases(f.Ctx)
	require.NoError(t, err)
	require.Len(t, pendingLeases2, 2)

	// Test GetActiveLeases helper
	activeLeases2, err := k.GetActiveLeases(f.Ctx)
	require.NoError(t, err)
	require.Len(t, activeLeases2, 3)
}

func TestGetPendingLeasesByProvider(t *testing.T) {
	f := initFixture(t)

	k := f.App.BillingKeeper

	tenant := f.TestAccs[0]

	// Create pending and active leases for different providers
	testCases := []struct {
		uuid         string
		providerUUID string
		state        types.LeaseState
	}{
		{"lease-1", "provider-1", types.LEASE_STATE_PENDING},
		{"lease-2", "provider-1", types.LEASE_STATE_PENDING},
		{"lease-3", "provider-1", types.LEASE_STATE_ACTIVE},
		{"lease-4", "provider-2", types.LEASE_STATE_PENDING},
		{"lease-5", "provider-2", types.LEASE_STATE_ACTIVE},
		{"lease-6", "provider-2", types.LEASE_STATE_ACTIVE},
	}

	for _, tc := range testCases {
		lease := types.Lease{
			Uuid:         tc.uuid,
			Tenant:       tenant.String(),
			ProviderUuid: tc.providerUUID,
			Items: []types.LeaseItem{
				{
					SkuUuid:     "sku-1",
					Quantity:    1,
					LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(100)),
				},
			},
			State:         tc.state,
			CreatedAt:     f.Ctx.BlockTime(),
			LastSettledAt: f.Ctx.BlockTime(),
		}
		err := k.SetLease(f.Ctx, lease)
		require.NoError(t, err)
	}

	// Test GetPendingLeasesByProvider
	provider1Pending, err := k.GetPendingLeasesByProvider(f.Ctx, "provider-1")
	require.NoError(t, err)
	require.Len(t, provider1Pending, 2)
	for _, l := range provider1Pending {
		require.Equal(t, types.LEASE_STATE_PENDING, l.State)
		require.Equal(t, "provider-1", l.ProviderUuid)
	}

	provider2Pending, err := k.GetPendingLeasesByProvider(f.Ctx, "provider-2")
	require.NoError(t, err)
	require.Len(t, provider2Pending, 1)
	for _, l := range provider2Pending {
		require.Equal(t, types.LEASE_STATE_PENDING, l.State)
		require.Equal(t, "provider-2", l.ProviderUuid)
	}

	// Unknown provider should return empty
	unknownPending, err := k.GetPendingLeasesByProvider(f.Ctx, "provider-999")
	require.NoError(t, err)
	require.Len(t, unknownPending, 0)
}

// ============================================================================
// Critical Gap Tests: Genesis Round-Trip with Reservations
// ============================================================================

// TestGenesisExportImportWithReservations verifies that reserved_amounts are correctly
// preserved through a full genesis export/import cycle with PENDING and ACTIVE leases.
func TestGenesisExportImportWithReservations(t *testing.T) {
	f := initFixture(t)

	k := f.App.BillingKeeper
	msgServer := keeper.NewMsgServerImpl(k)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	payoutAddr := f.TestAccs[2]

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600) // 3600 per hour = 1 per second

	// Fund tenant's credit account with enough for multiple leases
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	fundAmount := sdk.NewCoin(testDenom, sdkmath.NewInt(100000000)) // 100M
	f.fundAccount(t, creditAddr, sdk.NewCoins(fundAmount))

	// Create credit account
	err = k.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Get params for reservation calculation
	params, err := k.GetParams(f.Ctx)
	require.NoError(t, err)

	// Create first lease (will remain PENDING)
	resp1, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items: []types.LeaseItemInput{
			{SkuUuid: sku.Uuid, Quantity: 2},
		},
	})
	require.NoError(t, err)
	pendingLeaseUUID := resp1.LeaseUuid

	// Create second lease (will be acknowledged to ACTIVE)
	resp2, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items: []types.LeaseItemInput{
			{SkuUuid: sku.Uuid, Quantity: 3},
		},
	})
	require.NoError(t, err)
	activeLeaseUUID := resp2.LeaseUuid

	// Acknowledge the second lease to make it ACTIVE
	_, err = msgServer.AcknowledgeLease(f.Ctx, &types.MsgAcknowledgeLease{
		Sender:     providerAddr.String(),
		LeaseUuids: []string{activeLeaseUUID},
	})
	require.NoError(t, err)

	// Verify credit account has correct reservations before export
	caBeforeExport, err := k.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)

	// Get leases to calculate expected reservations
	pendingLease, err := k.GetLease(f.Ctx, pendingLeaseUUID)
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_PENDING, pendingLease.State)

	activeLease, err := k.GetLease(f.Ctx, activeLeaseUUID)
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_ACTIVE, activeLease.State)

	// Calculate expected reservations (both PENDING and ACTIVE leases have reservations)
	pendingReservation := types.CalculateLeaseReservation(pendingLease.Items, params.MinLeaseDuration)
	activeReservation := types.CalculateLeaseReservation(activeLease.Items, params.MinLeaseDuration)
	expectedReservations := pendingReservation.Add(activeReservation...)

	require.True(t, expectedReservations.Equal(caBeforeExport.ReservedAmounts),
		"before export: expected reservations %s, got %s",
		expectedReservations.String(), caBeforeExport.ReservedAmounts.String())

	// Verify lease counts
	require.Equal(t, uint64(1), caBeforeExport.PendingLeaseCount)
	require.Equal(t, uint64(1), caBeforeExport.ActiveLeaseCount)

	// Export genesis
	exportedGenesis := k.ExportGenesis(f.Ctx)

	// Verify exported genesis has correct data
	require.Len(t, exportedGenesis.Leases, 2)
	require.Len(t, exportedGenesis.CreditAccounts, 1)

	exportedCA := exportedGenesis.CreditAccounts[0]
	require.True(t, expectedReservations.Equal(exportedCA.ReservedAmounts),
		"exported genesis: expected reservations %s, got %s",
		expectedReservations.String(), exportedCA.ReservedAmounts.String())
	require.Equal(t, uint64(1), exportedCA.PendingLeaseCount)
	require.Equal(t, uint64(1), exportedCA.ActiveLeaseCount)

	// Validate the exported genesis (this is what ValidateGenesis does)
	err = exportedGenesis.Validate()
	require.NoError(t, err, "exported genesis should be valid")

	// Create a fresh fixture for import
	f2 := initFixture(t)
	k2 := f2.App.BillingKeeper

	// Create the same provider and SKU in the new context (required for InitGenesis validation)
	// We directly set them with the same UUIDs from the first fixture
	err = f2.App.SKUKeeper.SetProvider(f2.Ctx, provider)
	require.NoError(t, err)
	err = f2.App.SKUKeeper.SetSKU(f2.Ctx, sku)
	require.NoError(t, err)

	// Fund the credit address in the new context
	f2.fundAccount(t, creditAddr, sdk.NewCoins(fundAmount))

	// Import genesis
	err = k2.InitGenesis(f2.Ctx, exportedGenesis)
	require.NoError(t, err)

	// Verify imported state
	caAfterImport, err := k2.GetCreditAccount(f2.Ctx, tenant.String())
	require.NoError(t, err)

	// Verify reservations are preserved
	require.True(t, expectedReservations.Equal(caAfterImport.ReservedAmounts),
		"after import: expected reservations %s, got %s",
		expectedReservations.String(), caAfterImport.ReservedAmounts.String())

	// Verify lease counts are preserved
	require.Equal(t, uint64(1), caAfterImport.PendingLeaseCount)
	require.Equal(t, uint64(1), caAfterImport.ActiveLeaseCount)

	// Verify leases are preserved
	importedPendingLease, err := k2.GetLease(f2.Ctx, pendingLeaseUUID)
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_PENDING, importedPendingLease.State)
	require.Equal(t, pendingLease.MinLeaseDurationAtCreation, importedPendingLease.MinLeaseDurationAtCreation)

	importedActiveLease, err := k2.GetLease(f2.Ctx, activeLeaseUUID)
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_ACTIVE, importedActiveLease.State)
	require.Equal(t, activeLease.MinLeaseDurationAtCreation, importedActiveLease.MinLeaseDurationAtCreation)

	// Verify available credit is correct after import
	creditBalance := f2.App.BankKeeper.GetBalance(f2.Ctx, creditAddr, testDenom)
	availableCredit := types.GetAvailableCredit(
		sdk.NewCoins(creditBalance),
		caAfterImport.ReservedAmounts,
	)
	expectedAvailable := creditBalance.Amount.Sub(expectedReservations.AmountOf(testDenom))
	require.True(t, availableCredit.AmountOf(testDenom).Equal(expectedAvailable),
		"available credit: expected %s, got %s",
		expectedAvailable.String(), availableCredit.AmountOf(testDenom).String())
}

// TestGenesisSequenceRoundTrip verifies that UUID generation sequences survive
// a genesis export/import cycle, preventing UUID collisions after chain restart.
func TestGenesisSequenceRoundTrip(t *testing.T) {
	f := initFixture(t)

	k := f.App.BillingKeeper
	msgServer := keeper.NewMsgServerImpl(k)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	payoutAddr := f.TestAccs[2]

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Fund tenant
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(100_000_000))))
	err = k.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Create 3 leases to advance the sequence to 3
	var leaseUUIDs []string
	for range 3 {
		resp, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
			Tenant: tenant.String(),
			Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
		})
		require.NoError(t, err)
		leaseUUIDs = append(leaseUUIDs, resp.LeaseUuid)
	}

	// Export genesis
	exportedGenesis := k.ExportGenesis(f.Ctx)

	// Verify sequence was exported
	require.Equal(t, uint64(3), exportedGenesis.LeaseSequence,
		"exported LeaseSequence should equal number of leases created")

	// Verify exported genesis passes validation (same path as chain restart)
	require.NoError(t, exportedGenesis.Validate())

	// Import into fresh fixture
	f2 := initFixture(t)
	k2 := f2.App.BillingKeeper

	// Set up required provider/SKU in new context
	err = f2.App.SKUKeeper.SetProvider(f2.Ctx, provider)
	require.NoError(t, err)
	err = f2.App.SKUKeeper.SetSKU(f2.Ctx, sku)
	require.NoError(t, err)

	// Fund credit address in new context
	f2.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(100_000_000))))

	// Import genesis
	err = k2.InitGenesis(f2.Ctx, exportedGenesis)
	require.NoError(t, err)

	// Verify sequence was restored
	seq, err := k2.LeaseSequence.Peek(f2.Ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(3), seq, "LeaseSequence should be restored after import")

	// Create a new lease post-import and verify no UUID collision
	msgServer2 := keeper.NewMsgServerImpl(k2)
	resp, err := msgServer2.CreateLease(f2.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
	})
	require.NoError(t, err)

	// New lease UUID must not collide with any pre-import UUID
	for _, existingUUID := range leaseUUIDs {
		require.NotEqual(t, existingUUID, resp.LeaseUuid,
			"post-import lease UUID should not collide with pre-import UUIDs")
	}
}

// ============================================================================
// Critical Gap Tests: Settlement Atomicity
// ============================================================================

// TestSettlementAtomicityOnClose verifies that when a lease is closed,
// the settlement and state changes are applied atomically.
func TestSettlementAtomicityOnClose(t *testing.T) {
	f := initFixture(t)

	k := f.App.BillingKeeper
	msgServer := keeper.NewMsgServerImpl(k)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	payoutAddr := f.TestAccs[2]

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600) // 3600 per hour = 1 per second

	// Fund tenant's credit account
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	fundAmount := sdk.NewCoin(testDenom, sdkmath.NewInt(10000000))
	f.fundAccount(t, creditAddr, sdk.NewCoins(fundAmount))

	// Create and fund credit account
	err = k.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Create and acknowledge a lease
	resp, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items: []types.LeaseItemInput{
			{SkuUuid: sku.Uuid, Quantity: 1},
		},
	})
	require.NoError(t, err)
	leaseUUID := resp.LeaseUuid

	_, err = msgServer.AcknowledgeLease(f.Ctx, &types.MsgAcknowledgeLease{
		Sender:     providerAddr.String(),
		LeaseUuids: []string{leaseUUID},
	})
	require.NoError(t, err)

	// Verify lease is active with reservation
	lease, err := k.GetLease(f.Ctx, leaseUUID)
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_ACTIVE, lease.State)

	caBeforeClose, err := k.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)
	require.Equal(t, uint64(1), caBeforeClose.ActiveLeaseCount)
	require.False(t, caBeforeClose.ReservedAmounts.IsZero())

	// Record balances before close
	creditBalanceBefore := f.App.BankKeeper.GetBalance(f.Ctx, creditAddr, testDenom)
	payoutBalanceBefore := f.App.BankKeeper.GetBalance(f.Ctx, payoutAddr, testDenom)

	// Advance time to accrue some charges
	f.Ctx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(100 * time.Second))

	// Close the lease - this should atomically:
	// 1. Settle any accrued charges
	// 2. Release the reservation
	// 3. Update lease state to CLOSED
	// 4. Decrement active lease count
	_, err = msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
		Sender:     tenant.String(),
		LeaseUuids: []string{leaseUUID},
	})
	require.NoError(t, err)

	// Verify all state changes happened atomically
	leaseAfterClose, err := k.GetLease(f.Ctx, leaseUUID)
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_CLOSED, leaseAfterClose.State)
	require.NotNil(t, leaseAfterClose.ClosedAt)

	caAfterClose, err := k.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)
	require.Equal(t, uint64(0), caAfterClose.ActiveLeaseCount)
	require.True(t, caAfterClose.ReservedAmounts.IsZero(),
		"reservation should be released after close, got %s", caAfterClose.ReservedAmounts.String())

	// Verify settlement occurred (provider received payment)
	payoutBalanceAfter := f.App.BankKeeper.GetBalance(f.Ctx, payoutAddr, testDenom)
	require.True(t, payoutBalanceAfter.Amount.GT(payoutBalanceBefore.Amount),
		"provider should have received payment")

	// Verify credit was deducted
	creditBalanceAfter := f.App.BankKeeper.GetBalance(f.Ctx, creditAddr, testDenom)
	require.True(t, creditBalanceAfter.Amount.LT(creditBalanceBefore.Amount),
		"credit should have been deducted for settlement")

	// Verify trying to close again fails
	_, err = msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
		Sender:     tenant.String(),
		LeaseUuids: []string{leaseUUID},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not active")
}

// TestEndBlockerSkipsAlreadyClosedLeases verifies that EndBlocker's batch settlement
// correctly skips leases that were already closed during the same block.
func TestEndBlockerSkipsAlreadyClosedLeases(t *testing.T) {
	f := initFixture(t)

	k := f.App.BillingKeeper
	msgServer := keeper.NewMsgServerImpl(k)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	payoutAddr := f.TestAccs[2]

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Fund tenant's credit account
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	fundAmount := sdk.NewCoin(testDenom, sdkmath.NewInt(10000000))
	f.fundAccount(t, creditAddr, sdk.NewCoins(fundAmount))

	err = k.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Create and acknowledge a lease
	resp, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items: []types.LeaseItemInput{
			{SkuUuid: sku.Uuid, Quantity: 1},
		},
	})
	require.NoError(t, err)

	_, err = msgServer.AcknowledgeLease(f.Ctx, &types.MsgAcknowledgeLease{
		Sender:     providerAddr.String(),
		LeaseUuids: []string{resp.LeaseUuid},
	})
	require.NoError(t, err)

	// Advance time
	f.Ctx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(100 * time.Second))

	// Close the lease manually (simulating a transaction in the block)
	_, err = msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
		Sender:     tenant.String(),
		LeaseUuids: []string{resp.LeaseUuid},
	})
	require.NoError(t, err)

	// Record state after close
	caAfterClose, err := k.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)
	creditBalanceAfterClose := f.App.BankKeeper.GetBalance(f.Ctx, creditAddr, testDenom)
	payoutBalanceAfterClose := f.App.BankKeeper.GetBalance(f.Ctx, payoutAddr, testDenom)

	// Run EndBlocker (simulating end of block processing)
	// This should NOT process the already-closed lease
	err = k.EndBlocker(f.Ctx)
	require.NoError(t, err)

	// Verify state is unchanged - EndBlocker should have skipped the closed lease
	caAfterEndBlocker, err := k.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)
	require.Equal(t, caAfterClose.ActiveLeaseCount, caAfterEndBlocker.ActiveLeaseCount)
	require.True(t, caAfterClose.ReservedAmounts.Equal(caAfterEndBlocker.ReservedAmounts))

	// Verify balances unchanged
	creditBalanceAfterEndBlocker := f.App.BankKeeper.GetBalance(f.Ctx, creditAddr, testDenom)
	payoutBalanceAfterEndBlocker := f.App.BankKeeper.GetBalance(f.Ctx, payoutAddr, testDenom)

	require.True(t, creditBalanceAfterClose.Equal(creditBalanceAfterEndBlocker),
		"credit balance should be unchanged after EndBlocker")
	require.True(t, payoutBalanceAfterClose.Equal(payoutBalanceAfterEndBlocker),
		"payout balance should be unchanged after EndBlocker")
}

// TestReservationConsistencyAfterFailedClose verifies that if a close operation
// fails mid-way, the reservation remains intact (atomicity via CacheContext).
func TestReservationConsistencyAfterFailedClose(t *testing.T) {
	f := initFixture(t)

	k := f.App.BillingKeeper
	msgServer := keeper.NewMsgServerImpl(k)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	payoutAddr := f.TestAccs[2]

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Fund tenant's credit account
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	fundAmount := sdk.NewCoin(testDenom, sdkmath.NewInt(10000000))
	f.fundAccount(t, creditAddr, sdk.NewCoins(fundAmount))

	err = k.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Create and acknowledge a lease
	resp, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items: []types.LeaseItemInput{
			{SkuUuid: sku.Uuid, Quantity: 1},
		},
	})
	require.NoError(t, err)

	_, err = msgServer.AcknowledgeLease(f.Ctx, &types.MsgAcknowledgeLease{
		Sender:     providerAddr.String(),
		LeaseUuids: []string{resp.LeaseUuid},
	})
	require.NoError(t, err)

	// Record state before failed close attempt
	caBeforeFailedClose, err := k.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)
	reservationBefore := caBeforeFailedClose.ReservedAmounts

	// Try to close with wrong sender (should fail)
	wrongSender := f.TestAccs[3]
	_, err = msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
		Sender:     wrongSender.String(),
		LeaseUuids: []string{resp.LeaseUuid},
	})
	require.Error(t, err)

	// Verify reservation is unchanged after failed close
	caAfterFailedClose, err := k.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)
	require.True(t, reservationBefore.Equal(caAfterFailedClose.ReservedAmounts),
		"reservation should be unchanged after failed close: expected %s, got %s",
		reservationBefore.String(), caAfterFailedClose.ReservedAmounts.String())

	// Verify lease is still active
	lease, err := k.GetLease(f.Ctx, resp.LeaseUuid)
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_ACTIVE, lease.State)
	require.Equal(t, uint64(1), caAfterFailedClose.ActiveLeaseCount)
}

// ============================================================================
// Major Gap Tests: Parameter Change Impact
// ============================================================================

// TestParamChangeDoesNotAffectExistingLeaseReservations verifies that changing
// min_lease_duration parameter does not affect reservations for existing leases.
// Existing leases use their stored MinLeaseDurationAtCreation for consistency.
func TestParamChangeDoesNotAffectExistingLeaseReservations(t *testing.T) {
	f := initFixture(t)

	k := f.App.BillingKeeper
	msgServer := keeper.NewMsgServerImpl(k)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	payoutAddr := f.TestAccs[2]

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600) // 3600 per hour = 1 per second

	// Fund tenant's credit account
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	fundAmount := sdk.NewCoin(testDenom, sdkmath.NewInt(100000000))
	f.fundAccount(t, creditAddr, sdk.NewCoins(fundAmount))

	err = k.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Get initial params (min_lease_duration = 3600 by default)
	params, err := k.GetParams(f.Ctx)
	require.NoError(t, err)
	initialMinDuration := params.MinLeaseDuration
	require.Equal(t, uint64(3600), initialMinDuration)

	// Create a lease with initial min_lease_duration
	resp, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items: []types.LeaseItemInput{
			{SkuUuid: sku.Uuid, Quantity: 1},
		},
	})
	require.NoError(t, err)
	leaseUUID := resp.LeaseUuid

	// Get the lease and verify MinLeaseDurationAtCreation is stored
	lease, err := k.GetLease(f.Ctx, leaseUUID)
	require.NoError(t, err)
	require.Equal(t, initialMinDuration, lease.MinLeaseDurationAtCreation,
		"lease should store min_lease_duration at creation")

	// Get reservation before parameter change
	caBeforeParamChange, err := k.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)
	reservationBeforeChange := caBeforeParamChange.ReservedAmounts

	// Calculate expected reservation based on initial min_lease_duration
	// rate = 1/sec, quantity = 1, duration = 3600 => reservation = 3600
	expectedReservation := types.CalculateLeaseReservation(lease.Items, initialMinDuration)
	require.True(t, expectedReservation.Equal(reservationBeforeChange),
		"initial reservation should match: expected %s, got %s",
		expectedReservation.String(), reservationBeforeChange.String())

	// Change min_lease_duration parameter to double the value
	newMinDuration := uint64(7200) // Double the duration
	newParams := types.NewParams(
		params.MaxLeasesPerTenant,
		params.AllowedList,
		params.MaxItemsPerLease,
		newMinDuration,
		params.MaxPendingLeasesPerTenant,
		params.PendingTimeout,
	)
	err = k.SetParams(f.Ctx, newParams)
	require.NoError(t, err)

	// Verify params changed
	updatedParams, err := k.GetParams(f.Ctx)
	require.NoError(t, err)
	require.Equal(t, newMinDuration, updatedParams.MinLeaseDuration)

	// Verify existing lease's reservation is UNCHANGED
	// The lease should use its stored MinLeaseDurationAtCreation, not the new param
	caAfterParamChange, err := k.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)
	require.True(t, reservationBeforeChange.Equal(caAfterParamChange.ReservedAmounts),
		"existing lease reservation should be unchanged after param change: before %s, after %s",
		reservationBeforeChange.String(), caAfterParamChange.ReservedAmounts.String())

	// Create a NEW lease - it should use the new min_lease_duration
	resp2, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items: []types.LeaseItemInput{
			{SkuUuid: sku.Uuid, Quantity: 1},
		},
	})
	require.NoError(t, err)

	// Verify new lease has the new min_lease_duration stored
	newLease, err := k.GetLease(f.Ctx, resp2.LeaseUuid)
	require.NoError(t, err)
	require.Equal(t, newMinDuration, newLease.MinLeaseDurationAtCreation,
		"new lease should store new min_lease_duration")

	// Verify total reservations include both leases with their respective durations
	caAfterNewLease, err := k.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)

	// First lease: rate * quantity * 3600 = 3600
	// Second lease: rate * quantity * 7200 = 7200
	// Total: 10800
	expectedOldReservation := types.CalculateLeaseReservation(lease.Items, initialMinDuration)
	expectedNewReservation := types.CalculateLeaseReservation(newLease.Items, newMinDuration)
	expectedTotalReservation := expectedOldReservation.Add(expectedNewReservation...)

	require.True(t, expectedTotalReservation.Equal(caAfterNewLease.ReservedAmounts),
		"total reservations should be sum of both leases: expected %s, got %s",
		expectedTotalReservation.String(), caAfterNewLease.ReservedAmounts.String())

	// Verify that releasing the old lease uses its stored duration (not new param)
	_, err = msgServer.AcknowledgeLease(f.Ctx, &types.MsgAcknowledgeLease{
		Sender:     providerAddr.String(),
		LeaseUuids: []string{leaseUUID},
	})
	require.NoError(t, err)

	// Close the old lease
	_, err = msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
		Sender:     tenant.String(),
		LeaseUuids: []string{leaseUUID},
	})
	require.NoError(t, err)

	// Verify only the old lease's reservation was released (using its stored duration)
	caAfterOldClose, err := k.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)

	// Should have only the new lease's reservation remaining
	require.True(t, expectedNewReservation.Equal(caAfterOldClose.ReservedAmounts),
		"after closing old lease, only new lease reservation should remain: expected %s, got %s",
		expectedNewReservation.String(), caAfterOldClose.ReservedAmounts.String())
}

// ============================================================================
// Major Gap Tests: Partial Settlement on Credit Exhaustion
// ============================================================================

// TestPartialSettlementOnCreditExhaustion verifies the exact behavior when
// credit runs out during settlement - the lease should be settled up to the
// available credit and then closed.
func TestPartialSettlementOnCreditExhaustion(t *testing.T) {
	f := initFixture(t)

	k := f.App.BillingKeeper
	msgServer := keeper.NewMsgServerImpl(k)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	payoutAddr := f.TestAccs[2]

	// Create provider and SKU with a rate of 1 per second (3600 per hour)
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Fund tenant's credit account with LIMITED funds
	// Reservation for 1 hour = 3600, fund just enough for reservation + small amount
	// Rate = 1/sec * 1 quantity = 1/sec
	// We'll fund 5000 - reservation takes 3600, leaving 1400 available for new leases
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	fundAmount := sdk.NewCoin(testDenom, sdkmath.NewInt(5000))
	f.fundAccount(t, creditAddr, sdk.NewCoins(fundAmount))

	err = k.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Create and acknowledge a lease
	resp, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items: []types.LeaseItemInput{
			{SkuUuid: sku.Uuid, Quantity: 1},
		},
	})
	require.NoError(t, err)

	_, err = msgServer.AcknowledgeLease(f.Ctx, &types.MsgAcknowledgeLease{
		Sender:     providerAddr.String(),
		LeaseUuids: []string{resp.LeaseUuid},
	})
	require.NoError(t, err)

	// Verify lease is active and reservation is made
	lease, err := k.GetLease(f.Ctx, resp.LeaseUuid)
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_ACTIVE, lease.State)

	ca, err := k.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)
	require.Equal(t, uint64(1), ca.ActiveLeaseCount)

	// Check available credit (balance - reserved)
	// Balance: 5000, Reserved: 3600, Available: 1400
	creditBalance := f.App.BankKeeper.GetBalance(f.Ctx, creditAddr, testDenom)
	available := types.GetAvailableCredit(sdk.NewCoins(creditBalance), ca.ReservedAmounts)
	require.Equal(t, sdkmath.NewInt(1400), available.AmountOf(testDenom),
		"available credit should be balance minus reservation")

	// Record provider balance before settlement
	providerBalanceBefore := f.App.BankKeeper.GetBalance(f.Ctx, payoutAddr, testDenom)

	// Advance time beyond what the credit balance can cover
	// At 1/sec rate, with 5000 total balance = 5000 seconds max
	// Advance 6000 seconds (more than total credit)
	f.Ctx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(6000 * time.Second))

	// Close the lease - this triggers settlement first
	// Settlement should:
	// 1. Calculate accrued: 6000 (6000 seconds * 1/sec)
	// 2. Available credit balance: 5000
	// 3. Transfer min(6000, 5000) = 5000 (partial settlement)
	closeResp, err := msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
		Sender:     tenant.String(),
		LeaseUuids: []string{resp.LeaseUuid},
	})
	require.NoError(t, err)

	// Verify lease is closed
	leaseAfterClose, err := k.GetLease(f.Ctx, resp.LeaseUuid)
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_CLOSED, leaseAfterClose.State)
	require.NotNil(t, leaseAfterClose.ClosedAt)

	// Verify provider received partial payment (only what was available = 5000)
	providerBalanceAfter := f.App.BankKeeper.GetBalance(f.Ctx, payoutAddr, testDenom)
	settlementAmount := providerBalanceAfter.Amount.Sub(providerBalanceBefore.Amount)

	// Should receive exactly the available credit (5000), not the full accrued (6000)
	require.Equal(t, sdkmath.NewInt(5000), settlementAmount,
		"provider should receive partial settlement (available credit)")

	// Verify the settlement amount is reported in response
	require.True(t, closeResp.TotalSettledAmounts.AmountOf(testDenom).Equal(sdkmath.NewInt(5000)),
		"response should report settled amount")

	// Verify reservation was released
	caAfterClose, err := k.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)
	require.True(t, caAfterClose.ReservedAmounts.IsZero(),
		"reservation should be released after close")
	require.Equal(t, uint64(0), caAfterClose.ActiveLeaseCount)

	// Verify credit balance is zero (all consumed)
	creditBalanceAfter := f.App.BankKeeper.GetBalance(f.Ctx, creditAddr, testDenom)
	require.Equal(t, sdkmath.NewInt(0), creditBalanceAfter.Amount,
		"credit balance should be zero after exhaustion")
}

// TestAutoCloseOnWithdraw verifies that leases are automatically closed
// when credit is exhausted during provider Withdraw operation.
func TestAutoCloseOnWithdraw(t *testing.T) {
	f := initFixture(t)

	k := f.App.BillingKeeper
	msgServer := keeper.NewMsgServerImpl(k)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	payoutAddr := f.TestAccs[2]

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600) // 1/sec

	// Fund with very limited credit (just enough for reservation + tiny amount)
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	// Fund 3700: reservation=3600, leaves only 100 for actual usage
	fundAmount := sdk.NewCoin(testDenom, sdkmath.NewInt(3700))
	f.fundAccount(t, creditAddr, sdk.NewCoins(fundAmount))

	err = k.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Create and acknowledge a lease
	resp, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items: []types.LeaseItemInput{
			{SkuUuid: sku.Uuid, Quantity: 1},
		},
	})
	require.NoError(t, err)

	_, err = msgServer.AcknowledgeLease(f.Ctx, &types.MsgAcknowledgeLease{
		Sender:     providerAddr.String(),
		LeaseUuids: []string{resp.LeaseUuid},
	})
	require.NoError(t, err)

	// Verify lease is active
	lease, err := k.GetLease(f.Ctx, resp.LeaseUuid)
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_ACTIVE, lease.State)

	// Record provider balance
	providerBalanceBefore := f.App.BankKeeper.GetBalance(f.Ctx, payoutAddr, testDenom)

	// Advance time significantly beyond credit capacity
	// At 1/sec, 3700 total = 3700 seconds max
	// Advance 5000 seconds to ensure exhaustion
	f.Ctx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(5000 * time.Second))

	// Provider calls Withdraw - this triggers settlement and auto-close detection
	withdrawResp, err := msgServer.Withdraw(f.Ctx, &types.MsgWithdraw{
		Sender:     providerAddr.String(),
		LeaseUuids: []string{resp.LeaseUuid},
	})
	require.NoError(t, err)

	// Verify lease was auto-closed due to credit exhaustion
	leaseAfter, err := k.GetLease(f.Ctx, resp.LeaseUuid)
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_CLOSED, leaseAfter.State,
		"lease should be auto-closed due to credit exhaustion")
	require.NotNil(t, leaseAfter.ClosedAt)

	// Verify provider received payment (full available credit = 3700)
	providerBalanceAfter := f.App.BankKeeper.GetBalance(f.Ctx, payoutAddr, testDenom)
	settlementAmount := providerBalanceAfter.Amount.Sub(providerBalanceBefore.Amount)
	require.Equal(t, sdkmath.NewInt(3700), settlementAmount,
		"provider should receive all available credit")

	// Verify the withdraw response indicates what happened
	require.NotNil(t, withdrawResp)
	require.True(t, withdrawResp.TotalAmounts.AmountOf(testDenom).Equal(sdkmath.NewInt(3700)))

	// Verify reservation released and counts updated
	caAfter, err := k.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)
	require.True(t, caAfter.ReservedAmounts.IsZero(),
		"reservation should be released after auto-close")
	require.Equal(t, uint64(0), caAfter.ActiveLeaseCount)

	// Verify credit balance is zero
	creditBalanceAfter := f.App.BankKeeper.GetBalance(f.Ctx, creditAddr, testDenom)
	require.True(t, creditBalanceAfter.IsZero())
}

// TestClosureReasonTracking verifies that closure reasons are properly recorded
// for different lease closure scenarios: manual close with custom reason,
// auto-close due to credit exhaustion.
func TestClosureReasonTracking(t *testing.T) {
	f := initFixture(t)

	k := f.App.BillingKeeper
	msgServer := keeper.NewMsgServerImpl(k)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	payoutAddr := f.TestAccs[2]

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600) // 1/sec

	// Fund tenant's credit account
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	fundAmount := sdk.NewCoin(testDenom, sdkmath.NewInt(1000000))
	f.fundAccount(t, creditAddr, sdk.NewCoins(fundAmount))

	err = k.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	t.Run("manual close with custom reason", func(t *testing.T) {
		// Create and acknowledge lease
		resp, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
			Tenant: tenant.String(),
			Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
		})
		require.NoError(t, err)

		_, err = msgServer.AcknowledgeLease(f.Ctx, &types.MsgAcknowledgeLease{
			Sender:     providerAddr.String(),
			LeaseUuids: []string{resp.LeaseUuid},
		})
		require.NoError(t, err)

		// Close with custom reason
		customReason := "Resource no longer needed"
		_, err = msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
			Sender:     tenant.String(),
			LeaseUuids: []string{resp.LeaseUuid},
			Reason:     customReason,
		})
		require.NoError(t, err)

		// Verify the closure reason was recorded
		lease, err := k.GetLease(f.Ctx, resp.LeaseUuid)
		require.NoError(t, err)
		require.Equal(t, types.LEASE_STATE_CLOSED, lease.State)
		require.Equal(t, customReason, lease.ClosureReason,
			"closure reason should match the custom reason provided")
	})

	t.Run("manual close without reason", func(t *testing.T) {
		// Create and acknowledge another lease
		resp, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
			Tenant: tenant.String(),
			Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
		})
		require.NoError(t, err)

		_, err = msgServer.AcknowledgeLease(f.Ctx, &types.MsgAcknowledgeLease{
			Sender:     providerAddr.String(),
			LeaseUuids: []string{resp.LeaseUuid},
		})
		require.NoError(t, err)

		// Close without reason
		_, err = msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
			Sender:     tenant.String(),
			LeaseUuids: []string{resp.LeaseUuid},
			// No Reason field set
		})
		require.NoError(t, err)

		// Verify the closure reason is empty
		lease, err := k.GetLease(f.Ctx, resp.LeaseUuid)
		require.NoError(t, err)
		require.Equal(t, types.LEASE_STATE_CLOSED, lease.State)
		require.Empty(t, lease.ClosureReason,
			"closure reason should be empty when not provided")
	})

	t.Run("auto-close sets credit exhausted reason", func(t *testing.T) {
		// Create new tenant with limited credit
		limitedTenant := f.TestAccs[3]
		limitedCreditAddr, err := types.DeriveCreditAddressFromBech32(limitedTenant.String())
		require.NoError(t, err)

		// Fund with minimal credit (just enough for reservation + small buffer)
		limitedFund := sdk.NewCoin(testDenom, sdkmath.NewInt(3700))
		f.fundAccount(t, limitedCreditAddr, sdk.NewCoins(limitedFund))

		err = k.SetCreditAccount(f.Ctx, types.CreditAccount{
			Tenant:        limitedTenant.String(),
			CreditAddress: limitedCreditAddr.String(),
		})
		require.NoError(t, err)

		// Create and acknowledge lease
		resp, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
			Tenant: limitedTenant.String(),
			Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
		})
		require.NoError(t, err)

		_, err = msgServer.AcknowledgeLease(f.Ctx, &types.MsgAcknowledgeLease{
			Sender:     providerAddr.String(),
			LeaseUuids: []string{resp.LeaseUuid},
		})
		require.NoError(t, err)

		// Advance time to exhaust credit
		f.Ctx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(5000 * time.Second))

		// Provider withdraws, triggering auto-close
		_, err = msgServer.Withdraw(f.Ctx, &types.MsgWithdraw{
			Sender:     providerAddr.String(),
			LeaseUuids: []string{resp.LeaseUuid},
		})
		require.NoError(t, err)

		// Verify auto-close reason
		lease, err := k.GetLease(f.Ctx, resp.LeaseUuid)
		require.NoError(t, err)
		require.Equal(t, types.LEASE_STATE_CLOSED, lease.State)
		require.Equal(t, types.ClosureReasonCreditExhausted, lease.ClosureReason,
			"auto-closed lease should have credit exhausted reason")
	})
}

// TestAllowedListCreateLeaseForTenant verifies that the AllowedList parameter
// correctly restricts who can create leases on behalf of tenants.
func TestAllowedListCreateLeaseForTenant(t *testing.T) {
	f := initFixture(t)

	k := f.App.BillingKeeper
	msgServer := keeper.NewMsgServerImpl(k)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	payoutAddr := f.TestAccs[2]
	allowedAuthority := f.TestAccs[3]
	unauthorizedAddr := f.TestAccs[4]

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Fund tenant's credit account
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	fundAmount := sdk.NewCoin(testDenom, sdkmath.NewInt(1000000))
	f.fundAccount(t, creditAddr, sdk.NewCoins(fundAmount))

	err = k.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	t.Run("unauthorized address cannot create lease for tenant", func(t *testing.T) {
		// Try to create lease without being on the allowed list
		_, err := msgServer.CreateLeaseForTenant(f.Ctx, &types.MsgCreateLeaseForTenant{
			Authority: unauthorizedAddr.String(),
			Tenant:    tenant.String(),
			Items:     []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "unauthorized")
	})

	t.Run("allowed address can create lease for tenant", func(t *testing.T) {
		// Update params to add allowedAuthority to the allowed list
		params, err := k.GetParams(f.Ctx)
		require.NoError(t, err)
		params.AllowedList = []string{allowedAuthority.String()}
		err = k.SetParams(f.Ctx, params)
		require.NoError(t, err)

		// Now the allowed address should be able to create a lease for tenant
		resp, err := msgServer.CreateLeaseForTenant(f.Ctx, &types.MsgCreateLeaseForTenant{
			Authority: allowedAuthority.String(),
			Tenant:    tenant.String(),
			Items:     []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
		})
		require.NoError(t, err)
		require.NotEmpty(t, resp.LeaseUuid)

		// Verify the lease was created for the correct tenant
		lease, err := k.GetLease(f.Ctx, resp.LeaseUuid)
		require.NoError(t, err)
		require.Equal(t, tenant.String(), lease.Tenant)
	})

	t.Run("unauthorized after removal from allowed list", func(t *testing.T) {
		// Remove the authority from allowed list
		params, err := k.GetParams(f.Ctx)
		require.NoError(t, err)
		params.AllowedList = []string{}
		err = k.SetParams(f.Ctx, params)
		require.NoError(t, err)

		// The previously allowed address should now fail
		_, err = msgServer.CreateLeaseForTenant(f.Ctx, &types.MsgCreateLeaseForTenant{
			Authority: allowedAuthority.String(),
			Tenant:    tenant.String(),
			Items:     []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "unauthorized")
	})
}

// TestMetaHashValidation verifies that the MetaHash field validation
// works correctly during lease creation.
func TestMetaHashValidation(t *testing.T) {
	f := initFixture(t)

	k := f.App.BillingKeeper
	msgServer := keeper.NewMsgServerImpl(k)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	payoutAddr := f.TestAccs[2]

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Fund tenant's credit account
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	fundAmount := sdk.NewCoin(testDenom, sdkmath.NewInt(1000000))
	f.fundAccount(t, creditAddr, sdk.NewCoins(fundAmount))

	err = k.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	t.Run("valid meta_hash is accepted", func(t *testing.T) {
		// SHA-256 hash is 32 bytes
		validHash := make([]byte, 32)
		for i := range validHash {
			validHash[i] = byte(i)
		}

		resp, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
			Tenant:   tenant.String(),
			Items:    []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
			MetaHash: validHash,
		})
		require.NoError(t, err)
		require.NotEmpty(t, resp.LeaseUuid)

		// Verify meta_hash was stored
		lease, err := k.GetLease(f.Ctx, resp.LeaseUuid)
		require.NoError(t, err)
		require.Equal(t, validHash, lease.MetaHash)
	})

	t.Run("max length meta_hash is accepted", func(t *testing.T) {
		// Max length is 64 bytes (for SHA-512)
		maxHash := make([]byte, types.MaxMetaHashLength)
		for i := range maxHash {
			maxHash[i] = byte(i % 256)
		}

		resp, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
			Tenant:   tenant.String(),
			Items:    []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
			MetaHash: maxHash,
		})
		require.NoError(t, err)
		require.NotEmpty(t, resp.LeaseUuid)
	})

	t.Run("meta_hash exceeding max length is rejected", func(t *testing.T) {
		// Exceed max length
		oversizedHash := make([]byte, types.MaxMetaHashLength+1)

		_, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
			Tenant:   tenant.String(),
			Items:    []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
			MetaHash: oversizedHash,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "meta_hash")
	})

	t.Run("empty meta_hash is allowed", func(t *testing.T) {
		resp, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
			Tenant: tenant.String(),
			Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
			// No MetaHash set
		})
		require.NoError(t, err)
		require.NotEmpty(t, resp.LeaseUuid)

		// Verify meta_hash is empty/nil
		lease, err := k.GetLease(f.Ctx, resp.LeaseUuid)
		require.NoError(t, err)
		require.Empty(t, lease.MetaHash)
	})
}
