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
	providerID, err := f.App.SKUKeeper.GetNextProviderID(f.Ctx)
	require.NoError(t, err)

	provider := skutypes.Provider{
		Id:            providerID,
		Address:       address,
		PayoutAddress: payoutAddress,
		Active:        true,
	}
	err = f.App.SKUKeeper.SetProvider(f.Ctx, provider)
	require.NoError(t, err)
	return provider
}

// createTestSKU creates a SKU in the SKU module for testing.
func (f *testFixture) createTestSKU(t *testing.T, providerID uint64, name string, priceAmount int64) skutypes.SKU {
	t.Helper()
	skuID, err := f.App.SKUKeeper.GetNextSKUID(f.Ctx)
	require.NoError(t, err)

	sku := skutypes.SKU{
		Id:         skuID,
		ProviderId: providerID,
		Name:       name,
		Unit:       skutypes.Unit_UNIT_PER_HOUR,
		BasePrice:  sdk.NewCoin("umfx", sdkmath.NewInt(priceAmount)),
		Active:     true,
	}
	err = f.App.SKUKeeper.SetSKU(f.Ctx, sku)
	require.NoError(t, err)
	return sku
}

func TestInitGenesis(t *testing.T) {
	f := initFixture(t)

	k := f.App.BillingKeeper

	// Fund the credit address before importing genesis
	tenant := f.TestAccs[0]
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)

	denom := types.DefaultDenom
	balance := sdk.NewCoin(denom, sdkmath.NewInt(1000000))

	// Mint and send to credit address
	f.fundAccount(t, creditAddr, sdk.NewCoins(balance))

	genesisState := &types.GenesisState{
		Params: types.DefaultParams(),
		Leases: []types.Lease{
			{
				Id:         1,
				Tenant:     tenant.String(),
				ProviderId: 1,
				Items: []types.LeaseItem{
					{
						SkuId:       1,
						Quantity:    2,
						LockedPrice: sdkmath.NewInt(100),
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
		NextLeaseId: 2,
	}

	err = k.InitGenesis(f.Ctx, genesisState)
	require.NoError(t, err)

	// Verify lease was imported
	lease, err := k.GetLease(f.Ctx, 1)
	require.NoError(t, err)
	require.Equal(t, tenant.String(), lease.Tenant)
	require.Equal(t, uint64(1), lease.ProviderId)
	require.Equal(t, types.LEASE_STATE_ACTIVE, lease.State)

	// Verify credit account was imported
	ca, err := k.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)
	require.Equal(t, creditAddr.String(), ca.CreditAddress)
}

func TestExportGenesis(t *testing.T) {
	f := initFixture(t)

	k := f.App.BillingKeeper

	tenant := f.TestAccs[0]
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)

	denom := types.DefaultDenom
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
		Id:         1,
		Tenant:     tenant.String(),
		ProviderId: 1,
		Items: []types.LeaseItem{
			{
				SkuId:       1,
				Quantity:    1,
				LockedPrice: sdkmath.NewInt(50),
			},
		},
		State:     types.LEASE_STATE_ACTIVE,
		CreatedAt: f.Ctx.BlockTime(),
	}
	err = k.SetLease(f.Ctx, lease)
	require.NoError(t, err)

	err = k.NextLeaseID.Set(f.Ctx, 2)
	require.NoError(t, err)

	// Export genesis
	genState := k.ExportGenesis(f.Ctx)

	require.NotNil(t, genState)
	require.Equal(t, types.DefaultParams(), genState.Params)
	require.Len(t, genState.Leases, 1)
	require.Equal(t, uint64(1), genState.Leases[0].Id)
	require.Len(t, genState.CreditAccounts, 1)
	require.Equal(t, tenant.String(), genState.CreditAccounts[0].Tenant)
	require.Equal(t, uint64(2), genState.NextLeaseId)
}

func TestGetSetParams(t *testing.T) {
	f := initFixture(t)

	k := f.App.BillingKeeper

	// Get default params
	params, err := k.GetParams(f.Ctx)
	require.NoError(t, err)
	require.Equal(t, types.DefaultDenom, params.Denom)
	require.Equal(t, types.DefaultMinCreditBalance, params.MinCreditBalance)
	require.Equal(t, types.DefaultMaxLeasesPerTenant, params.MaxLeasesPerTenant)

	// Set new params
	newParams := types.NewParams(
		"factory/testdenom/upwr",
		sdkmath.NewInt(10000000),
		50,
	)
	err = k.SetParams(f.Ctx, newParams)
	require.NoError(t, err)

	// Verify new params
	gotParams, err := k.GetParams(f.Ctx)
	require.NoError(t, err)
	require.Equal(t, "factory/testdenom/upwr", gotParams.Denom)
	require.Equal(t, sdkmath.NewInt(10000000), gotParams.MinCreditBalance)
	require.Equal(t, uint64(50), gotParams.MaxLeasesPerTenant)
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
	_, err := k.GetLease(f.Ctx, 1)
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrLeaseNotFound)

	// Create a lease
	lease := types.Lease{
		Id:         1,
		Tenant:     tenant.String(),
		ProviderId: 1,
		Items: []types.LeaseItem{
			{
				SkuId:       1,
				Quantity:    1,
				LockedPrice: sdkmath.NewInt(100),
			},
		},
		State:     types.LEASE_STATE_ACTIVE,
		CreatedAt: f.Ctx.BlockTime(),
	}
	err = k.SetLease(f.Ctx, lease)
	require.NoError(t, err)

	// Get lease
	gotLease, err := k.GetLease(f.Ctx, 1)
	require.NoError(t, err)
	require.Equal(t, lease.Id, gotLease.Id)
	require.Equal(t, lease.Tenant, gotLease.Tenant)
	require.Equal(t, lease.ProviderId, gotLease.ProviderId)
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
			Id:         i,
			Tenant:     f.TestAccs[int(i-1)].String(),
			ProviderId: 1,
			Items: []types.LeaseItem{
				{
					SkuId:       i,
					Quantity:    1,
					LockedPrice: sdkmath.NewInt(100),
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
			Id:         i,
			Tenant:     tenant1.String(),
			ProviderId: 1,
			Items: []types.LeaseItem{
				{
					SkuId:       i,
					Quantity:    1,
					LockedPrice: sdkmath.NewInt(100),
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
			Id:         i,
			Tenant:     tenant2.String(),
			ProviderId: 2,
			Items: []types.LeaseItem{
				{
					SkuId:       i,
					Quantity:    1,
					LockedPrice: sdkmath.NewInt(100),
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
			Id:         i,
			Tenant:     f.TestAccs[0].String(),
			ProviderId: 1,
			Items: []types.LeaseItem{
				{
					SkuId:       i,
					Quantity:    1,
					LockedPrice: sdkmath.NewInt(100),
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
			Id:         i,
			Tenant:     f.TestAccs[1].String(),
			ProviderId: 2,
			Items: []types.LeaseItem{
				{
					SkuId:       i,
					Quantity:    1,
					LockedPrice: sdkmath.NewInt(100),
				},
			},
			State:     types.LEASE_STATE_ACTIVE,
			CreatedAt: f.Ctx.BlockTime(),
		}
		err := k.SetLease(f.Ctx, lease)
		require.NoError(t, err)
	}

	// Get leases by provider 1
	provider1Leases, err := k.GetLeasesByProviderID(f.Ctx, 1)
	require.NoError(t, err)
	require.Len(t, provider1Leases, 4)

	// Get leases by provider 2
	provider2Leases, err := k.GetLeasesByProviderID(f.Ctx, 2)
	require.NoError(t, err)
	require.Len(t, provider2Leases, 2)

	// Get leases by unknown provider
	unknownLeases, err := k.GetLeasesByProviderID(f.Ctx, 999)
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
			Id:         i,
			Tenant:     tenant.String(),
			ProviderId: 1,
			Items: []types.LeaseItem{
				{
					SkuId:       i,
					Quantity:    1,
					LockedPrice: sdkmath.NewInt(100),
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
			Id:         i,
			Tenant:     tenant.String(),
			ProviderId: 1,
			Items: []types.LeaseItem{
				{
					SkuId:       i,
					Quantity:    1,
					LockedPrice: sdkmath.NewInt(100),
				},
			},
			State:     types.LEASE_STATE_INACTIVE,
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

func TestGetNextLeaseID(t *testing.T) {
	f := initFixture(t)

	k := f.App.BillingKeeper

	// Set initial sequence
	err := k.NextLeaseID.Set(f.Ctx, 1)
	require.NoError(t, err)

	// Get next IDs - should increment
	id1, err := k.GetNextLeaseID(f.Ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(1), id1)

	id2, err := k.GetNextLeaseID(f.Ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(2), id2)

	id3, err := k.GetNextLeaseID(f.Ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(3), id3)
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
	denom := types.DefaultDenom

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
			name: "empty denom",
			params: types.NewParams(
				"",
				sdkmath.NewInt(5000000),
				100,
			),
			expectErr: true,
		},
		{
			name: "negative min credit balance",
			params: types.NewParams(
				"upwr",
				sdkmath.NewInt(-1),
				100,
			),
			expectErr: true,
		},
		{
			name: "zero max leases per tenant",
			params: types.NewParams(
				"upwr",
				sdkmath.NewInt(5000000),
				0,
			),
			expectErr: true,
		},
		{
			name: "valid custom params",
			params: types.NewParams(
				"factory/addr/upwr",
				sdkmath.NewInt(10000000),
				50,
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
						Id:         1,
						Tenant:     tenant.String(),
						ProviderId: 1,
						Items: []types.LeaseItem{
							{
								SkuId:       1,
								Quantity:    1,
								LockedPrice: sdkmath.NewInt(100),
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
				NextLeaseId: 2,
			},
			expectErr: false,
		},
		{
			name: "zero next lease id",
			genesis: types.GenesisState{
				Params:      types.DefaultParams(),
				NextLeaseId: 0,
			},
			expectErr: true,
		},
		{
			name: "duplicate lease id",
			genesis: types.GenesisState{
				Params: types.DefaultParams(),
				Leases: []types.Lease{
					{
						Id:         1,
						Tenant:     tenant.String(),
						ProviderId: 1,
						Items: []types.LeaseItem{
							{
								SkuId:       1,
								Quantity:    1,
								LockedPrice: sdkmath.NewInt(100),
							},
						},
						State:     types.LEASE_STATE_ACTIVE,
						CreatedAt: f.Ctx.BlockTime(),
					},
					{
						Id:         1,
						Tenant:     tenant.String(),
						ProviderId: 1,
						Items: []types.LeaseItem{
							{
								SkuId:       2,
								Quantity:    1,
								LockedPrice: sdkmath.NewInt(100),
							},
						},
						State:     types.LEASE_STATE_ACTIVE,
						CreatedAt: f.Ctx.BlockTime(),
					},
				},
				NextLeaseId: 2,
			},
			expectErr: true,
		},
		{
			name: "lease id >= next lease id",
			genesis: types.GenesisState{
				Params: types.DefaultParams(),
				Leases: []types.Lease{
					{
						Id:         5,
						Tenant:     tenant.String(),
						ProviderId: 1,
						Items: []types.LeaseItem{
							{
								SkuId:       1,
								Quantity:    1,
								LockedPrice: sdkmath.NewInt(100),
							},
						},
						State:     types.LEASE_STATE_ACTIVE,
						CreatedAt: f.Ctx.BlockTime(),
					},
				},
				NextLeaseId: 2,
			},
			expectErr: true,
		},
		{
			name: "inactive lease without closed_at",
			genesis: types.GenesisState{
				Params: types.DefaultParams(),
				Leases: []types.Lease{
					{
						Id:         1,
						Tenant:     tenant.String(),
						ProviderId: 1,
						Items: []types.LeaseItem{
							{
								SkuId:       1,
								Quantity:    1,
								LockedPrice: sdkmath.NewInt(100),
							},
						},
						State:     types.LEASE_STATE_INACTIVE,
						CreatedAt: f.Ctx.BlockTime(),
						// Missing ClosedAt
					},
				},
				NextLeaseId: 2,
			},
			expectErr: true,
		},
		{
			name: "valid inactive lease with closed_at",
			genesis: types.GenesisState{
				Params: types.DefaultParams(),
				Leases: []types.Lease{
					{
						Id:         1,
						Tenant:     tenant.String(),
						ProviderId: 1,
						Items: []types.LeaseItem{
							{
								SkuId:       1,
								Quantity:    1,
								LockedPrice: sdkmath.NewInt(100),
							},
						},
						State:     types.LEASE_STATE_INACTIVE,
						CreatedAt: f.Ctx.BlockTime(),
						ClosedAt:  &closedAt,
					},
				},
				NextLeaseId: 2,
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
				NextLeaseId: 1,
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
				Amount: sdk.NewCoin(types.DefaultDenom, sdkmath.NewInt(1000000)),
			},
			expectErr: false,
		},
		{
			name: "MsgFundCredit invalid sender",
			msg: &types.MsgFundCredit{
				Sender: "invalid",
				Tenant: validAddr.String(),
				Amount: sdk.NewCoin(types.DefaultDenom, sdkmath.NewInt(1000000)),
			},
			expectErr: true,
		},
		{
			name: "MsgFundCredit invalid tenant",
			msg: &types.MsgFundCredit{
				Sender: validAddr.String(),
				Tenant: "invalid",
				Amount: sdk.NewCoin(types.DefaultDenom, sdkmath.NewInt(1000000)),
			},
			expectErr: true,
		},
		{
			name: "MsgFundCredit zero amount",
			msg: &types.MsgFundCredit{
				Sender: validAddr.String(),
				Tenant: validAddr.String(),
				Amount: sdk.NewCoin(types.DefaultDenom, sdkmath.ZeroInt()),
			},
			expectErr: true,
		},
		{
			name: "valid MsgCreateLease",
			msg: &types.MsgCreateLease{
				Tenant: validAddr.String(),
				Items: []types.LeaseItemInput{
					{
						SkuId:    1,
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
						SkuId:    1,
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
			name: "MsgCreateLease zero sku_id",
			msg: &types.MsgCreateLease{
				Tenant: validAddr.String(),
				Items: []types.LeaseItemInput{
					{
						SkuId:    0,
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
						SkuId:    1,
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
						SkuId:    1,
						Quantity: 1,
					},
					{
						SkuId:    1,
						Quantity: 2,
					},
				},
			},
			expectErr: true,
		},
		{
			name: "valid MsgCloseLease",
			msg: &types.MsgCloseLease{
				Sender:  validAddr.String(),
				LeaseId: 1,
			},
			expectErr: false,
		},
		{
			name: "MsgCloseLease zero lease_id",
			msg: &types.MsgCloseLease{
				Sender:  validAddr.String(),
				LeaseId: 0,
			},
			expectErr: true,
		},
		{
			name: "valid MsgWithdraw",
			msg: &types.MsgWithdraw{
				Sender:  validAddr.String(),
				LeaseId: 1,
			},
			expectErr: false,
		},
		{
			name: "MsgWithdraw zero lease_id",
			msg: &types.MsgWithdraw{
				Sender:  validAddr.String(),
				LeaseId: 0,
			},
			expectErr: true,
		},
		{
			name: "valid MsgWithdrawAll",
			msg: &types.MsgWithdrawAll{
				Sender:     validAddr.String(),
				ProviderId: 1,
			},
			expectErr: false,
		},
		{
			name: "valid MsgWithdrawAll zero provider_id",
			msg: &types.MsgWithdrawAll{
				Sender:     validAddr.String(),
				ProviderId: 0,
			},
			expectErr: false, // provider_id can be zero if sender is provider address
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
			case *types.MsgWithdrawAll:
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

	// Initialize SKU sequences first
	err := f.App.SKUKeeper.NextProviderID.Set(f.Ctx, 1)
	require.NoError(t, err)
	err = f.App.SKUKeeper.NextSKUID.Set(f.Ctx, 1)
	require.NoError(t, err)

	// Create a provider and SKU using SKU keeper
	provider := f.createTestProvider(t, f.TestAccs[0].String(), f.TestAccs[1].String())
	require.Equal(t, uint64(1), provider.Id)

	sku := f.createTestSKU(t, provider.Id, "Test SKU", 100)
	require.Equal(t, uint64(1), sku.Id)

	// Verify we can look them up via SKU keeper
	gotProvider, err := f.App.SKUKeeper.GetProvider(f.Ctx, provider.Id)
	require.NoError(t, err)
	require.Equal(t, provider.Address, gotProvider.Address)

	gotSKU, err := f.App.SKUKeeper.GetSKU(f.Ctx, sku.Id)
	require.NoError(t, err)
	require.Equal(t, sku.Name, gotSKU.Name)

	// Register denomination for transfers
	f.App.BankKeeper.SetDenomMetaData(f.Ctx, banktypes.Metadata{
		Base:        types.DefaultDenom,
		Display:     "pwr",
		Description: "Test PWR token",
	})
}
