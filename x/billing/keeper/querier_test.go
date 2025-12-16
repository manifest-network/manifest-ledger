/*
Package keeper_test contains unit tests for the billing module querier.

Test Coverage:
- QueryParams: parameter queries
- QueryLease: single lease queries
- QueryLeases: paginated lease queries with active_only filter
- QueryLeasesByTenant: tenant-indexed lease queries
- QueryLeasesByProvider: provider-indexed lease queries
- QueryCreditAccount: credit account queries
- QueryCreditAddress: credit address derivation queries
- QueryWithdrawableAmount: per-lease withdrawable amount with accrual calculation
- QueryProviderWithdrawable: provider total withdrawable across all leases
*/
package keeper_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"

	"github.com/manifest-network/manifest-ledger/x/billing/keeper"
	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

func TestQueryParams(t *testing.T) {
	f := initFixture(t)

	querier := keeper.NewQuerier(f.App.BillingKeeper)

	// Query params
	resp, err := querier.Params(f.Ctx, &types.QueryParamsRequest{})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, types.DefaultMaxLeasesPerTenant, resp.Params.MaxLeasesPerTenant)
}

func TestQueryLease(t *testing.T) {
	f := initFixture(t)

	k := f.App.BillingKeeper
	querier := keeper.NewQuerier(k)

	tenant := f.TestAccs[0]

	// Query non-existent lease
	_, err := querier.Lease(f.Ctx, &types.QueryLeaseRequest{LeaseId: 1})
	require.Error(t, err)

	// Create a lease
	lease := types.Lease{
		Id:         1,
		Tenant:     tenant.String(),
		ProviderId: 1,
		Items: []types.LeaseItem{
			{
				SkuId:       1,
				Quantity:    2,
				LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(100)),
			},
		},
		State:     types.LEASE_STATE_ACTIVE,
		CreatedAt: f.Ctx.BlockTime(),
	}
	err = k.SetLease(f.Ctx, lease)
	require.NoError(t, err)

	// Query the lease
	resp, err := querier.Lease(f.Ctx, &types.QueryLeaseRequest{LeaseId: 1})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, lease.Id, resp.Lease.Id)
	require.Equal(t, lease.Tenant, resp.Lease.Tenant)
	require.Equal(t, lease.ProviderId, resp.Lease.ProviderId)

	// Query with zero lease_id
	_, err = querier.Lease(f.Ctx, &types.QueryLeaseRequest{LeaseId: 0})
	require.Error(t, err)

	// Query with nil request
	_, err = querier.Lease(f.Ctx, nil)
	require.Error(t, err)
}

func TestQueryLeases(t *testing.T) {
	f := initFixture(t)

	k := f.App.BillingKeeper
	querier := keeper.NewQuerier(k)

	// Create multiple leases
	for i := uint64(1); i <= 5; i++ {
		state := types.LEASE_STATE_ACTIVE
		var closedAt *time.Time
		if i%2 == 0 {
			state = types.LEASE_STATE_INACTIVE
			ct := f.Ctx.BlockTime()
			closedAt = &ct
		}

		lease := types.Lease{
			Id:         i,
			Tenant:     f.TestAccs[0].String(),
			ProviderId: 1,
			Items: []types.LeaseItem{
				{
					SkuId:       i,
					Quantity:    1,
					LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(100)),
				},
			},
			State:     state,
			CreatedAt: f.Ctx.BlockTime(),
			ClosedAt:  closedAt,
		}
		err := k.SetLease(f.Ctx, lease)
		require.NoError(t, err)
	}

	// Query all leases
	resp, err := querier.Leases(f.Ctx, &types.QueryLeasesRequest{})
	require.NoError(t, err)
	require.Len(t, resp.Leases, 5)

	// Query active only
	resp, err = querier.Leases(f.Ctx, &types.QueryLeasesRequest{ActiveOnly: true})
	require.NoError(t, err)
	require.Len(t, resp.Leases, 3) // 1, 3, 5 are active

	// Query with pagination
	resp, err = querier.Leases(f.Ctx, &types.QueryLeasesRequest{
		Pagination: &query.PageRequest{Limit: 2},
	})
	require.NoError(t, err)
	require.Len(t, resp.Leases, 2)
	require.NotNil(t, resp.Pagination.NextKey)

	// Query with nil request
	_, err = querier.Leases(f.Ctx, nil)
	require.Error(t, err)
}

func TestQueryLeasesByTenant(t *testing.T) {
	f := initFixture(t)

	k := f.App.BillingKeeper
	querier := keeper.NewQuerier(k)

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
					LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(100)),
				},
			},
			State:     types.LEASE_STATE_ACTIVE,
			CreatedAt: f.Ctx.BlockTime(),
		}
		err := k.SetLease(f.Ctx, lease)
		require.NoError(t, err)
	}

	// Create one inactive lease for tenant1
	closedAt := f.Ctx.BlockTime()
	inactiveLease := types.Lease{
		Id:         4,
		Tenant:     tenant1.String(),
		ProviderId: 1,
		Items: []types.LeaseItem{
			{
				SkuId:       4,
				Quantity:    1,
				LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(100)),
			},
		},
		State:     types.LEASE_STATE_INACTIVE,
		CreatedAt: f.Ctx.BlockTime(),
		ClosedAt:  &closedAt,
	}
	err := k.SetLease(f.Ctx, inactiveLease)
	require.NoError(t, err)

	// Create leases for tenant2
	for i := uint64(5); i <= 6; i++ {
		lease := types.Lease{
			Id:         i,
			Tenant:     tenant2.String(),
			ProviderId: 2,
			Items: []types.LeaseItem{
				{
					SkuId:       i,
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

	// Query by tenant1
	resp, err := querier.LeasesByTenant(f.Ctx, &types.QueryLeasesByTenantRequest{
		Tenant: tenant1.String(),
	})
	require.NoError(t, err)
	require.Len(t, resp.Leases, 4)

	// Query by tenant1 active only
	resp, err = querier.LeasesByTenant(f.Ctx, &types.QueryLeasesByTenantRequest{
		Tenant:     tenant1.String(),
		ActiveOnly: true,
	})
	require.NoError(t, err)
	require.Len(t, resp.Leases, 3)

	// Query by tenant2
	resp, err = querier.LeasesByTenant(f.Ctx, &types.QueryLeasesByTenantRequest{
		Tenant: tenant2.String(),
	})
	require.NoError(t, err)
	require.Len(t, resp.Leases, 2)

	// Query with empty tenant
	_, err = querier.LeasesByTenant(f.Ctx, &types.QueryLeasesByTenantRequest{
		Tenant: "",
	})
	require.Error(t, err)

	// Query with invalid tenant address
	_, err = querier.LeasesByTenant(f.Ctx, &types.QueryLeasesByTenantRequest{
		Tenant: "invalid",
	})
	require.Error(t, err)

	// Query with nil request
	_, err = querier.LeasesByTenant(f.Ctx, nil)
	require.Error(t, err)
}

func TestQueryLeasesByProvider(t *testing.T) {
	f := initFixture(t)

	k := f.App.BillingKeeper
	querier := keeper.NewQuerier(k)

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
					LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(100)),
				},
			},
			State:     types.LEASE_STATE_ACTIVE,
			CreatedAt: f.Ctx.BlockTime(),
		}
		err := k.SetLease(f.Ctx, lease)
		require.NoError(t, err)
	}

	// Create inactive leases for provider 1
	closedAt := f.Ctx.BlockTime()
	inactiveLease := types.Lease{
		Id:         5,
		Tenant:     f.TestAccs[0].String(),
		ProviderId: 1,
		Items: []types.LeaseItem{
			{
				SkuId:       5,
				Quantity:    1,
				LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(100)),
			},
		},
		State:     types.LEASE_STATE_INACTIVE,
		CreatedAt: f.Ctx.BlockTime(),
		ClosedAt:  &closedAt,
	}
	err := k.SetLease(f.Ctx, inactiveLease)
	require.NoError(t, err)

	// Create leases for provider 2
	for i := uint64(6); i <= 7; i++ {
		lease := types.Lease{
			Id:         i,
			Tenant:     f.TestAccs[1].String(),
			ProviderId: 2,
			Items: []types.LeaseItem{
				{
					SkuId:       i,
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

	// Query by provider 1
	resp, err := querier.LeasesByProvider(f.Ctx, &types.QueryLeasesByProviderRequest{
		ProviderId: 1,
	})
	require.NoError(t, err)
	require.Len(t, resp.Leases, 5)

	// Query by provider 1 active only
	resp, err = querier.LeasesByProvider(f.Ctx, &types.QueryLeasesByProviderRequest{
		ProviderId: 1,
		ActiveOnly: true,
	})
	require.NoError(t, err)
	require.Len(t, resp.Leases, 4)

	// Query by provider 2
	resp, err = querier.LeasesByProvider(f.Ctx, &types.QueryLeasesByProviderRequest{
		ProviderId: 2,
	})
	require.NoError(t, err)
	require.Len(t, resp.Leases, 2)

	// Query with zero provider_id
	_, err = querier.LeasesByProvider(f.Ctx, &types.QueryLeasesByProviderRequest{
		ProviderId: 0,
	})
	require.Error(t, err)

	// Query with nil request
	_, err = querier.LeasesByProvider(f.Ctx, nil)
	require.Error(t, err)
}

func TestQueryCreditAccount(t *testing.T) {
	f := initFixture(t)

	k := f.App.BillingKeeper
	querier := keeper.NewQuerier(k)

	tenant := f.TestAccs[0]
	denom := testDenom

	// Query non-existent credit account
	_, err := querier.CreditAccount(f.Ctx, &types.QueryCreditAccountRequest{
		Tenant: tenant.String(),
	})
	require.Error(t, err)

	// Create credit account
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)

	ca := types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	}
	err = k.SetCreditAccount(f.Ctx, ca)
	require.NoError(t, err)

	// Fund the credit address with some tokens for balance testing
	fundAmount := sdk.NewCoin(denom, sdkmath.NewInt(1000000))
	f.fundAccount(t, creditAddr, sdk.NewCoins(fundAmount))

	// Query credit account
	resp, err := querier.CreditAccount(f.Ctx, &types.QueryCreditAccountRequest{
		Tenant: tenant.String(),
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, ca.Tenant, resp.CreditAccount.Tenant)
	require.Equal(t, ca.CreditAddress, resp.CreditAccount.CreditAddress)
	require.Equal(t, sdk.NewCoins(fundAmount), resp.Balances)

	// Query with empty tenant
	_, err = querier.CreditAccount(f.Ctx, &types.QueryCreditAccountRequest{
		Tenant: "",
	})
	require.Error(t, err)

	// Query with invalid tenant address
	_, err = querier.CreditAccount(f.Ctx, &types.QueryCreditAccountRequest{
		Tenant: "invalid",
	})
	require.Error(t, err)

	// Query with nil request
	_, err = querier.CreditAccount(f.Ctx, nil)
	require.Error(t, err)
}

func TestQueryCreditAddress(t *testing.T) {
	f := initFixture(t)

	querier := keeper.NewQuerier(f.App.BillingKeeper)

	tenant := f.TestAccs[0]

	// Query credit address
	resp, err := querier.CreditAddress(f.Ctx, &types.QueryCreditAddressRequest{
		Tenant: tenant.String(),
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotEmpty(t, resp.CreditAddress)

	// Verify the derived address matches
	expectedAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	require.Equal(t, expectedAddr.String(), resp.CreditAddress)

	// Query with empty tenant
	_, err = querier.CreditAddress(f.Ctx, &types.QueryCreditAddressRequest{
		Tenant: "",
	})
	require.Error(t, err)

	// Query with invalid tenant address
	_, err = querier.CreditAddress(f.Ctx, &types.QueryCreditAddressRequest{
		Tenant: "invalid",
	})
	require.Error(t, err)

	// Query with nil request
	_, err = querier.CreditAddress(f.Ctx, nil)
	require.Error(t, err)
}

func TestQueryWithdrawableAmount(t *testing.T) {
	f := initFixture(t)

	k := f.App.BillingKeeper
	querier := keeper.NewQuerier(k)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	payoutAddr := f.TestAccs[2]
	denom := testDenom

	// Initialize sequences
	err := k.NextLeaseID.Set(f.Ctx, 1)
	require.NoError(t, err)
	err = f.App.SKUKeeper.NextProviderID.Set(f.Ctx, 1)
	require.NoError(t, err)
	err = f.App.SKUKeeper.NextSKUID.Set(f.Ctx, 1)
	require.NoError(t, err)

	// Create provider and SKU with 3600 per hour = 1 per second
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	sku := f.createTestSKU(t, provider.Id, 3600)

	// Fund tenant's credit account
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	fundAmount := sdk.NewCoin(denom, sdkmath.NewInt(10000000))
	f.fundAccount(t, creditAddr, sdk.NewCoins(fundAmount))

	err = k.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Create a lease with quantity 2
	lease := types.Lease{
		Id:         1,
		Tenant:     tenant.String(),
		ProviderId: provider.Id,
		Items: []types.LeaseItem{
			{
				SkuId:       sku.Id,
				Quantity:    2,
				LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(1)), // 1 per second
			},
		},
		State:         types.LEASE_STATE_ACTIVE,
		CreatedAt:     f.Ctx.BlockTime(),
		LastSettledAt: f.Ctx.BlockTime(),
	}
	err = k.SetLease(f.Ctx, lease)
	require.NoError(t, err)

	// Query at initial time - should be 0
	resp, err := querier.WithdrawableAmount(f.Ctx, &types.QueryWithdrawableAmountRequest{
		LeaseId: 1,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.True(t, resp.Amounts.IsZero())

	// Advance block time by 100 seconds
	newCtx := f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(100 * time.Second))

	// Query withdrawable amount - should be 200 (1 per second * 2 quantity * 100 seconds)
	resp, err = querier.WithdrawableAmount(newCtx, &types.QueryWithdrawableAmountRequest{
		LeaseId: 1,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, denom, resp.Amounts[0].Denom)
	require.Equal(t, sdkmath.NewInt(200), resp.Amounts[0].Amount)

	// Query with zero lease_id
	_, err = querier.WithdrawableAmount(f.Ctx, &types.QueryWithdrawableAmountRequest{
		LeaseId: 0,
	})
	require.Error(t, err)

	// Query non-existent lease
	_, err = querier.WithdrawableAmount(f.Ctx, &types.QueryWithdrawableAmountRequest{
		LeaseId: 999,
	})
	require.Error(t, err)

	// Query with nil request
	_, err = querier.WithdrawableAmount(f.Ctx, nil)
	require.Error(t, err)
}

func TestQueryProviderWithdrawable(t *testing.T) {
	f := initFixture(t)

	k := f.App.BillingKeeper
	querier := keeper.NewQuerier(k)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	payoutAddr := f.TestAccs[2]
	denom := testDenom

	// Initialize sequences
	err := k.NextLeaseID.Set(f.Ctx, 1)
	require.NoError(t, err)
	err = f.App.SKUKeeper.NextProviderID.Set(f.Ctx, 1)
	require.NoError(t, err)
	err = f.App.SKUKeeper.NextSKUID.Set(f.Ctx, 1)
	require.NoError(t, err)

	// Create provider and SKU with 3600 per hour = 1 per second
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	sku := f.createTestSKU(t, provider.Id, 3600)

	// Fund tenant's credit account
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	fundAmount := sdk.NewCoin(denom, sdkmath.NewInt(10000000))
	f.fundAccount(t, creditAddr, sdk.NewCoins(fundAmount))

	err = k.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Create leases for provider 1
	for i := uint64(1); i <= 3; i++ {
		lease := types.Lease{
			Id:         i,
			Tenant:     tenant.String(),
			ProviderId: provider.Id,
			Items: []types.LeaseItem{
				{
					SkuId:       sku.Id,
					Quantity:    1,
					LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(1)), // 1 per second
				},
			},
			State:         types.LEASE_STATE_ACTIVE,
			CreatedAt:     f.Ctx.BlockTime(),
			LastSettledAt: f.Ctx.BlockTime(),
		}
		err := k.SetLease(f.Ctx, lease)
		require.NoError(t, err)
	}

	// Create an inactive lease for provider 1
	closedAt := f.Ctx.BlockTime()
	inactiveLease := types.Lease{
		Id:         4,
		Tenant:     tenant.String(),
		ProviderId: provider.Id,
		Items: []types.LeaseItem{
			{
				SkuId:       sku.Id,
				Quantity:    1,
				LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(1)),
			},
		},
		State:         types.LEASE_STATE_INACTIVE,
		CreatedAt:     f.Ctx.BlockTime(),
		LastSettledAt: f.Ctx.BlockTime(),
		ClosedAt:      &closedAt,
	}
	err = k.SetLease(f.Ctx, inactiveLease)
	require.NoError(t, err)

	// Query at initial time - should be 0
	resp, err := querier.ProviderWithdrawable(f.Ctx, &types.QueryProviderWithdrawableRequest{
		ProviderId: provider.Id,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.True(t, resp.Amounts.IsZero())
	require.Equal(t, uint64(0), resp.LeaseCount) // No leases with withdrawable amounts yet

	// Advance block time by 100 seconds
	newCtx := f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(100 * time.Second))

	// Query provider withdrawable - should be 300 (1 per second * 1 quantity * 100 seconds * 3 active leases)
	resp, err = querier.ProviderWithdrawable(newCtx, &types.QueryProviderWithdrawableRequest{
		ProviderId: provider.Id,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, denom, resp.Amounts[0].Denom)
	require.Equal(t, sdkmath.NewInt(300), resp.Amounts[0].Amount)
	require.Equal(t, uint64(3), resp.LeaseCount) // Only active leases with withdrawable amounts

	// Query with zero provider_id
	_, err = querier.ProviderWithdrawable(f.Ctx, &types.QueryProviderWithdrawableRequest{
		ProviderId: 0,
	})
	require.Error(t, err)

	// Query with nil request
	_, err = querier.ProviderWithdrawable(f.Ctx, nil)
	require.Error(t, err)
}
