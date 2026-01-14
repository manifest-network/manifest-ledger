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
						Tenant:        tenant.String(),
						CreditAddress: creditAddr.String(),
					},
				},
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

func TestCheckAndCloseExhaustedLease(t *testing.T) {
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

	// Run CheckAndCloseExhaustedLease - should close the lease
	closed, err := k.CheckAndCloseExhaustedLease(f.Ctx, &lease)
	require.NoError(t, err)
	require.True(t, closed)

	// Verify lease is now inactive
	require.Equal(t, types.LEASE_STATE_CLOSED, lease.State)
	require.NotNil(t, lease.ClosedAt)

	// Verify active lease count was decremented
	updatedCA, err := k.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)
	require.Equal(t, uint64(0), updatedCA.ActiveLeaseCount)
}

func TestCheckAndCloseExhaustedLease_WithBalance(t *testing.T) {
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

	// Run CheckAndCloseExhaustedLease - should NOT close the lease (has balance)
	closed, err := k.CheckAndCloseExhaustedLease(f.Ctx, &lease)
	require.NoError(t, err)
	require.False(t, closed)

	// Verify lease is still active
	require.Equal(t, types.LEASE_STATE_ACTIVE, lease.State)
}

func TestCheckAndCloseExhaustedLease_InactiveLease(t *testing.T) {
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

	// Run CheckAndCloseExhaustedLease - should NOT try to close inactive leases
	closed, err := k.CheckAndCloseExhaustedLease(f.Ctx, &lease)
	require.NoError(t, err)
	require.False(t, closed)
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
