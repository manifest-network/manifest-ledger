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
- QueryCreditEstimate: credit duration estimation from streams, net of unsettled accrual
- QueryProviderWithdrawable: provider total withdrawable across its streams
- QueryStreamsByProvider: stream listing, pagination and input validation
- QueryStreamsByTenant: stream listing, pagination and input validation

The per-lease QueryWithdrawableAmount is deprecated and returns Unimplemented;
that is covered by TestStream_DeprecatedPerLeaseSurface in stream_test.go.
*/
package keeper_test

import (
	"cmp"
	"encoding/binary"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"cosmossdk.io/collections"
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

	providerUUID := testLeaseUUID2

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

	providerUUID1 := testLeaseUUID2
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

func TestQueryLeasesReverse(t *testing.T) {
	f := initFixture(t)

	k := f.App.BillingKeeper
	querier := keeper.NewQuerier(k)

	providerUUID := testProviderUUID

	// Create 5 active leases with ordered UUIDs
	for i := 1; i <= 5; i++ {
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
			State:     types.LEASE_STATE_ACTIVE,
			CreatedAt: f.Ctx.BlockTime(),
		}
		err := k.SetLease(f.Ctx, lease)
		require.NoError(t, err)
	}

	t.Run("reverse with state filter", func(t *testing.T) {
		respFwd, err := querier.Leases(f.Ctx, &types.QueryLeasesRequest{
			StateFilter: types.LEASE_STATE_ACTIVE,
		})
		require.NoError(t, err)
		require.Len(t, respFwd.Leases, 5)

		respRev, err := querier.Leases(f.Ctx, &types.QueryLeasesRequest{
			StateFilter: types.LEASE_STATE_ACTIVE,
			Pagination:  &query.PageRequest{Reverse: true},
		})
		require.NoError(t, err)
		require.Len(t, respRev.Leases, 5)

		for i := range respFwd.Leases {
			require.Equal(t, respFwd.Leases[i].Uuid, respRev.Leases[len(respRev.Leases)-1-i].Uuid)
		}
	})
}

func TestQueryLeasesByTenantReverse(t *testing.T) {
	f := initFixture(t)

	k := f.App.BillingKeeper
	querier := keeper.NewQuerier(k)

	tenant := f.TestAccs[0]
	providerUUID := testLeaseUUID2

	// Create 4 leases for tenant (3 active, 1 closed)
	for i := 1; i <= 3; i++ {
		leaseUUID := fmt.Sprintf("01912345-6789-7abc-8def-%012d", i)
		skuUUID := fmt.Sprintf("01912345-6789-7abc-8def-1%011d", i)

		lease := types.Lease{
			Uuid:         leaseUUID,
			Tenant:       tenant.String(),
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

	closedAt := f.Ctx.BlockTime()
	err := k.SetLease(f.Ctx, types.Lease{
		Uuid:         "01912345-6789-7abc-8def-000000000004",
		Tenant:       tenant.String(),
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
	})
	require.NoError(t, err)

	t.Run("reverse without state filter", func(t *testing.T) {
		respFwd, err := querier.LeasesByTenant(f.Ctx, &types.QueryLeasesByTenantRequest{
			Tenant: tenant.String(),
		})
		require.NoError(t, err)
		require.Len(t, respFwd.Leases, 4)

		respRev, err := querier.LeasesByTenant(f.Ctx, &types.QueryLeasesByTenantRequest{
			Tenant:     tenant.String(),
			Pagination: &query.PageRequest{Reverse: true},
		})
		require.NoError(t, err)
		require.Len(t, respRev.Leases, 4)

		for i := range respFwd.Leases {
			require.Equal(t, respFwd.Leases[i].Uuid, respRev.Leases[len(respRev.Leases)-1-i].Uuid)
		}
	})

	t.Run("reverse with state filter", func(t *testing.T) {
		respFwd, err := querier.LeasesByTenant(f.Ctx, &types.QueryLeasesByTenantRequest{
			Tenant:      tenant.String(),
			StateFilter: types.LEASE_STATE_ACTIVE,
		})
		require.NoError(t, err)
		require.Len(t, respFwd.Leases, 3)

		respRev, err := querier.LeasesByTenant(f.Ctx, &types.QueryLeasesByTenantRequest{
			Tenant:      tenant.String(),
			StateFilter: types.LEASE_STATE_ACTIVE,
			Pagination:  &query.PageRequest{Reverse: true},
		})
		require.NoError(t, err)
		require.Len(t, respRev.Leases, 3)

		for i := range respFwd.Leases {
			require.Equal(t, respFwd.Leases[i].Uuid, respRev.Leases[len(respRev.Leases)-1-i].Uuid)
		}
	})

	t.Run("reverse with limit and cursor", func(t *testing.T) {
		// 4 leases total. Reverse with limit=2 should paginate: [3,2], [1,closed4]
		// (reverse of forward order: [closed4, 1, 2, 3])
		page1, err := querier.LeasesByTenant(f.Ctx, &types.QueryLeasesByTenantRequest{
			Tenant:     tenant.String(),
			Pagination: &query.PageRequest{Reverse: true, Limit: 2},
		})
		require.NoError(t, err)
		require.Len(t, page1.Leases, 2)
		require.NotEmpty(t, page1.Pagination.NextKey, "should have a next page cursor")

		page2, err := querier.LeasesByTenant(f.Ctx, &types.QueryLeasesByTenantRequest{
			Tenant:     tenant.String(),
			Pagination: &query.PageRequest{Reverse: true, Key: page1.Pagination.NextKey, Limit: 2},
		})
		require.NoError(t, err)
		require.Len(t, page2.Leases, 2)
		require.Empty(t, page2.Pagination.NextKey, "last page should have no next cursor")

		// Combine pages and verify they are the full reversed forward set
		respFwd, err := querier.LeasesByTenant(f.Ctx, &types.QueryLeasesByTenantRequest{
			Tenant: tenant.String(),
		})
		require.NoError(t, err)
		require.Len(t, respFwd.Leases, 4)

		allRev := slices.Concat(page1.Leases, page2.Leases)
		for i := range respFwd.Leases {
			require.Equal(t, respFwd.Leases[i].Uuid, allRev[len(allRev)-1-i].Uuid)
		}
	})
}

func TestQueryLeasesByProviderReverse(t *testing.T) {
	f := initFixture(t)

	k := f.App.BillingKeeper
	querier := keeper.NewQuerier(k)

	providerUUID := testLeaseUUID2

	// Create 4 active leases + 1 closed
	for i := 1; i <= 4; i++ {
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
			State:     types.LEASE_STATE_ACTIVE,
			CreatedAt: f.Ctx.BlockTime(),
		}
		err := k.SetLease(f.Ctx, lease)
		require.NoError(t, err)
	}

	closedAt := f.Ctx.BlockTime()
	err := k.SetLease(f.Ctx, types.Lease{
		Uuid:         "01912345-6789-7abc-8def-000000000005",
		Tenant:       f.TestAccs[0].String(),
		ProviderUuid: providerUUID,
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
	})
	require.NoError(t, err)

	t.Run("reverse without state filter", func(t *testing.T) {
		respFwd, err := querier.LeasesByProvider(f.Ctx, &types.QueryLeasesByProviderRequest{
			ProviderUuid: providerUUID,
		})
		require.NoError(t, err)
		require.Len(t, respFwd.Leases, 5)

		respRev, err := querier.LeasesByProvider(f.Ctx, &types.QueryLeasesByProviderRequest{
			ProviderUuid: providerUUID,
			Pagination:   &query.PageRequest{Reverse: true},
		})
		require.NoError(t, err)
		require.Len(t, respRev.Leases, 5)

		for i := range respFwd.Leases {
			require.Equal(t, respFwd.Leases[i].Uuid, respRev.Leases[len(respRev.Leases)-1-i].Uuid)
		}
	})

	t.Run("reverse with state filter", func(t *testing.T) {
		respFwd, err := querier.LeasesByProvider(f.Ctx, &types.QueryLeasesByProviderRequest{
			ProviderUuid: providerUUID,
			StateFilter:  types.LEASE_STATE_ACTIVE,
		})
		require.NoError(t, err)
		require.Len(t, respFwd.Leases, 4)

		respRev, err := querier.LeasesByProvider(f.Ctx, &types.QueryLeasesByProviderRequest{
			ProviderUuid: providerUUID,
			StateFilter:  types.LEASE_STATE_ACTIVE,
			Pagination:   &query.PageRequest{Reverse: true},
		})
		require.NoError(t, err)
		require.Len(t, respRev.Leases, 4)

		for i := range respFwd.Leases {
			require.Equal(t, respFwd.Leases[i].Uuid, respRev.Leases[len(respRev.Leases)-1-i].Uuid)
		}
	})
}

func TestQueryLeasesBySKUReverse(t *testing.T) {
	f := initFixture(t)

	k := f.App.BillingKeeper
	querier := keeper.NewQuerier(k)

	tenant := f.TestAccs[0]
	providerUUID := testProviderUUID
	skuUUID := "01912345-6789-7abc-8def-sku000000001"

	// Create 5 leases with the same SKU for pagination testing
	for i := 1; i <= 5; i++ {
		leaseUUID := fmt.Sprintf("01912345-6789-7abc-8def-lease00000%02d", i)
		lease := types.Lease{
			Uuid:         leaseUUID,
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

	t.Run("full reverse", func(t *testing.T) {
		respFwd, err := querier.LeasesBySKU(f.Ctx, &types.QueryLeasesBySKURequest{
			SkuUuid: skuUUID,
		})
		require.NoError(t, err)
		require.Len(t, respFwd.Leases, 5)

		respRev, err := querier.LeasesBySKU(f.Ctx, &types.QueryLeasesBySKURequest{
			SkuUuid:    skuUUID,
			Pagination: &query.PageRequest{Reverse: true},
		})
		require.NoError(t, err)
		require.Len(t, respRev.Leases, 5)

		for i := range respFwd.Leases {
			require.Equal(t, respFwd.Leases[i].Uuid, respRev.Leases[len(respRev.Leases)-1-i].Uuid)
		}
	})

	t.Run("reverse with limit and cursor", func(t *testing.T) {
		// Page through all 5 leases in reverse with limit=2
		page1, err := querier.LeasesBySKU(f.Ctx, &types.QueryLeasesBySKURequest{
			SkuUuid:    skuUUID,
			Pagination: &query.PageRequest{Reverse: true, Limit: 2},
		})
		require.NoError(t, err)
		require.Len(t, page1.Leases, 2)
		require.NotEmpty(t, page1.Pagination.NextKey)

		page2, err := querier.LeasesBySKU(f.Ctx, &types.QueryLeasesBySKURequest{
			SkuUuid:    skuUUID,
			Pagination: &query.PageRequest{Reverse: true, Key: page1.Pagination.NextKey, Limit: 2},
		})
		require.NoError(t, err)
		require.Len(t, page2.Leases, 2)
		require.NotEmpty(t, page2.Pagination.NextKey)

		page3, err := querier.LeasesBySKU(f.Ctx, &types.QueryLeasesBySKURequest{
			SkuUuid:    skuUUID,
			Pagination: &query.PageRequest{Reverse: true, Key: page2.Pagination.NextKey, Limit: 2},
		})
		require.NoError(t, err)
		require.Len(t, page3.Leases, 1)
		require.Empty(t, page3.Pagination.NextKey, "last page should have no next cursor")

		// Combine all pages and verify they match the full reverse set
		respFwd, err := querier.LeasesBySKU(f.Ctx, &types.QueryLeasesBySKURequest{
			SkuUuid: skuUUID,
		})
		require.NoError(t, err)

		allRev := append(append(page1.Leases, page2.Leases...), page3.Leases...)
		require.Len(t, allRev, len(respFwd.Leases))
		for i := range respFwd.Leases {
			require.Equal(t, respFwd.Leases[i].Uuid, allRev[len(allRev)-1-i].Uuid)
		}
	})
}

func TestQueryLeasesBySKUCountTotal(t *testing.T) {
	f := initFixture(t)

	k := f.App.BillingKeeper
	querier := keeper.NewQuerier(k)

	tenant := f.TestAccs[0]
	providerUUID := testProviderUUID
	skuUUID := "01912345-6789-7abc-8def-sku00count01"

	// Create 5 leases with the same SKU
	for i := 1; i <= 5; i++ {
		leaseUUID := fmt.Sprintf("01912345-6789-7abc-8def-leasecount%02d", i)
		lease := types.Lease{
			Uuid:         leaseUUID,
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

	t.Run("countTotal with limit", func(t *testing.T) {
		resp, err := querier.LeasesBySKU(f.Ctx, &types.QueryLeasesBySKURequest{
			SkuUuid:    skuUUID,
			Pagination: &query.PageRequest{Limit: 2, CountTotal: true},
		})
		require.NoError(t, err)
		require.Len(t, resp.Leases, 2)
		require.Equal(t, uint64(5), resp.Pagination.Total)
		require.NotEmpty(t, resp.Pagination.NextKey)
	})

	t.Run("countTotal cursor continues correctly", func(t *testing.T) {
		// Page 1 with countTotal
		page1, err := querier.LeasesBySKU(f.Ctx, &types.QueryLeasesBySKURequest{
			SkuUuid:    skuUUID,
			Pagination: &query.PageRequest{Limit: 2, CountTotal: true},
		})
		require.NoError(t, err)
		require.Len(t, page1.Leases, 2)
		require.NotEmpty(t, page1.Pagination.NextKey)

		// Page 2 using cursor from page 1
		page2, err := querier.LeasesBySKU(f.Ctx, &types.QueryLeasesBySKURequest{
			SkuUuid:    skuUUID,
			Pagination: &query.PageRequest{Limit: 2, Key: page1.Pagination.NextKey},
		})
		require.NoError(t, err)
		require.Len(t, page2.Leases, 2)
		require.NotEmpty(t, page2.Pagination.NextKey)

		// Page 3
		page3, err := querier.LeasesBySKU(f.Ctx, &types.QueryLeasesBySKURequest{
			SkuUuid:    skuUUID,
			Pagination: &query.PageRequest{Limit: 2, Key: page2.Pagination.NextKey},
		})
		require.NoError(t, err)
		require.Len(t, page3.Leases, 1)
		require.Empty(t, page3.Pagination.NextKey)

		// Verify all 5 leases were returned across pages with no duplicates
		allUUIDs := make(map[string]bool)
		for _, l := range page1.Leases {
			allUUIDs[l.Uuid] = true
		}
		for _, l := range page2.Leases {
			allUUIDs[l.Uuid] = true
		}
		for _, l := range page3.Leases {
			allUUIDs[l.Uuid] = true
		}
		require.Len(t, allUUIDs, 5, "all 5 leases should be returned across pages with no duplicates")
	})
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

// queryTestTenant returns a tenant address whose byte order follows i, so that
// streams, which are keyed by (provider, tenant, denom), iterate in a known order.
func queryTestTenant(i uint32) sdk.AccAddress {
	addr := make([]byte, 20)
	binary.BigEndian.PutUint32(addr[16:], i)
	return addr
}

// queryProviderWithdrawable runs the ProviderWithdrawable query at ctx.
func queryProviderWithdrawable(
	t *testing.T, querier keeper.Querier, ctx sdk.Context, providerUUID string, page *query.PageRequest,
) *types.QueryProviderWithdrawableResponse {
	t.Helper()
	resp, err := querier.ProviderWithdrawable(ctx, &types.QueryProviderWithdrawableRequest{
		ProviderUuid: providerUUID,
		Pagination:   page,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, resp.Pagination)
	return resp
}

// TestQueryProviderWithdrawable covers what the query reports for a provider's
// streams: the per-stream accrual capped at the tenant's credit, summed over the
// provider's streams only, with LeaseCount counting the leases of the streams
// that have something to withdraw. The per-lease WithdrawableAmount query is
// gone (see TestStream_DeprecatedPerLeaseSurface).
func TestQueryProviderWithdrawable(t *testing.T) {
	s := newStreamFixture(t, testDenom)
	k := s.App.BillingKeeper
	querier := keeper.NewQuerier(k)

	// Tenants of the fixture provider:
	//   rich:   two leases (2/s) and plenty of credit
	//   poor:   one lease (1/s) and credit for exactly one hour
	//   closer: one lease, closed again, which leaves no stream behind
	rich, poor, closer := queryTestTenant(1), queryTestTenant(2), queryTestTenant(3)
	s.fundTenant(t, rich, testDenom, 1_000_000)
	s.fundTenant(t, poor, testDenom, 3600)
	s.fundTenant(t, closer, testDenom, 1_000_000)
	s.lease(t, rich)
	s.lease(t, rich)
	s.lease(t, poor)
	_, err := s.msgServer.CloseLease(s.Ctx, &types.MsgCloseLease{
		Sender:     closer.String(),
		LeaseUuids: []string{s.lease(t, closer)},
	})
	require.NoError(t, err)

	// The rich tenant also has a stream (5/s) with another provider. It draws on
	// the same credit but belongs to that provider's total only.
	otherProviderAddr := s.TestAccs[3]
	otherProvider := s.createTestProvider(t, otherProviderAddr.String(), s.TestAccs[4].String())
	otherSKU := s.createTestSKUWithDenom(t, otherProvider.Uuid, 3600, testDenom)
	s.createAndAcknowledgeLease(t, s.msgServer, rich, otherProviderAddr, []types.LeaseItemInput{
		{SkuUuid: otherSKU.Uuid, Quantity: 5},
	})

	ackTime := s.Ctx.BlockTime()
	at := func(d time.Duration) sdk.Context { return s.Ctx.WithBlockTime(ackTime.Add(d)) }
	amount := func(n int64) sdk.Coins { return sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(n))) }

	t.Run("nothing accrues in the acknowledgement block", func(t *testing.T) {
		resp := queryProviderWithdrawable(t, querier, s.Ctx, s.provider.Uuid, nil)
		require.True(t, resp.Amounts.IsZero())
		require.Zero(t, resp.LeaseCount, "no stream has anything to withdraw yet")
		require.Empty(t, resp.Pagination.NextKey)
	})

	t.Run("sums the accrual of the provider's streams", func(t *testing.T) {
		// 100s: rich 2/s + poor 1/s. The closed lease accrues nothing.
		resp := queryProviderWithdrawable(t, querier, at(100*time.Second), s.provider.Uuid, nil)
		require.Equal(t, amount(300), resp.Amounts)
		require.Equal(t, uint64(3), resp.LeaseCount, "leases covered by streams with a nonzero amount")
		require.Empty(t, resp.Pagination.NextKey)

		// The other provider sees its own stream and nothing else.
		resp = queryProviderWithdrawable(t, querier, at(100*time.Second), otherProvider.Uuid, nil)
		require.Equal(t, amount(500), resp.Amounts)
		require.Equal(t, uint64(1), resp.LeaseCount)
	})

	t.Run("accrues by the nanosecond and reports whole units", func(t *testing.T) {
		// 499ms: rich has accrued 0.998 and poor 0.499, no whole unit yet.
		resp := queryProviderWithdrawable(t, querier, at(499*time.Millisecond), s.provider.Uuid, nil)
		require.True(t, resp.Amounts.IsZero())
		require.Zero(t, resp.LeaseCount)

		// 500ms: rich reaches exactly 1; poor is at 0.5, so its lease does not count.
		resp = queryProviderWithdrawable(t, querier, at(500*time.Millisecond), s.provider.Uuid, nil)
		require.Equal(t, amount(1), resp.Amounts)
		require.Equal(t, uint64(2), resp.LeaseCount)

		// 999ms: rich 1.998, poor 0.999.
		resp = queryProviderWithdrawable(t, querier, at(999*time.Millisecond), s.provider.Uuid, nil)
		require.Equal(t, amount(1), resp.Amounts)
		require.Equal(t, uint64(2), resp.LeaseCount)

		// 1999ms: rich 3.998, poor 1.999.
		resp = queryProviderWithdrawable(t, querier, at(1999*time.Millisecond), s.provider.Uuid, nil)
		require.Equal(t, amount(3+1), resp.Amounts)
		require.Equal(t, uint64(3), resp.LeaseCount)

		// One nanosecond short of two seconds, and exactly two seconds.
		resp = queryProviderWithdrawable(t, querier, at(2*time.Second-time.Nanosecond), s.provider.Uuid, nil)
		require.Equal(t, amount(3+1), resp.Amounts)
		resp = queryProviderWithdrawable(t, querier, at(2*time.Second), s.provider.Uuid, nil)
		require.Equal(t, amount(4+2), resp.Amounts)
	})

	t.Run("a stream is capped at the tenant's credit", func(t *testing.T) {
		// 2h: rich 2 x 7200; poor has accrued 7200 but only holds 3600.
		resp := queryProviderWithdrawable(t, querier, at(2*time.Hour), s.provider.Uuid, nil)
		require.Equal(t, amount(14_400+3600), resp.Amounts)
		require.Equal(t, uint64(3), resp.LeaseCount)

		// Exactly at the boundary the poor tenant's whole credit is owed.
		resp = queryProviderWithdrawable(t, querier, at(time.Hour), s.provider.Uuid, nil)
		require.Equal(t, amount(7200+3600), resp.Amounts)
	})

	t.Run("the query settles nothing", func(t *testing.T) {
		for _, tenant := range []sdk.AccAddress{rich, poor} {
			state, exists := s.stream(t, tenant, testDenom)
			require.True(t, exists)
			require.Equal(t, ackTime, state.LastSettledAt)
			require.False(t, state.IsExhausted())
		}
		require.True(t, s.payout(testDenom).IsZero())
		require.Equal(t, sdkmath.NewInt(3600), s.credit(poor, testDenom))
	})

	t.Run("matches what a withdrawal then pays", func(t *testing.T) {
		s.advance(2 * time.Hour)
		expected := queryProviderWithdrawable(t, querier, s.Ctx, s.provider.Uuid, nil)
		paid := s.withdraw(t, 0, nil)
		require.Equal(t, expected.Amounts, paid.TotalAmounts)
		require.Equal(t, amount(18_000), paid.TotalAmounts)

		// Everything is settled: nothing is left to withdraw in this block.
		resp := queryProviderWithdrawable(t, querier, s.Ctx, s.provider.Uuid, nil)
		require.True(t, resp.Amounts.IsZero())
		require.Zero(t, resp.LeaseCount)
	})

	t.Run("a frozen stream has nothing to withdraw", func(t *testing.T) {
		// The withdrawal froze the poor tenant's stream. It accrues nothing more,
		// and its lease no longer counts, even though it is still ACTIVE.
		state, exists := s.stream(t, poor, testDenom)
		require.True(t, exists)
		require.True(t, state.IsExhausted())

		later := s.Ctx.WithBlockTime(s.Ctx.BlockTime().Add(100 * time.Second))
		resp := queryProviderWithdrawable(t, querier, later, s.provider.Uuid, nil)
		require.Equal(t, amount(200), resp.Amounts)
		require.Equal(t, uint64(2), resp.LeaseCount)

		// A top-up after the freeze does not bring the stream back.
		s.fundTenant(t, poor, testDenom, 50_000)
		resp = queryProviderWithdrawable(t, querier, later, s.provider.Uuid, nil)
		require.Equal(t, amount(200), resp.Amounts)
		require.Equal(t, uint64(2), resp.LeaseCount)
	})

	t.Run("an unrepresentable accrual is capped at the tenant's credit", func(t *testing.T) {
		// Rewind the rich tenant's stream beyond MaxDurationSeconds (~100 years).
		key := keeper.NewStreamKey(s.provider.Uuid, rich, testDenom)
		state, err := k.Streams.Get(s.Ctx, key)
		require.NoError(t, err)
		original := state.LastSettledAt
		state.LastSettledAt = s.Ctx.BlockTime().Add(-150 * 365 * 24 * time.Hour)
		require.NoError(t, k.Streams.Set(s.Ctx, key, state))

		resp := queryProviderWithdrawable(t, querier, s.Ctx, s.provider.Uuid, nil)
		require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, s.credit(rich, testDenom))), resp.Amounts)
		require.Equal(t, uint64(2), resp.LeaseCount)

		state.LastSettledAt = original
		require.NoError(t, k.Streams.Set(s.Ctx, key, state))
	})

	t.Run("input validation", func(t *testing.T) {
		_, err := querier.ProviderWithdrawable(s.Ctx, nil)
		require.Equal(t, codes.InvalidArgument, status.Code(err))

		_, err = querier.ProviderWithdrawable(s.Ctx, &types.QueryProviderWithdrawableRequest{ProviderUuid: ""})
		require.Equal(t, codes.InvalidArgument, status.Code(err))

		// A provider without streams (known or not) has nothing to withdraw.
		resp := queryProviderWithdrawable(t, querier, s.Ctx, "01912345-6789-7abc-8def-999999999999", nil)
		require.True(t, resp.Amounts.IsZero())
		require.Zero(t, resp.LeaseCount)
		require.Empty(t, resp.Pagination.NextKey)
	})

	s.requireInvariants(t)
}

// TestQueryProviderWithdrawable_Pagination covers pagination over the
// provider's streams. Pages hold streams, not leases, so the provider needs
// several tenants: tenant i has i leases, which makes every page's amount and
// LeaseCount distinct.
func TestQueryProviderWithdrawable_Pagination(t *testing.T) {
	s := newStreamFixture(t, testDenom)
	querier := keeper.NewQuerier(s.App.BillingKeeper)

	const tenants = 5
	for i := uint32(1); i <= tenants; i++ {
		tenant := queryTestTenant(i)
		s.fundTenant(t, tenant, testDenom, 10_000_000)
		for range i {
			s.lease(t, tenant)
		}
	}

	// Another provider's stream with the first tenant must stay out of every page.
	otherProviderAddr := s.TestAccs[3]
	otherProvider := s.createTestProvider(t, otherProviderAddr.String(), s.TestAccs[4].String())
	otherSKU := s.createTestSKUWithDenom(t, otherProvider.Uuid, 3600, testDenom)
	s.createAndAcknowledgeLease(t, s.msgServer, queryTestTenant(1), otherProviderAddr, []types.LeaseItemInput{
		{SkuUuid: otherSKU.Uuid, Quantity: 1},
	})

	// After 100s, tenant i's stream has accrued i x 100.
	s.advance(100 * time.Second)
	amount := func(n int64) sdk.Coins { return sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(n))) }
	page := func(t *testing.T, req *query.PageRequest) *types.QueryProviderWithdrawableResponse {
		t.Helper()
		return queryProviderWithdrawable(t, querier, s.Ctx, s.provider.Uuid, req)
	}

	t.Run("one page holds everything by default", func(t *testing.T) {
		resp := page(t, nil)
		require.Equal(t, amount(1500), resp.Amounts)
		require.Equal(t, uint64(15), resp.LeaseCount)
		require.Empty(t, resp.Pagination.NextKey)
	})

	t.Run("a limit pages over streams in key order", func(t *testing.T) {
		// Tenants 1 and 2: 100 + 200, covering 1 + 2 leases.
		resp := page(t, &query.PageRequest{Limit: 2})
		require.Equal(t, amount(300), resp.Amounts)
		require.Equal(t, uint64(3), resp.LeaseCount, "LeaseCount covers the streams of this page only")
		require.NotEmpty(t, resp.Pagination.NextKey, "more streams exist beyond the page")
	})

	t.Run("the cursor drains all pages and sums to the single-page total", func(t *testing.T) {
		full := page(t, &query.PageRequest{Limit: 100})
		require.Empty(t, full.Pagination.NextKey)

		wantAmounts := []int64{300, 700, 500}
		wantLeases := []uint64{3, 7, 5}

		total := sdk.NewCoins()
		var count uint64
		var key []byte
		pages := 0
		for {
			require.Less(t, pages, len(wantAmounts), "pagination must terminate")
			resp := page(t, &query.PageRequest{Limit: 2, Key: key})
			require.Equal(t, amount(wantAmounts[pages]), resp.Amounts)
			require.Equal(t, wantLeases[pages], resp.LeaseCount)
			total = total.Add(resp.Amounts...)
			count += resp.LeaseCount
			pages++
			if len(resp.Pagination.NextKey) == 0 {
				break
			}
			key = resp.Pagination.NextKey
		}
		require.Equal(t, 3, pages)
		require.Equal(t, full.Amounts, total, "paged total must equal the single-page total")
		require.Equal(t, full.LeaseCount, count)
	})

	t.Run("offset skips streams", func(t *testing.T) {
		// Tenants 4 and 5.
		resp := page(t, &query.PageRequest{Offset: 3, Limit: 10})
		require.Equal(t, amount(900), resp.Amounts)
		require.Equal(t, uint64(9), resp.LeaseCount)
		require.Empty(t, resp.Pagination.NextKey)

		resp = page(t, &query.PageRequest{Offset: tenants, Limit: 10})
		require.True(t, resp.Amounts.IsZero(), "an offset past the last stream yields an empty page")
		require.Zero(t, resp.LeaseCount)
	})

	t.Run("reverse starts from the last stream", func(t *testing.T) {
		// Tenants 5 and 4.
		resp := page(t, &query.PageRequest{Limit: 2, Reverse: true})
		require.Equal(t, amount(900), resp.Amounts)
		require.Equal(t, uint64(9), resp.LeaseCount)
		require.NotEmpty(t, resp.Pagination.NextKey)

		// Tenants 3 and 2, then tenant 1.
		resp = page(t, &query.PageRequest{Limit: 2, Reverse: true, Key: resp.Pagination.NextKey})
		require.Equal(t, amount(500), resp.Amounts)
		resp = page(t, &query.PageRequest{Limit: 2, Reverse: true, Key: resp.Pagination.NextKey})
		require.Equal(t, amount(100), resp.Amounts)
		require.Empty(t, resp.Pagination.NextKey)
	})

	t.Run("count_total counts the provider's streams only", func(t *testing.T) {
		resp := page(t, &query.PageRequest{Limit: 1, CountTotal: true})
		require.Equal(t, uint64(tenants), resp.Pagination.Total)
		require.Equal(t, amount(100), resp.Amounts)

		other := queryProviderWithdrawable(t, querier, s.Ctx, otherProvider.Uuid, &query.PageRequest{CountTotal: true})
		require.Equal(t, uint64(1), other.Pagination.Total)
		require.Equal(t, amount(100), other.Amounts)
	})

	t.Run("a zero-amount stream fills a page slot but counts for nothing", func(t *testing.T) {
		// Settle everything: in the same block every stream of the page is at zero.
		s.withdraw(t, 0, nil)
		resp := page(t, &query.PageRequest{Limit: 2})
		require.True(t, resp.Amounts.IsZero())
		require.Zero(t, resp.LeaseCount)
		require.NotEmpty(t, resp.Pagination.NextKey, "the page is still full, so the cursor still advances")

		// Half a second later tenant 1 (1/s) has accrued 0.5 and tenant 2 (2/s)
		// exactly 1: the page holds both streams, but only tenant 2's two leases
		// have something to withdraw.
		later := s.Ctx.WithBlockTime(s.Ctx.BlockTime().Add(500 * time.Millisecond))
		resp = queryProviderWithdrawable(t, querier, later, s.provider.Uuid, &query.PageRequest{Limit: 2})
		require.Equal(t, amount(1), resp.Amounts)
		require.Equal(t, uint64(2), resp.LeaseCount)
		require.NotEmpty(t, resp.Pagination.NextKey)

		// The remaining pages: tenants 3 and 4 (1.5 and 2.0), then tenant 5 (2.5).
		resp = queryProviderWithdrawable(t, querier, later, s.provider.Uuid, &query.PageRequest{Limit: 2, Key: resp.Pagination.NextKey})
		require.Equal(t, amount(1+2), resp.Amounts)
		require.Equal(t, uint64(3+4), resp.LeaseCount)
		resp = queryProviderWithdrawable(t, querier, later, s.provider.Uuid, &query.PageRequest{Limit: 2, Key: resp.Pagination.NextKey})
		require.Equal(t, amount(2), resp.Amounts)
		require.Equal(t, uint64(5), resp.LeaseCount)
		require.Empty(t, resp.Pagination.NextKey)
	})

	t.Run("offset and key together are rejected", func(t *testing.T) {
		first := page(t, &query.PageRequest{Limit: 2})
		_, err := querier.ProviderWithdrawable(s.Ctx, &types.QueryProviderWithdrawableRequest{
			ProviderUuid: s.provider.Uuid,
			Pagination:   &query.PageRequest{Offset: 1, Key: first.Pagination.NextKey},
		})
		require.ErrorContains(t, err, "either offset or key is expected")
	})

	s.requireInvariants(t)
}

// TestQueryProviderWithdrawable_ExactAcrossSubSecondSettlements verifies that
// accrual is exact when settlements do not fall on whole seconds: what a
// settlement cannot pay as a whole unit is carried in the stream's remainder,
// the query takes that remainder into account, and over any span the provider
// is paid exactly rate x time. Each query is checked against the withdrawal
// that follows it.
func TestQueryProviderWithdrawable_ExactAcrossSubSecondSettlements(t *testing.T) {
	s := newStreamFixture(t, testDenom)
	querier := keeper.NewQuerier(s.App.BillingKeeper)

	// One lease at 1/s for the first tenant, three (3/s) for the second.
	single, triple := queryTestTenant(1), queryTestTenant(2)
	s.fundTenant(t, single, testDenom, 1_000_000)
	s.fundTenant(t, triple, testDenom, 1_000_000)
	s.lease(t, single)
	for range 3 {
		s.lease(t, triple)
	}
	start := s.Ctx.BlockTime()

	amount := func(n int64) sdk.Coins { return sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(n))) }
	remainder := func(tenant sdk.AccAddress) sdkmath.Int {
		state, exists := s.stream(t, tenant, testDenom)
		require.True(t, exists)
		require.Equal(t, s.Ctx.BlockTime(), state.LastSettledAt, "a settlement moves last_settled_at to the block time")
		return state.Remainder
	}

	steps := []struct {
		name    string
		advance time.Duration
		// wantPaid is what the query reports and the withdrawal then pays.
		wantPaid      int64
		wantLeases    uint64
		wantSingleRem int64
		wantTripleRem int64
	}{
		{
			// single 1.5, triple 4.5: both keep half a unit.
			name: "1.5s: half a unit is carried by each stream", advance: 1500 * time.Millisecond,
			wantPaid: 1 + 4, wantLeases: 4, wantSingleRem: 500_000_000, wantTripleRem: 500_000_000,
		},
		{
			// single 0.5 + 0.499 = 0.999; triple 0.5 + 1.497 = 1.997.
			name: "499ms later: the single lease is one millisecond short of a unit", advance: 499 * time.Millisecond,
			wantPaid: 1, wantLeases: 3, wantSingleRem: 999_000_000, wantTripleRem: 997_000_000,
		},
		{
			// single 0.999 + 0.001 = 1; triple 0.997 + 0.003 = 1.
			name: "1ms later: both remainders complete a unit", advance: time.Millisecond,
			wantPaid: 1 + 1, wantLeases: 4, wantSingleRem: 0, wantTripleRem: 0,
		},
		{
			// single 0.000000001; triple 0.000000003.
			name: "1ns later: nanoseconds accrue too", advance: time.Nanosecond,
			wantPaid: 0, wantLeases: 0, wantSingleRem: 1, wantTripleRem: 3,
		},
		{
			// Up to the next whole second, 3s after the start.
			name: "to the whole second: the carried nanoseconds are not lost", advance: time.Second - time.Nanosecond,
			wantPaid: 1 + 3, wantLeases: 4, wantSingleRem: 0, wantTripleRem: 0,
		},
	}

	var totalPaid int64
	for _, step := range steps {
		t.Run(step.name, func(t *testing.T) {
			s.advance(step.advance)

			resp := queryProviderWithdrawable(t, querier, s.Ctx, s.provider.Uuid, nil)
			if step.wantPaid == 0 {
				require.True(t, resp.Amounts.IsZero())
			} else {
				require.Equal(t, amount(step.wantPaid), resp.Amounts)
			}
			require.Equal(t, step.wantLeases, resp.LeaseCount)

			paid := s.withdraw(t, 0, nil)
			require.True(t, resp.Amounts.Equal(paid.TotalAmounts),
				"the query (%s) matches what the withdrawal pays (%s)", resp.Amounts, paid.TotalAmounts)
			totalPaid += step.wantPaid

			require.Equal(t, sdkmath.NewInt(step.wantSingleRem), remainder(single))
			require.Equal(t, sdkmath.NewInt(step.wantTripleRem), remainder(triple))

			// Right after the settlement nothing is withdrawable, remainder or not.
			resp = queryProviderWithdrawable(t, querier, s.Ctx, s.provider.Uuid, nil)
			require.True(t, resp.Amounts.IsZero())
			require.Zero(t, resp.LeaseCount)
		})
	}

	// Five settlements over exactly 3 seconds paid exactly 3s x (1/s + 3/s).
	require.Equal(t, 3*time.Second, s.Ctx.BlockTime().Sub(start))
	require.Equal(t, int64(12), totalPaid)
	require.Equal(t, sdkmath.NewInt(12), s.payout(testDenom))
	s.requireInvariants(t)
}

// streamQueryFixture is a small, fully known set of streams for the
// StreamsByProvider and StreamsByTenant tests:
//
//	fixture provider: (tenantA, umfx) 2 leases at 1/s
//	                  (tenantA, upwr) 1 lease at 1/s
//	                  (tenantB, umfx) 1 lease at 1/s, frozen at settledAt
//	other provider:   (tenantA, umfx) 1 lease at 5/s, never settled
//
// The fixture provider's streams were all settled at settledAt (two hours after
// the leases were acknowledged at ackTime).
type streamQueryFixture struct {
	*streamFixture
	querier       keeper.Querier
	tenantA       sdk.AccAddress
	tenantB       sdk.AccAddress
	otherProvider string
	ackTime       time.Time
	settledAt     time.Time
}

func newStreamQueryFixture(t *testing.T) *streamQueryFixture {
	t.Helper()

	s := newStreamFixture(t, testDenom)
	q := &streamQueryFixture{
		streamFixture: s,
		querier:       keeper.NewQuerier(s.App.BillingKeeper),
		tenantA:       queryTestTenant(1),
		tenantB:       queryTestTenant(2),
	}

	skuOtherDenom := s.createTestSKUWithDenom(t, s.provider.Uuid, 3600, testDenom2)
	s.fundTenant(t, q.tenantA, testDenom, 1_000_000)
	s.fundTenant(t, q.tenantA, testDenom2, 1_000_000)
	s.fundTenant(t, q.tenantB, testDenom, 3600)

	s.lease(t, q.tenantA)
	s.lease(t, q.tenantA)
	s.createAndAcknowledgeLease(t, s.msgServer, q.tenantA, s.providerAddr, []types.LeaseItemInput{
		{SkuUuid: skuOtherDenom.Uuid, Quantity: 1},
	})
	s.lease(t, q.tenantB)

	otherProviderAddr := s.TestAccs[3]
	otherProvider := s.createTestProvider(t, otherProviderAddr.String(), s.TestAccs[4].String())
	otherSKU := s.createTestSKUWithDenom(t, otherProvider.Uuid, 3600, testDenom)
	s.createAndAcknowledgeLease(t, s.msgServer, q.tenantA, otherProviderAddr, []types.LeaseItemInput{
		{SkuUuid: otherSKU.Uuid, Quantity: 5},
	})
	q.otherProvider = otherProvider.Uuid
	q.ackTime = s.Ctx.BlockTime()

	// Two hours later the fixture provider withdraws: tenantB's credit covers
	// one hour only, so its stream freezes.
	s.advance(2 * time.Hour)
	s.withdraw(t, 0, nil)
	q.settledAt = s.Ctx.BlockTime()
	require.Equal(t, sdkmath.NewInt(1_000_000-2*7200), s.credit(q.tenantA, testDenom))
	require.Equal(t, sdkmath.NewInt(1_000_000-7200), s.credit(q.tenantA, testDenom2))
	require.True(t, s.credit(q.tenantB, testDenom).IsZero())
	s.requireInvariants(t)

	return q
}

// expected builds the expected query shape of one of the fixture's streams.
func (q *streamQueryFixture) expected(
	providerUUID string, tenant sdk.AccAddress, denom string, rate int64, leases uint64, settledAt time.Time, frozen bool,
) types.Stream {
	stream := types.Stream{
		ProviderUuid: providerUUID,
		Tenant:       tenant.String(),
		Denom:        denom,
		State: types.StreamState{
			Rate:          sdkmath.NewInt(rate),
			LastSettledAt: settledAt,
			LeaseCount:    leases,
			// Every settlement of the fixture falls on a whole second.
			Remainder: sdkmath.ZeroInt(),
		},
	}
	if frozen {
		exhaustedAt := settledAt
		stream.State.ExhaustedAt = &exhaustedAt
	}
	return stream
}

func TestQueryStreamsByProvider(t *testing.T) {
	q := newStreamQueryFixture(t)

	// In key order: (tenant, denom).
	want := []types.Stream{
		q.expected(q.provider.Uuid, q.tenantA, testDenom, 2, 2, q.settledAt, false),
		q.expected(q.provider.Uuid, q.tenantA, testDenom2, 1, 1, q.settledAt, false),
		q.expected(q.provider.Uuid, q.tenantB, testDenom, 1, 1, q.settledAt, true),
	}
	fetch := func(t *testing.T, providerUUID string, page *query.PageRequest) *types.QueryStreamsByProviderResponse {
		t.Helper()
		resp, err := q.querier.StreamsByProvider(q.Ctx, &types.QueryStreamsByProviderRequest{
			ProviderUuid: providerUUID,
			Pagination:   page,
		})
		require.NoError(t, err)
		require.NotNil(t, resp.Pagination)
		return resp
	}

	t.Run("returns the provider's streams with their state, in key order", func(t *testing.T) {
		resp := fetch(t, q.provider.Uuid, nil)
		require.Equal(t, want, resp.Streams)
		require.Empty(t, resp.Pagination.NextKey)
	})

	t.Run("returns only the streams of the requested provider", func(t *testing.T) {
		resp := fetch(t, q.otherProvider, nil)
		require.Equal(t, []types.Stream{
			q.expected(q.otherProvider, q.tenantA, testDenom, 5, 1, q.ackTime, false),
		}, resp.Streams)
	})

	t.Run("a provider without streams yields an empty page", func(t *testing.T) {
		resp := fetch(t, "01912345-6789-7abc-8def-999999999999", &query.PageRequest{CountTotal: true})
		require.Empty(t, resp.Streams)
		require.Empty(t, resp.Pagination.NextKey)
		require.Zero(t, resp.Pagination.Total)
	})

	t.Run("a prefix of a provider UUID does not match its streams", func(t *testing.T) {
		resp := fetch(t, q.provider.Uuid[:len(q.provider.Uuid)-1], nil)
		require.Empty(t, resp.Streams)
	})

	t.Run("limit and cursor", func(t *testing.T) {
		first := fetch(t, q.provider.Uuid, &query.PageRequest{Limit: 2})
		require.Equal(t, want[:2], first.Streams)
		require.NotEmpty(t, first.Pagination.NextKey)

		second := fetch(t, q.provider.Uuid, &query.PageRequest{Limit: 2, Key: first.Pagination.NextKey})
		require.Equal(t, want[2:], second.Streams)
		require.Empty(t, second.Pagination.NextKey)
	})

	t.Run("offset", func(t *testing.T) {
		resp := fetch(t, q.provider.Uuid, &query.PageRequest{Offset: 1, Limit: 1})
		require.Equal(t, want[1:2], resp.Streams)
		require.NotEmpty(t, resp.Pagination.NextKey)

		resp = fetch(t, q.provider.Uuid, &query.PageRequest{Offset: 3})
		require.Empty(t, resp.Streams)
	})

	t.Run("reverse", func(t *testing.T) {
		resp := fetch(t, q.provider.Uuid, &query.PageRequest{Reverse: true})
		reversed := slices.Clone(want)
		slices.Reverse(reversed)
		require.Equal(t, reversed, resp.Streams)

		first := fetch(t, q.provider.Uuid, &query.PageRequest{Reverse: true, Limit: 2})
		require.Equal(t, reversed[:2], first.Streams)
		second := fetch(t, q.provider.Uuid, &query.PageRequest{Reverse: true, Limit: 2, Key: first.Pagination.NextKey})
		require.Equal(t, reversed[2:], second.Streams)
		require.Empty(t, second.Pagination.NextKey)
	})

	t.Run("count_total counts the provider's streams only", func(t *testing.T) {
		resp := fetch(t, q.provider.Uuid, &query.PageRequest{Limit: 1, CountTotal: true})
		require.Equal(t, uint64(3), resp.Pagination.Total)
		require.Equal(t, want[:1], resp.Streams)
	})

	t.Run("input validation", func(t *testing.T) {
		_, err := q.querier.StreamsByProvider(q.Ctx, nil)
		require.Equal(t, codes.InvalidArgument, status.Code(err))

		_, err = q.querier.StreamsByProvider(q.Ctx, &types.QueryStreamsByProviderRequest{})
		require.Equal(t, codes.InvalidArgument, status.Code(err))
		require.ErrorContains(t, err, "provider_uuid cannot be empty")

		first := fetch(t, q.provider.Uuid, &query.PageRequest{Limit: 1})
		_, err = q.querier.StreamsByProvider(q.Ctx, &types.QueryStreamsByProviderRequest{
			ProviderUuid: q.provider.Uuid,
			Pagination:   &query.PageRequest{Offset: 1, Key: first.Pagination.NextKey},
		})
		require.ErrorContains(t, err, "either offset or key is expected")
	})

	t.Run("a drained stream disappears, a changed one is reported as it now is", func(t *testing.T) {
		// The EndBlocker closes tenantB's lease, which deletes its frozen stream.
		require.NoError(t, q.App.BillingKeeper.EndBlocker(q.Ctx))
		resp := fetch(t, q.provider.Uuid, nil)
		require.Equal(t, want[:2], resp.Streams)

		// A new lease of tenantA joins its umfx stream 10 seconds later.
		q.advance(10 * time.Second)
		q.lease(t, q.tenantA)
		resp = fetch(t, q.provider.Uuid, nil)
		require.Equal(t, []types.Stream{
			q.expected(q.provider.Uuid, q.tenantA, testDenom, 3, 3, q.Ctx.BlockTime(), false),
			want[1],
		}, resp.Streams)
		q.requireInvariants(t)
	})
}

func TestQueryStreamsByTenant(t *testing.T) {
	q := newStreamQueryFixture(t)

	// In key order: (provider_uuid, denom). The provider UUIDs are generated, so
	// sort the expectation the way the store does.
	want := []types.Stream{
		q.expected(q.provider.Uuid, q.tenantA, testDenom, 2, 2, q.settledAt, false),
		q.expected(q.provider.Uuid, q.tenantA, testDenom2, 1, 1, q.settledAt, false),
		q.expected(q.otherProvider, q.tenantA, testDenom, 5, 1, q.ackTime, false),
	}
	slices.SortFunc(want, func(a, b types.Stream) int {
		return cmp.Or(cmp.Compare(a.ProviderUuid, b.ProviderUuid), cmp.Compare(a.Denom, b.Denom))
	})
	fetch := func(t *testing.T, tenant sdk.AccAddress, page *query.PageRequest) *types.QueryStreamsByTenantResponse {
		t.Helper()
		resp, err := q.querier.StreamsByTenant(q.Ctx, &types.QueryStreamsByTenantRequest{
			Tenant:     tenant.String(),
			Pagination: page,
		})
		require.NoError(t, err)
		require.NotNil(t, resp.Pagination)
		return resp
	}

	t.Run("returns the tenant's streams across providers, in key order", func(t *testing.T) {
		resp := fetch(t, q.tenantA, nil)
		require.Equal(t, want, resp.Streams)
		require.Empty(t, resp.Pagination.NextKey)
	})

	t.Run("returns only the streams of the requested tenant", func(t *testing.T) {
		resp := fetch(t, q.tenantB, nil)
		require.Equal(t, []types.Stream{
			q.expected(q.provider.Uuid, q.tenantB, testDenom, 1, 1, q.settledAt, true),
		}, resp.Streams)
	})

	t.Run("a tenant without streams yields an empty page", func(t *testing.T) {
		// TestAccs[0] has neither a credit account nor a lease.
		resp := fetch(t, q.TestAccs[0], &query.PageRequest{CountTotal: true})
		require.Empty(t, resp.Streams)
		require.Empty(t, resp.Pagination.NextKey)
		require.Zero(t, resp.Pagination.Total)
	})

	t.Run("limit and cursor", func(t *testing.T) {
		first := fetch(t, q.tenantA, &query.PageRequest{Limit: 2})
		require.Equal(t, want[:2], first.Streams)
		require.NotEmpty(t, first.Pagination.NextKey)

		second := fetch(t, q.tenantA, &query.PageRequest{Limit: 2, Key: first.Pagination.NextKey})
		require.Equal(t, want[2:], second.Streams)
		require.Empty(t, second.Pagination.NextKey)
	})

	t.Run("offset", func(t *testing.T) {
		resp := fetch(t, q.tenantA, &query.PageRequest{Offset: 2, Limit: 5})
		require.Equal(t, want[2:], resp.Streams)
		require.Empty(t, resp.Pagination.NextKey)
	})

	t.Run("reverse", func(t *testing.T) {
		reversed := slices.Clone(want)
		slices.Reverse(reversed)

		first := fetch(t, q.tenantA, &query.PageRequest{Reverse: true, Limit: 2})
		require.Equal(t, reversed[:2], first.Streams)
		second := fetch(t, q.tenantA, &query.PageRequest{Reverse: true, Limit: 2, Key: first.Pagination.NextKey})
		require.Equal(t, reversed[2:], second.Streams)
		require.Empty(t, second.Pagination.NextKey)
	})

	t.Run("count_total counts the tenant's streams only", func(t *testing.T) {
		resp := fetch(t, q.tenantA, &query.PageRequest{Limit: 1, CountTotal: true})
		require.Equal(t, uint64(3), resp.Pagination.Total)
		require.Equal(t, want[:1], resp.Streams)
	})

	t.Run("agrees with StreamsByProvider", func(t *testing.T) {
		// Every stream is reachable from both sides.
		all, err := q.App.BillingKeeper.GetAllStreams(q.Ctx)
		require.NoError(t, err)
		require.Len(t, all, 4)

		byTenant := slices.Concat(fetch(t, q.tenantA, nil).Streams, fetch(t, q.tenantB, nil).Streams)
		require.ElementsMatch(t, all, byTenant)

		var byProvider []types.Stream
		for _, providerUUID := range []string{q.provider.Uuid, q.otherProvider} {
			resp, err := q.querier.StreamsByProvider(q.Ctx, &types.QueryStreamsByProviderRequest{ProviderUuid: providerUUID})
			require.NoError(t, err)
			byProvider = append(byProvider, resp.Streams...)
		}
		require.ElementsMatch(t, all, byProvider)
	})

	t.Run("input validation", func(t *testing.T) {
		_, err := q.querier.StreamsByTenant(q.Ctx, nil)
		require.Equal(t, codes.InvalidArgument, status.Code(err))

		_, err = q.querier.StreamsByTenant(q.Ctx, &types.QueryStreamsByTenantRequest{})
		require.Equal(t, codes.InvalidArgument, status.Code(err))
		require.ErrorContains(t, err, "tenant cannot be empty")

		_, err = q.querier.StreamsByTenant(q.Ctx, &types.QueryStreamsByTenantRequest{Tenant: "not-an-address"})
		require.Equal(t, codes.InvalidArgument, status.Code(err))
		require.ErrorContains(t, err, "invalid tenant address")

		// The same address under another bech32 prefix is not a tenant address.
		_, err = q.querier.StreamsByTenant(q.Ctx, &types.QueryStreamsByTenantRequest{
			Tenant: sdk.MustBech32ifyAddressBytes("cosmos", q.tenantA),
		})
		require.Equal(t, codes.InvalidArgument, status.Code(err))

		first := fetch(t, q.tenantA, &query.PageRequest{Limit: 1})
		_, err = q.querier.StreamsByTenant(q.Ctx, &types.QueryStreamsByTenantRequest{
			Tenant:     q.tenantA.String(),
			Pagination: &query.PageRequest{Offset: 1, Key: first.Pagination.NextKey},
		})
		require.ErrorContains(t, err, "either offset or key is expected")
	})

	t.Run("a drained stream disappears from the tenant's list", func(t *testing.T) {
		require.NoError(t, q.App.BillingKeeper.EndBlocker(q.Ctx))
		require.Empty(t, fetch(t, q.tenantB, nil).Streams)
		require.Equal(t, want, fetch(t, q.tenantA, nil).Streams)
		q.requireInvariants(t)
	})
}

// TestQueryStreams_PageLimits verifies the page size bounds shared by the three
// stream queries: DefaultProviderWithdrawableQueryLimit when no limit is given,
// and a hard cap of MaxProviderWithdrawableQueryLimit.
//
// The bounds only concern store iteration, so the streams are written straight
// into the store (one more than the cap, for one provider and for one tenant)
// instead of being built from a thousand leases.
func TestQueryStreams_PageLimits(t *testing.T) {
	f := initFixture(t)
	k := f.App.BillingKeeper
	querier := keeper.NewQuerier(k)

	const (
		defaultLimit = int(types.DefaultProviderWithdrawableQueryLimit)
		maxLimit     = int(types.MaxProviderWithdrawableQueryLimit)
		total        = maxLimit + 1
	)

	providerUUID := testProviderUUID
	tenant := f.TestAccs[0]
	state := types.StreamState{Rate: sdkmath.NewInt(1), LastSettledAt: f.Ctx.BlockTime(), LeaseCount: 1}
	oneCoin := sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(1)))
	for i := range uint32(total) {
		// One stream of the provider per tenant. Each tenant holds one coin, so
		// that one second later every stream has exactly one coin to withdraw.
		streamTenant := queryTestTenant(i + 1)
		require.NoError(t, k.Streams.Set(f.Ctx, keeper.NewStreamKey(providerUUID, streamTenant, testDenom), state))
		f.fundAccount(t, types.DeriveCreditAddress(streamTenant), oneCoin)

		// One stream of the tenant per provider.
		streamProvider := fmt.Sprintf("01912345-6789-7abc-8def-prov%08d", i)
		require.NoError(t, k.Streams.Set(f.Ctx, keeper.NewStreamKey(streamProvider, tenant, testDenom), state))
		require.NoError(t, k.StreamsByTenant.Set(f.Ctx, collections.Join(tenant, collections.Join(streamProvider, testDenom))))
	}
	later := f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(time.Second))

	// Each query checks that its page held wantSize streams, and returns the
	// next cursor.
	queries := []struct {
		name string
		run  func(t *testing.T, page *query.PageRequest, wantSize int) []byte
	}{
		{
			name: "StreamsByProvider",
			run: func(t *testing.T, page *query.PageRequest, wantSize int) []byte {
				t.Helper()
				resp, err := querier.StreamsByProvider(later, &types.QueryStreamsByProviderRequest{ProviderUuid: providerUUID, Pagination: page})
				require.NoError(t, err)
				require.Len(t, resp.Streams, wantSize)
				return resp.Pagination.NextKey
			},
		},
		{
			name: "StreamsByTenant",
			run: func(t *testing.T, page *query.PageRequest, wantSize int) []byte {
				t.Helper()
				resp, err := querier.StreamsByTenant(later, &types.QueryStreamsByTenantRequest{Tenant: tenant.String(), Pagination: page})
				require.NoError(t, err)
				require.Len(t, resp.Streams, wantSize)
				return resp.Pagination.NextKey
			},
		},
		{
			name: "ProviderWithdrawable",
			run: func(t *testing.T, page *query.PageRequest, wantSize int) []byte {
				t.Helper()
				resp := queryProviderWithdrawable(t, querier, later, providerUUID, page)
				// One coin and one lease per stream of the page.
				require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(int64(wantSize)))), resp.Amounts)
				require.EqualValues(t, wantSize, resp.LeaseCount)
				return resp.Pagination.NextKey
			},
		},
	}

	pages := []struct {
		name     string
		page     *query.PageRequest
		wantSize int
	}{
		{name: "no pagination uses the default limit", page: nil, wantSize: defaultLimit},
		{name: "zero limit uses the default limit", page: &query.PageRequest{}, wantSize: defaultLimit},
		{name: "explicit limit below the default", page: &query.PageRequest{Limit: 7}, wantSize: 7},
		{name: "explicit limit above the default", page: &query.PageRequest{Limit: 250}, wantSize: 250},
		{name: "limit at the maximum", page: &query.PageRequest{Limit: uint64(maxLimit)}, wantSize: maxLimit},
		{name: "limit above the maximum is capped, not rejected", page: &query.PageRequest{Limit: 10 * uint64(maxLimit)}, wantSize: maxLimit},
		{name: "reverse keeps the default limit", page: &query.PageRequest{Reverse: true}, wantSize: defaultLimit},
	}

	for _, q := range queries {
		t.Run(q.name, func(t *testing.T) {
			for _, tc := range pages {
				t.Run(tc.name, func(t *testing.T) {
					next := q.run(t, tc.page, tc.wantSize)
					require.NotEmpty(t, next, "more streams exist beyond every page")
				})
			}

			t.Run("the cursor of a capped page reaches the remaining stream", func(t *testing.T) {
				next := q.run(t, &query.PageRequest{Limit: 10 * uint64(maxLimit)}, maxLimit)
				next = q.run(t, &query.PageRequest{Limit: 10 * uint64(maxLimit), Key: next}, total-maxLimit)
				require.Empty(t, next)
			})

			t.Run("the default limit applies to a cursor without a limit", func(t *testing.T) {
				next := q.run(t, nil, defaultLimit)
				next = q.run(t, &query.PageRequest{Key: next}, defaultLimit)
				require.NotEmpty(t, next)
			})
		})
	}
}

// TestQueryCreditEstimate_Streams covers how the estimate is derived from the
// tenant's streams: their summed rate per denom, the deduction of what they
// have accrued but not settled, and frozen streams being left out.
func TestQueryCreditEstimate_Streams(t *testing.T) {
	type setup struct {
		*streamFixture
		tenant sdk.AccAddress
	}
	coins := func(denom string, n int64) sdk.Coins { return sdk.NewCoins(sdk.NewCoin(denom, sdkmath.NewInt(n))) }

	tests := []struct {
		name string
		// prepare builds the state on a fresh fixture, leaving s.Ctx at the
		// block the query runs in.
		prepare      func(t *testing.T, s setup)
		wantRate     sdk.Coins
		wantBalance  sdk.Coins
		wantDuration uint64
		wantLeases   uint64
	}{
		{
			name: "nothing accrued yet",
			prepare: func(t *testing.T, s setup) {
				t.Helper()
				s.fundTenant(t, s.tenant, testDenom, 10_000)
				s.lease(t, s.tenant)
			},
			wantRate:     coins(testDenom, 1),
			wantBalance:  coins(testDenom, 10_000),
			wantDuration: 10_000,
			wantLeases:   1,
		},
		{
			name: "unsettled accrual is deducted from the balance",
			prepare: func(t *testing.T, s setup) {
				t.Helper()
				s.fundTenant(t, s.tenant, testDenom, 10_000)
				s.lease(t, s.tenant)
				s.advance(1000 * time.Second)
			},
			wantRate:     coins(testDenom, 1),
			wantBalance:  coins(testDenom, 10_000),
			wantDuration: 9000,
			wantLeases:   1,
		},
		{
			name: "only whole accrued units are deducted",
			prepare: func(t *testing.T, s setup) {
				t.Helper()
				s.fundTenant(t, s.tenant, testDenom, 10_000)
				s.lease(t, s.tenant)
				// 1.999 accrued: one whole unit.
				s.advance(1999 * time.Millisecond)
			},
			wantRate:     coins(testDenom, 1),
			wantBalance:  coins(testDenom, 10_000),
			wantDuration: 9999,
			wantLeases:   1,
		},
		{
			name: "a carried remainder counts towards the unsettled accrual",
			prepare: func(t *testing.T, s setup) {
				t.Helper()
				s.fundTenant(t, s.tenant, testDenom, 10_000)
				s.lease(t, s.tenant)
				// Settled at 1.5s: 1 paid, half a unit carried. Half a second
				// later the carried half and the new half make one unit owed.
				s.advance(1500 * time.Millisecond)
				s.withdraw(t, 0, nil)
				s.advance(500 * time.Millisecond)
			},
			wantRate:     coins(testDenom, 1),
			wantBalance:  coins(testDenom, 9999),
			wantDuration: 9998,
			wantLeases:   1,
		},
		{
			name: "a frozen stream's denom is still reported in the balance after a top-up",
			prepare: func(t *testing.T, s setup) {
				t.Helper()
				s.fundTenant(t, s.tenant, testDenom, 3600)
				s.lease(t, s.tenant)
				s.advance(2 * time.Hour)
				s.withdraw(t, 0, nil)
				// The tenant's only stream is frozen; a top-up does not revive
				// it, but the credit is the tenant's and must be visible.
				s.fundTenant(t, s.tenant, testDenom, 10_000)
			},
			wantRate:     sdk.NewCoins(),
			wantBalance:  coins(testDenom, 10_000),
			wantDuration: 0,
			wantLeases:   1,
		},
		{
			name: "the balance covers the denoms of frozen and healthy streams alike",
			prepare: func(t *testing.T, s setup) {
				t.Helper()
				skuOtherDenom := s.createTestSKUWithDenom(t, s.provider.Uuid, 3600, testDenom2)
				s.fundTenant(t, s.tenant, testDenom, 3600)
				s.fundTenant(t, s.tenant, testDenom2, 100_000)
				s.lease(t, s.tenant)
				s.createAndAcknowledgeLease(t, s.msgServer, s.tenant, s.providerAddr, []types.LeaseItemInput{
					{SkuUuid: skuOtherDenom.Uuid, Quantity: 2},
				})
				// The umfx stream freezes; the upwr stream is paid 2 x 7200.
				s.advance(2 * time.Hour)
				s.withdraw(t, 0, nil)
				s.fundTenant(t, s.tenant, testDenom, 500)
			},
			// Only upwr burns: 85_600 / 2. The topped-up umfx is reported but
			// does not shorten or extend the estimate.
			wantRate:     coins(testDenom2, 2),
			wantBalance:  sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(500)), sdk.NewCoin(testDenom2, sdkmath.NewInt(85_600))),
			wantDuration: 42_800,
			wantLeases:   2,
		},
		{
			name: "settling moves the accrual out of the balance and leaves the estimate unchanged",
			prepare: func(t *testing.T, s setup) {
				t.Helper()
				s.fundTenant(t, s.tenant, testDenom, 10_000)
				s.lease(t, s.tenant)
				s.advance(1000 * time.Second)
				s.withdraw(t, 0, nil)
			},
			wantRate:     coins(testDenom, 1),
			wantBalance:  coins(testDenom, 9000),
			wantDuration: 9000,
			wantLeases:   1,
		},
		{
			name: "one second of credit left",
			prepare: func(t *testing.T, s setup) {
				t.Helper()
				s.fundTenant(t, s.tenant, testDenom, 3600)
				s.lease(t, s.tenant)
				s.advance(3599 * time.Second)
			},
			wantRate:     coins(testDenom, 1),
			wantBalance:  coins(testDenom, 3600),
			wantDuration: 1,
			wantLeases:   1,
		},
		{
			name: "accrual equal to the balance leaves nothing",
			prepare: func(t *testing.T, s setup) {
				t.Helper()
				s.fundTenant(t, s.tenant, testDenom, 3600)
				s.lease(t, s.tenant)
				s.advance(3600 * time.Second)
			},
			wantRate:     coins(testDenom, 1),
			wantBalance:  coins(testDenom, 3600),
			wantDuration: 0,
			wantLeases:   1,
		},
		{
			name: "accrual beyond the balance leaves nothing",
			prepare: func(t *testing.T, s setup) {
				t.Helper()
				s.fundTenant(t, s.tenant, testDenom, 3600)
				s.lease(t, s.tenant)
				s.advance(5000 * time.Second)
			},
			wantRate:     coins(testDenom, 1),
			wantBalance:  coins(testDenom, 3600),
			wantDuration: 0,
			wantLeases:   1,
		},
		{
			name: "an unrepresentable accrual leaves nothing",
			prepare: func(t *testing.T, s setup) {
				t.Helper()
				s.fundTenant(t, s.tenant, testDenom, 10_000)
				s.lease(t, s.tenant)

				// Rewind the stream beyond MaxDurationSeconds (~100 years).
				key := keeper.NewStreamKey(s.provider.Uuid, s.tenant, testDenom)
				state, err := s.App.BillingKeeper.Streams.Get(s.Ctx, key)
				require.NoError(t, err)
				state.LastSettledAt = s.Ctx.BlockTime().Add(-150 * 365 * 24 * time.Hour)
				require.NoError(t, s.App.BillingKeeper.Streams.Set(s.Ctx, key, state))
			},
			wantRate:     coins(testDenom, 1),
			wantBalance:  coins(testDenom, 10_000),
			wantDuration: 0,
			wantLeases:   1,
		},
		{
			name: "a frozen stream is left out of the rate",
			prepare: func(t *testing.T, s setup) {
				t.Helper()
				s.fundTenant(t, s.tenant, testDenom, 3600)
				s.lease(t, s.tenant)
				s.advance(2 * time.Hour)
				s.withdraw(t, 0, nil)
			},
			wantRate:     sdk.NewCoins(),
			wantBalance:  sdk.NewCoins(),
			wantDuration: 0,
			// The lease is still ACTIVE until the EndBlocker drains the stream.
			wantLeases: 1,
		},
		{
			name: "the leases of several streams in one denom burn the same balance",
			prepare: func(t *testing.T, s setup) {
				t.Helper()
				s.fundTenant(t, s.tenant, testDenom, 1_000_000)
				s.lease(t, s.tenant)
				s.lease(t, s.tenant)

				otherProviderAddr := s.TestAccs[3]
				otherProvider := s.createTestProvider(t, otherProviderAddr.String(), s.TestAccs[4].String())
				otherSKU := s.createTestSKUWithDenom(t, otherProvider.Uuid, 3600, testDenom)
				s.createAndAcknowledgeLease(t, s.msgServer, s.tenant, otherProviderAddr, []types.LeaseItemInput{
					{SkuUuid: otherSKU.Uuid, Quantity: 5},
				})
				s.advance(100 * time.Second)
			},
			// 2/s with the fixture provider and 5/s with the other one; 700
			// accrued: (1_000_000 - 700) / 7.
			wantRate:     coins(testDenom, 7),
			wantBalance:  coins(testDenom, 1_000_000),
			wantDuration: 142_757,
			wantLeases:   3,
		},
		{
			name: "a healthy stream keeps burning next to a frozen one",
			prepare: func(t *testing.T, s setup) {
				t.Helper()
				skuOtherDenom := s.createTestSKUWithDenom(t, s.provider.Uuid, 3600, testDenom2)
				s.fundTenant(t, s.tenant, testDenom, 3600)
				s.fundTenant(t, s.tenant, testDenom2, 100_000)
				s.lease(t, s.tenant)
				s.createAndAcknowledgeLease(t, s.msgServer, s.tenant, s.providerAddr, []types.LeaseItemInput{
					{SkuUuid: skuOtherDenom.Uuid, Quantity: 2},
				})
				// The umfx stream freezes; the upwr stream is paid 2 x 7200.
				s.advance(2 * time.Hour)
				s.withdraw(t, 0, nil)
				s.advance(100 * time.Second)
			},
			// upwr: (100_000 - 14_400 - 200) / 2.
			wantRate:     coins(testDenom2, 2),
			wantBalance:  coins(testDenom2, 85_600),
			wantDuration: 42_700,
			wantLeases:   2,
		},
		{
			name: "the denom that runs out first decides",
			prepare: func(t *testing.T, s setup) {
				t.Helper()
				skuOtherDenom := s.createTestSKUWithDenom(t, s.provider.Uuid, 3600, testDenom2)
				s.fundTenant(t, s.tenant, testDenom, 100_000)
				s.fundTenant(t, s.tenant, testDenom2, 20_000)
				// One lease priced in both denoms: 1 umfx/s and 2 upwr/s.
				s.createAndAcknowledgeLease(t, s.msgServer, s.tenant, s.providerAddr, []types.LeaseItemInput{
					{SkuUuid: s.sku.Uuid, Quantity: 1},
					{SkuUuid: skuOtherDenom.Uuid, Quantity: 2},
				})
				s.advance(100 * time.Second)
			},
			// umfx: (100_000 - 100) / 1 = 99_900; upwr: (20_000 - 200) / 2 = 9900.
			wantRate:     sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(1)), sdk.NewCoin(testDenom2, sdkmath.NewInt(2))),
			wantBalance:  sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(100_000)), sdk.NewCoin(testDenom2, sdkmath.NewInt(20_000))),
			wantDuration: 9900,
			wantLeases:   1,
		},
		{
			name: "a pending lease does not burn",
			prepare: func(t *testing.T, s setup) {
				t.Helper()
				s.fundTenant(t, s.tenant, testDenom, 10_000)
				_, err := s.msgServer.CreateLease(s.Ctx, &types.MsgCreateLease{
					Tenant: s.tenant.String(),
					Items:  []types.LeaseItemInput{{SkuUuid: s.sku.Uuid, Quantity: 1}},
				})
				require.NoError(t, err)
				s.advance(100 * time.Second)
			},
			wantRate:     sdk.NewCoins(),
			wantBalance:  sdk.NewCoins(),
			wantDuration: 0,
			wantLeases:   0,
		},
		{
			name: "a closed lease stops burning",
			prepare: func(t *testing.T, s setup) {
				t.Helper()
				s.fundTenant(t, s.tenant, testDenom, 10_000)
				s.lease(t, s.tenant)
				closing := s.lease(t, s.tenant)
				s.advance(100 * time.Second)
				_, err := s.msgServer.CloseLease(s.Ctx, &types.MsgCloseLease{
					Sender:     s.tenant.String(),
					LeaseUuids: []string{closing},
				})
				require.NoError(t, err)
				s.advance(100 * time.Second)
			},
			// The close settled 200; 100 more accrued since at 1/s.
			wantRate:     coins(testDenom, 1),
			wantBalance:  coins(testDenom, 9800),
			wantDuration: 9700,
			wantLeases:   1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := setup{streamFixture: newStreamFixture(t, testDenom), tenant: queryTestTenant(1)}
			tc.prepare(t, s)

			resp, err := keeper.NewQuerier(s.App.BillingKeeper).CreditEstimate(s.Ctx, &types.QueryCreditEstimateRequest{
				Tenant: s.tenant.String(),
			})
			require.NoError(t, err)
			require.Equal(t, tc.wantRate, resp.TotalRatePerSecond)
			require.Equal(t, tc.wantBalance, resp.CurrentBalance)
			require.Equal(t, tc.wantDuration, resp.EstimatedDurationSeconds)
			require.Equal(t, tc.wantLeases, resp.ActiveLeaseCount)
		})
	}
}

// TestLeasesByProvider_ActiveCursorSurvivesClosedBoundary is the read-side analogue
// of the withdrawal cursor test (TestStream_WithdrawCursor): it closes the
// exact lease named by a page's next_key (removing it from the (provider, ACTIVE)
// index) and asserts the next page still resumes from the surviving tail rather than
// silently returning empty. Under the old scan-until-match cursor this test fails.
func TestLeasesByProvider_ActiveCursorSurvivesClosedBoundary(t *testing.T) {
	f := initFixture(t)
	k := f.App.BillingKeeper
	querier := keeper.NewQuerier(k)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	uuids := make([]string, 0, 5)
	for i := 1; i <= 5; i++ {
		u := fmt.Sprintf("01912345-6789-7abc-8def-%012d", i)
		require.NoError(t, k.SetLease(f.Ctx, types.Lease{
			Uuid:         u,
			Tenant:       tenant.String(),
			ProviderUuid: provider.Uuid,
			Items:        []types.LeaseItem{{SkuUuid: sku.Uuid, Quantity: 1, LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(1))}},
			State:        types.LEASE_STATE_ACTIVE,
			CreatedAt:    f.Ctx.BlockTime(),
		}))
		uuids = append(uuids, u)
	}

	// Page 1 (limit 2): returns the two smallest; next_key points at the 3rd (still ACTIVE).
	p1, err := querier.LeasesByProvider(f.Ctx, &types.QueryLeasesByProviderRequest{
		ProviderUuid: provider.Uuid,
		StateFilter:  types.LEASE_STATE_ACTIVE,
		Pagination:   &query.PageRequest{Limit: 2},
	})
	require.NoError(t, err)
	require.Len(t, p1.Leases, 2)
	require.NotEmpty(t, p1.Pagination.NextKey)
	boundary := string(p1.Pagination.NextKey)

	// Close the boundary lease so it drops out of the (provider, ACTIVE) index.
	lease, err := k.GetLease(f.Ctx, boundary)
	require.NoError(t, err)
	closedAt := f.Ctx.BlockTime()
	lease.State = types.LEASE_STATE_CLOSED
	lease.ClosedAt = &closedAt
	require.NoError(t, k.SetLease(f.Ctx, lease))

	// Resume from the now-closed cursor: must reach the surviving tail, not return empty.
	seen := map[string]bool{}
	key := p1.Pagination.NextKey
	for i := 0; i < 10; i++ {
		p, err := querier.LeasesByProvider(f.Ctx, &types.QueryLeasesByProviderRequest{
			ProviderUuid: provider.Uuid,
			StateFilter:  types.LEASE_STATE_ACTIVE,
			Pagination:   &query.PageRequest{Limit: 2, Key: key},
		})
		require.NoError(t, err)
		for _, l := range p.Leases {
			seen[l.Uuid] = true
		}
		if len(p.Pagination.NextKey) == 0 {
			break
		}
		key = p.Pagination.NextKey
	}

	require.True(t, seen[uuids[3]], "tail lease must be reached after the boundary lease was closed")
	require.True(t, seen[uuids[4]], "tail lease must be reached after the boundary lease was closed")
	require.False(t, seen[boundary], "the closed boundary lease is no longer ACTIVE")
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
		// With no active leases, only denoms from active leases are fetched (DoS mitigation)
		require.True(t, resp.CurrentBalance.IsZero())
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
			State:     types.LEASE_STATE_ACTIVE,
			CreatedAt: f.Ctx.BlockTime(),
		}
		require.NoError(t, k.SetLease(f.Ctx, lease))

		// Update credit account lease count
		ca, err := k.GetCreditAccount(f.Ctx, tenant.String())
		require.NoError(t, err)
		ca.ActiveLeaseCount = 1
		require.NoError(t, k.SetCreditAccount(f.Ctx, ca))

		// The lease was written straight into the store: give it its stream,
		// last settled now. The burn rate is read from the stream.
		f.syncStreams(t, f.Ctx.BlockTime())

		resp, err := querier.CreditEstimate(f.Ctx, &types.QueryCreditEstimateRequest{
			Tenant: tenant.String(),
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Equal(t, uint64(1), resp.ActiveLeaseCount)

		// Rate should be 200 per second (100 * 2 quantity)
		require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(200))), resp.TotalRatePerSecond)
		require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(1000000))), resp.CurrentBalance)

		// With 1,000,000 balance and 200/second rate, should last 5000 seconds
		require.Equal(t, uint64(5000), resp.EstimatedDurationSeconds)
	})

	t.Run("deducts what the stream accrued but has not settled", func(t *testing.T) {
		// 1000 seconds later the stream owes 200,000 that no one has settled.
		// The balance still shows 1,000,000, but only 800,000 is left to burn.
		later := f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(1000 * time.Second))

		resp, err := querier.CreditEstimate(later, &types.QueryCreditEstimateRequest{
			Tenant: tenant.String(),
		})
		require.NoError(t, err)
		require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(1000000))), resp.CurrentBalance,
			"the balance is reported as held, before the deduction")
		require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(200))), resp.TotalRatePerSecond)
		require.Equal(t, uint64(4000), resp.EstimatedDurationSeconds)

		// The estimate is a query: it settled nothing.
		state, err := k.Streams.Get(f.Ctx, keeper.NewStreamKey(providerUUID, tenant, testDenom))
		require.NoError(t, err)
		require.Equal(t, f.Ctx.BlockTime(), state.LastSettledAt)
	})

	t.Run("active lease count comes from the credit account", func(t *testing.T) {
		ca, err := k.GetCreditAccount(f.Ctx, tenant.String())
		require.NoError(t, err)
		ca.ActiveLeaseCount = 42
		require.NoError(t, k.SetCreditAccount(f.Ctx, ca))

		resp, err := querier.CreditEstimate(f.Ctx, &types.QueryCreditEstimateRequest{
			Tenant: tenant.String(),
		})
		require.NoError(t, err)
		require.Equal(t, uint64(42), resp.ActiveLeaseCount)
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

// TestQueryCreditEstimate_ParamDerivedActiveCap verifies the CreditEstimate burn-rate
// sum tracks the max_leases_per_tenant param instead of the legacy hardcoded cap of 100.
// With the param raised to 200, a tenant with >100 active leases must have every lease's
// rate summed; truncating at 100 underestimates burn (overestimates remaining runway).
// The rate is read from the tenant's streams, so both shapes are covered: all the leases
// in one stream, and every lease in a stream of its own (more than 100 streams to walk).
func TestQueryCreditEstimate_ParamDerivedActiveCap(t *testing.T) {
	// 150 active leases, each 1 unit/sec. The legacy code caps iteration at 100,
	// under-summing the burn rate; the param-derived cap ((max_leases_per_tenant +
	// max_pending_leases_per_tenant) * max_items_per_lease = 210 * 20 streams) sums all 150.
	const (
		numLeases = 150
		balance   = 1_000_000_000
	)

	cases := []struct {
		name string
		// providerUUID returns the provider of lease i.
		providerUUID  func(i int) string
		expectStreams uint64
	}{
		{
			name:          "one provider: a single stream aggregates every lease",
			providerUUID:  func(int) string { return testProviderUUID },
			expectStreams: 1,
		},
		{
			name:          "one provider per lease: one stream per lease",
			providerUUID:  func(i int) string { return fmt.Sprintf("01912345-6789-7abc-8def-prov%08d", i) },
			expectStreams: numLeases,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := initFixture(t)
			k := f.App.BillingKeeper
			querier := keeper.NewQuerier(k)

			tenant := f.TestAccs[0]

			// Raise the active-lease param above the legacy hardcoded cap of 100.
			params, err := k.GetParams(f.Ctx)
			require.NoError(t, err)
			params.MaxLeasesPerTenant = 200
			require.NoError(t, k.SetParams(f.Ctx, params))

			creditAddr := types.DeriveCreditAddress(tenant)
			require.NoError(t, k.SetCreditAccount(f.Ctx, types.CreditAccount{
				Tenant:           tenant.String(),
				CreditAddress:    creditAddr.String(),
				ActiveLeaseCount: numLeases,
			}))

			// Fund generously so the estimate is rate-bound, not balance-bound.
			f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(balance))))

			for i := range numLeases {
				require.NoError(t, k.SetLease(f.Ctx, types.Lease{
					Uuid:         fmt.Sprintf("01912345-6789-7abc-8def-active%06d", i),
					Tenant:       tenant.String(),
					ProviderUuid: tc.providerUUID(i),
					Items: []types.LeaseItem{
						{
							SkuUuid:     "01912345-6789-7abc-8def-sku000000001",
							Quantity:    1,
							LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(1)),
						},
					},
					State:     types.LEASE_STATE_ACTIVE,
					CreatedAt: f.Ctx.BlockTime(),
				}))
			}
			// The leases were written straight into the store: build their streams.
			f.syncStreams(t, f.Ctx.BlockTime())

			byTenant, err := querier.StreamsByTenant(f.Ctx, &types.QueryStreamsByTenantRequest{
				Tenant:     tenant.String(),
				Pagination: &query.PageRequest{Limit: 1, CountTotal: true},
			})
			require.NoError(t, err)
			require.Equal(t, tc.expectStreams, byTenant.Pagination.Total)

			resp, err := querier.CreditEstimate(f.Ctx, &types.QueryCreditEstimateRequest{
				Tenant: tenant.String(),
			})
			require.NoError(t, err)
			require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(numLeases))), resp.TotalRatePerSecond,
				"burn rate must sum all active leases, not truncate at the legacy 100 cap")
			require.Equal(t, uint64(numLeases), resp.ActiveLeaseCount)
			require.Equal(t, uint64(balance/numLeases), resp.EstimatedDurationSeconds)
		})
	}
}

// TestQueryCreditAccount_IncludesActiveDenomBeyondLegacyCap verifies that a denom used
// only by an active lease past position #100 still appears in the CreditAccount balances.
// getRelevantDenomsForTenant must cap the active iteration by the param-derived reachable
// ceiling (max_leases_per_tenant + max_pending_leases_per_tenant), not the legacy hardcoded
// 100, or the tail denom's balance is silently omitted.
func TestQueryCreditAccount_IncludesActiveDenomBeyondLegacyCap(t *testing.T) {
	f := initFixture(t)
	k := f.App.BillingKeeper
	querier := keeper.NewQuerier(k)

	tenant := f.TestAccs[0]

	params, err := k.GetParams(f.Ctx)
	require.NoError(t, err)
	params.MaxLeasesPerTenant = 200
	require.NoError(t, k.SetParams(f.Ctx, params))

	creditAddr := types.DeriveCreditAddress(tenant)
	require.NoError(t, k.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	}))

	const tailDenom = "tailactivedenom"
	f.fundAccount(t, creditAddr, sdk.NewCoins(
		sdk.NewCoin(testDenom, sdkmath.NewInt(1_000_000)),
		sdk.NewCoin(tailDenom, sdkmath.NewInt(777)),
	))

	// 120 active leases; only the last (position 120, by ascending UUID) uses tailDenom. The
	// TenantState MatchExact index iterates ascending by primary key (UUID), so the zero-padded
	// UUIDs place the tail lease last — past the legacy cap of 100 but within the raised param cap.
	const numLeases = 120
	for i := range numLeases {
		denom := testDenom
		if i == numLeases-1 {
			denom = tailDenom
		}
		require.NoError(t, k.SetLease(f.Ctx, types.Lease{
			Uuid:         fmt.Sprintf("01912345-6789-7abc-8def-active%06d", i),
			Tenant:       tenant.String(),
			ProviderUuid: testProviderUUID,
			Items: []types.LeaseItem{
				{
					SkuUuid:     "01912345-6789-7abc-8def-sku000000001",
					Quantity:    1,
					LockedPrice: sdk.NewCoin(denom, sdkmath.NewInt(1)),
				},
			},
			State:     types.LEASE_STATE_ACTIVE,
			CreatedAt: f.Ctx.BlockTime(),
		}))
	}

	resp, err := querier.CreditAccount(f.Ctx, &types.QueryCreditAccountRequest{
		Tenant: tenant.String(),
	})
	require.NoError(t, err)
	require.Equal(t, sdkmath.NewInt(777), resp.Balances.AmountOf(tailDenom),
		"denom used only by an active lease past the legacy 100 cap must appear in balances")
}

// TestQueryCreditAccount_IncludesPendingDenomBeyondLegacyCap is the pending-state analogue:
// the pending iteration in getRelevantDenomsForTenant must cap by max_pending_leases_per_tenant,
// so a denom used only by a pending lease past position #100 still appears in balances.
func TestQueryCreditAccount_IncludesPendingDenomBeyondLegacyCap(t *testing.T) {
	f := initFixture(t)
	k := f.App.BillingKeeper
	querier := keeper.NewQuerier(k)

	tenant := f.TestAccs[0]

	params, err := k.GetParams(f.Ctx)
	require.NoError(t, err)
	params.MaxPendingLeasesPerTenant = 200
	require.NoError(t, k.SetParams(f.Ctx, params))

	creditAddr := types.DeriveCreditAddress(tenant)
	require.NoError(t, k.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	}))

	const tailDenom = "tailpendingdenom"
	f.fundAccount(t, creditAddr, sdk.NewCoins(
		sdk.NewCoin(testDenom, sdkmath.NewInt(1_000_000)),
		sdk.NewCoin(tailDenom, sdkmath.NewInt(555)),
	))

	// 120 pending leases; only the last (position 120, by ascending UUID) uses tailDenom. The
	// TenantState MatchExact index iterates ascending by primary key (UUID), so the zero-padded
	// UUIDs place the tail lease last — past the legacy cap of 100 but within the raised param cap.
	const numLeases = 120
	for i := range numLeases {
		denom := testDenom
		if i == numLeases-1 {
			denom = tailDenom
		}
		require.NoError(t, k.SetLease(f.Ctx, types.Lease{
			Uuid:         fmt.Sprintf("01912345-6789-7abc-8def-pending%05d", i),
			Tenant:       tenant.String(),
			ProviderUuid: testProviderUUID,
			Items: []types.LeaseItem{
				{
					SkuUuid:     "01912345-6789-7abc-8def-sku000000001",
					Quantity:    1,
					LockedPrice: sdk.NewCoin(denom, sdkmath.NewInt(1)),
				},
			},
			State:     types.LEASE_STATE_PENDING,
			CreatedAt: f.Ctx.BlockTime(),
		}))
	}

	resp, err := querier.CreditAccount(f.Ctx, &types.QueryCreditAccountRequest{
		Tenant: tenant.String(),
	})
	require.NoError(t, err)
	require.Equal(t, sdkmath.NewInt(555), resp.Balances.AmountOf(tailDenom),
		"denom used only by a pending lease past the legacy 100 cap must appear in balances")
}

// TestQueryCreditEstimate_ActiveCapBoundsIteration verifies the cap on the walk over a tenant's
// streams both (a) never truncates a legitimately-reachable state and (b) still bounds iteration
// for a tenant with more streams than the cap. A tenant has at most one stream per active lease
// and denom, so the cap is the active-lease ceiling times max_items_per_lease. With
// max_leases_per_tenant=5 and max_pending_leases_per_tenant=3 the lease ceiling is 8 (a tenant can
// legitimately reach 7 active leases through the acknowledge overshoot). Every lease below has
// its own provider, hence its own stream(s): at or below the cap all of them must be summed; above
// it the sum is bounded. The over-cap rows guard against a regression that drops the bound.
func TestQueryCreditEstimate_ActiveCapBoundsIteration(t *testing.T) {
	const (
		maxLeases  = 5
		maxPending = 3
		capLimit   = maxLeases + maxPending // 8
		balance    = 1_000_000_000
	)
	denoms := []string{testDenom, testDenom2}

	cases := []struct {
		name string
		// denomsPerLease is both max_items_per_lease and the number of denoms
		// (streams) of every lease.
		denomsPerLease uint64
		numLeases      uint64
		// expectRate is the expected burn rate in each denom.
		expectRate uint64
	}{
		{"below cap (overshoot band)", 1, 7, 7},
		{"exactly at cap", 1, capLimit, capLimit},
		{"one over cap", 1, capLimit + 1, capLimit},
		{"well over cap", 1, 12, capLimit},
		{"two streams per lease, exactly at cap", 2, capLimit, capLimit},
		{"two streams per lease, one lease over cap", 2, capLimit + 1, capLimit},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := initFixture(t)
			k := f.App.BillingKeeper
			querier := keeper.NewQuerier(k)
			tenant := f.TestAccs[0]

			params, err := k.GetParams(f.Ctx)
			require.NoError(t, err)
			params.MaxLeasesPerTenant = maxLeases
			params.MaxPendingLeasesPerTenant = maxPending
			params.MaxItemsPerLease = tc.denomsPerLease
			require.NoError(t, k.SetParams(f.Ctx, params))

			creditAddr := types.DeriveCreditAddress(tenant)
			require.NoError(t, k.SetCreditAccount(f.Ctx, types.CreditAccount{
				Tenant:           tenant.String(),
				CreditAddress:    creditAddr.String(),
				ActiveLeaseCount: tc.numLeases,
			}))

			expectedRate := sdk.NewCoins()
			for _, denom := range denoms[:tc.denomsPerLease] {
				f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(denom, sdkmath.NewInt(balance))))
				expectedRate = expectedRate.Add(sdk.NewCoin(denom, sdkmath.NewIntFromUint64(tc.expectRate)))
			}

			for i := range tc.numLeases {
				items := make([]types.LeaseItem, 0, tc.denomsPerLease)
				for j, denom := range denoms[:tc.denomsPerLease] {
					items = append(items, types.LeaseItem{
						SkuUuid:     fmt.Sprintf("01912345-6789-7abc-8def-sku00000000%d", j),
						Quantity:    1,
						LockedPrice: sdk.NewCoin(denom, sdkmath.NewInt(1)),
					})
				}
				require.NoError(t, k.SetLease(f.Ctx, types.Lease{
					Uuid:         fmt.Sprintf("01912345-6789-7abc-8def-bound%07d", i),
					Tenant:       tenant.String(),
					ProviderUuid: fmt.Sprintf("01912345-6789-7abc-8def-prov%08d", i),
					Items:        items,
					State:        types.LEASE_STATE_ACTIVE,
					CreatedAt:    f.Ctx.BlockTime(),
				}))
			}
			// The leases were written straight into the store: build their streams.
			f.syncStreams(t, f.Ctx.BlockTime())

			byTenant, err := querier.StreamsByTenant(f.Ctx, &types.QueryStreamsByTenantRequest{
				Tenant:     tenant.String(),
				Pagination: &query.PageRequest{Limit: 1, CountTotal: true},
			})
			require.NoError(t, err)
			require.Equal(t, tc.numLeases*tc.denomsPerLease, byTenant.Pagination.Total,
				"sanity: one stream per lease and denom")

			resp, err := querier.CreditEstimate(f.Ctx, &types.QueryCreditEstimateRequest{Tenant: tenant.String()})
			require.NoError(t, err)
			require.Equal(t, expectedRate, resp.TotalRatePerSecond,
				"stream rate sum must equal min(numLeases, cap) in every denom")
			require.Equal(t, balance/tc.expectRate, resp.EstimatedDurationSeconds)
			require.Equal(t, tc.numLeases, resp.ActiveLeaseCount,
				"ActiveLeaseCount is the credit account's count, not the number of streams walked")
		})
	}
}

// TestQueryCreditEstimate_CoversAcknowledgeOvershoot drives real CreateLease/AcknowledgeLease
// messages to reach the pending->active acknowledge overshoot (active count exceeds
// max_leases_per_tenant), then verifies CreditEstimate counts and sums ALL active leases. This is
// the end-to-end guard for the reachable-ceiling cap: a max_leases_per_tenant-only cap would
// truncate the overshooting leases from the burn-rate sum.
func TestQueryCreditEstimate_CoversAcknowledgeOvershoot(t *testing.T) {
	f := initFixture(t)
	k := f.App.BillingKeeper
	msgServer := keeper.NewMsgServerImpl(k)
	querier := keeper.NewQuerier(k)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]

	// Low limits so the overshoot is reachable with a handful of messages:
	// reachable active = MaxLeasesPerTenant-1 + MaxPendingLeasesPerTenant = 4 + 3 = 7 > MaxLeasesPerTenant(5).
	params, err := k.GetParams(f.Ctx)
	require.NoError(t, err)
	params.MaxLeasesPerTenant = 5
	params.MaxPendingLeasesPerTenant = 3
	require.NoError(t, k.SetParams(f.Ctx, params))

	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600) // 3600 umfx/hour = 1 umfx/second per unit

	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	require.NoError(t, k.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	}))
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin("umfx", sdkmath.NewInt(1_000_000_000))))

	// createAndAck creates n pending leases (respecting the pending gate, n <= MaxPendingLeasesPerTenant)
	// and acknowledges them, moving the active count up by n.
	createAndAck := func(n int) {
		uuids := make([]string, 0, n)
		for range n {
			resp, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
				Tenant: tenant.String(),
				Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
			})
			require.NoError(t, err)
			uuids = append(uuids, resp.LeaseUuid)
		}
		_, err := msgServer.AcknowledgeLease(f.Ctx, &types.MsgAcknowledgeLease{
			Sender:     providerAddr.String(),
			LeaseUuids: uuids,
		})
		require.NoError(t, err)
	}

	createAndAck(3) // active 3
	createAndAck(1) // active 4 = MaxLeasesPerTenant-1
	createAndAck(3) // active 7 -> overshoot past MaxLeasesPerTenant(5)

	ca, err := k.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)
	require.Equal(t, uint64(7), ca.ActiveLeaseCount, "sanity: overshoot state reached via real handlers")

	resp, err := querier.CreditEstimate(f.Ctx, &types.QueryCreditEstimateRequest{Tenant: tenant.String()})
	require.NoError(t, err)
	require.Equal(t, uint64(7), resp.ActiveLeaseCount, "all overshoot active leases must be counted")
	require.Equal(t, sdkmath.NewInt(7), resp.TotalRatePerSecond.AmountOf("umfx"),
		"burn rate must sum all 7 active leases (1 umfx/sec each), not truncate at max_leases_per_tenant")
	require.Equal(t, uint64(1_000_000_000/7), resp.EstimatedDurationSeconds)

	// All 7 leases share one stream with the provider, which is what the estimate reads.
	state, err := k.Streams.Get(f.Ctx, keeper.NewStreamKey(provider.Uuid, tenant, "umfx"))
	require.NoError(t, err)
	require.Equal(t, sdkmath.NewInt(7), state.Rate)
	require.Equal(t, uint64(7), state.LeaseCount)
}

// TestQueryCreditAccount_ActiveDenomCapBounds verifies the denom-collection cap bounds iteration:
// a denom used only by an active lease PAST the cap is excluded from the balances response. With
// max_leases_per_tenant=5 and max_pending_leases_per_tenant=3 the active cap is 8, so a tail denom
// on the 10th active lease (by ascending UUID) must not appear, while denoms within the cap do.
func TestQueryCreditAccount_ActiveDenomCapBounds(t *testing.T) {
	f := initFixture(t)
	k := f.App.BillingKeeper
	querier := keeper.NewQuerier(k)
	tenant := f.TestAccs[0]

	params, err := k.GetParams(f.Ctx)
	require.NoError(t, err)
	params.MaxLeasesPerTenant = 5
	params.MaxPendingLeasesPerTenant = 3
	require.NoError(t, k.SetParams(f.Ctx, params))

	creditAddr := types.DeriveCreditAddress(tenant)
	require.NoError(t, k.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	}))

	const tailDenom = "beyondcapdenom"
	f.fundAccount(t, creditAddr, sdk.NewCoins(
		sdk.NewCoin(testDenom, sdkmath.NewInt(1_000_000)),
		sdk.NewCoin(tailDenom, sdkmath.NewInt(999)),
	))

	// 10 active leases (> cap of 8); tailDenom only on the last (position 10, ascending UUID).
	const numLeases = 10
	for i := range numLeases {
		denom := testDenom
		if i == numLeases-1 {
			denom = tailDenom
		}
		require.NoError(t, k.SetLease(f.Ctx, types.Lease{
			Uuid:         fmt.Sprintf("01912345-6789-7abc-8def-capbnd%06d", i),
			Tenant:       tenant.String(),
			ProviderUuid: testProviderUUID,
			Items: []types.LeaseItem{{
				SkuUuid:     "01912345-6789-7abc-8def-sku000000001",
				Quantity:    1,
				LockedPrice: sdk.NewCoin(denom, sdkmath.NewInt(1)),
			}},
			State:     types.LEASE_STATE_ACTIVE,
			CreatedAt: f.Ctx.BlockTime(),
		}))
	}

	resp, err := querier.CreditAccount(f.Ctx, &types.QueryCreditAccountRequest{Tenant: tenant.String()})
	require.NoError(t, err)
	require.True(t, resp.Balances.AmountOf(tailDenom).IsZero(),
		"denom on a lease beyond the active cap must be excluded (iteration is bounded)")
	require.Equal(t, sdkmath.NewInt(1_000_000), resp.Balances.AmountOf(testDenom),
		"denoms within the cap must still be present")
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

	t.Run("ProviderWithdrawable with empty/invalid UUID", func(t *testing.T) {
		// Empty provider_uuid should error
		_, err := querier.ProviderWithdrawable(f.Ctx, &types.QueryProviderWithdrawableRequest{
			ProviderUuid: "",
		})
		require.Equal(t, codes.InvalidArgument, status.Code(err), "empty provider_uuid should error")

		// Invalid UUID format returns empty results (no format validation)
		resp, err := querier.ProviderWithdrawable(f.Ctx, &types.QueryProviderWithdrawableRequest{
			ProviderUuid: "bad-uuid",
		})
		require.NoError(t, err)
		require.True(t, resp.Amounts.IsZero(), "invalid UUID should return zero amounts")
		require.Zero(t, resp.LeaseCount)
	})

	t.Run("StreamsByProvider with empty/invalid UUID", func(t *testing.T) {
		// Empty provider_uuid should error
		_, err := querier.StreamsByProvider(f.Ctx, &types.QueryStreamsByProviderRequest{
			ProviderUuid: "",
		})
		require.Equal(t, codes.InvalidArgument, status.Code(err), "empty provider_uuid should error")

		// Invalid UUID format returns empty results (no format validation)
		resp, err := querier.StreamsByProvider(f.Ctx, &types.QueryStreamsByProviderRequest{
			ProviderUuid: "bad-uuid",
		})
		require.NoError(t, err)
		require.Empty(t, resp.Streams, "invalid UUID should return empty results")
	})

	t.Run("StreamsByTenant with empty/invalid address", func(t *testing.T) {
		// Empty tenant should error
		_, err := querier.StreamsByTenant(f.Ctx, &types.QueryStreamsByTenantRequest{
			Tenant: "",
		})
		require.Equal(t, codes.InvalidArgument, status.Code(err), "empty tenant should error")

		// Invalid address should error (the address is decoded to build the key)
		_, err = querier.StreamsByTenant(f.Ctx, &types.QueryStreamsByTenantRequest{
			Tenant: "not-an-address",
		})
		require.Equal(t, codes.InvalidArgument, status.Code(err), "invalid tenant address should error")
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

		// Credit estimate for a tenant without a credit account
		_, err = querier.CreditEstimate(f.Ctx, &types.QueryCreditEstimateRequest{
			Tenant: validButNonExistent,
		})
		require.Equal(t, codes.NotFound, status.Code(err), "should error for estimate without a credit account")
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
