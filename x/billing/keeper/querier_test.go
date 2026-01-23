/*
Package keeper_test contains unit tests for the billing module querier.

Test Coverage:
- QueryParams: parameter queries
- QueryLease: single lease queries
- QueryLeases: paginated lease queries with state filter
- QueryLeasesByTenant: tenant-indexed lease queries
- QueryLeasesByProvider: provider-indexed lease queries
- QueryLeasesBySKU: SKU-based lease queries with state filter
- QueryCreditAccount: credit account queries
- QueryCreditAccounts: paginated credit account list queries
- QueryCreditAddress: credit address derivation queries
- QueryCreditEstimate: credit duration estimation queries
- QueryWithdrawableAmount: per-lease withdrawable amount with accrual calculation
- QueryProviderWithdrawable: provider total withdrawable across all leases
*/
package keeper_test

import (
	"fmt"
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

	leaseUUID := "01912345-6789-7abc-8def-0123456789ab"

	// Query non-existent lease
	_, err := querier.Lease(f.Ctx, &types.QueryLeaseRequest{LeaseUuid: leaseUUID})
	require.Error(t, err)

	// Create a lease
	lease := types.Lease{
		Uuid:         leaseUUID,
		Tenant:       tenant.String(),
		ProviderUuid: "01912345-6789-7abc-8def-0123456789ac",
		Items: []types.LeaseItem{
			{
				SkuUuid:     "01912345-6789-7abc-8def-0123456789ad",
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
	resp, err := querier.Lease(f.Ctx, &types.QueryLeaseRequest{LeaseUuid: leaseUUID})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, lease.Uuid, resp.Lease.Uuid)
	require.Equal(t, lease.Tenant, resp.Lease.Tenant)
	require.Equal(t, lease.ProviderUuid, resp.Lease.ProviderUuid)

	// Query with empty lease_uuid
	_, err = querier.Lease(f.Ctx, &types.QueryLeaseRequest{LeaseUuid: ""})
	require.Error(t, err)

	// Query with nil request
	_, err = querier.Lease(f.Ctx, nil)
	require.Error(t, err)
}

func TestQueryLeases(t *testing.T) {
	f := initFixture(t)

	k := f.App.BillingKeeper
	querier := keeper.NewQuerier(k)

	providerUUID := testProviderUUID

	// Create multiple leases
	for i := 1; i <= 5; i++ {
		state := types.LEASE_STATE_ACTIVE
		var closedAt *time.Time
		if i%2 == 0 {
			state = types.LEASE_STATE_CLOSED
			ct := f.Ctx.BlockTime()
			closedAt = &ct
		}

		leaseUUID := fmt.Sprintf("01912345-6789-7abc-8def-%012d", i)
		skuUUID := fmt.Sprintf("01912345-6789-7abc-8def-1%011d", i)

		lease := types.Lease{
			Uuid:         leaseUUID,
			Tenant:       f.TestAccs[0].String(),
			ProviderUuid: providerUUID,
			Items: []types.LeaseItem{
				{
					SkuUuid:     skuUUID,
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
	resp, err = querier.Leases(f.Ctx, &types.QueryLeasesRequest{StateFilter: types.LEASE_STATE_ACTIVE})
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

	providerUUID := "01912345-6789-7abc-8def-0123456789ac"

	// Create leases for tenant1
	for i := 1; i <= 3; i++ {
		leaseUUID := fmt.Sprintf("01912345-6789-7abc-8def-%012d", i)
		skuUUID := fmt.Sprintf("01912345-6789-7abc-8def-1%011d", i)

		lease := types.Lease{
			Uuid:         leaseUUID,
			Tenant:       tenant1.String(),
			ProviderUuid: providerUUID,
			Items: []types.LeaseItem{
				{
					SkuUuid:     skuUUID,
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
		Uuid:         "01912345-6789-7abc-8def-000000000004",
		Tenant:       tenant1.String(),
		ProviderUuid: providerUUID,
		Items: []types.LeaseItem{
			{
				SkuUuid:     "01912345-6789-7abc-8def-100000000004",
				Quantity:    1,
				LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(100)),
			},
		},
		State:     types.LEASE_STATE_CLOSED,
		CreatedAt: f.Ctx.BlockTime(),
		ClosedAt:  &closedAt,
	}
	err := k.SetLease(f.Ctx, inactiveLease)
	require.NoError(t, err)

	// Create leases for tenant2
	providerUUID2 := "01912345-6789-7abc-8def-0123456789ad"
	for i := 5; i <= 6; i++ {
		leaseUUID := fmt.Sprintf("01912345-6789-7abc-8def-%012d", i)
		skuUUID := fmt.Sprintf("01912345-6789-7abc-8def-1%011d", i)

		lease := types.Lease{
			Uuid:         leaseUUID,
			Tenant:       tenant2.String(),
			ProviderUuid: providerUUID2,
			Items: []types.LeaseItem{
				{
					SkuUuid:     skuUUID,
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
		Tenant:      tenant1.String(),
		StateFilter: types.LEASE_STATE_ACTIVE,
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

	providerUUID1 := "01912345-6789-7abc-8def-0123456789ac"
	providerUUID2 := "01912345-6789-7abc-8def-0123456789ad"

	// Create leases for provider 1
	for i := 1; i <= 4; i++ {
		leaseUUID := fmt.Sprintf("01912345-6789-7abc-8def-%012d", i)
		skuUUID := fmt.Sprintf("01912345-6789-7abc-8def-1%011d", i)

		lease := types.Lease{
			Uuid:         leaseUUID,
			Tenant:       f.TestAccs[0].String(),
			ProviderUuid: providerUUID1,
			Items: []types.LeaseItem{
				{
					SkuUuid:     skuUUID,
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
		Uuid:         "01912345-6789-7abc-8def-000000000005",
		Tenant:       f.TestAccs[0].String(),
		ProviderUuid: providerUUID1,
		Items: []types.LeaseItem{
			{
				SkuUuid:     "01912345-6789-7abc-8def-100000000005",
				Quantity:    1,
				LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(100)),
			},
		},
		State:     types.LEASE_STATE_CLOSED,
		CreatedAt: f.Ctx.BlockTime(),
		ClosedAt:  &closedAt,
	}
	err := k.SetLease(f.Ctx, inactiveLease)
	require.NoError(t, err)

	// Create leases for provider 2
	for i := 6; i <= 7; i++ {
		leaseUUID := fmt.Sprintf("01912345-6789-7abc-8def-%012d", i)
		skuUUID := fmt.Sprintf("01912345-6789-7abc-8def-1%011d", i)

		lease := types.Lease{
			Uuid:         leaseUUID,
			Tenant:       f.TestAccs[1].String(),
			ProviderUuid: providerUUID2,
			Items: []types.LeaseItem{
				{
					SkuUuid:     skuUUID,
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
		ProviderUuid: providerUUID1,
	})
	require.NoError(t, err)
	require.Len(t, resp.Leases, 5)

	// Query by provider 1 active only
	resp, err = querier.LeasesByProvider(f.Ctx, &types.QueryLeasesByProviderRequest{
		ProviderUuid: providerUUID1,
		StateFilter:  types.LEASE_STATE_ACTIVE,
	})
	require.NoError(t, err)
	require.Len(t, resp.Leases, 4)

	// Query by provider 2
	resp, err = querier.LeasesByProvider(f.Ctx, &types.QueryLeasesByProviderRequest{
		ProviderUuid: providerUUID2,
	})
	require.NoError(t, err)
	require.Len(t, resp.Leases, 2)

	// Query with empty provider_uuid
	_, err = querier.LeasesByProvider(f.Ctx, &types.QueryLeasesByProviderRequest{
		ProviderUuid: "",
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

	// Create provider and SKU with 3600 per hour = 1 per second
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

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

	leaseUUID := "01912345-6789-7abc-8def-0123456789ab"

	// Create a lease with quantity 2
	lease := types.Lease{
		Uuid:         leaseUUID,
		Tenant:       tenant.String(),
		ProviderUuid: provider.Uuid,
		Items: []types.LeaseItem{
			{
				SkuUuid:     sku.Uuid,
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
		LeaseUuid: leaseUUID,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.True(t, resp.Amounts.IsZero())

	// Advance block time by 100 seconds
	newCtx := f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(100 * time.Second))

	// Query withdrawable amount - should be 200 (1 per second * 2 quantity * 100 seconds)
	resp, err = querier.WithdrawableAmount(newCtx, &types.QueryWithdrawableAmountRequest{
		LeaseUuid: leaseUUID,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, denom, resp.Amounts[0].Denom)
	require.Equal(t, sdkmath.NewInt(200), resp.Amounts[0].Amount)

	// Query with empty lease_uuid
	_, err = querier.WithdrawableAmount(f.Ctx, &types.QueryWithdrawableAmountRequest{
		LeaseUuid: "",
	})
	require.Error(t, err)

	// Query non-existent lease
	_, err = querier.WithdrawableAmount(f.Ctx, &types.QueryWithdrawableAmountRequest{
		LeaseUuid: "01912345-6789-7abc-8def-999999999999",
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

	// Create provider and SKU with 3600 per hour = 1 per second
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

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
	for i := 1; i <= 3; i++ {
		leaseUUID := fmt.Sprintf("01912345-6789-7abc-8def-%012d", i)

		lease := types.Lease{
			Uuid:         leaseUUID,
			Tenant:       tenant.String(),
			ProviderUuid: provider.Uuid,
			Items: []types.LeaseItem{
				{
					SkuUuid:     sku.Uuid,
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
		Uuid:         "01912345-6789-7abc-8def-000000000004",
		Tenant:       tenant.String(),
		ProviderUuid: provider.Uuid,
		Items: []types.LeaseItem{
			{
				SkuUuid:     sku.Uuid,
				Quantity:    1,
				LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(1)),
			},
		},
		State:         types.LEASE_STATE_CLOSED,
		CreatedAt:     f.Ctx.BlockTime(),
		LastSettledAt: f.Ctx.BlockTime(),
		ClosedAt:      &closedAt,
	}
	err = k.SetLease(f.Ctx, inactiveLease)
	require.NoError(t, err)

	// Query at initial time - should be 0
	resp, err := querier.ProviderWithdrawable(f.Ctx, &types.QueryProviderWithdrawableRequest{
		ProviderUuid: provider.Uuid,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.True(t, resp.Amounts.IsZero())
	require.Equal(t, uint64(0), resp.LeaseCount) // No leases with withdrawable amounts yet

	// Advance block time by 100 seconds
	newCtx := f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(100 * time.Second))

	// Query provider withdrawable - should be 300 (1 per second * 1 quantity * 100 seconds * 3 active leases)
	resp, err = querier.ProviderWithdrawable(newCtx, &types.QueryProviderWithdrawableRequest{
		ProviderUuid: provider.Uuid,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, denom, resp.Amounts[0].Denom)
	require.Equal(t, sdkmath.NewInt(300), resp.Amounts[0].Amount)
	require.Equal(t, uint64(3), resp.LeaseCount) // Only active leases with withdrawable amounts

	// Query with empty provider_uuid
	_, err = querier.ProviderWithdrawable(f.Ctx, &types.QueryProviderWithdrawableRequest{
		ProviderUuid: "",
	})
	require.Error(t, err)

	// Query with nil request
	_, err = querier.ProviderWithdrawable(f.Ctx, nil)
	require.Error(t, err)

	// Test pagination with limit parameter
	t.Run("pagination with limit", func(t *testing.T) {
		// Query with limit=2 - should return partial results
		resp, err := querier.ProviderWithdrawable(newCtx, &types.QueryProviderWithdrawableRequest{
			ProviderUuid: provider.Uuid,
			Limit:        2,
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.True(t, resp.HasMore) // More leases exist beyond limit
		// Note: LeaseCount only counts leases with non-zero withdrawable that were processed
	})

	t.Run("no has_more when all leases processed", func(t *testing.T) {
		// Query with high limit - should process all leases
		resp, err := querier.ProviderWithdrawable(newCtx, &types.QueryProviderWithdrawableRequest{
			ProviderUuid: provider.Uuid,
			Limit:        100,
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.False(t, resp.HasMore) // All leases processed
		require.Equal(t, uint64(3), resp.LeaseCount)
	})

	t.Run("default limit applied when limit=0", func(t *testing.T) {
		// Query with limit=0 should use default (100)
		resp, err := querier.ProviderWithdrawable(newCtx, &types.QueryProviderWithdrawableRequest{
			ProviderUuid: provider.Uuid,
			Limit:        0,
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.False(t, resp.HasMore) // Default limit (100) is more than 4 leases
		require.Equal(t, uint64(3), resp.LeaseCount)
	})

	t.Run("limit capped at maximum", func(t *testing.T) {
		// Query with limit exceeding max should be capped
		resp, err := querier.ProviderWithdrawable(newCtx, &types.QueryProviderWithdrawableRequest{
			ProviderUuid: provider.Uuid,
			Limit:        10000, // Exceeds MaxProviderWithdrawableQueryLimit (1000)
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		// Should still work, just capped at max
		require.Equal(t, uint64(3), resp.LeaseCount)
	})
}

func TestQueryCreditAccounts(t *testing.T) {
	f := initFixture(t)

	k := f.App.BillingKeeper
	querier := keeper.NewQuerier(k)

	t.Run("empty result when no credit accounts", func(t *testing.T) {
		resp, err := querier.CreditAccounts(f.Ctx, &types.QueryCreditAccountsRequest{})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Empty(t, resp.CreditAccounts)
	})

	t.Run("returns credit accounts with pagination", func(t *testing.T) {
		// Create credit accounts for multiple tenants
		tenants := f.TestAccs[:3]
		for _, tenant := range tenants {
			creditAddr := types.DeriveCreditAddress(tenant)
			ca := types.CreditAccount{
				Tenant:           tenant.String(),
				CreditAddress:    creditAddr.String(),
				ActiveLeaseCount: 1,
			}
			err := k.SetCreditAccount(f.Ctx, ca)
			require.NoError(t, err)
		}

		// Query all
		resp, err := querier.CreditAccounts(f.Ctx, &types.QueryCreditAccountsRequest{})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Len(t, resp.CreditAccounts, 3)

		// Query with pagination
		resp, err = querier.CreditAccounts(f.Ctx, &types.QueryCreditAccountsRequest{
			Pagination: &query.PageRequest{Limit: 2},
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Len(t, resp.CreditAccounts, 2)
		require.NotNil(t, resp.Pagination)
	})

	t.Run("nil request returns error", func(t *testing.T) {
		_, err := querier.CreditAccounts(f.Ctx, nil)
		require.Error(t, err)
	})
}

func TestQueryLeasesBySKU(t *testing.T) {
	f := initFixture(t)

	k := f.App.BillingKeeper
	querier := keeper.NewQuerier(k)

	tenant := f.TestAccs[0]
	providerUUID := testProviderUUID
	skuUUID1 := "01912345-6789-7abc-8def-sku000000001"
	skuUUID2 := "01912345-6789-7abc-8def-sku000000002"

	t.Run("empty result when no leases exist", func(t *testing.T) {
		resp, err := querier.LeasesBySKU(f.Ctx, &types.QueryLeasesBySKURequest{
			SkuUuid: skuUUID1,
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Empty(t, resp.Leases)
	})

	t.Run("returns leases containing the SKU", func(t *testing.T) {
		// Create leases with different SKUs
		lease1 := types.Lease{
			Uuid:         "01912345-6789-7abc-8def-lease0000001",
			Tenant:       tenant.String(),
			ProviderUuid: providerUUID,
			Items: []types.LeaseItem{
				{SkuUuid: skuUUID1, Quantity: 1, LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(100))},
			},
			State:     types.LEASE_STATE_ACTIVE,
			CreatedAt: f.Ctx.BlockTime(),
		}
		lease2 := types.Lease{
			Uuid:         "01912345-6789-7abc-8def-lease0000002",
			Tenant:       tenant.String(),
			ProviderUuid: providerUUID,
			Items: []types.LeaseItem{
				{SkuUuid: skuUUID1, Quantity: 2, LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(100))},
				{SkuUuid: skuUUID2, Quantity: 1, LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(200))},
			},
			State:     types.LEASE_STATE_CLOSED,
			CreatedAt: f.Ctx.BlockTime(),
		}
		lease3 := types.Lease{
			Uuid:         "01912345-6789-7abc-8def-lease0000003",
			Tenant:       tenant.String(),
			ProviderUuid: providerUUID,
			Items: []types.LeaseItem{
				{SkuUuid: skuUUID2, Quantity: 1, LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(200))},
			},
			State:     types.LEASE_STATE_ACTIVE,
			CreatedAt: f.Ctx.BlockTime(),
		}

		require.NoError(t, k.SetLease(f.Ctx, lease1))
		require.NoError(t, k.SetLease(f.Ctx, lease2))
		require.NoError(t, k.SetLease(f.Ctx, lease3))

		// Query for skuUUID1 - should return lease1 and lease2
		resp, err := querier.LeasesBySKU(f.Ctx, &types.QueryLeasesBySKURequest{
			SkuUuid: skuUUID1,
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Len(t, resp.Leases, 2)

		// Query for skuUUID2 - should return lease2 and lease3
		resp, err = querier.LeasesBySKU(f.Ctx, &types.QueryLeasesBySKURequest{
			SkuUuid: skuUUID2,
		})
		require.NoError(t, err)
		require.Len(t, resp.Leases, 2)
	})

	t.Run("state filter works", func(t *testing.T) {
		// Query for skuUUID1 with active state filter
		resp, err := querier.LeasesBySKU(f.Ctx, &types.QueryLeasesBySKURequest{
			SkuUuid:     skuUUID1,
			StateFilter: types.LEASE_STATE_ACTIVE,
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Len(t, resp.Leases, 1)
		require.Equal(t, types.LEASE_STATE_ACTIVE, resp.Leases[0].State)

		// Query for skuUUID1 with closed state filter
		resp, err = querier.LeasesBySKU(f.Ctx, &types.QueryLeasesBySKURequest{
			SkuUuid:     skuUUID1,
			StateFilter: types.LEASE_STATE_CLOSED,
		})
		require.NoError(t, err)
		require.Len(t, resp.Leases, 1)
		require.Equal(t, types.LEASE_STATE_CLOSED, resp.Leases[0].State)
	})

	t.Run("error cases", func(t *testing.T) {
		// Nil request
		_, err := querier.LeasesBySKU(f.Ctx, nil)
		require.Error(t, err)

		// Empty sku_uuid
		_, err = querier.LeasesBySKU(f.Ctx, &types.QueryLeasesBySKURequest{
			SkuUuid: "",
		})
		require.Error(t, err)
	})
}

// TestQueryLeasesBySKUPaginationEdgeCases tests edge cases in LeasesBySKU pagination.
func TestQueryLeasesBySKUPaginationEdgeCases(t *testing.T) {
	f := initFixture(t)

	k := f.App.BillingKeeper
	querier := keeper.NewQuerier(k)

	tenant := f.TestAccs[0]
	providerUUID := testProviderUUID
	skuUUID := "01912345-6789-7abc-8def-skupage00001"

	// Create 5 leases with the same SKU
	for i := 0; i < 5; i++ {
		lease := types.Lease{
			Uuid:         fmt.Sprintf("01912345-6789-7abc-8def-leasepage0%02d", i+1),
			Tenant:       tenant.String(),
			ProviderUuid: providerUUID,
			Items: []types.LeaseItem{
				{SkuUuid: skuUUID, Quantity: 1, LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(100))},
			},
			State:     types.LEASE_STATE_ACTIVE,
			CreatedAt: f.Ctx.BlockTime(),
		}
		require.NoError(t, k.SetLease(f.Ctx, lease))
	}

	t.Run("offset exceeds total results returns empty", func(t *testing.T) {
		resp, err := querier.LeasesBySKU(f.Ctx, &types.QueryLeasesBySKURequest{
			SkuUuid: skuUUID,
			Pagination: &query.PageRequest{
				Offset: 100, // Far exceeds 5 leases
				Limit:  10,
			},
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Empty(t, resp.Leases, "should return empty when offset exceeds total")
	})

	t.Run("limit of 0 uses default", func(t *testing.T) {
		resp, err := querier.LeasesBySKU(f.Ctx, &types.QueryLeasesBySKURequest{
			SkuUuid: skuUUID,
			Pagination: &query.PageRequest{
				Limit: 0, // Should use default
			},
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Len(t, resp.Leases, 5, "should return all 5 leases with default limit")
	})

	t.Run("pagination with limit works", func(t *testing.T) {
		resp, err := querier.LeasesBySKU(f.Ctx, &types.QueryLeasesBySKURequest{
			SkuUuid: skuUUID,
			Pagination: &query.PageRequest{
				Limit: 2,
			},
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Len(t, resp.Leases, 2, "should return only 2 leases")
		require.NotNil(t, resp.Pagination, "should have pagination response")
	})

	t.Run("pagination with offset and limit works", func(t *testing.T) {
		resp, err := querier.LeasesBySKU(f.Ctx, &types.QueryLeasesBySKURequest{
			SkuUuid: skuUUID,
			Pagination: &query.PageRequest{
				Offset: 2,
				Limit:  2,
			},
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Len(t, resp.Leases, 2, "should return 2 leases starting from offset 2")
	})

	t.Run("offset at boundary returns remaining", func(t *testing.T) {
		resp, err := querier.LeasesBySKU(f.Ctx, &types.QueryLeasesBySKURequest{
			SkuUuid: skuUUID,
			Pagination: &query.PageRequest{
				Offset: 4, // 5 total, offset 4 = last one
				Limit:  10,
			},
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Len(t, resp.Leases, 1, "should return 1 lease at boundary")
	})

	t.Run("empty SKU index with pagination", func(t *testing.T) {
		nonExistentSKU := "01912345-6789-7abc-8def-skunotexist1"
		resp, err := querier.LeasesBySKU(f.Ctx, &types.QueryLeasesBySKURequest{
			SkuUuid: nonExistentSKU,
			Pagination: &query.PageRequest{
				Offset: 0,
				Limit:  10,
			},
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Empty(t, resp.Leases, "should return empty for non-existent SKU")
	})
}

func TestQueryCreditEstimate(t *testing.T) {
	f := initFixture(t)

	k := f.App.BillingKeeper
	querier := keeper.NewQuerier(k)

	tenant := f.TestAccs[0]
	providerUUID := testProviderUUID

	t.Run("error when credit account not found", func(t *testing.T) {
		_, err := querier.CreditEstimate(f.Ctx, &types.QueryCreditEstimateRequest{
			Tenant: tenant.String(),
		})
		require.Error(t, err)
	})

	t.Run("zero estimate with no active leases", func(t *testing.T) {
		// Create credit account with balance
		creditAddr := types.DeriveCreditAddress(tenant)
		ca := types.CreditAccount{
			Tenant:           tenant.String(),
			CreditAddress:    creditAddr.String(),
			ActiveLeaseCount: 0,
		}
		require.NoError(t, k.SetCreditAccount(f.Ctx, ca))

		// Fund the credit account using the test fixture helper
		fundCoins := sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(1000000)))
		f.fundAccount(t, creditAddr, fundCoins)

		resp, err := querier.CreditEstimate(f.Ctx, &types.QueryCreditEstimateRequest{
			Tenant: tenant.String(),
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Equal(t, uint64(0), resp.ActiveLeaseCount)
		require.Equal(t, uint64(0), resp.EstimatedDurationSeconds)
		require.True(t, resp.TotalRatePerSecond.IsZero())
		require.True(t, resp.CurrentBalance.AmountOf(testDenom).Equal(sdkmath.NewInt(1000000)))
	})

	t.Run("calculates estimate with active leases", func(t *testing.T) {
		// Create an active lease with known rate
		// Rate: 100 per second, quantity 2 = 200 per second total
		lease := types.Lease{
			Uuid:         "01912345-6789-7abc-8def-estimate0001",
			Tenant:       tenant.String(),
			ProviderUuid: providerUUID,
			Items: []types.LeaseItem{
				{
					SkuUuid:     "01912345-6789-7abc-8def-sku000000001",
					Quantity:    2,
					LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(100)), // 100 per second
				},
			},
			State:         types.LEASE_STATE_ACTIVE,
			CreatedAt:     f.Ctx.BlockTime(),
			LastSettledAt: f.Ctx.BlockTime(),
		}
		require.NoError(t, k.SetLease(f.Ctx, lease))

		// Update credit account lease count
		ca, err := k.GetCreditAccount(f.Ctx, tenant.String())
		require.NoError(t, err)
		ca.ActiveLeaseCount = 1
		require.NoError(t, k.SetCreditAccount(f.Ctx, ca))

		resp, err := querier.CreditEstimate(f.Ctx, &types.QueryCreditEstimateRequest{
			Tenant: tenant.String(),
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Equal(t, uint64(1), resp.ActiveLeaseCount)

		// Rate should be 200 per second (100 * 2 quantity)
		require.Equal(t, sdkmath.NewInt(200), resp.TotalRatePerSecond.AmountOf(testDenom))

		// With 1,000,000 balance and 200/second rate, should last 5000 seconds
		require.Equal(t, uint64(5000), resp.EstimatedDurationSeconds)
	})

	t.Run("error cases", func(t *testing.T) {
		// Nil request
		_, err := querier.CreditEstimate(f.Ctx, nil)
		require.Error(t, err)

		// Empty tenant
		_, err = querier.CreditEstimate(f.Ctx, &types.QueryCreditEstimateRequest{
			Tenant: "",
		})
		require.Error(t, err)

		// Invalid tenant address
		_, err = querier.CreditEstimate(f.Ctx, &types.QueryCreditEstimateRequest{
			Tenant: "invalid-address",
		})
		require.Error(t, err)
	})
}

// TestQueryErrorCasesComprehensive tests additional error cases across queries
// including invalid UUID formats, non-existent resources, and malformed requests.
func TestQueryErrorCasesComprehensive(t *testing.T) {
	f := initFixture(t)

	k := f.App.BillingKeeper
	querier := keeper.NewQuerier(k)

	t.Run("Lease query with invalid UUID format", func(t *testing.T) {
		// Not a valid UUIDv7 format
		_, err := querier.Lease(f.Ctx, &types.QueryLeaseRequest{
			LeaseUuid: "not-a-valid-uuid",
		})
		require.Error(t, err)

		// Too short
		_, err = querier.Lease(f.Ctx, &types.QueryLeaseRequest{
			LeaseUuid: "12345",
		})
		require.Error(t, err)
	})

	t.Run("LeasesByProvider with empty/invalid UUID", func(t *testing.T) {
		// Empty provider_uuid should error
		_, err := querier.LeasesByProvider(f.Ctx, &types.QueryLeasesByProviderRequest{
			ProviderUuid: "",
		})
		require.Error(t, err, "empty provider_uuid should error")

		// Invalid UUID format returns empty results (no format validation)
		resp, err := querier.LeasesByProvider(f.Ctx, &types.QueryLeasesByProviderRequest{
			ProviderUuid: "invalid-uuid-format",
		})
		require.NoError(t, err)
		require.Empty(t, resp.Leases, "invalid UUID should return empty results")
	})

	t.Run("WithdrawableAmount with empty/invalid UUID", func(t *testing.T) {
		// Empty lease_uuid should error
		_, err := querier.WithdrawableAmount(f.Ctx, &types.QueryWithdrawableAmountRequest{
			LeaseUuid: "",
		})
		require.Error(t, err, "empty lease_uuid should error")

		// Invalid UUID format should error (does format validation)
		_, err = querier.WithdrawableAmount(f.Ctx, &types.QueryWithdrawableAmountRequest{
			LeaseUuid: "not-valid",
		})
		require.Error(t, err, "invalid UUID format should error")
	})

	t.Run("ProviderWithdrawable with empty/invalid UUID", func(t *testing.T) {
		// Empty provider_uuid should error
		_, err := querier.ProviderWithdrawable(f.Ctx, &types.QueryProviderWithdrawableRequest{
			ProviderUuid: "",
		})
		require.Error(t, err, "empty provider_uuid should error")

		// Invalid UUID format returns empty results (no format validation)
		resp, err := querier.ProviderWithdrawable(f.Ctx, &types.QueryProviderWithdrawableRequest{
			ProviderUuid: "bad-uuid",
		})
		require.NoError(t, err)
		require.True(t, resp.Amounts.IsZero(), "invalid UUID should return zero amounts")
	})

	t.Run("LeasesBySKU with empty/invalid UUID", func(t *testing.T) {
		// Empty sku_uuid should error
		_, err := querier.LeasesBySKU(f.Ctx, &types.QueryLeasesBySKURequest{
			SkuUuid: "",
		})
		require.Error(t, err, "empty sku_uuid should error")

		// Invalid UUID format returns empty results (no format validation)
		resp, err := querier.LeasesBySKU(f.Ctx, &types.QueryLeasesBySKURequest{
			SkuUuid: "invalid",
		})
		require.NoError(t, err)
		require.Empty(t, resp.Leases, "invalid UUID should return empty results")
	})

	t.Run("non-existent resources return appropriate errors", func(t *testing.T) {
		// Non-existent lease (valid UUID format but doesn't exist)
		nonExistentUUID := "01912345-6789-7abc-8def-999999999999"
		_, err := querier.Lease(f.Ctx, &types.QueryLeaseRequest{
			LeaseUuid: nonExistentUUID,
		})
		require.Error(t, err, "should error for non-existent lease")

		// Non-existent credit account (valid address but no account)
		validButNonExistent := f.TestAccs[4].String()
		_, err = querier.CreditAccount(f.Ctx, &types.QueryCreditAccountRequest{
			Tenant: validButNonExistent,
		})
		require.Error(t, err, "should error for non-existent credit account")

		// Non-existent lease for withdrawable amount
		_, err = querier.WithdrawableAmount(f.Ctx, &types.QueryWithdrawableAmountRequest{
			LeaseUuid: nonExistentUUID,
		})
		require.Error(t, err, "should error for withdrawable on non-existent lease")
	})

	t.Run("queries with valid but empty results", func(t *testing.T) {
		// Provider with no leases (valid UUID format)
		validProviderUUID := "01912345-6789-7abc-8def-000000000001"
		resp, err := querier.LeasesByProvider(f.Ctx, &types.QueryLeasesByProviderRequest{
			ProviderUuid: validProviderUUID,
		})
		require.NoError(t, err, "should not error for provider with no leases")
		require.Empty(t, resp.Leases, "should return empty list")

		// Tenant with no leases (valid address)
		resp2, err := querier.LeasesByTenant(f.Ctx, &types.QueryLeasesByTenantRequest{
			Tenant: f.TestAccs[4].String(),
		})
		require.NoError(t, err, "should not error for tenant with no leases")
		require.Empty(t, resp2.Leases, "should return empty list")

		// SKU with no leases
		validSKUUUID := "01912345-6789-7abc-8def-000000000002"
		resp3, err := querier.LeasesBySKU(f.Ctx, &types.QueryLeasesBySKURequest{
			SkuUuid: validSKUUUID,
		})
		require.NoError(t, err, "should not error for SKU with no leases")
		require.Empty(t, resp3.Leases, "should return empty list")
	})
}
