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
- QueryProviderWithdrawable: ordered page-local provider withdrawal estimates
*/
package keeper_test

import (
	"encoding/binary"
	"fmt"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"

	sharedpagination "github.com/manifest-network/manifest-ledger/pkg/pagination"
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

func TestListQueriesSupportBoundedSDKPagination(t *testing.T) {
	f := initFixture(t)
	k := f.App.BillingKeeper
	querier := keeper.NewQuerier(k)
	tenant := f.TestAccs[0]
	require.NoError(t, k.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: types.DeriveCreditAddress(tenant).String(),
	}))
	require.NoError(t, k.SetLease(f.Ctx, types.Lease{
		Uuid:         testLeaseUUID1,
		Tenant:       tenant.String(),
		ProviderUuid: testProviderUUID,
		Items: []types.LeaseItem{{
			SkuUuid:     testSKUUUID,
			Quantity:    1,
			LockedPrice: sdk.NewInt64Coin(testDenom, 1),
		}},
		State:                      types.LEASE_STATE_ACTIVE,
		CreatedAt:                  f.Ctx.BlockTime(),
		MinLeaseDurationAtCreation: 1,
		Reservation:                &types.LeaseReservation{RemainingAmounts: sdk.NewCoins()},
	}))

	queries := []struct {
		name string
		key  []byte
		call func(*query.PageRequest) (*query.PageResponse, error)
	}{
		{
			name: "leases",
			key:  []byte(testLeaseUUID1),
			call: func(pageReq *query.PageRequest) (*query.PageResponse, error) {
				resp, err := querier.Leases(f.Ctx, &types.QueryLeasesRequest{Pagination: pageReq})
				if err != nil {
					return nil, err
				}
				return resp.Pagination, nil
			},
		},
		{
			name: "leases by tenant",
			key:  []byte(testLeaseUUID1),
			call: func(pageReq *query.PageRequest) (*query.PageResponse, error) {
				resp, err := querier.LeasesByTenant(f.Ctx, &types.QueryLeasesByTenantRequest{
					Tenant:     tenant.String(),
					Pagination: pageReq,
				})
				if err != nil {
					return nil, err
				}
				return resp.Pagination, nil
			},
		},
		{
			name: "leases by provider",
			key:  []byte(testLeaseUUID1),
			call: func(pageReq *query.PageRequest) (*query.PageResponse, error) {
				resp, err := querier.LeasesByProvider(f.Ctx, &types.QueryLeasesByProviderRequest{
					ProviderUuid: testProviderUUID,
					Pagination:   pageReq,
				})
				if err != nil {
					return nil, err
				}
				return resp.Pagination, nil
			},
		},
		{
			name: "credit accounts",
			key:  []byte(tenant),
			call: func(pageReq *query.PageRequest) (*query.PageResponse, error) {
				resp, err := querier.CreditAccounts(f.Ctx, &types.QueryCreditAccountsRequest{Pagination: pageReq})
				if err != nil {
					return nil, err
				}
				return resp.Pagination, nil
			},
		},
		{
			name: "leases by SKU",
			key:  []byte(testLeaseUUID1),
			call: func(pageReq *query.PageRequest) (*query.PageResponse, error) {
				resp, err := querier.LeasesBySKU(f.Ctx, &types.QueryLeasesBySKURequest{
					SkuUuid:    testSKUUUID,
					Pagination: pageReq,
				})
				if err != nil {
					return nil, err
				}
				return resp.Pagination, nil
			},
		},
	}

	for _, queryCase := range queries {
		t.Run(queryCase.name+"/bounded offset and count total", func(t *testing.T) {
			pageRes, err := queryCase.call(&query.PageRequest{Offset: 1, Limit: 1, CountTotal: true})
			require.NoError(t, err)
			require.NotNil(t, pageRes)
			require.Equal(t, uint64(1), pageRes.Total)
		})
		t.Run(queryCase.name+"/count total ignored with key", func(t *testing.T) {
			pageRes, err := queryCase.call(&query.PageRequest{Key: queryCase.key, CountTotal: true})
			require.NoError(t, err)
			require.NotNil(t, pageRes)
			require.Zero(t, pageRes.Total)
		})
		t.Run(queryCase.name+"/key and offset", func(t *testing.T) {
			_, err := queryCase.call(&query.PageRequest{Key: queryCase.key, Offset: 1})
			require.Equal(t, codes.InvalidArgument, status.Code(err))
		})
	}
}

func TestCreditAccounts_CountTotalScanCeilingReturnsResourceExhausted(t *testing.T) {
	f := initFixture(t)
	k := f.App.BillingKeeper
	account := types.CreditAccount{
		Tenant:        f.TestAccs[0].String(),
		CreditAddress: types.DeriveCreditAddress(f.TestAccs[0]).String(),
	}

	for i := uint64(0); i <= sharedpagination.MaxOffsetCountTotalScanLimit; i++ {
		var addressBytes [20]byte
		binary.BigEndian.PutUint64(addressBytes[12:], i+1)
		require.NoError(t, k.CreditAccounts.Set(f.Ctx, sdk.AccAddress(addressBytes[:]), account))
	}

	_, err := keeper.NewQuerier(k).CreditAccounts(f.Ctx, &types.QueryCreditAccountsRequest{
		Pagination: &query.PageRequest{Limit: 1, CountTotal: true},
	})
	require.Equal(t, codes.ResourceExhausted, status.Code(err))
	require.ErrorContains(t, err, "use pagination.key")
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
		State:                      types.LEASE_STATE_ACTIVE,
		CreatedAt:                  f.Ctx.BlockTime(),
		MinLeaseDurationAtCreation: 1,
		Reservation:                &types.LeaseReservation{RemainingAmounts: sdk.NewCoins()},
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
			State:                      state,
			CreatedAt:                  f.Ctx.BlockTime(),
			ClosedAt:                   closedAt,
			MinLeaseDurationAtCreation: 1,
			Reservation:                &types.LeaseReservation{RemainingAmounts: sdk.NewCoins()},
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
			State:                      types.LEASE_STATE_ACTIVE,
			CreatedAt:                  f.Ctx.BlockTime(),
			MinLeaseDurationAtCreation: 1,
			Reservation:                &types.LeaseReservation{RemainingAmounts: sdk.NewCoins()},
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
			State:                      types.LEASE_STATE_ACTIVE,
			CreatedAt:                  f.Ctx.BlockTime(),
			MinLeaseDurationAtCreation: 1,
			Reservation:                &types.LeaseReservation{RemainingAmounts: sdk.NewCoins()},
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
			State:                      types.LEASE_STATE_ACTIVE,
			CreatedAt:                  f.Ctx.BlockTime(),
			MinLeaseDurationAtCreation: 1,
			Reservation:                &types.LeaseReservation{RemainingAmounts: sdk.NewCoins()},
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
			State:                      types.LEASE_STATE_ACTIVE,
			CreatedAt:                  f.Ctx.BlockTime(),
			MinLeaseDurationAtCreation: 1,
			Reservation:                &types.LeaseReservation{RemainingAmounts: sdk.NewCoins()},
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
			State:                      types.LEASE_STATE_ACTIVE,
			CreatedAt:                  f.Ctx.BlockTime(),
			MinLeaseDurationAtCreation: 1,
			Reservation:                &types.LeaseReservation{RemainingAmounts: sdk.NewCoins()},
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
			State:                      types.LEASE_STATE_ACTIVE,
			CreatedAt:                  f.Ctx.BlockTime(),
			MinLeaseDurationAtCreation: 1,
			Reservation:                &types.LeaseReservation{RemainingAmounts: sdk.NewCoins()},
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
			State:                      types.LEASE_STATE_ACTIVE,
			CreatedAt:                  f.Ctx.BlockTime(),
			MinLeaseDurationAtCreation: 1,
			Reservation:                &types.LeaseReservation{RemainingAmounts: sdk.NewCoins()},
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
	skuUUID := testSKUUUID

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
			State:                      types.LEASE_STATE_ACTIVE,
			CreatedAt:                  f.Ctx.BlockTime(),
			MinLeaseDurationAtCreation: 1,
			Reservation:                &types.LeaseReservation{RemainingAmounts: sdk.NewCoins()},
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

func TestQueryLeasesBySKUBoundedSDKPagination(t *testing.T) {
	f := initFixture(t)

	k := f.App.BillingKeeper
	querier := keeper.NewQuerier(k)

	tenant := f.TestAccs[0]
	providerUUID := testProviderUUID
	skuUUID := testSKUUUID

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
			State:                      types.LEASE_STATE_ACTIVE,
			CreatedAt:                  f.Ctx.BlockTime(),
			MinLeaseDurationAtCreation: 1,
			Reservation:                &types.LeaseReservation{RemainingAmounts: sdk.NewCoins()},
		}
		require.NoError(t, k.SetLease(f.Ctx, lease))
	}

	t.Run("offset and count total are exact", func(t *testing.T) {
		page, err := querier.LeasesBySKU(f.Ctx, &types.QueryLeasesBySKURequest{
			SkuUuid:    skuUUID,
			Pagination: &query.PageRequest{Offset: 1, Limit: 2, CountTotal: true},
		})
		require.NoError(t, err)
		require.Len(t, page.Leases, 2)
		require.Equal(t, uint64(5), page.Pagination.Total)
		require.Equal(t, "01912345-6789-7abc-8def-leasecount02", page.Leases[0].Uuid)
	})

	t.Run("cursor continues correctly", func(t *testing.T) {
		page1, err := querier.LeasesBySKU(f.Ctx, &types.QueryLeasesBySKURequest{
			SkuUuid:    skuUUID,
			Pagination: &query.PageRequest{Limit: 2},
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

func TestQueryLeasesBySKU_StateFilterScanCeiling(t *testing.T) {
	f := initFixture(t)
	k := f.App.BillingKeeper
	querier := keeper.NewQuerier(k)
	tenant := f.TestAccs[0]
	lastLeaseUUID := ""

	for i := uint64(0); i <= sharedpagination.MaxPageScanLimit; i++ {
		leaseUUID := fmt.Sprintf("01912345-6789-7abc-8def-%012d", i)
		state := types.LEASE_STATE_CLOSED
		if i == sharedpagination.MaxPageScanLimit {
			state = types.LEASE_STATE_ACTIVE
			lastLeaseUUID = leaseUUID
		}
		require.NoError(t, k.SetLease(f.Ctx, types.Lease{
			Uuid:         leaseUUID,
			Tenant:       tenant.String(),
			ProviderUuid: testProviderUUID,
			Items: []types.LeaseItem{{
				SkuUuid:     testSKUUUID,
				Quantity:    1,
				LockedPrice: sdk.NewInt64Coin(testDenom, 1),
			}},
			State:                      state,
			CreatedAt:                  f.Ctx.BlockTime(),
			MinLeaseDurationAtCreation: 1,
			Reservation:                &types.LeaseReservation{RemainingAmounts: sdk.NewCoins()},
		}))
	}

	_, err := querier.LeasesBySKU(f.Ctx, &types.QueryLeasesBySKURequest{
		SkuUuid:     testSKUUUID,
		StateFilter: types.LEASE_STATE_ACTIVE,
		Pagination:  &query.PageRequest{Limit: 1, CountTotal: true},
	})
	require.Equal(t, codes.ResourceExhausted, status.Code(err),
		"an exact filtered total must fail rather than exceed its value-decode budget")

	firstPage, err := querier.LeasesBySKU(f.Ctx, &types.QueryLeasesBySKURequest{
		SkuUuid:     testSKUUUID,
		StateFilter: types.LEASE_STATE_ACTIVE,
		Pagination:  &query.PageRequest{Limit: 1},
	})
	require.NoError(t, err)
	require.Empty(t, firstPage.Leases)
	require.Equal(t, []byte(lastLeaseUUID), firstPage.Pagination.NextKey)

	secondPage, err := querier.LeasesBySKU(f.Ctx, &types.QueryLeasesBySKURequest{
		SkuUuid:     testSKUUUID,
		StateFilter: types.LEASE_STATE_ACTIVE,
		Pagination:  &query.PageRequest{Key: firstPage.Pagination.NextKey, Limit: 1},
	})
	require.NoError(t, err)
	require.Len(t, secondPage.Leases, 1)
	require.Equal(t, lastLeaseUUID, secondPage.Leases[0].Uuid)
	require.Empty(t, secondPage.Pagination.NextKey)
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

func TestQueryCreditAccountPaginatesAllBankBalances(t *testing.T) {
	f := initFixture(t)
	k := f.App.BillingKeeper
	querier := keeper.NewQuerier(k)
	tenant := f.TestAccs[0]
	creditAddr := types.DeriveCreditAddress(tenant)

	reserved := sdk.NewCoins(sdk.NewInt64Coin("bbeta", 2))
	require.NoError(t, k.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:          tenant.String(),
		CreditAddress:   creditAddr.String(),
		ReservedAmounts: reserved,
	}))
	f.fundAccount(t, creditAddr, sdk.NewCoins(
		sdk.NewInt64Coin("aalpha", 5),
		sdk.NewInt64Coin("bbeta", 10),
		sdk.NewInt64Coin("cgamma", 15),
	))

	var key []byte
	balances := sdk.NewCoins()
	available := sdk.NewCoins()
	for pages := 0; ; pages++ {
		require.Less(t, pages, 4, "cursor pagination must terminate")
		response, err := querier.CreditAccount(f.Ctx, &types.QueryCreditAccountRequest{
			Tenant: tenant.String(),
			Pagination: &query.PageRequest{
				Key:   key,
				Limit: 1,
			},
		})
		require.NoError(t, err)
		require.Len(t, response.Balances, 1)
		balances = balances.Add(response.Balances...)
		available = available.Add(response.AvailableBalances...)
		if len(response.Pagination.NextKey) == 0 {
			break
		}
		key = response.Pagination.NextKey
	}
	require.Equal(t, sdk.NewCoins(
		sdk.NewInt64Coin("aalpha", 5),
		sdk.NewInt64Coin("bbeta", 10),
		sdk.NewInt64Coin("cgamma", 15),
	), balances)
	require.Equal(t, sdk.NewCoins(
		sdk.NewInt64Coin("aalpha", 5),
		sdk.NewInt64Coin("bbeta", 8),
		sdk.NewInt64Coin("cgamma", 15),
	), available)

	reverse, err := querier.CreditAccount(f.Ctx, &types.QueryCreditAccountRequest{
		Tenant: tenant.String(),
		Pagination: &query.PageRequest{
			Limit:   2,
			Reverse: true,
		},
	})
	require.NoError(t, err)
	require.Equal(t, sdk.Coins{
		sdk.NewInt64Coin("cgamma", 15),
		sdk.NewInt64Coin("bbeta", 10),
	}, reverse.Balances)
	require.Equal(t, sdk.Coins{
		sdk.NewInt64Coin("cgamma", 15),
		sdk.NewInt64Coin("bbeta", 8),
	}, reverse.AvailableBalances)
	require.NotEmpty(t, reverse.Pagination.NextKey)

	for _, pagination := range []*query.PageRequest{
		{Offset: 1, Limit: 1},
		{CountTotal: true, Limit: 1},
	} {
		_, err := querier.CreditAccount(f.Ctx, &types.QueryCreditAccountRequest{
			Tenant:     tenant.String(),
			Pagination: pagination,
		})
		require.Equal(t, codes.InvalidArgument, status.Code(err))
	}
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
		State:                      types.LEASE_STATE_ACTIVE,
		CreatedAt:                  f.Ctx.BlockTime(),
		LastSettledAt:              f.Ctx.BlockTime(),
		MinLeaseDurationAtCreation: 1,
		Reservation:                &types.LeaseReservation{RemainingAmounts: sdk.NewCoins()},
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

func TestArithmeticQueries_HandleOverflowWithoutPanicking(t *testing.T) {
	f := initFixture(t)
	k := f.App.BillingKeeper
	querier := keeper.NewQuerier(k)
	tenant := f.TestAccs[0]
	creditAddr := types.DeriveCreditAddress(tenant)
	allocation := sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(123)))
	f.fundAccount(t, creditAddr, allocation)

	require.NoError(t, k.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:           tenant.String(),
		CreditAddress:    creditAddr.String(),
		ActiveLeaseCount: 1,
		ReservedAmounts:  append(sdk.Coins(nil), allocation...),
	}))
	lease := types.Lease{
		Uuid:         testLeaseUUID1,
		Tenant:       tenant.String(),
		ProviderUuid: testProviderUUID,
		Items: []types.LeaseItem{{
			SkuUuid:     testSKUUUID,
			Quantity:    2,
			LockedPrice: sdk.NewCoin(testDenom, highBitBillingTestInt()),
		}},
		State:                      types.LEASE_STATE_ACTIVE,
		CreatedAt:                  f.Ctx.BlockTime().Add(-time.Second),
		LastSettledAt:              f.Ctx.BlockTime().Add(-time.Second),
		MinLeaseDurationAtCreation: 1,
		Reservation: &types.LeaseReservation{
			RemainingAmounts: append(sdk.Coins(nil), allocation...),
		},
	}
	require.NoError(t, k.SetLease(f.Ctx, lease))

	t.Run("withdrawable amount", func(t *testing.T) {
		var (
			resp *types.QueryWithdrawableAmountResponse
			err  error
		)
		require.NotPanics(t, func() {
			resp, err = querier.WithdrawableAmount(f.Ctx, &types.QueryWithdrawableAmountRequest{
				LeaseUuid: lease.Uuid,
			})
		})
		require.NoError(t, err)
		require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(123))), resp.Amounts)
	})

	t.Run("credit estimate", func(t *testing.T) {
		var err error
		require.NotPanics(t, func() {
			_, err = querier.CreditEstimate(f.Ctx, &types.QueryCreditEstimateRequest{
				Tenant: tenant.String(),
			})
		})
		require.Equal(t, codes.Internal, status.Code(err))
		require.Contains(t, err.Error(), types.ErrArithmeticOverflow.Error())
	})

	t.Run("credit estimate rejects stored quantity above runtime maximum", func(t *testing.T) {
		lease.Items[0].Quantity = types.MaxQuantityPerItem + 1
		lease.Items[0].LockedPrice = sdk.NewCoin(testDenom, sdkmath.OneInt())
		require.NoError(t, k.SetLease(f.Ctx, lease))

		var err error
		require.NotPanics(t, func() {
			_, err = querier.CreditEstimate(f.Ctx, &types.QueryCreditEstimateRequest{
				Tenant: tenant.String(),
			})
		})
		require.Equal(t, codes.Internal, status.Code(err))
		require.Contains(t, err.Error(), types.ErrInvalidQuantity.Error())
	})
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
			State:                      types.LEASE_STATE_ACTIVE,
			CreatedAt:                  f.Ctx.BlockTime(),
			LastSettledAt:              f.Ctx.BlockTime(),
			MinLeaseDurationAtCreation: 1,
			Reservation:                &types.LeaseReservation{RemainingAmounts: sdk.NewCoins()},
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
	require.Equal(t, uint64(0), resp.LeaseCount) // No settlement or auto-close succeeds yet

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
	require.Equal(t, uint64(3), resp.LeaseCount) // Three successful simulated settlements

	// Query with empty provider_uuid
	_, err = querier.ProviderWithdrawable(f.Ctx, &types.QueryProviderWithdrawableRequest{
		ProviderUuid: "",
	})
	require.Error(t, err)

	// Query with nil request
	_, err = querier.ProviderWithdrawable(f.Ctx, nil)
	require.Error(t, err)

	// A missing provider must fail even when its ACTIVE index is empty.
	_, err = querier.ProviderWithdrawable(f.Ctx, &types.QueryProviderWithdrawableRequest{
		ProviderUuid: "01912345-6789-7abc-8def-000000009999",
	})
	require.Equal(t, codes.NotFound, status.Code(err))

	// Test pagination with page limit
	t.Run("pagination with limit", func(t *testing.T) {
		// Query with limit=2 - should return partial results plus a cursor
		resp, err := querier.ProviderWithdrawable(newCtx, &types.QueryProviderWithdrawableRequest{
			ProviderUuid: provider.Uuid,
			Pagination:   &query.PageRequest{Limit: 2},
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.NotEmpty(t, resp.Pagination.NextKey) // More leases exist beyond the page
		// LeaseCount follows transaction WithdrawalCount for the two simulated leases.
		require.Equal(t, uint64(2), resp.LeaseCount)
	})

	t.Run("empty next_key when all leases processed", func(t *testing.T) {
		// Query with high limit - should process all leases
		resp, err := querier.ProviderWithdrawable(newCtx, &types.QueryProviderWithdrawableRequest{
			ProviderUuid: provider.Uuid,
			Pagination:   &query.PageRequest{Limit: 100},
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Empty(t, resp.Pagination.NextKey) // All leases processed
		require.Equal(t, uint64(3), resp.LeaseCount)
	})

	t.Run("default limit applied when pagination omitted", func(t *testing.T) {
		// nil pagination should use the transaction-aligned default page size (50)
		resp, err := querier.ProviderWithdrawable(newCtx, &types.QueryProviderWithdrawableRequest{
			ProviderUuid: provider.Uuid,
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Empty(t, resp.Pagination.NextKey) // Default limit (50) exceeds the 3 active leases
		require.Equal(t, uint64(3), resp.LeaseCount)
	})

	t.Run("limit capped at maximum", func(t *testing.T) {
		// Page limit exceeding the max should be capped, not rejected
		resp, err := querier.ProviderWithdrawable(newCtx, &types.QueryProviderWithdrawableRequest{
			ProviderUuid: provider.Uuid,
			Pagination:   &query.PageRequest{Limit: 10000}, // Exceeds MaxProviderWithdrawableQueryLimit (100)
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		// Should still work, just capped at max
		require.Equal(t, uint64(3), resp.LeaseCount)
	})

	t.Run("rejects unbounded pagination modes", func(t *testing.T) {
		for _, pageReq := range []*query.PageRequest{
			{Offset: 1, Limit: 1},
			{CountTotal: true, Limit: 1},
		} {
			_, err := querier.ProviderWithdrawable(newCtx, &types.QueryProviderWithdrawableRequest{
				ProviderUuid: provider.Uuid,
				Pagination:   pageReq,
			})
			require.Equal(t, codes.InvalidArgument, status.Code(err))
		}
	})

	t.Run("cursor drains all pages with ample credit", func(t *testing.T) {
		// Full total and count in one page.
		full, err := querier.ProviderWithdrawable(newCtx, &types.QueryProviderWithdrawableRequest{
			ProviderUuid: provider.Uuid,
			Pagination:   &query.PageRequest{Limit: 100},
		})
		require.NoError(t, err)
		require.Empty(t, full.Pagination.NextKey)

		// This fixture has enough unreserved credit for every lease, so its page
		// estimates happen to add up. That is not the API contract: constrained
		// same-tenant pages share balances and must be re-queried after execution.
		total := sdk.NewCoins()
		var count uint64
		var key []byte
		pages := 0
		for {
			resp, err := querier.ProviderWithdrawable(newCtx, &types.QueryProviderWithdrawableRequest{
				ProviderUuid: provider.Uuid,
				Pagination:   &query.PageRequest{Limit: 1, Key: key},
			})
			require.NoError(t, err)
			total = total.Add(resp.Amounts...)
			count += resp.LeaseCount
			pages++
			require.Less(t, pages, 100, "pagination must terminate")
			if len(resp.Pagination.NextKey) == 0 {
				break
			}
			key = resp.Pagination.NextKey
		}
		require.Equal(t, full.Amounts, total, "paged total must equal the single-page total")
		require.Equal(t, full.LeaseCount, count)
	})
}

func TestQueryProviderWithdrawable_ClampsPageToTransactionMaximum(t *testing.T) {
	f := initFixture(t)
	k := f.App.BillingKeeper
	querier := keeper.NewQuerier(k)

	tenant := f.TestAccs[0]
	providerAddress := f.TestAccs[1]
	payoutAddress := f.TestAccs[2]
	provider := f.createTestProvider(t, providerAddress.String(), payoutAddress.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)
	creditAddress := types.DeriveCreditAddress(tenant)
	now := f.Ctx.BlockTime()
	leaseCount := types.MaxProviderWithdrawableQueryLimit + 1
	leaseUUIDs := make([]string, 0, leaseCount)
	allocation := sdk.NewCoins(sdk.NewInt64Coin(testDenom, 1))

	for i := uint64(1); i <= leaseCount; i++ {
		leaseUUID := fmt.Sprintf("01912345-6789-7abc-8def-%012d", i)
		leaseUUIDs = append(leaseUUIDs, leaseUUID)
		require.NoError(t, k.SetLease(f.Ctx, types.Lease{
			Uuid:         leaseUUID,
			Tenant:       tenant.String(),
			ProviderUuid: provider.Uuid,
			Items: []types.LeaseItem{{
				SkuUuid:     sku.Uuid,
				Quantity:    1,
				LockedPrice: sdk.NewInt64Coin(testDenom, 1),
			}},
			State:                      types.LEASE_STATE_ACTIVE,
			CreatedAt:                  now,
			LastSettledAt:              now,
			MinLeaseDurationAtCreation: 1,
			Reservation: &types.LeaseReservation{
				RemainingAmounts: append(sdk.Coins(nil), allocation...),
			},
		}))
	}

	require.NoError(t, k.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:           tenant.String(),
		CreditAddress:    creditAddress.String(),
		ActiveLeaseCount: leaseCount,
		ReservedAmounts: sdk.NewCoins(
			sdk.NewInt64Coin(testDenom, int64(leaseCount)),
		),
	}))
	f.fundAccount(t, creditAddress, sdk.NewCoins(
		sdk.NewInt64Coin(testDenom, int64(leaseCount*2)),
	))

	queryCtx := f.Ctx.WithBlockTime(now.Add(time.Second))
	firstPage, err := querier.ProviderWithdrawable(queryCtx, &types.QueryProviderWithdrawableRequest{
		ProviderUuid: provider.Uuid,
		Pagination: &query.PageRequest{
			Limit: leaseCount, // Requests 101; the query clamps this to the transaction max of 100.
		},
	})
	require.NoError(t, err)
	require.Equal(t, types.MaxProviderWithdrawableQueryLimit, firstPage.LeaseCount)
	require.Equal(t, sdkmath.NewInt(int64(types.MaxProviderWithdrawableQueryLimit)),
		firstPage.Amounts.AmountOf(testDenom))
	require.Empty(t, firstPage.FailedLeaseUuids)
	require.Equal(t, []byte(leaseUUIDs[types.MaxProviderWithdrawableQueryLimit]),
		firstPage.Pagination.NextKey, "the cursor must identify the one unread lease")

	secondPage, err := querier.ProviderWithdrawable(queryCtx, &types.QueryProviderWithdrawableRequest{
		ProviderUuid: provider.Uuid,
		Pagination: &query.PageRequest{
			Key:   firstPage.Pagination.NextKey,
			Limit: leaseCount,
		},
	})
	require.NoError(t, err)
	require.Equal(t, uint64(1), secondPage.LeaseCount)
	require.Equal(t, sdkmath.OneInt(), secondPage.Amounts.AmountOf(testDenom))
	require.Empty(t, secondPage.FailedLeaseUuids)
	require.Empty(t, secondPage.Pagination.NextKey)
}

func TestQueryProviderWithdrawable_DryRunCountsSharedCreditOncePerPage(t *testing.T) {
	f := initFixture(t)
	k := f.App.BillingKeeper
	querier := keeper.NewQuerier(k)
	msgServer := keeper.NewMsgServerImpl(k)

	tenant := f.TestAccs[0]
	providerAddress := f.TestAccs[1]
	provider := f.createTestProvider(t, providerAddress.String(), providerAddress.String())
	creditAddr := types.DeriveCreditAddress(tenant)
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewInt64Coin(testDenom, 30)))

	now := f.Ctx.BlockTime()
	allocation := sdk.NewCoins(sdk.NewInt64Coin(testDenom, 10))
	lease := func(uuid string) types.Lease {
		return types.Lease{
			Uuid:         uuid,
			Tenant:       tenant.String(),
			ProviderUuid: provider.Uuid,
			Items: []types.LeaseItem{{
				SkuUuid:     testSKUUUID,
				Quantity:    1,
				LockedPrice: sdk.NewInt64Coin(testDenom, 1),
			}},
			State:                      types.LEASE_STATE_ACTIVE,
			CreatedAt:                  now,
			LastSettledAt:              now,
			MinLeaseDurationAtCreation: 10,
			Reservation: &types.LeaseReservation{
				RemainingAmounts: append(sdk.Coins(nil), allocation...),
			},
		}
	}
	leaseA := lease(testLeaseUUID1)
	leaseB := lease(testLeaseUUID2)
	require.NoError(t, k.SetLease(f.Ctx, leaseA))
	require.NoError(t, k.SetLease(f.Ctx, leaseB))
	require.NoError(t, k.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:           tenant.String(),
		CreditAddress:    creditAddr.String(),
		ActiveLeaseCount: 2,
		ReservedAmounts:  sdk.NewCoins(sdk.NewInt64Coin(testDenom, 20)),
	}))

	queryCtx := f.Ctx.WithBlockTime(now.Add(20 * time.Second))
	for _, reverse := range []bool{false, true} {
		t.Run(fmt.Sprintf("reverse=%t", reverse), func(t *testing.T) {
			response, err := querier.ProviderWithdrawable(queryCtx, &types.QueryProviderWithdrawableRequest{
				ProviderUuid: provider.Uuid,
				Pagination: &query.PageRequest{
					Limit:   10,
					Reverse: reverse,
				},
			})
			require.NoError(t, err)
			require.Equal(t, uint64(2), response.LeaseCount)
			require.Equal(t, sdkmath.NewInt(30), response.Amounts.AmountOf(testDenom))
		})
	}

	firstPage, err := querier.ProviderWithdrawable(queryCtx, &types.QueryProviderWithdrawableRequest{
		ProviderUuid: provider.Uuid,
		Pagination:   &query.PageRequest{Limit: 1},
	})
	require.NoError(t, err)
	require.NotEmpty(t, firstPage.Pagination.NextKey)
	require.Equal(t, sdkmath.NewInt(20), firstPage.Amounts.AmountOf(testDenom))
	secondPage, err := querier.ProviderWithdrawable(queryCtx, &types.QueryProviderWithdrawableRequest{
		ProviderUuid: provider.Uuid,
		Pagination: &query.PageRequest{
			Limit: 1,
			Key:   firstPage.Pagination.NextKey,
		},
	})
	require.NoError(t, err)
	require.Equal(t, sdkmath.NewInt(20), secondPage.Amounts.AmountOf(testDenom))
	require.Equal(t, sdkmath.NewInt(40),
		firstPage.Amounts.Add(secondPage.Amounts...).AmountOf(testDenom),
		"separately queried pages are execution estimates, not additive snapshots",
	)

	// Each lease independently reports 20, demonstrating the old provider query's
	// 40-token overcount. The ordered page dry-run caps their combined estimate at
	// the account's actual 30-token balance.
	for _, leaseUUID := range []string{leaseA.Uuid, leaseB.Uuid} {
		response, err := querier.WithdrawableAmount(queryCtx, &types.QueryWithdrawableAmountRequest{
			LeaseUuid: leaseUUID,
		})
		require.NoError(t, err)
		require.Equal(t, sdkmath.NewInt(20), response.Amounts.AmountOf(testDenom))
	}

	// Queries are dry-runs: neither bank funds nor reservation state changes.
	account, err := k.GetCreditAccount(queryCtx, tenant.String())
	require.NoError(t, err)
	require.Equal(t, sdkmath.NewInt(20), account.ReservedAmounts.AmountOf(testDenom))
	require.Equal(t, sdkmath.NewInt(30),
		f.App.BankKeeper.GetBalance(queryCtx, creditAddr, testDenom).Amount)
	for _, leaseUUID := range []string{leaseA.Uuid, leaseB.Uuid} {
		stored, err := k.GetLease(queryCtx, leaseUUID)
		require.NoError(t, err)
		require.NotNil(t, stored.Reservation)
		require.Equal(t, sdkmath.NewInt(10),
			stored.Reservation.RemainingAmounts.AmountOf(testDenom))
	}

	// The forward estimate and the real provider-wide transaction use the same
	// per-lease lifecycle. Here both leases auto-close while competing for one
	// shared balance, so this also pins ordering and page-local cache parity.
	estimate, err := querier.ProviderWithdrawable(queryCtx, &types.QueryProviderWithdrawableRequest{
		ProviderUuid: provider.Uuid,
		Pagination:   &query.PageRequest{Limit: 2},
	})
	require.NoError(t, err)
	withdrawal, err := msgServer.Withdraw(queryCtx, &types.MsgWithdraw{
		Sender:       providerAddress.String(),
		ProviderUuid: provider.Uuid,
		Limit:        2,
	})
	require.NoError(t, err)
	require.Equal(t, estimate.Amounts, withdrawal.TotalAmounts)
	require.Equal(t, estimate.LeaseCount, withdrawal.WithdrawalCount)
	require.Equal(t, sdkmath.NewInt(30), withdrawal.TotalAmounts.AmountOf(testDenom))
	require.Equal(t, uint64(2), withdrawal.WithdrawalCount)
}

func TestQueryProviderWithdrawable_ParityIncludesZeroTransferAutoClose(t *testing.T) {
	f := initFixture(t)
	k := f.App.BillingKeeper
	querier := keeper.NewQuerier(k)
	msgServer := keeper.NewMsgServerImpl(k)

	tenant := f.TestAccs[0]
	providerAddress := f.TestAccs[1]
	payoutAddress := f.TestAccs[2]
	provider := f.createTestProvider(t, providerAddress.String(), payoutAddress.String())
	creditAddress := types.DeriveCreditAddress(tenant)
	now := f.Ctx.BlockTime()
	lease := types.Lease{
		Uuid:         testLeaseUUID1,
		Tenant:       tenant.String(),
		ProviderUuid: provider.Uuid,
		Items: []types.LeaseItem{{
			SkuUuid:     testSKUUUID,
			Quantity:    1,
			LockedPrice: sdk.NewInt64Coin(testDenom, 1),
		}},
		State:                      types.LEASE_STATE_ACTIVE,
		CreatedAt:                  now,
		LastSettledAt:              now,
		MinLeaseDurationAtCreation: 1,
		Reservation:                &types.LeaseReservation{RemainingAmounts: sdk.NewCoins()},
	}
	require.NoError(t, k.SetLease(f.Ctx, lease))
	require.NoError(t, k.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:           tenant.String(),
		CreditAddress:    creditAddress.String(),
		ActiveLeaseCount: 1,
		ReservedAmounts:  sdk.NewCoins(),
	}))

	// With no balance at the required denomination, touching the lease at the
	// same block time auto-closes it without transferring a token.
	estimate, err := querier.ProviderWithdrawable(f.Ctx, &types.QueryProviderWithdrawableRequest{
		ProviderUuid: provider.Uuid,
		Pagination:   &query.PageRequest{Limit: 1},
	})
	require.NoError(t, err)
	require.True(t, estimate.Amounts.IsZero())
	require.Equal(t, uint64(1), estimate.LeaseCount)

	// Query simulation is discarded, including the close and count decrement.
	stored, err := k.GetLease(f.Ctx, lease.Uuid)
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_ACTIVE, stored.State)
	account, err := k.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)
	require.Equal(t, uint64(1), account.ActiveLeaseCount)

	withdrawal, err := msgServer.Withdraw(f.Ctx, &types.MsgWithdraw{
		Sender:       providerAddress.String(),
		ProviderUuid: provider.Uuid,
		Limit:        1,
	})
	require.NoError(t, err)
	require.Equal(t, estimate.Amounts, withdrawal.TotalAmounts)
	require.Equal(t, estimate.LeaseCount, withdrawal.WithdrawalCount)
	require.True(t, withdrawal.TotalAmounts.IsZero())
	require.Equal(t, uint64(1), withdrawal.WithdrawalCount)

	stored, err = k.GetLease(f.Ctx, lease.Uuid)
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_CLOSED, stored.State)
	account, err = k.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)
	require.Zero(t, account.ActiveLeaseCount)
}

func TestQueryProviderWithdrawable_DefaultLimitAndDualCursorsMatchWithdraw(t *testing.T) {
	f := initFixture(t)
	k := f.App.BillingKeeper
	querier := keeper.NewQuerier(k)
	msgServer := keeper.NewMsgServerImpl(k)

	tenant := f.TestAccs[0]
	providerAddress := f.TestAccs[1]
	payoutAddress := f.TestAccs[2]
	provider := f.createTestProvider(t, providerAddress.String(), payoutAddress.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)
	creditAddress := types.DeriveCreditAddress(tenant)
	now := f.Ctx.BlockTime()
	leaseCount := int(types.DefaultProviderWithdrawableQueryLimit) + 1
	leaseUUIDs := make([]string, 0, leaseCount)
	allocation := sdk.NewCoins(sdk.NewInt64Coin(testDenom, 1))

	for i := 1; i <= leaseCount; i++ {
		leaseUUID := fmt.Sprintf("01912345-6789-7abc-8def-%012d", i)
		leaseUUIDs = append(leaseUUIDs, leaseUUID)
		require.NoError(t, k.SetLease(f.Ctx, types.Lease{
			Uuid:         leaseUUID,
			Tenant:       tenant.String(),
			ProviderUuid: provider.Uuid,
			Items: []types.LeaseItem{{
				SkuUuid:     sku.Uuid,
				Quantity:    1,
				LockedPrice: sdk.NewInt64Coin(testDenom, 1),
			}},
			State:                      types.LEASE_STATE_ACTIVE,
			CreatedAt:                  now,
			LastSettledAt:              now,
			MinLeaseDurationAtCreation: 1,
			Reservation: &types.LeaseReservation{
				RemainingAmounts: append(sdk.Coins(nil), allocation...),
			},
		}))
	}

	reserved := sdk.NewCoins(sdk.NewInt64Coin(testDenom, int64(leaseCount)))
	require.NoError(t, k.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:           tenant.String(),
		CreditAddress:    creditAddress.String(),
		ActiveLeaseCount: types.DefaultProviderWithdrawableQueryLimit + 1,
		ReservedAmounts:  reserved,
	}))
	f.fundAccount(t, creditAddress, sdk.NewCoins(sdk.NewInt64Coin(testDenom, int64(leaseCount*2))))

	settleCtx := f.Ctx.WithBlockTime(now.Add(time.Second))
	firstEstimate, err := querier.ProviderWithdrawable(settleCtx, &types.QueryProviderWithdrawableRequest{
		ProviderUuid: provider.Uuid,
	})
	require.NoError(t, err)
	require.Equal(t, types.DefaultProviderWithdrawableQueryLimit, firstEstimate.LeaseCount)
	require.Equal(t, sdkmath.NewInt(int64(types.DefaultProviderWithdrawableQueryLimit)),
		firstEstimate.Amounts.AmountOf(testDenom))
	require.Equal(t, []byte(leaseUUIDs[leaseCount-1]), firstEstimate.Pagination.NextKey,
		"the query cursor is the first unread lease")

	firstWithdrawal, err := msgServer.Withdraw(settleCtx, &types.MsgWithdraw{
		Sender:       providerAddress.String(),
		ProviderUuid: provider.Uuid,
		// Limit zero exercises the transaction's matching default of 50.
	})
	require.NoError(t, err)
	require.Equal(t, firstEstimate.Amounts, firstWithdrawal.TotalAmounts)
	require.Equal(t, firstEstimate.LeaseCount, firstWithdrawal.WithdrawalCount)
	require.True(t, firstWithdrawal.HasMore)
	require.Equal(t, []byte(leaseUUIDs[leaseCount-2]), firstWithdrawal.NextKey,
		"the transaction cursor is the last processed lease")
	require.NotEqual(t, firstEstimate.Pagination.NextKey, firstWithdrawal.NextKey)

	// After the first transaction commits, advance each operation with its own
	// cursor. The next estimates and withdrawals still match exactly.
	secondEstimate, err := querier.ProviderWithdrawable(settleCtx, &types.QueryProviderWithdrawableRequest{
		ProviderUuid: provider.Uuid,
		Pagination: &query.PageRequest{
			Key: firstEstimate.Pagination.NextKey,
		},
	})
	require.NoError(t, err)
	require.Equal(t, uint64(1), secondEstimate.LeaseCount)
	require.Equal(t, sdkmath.OneInt(), secondEstimate.Amounts.AmountOf(testDenom))
	require.Empty(t, secondEstimate.Pagination.NextKey)

	secondWithdrawal, err := msgServer.Withdraw(settleCtx, &types.MsgWithdraw{
		Sender:       providerAddress.String(),
		ProviderUuid: provider.Uuid,
		Key:          firstWithdrawal.NextKey,
	})
	require.NoError(t, err)
	require.Equal(t, secondEstimate.Amounts, secondWithdrawal.TotalAmounts)
	require.Equal(t, secondEstimate.LeaseCount, secondWithdrawal.WithdrawalCount)
	require.False(t, secondWithdrawal.HasMore)
	require.Empty(t, secondWithdrawal.NextKey)
}

func TestQueryProviderWithdrawable_DryRunAppliesAutoCloseReservationRelease(t *testing.T) {
	f := initFixture(t)
	k := f.App.BillingKeeper
	querier := keeper.NewQuerier(k)

	tenant := f.TestAccs[0]
	providerAddress := f.TestAccs[1]
	payoutAddress := f.TestAccs[2]
	provider := f.createTestProvider(t, providerAddress.String(), payoutAddress.String())
	creditAddress := types.DeriveCreditAddress(tenant)
	creditBalance := sdk.NewCoins(
		sdk.NewInt64Coin(testDenom, 5),
		sdk.NewInt64Coin(testDenom2, 20),
	)
	f.fundAccount(t, creditAddress, creditBalance)

	now := f.Ctx.BlockTime()
	firstAllocation := sdk.NewCoins(
		sdk.NewInt64Coin(testDenom, 5),
		sdk.NewInt64Coin(testDenom2, 10),
	)
	secondAllocation := sdk.NewCoins(sdk.NewInt64Coin(testDenom2, 10))
	first := types.Lease{
		Uuid:         testLeaseUUID1,
		Tenant:       tenant.String(),
		ProviderUuid: provider.Uuid,
		Items: []types.LeaseItem{
			{
				SkuUuid:     testSKUUUID,
				Quantity:    1,
				LockedPrice: sdk.NewInt64Coin(testDenom, 1),
			},
			{
				SkuUuid:     reservationRuntimeSKUUUID2,
				Quantity:    1,
				LockedPrice: sdk.NewInt64Coin(testDenom2, 1),
			},
		},
		State:                      types.LEASE_STATE_ACTIVE,
		CreatedAt:                  now,
		LastSettledAt:              now,
		MinLeaseDurationAtCreation: 10,
		Reservation: &types.LeaseReservation{
			RemainingAmounts: append(sdk.Coins(nil), firstAllocation...),
		},
	}
	second := types.Lease{
		Uuid:         testLeaseUUID2,
		Tenant:       tenant.String(),
		ProviderUuid: provider.Uuid,
		Items: []types.LeaseItem{{
			SkuUuid:     testSKUUUID,
			Quantity:    1,
			LockedPrice: sdk.NewInt64Coin(testDenom2, 3),
		}},
		State:                      types.LEASE_STATE_ACTIVE,
		CreatedAt:                  now,
		LastSettledAt:              now,
		MinLeaseDurationAtCreation: 10,
		Reservation: &types.LeaseReservation{
			RemainingAmounts: append(sdk.Coins(nil), secondAllocation...),
		},
	}
	require.NoError(t, k.SetLease(f.Ctx, first))
	require.NoError(t, k.SetLease(f.Ctx, second))
	require.NoError(t, k.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:           tenant.String(),
		CreditAddress:    creditAddress.String(),
		ActiveLeaseCount: 2,
		ReservedAmounts:  append(sdk.Coins(nil), creditBalance...),
	}))

	queryCtx := f.Ctx.WithBlockTime(now.Add(5 * time.Second))
	response, err := querier.ProviderWithdrawable(queryCtx, &types.QueryProviderWithdrawableRequest{
		ProviderUuid: provider.Uuid,
		Pagination:   &query.PageRequest{Limit: 2},
	})
	require.NoError(t, err)
	require.Equal(t, uint64(2), response.LeaseCount)
	require.Equal(t, sdkmath.NewInt(5), response.Amounts.AmountOf(testDenom))
	require.Equal(t, sdkmath.NewInt(20), response.Amounts.AmountOf(testDenom2),
		"the second lease must observe the first auto-close releasing unused reservation")

	// The lifecycle simulation runs in a discarded cache context.
	account, err := k.GetCreditAccount(queryCtx, tenant.String())
	require.NoError(t, err)
	require.Equal(t, uint64(2), account.ActiveLeaseCount)
	require.True(t, creditBalance.Equal(account.ReservedAmounts))
	require.True(t, creditBalance.Equal(f.App.BankKeeper.GetAllBalances(queryCtx, creditAddress)))
	require.True(t, f.App.BankKeeper.GetAllBalances(queryCtx, payoutAddress).IsZero())
	for _, expected := range []types.Lease{first, second} {
		stored, err := k.GetLease(queryCtx, expected.Uuid)
		require.NoError(t, err)
		require.Equal(t, expected, stored)
	}
}

func TestQueryProviderWithdrawable_SkipsFailedLeaseLikeProviderWithdrawal(t *testing.T) {
	f := initFixture(t)
	k := f.App.BillingKeeper
	querier := keeper.NewQuerier(k)
	msgServer := keeper.NewMsgServerImpl(k)

	tenant := f.TestAccs[0]
	providerAddress := f.TestAccs[1]
	payoutAddress := f.TestAccs[2]
	provider := f.createTestProvider(t, providerAddress.String(), payoutAddress.String())
	creditAddress := types.DeriveCreditAddress(tenant)
	f.fundAccount(t, creditAddress, sdk.NewCoins(sdk.NewInt64Coin(testDenom, 20)))
	now := f.Ctx.BlockTime()

	malformed := types.Lease{
		Uuid:         testLeaseUUID1,
		Tenant:       tenant.String(),
		ProviderUuid: provider.Uuid,
		Items: []types.LeaseItem{{
			SkuUuid:     testSKUUUID,
			Quantity:    types.MaxQuantityPerItem + 1,
			LockedPrice: sdk.NewInt64Coin(testDenom, 1),
		}},
		State:                      types.LEASE_STATE_ACTIVE,
		CreatedAt:                  now,
		LastSettledAt:              now,
		MinLeaseDurationAtCreation: 10,
		Reservation:                &types.LeaseReservation{RemainingAmounts: sdk.NewCoins()},
	}
	validAllocation := sdk.NewCoins(sdk.NewInt64Coin(testDenom, 10))
	valid := types.Lease{
		Uuid:         testLeaseUUID2,
		Tenant:       tenant.String(),
		ProviderUuid: provider.Uuid,
		Items: []types.LeaseItem{{
			SkuUuid:     testSKUUUID,
			Quantity:    1,
			LockedPrice: sdk.NewInt64Coin(testDenom, 1),
		}},
		State:                      types.LEASE_STATE_ACTIVE,
		CreatedAt:                  now,
		LastSettledAt:              now,
		MinLeaseDurationAtCreation: 10,
		Reservation: &types.LeaseReservation{
			RemainingAmounts: append(sdk.Coins(nil), validAllocation...),
		},
	}
	require.NoError(t, k.SetLease(f.Ctx, malformed))
	require.NoError(t, k.SetLease(f.Ctx, valid))
	require.NoError(t, k.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:           tenant.String(),
		CreditAddress:    creditAddress.String(),
		ActiveLeaseCount: 2,
		ReservedAmounts:  append(sdk.Coins(nil), validAllocation...),
	}))

	queryCtx := f.Ctx.WithBlockTime(now.Add(5 * time.Second))
	response, err := querier.ProviderWithdrawable(queryCtx, &types.QueryProviderWithdrawableRequest{
		ProviderUuid: provider.Uuid,
		Pagination:   &query.PageRequest{Limit: 2},
	})
	require.NoError(t, err)
	require.Equal(t, uint64(1), response.LeaseCount)
	require.Equal(t, sdkmath.NewInt(5), response.Amounts.AmountOf(testDenom))
	require.Equal(t, []string{malformed.Uuid}, response.FailedLeaseUuids)

	// The query is still a dry-run even when an earlier lease fails.
	account, err := k.GetCreditAccount(queryCtx, tenant.String())
	require.NoError(t, err)
	require.Equal(t, uint64(2), account.ActiveLeaseCount)
	require.True(t, validAllocation.Equal(account.ReservedAmounts))
	require.Equal(t, sdkmath.NewInt(20),
		f.App.BankKeeper.GetBalance(queryCtx, creditAddress, testDenom).Amount)

	withdrawal, err := msgServer.Withdraw(queryCtx, &types.MsgWithdraw{
		Sender:       providerAddress.String(),
		ProviderUuid: provider.Uuid,
		Limit:        2,
	})
	require.NoError(t, err)
	require.Equal(t, response.Amounts, withdrawal.TotalAmounts)
	require.Equal(t, response.LeaseCount, withdrawal.WithdrawalCount)
	require.Equal(t, response.FailedLeaseUuids, withdrawal.FailedLeaseUuids)
}

// TestLeasesByProvider_ActiveCursorSurvivesClosedBoundary is the read-side analogue
// of TestMsgWithdraw_ProviderWideCursorSurvivesClosedBoundaryLease: it closes the
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
			Uuid:                       u,
			Tenant:                     tenant.String(),
			ProviderUuid:               provider.Uuid,
			Items:                      []types.LeaseItem{{SkuUuid: sku.Uuid, Quantity: 1, LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(1))}},
			State:                      types.LEASE_STATE_ACTIVE,
			CreatedAt:                  f.Ctx.BlockTime(),
			LastSettledAt:              f.Ctx.BlockTime(),
			MinLeaseDurationAtCreation: 1,
			Reservation:                &types.LeaseReservation{RemainingAmounts: sdk.NewCoins()},
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
	skuUUID1 := testSKUUUID
	skuUUID2 := "01912345-6789-7abc-8def-0123456789af"

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
			State:                      types.LEASE_STATE_ACTIVE,
			CreatedAt:                  f.Ctx.BlockTime(),
			MinLeaseDurationAtCreation: 1,
			Reservation:                &types.LeaseReservation{RemainingAmounts: sdk.NewCoins()},
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
			State:                      types.LEASE_STATE_ACTIVE,
			CreatedAt:                  f.Ctx.BlockTime(),
			MinLeaseDurationAtCreation: 1,
			Reservation:                &types.LeaseReservation{RemainingAmounts: sdk.NewCoins()},
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
	skuUUID := testSKUUUID

	// Create 5 leases with the same SKU
	for i := 0; i < 5; i++ {
		lease := types.Lease{
			Uuid:         fmt.Sprintf("01912345-6789-7abc-8def-leasepage0%02d", i+1),
			Tenant:       tenant.String(),
			ProviderUuid: providerUUID,
			Items: []types.LeaseItem{
				{SkuUuid: skuUUID, Quantity: 1, LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(100))},
			},
			State:                      types.LEASE_STATE_ACTIVE,
			CreatedAt:                  f.Ctx.BlockTime(),
			MinLeaseDurationAtCreation: 1,
			Reservation:                &types.LeaseReservation{RemainingAmounts: sdk.NewCoins()},
		}
		require.NoError(t, k.SetLease(f.Ctx, lease))
	}

	t.Run("offset beyond results returns empty page", func(t *testing.T) {
		resp, err := querier.LeasesBySKU(f.Ctx, &types.QueryLeasesBySKURequest{
			SkuUuid: skuUUID,
			Pagination: &query.PageRequest{
				Offset: 100,
				Limit:  10,
			},
		})
		require.NoError(t, err)
		require.Empty(t, resp.Leases)
		require.Empty(t, resp.Pagination.NextKey)
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

	t.Run("empty SKU index with pagination", func(t *testing.T) {
		nonExistentSKU := "01912345-6789-7abc-8def-999999999999"
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
			State:                      types.LEASE_STATE_ACTIVE,
			CreatedAt:                  f.Ctx.BlockTime(),
			LastSettledAt:              f.Ctx.BlockTime(),
			MinLeaseDurationAtCreation: 1,
			Reservation:                &types.LeaseReservation{RemainingAmounts: sdk.NewCoins()},
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

func TestQueryCreditEstimateReportsGrossBalanceRunway(t *testing.T) {
	f := initFixture(t)
	k := f.App.BillingKeeper
	querier := keeper.NewQuerier(k)
	tenant := f.TestAccs[0]
	creditAddr := types.DeriveCreditAddress(tenant)
	now := f.Ctx.BlockTime()

	activeReservation := sdk.NewCoins(sdk.NewInt64Coin(testDenom, 10))
	pendingReservation := sdk.NewCoins(sdk.NewInt64Coin(testDenom, 90))
	active := types.Lease{
		Uuid:         testLeaseUUID1,
		Tenant:       tenant.String(),
		ProviderUuid: testProviderUUID,
		Items: []types.LeaseItem{{
			SkuUuid:     testSKUUUID,
			Quantity:    1,
			LockedPrice: sdk.NewInt64Coin(testDenom, 1),
		}},
		State:                      types.LEASE_STATE_ACTIVE,
		CreatedAt:                  now,
		LastSettledAt:              now,
		MinLeaseDurationAtCreation: 10,
		Reservation: &types.LeaseReservation{
			RemainingAmounts: activeReservation,
		},
	}
	pending := active
	pending.Uuid = testLeaseUUID2
	pending.State = types.LEASE_STATE_PENDING
	pending.MinLeaseDurationAtCreation = 90
	pending.Reservation = &types.LeaseReservation{RemainingAmounts: pendingReservation}
	require.NoError(t, k.SetLease(f.Ctx, active))
	require.NoError(t, k.SetLease(f.Ctx, pending))
	require.NoError(t, k.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:            tenant.String(),
		CreditAddress:     creditAddr.String(),
		ActiveLeaseCount:  1,
		PendingLeaseCount: 1,
		ReservedAmounts: sdk.NewCoins(
			sdk.NewInt64Coin(testDenom, 100),
		),
	}))
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewInt64Coin(testDenom, 100)))

	response, err := querier.CreditEstimate(f.Ctx, &types.QueryCreditEstimateRequest{
		Tenant: tenant.String(),
	})
	require.NoError(t, err)
	require.Equal(t, uint64(100), response.EstimatedDurationSeconds,
		"the documented metric is raw balance/rate, not per-lease spendable runway")
	require.Equal(t, sdkmath.OneInt(), response.TotalRatePerSecond.AmountOf(testDenom))
}

func TestQueryCreditEstimate_SaturatesLegitimateUint64Boundary(t *testing.T) {
	tests := []struct {
		name    string
		balance sdkmath.Int
	}{
		{
			name:    "exact max uint64",
			balance: sdkmath.NewIntFromUint64(math.MaxUint64),
		},
		{
			name: "above max uint64",
			balance: func() sdkmath.Int {
				amount, err := sdkmath.NewIntFromUint64(math.MaxUint64).SafeAdd(sdkmath.OneInt())
				require.NoError(t, err)
				return amount
			}(),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := initFixture(t)
			k := f.App.BillingKeeper
			tenant := f.TestAccs[0]
			creditAddr := types.DeriveCreditAddress(tenant)
			f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, tc.balance)))
			require.NoError(t, k.SetCreditAccount(f.Ctx, types.CreditAccount{
				Tenant:           tenant.String(),
				CreditAddress:    creditAddr.String(),
				ActiveLeaseCount: 1,
			}))
			require.NoError(t, k.SetLease(f.Ctx, types.Lease{
				Uuid:         testLeaseUUID1,
				Tenant:       tenant.String(),
				ProviderUuid: testProviderUUID,
				Items: []types.LeaseItem{{
					SkuUuid:     testSKUUUID,
					Quantity:    1,
					LockedPrice: sdk.NewCoin(testDenom, sdkmath.OneInt()),
				}},
				State:                      types.LEASE_STATE_ACTIVE,
				CreatedAt:                  f.Ctx.BlockTime(),
				LastSettledAt:              f.Ctx.BlockTime(),
				MinLeaseDurationAtCreation: 1,
				Reservation:                &types.LeaseReservation{RemainingAmounts: sdk.NewCoins()},
			}))

			resp, err := keeper.NewQuerier(k).CreditEstimate(f.Ctx, &types.QueryCreditEstimateRequest{
				Tenant: tenant.String(),
			})
			require.NoError(t, err)
			require.Equal(t, uint64(math.MaxUint64), resp.EstimatedDurationSeconds)
		})
	}
}

// TestQueryCreditEstimate_StoredActiveCount verifies that CreditEstimate uses the
// credit account's maintained active count instead of the legacy hardcoded cap of 100.
func TestQueryCreditEstimate_StoredActiveCount(t *testing.T) {
	f := initFixture(t)
	k := f.App.BillingKeeper
	querier := keeper.NewQuerier(k)

	tenant := f.TestAccs[0]
	const numLeases = 150

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
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(1_000_000_000))))

	// Create 150 active leases, each 1 unit/sec. The stored aggregate count lets the
	// query sum all 150 while the fixed hard ceiling keeps the scan bounded.
	for i := range numLeases {
		require.NoError(t, k.SetLease(f.Ctx, types.Lease{
			Uuid:         fmt.Sprintf("01912345-6789-7abc-8def-active%06d", i),
			Tenant:       tenant.String(),
			ProviderUuid: testProviderUUID,
			Items: []types.LeaseItem{
				{
					SkuUuid:     "01912345-6789-7abc-8def-sku000000001",
					Quantity:    1,
					LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(1)),
				},
			},
			State:                      types.LEASE_STATE_ACTIVE,
			CreatedAt:                  f.Ctx.BlockTime(),
			LastSettledAt:              f.Ctx.BlockTime(),
			MinLeaseDurationAtCreation: 1,
			Reservation:                &types.LeaseReservation{RemainingAmounts: sdk.NewCoins()},
		}))
	}

	resp, err := querier.CreditEstimate(f.Ctx, &types.QueryCreditEstimateRequest{
		Tenant: tenant.String(),
	})
	require.NoError(t, err)
	require.Equal(t, sdkmath.NewInt(numLeases), resp.TotalRatePerSecond.AmountOf(testDenom),
		"burn rate must sum all active leases, not truncate at the legacy 100 cap")
	require.Equal(t, uint64(numLeases), resp.ActiveLeaseCount)
}

func TestCreditEstimateRejectsCountsAboveHistoricalBound(t *testing.T) {
	f := initFixture(t)
	k := f.App.BillingKeeper
	querier := keeper.NewQuerier(k)
	tenant := f.TestAccs[0]
	require.NoError(t, k.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:           tenant.String(),
		CreditAddress:    types.DeriveCreditAddress(tenant).String(),
		ActiveLeaseCount: types.MaxActiveLeasesPerTenantStateUpperBound + 1,
	}))

	_, err := querier.CreditEstimate(f.Ctx, &types.QueryCreditEstimateRequest{Tenant: tenant.String()})
	require.Equal(t, codes.ResourceExhausted, status.Code(err))
	require.ErrorContains(t, err, types.ErrLeaseQueryLimitExceeded.Error())
}

func TestCreditEstimateRejectsLeaseCountIndexMismatch(t *testing.T) {
	f := initFixture(t)
	k := f.App.BillingKeeper
	querier := keeper.NewQuerier(k)
	tenant := f.TestAccs[0]
	require.NoError(t, k.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:           tenant.String(),
		CreditAddress:    types.DeriveCreditAddress(tenant).String(),
		ActiveLeaseCount: 1,
	}))

	_, err := querier.CreditEstimate(f.Ctx, &types.QueryCreditEstimateRequest{Tenant: tenant.String()})
	require.Equal(t, codes.Internal, status.Code(err))
	require.ErrorContains(t, err, types.ErrReservationInvariant.Error())
}

// TestQueryCreditEstimate_UsesStoredCountAfterParamReduction verifies a governance
// limit reduction cannot hide leases created through the real message handlers.
func TestQueryCreditEstimate_UsesStoredCountAfterParamReduction(t *testing.T) {
	f := initFixture(t)
	k := f.App.BillingKeeper
	msgServer := keeper.NewMsgServerImpl(k)
	querier := keeper.NewQuerier(k)
	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]

	params, err := k.GetParams(f.Ctx)
	require.NoError(t, err)
	params.MaxLeasesPerTenant = 5
	params.MaxPendingLeasesPerTenant = 3
	require.NoError(t, k.SetParams(f.Ctx, params))

	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)
	creditAddr := types.DeriveCreditAddress(tenant)
	require.NoError(t, k.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	}))
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(1_000_000_000))))

	createAndAcknowledge := func(count int) {
		leaseUUIDs := make([]string, 0, count)
		for range count {
			resp, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
				Tenant: tenant.String(),
				Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
			})
			require.NoError(t, err)
			leaseUUIDs = append(leaseUUIDs, resp.LeaseUuid)
		}
		_, err := msgServer.AcknowledgeLease(f.Ctx, &types.MsgAcknowledgeLease{
			Sender:     providerAddr.String(),
			LeaseUuids: leaseUUIDs,
		})
		require.NoError(t, err)
	}

	createAndAcknowledge(3)
	createAndAcknowledge(2)
	account, err := k.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)
	require.Equal(t, uint64(5), account.ActiveLeaseCount)

	params.MaxLeasesPerTenant = 2
	params.MaxPendingLeasesPerTenant = 1
	require.NoError(t, k.SetParams(f.Ctx, params))

	resp, err := querier.CreditEstimate(f.Ctx, &types.QueryCreditEstimateRequest{Tenant: tenant.String()})
	require.NoError(t, err)
	require.Equal(t, uint64(5), resp.ActiveLeaseCount)
	require.Equal(t, sdkmath.NewInt(5), resp.TotalRatePerSecond.AmountOf(testDenom),
		"lowering current limits must not truncate the burn rate of pre-existing active leases")
}

// TestQueryCreditEstimate_RejectsAcknowledgeOvershoot verifies that real message handlers can no
// longer enter the legacy overshoot band and that a failed batch leaves CreditEstimate unchanged.
func TestQueryCreditEstimate_RejectsAcknowledgeOvershoot(t *testing.T) {
	f := initFixture(t)
	k := f.App.BillingKeeper
	msgServer := keeper.NewMsgServerImpl(k)
	querier := keeper.NewQuerier(k)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]

	// Low limits make a post-batch overshoot attempt reachable with a handful of messages.
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

	// createAndAck creates n pending leases and acknowledges them within the active cap.
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

	overshootUUIDs := make([]string, 0, 3)
	for range 3 {
		resp, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
			Tenant: tenant.String(),
			Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
		})
		require.NoError(t, err)
		overshootUUIDs = append(overshootUUIDs, resp.LeaseUuid)
	}

	beforeAccount, err := k.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)
	require.Equal(t, uint64(4), beforeAccount.ActiveLeaseCount)
	require.Equal(t, uint64(3), beforeAccount.PendingLeaseCount)

	_, err = msgServer.AcknowledgeLease(f.Ctx, &types.MsgAcknowledgeLease{
		Sender:     providerAddr.String(),
		LeaseUuids: overshootUUIDs,
	})
	require.ErrorIs(t, err, types.ErrLeaseAcknowledgementActiveCapExceeded)

	afterAccount, err := k.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)
	require.Equal(t, beforeAccount, afterAccount)
	for _, leaseUUID := range overshootUUIDs {
		lease, err := k.GetLease(f.Ctx, leaseUUID)
		require.NoError(t, err)
		require.Equal(t, types.LEASE_STATE_PENDING, lease.State)
		require.Nil(t, lease.AcknowledgedAt)
	}

	resp, err := querier.CreditEstimate(f.Ctx, &types.QueryCreditEstimateRequest{Tenant: tenant.String()})
	require.NoError(t, err)
	require.Equal(t, uint64(4), resp.ActiveLeaseCount)
	require.Equal(t, sdkmath.NewInt(4), resp.TotalRatePerSecond.AmountOf("umfx"),
		"failed acknowledgement must not change the active burn rate")
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

		// Invalid UUID formats are rejected before composite-key construction.
		_, err = querier.LeasesByProvider(f.Ctx, &types.QueryLeasesByProviderRequest{
			ProviderUuid: "invalid-uuid-format",
		})
		require.Equal(t, codes.InvalidArgument, status.Code(err))

		_, err = querier.LeasesByProvider(f.Ctx, &types.QueryLeasesByProviderRequest{
			ProviderUuid: testProviderUUID + "\x00",
		})
		require.Equal(t, codes.InvalidArgument, status.Code(err))

		resp, err := querier.LeasesByProvider(f.Ctx, &types.QueryLeasesByProviderRequest{
			ProviderUuid: "01912345-6789-7abc-8def-999999999999",
		})
		require.NoError(t, err)
		require.Empty(t, resp.Leases, "unknown valid UUID should return empty results")
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

		// Unknown provider identifiers fail before index iteration.
		_, err = querier.ProviderWithdrawable(f.Ctx, &types.QueryProviderWithdrawableRequest{
			ProviderUuid: "bad-uuid",
		})
		require.Equal(t, codes.NotFound, status.Code(err))
	})

	t.Run("LeasesBySKU with empty/invalid UUID", func(t *testing.T) {
		// Empty sku_uuid should error
		_, err := querier.LeasesBySKU(f.Ctx, &types.QueryLeasesBySKURequest{
			SkuUuid: "",
		})
		require.Error(t, err, "empty sku_uuid should error")

		// Invalid UUID formats are rejected before composite-key construction.
		_, err = querier.LeasesBySKU(f.Ctx, &types.QueryLeasesBySKURequest{
			SkuUuid: "invalid",
		})
		require.Equal(t, codes.InvalidArgument, status.Code(err))

		_, err = querier.LeasesBySKU(f.Ctx, &types.QueryLeasesBySKURequest{
			SkuUuid: testSKUUUID + "\x00",
		})
		require.Equal(t, codes.InvalidArgument, status.Code(err))

		resp, err := querier.LeasesBySKU(f.Ctx, &types.QueryLeasesBySKURequest{
			SkuUuid: "01912345-6789-7abc-8def-999999999999",
		})
		require.NoError(t, err)
		require.Empty(t, resp.Leases, "unknown valid UUID should return empty results")
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
