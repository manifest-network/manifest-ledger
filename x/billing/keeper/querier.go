package keeper

import (
	"context"
	"math"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"

	"github.com/manifest-network/manifest-ledger/pkg/pagination"
	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

var _ types.QueryServer = Querier{}

// Querier implements the Query gRPC service.
type Querier struct {
	k Keeper
}

// NewQuerier returns a new Querier instance.
func NewQuerier(keeper Keeper) Querier {
	return Querier{k: keeper}
}

// Params queries the module parameters.
func (q Querier) Params(ctx context.Context, _ *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	params, err := q.k.GetParams(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryParamsResponse{Params: params}, nil
}

// Lease queries a lease by ID.
func (q Querier) Lease(ctx context.Context, req *types.QueryLeaseRequest) (*types.QueryLeaseResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}

	if req.LeaseUuid == "" {
		return nil, status.Error(codes.InvalidArgument, "lease_uuid cannot be empty")
	}

	// Use simple GetLease for queries - auto-close only happens during transactions
	lease, err := q.k.GetLease(ctx, req.LeaseUuid)
	if err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}

	return &types.QueryLeaseResponse{Lease: lease}, nil
}

// Leases queries all leases with pagination.
func (q Querier) Leases(ctx context.Context, req *types.QueryLeasesRequest) (*types.QueryLeasesResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}

	// Use state index for efficient lookup when filtering by state
	if req.StateFilter != types.LEASE_STATE_UNSPECIFIED {
		iter, err := q.k.Leases.Indexes.State.MatchExact(ctx, int32(req.StateFilter))
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}

		leases, pageRes, err := pagination.PaginateStringIndex(
			ctx,
			iter,
			q.k.Leases.Get,
			req.Pagination,
			nil, // No additional filter needed since we already used the state index
		)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}

		return &types.QueryLeasesResponse{
			Leases:     leases,
			Pagination: pageRes,
		}, nil
	}

	leases, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.Leases,
		req.Pagination,
		func(_ string, lease types.Lease) (types.Lease, error) {
			return lease, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryLeasesResponse{
		Leases:     leases,
		Pagination: pageRes,
	}, nil
}

// LeasesByTenant queries leases by tenant address.
// Uses the Tenant index for efficient lookup - only iterates over leases belonging to this tenant.
func (q Querier) LeasesByTenant(ctx context.Context, req *types.QueryLeasesByTenantRequest) (*types.QueryLeasesByTenantResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}

	if req.Tenant == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant cannot be empty")
	}

	tenantAddr, err := sdk.AccAddressFromBech32(req.Tenant)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid tenant address")
	}

	// Use the tenant index to iterate only over this tenant's leases
	iter, err := q.k.Leases.Indexes.Tenant.MatchExact(ctx, tenantAddr)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	// Build filter based on state_filter
	var filter func(types.Lease) bool
	if req.StateFilter != types.LEASE_STATE_UNSPECIFIED {
		filter = func(l types.Lease) bool { return l.State == req.StateFilter }
	}

	leases, pageRes, err := pagination.PaginateStringIndex(
		ctx,
		iter,
		q.k.Leases.Get,
		req.Pagination,
		filter,
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryLeasesByTenantResponse{
		Leases:     leases,
		Pagination: pageRes,
	}, nil
}

// LeasesByProvider queries leases by provider ID.
// Uses the Provider index for efficient lookup - only iterates over leases belonging to this provider.
func (q Querier) LeasesByProvider(ctx context.Context, req *types.QueryLeasesByProviderRequest) (*types.QueryLeasesByProviderResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}

	if req.ProviderUuid == "" {
		return nil, status.Error(codes.InvalidArgument, "provider_uuid cannot be empty")
	}

	// Use the provider index to iterate only over this provider's leases
	iter, err := q.k.Leases.Indexes.Provider.MatchExact(ctx, req.ProviderUuid)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	// Build filter based on state_filter
	var filter func(types.Lease) bool
	if req.StateFilter != types.LEASE_STATE_UNSPECIFIED {
		filter = func(l types.Lease) bool { return l.State == req.StateFilter }
	}

	leases, pageRes, err := pagination.PaginateStringIndex(
		ctx,
		iter,
		q.k.Leases.Get,
		req.Pagination,
		filter,
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryLeasesByProviderResponse{
		Leases:     leases,
		Pagination: pageRes,
	}, nil
}

// CreditAccount queries a tenant's credit account.
func (q Querier) CreditAccount(ctx context.Context, req *types.QueryCreditAccountRequest) (*types.QueryCreditAccountResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}

	if req.Tenant == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant cannot be empty")
	}

	if _, err := sdk.AccAddressFromBech32(req.Tenant); err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid tenant address")
	}

	ca, err := q.k.GetCreditAccount(ctx, req.Tenant)
	if err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}

	// Fetch all balances from the bank module
	creditAddr, err := sdk.AccAddressFromBech32(ca.CreditAddress)
	if err != nil {
		return nil, status.Error(codes.Internal, "invalid credit address")
	}

	balances := q.k.bankKeeper.GetAllBalances(ctx, creditAddr)

	return &types.QueryCreditAccountResponse{
		CreditAccount: ca,
		Balances:      balances,
	}, nil
}

// CreditAddress derives the credit address for a tenant.
func (q Querier) CreditAddress(_ context.Context, req *types.QueryCreditAddressRequest) (*types.QueryCreditAddressResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}

	if req.Tenant == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant cannot be empty")
	}

	creditAddr, err := types.DeriveCreditAddressFromBech32(req.Tenant)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid tenant address")
	}

	return &types.QueryCreditAddressResponse{CreditAddress: creditAddr.String()}, nil
}

// WithdrawableAmount queries the amounts available for provider withdrawal from a lease.
func (q Querier) WithdrawableAmount(ctx context.Context, req *types.QueryWithdrawableAmountRequest) (*types.QueryWithdrawableAmountResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}

	if req.LeaseUuid == "" {
		return nil, status.Error(codes.InvalidArgument, "lease_uuid cannot be empty")
	}

	// Use simple GetLease for queries - auto-close only happens during transactions
	lease, err := q.k.GetLease(ctx, req.LeaseUuid)
	if err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}

	// Calculate withdrawable amounts based on accrual since last settlement
	withdrawableAmounts := q.k.CalculateWithdrawableForLease(ctx, lease)

	return &types.QueryWithdrawableAmountResponse{
		Amounts: withdrawableAmounts,
	}, nil
}

// ProviderWithdrawable queries the total amounts available for a provider to withdraw.
// This query uses streaming iteration with a configurable limit to prevent DoS attacks
// on RPC nodes for providers with many leases.
func (q Querier) ProviderWithdrawable(ctx context.Context, req *types.QueryProviderWithdrawableRequest) (*types.QueryProviderWithdrawableResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}

	if req.ProviderUuid == "" {
		return nil, status.Error(codes.InvalidArgument, "provider_uuid cannot be empty")
	}

	// Apply default limit if not specified, cap at maximum
	limit := req.Limit
	if limit == 0 {
		limit = types.DefaultProviderWithdrawableQueryLimit
	}
	if limit > types.MaxProviderWithdrawableQueryLimit {
		limit = types.MaxProviderWithdrawableQueryLimit
	}

	// Use streaming iteration to avoid loading all leases into memory
	totalWithdrawable := sdk.NewCoins()
	var leaseCount uint64
	var processedCount uint64
	hasMore := false

	err := q.k.IterateLeasesByProvider(ctx, req.ProviderUuid, func(lease types.Lease) (stop bool, iterErr error) {
		// Check if we've reached the limit
		if processedCount >= limit {
			hasMore = true
			return true, nil // Stop iteration
		}
		processedCount++

		withdrawable := q.k.CalculateWithdrawableForLease(ctx, lease)
		if !withdrawable.IsZero() {
			totalWithdrawable = totalWithdrawable.Add(withdrawable...)
			leaseCount++
		}

		return false, nil // Continue iteration
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryProviderWithdrawableResponse{
		Amounts:    totalWithdrawable,
		LeaseCount: leaseCount,
		HasMore:    hasMore,
	}, nil
}

// CreditAccounts queries all credit accounts with pagination.
func (q Querier) CreditAccounts(ctx context.Context, req *types.QueryCreditAccountsRequest) (*types.QueryCreditAccountsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}

	creditAccounts, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.CreditAccounts,
		req.Pagination,
		func(_ sdk.AccAddress, ca types.CreditAccount) (types.CreditAccount, error) {
			return ca, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryCreditAccountsResponse{
		CreditAccounts: creditAccounts,
		Pagination:     pageRes,
	}, nil
}

// LeasesBySKU queries leases by SKU UUID.
// Note: This query requires scanning all leases since there is no SKU index.
// For performance with large datasets, consider filtering by state.
func (q Querier) LeasesBySKU(ctx context.Context, req *types.QueryLeasesBySKURequest) (*types.QueryLeasesBySKUResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}

	if req.SkuUuid == "" {
		return nil, status.Error(codes.InvalidArgument, "sku_uuid cannot be empty")
	}

	// Build filter function that checks if lease contains the SKU
	filterFn := func(_ string, lease types.Lease) (bool, error) {
		// Check state filter first (cheaper check)
		if req.StateFilter != types.LEASE_STATE_UNSPECIFIED && lease.State != req.StateFilter {
			return false, nil
		}
		// Check if any lease item has the matching SKU
		for _, item := range lease.Items {
			if item.SkuUuid == req.SkuUuid {
				return true, nil
			}
		}
		return false, nil
	}

	leases, pageRes, err := query.CollectionFilteredPaginate(
		ctx,
		q.k.Leases,
		req.Pagination,
		filterFn,
		func(_ string, lease types.Lease) (types.Lease, error) {
			return lease, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryLeasesBySKUResponse{
		Leases:     leases,
		Pagination: pageRes,
	}, nil
}

// CreditEstimate estimates remaining lease duration for a tenant.
func (q Querier) CreditEstimate(ctx context.Context, req *types.QueryCreditEstimateRequest) (*types.QueryCreditEstimateResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}

	if req.Tenant == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant cannot be empty")
	}

	tenantAddr, err := sdk.AccAddressFromBech32(req.Tenant)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid tenant address")
	}

	// Get credit account
	ca, err := q.k.GetCreditAccount(ctx, req.Tenant)
	if err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}

	// Get current balance
	creditAddr, err := sdk.AccAddressFromBech32(ca.CreditAddress)
	if err != nil {
		return nil, status.Error(codes.Internal, "invalid credit address")
	}
	currentBalance := q.k.bankKeeper.GetAllBalances(ctx, creditAddr)

	// Calculate total rate per second across all active leases
	// Limited to MaxCreditEstimateLeases to prevent DoS on tenants with many leases
	totalRatePerSecond := sdk.NewCoins()
	var activeLeaseCount uint64
	var processedCount uint64

	// Use tenant index to iterate over tenant's leases
	iter, err := q.k.Leases.Indexes.Tenant.MatchExact(ctx, tenantAddr)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	defer iter.Close()

	for ; iter.Valid(); iter.Next() {
		// Limit total iterations to prevent DoS from accumulated historical leases
		processedCount++
		if processedCount > types.MaxCreditEstimateLeases*10 {
			// Allow 10x the active lease limit for iteration to account for historical leases
			break
		}

		leaseUUID, err := iter.PrimaryKey()
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}

		lease, err := q.k.Leases.Get(ctx, leaseUUID)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}

		if lease.State != types.LEASE_STATE_ACTIVE {
			continue // Skip non-active leases
		}
		activeLeaseCount++

		// Limit active leases processed (should match max_leases_per_tenant param)
		if activeLeaseCount > types.MaxCreditEstimateLeases {
			break
		}

		// Sum up rates for all items in this lease
		for _, item := range lease.Items {
			// Rate per second = locked_price * quantity
			// locked_price is already in per-second terms
			itemRate := sdk.NewCoin(item.LockedPrice.Denom, item.LockedPrice.Amount.Mul(sdkmath.NewIntFromUint64(item.Quantity)))
			totalRatePerSecond = totalRatePerSecond.Add(itemRate)
		}
	}

	// Calculate estimated duration
	// Find minimum duration across all denoms: min(balance[denom] / rate[denom])
	var estimatedDurationSeconds uint64
	if activeLeaseCount > 0 && !totalRatePerSecond.IsZero() {
		// Start with max uint64, then find minimum
		estimatedDurationSeconds = math.MaxUint64

		for _, rateCoin := range totalRatePerSecond {
			if rateCoin.Amount.IsZero() {
				continue
			}
			balanceAmount := currentBalance.AmountOf(rateCoin.Denom)
			if balanceAmount.IsZero() {
				// No balance for this denom means immediate exhaustion
				estimatedDurationSeconds = 0
				break
			}
			// Duration = balance / rate (integer division, rounds down)
			duration := balanceAmount.Quo(rateCoin.Amount).Uint64()
			if duration < estimatedDurationSeconds {
				estimatedDurationSeconds = duration
			}
		}

		// If we never found a matching denom, set to 0
		if estimatedDurationSeconds == math.MaxUint64 {
			estimatedDurationSeconds = 0
		}
	}

	return &types.QueryCreditEstimateResponse{
		CurrentBalance:           currentBalance,
		TotalRatePerSecond:       totalRatePerSecond,
		EstimatedDurationSeconds: estimatedDurationSeconds,
		ActiveLeaseCount:         activeLeaseCount,
	}, nil
}
