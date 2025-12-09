package keeper

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"

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

	if req.LeaseId == 0 {
		return nil, status.Error(codes.InvalidArgument, "lease_id cannot be zero")
	}

	lease, err := q.k.GetLease(ctx, req.LeaseId)
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

	// Use filtered pagination if active_only is set
	if req.ActiveOnly {
		leases, pageRes, err := query.CollectionFilteredPaginate(
			ctx,
			q.k.Leases,
			req.Pagination,
			func(_ uint64, lease types.Lease) (bool, error) {
				return lease.State == types.LEASE_STATE_ACTIVE, nil
			},
			func(_ uint64, lease types.Lease) (types.Lease, error) {
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

	leases, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.Leases,
		req.Pagination,
		func(_ uint64, lease types.Lease) (types.Lease, error) {
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
func (q Querier) LeasesByTenant(ctx context.Context, req *types.QueryLeasesByTenantRequest) (*types.QueryLeasesByTenantResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}

	if req.Tenant == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant cannot be empty")
	}

	if _, err := sdk.AccAddressFromBech32(req.Tenant); err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid tenant address")
	}

	// Use indexed pagination through the tenant index
	leases, pageRes, err := query.CollectionFilteredPaginate(
		ctx,
		q.k.Leases,
		req.Pagination,
		func(_ uint64, lease types.Lease) (bool, error) {
			// Filter by tenant
			if lease.Tenant != req.Tenant {
				return false, nil
			}
			// Filter by active_only if requested
			if req.ActiveOnly && lease.State != types.LEASE_STATE_ACTIVE {
				return false, nil
			}
			return true, nil
		},
		func(_ uint64, lease types.Lease) (types.Lease, error) {
			return lease, nil
		},
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
func (q Querier) LeasesByProvider(ctx context.Context, req *types.QueryLeasesByProviderRequest) (*types.QueryLeasesByProviderResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}

	if req.ProviderId == 0 {
		return nil, status.Error(codes.InvalidArgument, "provider_id cannot be zero")
	}

	// Use indexed pagination through the provider index
	leases, pageRes, err := query.CollectionFilteredPaginate(
		ctx,
		q.k.Leases,
		req.Pagination,
		func(_ uint64, lease types.Lease) (bool, error) {
			// Filter by provider
			if lease.ProviderId != req.ProviderId {
				return false, nil
			}
			// Filter by active_only if requested
			if req.ActiveOnly && lease.State != types.LEASE_STATE_ACTIVE {
				return false, nil
			}
			return true, nil
		},
		func(_ uint64, lease types.Lease) (types.Lease, error) {
			return lease, nil
		},
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

	// Fetch the balance from the bank module
	params, err := q.k.GetParams(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	creditAddr, err := sdk.AccAddressFromBech32(ca.CreditAddress)
	if err != nil {
		return nil, status.Error(codes.Internal, "invalid credit address")
	}

	balance := q.k.bankKeeper.GetBalance(ctx, creditAddr, params.Denom)

	return &types.QueryCreditAccountResponse{
		CreditAccount: ca,
		Balance:       balance,
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

// WithdrawableAmount queries the amount available for provider withdrawal from a lease.
func (q Querier) WithdrawableAmount(ctx context.Context, req *types.QueryWithdrawableAmountRequest) (*types.QueryWithdrawableAmountResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}

	if req.LeaseId == 0 {
		return nil, status.Error(codes.InvalidArgument, "lease_id cannot be zero")
	}

	lease, err := q.k.GetLease(ctx, req.LeaseId)
	if err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}

	params, err := q.k.GetParams(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	// Calculate withdrawable amount based on accrual since last settlement
	withdrawableAmount := q.k.CalculateWithdrawableForLease(ctx, lease)

	return &types.QueryWithdrawableAmountResponse{
		Amount: sdk.NewCoin(params.Denom, withdrawableAmount),
	}, nil
}

// ProviderWithdrawable queries the total amount available for a provider to withdraw.
func (q Querier) ProviderWithdrawable(ctx context.Context, req *types.QueryProviderWithdrawableRequest) (*types.QueryProviderWithdrawableResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}

	if req.ProviderId == 0 {
		return nil, status.Error(codes.InvalidArgument, "provider_id cannot be zero")
	}

	params, err := q.k.GetParams(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	leases, err := q.k.GetLeasesByProviderID(ctx, req.ProviderId)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	// Calculate total withdrawable and count leases with withdrawable amounts
	totalWithdrawable := math.ZeroInt()
	var leaseCount uint64

	for _, lease := range leases {
		withdrawable := q.k.CalculateWithdrawableForLease(ctx, lease)
		if withdrawable.IsPositive() {
			totalWithdrawable = totalWithdrawable.Add(withdrawable)
			leaseCount++
		}
	}

	return &types.QueryProviderWithdrawableResponse{
		Amount:     sdk.NewCoin(params.Denom, totalWithdrawable),
		LeaseCount: leaseCount,
	}, nil
}
